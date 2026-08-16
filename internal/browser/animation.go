package browser

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

type pageAnimationFrame struct {
	id         AnimationFrameID
	callback   ValueHandle
	ref        memory.Ref
	generation DocumentGeneration
	queued     bool
}

func (page *Page) performanceNowLocked() float64 {
	if page.timeOrigin.IsZero() {
		page.timeOrigin = time.Now()
	}
	value := float64(time.Since(page.timeOrigin).Nanoseconds()) / 1e6
	if value < 0 {
		return 0
	}
	return value
}

func (page *Page) requestAnimationFrameFromTask(task *browserruntime.TaskContext, callback ValueHandle) (AnimationFrameID, error) {
	if callback == 0 {
		return 0, fmt.Errorf("browser: invalid animation callback handle 0")
	}
	page.mutex.Lock()
	if page.closed {
		page.mutex.Unlock()
		return 0, ErrPageClosed
	}
	page.nextAnimationFrame++
	id := page.nextAnimationFrame
	generation := page.documentGeneration
	page.mutex.Unlock()

	local, err := task.NewHostObject(memory.HostObject{
		Class:    browserAnimationFrameHostClass,
		Scope:    uint64(generation),
		Identity: uint64(id),
	})
	if err != nil {
		return 0, err
	}
	retained, err := task.CopyToRealm(local)
	if err != nil {
		return 0, err
	}
	frame := &pageAnimationFrame{id: id, callback: callback, ref: retained[0], generation: generation}
	page.mutex.Lock()
	if page.closed || page.documentGeneration != generation {
		page.mutex.Unlock()
		_ = page.releaseAsyncRef(frame.ref)
		return 0, ErrStaleNodeHandle
	}
	page.animationFrames[id] = frame
	page.mutex.Unlock()
	return id, nil
}

func (page *Page) cancelAnimationFrame(id AnimationFrameID) error {
	if page == nil || id == 0 {
		return nil
	}
	page.mutex.Lock()
	frame := page.animationFrames[id]
	if frame != nil {
		delete(page.animationFrames, id)
	}
	page.mutex.Unlock()
	if frame == nil || frame.queued {
		return nil
	}
	return page.releaseAsyncRef(frame.ref)
}

func (page *Page) queueAnimationFrameFromTask() error {
	page.mutex.Lock()
	if page.closed {
		page.mutex.Unlock()
		return ErrPageClosed
	}
	batch := make([]*pageAnimationFrame, 0, len(page.animationFrames))
	refs := make([]memory.Ref, 0, len(page.animationFrames))
	for _, frame := range page.animationFrames {
		if frame.queued {
			continue
		}
		frame.queued = true
		batch = append(batch, frame)
		refs = append(refs, frame.ref)
	}
	slices.SortFunc(batch, func(left, right *pageAnimationFrame) int {
		return cmp.Compare(left.id, right.id)
	})
	refs = refs[:0]
	for _, frame := range batch {
		refs = append(refs, frame.ref)
	}
	page.mutex.Unlock()
	if len(batch) == 0 {
		return nil
	}
	_, err := page.Realm.EnqueueRealmRefTask(func(context *browserruntime.TaskContext) error {
		return page.runAnimationFrameBatch(context, batch)
	}, refs...)
	if err != nil {
		page.mutex.Lock()
		for _, frame := range batch {
			if page.animationFrames[frame.id] == frame {
				frame.queued = false
			}
		}
		page.mutex.Unlock()
	}
	return err
}

func (page *Page) runAnimationFrameBatch(context *browserruntime.TaskContext, batch []*pageAnimationFrame) error {
	if len(context.Refs) != len(batch) {
		return fmt.Errorf("browser: animation task has %d native records, want %d", len(context.Refs), len(batch))
	}
	page.mutex.Lock()
	timestamp := page.performanceNowLocked()
	if timestamp <= page.lastFrameTime {
		timestamp = page.lastFrameTime + .001
	}
	page.lastFrameTime = timestamp
	generation := page.documentGeneration
	script := page.script
	page.mutex.Unlock()

	host := &taskHost{page: page, task: context, generation: generation, autoRender: true}
	var result error
	for index, frame := range batch {
		record, err := context.DerefHostObject(context.Refs[index])
		want := memory.HostObject{Class: browserAnimationFrameHostClass, Scope: uint64(frame.generation), Identity: uint64(frame.id)}
		if err != nil || record != want {
			result = errors.Join(result, err)
			if err == nil {
				result = errors.Join(result, fmt.Errorf("browser: invalid animation host record: got %#v, want %#v", record, want))
			}
			continue
		}
		page.mutex.Lock()
		current := page.animationFrames[frame.id] == frame && !page.closed && frame.generation == page.documentGeneration
		if current {
			delete(page.animationFrames, frame.id)
		}
		page.mutex.Unlock()
		if !current || script == nil {
			continue
		}
		if animationRealm, ok := script.(AnimationFrameRealm); ok {
			result = errors.Join(result, animationRealm.InvokeAnimationFrame(host, frame.callback, timestamp))
		} else {
			result = errors.Join(result, script.Invoke(host, frame.callback))
		}
	}
	if script != nil {
		result = errors.Join(result, script.DrainMicrotasks(host))
	}
	return errors.Join(result, host.finish())
}

func (page *Page) takeAnimationFramesLocked() []memory.Ref {
	refs := make([]memory.Ref, 0, len(page.animationFrames))
	for id, frame := range page.animationFrames {
		delete(page.animationFrames, id)
		if !frame.queued {
			refs = append(refs, frame.ref)
		}
	}
	return refs
}

func (page *Page) releaseAnimationFrames(refs []memory.Ref) error {
	var result error
	for _, ref := range refs {
		result = errors.Join(result, page.releaseAsyncRef(ref))
	}
	return result
}

// QueueViewportResize publishes an external viewport change through the Page
// task queue. CSS environment invalidation is synchronous within that task;
// the resize event, microtasks, animation requests, and one render preserve
// normal task-boundary ordering.
func (page *Page) QueueViewportResize(viewport render.Viewport) (browserruntime.TaskID, error) {
	if page == nil {
		return 0, fmt.Errorf("browser: nil page")
	}
	if viewport.Width <= 0 || viewport.Height <= 0 {
		return 0, fmt.Errorf("browser: invalid viewport %dx%d", viewport.Width, viewport.Height)
	}
	return page.Realm.EnqueueTask(func(context *browserruntime.TaskContext) error {
		page.mutex.Lock()
		if page.closed {
			page.mutex.Unlock()
			return ErrPageClosed
		}
		changed := page.setViewportLocked(viewport)
		generation := page.documentGeneration
		script := page.script
		root, found, rootErr := page.document.RelatedNode(page.document.RootID(), dom.DocumentElement)
		page.mutex.Unlock()
		if !changed || rootErr != nil || !found {
			return rootErr
		}
		host := &taskHost{page: page, task: context, generation: generation, autoRender: true, resized: true}
		if script == nil {
			return host.finish()
		}
		_, dispatchErr := script.DispatchEvent(host, InputEvent{
			Type:   InputResize,
			Target: NodeHandle{Document: generation, Node: root},
		})
		microtaskErr := script.DrainMicrotasks(host)
		return errors.Join(dispatchErr, microtaskErr, host.finish())
	})
}
