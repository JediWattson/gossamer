package browser

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
)

// HitTest converts the renderer's transitional pointer result into stable
// identity from the exact document generation that produced the Frame.
func (page *Page) HitTest(x, y float64) (NodeHandle, bool) {
	if page == nil || math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) {
		return NodeHandle{}, false
	}
	page.mutex.RLock()
	defer page.mutex.RUnlock()
	return page.hitTestLocked(x, y)
}

func (page *Page) hitTestLocked(x, y float64) (NodeHandle, bool) {
	if page.closed || page.frame == nil || page.frameGeneration != page.documentGeneration {
		return NodeHandle{}, false
	}
	node := render.HitTest(page.frame, x, y)
	if node == nil {
		return NodeHandle{}, false
	}
	rawID, ok := page.document.ID(node)
	if !ok {
		return NodeHandle{}, false
	}
	elementID, ok := page.document.ClosestElement(rawID)
	if !ok {
		return NodeHandle{}, false
	}
	return NodeHandle{Document: page.documentGeneration, Node: elementID}, true
}

// QueueClick publishes external input through the Page task queue. Hit testing
// happens when that task executes, after all earlier Page work.
func (page *Page) QueueClick(x, y float64, button int) (browserruntime.TaskID, error) {
	if page == nil {
		return 0, fmt.Errorf("browser: nil page")
	}
	if math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) {
		return 0, fmt.Errorf("browser: invalid input coordinate")
	}
	task, _, err := page.browser.scheduler.EnqueueExternalTask(page.Realm, func(context *browserruntime.TaskContext) error {
		return page.dispatchInput(context, InputEvent{Type: InputClick, X: x, Y: y, Button: button})
	})
	return task, err
}

func (page *Page) dispatchInput(context *browserruntime.TaskContext, event InputEvent) error {
	page.mutex.RLock()
	if page.closed {
		page.mutex.RUnlock()
		return ErrPageClosed
	}
	target, ok := page.hitTestLocked(event.X, event.Y)
	script := page.script
	generation := page.documentGeneration
	page.mutex.RUnlock()
	if !ok || script == nil {
		return nil
	}
	event.Target = target
	host := &taskHost{page: page, task: context, generation: generation, autoRender: true}
	dispatchErr := script.DispatchEvent(host, event)
	microtaskErr := script.DrainMicrotasks(host)
	return errors.Join(dispatchErr, microtaskErr, host.finish())
}

func (page *Page) invokeScript(
	context *browserruntime.TaskContext,
	generation DocumentGeneration,
	callback ValueHandle,
	autoRender bool,
) error {
	page.mutex.RLock()
	if page.closed {
		page.mutex.RUnlock()
		return ErrPageClosed
	}
	if page.documentGeneration != generation {
		page.mutex.RUnlock()
		return nil
	}
	script := page.script
	page.mutex.RUnlock()
	if script == nil {
		return ErrScriptDisabled
	}
	host := &taskHost{page: page, task: context, generation: generation, autoRender: autoRender}
	invokeErr := script.Invoke(host, callback)
	microtaskErr := script.DrainMicrotasks(host)
	return errors.Join(invokeErr, microtaskErr, host.finish())
}

// QueueScript publishes source evaluation through the Page queue and binds it
// to the current document generation.
func (page *Page) QueueScript(source ScriptSource) (browserruntime.TaskID, error) {
	if page == nil {
		return 0, fmt.Errorf("browser: nil page")
	}
	page.mutex.RLock()
	generation := page.documentGeneration
	page.mutex.RUnlock()
	task, _, err := page.browser.scheduler.EnqueueExternalTask(page.Realm, func(context *browserruntime.TaskContext) error {
		return page.evaluateScript(context, generation, source)
	})
	return task, err
}

func (page *Page) evaluateScript(
	context *browserruntime.TaskContext,
	generation DocumentGeneration,
	source ScriptSource,
) error {
	page.mutex.RLock()
	if page.closed {
		page.mutex.RUnlock()
		return ErrPageClosed
	}
	if page.documentGeneration != generation {
		page.mutex.RUnlock()
		return nil
	}
	script := page.script
	page.mutex.RUnlock()
	if script == nil {
		return ErrScriptDisabled
	}
	host := &taskHost{page: page, task: context, generation: generation, autoRender: true}
	evaluateErr := script.Evaluate(host, source)
	microtaskErr := script.DrainMicrotasks(host)
	return errors.Join(evaluateErr, microtaskErr, host.finish())
}

type taskHost struct {
	page       *Page
	task       *browserruntime.TaskContext
	generation DocumentGeneration
	mutated    bool
	autoRender bool
}

func (host *taskHost) GetElementByID(value string) (NodeHandle, bool, error) {
	host.page.mutex.RLock()
	defer host.page.mutex.RUnlock()
	if host.page.closed {
		return NodeHandle{}, false, ErrPageClosed
	}
	if host.page.documentGeneration != host.generation {
		return NodeHandle{}, false, ErrStaleNodeHandle
	}
	node, ok := host.page.document.ElementByID(value)
	if !ok {
		return NodeHandle{}, false, nil
	}
	return NodeHandle{Document: host.generation, Node: node}, true, nil
}

func (host *taskHost) CreateElement(name string) (NodeHandle, error) {
	host.page.mutex.Lock()
	defer host.page.mutex.Unlock()
	if host.page.closed {
		return NodeHandle{}, ErrPageClosed
	}
	if host.page.documentGeneration != host.generation {
		return NodeHandle{}, ErrStaleNodeHandle
	}
	node, err := host.page.document.CreateElement(name)
	if err != nil {
		return NodeHandle{}, err
	}
	return NodeHandle{Document: host.generation, Node: node}, nil
}

func (host *taskHost) CreateTextNode(data string) (NodeHandle, error) {
	host.page.mutex.Lock()
	defer host.page.mutex.Unlock()
	if host.page.closed {
		return NodeHandle{}, ErrPageClosed
	}
	if host.page.documentGeneration != host.generation {
		return NodeHandle{}, ErrStaleNodeHandle
	}
	node, err := host.page.document.CreateTextNode(data)
	if err != nil {
		return NodeHandle{}, err
	}
	return NodeHandle{Document: host.generation, Node: node}, nil
}

func (host *taskHost) TextContent(handle NodeHandle) (string, error) {
	host.page.mutex.RLock()
	defer host.page.mutex.RUnlock()
	if err := host.validateHandleLocked(handle); err != nil {
		return "", err
	}
	return host.page.document.TextContent(handle.Node)
}

func (host *taskHost) SetTextContent(handle NodeHandle, data string) error {
	host.page.mutex.Lock()
	defer host.page.mutex.Unlock()
	if err := host.validateHandleLocked(handle); err != nil {
		return err
	}
	before := host.page.document.Version()
	if err := host.page.document.SetTextContent(handle.Node, data); err != nil {
		return err
	}
	if host.page.document.Version() != before {
		host.page.dirty = true
		host.mutated = true
	}
	return nil
}

func (host *taskHost) AppendChild(parent, child NodeHandle) error {
	return host.mutateNodes(parent, child, NodeHandle{}, func() error {
		return host.page.document.AppendNode(parent.Node, child.Node)
	})
}

func (host *taskHost) InsertBefore(parent, child, reference NodeHandle) error {
	return host.mutateNodes(parent, child, reference, func() error {
		return host.page.document.InsertBefore(parent.Node, child.Node, reference.Node)
	})
}

func (host *taskHost) RemoveChild(parent, child NodeHandle) error {
	return host.mutateNodes(parent, child, NodeHandle{}, func() error {
		return host.page.document.RemoveChild(parent.Node, child.Node)
	})
}

func (host *taskHost) GetAttribute(handle NodeHandle, name string) (string, bool, error) {
	host.page.mutex.RLock()
	defer host.page.mutex.RUnlock()
	if err := host.validateHandleLocked(handle); err != nil {
		return "", false, err
	}
	return host.page.document.GetAttribute(handle.Node, name)
}

func (host *taskHost) SetAttribute(handle NodeHandle, name, value string) error {
	return host.mutateNodes(handle, NodeHandle{}, NodeHandle{}, func() error {
		return host.page.document.SetAttribute(handle.Node, name, value)
	})
}

func (host *taskHost) RemoveAttribute(handle NodeHandle, name string) error {
	return host.mutateNodes(handle, NodeHandle{}, NodeHandle{}, func() error {
		return host.page.document.RemoveAttribute(handle.Node, name)
	})
}

func (host *taskHost) mutateNodes(first, second, third NodeHandle, mutation func() error) error {
	host.page.mutex.Lock()
	defer host.page.mutex.Unlock()
	for _, handle := range []NodeHandle{first, second, third} {
		if handle.Node == dom.InvalidNodeID {
			continue
		}
		if err := host.validateHandleLocked(handle); err != nil {
			return err
		}
	}
	before := host.page.document.Version()
	if err := mutation(); err != nil {
		return err
	}
	if host.page.document.Version() != before {
		host.page.dirty = true
		host.mutated = true
	}
	return nil
}

func (host *taskHost) Text(handle NodeHandle) (string, error) {
	host.page.mutex.RLock()
	defer host.page.mutex.RUnlock()
	if err := host.validateHandleLocked(handle); err != nil {
		return "", err
	}
	return host.page.document.Text(handle.Node)
}

func (host *taskHost) SetText(handle NodeHandle, data string) error {
	host.page.mutex.Lock()
	defer host.page.mutex.Unlock()
	if err := host.validateHandleLocked(handle); err != nil {
		return err
	}
	before := host.page.document.Version()
	if err := host.page.document.SetText(handle.Node, data); err != nil {
		return err
	}
	if host.page.document.Version() != before {
		host.page.dirty = true
		host.mutated = true
	}
	return nil
}

func (host *taskHost) QueueCallback(callback ValueHandle) error {
	object, err := host.task.NewObject()
	if err != nil {
		return err
	}
	_, err = host.task.QueueTask(func(next *browserruntime.TaskContext) error {
		return host.page.invokeScript(next, host.generation, callback, host.autoRender)
	}, object)
	return err
}

func (host *taskHost) QueueMicrotask(callback ValueHandle) error {
	object, err := host.task.NewObject()
	if err != nil {
		return err
	}
	_, err = host.task.QueueMicrotask(func(next *browserruntime.TaskContext) error {
		return host.page.invokeScript(next, host.generation, callback, host.autoRender)
	}, object)
	return err
}

func (host *taskHost) SetTimeout(callback ValueHandle, delay time.Duration) (TimerID, error) {
	return host.page.setTimeoutFromTask(host.task, callback, delay)
}

func (host *taskHost) ClearTimeout(timer TimerID) error {
	return host.page.clearTimeout(timer)
}

func (host *taskHost) finish() error {
	if !host.mutated || !host.autoRender {
		return nil
	}
	return host.page.queueRenderFromTask(host.task)
}

func (host *taskHost) validateHandleLocked(handle NodeHandle) error {
	if host.page.closed {
		return ErrPageClosed
	}
	if handle.Document == 0 || handle.Node == dom.InvalidNodeID ||
		host.page.documentGeneration != handle.Document {
		return ErrStaleNodeHandle
	}
	_, ok := host.page.document.Resolve(handle.Node)
	if !ok {
		return dom.ErrUnknownNode
	}
	return nil
}

var ErrScriptDisabled = errors.New("browser: scripting is disabled for page")
