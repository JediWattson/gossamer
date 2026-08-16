package render

import (
	"fmt"

	"github.com/JediWattson/gossamer/internal/dom"
	computed "github.com/JediWattson/gossamer/internal/style"
)

// LayoutGeometry is the used box geometry retained for CSSOM View reads.
// Bounds is the border box, ClientBounds is the padding box, and
// ContentBounds is the content box used by resolved width and height
// serialization. ScrollWidth and ScrollHeight include visible descendant
// overflow in the current formatting-context slice.
type LayoutGeometry struct {
	Bounds        Rect
	ClientBounds  Rect
	ContentBounds Rect
	ScrollWidth   float64
	ScrollHeight  float64
	// PercentHeightResolved reports that a percentage-dependent specified
	// height had a definite containing-block base. When false, CSSOM retains
	// the computed percentage rather than exposing content-derived geometry.
	PercentHeightResolved bool
}

// LayoutSnapshot is an immutable layout result that can be queried before it
// is painted. Stable-document snapshots expose geometry by NodeID and retain
// the exact computed-style snapshot used to produce the layout.
type LayoutSnapshot struct {
	viewport       Viewport
	root           *Box
	styles         map[*dom.Node]computedStyle
	computedStyles *computed.Snapshot
	rootNode       *dom.Node
	rootID         dom.NodeID
	document       dom.DocumentIdentity
	version        uint64
	byNode         map[*dom.Node]LayoutGeometry
	byID           map[dom.NodeID]LayoutGeometry
	byPseudoNode   map[pointerPseudoGeometryKey]LayoutGeometry
	byPseudoID     map[stablePseudoGeometryKey]LayoutGeometry
}

type pointerPseudoGeometryKey struct {
	node   *dom.Node
	pseudo computed.PseudoElement
}

type stablePseudoGeometryKey struct {
	id     dom.NodeID
	pseudo computed.PseudoElement
}

// Viewport returns the CSS-pixel viewport used to compute the snapshot.
func (snapshot *LayoutSnapshot) Viewport() Viewport {
	if snapshot == nil {
		return Viewport{}
	}
	return snapshot.viewport
}

// ComputedStyles returns the exact immutable style snapshot used by layout.
func (snapshot *LayoutSnapshot) ComputedStyles() *computed.Snapshot {
	if snapshot == nil {
		return nil
	}
	return snapshot.computedStyles
}

// DocumentIdentity identifies the stable Document that owns an ID-indexed
// snapshot. Pointer-based snapshots return the zero identity.
func (snapshot *LayoutSnapshot) DocumentIdentity() dom.DocumentIdentity {
	if snapshot == nil {
		return dom.DocumentIdentity{}
	}
	return snapshot.document
}

// Version returns the coherent DOM mutation version used by an ID-indexed
// snapshot. Pointer-based snapshots return zero.
func (snapshot *LayoutSnapshot) Version() uint64 {
	if snapshot == nil {
		return 0
	}
	return snapshot.version
}

// Geometry returns used geometry for a node in a pointer-based snapshot.
func (snapshot *LayoutSnapshot) Geometry(node *dom.Node) (LayoutGeometry, bool) {
	if snapshot == nil || node == nil || snapshot.byNode == nil {
		return LayoutGeometry{}, false
	}
	geometry, ok := snapshot.byNode[node]
	return geometry, ok
}

// GeometryID returns used geometry for a node in a stable-ID snapshot.
func (snapshot *LayoutSnapshot) GeometryID(id dom.NodeID) (LayoutGeometry, bool) {
	if snapshot == nil || id == dom.InvalidNodeID || snapshot.byID == nil {
		return LayoutGeometry{}, false
	}
	geometry, ok := snapshot.byID[id]
	return geometry, ok
}

// PseudoGeometry returns used geometry for a generated pseudo-element box in
// a pointer snapshot. Inline generated content has no principal box until the
// retained inline-box formatter lands.
func (snapshot *LayoutSnapshot) PseudoGeometry(node *dom.Node, pseudo computed.PseudoElement) (LayoutGeometry, bool) {
	if snapshot == nil || node == nil || pseudo == computed.PseudoElementNone {
		return LayoutGeometry{}, false
	}
	geometry, ok := snapshot.byPseudoNode[pointerPseudoGeometryKey{node: node, pseudo: pseudo}]
	return geometry, ok
}

// PseudoGeometryID returns used geometry for a generated pseudo-element box
// in a stable-ID snapshot.
func (snapshot *LayoutSnapshot) PseudoGeometryID(id dom.NodeID, pseudo computed.PseudoElement) (LayoutGeometry, bool) {
	if snapshot == nil || id == dom.InvalidNodeID || pseudo == computed.PseudoElementNone {
		return LayoutGeometry{}, false
	}
	geometry, ok := snapshot.byPseudoID[stablePseudoGeometryKey{id: id, pseudo: pseudo}]
	return geometry, ok
}

func newPointerLayoutSnapshot(
	document *dom.Node,
	root *Box,
	styles map[*dom.Node]computedStyle,
	viewport Viewport,
	computedStyles *computed.Snapshot,
) *LayoutSnapshot {
	snapshot := &LayoutSnapshot{
		viewport:       viewport,
		root:           root,
		styles:         styles,
		computedStyles: computedStyles,
		rootNode:       document,
		byNode:         make(map[*dom.Node]LayoutGeometry),
		byPseudoNode:   make(map[pointerPseudoGeometryKey]LayoutGeometry),
	}
	indexPointerGeometry(root, snapshot.byNode, snapshot.byPseudoNode)
	return snapshot
}

func newStableLayoutSnapshot(
	access *dom.ReadAccess,
	root *Box,
	styles map[*dom.Node]computedStyle,
	viewport Viewport,
	computedStyles *computed.Snapshot,
) (*LayoutSnapshot, error) {
	document := access.Root()
	rootID, ok := access.ID(document)
	if !ok {
		return nil, fmt.Errorf("render: layout document root has no stable identity")
	}
	snapshot := &LayoutSnapshot{
		viewport:       viewport,
		root:           root,
		styles:         styles,
		computedStyles: computedStyles,
		rootID:         rootID,
		document:       access.Identity(),
		version:        access.Version(),
		byID:           make(map[dom.NodeID]LayoutGeometry),
		byPseudoID:     make(map[stablePseudoGeometryKey]LayoutGeometry),
	}
	indexStableGeometry(root, access, snapshot.byID, snapshot.byPseudoID)
	return snapshot, nil
}

type layoutExtent struct {
	right  float64
	bottom float64
}

func boxClientBounds(box *Box) Rect {
	if box == nil {
		return Rect{}
	}
	width := box.Bounds.Width - box.Border.Left - box.Border.Right
	height := box.Bounds.Height - box.Border.Top - box.Border.Bottom
	if width < 0 {
		width = 0
	}
	if height < 0 {
		height = 0
	}
	return Rect{
		X:      box.Bounds.X + box.Border.Left,
		Y:      box.Bounds.Y + box.Border.Top,
		Width:  width,
		Height: height,
	}
}

func boxGeometry(box *Box, extent layoutExtent) LayoutGeometry {
	client := boxClientBounds(box)
	scrollWidth := client.Width
	scrollHeight := client.Height
	if overflow := extent.right - client.X; overflow > scrollWidth {
		scrollWidth = overflow
	}
	if overflow := extent.bottom - client.Y; overflow > scrollHeight {
		scrollHeight = overflow
	}
	return LayoutGeometry{
		Bounds:                box.Bounds,
		ClientBounds:          client,
		ContentBounds:         box.ContentBounds,
		ScrollWidth:           scrollWidth,
		ScrollHeight:          scrollHeight,
		PercentHeightResolved: box.percentHeightResolved,
	}
}

func indexPointerGeometry(box *Box, index map[*dom.Node]LayoutGeometry, pseudoIndex map[pointerPseudoGeometryKey]LayoutGeometry) layoutExtent {
	if box == nil {
		return layoutExtent{}
	}
	extent := layoutExtent{
		right:  box.Bounds.X + box.Bounds.Width,
		bottom: box.Bounds.Y + box.Bounds.Height,
	}
	for _, fragment := range box.Fragments {
		bounds := Rect{}
		switch fragment.Kind {
		case ImageFragmentKind:
			bounds = fragment.Image.Bounds
		case TextFragmentKind:
			bounds = textFragmentBounds(fragment.Text)
		}
		if right := bounds.X + bounds.Width; right > extent.right {
			extent.right = right
		}
		if bottom := bounds.Y + bounds.Height; bottom > extent.bottom {
			extent.bottom = bottom
		}
	}
	for _, child := range box.Children {
		childExtent := indexPointerGeometry(child, index, pseudoIndex)
		if childExtent.right > extent.right {
			extent.right = childExtent.right
		}
		if childExtent.bottom > extent.bottom {
			extent.bottom = childExtent.bottom
		}
	}
	if box.Node != nil && box.Node.Type == dom.ElementNode {
		if box.Pseudo == computed.PseudoElementNone {
			index[box.Node] = boxGeometry(box, extent)
		} else {
			pseudoIndex[pointerPseudoGeometryKey{node: box.Node, pseudo: box.Pseudo}] = boxGeometry(box, extent)
		}
	}
	for _, fragment := range box.Fragments {
		if fragment.Kind != ImageFragmentKind || fragment.Image.Node == nil {
			continue
		}
		if _, exists := index[fragment.Image.Node]; !exists {
			index[fragment.Image.Node] = LayoutGeometry{
				Bounds:                fragment.Image.Bounds,
				ClientBounds:          fragment.Image.Bounds,
				ContentBounds:         fragment.Image.Bounds,
				ScrollWidth:           fragment.Image.Bounds.Width,
				ScrollHeight:          fragment.Image.Bounds.Height,
				PercentHeightResolved: fragment.Image.percentHeightResolved,
			}
		}
	}
	return extent
}

func indexStableGeometry(box *Box, access *dom.ReadAccess, index map[dom.NodeID]LayoutGeometry, pseudoIndex map[stablePseudoGeometryKey]LayoutGeometry) layoutExtent {
	if box == nil {
		return layoutExtent{}
	}
	extent := layoutExtent{
		right:  box.Bounds.X + box.Bounds.Width,
		bottom: box.Bounds.Y + box.Bounds.Height,
	}
	for _, fragment := range box.Fragments {
		bounds := Rect{}
		switch fragment.Kind {
		case ImageFragmentKind:
			bounds = fragment.Image.Bounds
		case TextFragmentKind:
			bounds = textFragmentBounds(fragment.Text)
		}
		if right := bounds.X + bounds.Width; right > extent.right {
			extent.right = right
		}
		if bottom := bounds.Y + bounds.Height; bottom > extent.bottom {
			extent.bottom = bottom
		}
	}
	for _, child := range box.Children {
		childExtent := indexStableGeometry(child, access, index, pseudoIndex)
		if childExtent.right > extent.right {
			extent.right = childExtent.right
		}
		if childExtent.bottom > extent.bottom {
			extent.bottom = childExtent.bottom
		}
	}
	if box.Node != nil && box.Node.Type == dom.ElementNode {
		if id, ok := access.ID(box.Node); ok {
			if box.Pseudo == computed.PseudoElementNone {
				index[id] = boxGeometry(box, extent)
			} else {
				pseudoIndex[stablePseudoGeometryKey{id: id, pseudo: box.Pseudo}] = boxGeometry(box, extent)
			}
		}
	}
	for _, fragment := range box.Fragments {
		if fragment.Kind != ImageFragmentKind || fragment.Image.Node == nil {
			continue
		}
		if id, ok := access.ID(fragment.Image.Node); ok {
			if _, exists := index[id]; !exists {
				index[id] = LayoutGeometry{
					Bounds:                fragment.Image.Bounds,
					ClientBounds:          fragment.Image.Bounds,
					ContentBounds:         fragment.Image.Bounds,
					ScrollWidth:           fragment.Image.Bounds.Width,
					ScrollHeight:          fragment.Image.Bounds.Height,
					PercentHeightResolved: fragment.Image.percentHeightResolved,
				}
			}
		}
	}
	return extent
}
