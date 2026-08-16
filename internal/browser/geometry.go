package browser

import (
	"fmt"
	"math"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
	computed "github.com/JediWattson/gossamer/internal/style"
)

type scrollOffset struct {
	x float64
	y float64
}

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
		visual := page.visualTransformForNodeLocked(node, layout, styles)
		result.Rect = DOMRect{
			X:      geometry.Bounds.X - visual.OffsetX,
			Y:      geometry.Bounds.Y - visual.OffsetY,
			Width:  geometry.Bounds.Width,
			Height: geometry.Bounds.Height,
		}
		result.ClientWidth = clientWidth
		result.ClientHeight = clientHeight
		result.OffsetWidth = geometry.Bounds.Width
		result.OffsetHeight = geometry.Bounds.Height
		result.ScrollWidth = scrollWidth
		result.ScrollHeight = scrollHeight
		if handle.Node != rootID {
			offset := page.elementScroll[handle.Node]
			result.ScrollLeft = offset.x
			result.ScrollTop = offset.y
		}
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

// ScrollElement updates Page-owned scroll state for the root or an element
// whose computed overflow establishes a programmatic scroll container.
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
	return page.scrollElementLocked(handle, x, y)
}

func (page *Page) scrollElementLocked(handle NodeHandle, x, y float64) (bool, error) {
	if snapshot, err := page.document.Snapshot(handle.Node); err != nil {
		return false, err
	} else if snapshot.Type != dom.ElementNode {
		return false, dom.ErrWrongNodeKind
	}
	rootID, found, err := page.document.RelatedNode(page.document.RootID(), dom.DocumentElement)
	if err != nil || !found {
		return false, err
	}
	if handle.Node == rootID {
		return page.scrollViewportLocked(x, y)
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
	geometry, err := page.elementGeometryLocked(handle)
	if err != nil {
		return false, err
	}
	style, ok := page.computedStyle.snapshot.LookupID(handle.Node)
	if !ok {
		return false, nil
	}
	if !overflowScrollable(style.OverflowX()) {
		x = 0
	}
	if !overflowScrollable(style.OverflowY()) {
		y = 0
	}
	x = math.Max(0, math.Min(x, math.Max(0, geometry.ScrollWidth-geometry.ClientWidth)))
	y = math.Max(0, math.Min(y, math.Max(0, geometry.ScrollHeight-geometry.ClientHeight)))
	current := page.elementScroll[handle.Node]
	if current.x == x && current.y == y {
		return false, nil
	}
	if page.elementScroll == nil {
		page.elementScroll = make(map[dom.NodeID]scrollOffset)
	}
	if x == 0 && y == 0 {
		delete(page.elementScroll, handle.Node)
	} else {
		page.elementScroll[handle.Node] = scrollOffset{x: x, y: y}
	}
	page.dirty = true
	return true, nil
}

// ScrollIntoView aligns the element through each scrollable ancestor and then
// the root viewport, clamping every Page-owned offset to its scroll range.
func (page *Page) ScrollIntoView(handle NodeHandle) (bool, error) {
	if page == nil {
		return false, fmt.Errorf("browser: nil page")
	}
	page.mutex.Lock()
	defer page.mutex.Unlock()
	if page.closed || handle.Document != page.documentGeneration {
		return false, ErrStaleNodeHandle
	}
	geometry, err := page.elementGeometryLocked(handle)
	if err != nil {
		return false, err
	}
	changed := false
	ancestor, found, err := page.document.RelatedNode(handle.Node, dom.ParentElement)
	for err == nil && found {
		style, ok := page.computedStyle.snapshot.LookupID(ancestor)
		if ok && (overflowScrollable(style.OverflowX()) || overflowScrollable(style.OverflowY())) {
			container, geometryErr := page.elementGeometryLocked(NodeHandle{Document: handle.Document, Node: ancestor})
			if geometryErr != nil {
				return changed, geometryErr
			}
			offset := page.elementScroll[ancestor]
			x := offset.x + geometry.Rect.X - container.Rect.X
			y := offset.y + geometry.Rect.Y - container.Rect.Y
			ancestorChanged, scrollErr := page.scrollElementLocked(NodeHandle{Document: handle.Document, Node: ancestor}, x, y)
			if scrollErr != nil {
				return changed, scrollErr
			}
			changed = changed || ancestorChanged
			geometry, err = page.elementGeometryLocked(handle)
			if err != nil {
				return changed, err
			}
		}
		ancestor, found, err = page.document.RelatedNode(ancestor, dom.ParentElement)
	}
	if err != nil {
		return changed, err
	}
	rootChanged, err := page.scrollViewportLocked(geometry.Rect.X+page.scrollX, geometry.Rect.Y+page.scrollY)
	return changed || rootChanged, err
}

func overflowScrollable(mode computed.OverflowMode) bool {
	return mode == computed.OverflowHidden || mode == computed.OverflowScroll || mode == computed.OverflowAuto
}

func overflowClips(mode computed.OverflowMode) bool {
	return mode != computed.OverflowVisible
}

func (page *Page) visualTransformsLocked(frame *render.Frame) map[*dom.Node]render.VisualTransform {
	transforms := make(map[*dom.Node]render.VisualTransform)
	if frame == nil || frame.Layout == nil || frame.ComputedStyles == nil {
		return transforms
	}
	indexNode := func(node *dom.Node) {
		if node != nil {
			if _, exists := transforms[node]; !exists {
				transforms[node] = page.visualTransformForNodeLocked(node, frame.Layout, frame.ComputedStyles)
			}
		}
	}
	var indexBox func(*render.Box)
	indexBox = func(box *render.Box) {
		if box == nil {
			return
		}
		indexNode(box.Node)
		for _, fragment := range box.Fragments {
			if fragment.Kind == render.ImageFragmentKind {
				indexNode(fragment.Image.Node)
			} else {
				indexNode(fragment.Text.Node)
			}
		}
		for _, child := range box.Children {
			indexBox(child)
		}
	}
	indexBox(frame.Root)
	return transforms
}

func (page *Page) visualTransformForNodeLocked(node *dom.Node, layout *render.LayoutSnapshot, styles *computed.Snapshot) render.VisualTransform {
	transform := render.VisualTransform{OffsetX: page.scrollX, OffsetY: page.scrollY}
	if node == nil || layout == nil || styles == nil {
		return transform
	}
	ancestors := make([]*dom.Node, 0, 8)
	for ancestor := node.Parent; ancestor != nil; ancestor = ancestor.Parent {
		ancestors = append(ancestors, ancestor)
	}
	for left, right := 0, len(ancestors)-1; left < right; left, right = left+1, right-1 {
		ancestors[left], ancestors[right] = ancestors[right], ancestors[left]
	}
	for _, ancestor := range ancestors {
		id, ok := page.document.ID(ancestor)
		if !ok {
			continue
		}
		style, ok := styles.LookupID(id)
		if !ok || (!overflowClips(style.OverflowX()) && !overflowClips(style.OverflowY())) {
			continue
		}
		geometry, ok := layout.GeometryID(id)
		if !ok {
			continue
		}
		clip := geometry.ClientBounds
		clip.X -= transform.OffsetX
		clip.Y -= transform.OffsetY
		if transform.HasClip {
			clip = intersectRenderRect(transform.Clip, clip)
		}
		transform.HasClip = true
		transform.Clip = clip
		offset := page.elementScroll[id]
		if overflowScrollable(style.OverflowX()) {
			transform.OffsetX += offset.x
		}
		if overflowScrollable(style.OverflowY()) {
			transform.OffsetY += offset.y
		}
	}
	return transform
}

func intersectRenderRect(left, right render.Rect) render.Rect {
	x := math.Max(left.X, right.X)
	y := math.Max(left.Y, right.Y)
	endX := math.Min(left.X+left.Width, right.X+right.Width)
	endY := math.Min(left.Y+left.Height, right.Y+right.Height)
	if endX <= x || endY <= y {
		return render.Rect{X: x, Y: y}
	}
	return render.Rect{X: x, Y: y, Width: endX - x, Height: endY - y}
}
