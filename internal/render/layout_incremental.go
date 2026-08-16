package render

import (
	"fmt"
	"math"
	"strings"

	"github.com/JediWattson/gossamer/internal/dom"
	computed "github.com/JediWattson/gossamer/internal/style"
)

// ReuseLayoutSnapshotFromReadView advances an immutable layout snapshot when
// mutation records and computed-property metadata prove that used geometry is
// unchanged. Paint-only changes clone paint-bearing artifacts; neutral changes
// share the complete prior layout storage. The boolean is false when a full
// layout pass is required.
func ReuseLayoutSnapshotFromReadView(
	view dom.ReadView,
	viewport Viewport,
	previous *LayoutSnapshot,
	styles *computed.Snapshot,
	records []dom.MutationRecord,
) (*LayoutSnapshot, bool, error) {
	access, err := view.Acquire()
	if err != nil {
		return nil, false, err
	}
	defer access.Close()
	if previous == nil || styles == nil || previous.document != access.Identity() ||
		previous.rootID == dom.InvalidNodeID || previous.rootID != stableLayoutRootID(access) ||
		previous.viewport != viewport || previous.computedStyles == nil || previous.version != previous.computedStyles.Version() {
		return nil, false, fmt.Errorf("render: layout snapshot does not belong to read view")
	}
	if styles.DocumentIdentity() != access.Identity() || styles.RootID() != previous.rootID ||
		styles.Version() != access.Version() || styles.Environment().Width != viewport.Width ||
		styles.Environment().Height != viewport.Height {
		return nil, false, fmt.Errorf("render: current style snapshot does not belong to read view")
	}
	if previous.version == access.Version() {
		if previous.computedStyles != styles {
			return nil, false, nil
		}
		return previous, true, nil
	}
	if len(records) == 0 || !layoutMutationRecordsNeutral(access, records, previous.computedStyles, styles) {
		return nil, false, nil
	}
	damage, comparable := styles.DamageComparedTo(previous.computedStyles)
	if !comparable || damage.Class.HasLayout() {
		return nil, false, nil
	}
	if damageTouchesCollapsedBorderColor(previous, damage) {
		return nil, false, nil
	}

	updated := *previous
	updated.version = access.Version()
	updated.computedStyles = styles
	updated.reusedLayout = true
	updated.damage = nil
	if !damage.Class.HasPaint() {
		return &updated, true, nil
	}
	root, paintStyles, cloneErr := cloneLayoutPaintArtifacts(access, previous, styles)
	if cloneErr != nil {
		return nil, false, cloneErr
	}
	updated.root = root
	updated.styles = paintStyles
	updated.damage = layoutStyleDamageRects(access, previous, damage)
	return &updated, true, nil
}

func stableLayoutRootID(access *dom.ReadAccess) dom.NodeID {
	id, _ := access.ID(access.Root())
	return id
}

func layoutMutationRecordsNeutral(
	access *dom.ReadAccess,
	records []dom.MutationRecord,
	previous, current *computed.Snapshot,
) bool {
	for _, record := range records {
		if !record.Connected {
			continue
		}
		switch record.Type {
		case dom.MutationAttributes:
			node, found := access.Resolve(record.Target)
			if !found || node == nil || node.Type != dom.ElementNode {
				return false
			}
			name := lowerASCIIAttribute(record.AttributeName)
			if directLayoutAttribute(name) || previous.GeneratedContentDependsOnAttribute(name) ||
				current.GeneratedContentDependsOnAttribute(name) {
				return false
			}
		case dom.MutationState:
			// Native state reaches layout only through computed properties in the
			// currently implemented form-control rendering model.
			continue
		case dom.MutationChildList, dom.MutationCharacterData:
			return false
		default:
			return false
		}
	}
	return true
}

func directLayoutAttribute(name string) bool {
	switch name {
	case "alt", "colspan", "reversed", "rowspan", "sizes", "span", "src", "srcset", "start", "value":
		return true
	default:
		return false
	}
}

func lowerASCIIAttribute(value string) string {
	result := []byte(value)
	for index, current := range result {
		if current >= 'A' && current <= 'Z' {
			result[index] = current + ('a' - 'A')
		}
	}
	return string(result)
}

func damageTouchesCollapsedBorderColor(layout *LayoutSnapshot, damage computed.SnapshotStyleDamage) bool {
	if layout == nil || !layoutHasCollapsedBorder(layout.root, layout.styles) {
		return false
	}
	for _, node := range damage.Nodes {
		for _, property := range node.Properties {
			if property == "color" || strings.HasPrefix(property, "border-") && strings.HasSuffix(property, "-color") {
				return true
			}
		}
	}
	return false
}

func layoutHasCollapsedBorder(box *Box, styles map[*dom.Node]computedStyle) bool {
	if box == nil {
		return false
	}
	style, ok := box.style, box.hasStyle
	if !ok {
		style, ok = styles[box.Node]
	}
	if ok && style.BorderCollapse() == borderCollapseCollapse {
		return true
	}
	for _, child := range box.Children {
		if layoutHasCollapsedBorder(child, styles) {
			return true
		}
	}
	return box.tableRoot != nil && layoutHasCollapsedBorder(box.tableRoot, styles)
}

type layoutPaintClone struct {
	access   *dom.ReadAccess
	styles   *computed.Snapshot
	boxes    map[*Box]*Box
	styleMap map[*dom.Node]computedStyle
}

func cloneLayoutPaintArtifacts(
	access *dom.ReadAccess,
	previous *LayoutSnapshot,
	styles *computed.Snapshot,
) (*Box, map[*dom.Node]computedStyle, error) {
	clone := &layoutPaintClone{
		access: access, styles: styles, boxes: make(map[*Box]*Box),
		styleMap: make(map[*dom.Node]computedStyle, len(previous.styles)),
	}
	for node, oldStyle := range previous.styles {
		updated, ok := clone.styleFor(node, computed.PseudoElementNone, oldStyle)
		if ok {
			clone.styleMap[node] = updated
		} else {
			clone.styleMap[node] = oldStyle
		}
	}
	root := clone.box(previous.root)
	if root == nil && previous.root != nil {
		return nil, nil, fmt.Errorf("render: could not clone layout root")
	}
	return root, clone.styleMap, nil
}

func (clone *layoutPaintClone) styleFor(node *dom.Node, pseudo computed.PseudoElement, old computedStyle) (computedStyle, bool) {
	if node == nil {
		return old, false
	}
	id, ok := clone.access.ID(node)
	if !ok {
		return old, false
	}
	var value computed.ComputedStyle
	if pseudo == computed.PseudoElementNone {
		value, ok = clone.styles.LookupID(id)
	} else {
		value, ok = clone.styles.LookupPseudoID(id, pseudo)
	}
	if !ok {
		return old, false
	}
	old.ComputedStyle = value
	return old, true
}

func (clone *layoutPaintClone) box(source *Box) *Box {
	if source == nil {
		return nil
	}
	if existing, ok := clone.boxes[source]; ok {
		return existing
	}
	destination := *source
	clone.boxes[source] = &destination
	if source.hasStyle {
		if style, ok := clone.styleFor(source.Node, source.Pseudo, source.style); ok {
			destination.style = style
		}
	}
	destination.Children = make([]*Box, len(source.Children))
	for index, child := range source.Children {
		destination.Children[index] = clone.box(child)
	}
	destination.tableRoot = clone.box(source.tableRoot)
	destination.flow = append([]flowItem(nil), source.flow...)
	for index := range destination.flow {
		if destination.flow[index].box != nil {
			destination.flow[index].box = clone.box(destination.flow[index].box)
		} else {
			clone.patchInlineFragment(&destination.flow[index].fragment)
		}
	}
	destination.Fragments = append([]InlineFragment(nil), source.Fragments...)
	for index := range destination.Fragments {
		clone.patchInlineFragment(&destination.Fragments[index])
	}
	destination.Text = append([]TextFragment(nil), source.Text...)
	for index := range destination.Text {
		clone.patchTextFragment(&destination.Text[index])
	}
	destination.afterPaint = append([]boxPaintRect(nil), source.afterPaint...)
	for index := range destination.afterPaint {
		clone.patchBorderPaint(&destination.afterPaint[index])
	}
	return &destination
}

func (clone *layoutPaintClone) patchInlineFragment(fragment *InlineFragment) {
	if fragment == nil {
		return
	}
	if fragment.Kind == TextFragmentKind {
		clone.patchTextFragment(&fragment.Text)
	}
}

func (clone *layoutPaintClone) patchTextFragment(fragment *TextFragment) {
	if fragment == nil {
		return
	}
	style, ok := clone.styleFor(fragment.Node, fragment.Pseudo, computedStyle{})
	if !ok {
		return
	}
	value := style.Color()
	value.A = uint8(math.Round(float64(value.A) * fragment.paintOpacity))
	fragment.Color = value
	fragment.Underline = style.Underline()
}

func (clone *layoutPaintClone) patchBorderPaint(paint *boxPaintRect) {
	if paint == nil {
		return
	}
	style, ok := clone.styleFor(paint.Node, paint.Pseudo, computedStyle{})
	if !ok {
		return
	}
	var side computed.BorderSide
	switch paint.Edge {
	case borderPaintRight:
		side = style.BorderRight()
	case borderPaintBottom:
		side = style.BorderBottom()
	case borderPaintLeft:
		side = style.BorderLeft()
	default:
		side = style.BorderTop()
	}
	value, explicit := side.Color()
	if !explicit {
		value = style.Color()
	}
	paint.Color = value
}

func layoutStyleDamageRects(
	access *dom.ReadAccess,
	layout *LayoutSnapshot,
	damage computed.SnapshotStyleDamage,
) []Rect {
	viewport := Rect{Width: float64(layout.viewport.Width), Height: float64(layout.viewport.Height)}
	var union Rect
	hasUnion := false
	for _, nodeDamage := range damage.Nodes {
		node, found := access.Resolve(nodeDamage.Node)
		if !found || node == nil {
			return []Rect{viewport}
		}
		if node.Type == dom.ElementNode && (node.Data == "html" || node.Data == "body") {
			return []Rect{viewport}
		}
		var geometry LayoutGeometry
		var ok bool
		if nodeDamage.Pseudo == computed.PseudoElementNone {
			geometry, ok = layout.byID[nodeDamage.Node]
		} else {
			geometry, ok = layout.byPseudoID[stablePseudoGeometryKey{id: nodeDamage.Node, pseudo: nodeDamage.Pseudo}]
		}
		if !ok {
			return []Rect{viewport}
		}
		rectangle := geometry.Bounds
		if rectangle.Width <= 0 || rectangle.Height <= 0 {
			for _, fragment := range geometry.clientRects {
				rectangle = unionRect(rectangle, fragment)
			}
		}
		if rectangle.Width <= 0 || rectangle.Height <= 0 {
			continue
		}
		if !hasUnion {
			union = rectangle
			hasUnion = true
		} else {
			union = unionRect(union, rectangle)
		}
	}
	if !hasUnion {
		return nil
	}
	return []Rect{union}
}

func unionRect(left, right Rect) Rect {
	if left.Width <= 0 || left.Height <= 0 {
		return right
	}
	if right.Width <= 0 || right.Height <= 0 {
		return left
	}
	x := math.Min(left.X, right.X)
	y := math.Min(left.Y, right.Y)
	rightEdge := math.Max(left.X+left.Width, right.X+right.Width)
	bottomEdge := math.Max(left.Y+left.Height, right.Y+right.Height)
	return Rect{X: x, Y: y, Width: rightEdge - x, Height: bottomEdge - y}
}
