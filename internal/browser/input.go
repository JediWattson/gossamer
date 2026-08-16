package browser

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
	htmlparser "github.com/JediWattson/gossamer/internal/html"
	"github.com/JediWattson/gossamer/internal/render"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
	computed "github.com/JediWattson/gossamer/internal/style"
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
	if event.Type == InputBeforeInput {
		result, beforeInputErr := script.DispatchEvent(host, event)
		var editErr error
		var inputErr error
		if !result.DefaultPrevented {
			editErr = host.ReplaceFormSelection(event.Target, event.Data, event.InputType)
			if errors.Is(editErr, dom.ErrWrongNodeKind) {
				editErr = nil
			} else if editErr == nil {
				inputEvent := event
				inputEvent.Type = InputInput
				_, inputErr = script.DispatchEvent(host, inputEvent)
			}
		}
		microtaskErr := script.DrainMicrotasks(host)
		return errors.Join(beforeInputErr, editErr, inputErr, microtaskErr, host.finish())
	}
	rollback, defaultErr := host.applyInputStateBeforeDispatch(event)
	result, dispatchErr := script.DispatchEvent(host, event)
	if result.DefaultPrevented && rollback != nil {
		defaultErr = errors.Join(defaultErr, rollback())
	} else if !result.DefaultPrevented && event.Type == InputClick {
		defaultErr = errors.Join(defaultErr, host.Focus(event.Target))
	}
	microtaskErr := script.DrainMicrotasks(host)
	return errors.Join(defaultErr, dispatchErr, microtaskErr, host.finish())
}

func (host *taskHost) applyInputStateBeforeDispatch(event InputEvent) (func() error, error) {
	switch event.Type {
	case InputFocus, InputFocusIn:
		return nil, host.Focus(event.Target)
	case InputBlur, InputFocusOut:
		return nil, host.Blur(event.Target)
	case InputInput:
		err := host.ReplaceFormSelection(event.Target, event.Data, event.InputType)
		if err != nil {
			if errors.Is(err, dom.ErrWrongNodeKind) {
				return nil, nil
			}
			return nil, err
		}
		return nil, nil
	case InputClick:
		host.page.mutex.RLock()
		if err := host.validateHandleLocked(event.Target); err != nil {
			host.page.mutex.RUnlock()
			return nil, err
		}
		node, _ := host.page.document.Resolve(event.Target.Node)
		isInput := node.Type == dom.ElementNode && node.Data == "input"
		inputType := ""
		if isInput {
			inputType, _, _ = host.page.document.GetAttribute(event.Target.Node, "type")
		}
		host.page.mutex.RUnlock()
		if !isInput {
			return nil, nil
		}
		previous, err := host.FormChecked(event.Target)
		if err != nil {
			return nil, err
		}
		switch {
		case strings.EqualFold(inputType, "checkbox"):
			if err := host.SetFormChecked(event.Target, !previous); err != nil {
				return nil, err
			}
			return func() error { return host.SetFormChecked(event.Target, previous) }, nil
		case strings.EqualFold(inputType, "radio"):
			if previous {
				return nil, nil
			}
			host.page.mutex.RLock()
			group, groupErr := host.page.document.RadioGroupNodes(event.Target.Node)
			host.page.mutex.RUnlock()
			if groupErr != nil {
				return nil, groupErr
			}
			var prior NodeHandle
			for _, candidate := range group {
				handle := NodeHandle{Document: host.generation, Node: candidate}
				checked, checkedErr := host.FormChecked(handle)
				if checkedErr != nil {
					return nil, checkedErr
				}
				if checked {
					prior = handle
					break
				}
			}
			if err := host.SetFormChecked(event.Target, true); err != nil {
				return nil, err
			}
			return func() error {
				err := host.SetFormChecked(event.Target, false)
				if prior.Node != dom.InvalidNodeID {
					err = errors.Join(err, host.SetFormChecked(prior, true))
				}
				return err
			}, nil
		default:
			return nil, nil
		}
	default:
		return nil, nil
	}
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

var _ DOMComputedStyleHost = (*taskHost)(nil)
var _ DOMMutationObserverHost = (*taskHost)(nil)

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

func (host *taskHost) CreateDocumentFragment() (NodeHandle, error) {
	host.page.mutex.Lock()
	defer host.page.mutex.Unlock()
	if host.page.closed {
		return NodeHandle{}, ErrPageClosed
	}
	if host.page.documentGeneration != host.generation {
		return NodeHandle{}, ErrStaleNodeHandle
	}
	node, err := host.page.document.CreateDocumentFragment()
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

func (host *taskHost) QuerySelector(handle NodeHandle, source string, all bool) ([]NodeHandle, error) {
	selectors, err := css.ParseSelectorList(source)
	if err != nil {
		return nil, dom.NewException(dom.SyntaxError, err, "invalid selector %q", source)
	}
	host.page.mutex.RLock()
	defer host.page.mutex.RUnlock()
	if err := host.validateHandleLocked(handle); err != nil {
		return nil, err
	}
	scope, ok := host.page.document.Resolve(handle.Node)
	if !ok {
		return nil, dom.ErrUnknownNode
	}
	if scope.Type != dom.DocumentNode && scope.Type != dom.ElementNode && scope.Type != dom.DocumentFragmentNode {
		return nil, fmt.Errorf("%w: selector scope is not a document, fragment, or element", dom.ErrWrongNodeKind)
	}
	stack := make([]*dom.Node, 0, len(scope.Children))
	for index := len(scope.Children) - 1; index >= 0; index-- {
		stack = append(stack, scope.Children[index])
	}
	var matches []NodeHandle
	for len(stack) != 0 {
		last := len(stack) - 1
		node := stack[last]
		stack = stack[:last]
		if node.Type == dom.ElementNode && css.MatchesAny(selectors, node) {
			id, found := host.page.document.ID(node)
			if !found {
				return nil, dom.ErrUnknownNode
			}
			matches = append(matches, NodeHandle{Document: host.generation, Node: id})
			if !all {
				return matches, nil
			}
		}
		for index := len(node.Children) - 1; index >= 0; index-- {
			stack = append(stack, node.Children[index])
		}
	}
	return matches, nil
}

func (host *taskHost) MatchesSelector(handle NodeHandle, source string) (bool, error) {
	selectors, err := css.ParseSelectorList(source)
	if err != nil {
		return false, dom.NewException(dom.SyntaxError, err, "invalid selector %q", source)
	}
	host.page.mutex.RLock()
	defer host.page.mutex.RUnlock()
	if err := host.validateHandleLocked(handle); err != nil {
		return false, err
	}
	node, ok := host.page.document.Resolve(handle.Node)
	if !ok {
		return false, dom.ErrUnknownNode
	}
	if node.Type != dom.ElementNode {
		return false, fmt.Errorf("%w: selector target is not an element", dom.ErrWrongNodeKind)
	}
	return css.MatchesAny(selectors, node), nil
}

func (host *taskHost) ClosestSelector(handle NodeHandle, source string) (NodeHandle, bool, error) {
	selectors, err := css.ParseSelectorList(source)
	if err != nil {
		return NodeHandle{}, false, dom.NewException(dom.SyntaxError, err, "invalid selector %q", source)
	}
	host.page.mutex.RLock()
	defer host.page.mutex.RUnlock()
	if err := host.validateHandleLocked(handle); err != nil {
		return NodeHandle{}, false, err
	}
	node, ok := host.page.document.Resolve(handle.Node)
	if !ok {
		return NodeHandle{}, false, dom.ErrUnknownNode
	}
	if node.Type != dom.ElementNode {
		return NodeHandle{}, false, fmt.Errorf("%w: closest target is not an element", dom.ErrWrongNodeKind)
	}
	for candidate := node; candidate != nil; candidate = candidate.Parent {
		if candidate.Type != dom.ElementNode || !css.MatchesAny(selectors, candidate) {
			continue
		}
		id, found := host.page.document.ID(candidate)
		if !found {
			return NodeHandle{}, false, dom.ErrUnknownNode
		}
		return NodeHandle{Document: host.generation, Node: id}, true, nil
	}
	return NodeHandle{}, false, nil
}

func (host *taskHost) CloneNode(handle NodeHandle, deep bool) (NodeHandle, error) {
	host.page.mutex.Lock()
	defer host.page.mutex.Unlock()
	if err := host.validateHandleLocked(handle); err != nil {
		return NodeHandle{}, err
	}
	clone, err := host.page.document.CloneNode(handle.Node, deep)
	if err != nil {
		return NodeHandle{}, err
	}
	if err := host.page.nodeLifetimes.sync(host.task); err != nil {
		return NodeHandle{}, err
	}
	return NodeHandle{Document: host.generation, Node: clone}, nil
}

func (host *taskHost) TemplateContent(handle NodeHandle) (NodeHandle, error) {
	host.page.mutex.Lock()
	defer host.page.mutex.Unlock()
	if err := host.validateHandleLocked(handle); err != nil {
		return NodeHandle{}, err
	}
	content, err := host.page.document.TemplateContent(handle.Node)
	if err != nil {
		return NodeHandle{}, err
	}
	if err := host.page.nodeLifetimes.sync(host.task); err != nil {
		return NodeHandle{}, err
	}
	return NodeHandle{Document: host.generation, Node: content}, nil
}

func (host *taskHost) SplitText(handle NodeHandle, offset int) (NodeHandle, error) {
	var split dom.NodeID
	err := host.mutateNodes(handle, NodeHandle{}, NodeHandle{}, func() error {
		var err error
		split, err = host.page.document.SplitText(handle.Node, offset)
		return err
	})
	if err != nil {
		return NodeHandle{}, err
	}
	return NodeHandle{Document: host.generation, Node: split}, nil
}

func (host *taskHost) Normalize(handle NodeHandle) error {
	return host.mutateNodes(handle, NodeHandle{}, NodeHandle{}, func() error {
		return host.page.document.Normalize(handle.Node)
	})
}

func (host *taskHost) AdoptNode(handle NodeHandle) (NodeHandle, error) {
	var adopted dom.NodeID
	err := host.mutateNodes(handle, NodeHandle{}, NodeHandle{}, func() error {
		var err error
		adopted, err = host.page.document.AdoptNode(handle.Node)
		return err
	})
	if err != nil {
		return NodeHandle{}, err
	}
	return NodeHandle{Document: host.generation, Node: adopted}, nil
}

func (host *taskHost) RangeContents(
	start NodeHandle,
	startOffset int,
	end NodeHandle,
	endOffset int,
	operation dom.RangeContentOperation,
) (NodeHandle, error) {
	host.page.mutex.Lock()
	defer host.page.mutex.Unlock()
	if err := host.validateHandleLocked(start); err != nil {
		return NodeHandle{}, err
	}
	if err := host.validateHandleLocked(end); err != nil {
		return NodeHandle{}, err
	}
	before := host.page.document.Version()
	fragment, err := host.page.document.RangeContents(start.Node, startOffset, end.Node, endOffset, operation)
	if err != nil {
		return NodeHandle{}, err
	}
	if host.page.document.Version() != before {
		host.page.dirty = true
		host.mutated = true
	}
	if err := host.page.nodeLifetimes.sync(host.task); err != nil {
		return NodeHandle{}, err
	}
	return NodeHandle{Document: host.generation, Node: fragment}, nil
}

func (host *taskHost) InnerHTML(handle NodeHandle) (string, error) {
	host.page.mutex.RLock()
	defer host.page.mutex.RUnlock()
	if err := host.validateHandleLocked(handle); err != nil {
		return "", err
	}
	node, ok := host.page.document.Resolve(handle.Node)
	if !ok {
		return "", dom.ErrUnknownNode
	}
	if node.Type != dom.ElementNode {
		return "", fmt.Errorf("%w: innerHTML target is not an element", dom.ErrWrongNodeKind)
	}
	if node.TemplateContent != nil {
		node = node.TemplateContent
	}
	return htmlparser.SerializeChildren(node), nil
}

func (host *taskHost) SetInnerHTML(handle NodeHandle, source string) error {
	host.page.mutex.RLock()
	if err := host.validateHandleLocked(handle); err != nil {
		host.page.mutex.RUnlock()
		return err
	}
	node, ok := host.page.document.Resolve(handle.Node)
	if !ok || node.Type != dom.ElementNode {
		host.page.mutex.RUnlock()
		return fmt.Errorf("%w: innerHTML target is not an element", dom.ErrWrongNodeKind)
	}
	contextName := node.Data
	targetID := handle.Node
	if node.TemplateContent != nil {
		contentID, contentErr := host.page.document.TemplateContent(handle.Node)
		if contentErr != nil {
			host.page.mutex.RUnlock()
			return contentErr
		}
		targetID = contentID
	}
	host.page.mutex.RUnlock()
	fragment, err := htmlparser.ParseFragment(strings.NewReader(source), contextName)
	if err != nil {
		return err
	}
	return host.mutateNodes(handle, NodeHandle{}, NodeHandle{}, func() error {
		return host.page.document.ReplaceChildrenFromFragment(targetID, fragment)
	})
}

func (host *taskHost) InsertAdjacentHTML(handle NodeHandle, position, source string) error {
	position = strings.ToLower(strings.TrimSpace(position))
	switch position {
	case "beforebegin", "afterbegin", "beforeend", "afterend":
	default:
		return dom.NewException(dom.SyntaxError, dom.ErrInvalidTree, "invalid insertAdjacentHTML position %q", position)
	}
	host.page.mutex.RLock()
	if err := host.validateHandleLocked(handle); err != nil {
		host.page.mutex.RUnlock()
		return err
	}
	target, ok := host.page.document.Resolve(handle.Node)
	if !ok || target.Type != dom.ElementNode {
		host.page.mutex.RUnlock()
		return fmt.Errorf("%w: insertAdjacentHTML target is not an element", dom.ErrWrongNodeKind)
	}
	contextName := target.Data
	if (position == "beforebegin" || position == "afterend") && target.Parent != nil && target.Parent.Type == dom.ElementNode {
		contextName = target.Parent.Data
	}
	host.page.mutex.RUnlock()
	fragment, err := htmlparser.ParseFragment(strings.NewReader(source), contextName)
	if err != nil {
		return err
	}
	return host.mutateNodes(handle, NodeHandle{}, NodeHandle{}, func() error {
		target, found := host.page.document.Resolve(handle.Node)
		if !found {
			return dom.ErrUnknownNode
		}
		parent := target
		reference := dom.InvalidNodeID
		switch position {
		case "beforebegin":
			if target.Parent == nil {
				return fmt.Errorf("%w: beforebegin target has no parent", dom.ErrInvalidTree)
			}
			parent = target.Parent
			reference = handle.Node
		case "afterbegin":
			if len(target.Children) != 0 {
				var ok bool
				reference, ok = host.page.document.ID(target.Children[0])
				if !ok {
					return dom.ErrUnknownNode
				}
			}
		case "beforeend":
		case "afterend":
			if target.Parent == nil {
				return fmt.Errorf("%w: afterend target has no parent", dom.ErrInvalidTree)
			}
			parent = target.Parent
			index := -1
			for candidateIndex, candidate := range parent.Children {
				if candidate == target {
					index = candidateIndex
					break
				}
			}
			if index >= 0 && index+1 < len(parent.Children) {
				var ok bool
				reference, ok = host.page.document.ID(parent.Children[index+1])
				if !ok {
					return dom.ErrUnknownNode
				}
			}
		default:
			return fmt.Errorf("browser: invalid insertAdjacentHTML position %q", position)
		}
		parentID, found := host.page.document.ID(parent)
		if !found {
			return dom.ErrUnknownNode
		}
		return host.page.document.InsertFragment(parentID, reference, fragment)
	})
}

func (host *taskHost) MutateNodes(receiver NodeHandle, operation dom.MutationOperation, nodes []NodeHandle) error {
	return host.mutateNodeSlice(receiver, nodes, func() error {
		ids := make([]dom.NodeID, len(nodes))
		for index, node := range nodes {
			ids[index] = node.Node
		}
		return host.page.document.Mutate(receiver.Node, operation, ids)
	})
}

func (host *taskHost) FormValue(handle NodeHandle) (string, error) {
	host.page.mutex.RLock()
	defer host.page.mutex.RUnlock()
	if err := host.validateHandleLocked(handle); err != nil {
		return "", err
	}
	return host.page.document.FormValue(handle.Node)
}

func (host *taskHost) SetFormValue(handle NodeHandle, value string) error {
	return host.mutateNodes(handle, NodeHandle{}, NodeHandle{}, func() error {
		return host.page.document.SetFormValue(handle.Node, value)
	})
}

func (host *taskHost) FormSelection(handle NodeHandle) (int, int, string, error) {
	host.page.mutex.RLock()
	defer host.page.mutex.RUnlock()
	if err := host.validateHandleLocked(handle); err != nil {
		return 0, 0, "none", err
	}
	return host.page.document.FormSelection(handle.Node)
}

func (host *taskHost) SetFormSelection(handle NodeHandle, start, end int, direction string) error {
	return host.mutateNodes(handle, NodeHandle{}, NodeHandle{}, func() error {
		return host.page.document.SetFormSelection(handle.Node, start, end, direction)
	})
}

func (host *taskHost) ReplaceFormSelection(handle NodeHandle, data, inputType string) error {
	return host.mutateNodes(handle, NodeHandle{}, NodeHandle{}, func() error {
		return host.page.document.ReplaceFormSelection(handle.Node, data, inputType)
	})
}

func (host *taskHost) FormChecked(handle NodeHandle) (bool, error) {
	host.page.mutex.RLock()
	defer host.page.mutex.RUnlock()
	if err := host.validateHandleLocked(handle); err != nil {
		return false, err
	}
	return host.page.document.FormChecked(handle.Node)
}

func (host *taskHost) SetFormChecked(handle NodeHandle, checked bool) error {
	return host.mutateNodes(handle, NodeHandle{}, NodeHandle{}, func() error {
		return host.page.document.SetFormChecked(handle.Node, checked)
	})
}

func (host *taskHost) FormSelected(handle NodeHandle) (bool, error) {
	host.page.mutex.RLock()
	defer host.page.mutex.RUnlock()
	if err := host.validateHandleLocked(handle); err != nil {
		return false, err
	}
	return host.page.document.FormSelected(handle.Node)
}

func (host *taskHost) SetFormSelected(handle NodeHandle, selected bool) error {
	return host.mutateNodes(handle, NodeHandle{}, NodeHandle{}, func() error {
		return host.page.document.SetFormSelected(handle.Node, selected)
	})
}

func (host *taskHost) FormSelectedIndex(handle NodeHandle) (int, error) {
	host.page.mutex.RLock()
	defer host.page.mutex.RUnlock()
	if err := host.validateHandleLocked(handle); err != nil {
		return -1, err
	}
	return host.page.document.FormSelectedIndex(handle.Node)
}

func (host *taskHost) SetFormSelectedIndex(handle NodeHandle, selectedIndex int) error {
	return host.mutateNodes(handle, NodeHandle{}, NodeHandle{}, func() error {
		return host.page.document.SetFormSelectedIndex(handle.Node, selectedIndex)
	})
}

func (host *taskHost) FormControlNodes(handle NodeHandle, kind dom.FormCollectionKind) ([]NodeHandle, error) {
	host.page.mutex.RLock()
	defer host.page.mutex.RUnlock()
	if err := host.validateHandleLocked(handle); err != nil {
		return nil, err
	}
	ids, err := host.page.document.FormControlNodes(handle.Node, kind)
	if err != nil {
		return nil, err
	}
	result := make([]NodeHandle, len(ids))
	for index, id := range ids {
		result[index] = NodeHandle{Document: host.generation, Node: id}
	}
	return result, nil
}

func (host *taskHost) FormOwner(handle NodeHandle) (NodeHandle, bool, error) {
	host.page.mutex.RLock()
	defer host.page.mutex.RUnlock()
	if err := host.validateHandleLocked(handle); err != nil {
		return NodeHandle{}, false, err
	}
	id, found, err := host.page.document.FormOwner(handle.Node)
	if err != nil || !found {
		return NodeHandle{}, false, err
	}
	return NodeHandle{Document: host.generation, Node: id}, true, nil
}

func (host *taskHost) ResetForm(handle NodeHandle) error {
	return host.mutateNodes(handle, NodeHandle{}, NodeHandle{}, func() error {
		return host.page.document.ResetForm(handle.Node)
	})
}

func (host *taskHost) MutationSequence() (uint64, error) {
	host.page.mutex.RLock()
	defer host.page.mutex.RUnlock()
	if host.page.closed || host.page.documentGeneration != host.generation {
		return 0, ErrPageClosed
	}
	return host.page.document.MutationSequence(), nil
}

func (host *taskHost) MutationRecordsSince(sequence uint64) ([]dom.MutationRecord, uint64, error) {
	host.page.mutex.RLock()
	defer host.page.mutex.RUnlock()
	if host.page.closed || host.page.documentGeneration != host.generation {
		return nil, sequence, ErrPageClosed
	}
	return host.page.document.MutationRecordsSince(sequence)
}

func (host *taskHost) Focus(handle NodeHandle) error {
	host.page.mutex.Lock()
	defer host.page.mutex.Unlock()
	if err := host.validateHandleLocked(handle); err != nil {
		return err
	}
	snapshot, err := host.page.document.Snapshot(handle.Node)
	if err != nil {
		return err
	}
	if snapshot.Type != dom.ElementNode {
		return fmt.Errorf("%w: focus target is not an element", dom.ErrWrongNodeKind)
	}
	node, _ := host.page.document.Resolve(handle.Node)
	focusable := false
	switch node.Data {
	case "input", "select", "textarea", "button":
		focusable = true
	case "a":
		_, focusable, _ = host.page.document.GetAttribute(handle.Node, "href")
	default:
		_, focusable, _ = host.page.document.GetAttribute(handle.Node, "tabindex")
	}
	if snapshot.Connected && focusable {
		host.page.activeElement = handle.Node
	}
	return nil
}

func (host *taskHost) Blur(handle NodeHandle) error {
	host.page.mutex.Lock()
	defer host.page.mutex.Unlock()
	if err := host.validateHandleLocked(handle); err != nil {
		return err
	}
	if host.page.activeElement == handle.Node {
		host.page.activeElement = dom.InvalidNodeID
	}
	return nil
}

func (host *taskHost) ActiveElement() (NodeHandle, bool, error) {
	host.page.mutex.RLock()
	defer host.page.mutex.RUnlock()
	if host.page.closed {
		return NodeHandle{}, false, ErrPageClosed
	}
	if host.page.documentGeneration != host.generation {
		return NodeHandle{}, false, ErrStaleNodeHandle
	}
	active := host.page.activeElement
	if active != dom.InvalidNodeID {
		snapshot, err := host.page.document.Snapshot(active)
		if err == nil && snapshot.Connected {
			return NodeHandle{Document: host.generation, Node: active}, true, nil
		}
	}
	body, found, err := host.page.document.RelatedNode(host.page.document.RootID(), dom.DocumentBody)
	if err != nil || !found {
		return NodeHandle{}, false, err
	}
	return NodeHandle{Document: host.generation, Node: body}, true, nil
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

func (host *taskHost) ComputedStyleProperty(handle NodeHandle, pseudo, property string) (string, bool, error) {
	if pseudo != "" {
		_, err := host.page.ComputedStyle(handle)
		if err != nil {
			if errors.Is(err, ErrComputedStyleUnavailable) {
				return "", false, nil
			}
			return "", false, err
		}
		return "", false, nil
	}
	value, found, err := host.page.ComputedStyleProperty(handle, property)
	if errors.Is(err, ErrComputedStyleUnavailable) {
		return "", false, nil
	}
	return value, found, err
}

func (host *taskHost) ComputedStylePropertyNames(handle NodeHandle, pseudo string) ([]string, error) {
	computedStyle, err := host.page.ComputedStyle(handle)
	if err != nil {
		if errors.Is(err, ErrComputedStyleUnavailable) {
			return []string{}, nil
		}
		return nil, err
	}
	if pseudo != "" {
		return []string{}, nil
	}
	return computed.ComputedPropertyNames(computedStyle), nil
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

func (host *taskHost) mutateNodeSlice(receiver NodeHandle, nodes []NodeHandle, mutation func() error) error {
	host.page.mutex.Lock()
	defer host.page.mutex.Unlock()
	if err := host.validateHandleLocked(receiver); err != nil {
		return err
	}
	for _, handle := range nodes {
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

func (host *taskHost) NodeFacadeRef(handle NodeHandle) (memory.Ref, error) {
	return host.page.NodeFacadeRef(handle)
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
