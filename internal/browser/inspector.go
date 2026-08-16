package browser

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/JediWattson/gossamer/internal/dom"
)

// InspectorNodeSnapshot is a copied description of one DOM node. Inspector
// clients never retain raw Node pointers across the Page ownership boundary.
type InspectorNodeSnapshot struct {
	Handle     NodeHandle
	Name       string
	Namespace  string
	Attributes []dom.Attribute
}

// InspectorDOMLines returns a bounded, deterministic text projection of the
// active document. The dump is completed while the document read view is live,
// then only owned strings cross into browser chrome.
func (page *Page) InspectorDOMLines(limit int) ([]string, error) {
	if page == nil {
		return nil, fmt.Errorf("browser: nil page")
	}
	if limit <= 0 {
		return nil, nil
	}
	page.mutex.RLock()
	defer page.mutex.RUnlock()
	if page.closed {
		return nil, ErrPageClosed
	}
	var dump bytes.Buffer
	if err := page.document.WithReadView(func(view dom.ReadView) error {
		access, err := view.Acquire()
		if err != nil {
			return err
		}
		defer access.Close()
		return dom.Dump(&dump, access.Root())
	}); err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSuffix(dump.String(), "\n"), "\n")
	if len(lines) > limit {
		lines = append(append([]string(nil), lines[:limit-1]...), "...")
	} else {
		lines = append([]string(nil), lines...)
	}
	return lines, nil
}

// DocumentElementHandle returns stable identity for the active document's
// root element without exposing the backing DOM node.
func (page *Page) DocumentElementHandle() (NodeHandle, bool) {
	if page == nil {
		return NodeHandle{}, false
	}
	page.mutex.RLock()
	defer page.mutex.RUnlock()
	if page.closed {
		return NodeHandle{}, false
	}
	node, found, err := page.document.RelatedNode(page.document.RootID(), dom.DocumentElement)
	if err != nil || !found {
		return NodeHandle{}, false
	}
	return NodeHandle{Document: page.documentGeneration, Node: node}, true
}

// InspectorNode returns copied metadata for one generation-safe node handle.
func (page *Page) InspectorNode(handle NodeHandle) (InspectorNodeSnapshot, error) {
	if page == nil {
		return InspectorNodeSnapshot{}, fmt.Errorf("browser: nil page")
	}
	page.mutex.RLock()
	defer page.mutex.RUnlock()
	if page.closed {
		return InspectorNodeSnapshot{}, ErrPageClosed
	}
	if handle.Document == 0 || handle.Node == dom.InvalidNodeID || handle.Document != page.documentGeneration {
		return InspectorNodeSnapshot{}, ErrStaleNodeHandle
	}
	node, found := page.document.Resolve(handle.Node)
	if !found {
		return InspectorNodeSnapshot{}, fmt.Errorf("%w: %d", dom.ErrUnknownNode, handle.Node)
	}
	return InspectorNodeSnapshot{
		Handle:     handle,
		Name:       node.Data,
		Namespace:  node.NamespaceURI,
		Attributes: append([]dom.Attribute(nil), node.Attributes...),
	}, nil
}
