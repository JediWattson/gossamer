package browser

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/JediWattson/gossamer/internal/css"
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
	return page.QueueInputEvent(InputEvent{Type: InputClick, X: x, Y: y, Button: button})
}

// QueueInputEvent publishes one normalized input record through the Page task
// queue. Pointer-family events may omit Target and resolve it by hit testing
// when their task runs; keyboard, input, focus, and change events carry an
// explicit generation-safe target.
func (page *Page) QueueInputEvent(event InputEvent) (browserruntime.TaskID, error) {
	if page == nil {
		return 0, fmt.Errorf("browser: nil page")
	}
	if event.Type.String() == "" {
		return 0, fmt.Errorf("browser: unsupported input event type %d", event.Type)
	}
	if math.IsNaN(event.X) || math.IsNaN(event.Y) || math.IsInf(event.X, 0) || math.IsInf(event.Y, 0) {
		return 0, fmt.Errorf("browser: invalid input coordinate")
	}
	if event.Target.Node == dom.InvalidNodeID && !event.Type.pointerTargeted() {
		return 0, fmt.Errorf("browser: %s input requires a target", event.Type)
	}
	task, _, err := page.browser.scheduler.EnqueueExternalTask(page.Realm, func(context *browserruntime.TaskContext) error {
		return page.dispatchInput(context, event)
	})
	return task, err
}

func (page *Page) dispatchInput(context *browserruntime.TaskContext, event InputEvent) error {
	page.mutex.RLock()
	if page.closed {
		page.mutex.RUnlock()
		return ErrPageClosed
	}
	target := event.Target
	ok := false
	if target.Node == dom.InvalidNodeID {
		target, ok = page.hitTestLocked(event.X, event.Y)
	} else if target.Document == page.documentGeneration {
		_, ok = page.document.Resolve(target.Node)
	}
	if event.RelatedTarget.Node != dom.InvalidNodeID {
		if event.RelatedTarget.Document != page.documentGeneration {
			page.mutex.RUnlock()
			return nil
		}
		if _, relatedOK := page.document.Resolve(event.RelatedTarget.Node); !relatedOK {
			page.mutex.RUnlock()
			return nil
		}
	}
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
	if err := host.page.nodeLifetimes.sync(host.task); err != nil {
		return NodeHandle{}, err
	}
	return NodeHandle{Document: host.generation, Node: node}, nil
}

func (host *taskHost) CreateElementNS(namespaceURI, qualifiedName string) (NodeHandle, error) {
	host.page.mutex.Lock()
	defer host.page.mutex.Unlock()
	if host.page.closed {
		return NodeHandle{}, ErrPageClosed
	}
	if host.page.documentGeneration != host.generation {
		return NodeHandle{}, ErrStaleNodeHandle
	}
	node, err := host.page.document.CreateElementNS(namespaceURI, qualifiedName)
	if err != nil {
		return NodeHandle{}, err
	}
	if err := host.page.nodeLifetimes.sync(host.task); err != nil {
		return NodeHandle{}, err
	}
	return NodeHandle{Document: host.generation, Node: node}, nil
}

func (host *taskHost) DocumentMetadata() (DocumentMetadata, error) {
	host.page.mutex.RLock()
	defer host.page.mutex.RUnlock()
	if host.page.closed {
		return DocumentMetadata{}, ErrPageClosed
	}
	if host.page.documentGeneration != host.generation {
		return DocumentMetadata{}, ErrStaleNodeHandle
	}
	baseURI := "about:blank"
	if host.page.location != nil {
		baseURI = host.page.location.String()
	}
	return DocumentMetadata{
		Root:    NodeHandle{Document: host.generation, Node: host.page.document.RootID()},
		BaseURI: baseURI,
	}, nil
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
	if err := host.page.nodeLifetimes.sync(host.task); err != nil {
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
		if err := host.page.nodeLifetimes.sync(host.task); err != nil {
			return err
		}
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

func (host *taskHost) NodeMetadata(handle NodeHandle) (NodeMetadata, error) {
	host.page.mutex.RLock()
	defer host.page.mutex.RUnlock()
	if err := host.validateHandleLocked(handle); err != nil {
		return NodeMetadata{}, err
	}
	snapshot, err := host.page.document.Snapshot(handle.Node)
	if err != nil {
		return NodeMetadata{}, err
	}
	return domNodeMetadata(snapshot), nil
}

func (host *taskHost) RelatedNode(handle NodeHandle, relation NodeRelation) (NodeHandle, bool, error) {
	host.page.mutex.RLock()
	defer host.page.mutex.RUnlock()
	if err := host.validateHandleLocked(handle); err != nil {
		return NodeHandle{}, false, err
	}
	domRelation, ok := browserNodeRelation(relation)
	if !ok {
		return NodeHandle{}, false, fmt.Errorf("browser: unknown node relation %d", relation)
	}
	related, found, err := host.page.document.RelatedNode(handle.Node, domRelation)
	if err != nil || !found {
		return NodeHandle{}, found, err
	}
	return NodeHandle{Document: host.generation, Node: related}, true, nil
}

func (host *taskHost) ChildNodes(handle NodeHandle, elementsOnly bool) ([]NodeHandle, error) {
	host.page.mutex.RLock()
	defer host.page.mutex.RUnlock()
	if err := host.validateHandleLocked(handle); err != nil {
		return nil, err
	}
	nodes, err := host.page.document.ChildNodes(handle.Node, elementsOnly)
	if err != nil {
		return nil, err
	}
	handles := make([]NodeHandle, len(nodes))
	for index, node := range nodes {
		handles[index] = NodeHandle{Document: host.generation, Node: node}
	}
	return handles, nil
}

func (host *taskHost) Contains(handle, other NodeHandle) (bool, error) {
	host.page.mutex.RLock()
	defer host.page.mutex.RUnlock()
	if err := host.validateHandleLocked(handle); err != nil {
		return false, err
	}
	if err := host.validateHandleLocked(other); err != nil {
		return false, err
	}
	return host.page.document.Contains(handle.Node, other.Node)
}

func (host *taskHost) ReplaceChild(parent, child, replaced NodeHandle) error {
	return host.mutateNodes(parent, child, replaced, func() error {
		return host.page.document.ReplaceChild(parent.Node, child.Node, replaced.Node)
	})
}

func (host *taskHost) NodeValue(handle NodeHandle) (string, bool, error) {
	host.page.mutex.RLock()
	defer host.page.mutex.RUnlock()
	if err := host.validateHandleLocked(handle); err != nil {
		return "", false, err
	}
	return host.page.document.NodeValue(handle.Node)
}

func (host *taskHost) SetNodeValue(handle NodeHandle, value string) error {
	return host.mutateNodes(handle, NodeHandle{}, NodeHandle{}, func() error {
		return host.page.document.SetNodeValue(handle.Node, value)
	})
}

func (host *taskHost) HasAttribute(handle NodeHandle, name string) (bool, error) {
	host.page.mutex.RLock()
	defer host.page.mutex.RUnlock()
	if err := host.validateHandleLocked(handle); err != nil {
		return false, err
	}
	return host.page.document.HasAttribute(handle.Node, name)
}

func (host *taskHost) AttributeNames(handle NodeHandle) ([]string, error) {
	host.page.mutex.RLock()
	defer host.page.mutex.RUnlock()
	if err := host.validateHandleLocked(handle); err != nil {
		return nil, err
	}
	return host.page.document.AttributeNames(handle.Node)
}

func (host *taskHost) StyleCSSText(handle NodeHandle) (string, error) {
	host.page.mutex.RLock()
	defer host.page.mutex.RUnlock()
	if err := host.validateHandleLocked(handle); err != nil {
		return "", err
	}
	source, _, err := host.page.document.GetAttribute(handle.Node, "style")
	if err != nil {
		return "", err
	}
	return css.SerializeDeclarationList(css.ParseDeclarationList(source)), nil
}

func (host *taskHost) SetStyleCSSText(handle NodeHandle, source string) error {
	normalized := css.SerializeDeclarationList(css.ParseDeclarationList(source))
	return host.mutateNodes(handle, NodeHandle{}, NodeHandle{}, func() error {
		if normalized == "" {
			return host.page.document.RemoveAttribute(handle.Node, "style")
		}
		return host.page.document.SetAttribute(handle.Node, "style", normalized)
	})
}

func (host *taskHost) StyleProperty(handle NodeHandle, property string) (string, string, bool, error) {
	host.page.mutex.RLock()
	defer host.page.mutex.RUnlock()
	if err := host.validateHandleLocked(handle); err != nil {
		return "", "", false, err
	}
	source, _, err := host.page.document.GetAttribute(handle.Node, "style")
	if err != nil {
		return "", "", false, err
	}
	value, important, found := css.DeclarationValue(css.ParseDeclarationList(source), property)
	priority := ""
	if important {
		priority = "important"
	}
	return value, priority, found, nil
}

func (host *taskHost) SetStyleProperty(handle NodeHandle, property, value, priority string) error {
	if strings.TrimSpace(value) == "" {
		_, err := host.RemoveStyleProperty(handle, property)
		return err
	}
	priority = strings.TrimSpace(priority)
	if priority != "" && !strings.EqualFold(priority, "important") {
		return nil
	}
	return host.mutateNodes(handle, NodeHandle{}, NodeHandle{}, func() error {
		source, _, err := host.page.document.GetAttribute(handle.Node, "style")
		if err != nil {
			return err
		}
		declarations := css.ParseDeclarationList(source)
		updated, changed := css.SetDeclaration(declarations, property, value, priority != "")
		if !changed {
			return nil
		}
		return host.setInlineStyleLocked(handle.Node, updated)
	})
}

func (host *taskHost) RemoveStyleProperty(handle NodeHandle, property string) (string, error) {
	previous := ""
	err := host.mutateNodes(handle, NodeHandle{}, NodeHandle{}, func() error {
		source, _, err := host.page.document.GetAttribute(handle.Node, "style")
		if err != nil {
			return err
		}
		updated, value, changed := css.RemoveDeclaration(css.ParseDeclarationList(source), property)
		previous = value
		if !changed {
			return nil
		}
		return host.setInlineStyleLocked(handle.Node, updated)
	})
	return previous, err
}

func (host *taskHost) StylePropertyNames(handle NodeHandle) ([]string, error) {
	host.page.mutex.RLock()
	defer host.page.mutex.RUnlock()
	if err := host.validateHandleLocked(handle); err != nil {
		return nil, err
	}
	source, _, err := host.page.document.GetAttribute(handle.Node, "style")
	if err != nil {
		return nil, err
	}
	return css.DeclarationPropertyNames(css.ParseDeclarationList(source)), nil
}

func (host *taskHost) setInlineStyleLocked(node dom.NodeID, declarations []css.Declaration) error {
	serialized := css.SerializeDeclarationList(declarations)
	if serialized == "" {
		return host.page.document.RemoveAttribute(node, "style")
	}
	return host.page.document.SetAttribute(node, "style", serialized)
}

func browserNodeRelation(relation NodeRelation) (dom.NodeRelation, bool) {
	switch relation {
	case RelationParentNode:
		return dom.ParentNode, true
	case RelationParentElement:
		return dom.ParentElement, true
	case RelationFirstChild:
		return dom.FirstChild, true
	case RelationLastChild:
		return dom.LastChild, true
	case RelationPreviousSibling:
		return dom.PreviousSibling, true
	case RelationNextSibling:
		return dom.NextSibling, true
	case RelationFirstElementChild:
		return dom.FirstElementChild, true
	case RelationLastElementChild:
		return dom.LastElementChild, true
	case RelationPreviousElementSibling:
		return dom.PreviousElementSibling, true
	case RelationNextElementSibling:
		return dom.NextElementSibling, true
	case RelationDocumentElement:
		return dom.DocumentElement, true
	case RelationDocumentHead:
		return dom.DocumentHead, true
	case RelationDocumentBody:
		return dom.DocumentBody, true
	default:
		return 0, false
	}
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
		if err := host.page.nodeLifetimes.sync(host.task); err != nil {
			return err
		}
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

func (host *taskHost) RetainNodeWrapper(handle NodeHandle) error {
	return host.page.RetainNodeWrapper(handle)
}

func (host *taskHost) ReleaseNodeWrappers(handles []NodeHandle) error {
	return host.page.ReleaseNodeWrappers(handles)
}

func (host *taskHost) RetainNodeEventTarget(handle NodeHandle) error {
	return host.page.RetainNodeEventTarget(handle)
}

func (host *taskHost) ReleaseNodeEventTarget(handle NodeHandle) error {
	return host.page.ReleaseNodeEventTarget(handle)
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
