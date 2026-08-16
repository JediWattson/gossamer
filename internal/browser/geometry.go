package browser

import (
	"fmt"
	"math"

	"github.com/JediWattson/gossamer/internal/dom"
)

// ElementGeometry synchronously flushes the current immutable layout snapshot
// and projects one element into viewport coordinates. It never publishes a
// Frame or clears the Page's render dirtiness.
func (page *Page) ElementGeometry(handle NodeHandle) (DOMElementGeometry, error) {
	if page == nil {
		return DOMElementGeometry{}, fmt.Errorf("browser: nil page")
	}
	page.mutex.Lock()
	defer page.mutex.Unlock()
	return page.elementGeometryLocked(handle)
}

func (page *Page) elementGeometryLocked(handle NodeHandle) (DOMElementGeometry, error) {
	if page.closed {
		return DOMElementGeometry{}, ErrPageClosed
	}
	if handle.Document == 0 || handle.Node == dom.InvalidNodeID || handle.Document != page.documentGeneration {
		return DOMElementGeometry{}, ErrStaleNodeHandle
	}

	resources := page.resources.rendererResources(page.document)
	rootID, _, _ := page.document.RelatedNode(page.document.RootID(), dom.DocumentElement)
	var result DOMElementGeometry
	err := page.document.WithReadView(func(view dom.ReadView) error {
		node, ok := view.Resolve(handle.Node)
		if !ok {
			return fmt.Errorf("%w: %d", dom.ErrUnknownNode, handle.Node)
		}
		if node.Type != dom.ElementNode {
			return fmt.Errorf("%w: node %d is %d, want element", dom.ErrWrongNodeKind, handle.Node, node.Type)
		}
		styles, err := page.styleSnapshotForViewLocked(view, resources)
		if err != nil {
			return err
		}
		layout, err := page.layoutSnapshotForViewLocked(view, resources, styles)
		if err != nil {
			return err
		}
		geometry, found := layout.GeometryID(handle.Node)
		if !found {
			// Elements without a principal box expose zero CSSOM View geometry.
			return nil
		}
		clientWidth := geometry.ClientBounds.Width
		clientHeight := geometry.ClientBounds.Height
		scrollWidth := geometry.ScrollWidth
		scrollHeight := geometry.ScrollHeight
		if handle.Node == rootID {
			clientWidth = float64(page.viewport.Width)
			clientHeight = float64(page.viewport.Height)
			scrollWidth = math.Max(clientWidth, scrollWidth)
			scrollHeight = math.Max(clientHeight, scrollHeight)
			result.ScrollLeft = page.scrollX
			result.ScrollTop = page.scrollY
		}
		result.Rect = DOMRect{
			X:      geometry.Bounds.X - page.scrollX,
			Y:      geometry.Bounds.Y - page.scrollY,
			Width:  geometry.Bounds.Width,
			Height: geometry.Bounds.Height,
		}
		result.ClientWidth = clientWidth
		result.ClientHeight = clientHeight
		result.OffsetWidth = geometry.Bounds.Width
		result.OffsetHeight = geometry.Bounds.Height
		result.ScrollWidth = scrollWidth
		result.ScrollHeight = scrollHeight
		return nil
	})
	return result, err
}

// ViewportGeometry returns the live viewport and root scrolling metrics.
func (page *Page) ViewportGeometry() (DOMViewportGeometry, error) {
	if page == nil {
		return DOMViewportGeometry{}, fmt.Errorf("browser: nil page")
	}
	page.mutex.Lock()
	defer page.mutex.Unlock()
	return page.viewportGeometryLocked()
}

func (page *Page) viewportGeometryLocked() (DOMViewportGeometry, error) {
	if page.closed {
		return DOMViewportGeometry{}, ErrPageClosed
	}
	result := DOMViewportGeometry{
		InnerWidth:   float64(page.viewport.Width),
		InnerHeight:  float64(page.viewport.Height),
		ScrollX:      page.scrollX,
		ScrollY:      page.scrollY,
		ScrollWidth:  float64(page.viewport.Width),
		ScrollHeight: float64(page.viewport.Height),
	}
	rootID, found, err := page.document.RelatedNode(page.document.RootID(), dom.DocumentElement)
	if err != nil || !found {
		return result, err
	}
	geometry, err := page.elementGeometryLocked(NodeHandle{Document: page.documentGeneration, Node: rootID})
	if err != nil {
		return DOMViewportGeometry{}, err
	}
	result.ScrollWidth = math.Max(result.InnerWidth, geometry.ScrollWidth)
	result.ScrollHeight = math.Max(result.InnerHeight, geometry.ScrollHeight)
	return result, nil
}

// ScrollViewport updates the Page-owned root offset and reports whether it
// changed. Scrolling invalidates paint but deliberately reuses style/layout.
func (page *Page) ScrollViewport(x, y float64) (bool, error) {
	if page == nil {
		return false, fmt.Errorf("browser: nil page")
	}
	page.mutex.Lock()
	defer page.mutex.Unlock()
	return page.scrollViewportLocked(x, y)
}

func (page *Page) scrollViewportLocked(x, y float64) (bool, error) {
	if page.closed {
		return false, ErrPageClosed
	}
	if math.IsNaN(x) {
		x = 0
	}
	if math.IsNaN(y) {
		y = 0
	}
	if math.IsInf(x, 0) || math.IsInf(y, 0) {
		return false, fmt.Errorf("browser: non-finite scroll offset")
	}
	geometry, err := page.viewportGeometryLocked()
	if err != nil {
		return false, err
	}
	x = math.Max(0, math.Min(x, math.Max(0, geometry.ScrollWidth-geometry.InnerWidth)))
	y = math.Max(0, math.Min(y, math.Max(0, geometry.ScrollHeight-geometry.InnerHeight)))
	if x == page.scrollX && y == page.scrollY {
		return false, nil
	}
	page.scrollX = x
	page.scrollY = y
	page.dirty = true
	return true, nil
}

// ScrollElement maps the document scrolling element onto the viewport. Other
// elements currently have no overflow formatting context and therefore remain
// non-scrollable with zero offsets.
func (page *Page) ScrollElement(handle NodeHandle, x, y float64) (bool, error) {
	if page == nil {
		return false, fmt.Errorf("browser: nil page")
	}
	page.mutex.Lock()
	defer page.mutex.Unlock()
	if page.closed {
		return false, ErrPageClosed
	}
	if handle.Document != page.documentGeneration || handle.Node == dom.InvalidNodeID {
		return false, ErrStaleNodeHandle
	}
	if snapshot, err := page.document.Snapshot(handle.Node); err != nil {
		return false, err
	} else if snapshot.Type != dom.ElementNode {
		return false, dom.ErrWrongNodeKind
	}
	rootID, found, err := page.document.RelatedNode(page.document.RootID(), dom.DocumentElement)
	if err != nil || !found || handle.Node != rootID {
		return false, err
	}
	return page.scrollViewportLocked(x, y)
}

// ScrollIntoView aligns the element's border-box start edge with the viewport
// while clamping to the root scrolling range.
func (page *Page) ScrollIntoView(handle NodeHandle) (bool, error) {
	if page == nil {
		return false, fmt.Errorf("browser: nil page")
	}
	page.mutex.Lock()
	defer page.mutex.Unlock()
	geometry, err := page.elementGeometryLocked(handle)
	if err != nil {
		return false, err
	}
	documentX := geometry.Rect.X + page.scrollX
	documentY := geometry.Rect.Y + page.scrollY
	return page.scrollViewportLocked(documentX, documentY)
}
