package browser

import (
	"errors"
	"fmt"
	"time"

	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

type TimerID uint64

type pageTimer struct {
	id         TimerID
	callback   ValueHandle
	ref        memory.Ref
	generation DocumentGeneration
	clock      *time.Timer
	done       chan struct{}
}

func (page *Page) setTimeoutFromTask(
	task *browserruntime.TaskContext,
	callback ValueHandle,
	delay time.Duration,
) (TimerID, error) {
	if callback == 0 {
		return 0, fmt.Errorf("browser: invalid callback handle 0")
	}
	if delay < 0 {
		delay = 0
	}

	page.mutex.Lock()
	if page.closed {
		page.mutex.Unlock()
		return 0, ErrPageClosed
	}
	page.nextTimer++
	id := page.nextTimer
	generation := page.documentGeneration
	page.mutex.Unlock()

	local, err := task.NewHostObject(memory.HostObject{
		Class:    browserTimerHostClass,
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
	ref := retained[0]

	page.mutex.Lock()
	if page.closed {
		page.mutex.Unlock()
		_ = page.releaseAsyncRef(ref)
		return 0, ErrPageClosed
	}
	if generation != page.documentGeneration {
		page.mutex.Unlock()
		_ = page.releaseAsyncRef(ref)
		return 0, ErrStaleNodeHandle
	}
	timer := &pageTimer{
		id:         id,
		callback:   callback,
		ref:        ref,
		generation: generation,
		clock:      time.NewTimer(delay),
		done:       make(chan struct{}),
	}
	page.timers[id] = timer
	page.mutex.Unlock()

	go func() {
		select {
		case <-timer.clock.C:
			page.fireTimer(id)
		case <-timer.done:
		}
	}()
	return id, nil
}

func (page *Page) clearTimeout(id TimerID) error {
	if page == nil || id == 0 {
		return nil
	}
	page.mutex.Lock()
	timer := page.timers[id]
	if timer != nil {
		delete(page.timers, id)
		timer.clock.Stop()
		close(timer.done)
	}
	page.mutex.Unlock()
	if timer == nil {
		return nil
	}
	return page.releaseAsyncRef(timer.ref)
}

func (page *Page) fireTimer(id TimerID) {
	page.mutex.Lock()
	timer := page.timers[id]
	if timer == nil {
		page.mutex.Unlock()
		return
	}
	delete(page.timers, id)
	current := !page.closed && timer.generation == page.documentGeneration && page.script != nil
	page.mutex.Unlock()

	if !current {
		_ = page.releaseAsyncRef(timer.ref)
		return
	}
	_, err := page.Realm.EnqueueRealmRefTask(func(task *browserruntime.TaskContext) error {
		return page.invokeAsyncScript(task, browserTimerHostClass, timer.generation, uint64(timer.id), timer.callback, true)
	}, timer.ref)
	if err != nil {
		_ = page.releaseAsyncRef(timer.ref)
	}
}

func (page *Page) takeTimersLocked() []*pageTimer {
	timers := make([]*pageTimer, 0, len(page.timers))
	for id, timer := range page.timers {
		delete(page.timers, id)
		timer.clock.Stop()
		close(timer.done)
		timers = append(timers, timer)
	}
	return timers
}

func (page *Page) releaseTimers(timers []*pageTimer) error {
	var result error
	for _, timer := range timers {
		result = errors.Join(result, page.releaseAsyncRef(timer.ref))
	}
	return result
}
