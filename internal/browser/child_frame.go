package browser

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strings"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
)

const blankChildFrameSource = "about:blank"

type childFrameDescriptor struct {
	handle NodeHandle
	source string
	url    string
}

// AttachChildFrame creates an independently sequenced child Page for one
// current-document iframe. The parent owns the child lifetime; navigation or
// teardown of the parent closes the child Realm in bulk.
func (page *Page) AttachChildFrame(iframe NodeHandle, root *dom.Node, location *url.URL) (*Page, error) {
	if page == nil || page.browser == nil {
		return nil, fmt.Errorf("browser: nil page")
	}
	page.mutex.RLock()
	err := page.validateFrameOwnerLocked(iframe)
	page.mutex.RUnlock()
	if err != nil {
		return nil, err
	}
	child, err := page.browser.NewPage(root, location)
	if err != nil {
		return nil, err
	}

	page.mutex.Lock()
	if err := page.validateFrameOwnerLocked(iframe); err != nil {
		page.mutex.Unlock()
		_ = child.Close()
		return nil, err
	}
	if page.children[iframe.Node] != nil {
		page.mutex.Unlock()
		_ = child.Close()
		return nil, fmt.Errorf("browser: iframe %d already owns a child realm", iframe.Node)
	}
	child.mutex.Lock()
	if child.closed {
		child.mutex.Unlock()
		page.mutex.Unlock()
		return nil, ErrPageClosed
	}
	child.parent = page
	child.frameOwner = iframe
	child.mutex.Unlock()
	page.children[iframe.Node] = child
	page.mutex.Unlock()
	return child, nil
}

func (page *Page) ChildFrame(iframe NodeHandle) (*Page, bool) {
	if page == nil {
		return nil, false
	}
	page.mutex.RLock()
	if page.validateFrameOwnerLocked(iframe) != nil {
		page.mutex.RUnlock()
		return nil, false
	}
	child := page.children[iframe.Node]
	page.mutex.RUnlock()
	return child, child != nil
}

func (page *Page) FrameOwner() (NodeHandle, bool) {
	if page == nil {
		return NodeHandle{}, false
	}
	page.mutex.RLock()
	owner := page.frameOwner
	parent := page.parent
	page.mutex.RUnlock()
	return owner, parent != nil && owner.Node != dom.InvalidNodeID
}

func (page *Page) ContentDocument(iframe NodeHandle) (NodeHandle, bool) {
	child, ok := page.ChildFrame(iframe)
	if !ok {
		return NodeHandle{}, false
	}
	page.mutex.RLock()
	parentLocation := cloneURL(page.location)
	page.mutex.RUnlock()
	child.mutex.RLock()
	defer child.mutex.RUnlock()
	if child.closed || !sameOrigin(parentLocation, child.location) {
		return NodeHandle{}, false
	}
	return NodeHandle{Document: child.documentGeneration, Node: child.document.RootID()}, true
}

func (page *Page) validateFrameOwnerLocked(iframe NodeHandle) error {
	if page.closed {
		return ErrPageClosed
	}
	if iframe.Document != page.documentGeneration || iframe.Node == dom.InvalidNodeID {
		return ErrStaleNodeHandle
	}
	node, ok := page.document.Resolve(iframe.Node)
	if !ok {
		return dom.ErrUnknownNode
	}
	if node.Type != dom.ElementNode || node.NamespaceURI != dom.HTMLNamespace || node.Data != "iframe" {
		return fmt.Errorf("%w: child frame owner is not an HTML iframe", dom.ErrWrongNodeKind)
	}
	snapshot, err := page.document.Snapshot(iframe.Node)
	if err != nil {
		return err
	}
	if !snapshot.Connected {
		return fmt.Errorf("%w: child frame owner is disconnected", dom.ErrInvalidTree)
	}
	return nil
}

func (page *Page) syncChildFrameViewports() error {
	type update struct {
		child    *Page
		viewport render.Viewport
	}
	page.mutex.RLock()
	frame := page.frame
	updates := make([]update, 0, len(page.childFrameSources))
	if frame != nil && frame.Layout != nil {
		for node, child := range page.children {
			if _, automatic := page.childFrameSources[node]; !automatic {
				continue
			}
			geometry, found := frame.Layout.GeometryID(node)
			if !found {
				continue
			}
			width := int(math.Round(geometry.ContentBounds.Width))
			height := int(math.Round(geometry.ContentBounds.Height))
			if width <= 0 || height <= 0 {
				continue
			}
			updates = append(updates, update{child: child, viewport: render.Viewport{Width: width, Height: height}})
		}
	}
	page.mutex.RUnlock()
	for _, candidate := range updates {
		candidate.child.mutex.RLock()
		current := candidate.child.viewport
		closed := candidate.child.closed
		candidate.child.mutex.RUnlock()
		if closed || current == candidate.viewport {
			continue
		}
		if err := candidate.child.SetViewport(candidate.viewport); err != nil {
			if errors.Is(err, ErrPageClosed) {
				continue
			}
			return err
		}
		if _, err := candidate.child.QueueRender(); err != nil && !errors.Is(err, browserruntime.ErrRealmClosed) {
			return err
		}
	}
	return nil
}

func (page *Page) notifyParentFrameChanged() {
	if page == nil {
		return
	}
	page.mutex.RLock()
	parent := page.parent
	owner := page.frameOwner
	page.mutex.RUnlock()
	if parent == nil {
		return
	}
	parent.mutex.Lock()
	current := !parent.closed && owner.Document == parent.documentGeneration && parent.children[owner.Node] == page
	if current {
		parent.dirty = true
	}
	parent.mutex.Unlock()
	if current {
		_, _ = parent.QueueRender()
	}
}

func (page *Page) takeChildFramesLocked() []*Page {
	children := make([]*Page, 0, len(page.children))
	for node, child := range page.children {
		delete(page.children, node)
		delete(page.childFrameSources, node)
		if cancel := page.childFrameCancels[node]; cancel != nil {
			cancel()
		}
		delete(page.childFrameCancels, node)
		children = append(children, child)
	}
	return children
}

func (page *Page) removeChildFrame(child *Page) {
	if page == nil || child == nil {
		return
	}
	page.mutex.Lock()
	for node, candidate := range page.children {
		if candidate == child {
			delete(page.children, node)
			delete(page.childFrameSources, node)
			if cancel := page.childFrameCancels[node]; cancel != nil {
				cancel()
			}
			delete(page.childFrameCancels, node)
			break
		}
	}
	page.mutex.Unlock()
}

// syncChildFrames reconciles connected iframe elements with browser-owned
// child Pages. Discovery deliberately crosses both open and closed shadow
// roots: encapsulation affects DOM exposure, not the lifetime of a connected
// nested browsing context.
func (page *Page) syncChildFrames() error {
	if page == nil {
		return fmt.Errorf("browser: nil page")
	}
	descriptors, generation, err := page.connectedChildFrames()
	if err != nil {
		return err
	}
	connected := make(map[dom.NodeID]childFrameDescriptor, len(descriptors))
	for _, descriptor := range descriptors {
		connected[descriptor.handle.Node] = descriptor
	}

	var retired []*Page
	page.mutex.Lock()
	if page.closed || page.documentGeneration != generation {
		page.mutex.Unlock()
		return nil
	}
	for node, child := range page.children {
		descriptor, remains := connected[node]
		automaticSource, automatic := page.childFrameSources[node]
		if remains && (!automatic || automaticSource == descriptor.source) {
			continue
		}
		delete(page.children, node)
		delete(page.childFrameSources, node)
		if cancel := page.childFrameCancels[node]; cancel != nil {
			cancel()
		}
		delete(page.childFrameCancels, node)
		retired = append(retired, child)
	}
	page.mutex.Unlock()
	for _, child := range retired {
		_ = child.Close()
	}

	for _, descriptor := range descriptors {
		page.mutex.RLock()
		child := page.children[descriptor.handle.Node]
		page.mutex.RUnlock()
		if child != nil {
			continue
		}
		if err := page.attachAutomaticChildFrame(descriptor); err != nil {
			page.queueChildFrameEvent(descriptor.handle, nil, InputError)
		}
	}
	return page.syncChildFrameViewports()
}

func (page *Page) connectedChildFrames() ([]childFrameDescriptor, DocumentGeneration, error) {
	page.mutex.RLock()
	if page.closed {
		page.mutex.RUnlock()
		return nil, 0, ErrPageClosed
	}
	document := page.document
	generation := page.documentGeneration
	base := cloneURL(page.location)
	page.mutex.RUnlock()

	descriptors := make([]childFrameDescriptor, 0)
	err := document.WithReadView(func(view dom.ReadView) error {
		access, err := view.Acquire()
		if err != nil {
			return err
		}
		defer access.Close()
		var visit func(*dom.Node)
		visit = func(node *dom.Node) {
			if node == nil {
				return
			}
			if node.Type == dom.ElementNode && node.NamespaceURI == dom.HTMLNamespace && strings.EqualFold(node.Data, "iframe") {
				if id, ok := access.ID(node); ok {
					rawSource, _ := nodeAttribute(node, "src")
					resolved, source := resolveChildFrameSource(base, rawSource)
					descriptors = append(descriptors, childFrameDescriptor{
						handle: NodeHandle{Document: generation, Node: id},
						source: source,
						url:    resolved,
					})
				}
			}
			for _, child := range node.Children {
				visit(child)
			}
			if node.ShadowRoot != nil {
				visit(node.ShadowRoot)
			}
		}
		visit(access.Root())
		return nil
	})
	return descriptors, generation, err
}

func resolveChildFrameSource(base *url.URL, raw string) (resolved, identity string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", blankChildFrameSource
	}
	reference, err := url.Parse(raw)
	if err != nil {
		return "", "invalid:" + raw
	}
	if base != nil {
		reference = base.ResolveReference(reference)
	}
	if reference.Scheme != "http" && reference.Scheme != "https" {
		return "", "unsupported:" + reference.String()
	}
	return reference.String(), reference.String()
}

func (page *Page) attachAutomaticChildFrame(descriptor childFrameDescriptor) error {
	if descriptor.url == "" && descriptor.source != blankChildFrameSource {
		return fmt.Errorf("browser: unsupported iframe source")
	}

	page.mutex.RLock()
	parentContext := page.documentContext
	loader := page.navigationLoader
	location := cloneURL(page.location)
	viewport := page.viewport
	scale := page.deviceScaleFactor
	page.mutex.RUnlock()
	if parentContext == nil {
		parentContext = context.Background()
	}
	frameContext, cancel := context.WithCancel(parentContext)
	childLocation := (*url.URL)(nil)
	if descriptor.source == blankChildFrameSource {
		childLocation = location
	}
	child, err := page.AttachChildFrame(descriptor.handle, dom.NewDocument(), childLocation)
	if err != nil {
		cancel()
		return err
	}
	if viewport.Width > 0 && viewport.Height > 0 {
		_ = child.SetViewport(viewport)
	}
	if scale > 0 {
		_ = child.SetDeviceScaleFactor(scale)
	}
	child.SetNavigationLoader(loader)

	page.mutex.Lock()
	if page.closed || page.documentGeneration != descriptor.handle.Document || page.children[descriptor.handle.Node] != child {
		page.mutex.Unlock()
		cancel()
		_ = child.Close()
		return nil
	}
	page.childFrameSources[descriptor.handle.Node] = descriptor.source
	page.childFrameCancels[descriptor.handle.Node] = cancel
	page.mutex.Unlock()

	if descriptor.source == blankChildFrameSource {
		if _, err := child.QueueRender(); err != nil {
			cancel()
			_ = child.Close()
			return err
		}
		go page.runAutomaticChildFrame(frameContext, descriptor.handle, child, 0)
		return nil
	}
	navigation, err := child.Navigate(frameContext, descriptor.url, loader)
	if err != nil {
		cancel()
		_ = child.Close()
		return err
	}
	go page.runAutomaticChildFrame(frameContext, descriptor.handle, child, navigation)
	return nil
}

func (page *Page) runAutomaticChildFrame(ctx context.Context, owner NodeHandle, child *Page, navigation NavigationID) {
	var loadErr error
	if navigation == 0 {
		loadErr = child.Realm.RunOne(ctx)
	} else {
		loadErr = child.WaitNavigation(ctx, navigation)
	}
	if ctx.Err() != nil || errors.Is(loadErr, ErrPageClosed) || errors.Is(loadErr, browserruntime.ErrRealmClosed) {
		return
	}
	eventType := InputLoad
	if loadErr != nil {
		eventType = InputError
	}
	page.queueChildFrameEvent(owner, child, eventType)
	if loadErr == nil {
		_, _ = page.QueueRender()
	}
	_ = child.Realm.Run(ctx)
}

func (page *Page) queueChildFrameEvent(owner NodeHandle, child *Page, eventType InputEventType) {
	page.mutex.RLock()
	connected := !page.closed && owner.Document == page.documentGeneration
	if child != nil {
		connected = connected && page.children[owner.Node] == child
	}
	page.mutex.RUnlock()
	if !connected {
		return
	}
	_, _ = page.QueueInputEvent(InputEvent{Type: eventType, Target: owner})
}

func sameOrigin(left, right *url.URL) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	if !strings.EqualFold(left.Scheme, right.Scheme) {
		return false
	}
	return strings.EqualFold(left.Hostname(), right.Hostname()) && effectivePort(left) == effectivePort(right)
}

func effectivePort(location *url.URL) string {
	if location == nil {
		return ""
	}
	if port := location.Port(); port != "" {
		return port
	}
	switch strings.ToLower(location.Scheme) {
	case "http":
		return "80"
	case "https":
		return "443"
	default:
		return ""
	}
}
