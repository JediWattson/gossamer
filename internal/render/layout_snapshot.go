package render

import (
	"fmt"

	"github.com/JediWattson/gossamer/internal/dom"
	computed "github.com/JediWattson/gossamer/internal/style"
)

// LayoutGeometry is the used box geometry retained for CSSOM reads. Bounds is
// the border box; ContentBounds is the content box used by resolved width and
// height serialization.
type LayoutGeometry struct {
	Bounds        Rect
	ContentBounds Rect
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
	}
	indexPointerGeometry(root, snapshot.byNode)
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
	}
	indexStableGeometry(root, access, snapshot.byID)
	return snapshot, nil
}

func indexPointerGeometry(box *Box, index map[*dom.Node]LayoutGeometry) {
	if box == nil {
		return
	}
	if box.Node != nil && box.Node.Type == dom.ElementNode {
		index[box.Node] = LayoutGeometry{Bounds: box.Bounds, ContentBounds: box.ContentBounds}
	}
	for _, fragment := range box.Fragments {
		if fragment.Kind != ImageFragmentKind || fragment.Image.Node == nil {
			continue
		}
		if _, exists := index[fragment.Image.Node]; !exists {
			index[fragment.Image.Node] = LayoutGeometry{
				Bounds:        fragment.Image.Bounds,
				ContentBounds: fragment.Image.Bounds,
			}
		}
	}
	for _, child := range box.Children {
		indexPointerGeometry(child, index)
	}
}

func indexStableGeometry(box *Box, access *dom.ReadAccess, index map[dom.NodeID]LayoutGeometry) {
	if box == nil {
		return
	}
	if box.Node != nil && box.Node.Type == dom.ElementNode {
		if id, ok := access.ID(box.Node); ok {
			index[id] = LayoutGeometry{Bounds: box.Bounds, ContentBounds: box.ContentBounds}
		}
	}
	for _, fragment := range box.Fragments {
		if fragment.Kind != ImageFragmentKind || fragment.Image.Node == nil {
			continue
		}
		if id, ok := access.ID(fragment.Image.Node); ok {
			if _, exists := index[id]; !exists {
				index[id] = LayoutGeometry{
					Bounds:        fragment.Image.Bounds,
					ContentBounds: fragment.Image.Bounds,
				}
			}
		}
	}
	for _, child := range box.Children {
		indexStableGeometry(child, access, index)
	}
}
