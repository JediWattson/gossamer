package render

import (
	"fmt"
	"image"
	"math"
	"reflect"
	"strings"

	"github.com/JediWattson/gossamer/internal/dom"
	computed "github.com/JediWattson/gossamer/internal/style"
)

// ReuseFrameWithLayoutSnapshot advances an immutable Frame header when a
// pointer-based layout snapshot reused its prior box tree without paint damage.
// The returned Frame shares the prior DisplayList and PaintArtifactID.
func ReuseFrameWithLayoutSnapshot(document *dom.Node, previous *Frame, snapshot *LayoutSnapshot) (*Frame, bool, error) {
	if err := validatePointerLayoutSnapshot(document, snapshot); err != nil {
		return nil, false, err
	}
	frame, reused := reuseFrameForValidatedLayout(previous, snapshot)
	return frame, reused, nil
}

// ReuseFrameReadViewWithLayoutSnapshot advances an immutable Frame header when
// a stable-ID layout snapshot reused its prior box tree without paint damage.
// The returned Frame shares the prior DisplayList and PaintArtifactID so
// downstream scroll projection and retained framebuffers can avoid full
// display-list rebuilds and artifact fallbacks on paint-neutral DOM mutations.
func ReuseFrameReadViewWithLayoutSnapshot(view dom.ReadView, previous *Frame, snapshot *LayoutSnapshot) (*Frame, bool, error) {
	access, err := view.Acquire()
	if err != nil {
		return nil, false, err
	}
	defer access.Close()
	if err := validateStableLayoutSnapshot(access, snapshot); err != nil {
		return nil, false, err
	}
	frame, reused := reuseFrameForValidatedLayout(previous, snapshot)
	return frame, reused, nil
}

func reuseFrameForValidatedLayout(previous *Frame, snapshot *LayoutSnapshot) (*Frame, bool) {
	if previous == nil || snapshot == nil || previous.Layout == nil || previous.paintArtifact == 0 {
		return nil, false
	}
	if previous.Layout.document != snapshot.document ||
		previous.Layout.rootID != snapshot.rootID ||
		previous.Layout.rootNode != snapshot.rootNode {
		return nil, false
	}
	if previous.Viewport != snapshot.viewport ||
		previous.DisplayList.Viewport != snapshot.viewport ||
		len(previous.DisplayList.Commands) == 0 {
		return nil, false
	}
	if previous.Layout == snapshot &&
		previous.ComputedStyles == snapshot.computedStyles &&
		previous.Root == snapshot.root {
		return previous, true
	}
	if !snapshot.ReusedLayout() || len(snapshot.damage) != 0 {
		return nil, false
	}
	if previous.Root != snapshot.root ||
		!sameBackgroundImageMap(previous.DisplayList.backgroundImages, snapshot.backgroundImages) ||
		!sameFrameFonts(previous.Fonts, snapshot.fonts) {
		return nil, false
	}
	updated := *previous
	updated.Layout = snapshot
	updated.ComputedStyles = snapshot.computedStyles
	return &updated, true
}

func sameBackgroundImageMap(left, right map[*dom.Node]map[string]image.Image) bool {
	if len(left) != len(right) {
		return false
	}
	if len(left) == 0 {
		return true
	}
	if reflect.ValueOf(left).Pointer() == reflect.ValueOf(right).Pointer() {
		return true
	}
	for node, leftImages := range left {
		rightImages, ok := right[node]
		if !ok || len(leftImages) != len(rightImages) {
			return false
		}
		for url, leftImage := range leftImages {
			rightImage, found := rightImages[url]
			if !found || !sameImageIdentity(leftImage, rightImage) {
				return false
			}
		}
	}
	return true
}

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
	if damageTouchesCollapsedBorderColor(previous, damage) || damageTouchesTableStructuralBackground(previous, styles, damage) {
		return nil, false, nil
	}

	updated := *previous
	updated.version = access.Version()
	updated.computedStyles = styles
	updated.reusedLayout = true
	updated.damage = nil
	if !damage.Class.HasPaint() || allDamageNodesHidden(access, damage, styles) {
		return &updated, true, nil
	}
	root, paintStyles, cloneErr := cloneLayoutPaintArtifacts(access, previous, styles)
	if cloneErr != nil {
		return nil, false, cloneErr
	}
	updated.root = root
	updated.styles = paintStyles
	updated.damage = layoutStyleDamageRects(access, previous, root, paintStyles, styles, damage)
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
			if directLayoutAttribute(name) ||
				(dom.IsHTMLCanvas(node) && (name == "width" || name == "height")) ||
				previous.GeneratedContentDependsOnAttribute(name) ||
				current.GeneratedContentDependsOnAttribute(name) {
				return false
			}
		case dom.MutationState:
			if record.StateName == "canvas" {
				return false
			}
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

func damageTouchesTableStructuralBackground(
	previous *LayoutSnapshot,
	styles *computed.Snapshot,
	damage computed.SnapshotStyleDamage,
) bool {
	if previous == nil || styles == nil {
		return false
	}
	for _, node := range damage.Nodes {
		if node.Pseudo != computed.PseudoElementNone || !sliceContainsProperty(node.Properties, "background-color") {
			continue
		}
		current, ok := styles.LookupID(node.Node)
		if !ok && previous.computedStyles != nil {
			current, ok = previous.computedStyles.LookupID(node.Node)
		}
		if !ok {
			continue
		}
		switch current.Display() {
		case displayTableRow, displayRowGroup, displayHeaderGroup, displayFooterGroup, displayTableColumn, displayColumnGroup:
			return true
		}
	}
	return false
}

func sliceContainsProperty(properties []string, target string) bool {
	for _, property := range properties {
		if property == target {
			return true
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
	paint.Color = side.ResolveColor(style.Color())
}

func layoutStyleDamageRects(
	access *dom.ReadAccess,
	previous *LayoutSnapshot,
	currentRoot *Box,
	currentStyles map[*dom.Node]computedStyle,
	styles *computed.Snapshot,
	damage computed.SnapshotStyleDamage,
) []Rect {
	viewport := Rect{Width: float64(previous.viewport.Width), Height: float64(previous.viewport.Height)}
	var rectangles []Rect
	for _, nodeDamage := range damage.Nodes {
		node, found := access.Resolve(nodeDamage.Node)
		if !found || node == nil {
			return []Rect{viewport}
		}
		if node.Type == dom.ElementNode && (node.Data == "html" || node.Data == "body") {
			return []Rect{viewport}
		}
		if nodeDamageIsHidden(access, node, nodeDamage, styles) {
			continue
		}
		includeSubtree := nodeDamageTouchesSubtree(nodeDamage)
		if node.Type == dom.ElementNode && nodeDamage.Pseudo == computed.PseudoElementNone {
			if _, hasPrincipalBox := previous.byID[nodeDamage.Node]; !hasPrincipalBox {
				includeSubtree = true
			}
		}
		oldBounds, oldFound := paintedNodeDamageBounds(previous.root, previous.styles, previous.viewport, node, nodeDamage.Pseudo, includeSubtree)
		newBounds, newFound := paintedNodeDamageBounds(currentRoot, currentStyles, previous.viewport, node, nodeDamage.Pseudo, includeSubtree)
		rectangle := unionRect(oldBounds, newBounds)
		if !oldFound && !newFound {
			if geometry, ok := lookupLayoutNodeGeometry(previous, nodeDamage); ok {
				rectangle = geometry.Bounds
				if rectangle.Width <= 0 || rectangle.Height <= 0 {
					for _, fragment := range geometry.clientRects {
						rectangle = unionRect(rectangle, fragment)
					}
				}
			} else if node.Type == dom.TextNode || node.Type == dom.ElementNode {
				continue
			} else {
				return []Rect{viewport}
			}
		}
		if rectangle.Width <= 0 || rectangle.Height <= 0 {
			continue
		}
		rectangles = append(rectangles, rectangle)
	}
	return coalesceLayoutDamageRects(rectangles)
}

func lookupLayoutNodeGeometry(layout *LayoutSnapshot, nodeDamage computed.NodeStyleDamage) (LayoutGeometry, bool) {
	if layout == nil {
		return LayoutGeometry{}, false
	}
	if nodeDamage.Pseudo == computed.PseudoElementNone {
		geometry, ok := layout.byID[nodeDamage.Node]
		return geometry, ok
	}
	geometry, ok := layout.byPseudoID[stablePseudoGeometryKey{id: nodeDamage.Node, pseudo: nodeDamage.Pseudo}]
	return geometry, ok
}

func allDamageNodesHidden(
	access *dom.ReadAccess,
	damage computed.SnapshotStyleDamage,
	styles *computed.Snapshot,
) bool {
	if len(damage.Nodes) == 0 {
		return true
	}
	for _, nodeDamage := range damage.Nodes {
		node, found := access.Resolve(nodeDamage.Node)
		if !found || node == nil || !nodeDamageIsHidden(access, node, nodeDamage, styles) {
			return false
		}
	}
	return true
}

func nodeDamageIsHidden(
	access *dom.ReadAccess,
	node *dom.Node,
	nodeDamage computed.NodeStyleDamage,
	styles *computed.Snapshot,
) bool {
	if styles == nil || node == nil {
		return false
	}
	if nodeDamage.Pseudo == computed.PseudoElementNone {
		if style, ok := styles.LookupID(nodeDamage.Node); ok && style.Display() == displayNone {
			return true
		}
	} else {
		style, ok := styles.LookupPseudoID(nodeDamage.Node, nodeDamage.Pseudo)
		if !ok || style.Display() == displayNone {
			return true
		}
	}
	for ancestor := composedDamageParent(node.Parent); ancestor != nil; ancestor = composedDamageParent(ancestor.Parent) {
		if ancestor.Type != dom.ElementNode {
			continue
		}
		if id, ok := access.ID(ancestor); ok {
			if style, found := styles.LookupID(id); found && style.Display() == displayNone {
				return true
			}
		}
	}
	return false
}

func composedDamageParent(node *dom.Node) *dom.Node {
	if node != nil && node.ShadowHost != nil {
		return node.ShadowHost
	}
	return node
}

func nodeDamageTouchesSubtree(nodeDamage computed.NodeStyleDamage) bool {
	for _, property := range nodeDamage.Properties {
		switch property {
		case "rotate", "scale", "text-decoration-line", "transform", "transform-origin", "translate":
			return true
		}
	}
	return false
}

func domNodeWithin(node, ancestor *dom.Node) bool {
	if node == nil || ancestor == nil {
		return false
	}
	for current := node; current != nil; current = composedDamageParent(current.Parent) {
		if current == ancestor {
			return true
		}
	}
	return false
}

func paintedNodeDamageBounds(
	root *Box,
	styles map[*dom.Node]computedStyle,
	viewport Viewport,
	targetNode *dom.Node,
	targetPseudo computed.PseudoElement,
	includeSubtree bool,
) (Rect, bool) {
	if root == nil || targetNode == nil {
		return Rect{}, false
	}
	var visit func(box *Box, matchSubtree bool) (Rect, bool)
	visit = func(box *Box, matchSubtree bool) (Rect, bool) {
		if box == nil {
			return Rect{}, false
		}
		style, hasStyle := box.style, box.hasStyle
		if !hasStyle {
			style, hasStyle = styles[box.Node]
		}
		directMatch := box.Node == targetNode && box.Pseudo == targetPseudo
		domSubtreeMatch := includeSubtree && targetPseudo == computed.PseudoElementNone && domNodeWithin(box.Node, targetNode)
		boxMatched := matchSubtree || directMatch || domSubtreeMatch

		var boxDamage Rect
		found := false
		if boxMatched {
			found = true
			if !box.suppressDecorations {
				decoration := box.Bounds
				if box.hasDecorationBounds {
					decoration = box.decorationBounds
				}
				decoration = expandRectForBoxShadows(decoration, style, viewport)
				boxDamage = unionRect(boxDamage, decoration)
				for _, backgroundRect := range box.backgroundRects {
					boxDamage = unionRect(boxDamage, backgroundRect)
				}
			}
		}

		childMatchSubtree := matchSubtree || (directMatch && includeSubtree) || domSubtreeMatch
		var contentDamage Rect
		addTextFragment := func(fragment TextFragment) {
			fragmentMatched := childMatchSubtree || directMatch ||
				(fragment.Node == targetNode && fragment.Pseudo == targetPseudo) ||
				(includeSubtree && targetPseudo == computed.PseudoElementNone && domNodeWithin(fragment.Node, targetNode))
			if !fragmentMatched {
				return
			}
			found = true
			contentDamage = unionRect(contentDamage, textFragmentPaintDamageBounds(fragment))
		}
		addImageFragment := func(fragment ImageFragment) {
			fragmentMatched := childMatchSubtree ||
				(fragment.Node == targetNode && targetPseudo == computed.PseudoElementNone) ||
				(includeSubtree && targetPseudo == computed.PseudoElementNone && domNodeWithin(fragment.Node, targetNode))
			if !fragmentMatched {
				return
			}
			found = true
			contentDamage = unionRect(contentDamage, fragment.Bounds)
		}

		if len(box.flow) != 0 {
			for _, item := range box.flow {
				if item.box != nil {
					continue
				}
				switch item.fragment.Kind {
				case TextFragmentKind:
					addTextFragment(item.fragment.Text)
				case ImageFragmentKind:
					addImageFragment(item.fragment.Image)
				}
			}
		} else if len(box.Fragments) != 0 {
			for _, fragment := range box.Fragments {
				switch fragment.Kind {
				case TextFragmentKind:
					addTextFragment(fragment.Text)
				case ImageFragmentKind:
					addImageFragment(fragment.Image)
				}
			}
		} else {
			for _, fragment := range box.Text {
				addTextFragment(fragment)
			}
		}

		visitedTableRoot := false
		for _, child := range box.Children {
			if child == box.tableRoot {
				visitedTableRoot = true
			}
			if childBounds, childFound := visit(child, childMatchSubtree); childFound {
				found = true
				contentDamage = unionRect(contentDamage, childBounds)
			}
		}
		if box.tableRoot != nil && !visitedTableRoot {
			if tableBounds, tableFound := visit(box.tableRoot, childMatchSubtree); tableFound {
				found = true
				contentDamage = unionRect(contentDamage, tableBounds)
			}
		}
		for _, overlay := range box.afterPaint {
			overlayMatched := childMatchSubtree || directMatch ||
				(overlay.Node == targetNode && overlay.Pseudo == targetPseudo) ||
				(includeSubtree && targetPseudo == computed.PseudoElementNone && domNodeWithin(overlay.Node, targetNode))
			if overlayMatched {
				found = true
				contentDamage = unionRect(contentDamage, overlay.Rect)
			}
		}

		if hasStyle && clipsOverflow(style) && contentDamage.Width > 0 && contentDamage.Height > 0 {
			contentDamage = intersectRects(contentDamage, boxClientBounds(box))
		}
		total := unionRect(boxDamage, contentDamage)
		if box.hasClipBounds && total.Width > 0 && total.Height > 0 {
			total = intersectRects(total, box.clipBounds)
		}
		if hasStyle && hasUsedTransform(style) && found {
			if !boxMatched {
				if fullGroup, ok := visit(box, true); ok {
					total = unionRect(total, fullGroup)
					return total, true
				}
			}
			if total.Width > 0 && total.Height > 0 {
				matrix := usedTransform(style, box.Bounds, viewport)
				transformed := transformRasterRect(total, matrix)
				if matrix.A == 1 && matrix.B == 0 && matrix.C == 0 && matrix.D == 1 &&
					isFinite(matrix.E) && isFinite(matrix.F) {
					transformed = unionRect(transformed, Rect{
						X:      matrix.E,
						Y:      matrix.F,
						Width:  total.Width,
						Height: total.Height,
					})
				}
				total = Rect{
					X:      transformed.X - 3,
					Y:      transformed.Y - 3,
					Width:  transformed.Width + 6,
					Height: transformed.Height + 6,
				}
			}
		}
		return total, found
	}
	return visit(root, false)
}

func expandRectForBoxShadows(bounds Rect, style computedStyle, viewport Viewport) Rect {
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return bounds
	}
	result := bounds
	shadows := style.BoxShadows()
	for index := 0; index < shadows.Len(); index++ {
		shadow, ok := shadows.At(index)
		if !ok || shadow.Inset() {
			continue
		}
		offsetX := resolveLength(shadow.OffsetX(), 0, viewport, 0)
		offsetY := resolveLength(shadow.OffsetY(), 0, viewport, 0)
		blur := math.Max(0, resolveLength(shadow.BlurRadius(), 0, viewport, 0))
		spread := resolveLength(shadow.SpreadRadius(), 0, viewport, 0)
		shadowColor := shadow.Color(style.Color())
		if shadowColor.A == 0 || !isFinite(offsetX) || !isFinite(offsetY) || !isFinite(blur) || !isFinite(spread) {
			continue
		}
		extent := math.Ceil(blur/2)*3 + 2 + math.Abs(spread)
		shadowBounds := Rect{
			X:      bounds.X + offsetX - extent,
			Y:      bounds.Y + offsetY - extent,
			Width:  bounds.Width + extent*2,
			Height: bounds.Height + extent*2,
		}
		result = unionRect(result, shadowBounds)
	}
	return result
}

func textFragmentPaintDamageBounds(fragment TextFragment) Rect {
	bounds := textFragmentBounds(fragment)
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return Rect{}
	}
	thickness := math.Max(1, fragment.FontSize/16)
	underline := Rect{X: fragment.X, Y: fragment.BaselineY + thickness, Width: fragment.Width, Height: thickness}
	if fragment.paintOrientation != textPaintHorizontal {
		underline = Rect{
			X: fragment.paintBounds.X, Y: fragment.paintBounds.Y,
			Width: thickness, Height: fragment.paintBounds.Height,
		}
	}
	return unionRect(bounds, underline)
}

func coalesceLayoutDamageRects(rectangles []Rect) []Rect {
	filtered := make([]Rect, 0, len(rectangles))
	for _, rectangle := range rectangles {
		if rectangle.Width > 0 && rectangle.Height > 0 {
			filtered = append(filtered, rectangle)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	for {
		merged := false
		for first := 0; first < len(filtered) && !merged; first++ {
			for second := first + 1; second < len(filtered); second++ {
				if rectsOverlapOrTouch(filtered[first], filtered[second]) {
					filtered[first] = unionRect(filtered[first], filtered[second])
					filtered = append(filtered[:second], filtered[second+1:]...)
					merged = true
					break
				}
			}
		}
		if !merged {
			break
		}
	}
	for len(filtered) > maxRasterDamageRects {
		bestFirst, bestSecond := 0, 1
		bestPenalty := math.Inf(1)
		for first := 0; first < len(filtered); first++ {
			for second := first + 1; second < len(filtered); second++ {
				combined := unionRect(filtered[first], filtered[second])
				penalty := combined.Width*combined.Height -
					(filtered[first].Width*filtered[first].Height + filtered[second].Width*filtered[second].Height)
				if penalty < bestPenalty {
					bestFirst, bestSecond, bestPenalty = first, second, penalty
				}
			}
		}
		filtered[bestFirst] = unionRect(filtered[bestFirst], filtered[bestSecond])
		filtered = append(filtered[:bestSecond], filtered[bestSecond+1:]...)
	}
	return filtered
}

func rectsOverlapOrTouch(left, right Rect) bool {
	if left.Width <= 0 || left.Height <= 0 || right.Width <= 0 || right.Height <= 0 {
		return false
	}
	return left.X <= right.X+right.Width &&
		right.X <= left.X+left.Width &&
		left.Y <= right.Y+right.Height &&
		right.Y <= left.Y+left.Height
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
