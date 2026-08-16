package browser

import (
	"fmt"
	"net/url"

	"github.com/JediWattson/gossamer/internal/dom"
)

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
	child.mutex.RLock()
	defer child.mutex.RUnlock()
	if child.closed {
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
	return nil
}

func (page *Page) takeChildFramesLocked() []*Page {
	children := make([]*Page, 0, len(page.children))
	for node, child := range page.children {
		delete(page.children, node)
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
			break
		}
	}
	page.mutex.Unlock()
}
