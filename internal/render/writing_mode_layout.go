package render

import (
	"fmt"
	"math"
)

// layoutVerticalTableContainer runs the existing table algorithms in the
// table's logical coordinate space, then publishes one physical tree. This is
// deliberately not a paint-only rotation: every retained rectangle consumed
// by CSSOM, hit testing, clipping, scrolling, and collapsed-border painting is
// transformed together.
func (context *layoutContext) layoutVerticalTableContainer(
	table *styledNode,
	wrapper, tableBox *Box,
	availableBlockSize float64,
	availableInlineSize *float64,
	physicalBlockSize float64,
	blockSizeDefinite bool,
) error {
	if table == nil || wrapper == nil || tableBox == nil {
		return fmt.Errorf("render: invalid vertical table layout input")
	}
	mode := table.style.WritingMode()
	if mode != writingModeVerticalRL && mode != writingModeVerticalLR {
		return fmt.Errorf("render: unsupported vertical table writing mode %d", mode)
	}

	logicalTable := context.cloneStyledNodeWithLayoutAxes(table, mode)
	inlineAvailable := float64(context.viewport.Height)
	inlineSizeDefinite := availableInlineSize != nil && isFinite(*availableInlineSize) && *availableInlineSize >= 0
	if inlineSizeDefinite {
		inlineAvailable = *availableInlineSize
	}
	inlineAvailable = math.Max(0, inlineAvailable)

	logicalPadding := context.resolvePadding(logicalTable.style, inlineAvailable)
	logicalBorder := context.resolveBorder(logicalTable.style, inlineAvailable)
	if logicalTable.style.BorderCollapse() == borderCollapseCollapse {
		collapsedBorder, err := context.collapsedTableOuterEdges(logicalTable)
		if err != nil {
			return err
		}
		logicalPadding = Edges{}
		logicalBorder = collapsedBorder
	}
	inlineInsets := logicalPadding.Left + logicalPadding.Right + logicalBorder.Left + logicalBorder.Right
	intrinsic, err := context.intrinsicContentWidths(logicalTable, inlineAvailable)
	if err != nil {
		return err
	}
	inlineSize := math.Max(0, inlineAvailable-inlineInsets)
	inlineSpecified := logicalTable.style.Width().Unit() != lengthAuto &&
		(inlineSizeDefinite || !logicalTable.style.Width().DependsOnPercent())
	if inlineSpecified {
		inlineSize = resolveLength(logicalTable.style.Width(), inlineAvailable, context.viewport, inlineSize)
		if logicalTable.style.BoxSizing() == boxSizingBorderBox {
			inlineSize = math.Max(0, inlineSize-inlineInsets)
		}
	} else {
		inlineSize = math.Min(math.Max(intrinsic.minimum, inlineSize), intrinsic.preferred)
	}
	inlineSize = context.constrainVerticalInlineSize(logicalTable.style, inlineSize, inlineAvailable, inlineInsets, inlineSizeDefinite)
	// A table grid overflows rather than crushing its intrinsic inline tracks.
	inlineSize = math.Max(inlineSize, intrinsic.minimum)

	originX, originY := wrapper.Bounds.X, wrapper.Bounds.Y
	logicalOuterInline := logicalBorder.Left + logicalPadding.Left + inlineSize + logicalPadding.Right + logicalBorder.Right
	tableBox.Bounds = Rect{X: originX, Y: originY, Width: logicalOuterInline}
	tableBox.ContentBounds = Rect{
		X:     originX + logicalBorder.Left + logicalPadding.Left,
		Y:     originY + logicalBorder.Top + logicalPadding.Top,
		Width: inlineSize,
	}
	tableBox.Padding = logicalPadding
	tableBox.Border = logicalBorder
	tableBox.style = logicalTable.style
	wrapper.Bounds = Rect{X: originX, Y: originY, Width: logicalOuterInline}
	wrapper.ContentBounds = tableBox.ContentBounds
	wrapper.style = logicalTable.style

	logicalContainingBlock := math.Max(0, availableBlockSize)
	var blockTarget *float64
	if blockSizeDefinite {
		target := math.Max(0, physicalBlockSize)
		blockTarget = &target
	}
	blockInsets := logicalPadding.Top + logicalPadding.Bottom + logicalBorder.Top + logicalBorder.Bottom
	if _, err := context.layoutTableContainer(
		logicalTable, wrapper, tableBox, inlineSize, blockTarget, &logicalContainingBlock,
		math.Max(0, physicalBlockSize), blockSizeDefinite, blockInsets,
	); err != nil {
		return err
	}

	logicalBlockExtent := wrapper.Bounds.Height
	transformVerticalLayoutBox(wrapper, originX, originY, logicalBlockExtent, mode)
	return nil
}

func (context *layoutContext) constrainVerticalInlineSize(style computedStyle, size, available, insets float64, percentageBaseDefinite bool) float64 {
	maximum := math.Inf(1)
	if style.MaxWidth().Unit() != lengthAuto && (percentageBaseDefinite || !style.MaxWidth().DependsOnPercent()) {
		maximum = math.Max(0, resolveLength(style.MaxWidth(), available, context.viewport, size))
		if style.BoxSizing() == boxSizingBorderBox {
			maximum = math.Max(0, maximum-insets)
		}
	}
	minimum := 0.0
	if style.MinWidth().Unit() != lengthAuto && (percentageBaseDefinite || !style.MinWidth().DependsOnPercent()) {
		minimum = math.Max(0, resolveLength(style.MinWidth(), available, context.viewport, 0))
		if style.BoxSizing() == boxSizingBorderBox {
			minimum = math.Max(0, minimum-insets)
		}
	}
	return clamp(size, minimum, math.Max(minimum, maximum))
}

// layoutVerticalGridContainer lays out a vertical Grid in its own logical
// inline/block coordinate space and then transforms the complete retained box
// tree back into physical coordinates. Subgrid axes arrive already mapped to
// the child's logical columns and rows, including the orthogonal parent-axis
// swap performed by gridSubgridContextForItem.
func (context *layoutContext) layoutVerticalGridContainer(
	grid *styledNode,
	box *Box,
	availableBlockSize float64,
	availableInlineSize *float64,
	physicalContentBlockSize float64,
	inherited *gridSubgridContext,
) (float64, error) {
	if grid == nil || box == nil {
		return 0, fmt.Errorf("render: invalid vertical grid layout input")
	}
	mode := grid.style.WritingMode()
	if mode != writingModeVerticalRL && mode != writingModeVerticalLR {
		return 0, fmt.Errorf("render: unsupported vertical grid writing mode %d", mode)
	}

	logicalGrid := context.cloneStyledNodeWithLayoutAxes(grid, mode)
	inlineAvailable := float64(context.viewport.Height)
	inlineBaseDefinite := availableInlineSize != nil && isFinite(*availableInlineSize) && *availableInlineSize >= 0
	if inlineBaseDefinite {
		inlineAvailable = *availableInlineSize
	}
	inlineAvailable = math.Max(0, inlineAvailable)

	logicalPadding := context.resolvePadding(logicalGrid.style, inlineAvailable)
	logicalBorder := context.resolveBorder(logicalGrid.style, inlineAvailable)
	inlineInsets := logicalPadding.Left + logicalPadding.Right + logicalBorder.Left + logicalBorder.Right

	columnGap := math.Max(0, resolveLength(logicalGrid.style.ColumnGap(), inlineAvailable, context.viewport, 0))
	rowGap := math.Max(0, resolveLength(logicalGrid.style.RowGap(), inlineAvailable, context.viewport, 0))
	inlineSize := 0.0
	columnSubgrid := inherited != nil && inherited.columns != nil && logicalGrid.style.GridTemplateColumns().IsSubgrid()
	if columnSubgrid {
		_, _, ends := subgridAxisGeometry(inherited.columns, columnGap, logicalGrid.style.ColumnGapNormal())
		if len(ends) != 0 {
			inlineSize = math.Max(0, ends[len(ends)-1])
		}
	} else {
		intrinsic, err := context.intrinsicContentWidths(logicalGrid, inlineAvailable)
		if err != nil {
			return 0, err
		}
		inlineSize = math.Max(0, inlineAvailable-inlineInsets)
		if logicalGrid.style.Width().Unit() != lengthAuto && (inlineBaseDefinite || !logicalGrid.style.Width().DependsOnPercent()) {
			inlineSize = resolveLength(logicalGrid.style.Width(), inlineAvailable, context.viewport, inlineSize)
			if logicalGrid.style.BoxSizing() == boxSizingBorderBox {
				inlineSize = math.Max(0, inlineSize-inlineInsets)
			}
		} else {
			inlineSize = math.Min(math.Max(intrinsic.minimum, inlineSize), intrinsic.preferred)
		}
		inlineSize = context.constrainVerticalInlineSize(logicalGrid.style, inlineSize, inlineAvailable, inlineInsets, inlineBaseDefinite)
	}

	blockSize := math.Max(0, physicalContentBlockSize)
	rowSubgrid := inherited != nil && inherited.rows != nil && logicalGrid.style.GridTemplateRows().IsSubgrid()
	if rowSubgrid {
		_, _, ends := subgridAxisGeometry(inherited.rows, rowGap, logicalGrid.style.RowGapNormal())
		if len(ends) != 0 {
			blockSize = math.Max(0, ends[len(ends)-1])
		}
	}

	originX, originY := box.Bounds.X, box.Bounds.Y
	box.Bounds = Rect{
		X: originX, Y: originY,
		Width:  logicalBorder.Left + logicalPadding.Left + inlineSize + logicalPadding.Right + logicalBorder.Right,
		Height: logicalBorder.Top + logicalPadding.Top + blockSize + logicalPadding.Bottom + logicalBorder.Bottom,
	}
	box.ContentBounds = Rect{
		X:     originX + logicalBorder.Left + logicalPadding.Left,
		Y:     originY + logicalBorder.Top + logicalPadding.Top,
		Width: inlineSize, Height: blockSize,
	}
	box.Padding = logicalPadding
	box.Border = logicalBorder
	box.style = logicalGrid.style

	if _, err := context.layoutGridContainer(logicalGrid, box, inlineSize, &blockSize, &blockSize, false, inherited); err != nil {
		return 0, err
	}
	logicalBlockExtent := box.Bounds.Height
	transformVerticalLayoutBox(box, originX, originY, logicalBlockExtent, mode)
	return inlineSize, nil
}

func (context *layoutContext) layoutHorizontalGridInVerticalPlane(
	grid *styledNode,
	box *Box,
	availableBlockInParent *float64,
	parentInlineContentSize float64,
	inherited *gridSubgridContext,
	parentMode writingMode,
) (float64, error) {
	if grid == nil || box == nil || grid.style.WritingMode() != writingModeHorizontalTB ||
		(parentMode != writingModeVerticalRL && parentMode != writingModeVerticalLR) {
		return 0, fmt.Errorf("render: invalid horizontal grid in vertical layout input")
	}

	logicalGrid := context.cloneStyledNodeWithLayoutAxes(grid, writingModeHorizontalTB)
	inlineAvailable := float64(context.viewport.Width)
	inlineBaseDefinite := availableBlockInParent != nil && isFinite(*availableBlockInParent) && *availableBlockInParent >= 0
	if inlineBaseDefinite {
		inlineAvailable = *availableBlockInParent
	}
	inlineAvailable = math.Max(0, inlineAvailable)
	logicalPadding := context.resolvePadding(logicalGrid.style, inlineAvailable)
	logicalBorder := context.resolveBorder(logicalGrid.style, inlineAvailable)
	inlineInsets := logicalPadding.Left + logicalPadding.Right + logicalBorder.Left + logicalBorder.Right

	columnGap := math.Max(0, resolveLength(logicalGrid.style.ColumnGap(), inlineAvailable, context.viewport, 0))
	rowGap := math.Max(0, resolveLength(logicalGrid.style.RowGap(), inlineAvailable, context.viewport, 0))
	inlineSize := 0.0
	columnSubgrid := inherited != nil && inherited.columns != nil && logicalGrid.style.GridTemplateColumns().IsSubgrid()
	if columnSubgrid {
		_, _, ends := subgridAxisGeometry(inherited.columns, columnGap, logicalGrid.style.ColumnGapNormal())
		if len(ends) != 0 {
			inlineSize = math.Max(0, ends[len(ends)-1])
		}
	} else {
		intrinsic, err := context.intrinsicContentWidths(logicalGrid, inlineAvailable)
		if err != nil {
			return 0, err
		}
		inlineSize = math.Max(0, inlineAvailable-inlineInsets)
		if logicalGrid.style.Width().Unit() != lengthAuto && (inlineBaseDefinite || !logicalGrid.style.Width().DependsOnPercent()) {
			inlineSize = resolveLength(logicalGrid.style.Width(), inlineAvailable, context.viewport, inlineSize)
			if logicalGrid.style.BoxSizing() == boxSizingBorderBox {
				inlineSize = math.Max(0, inlineSize-inlineInsets)
			}
		} else {
			inlineSize = math.Min(math.Max(intrinsic.minimum, inlineSize), intrinsic.preferred)
		}
		inlineSize = context.constrainWidth(logicalGrid.style, inlineSize, inlineAvailable, inlineInsets)
	}

	blockSize := math.Max(0, parentInlineContentSize)
	rowSubgrid := inherited != nil && inherited.rows != nil && logicalGrid.style.GridTemplateRows().IsSubgrid()
	if rowSubgrid {
		_, _, ends := subgridAxisGeometry(inherited.rows, rowGap, logicalGrid.style.RowGapNormal())
		if len(ends) != 0 {
			blockSize = math.Max(0, ends[len(ends)-1])
		}
	}

	originX, originY := box.Bounds.X, box.Bounds.Y
	box.Bounds = Rect{
		X: originX, Y: originY,
		Width:  logicalBorder.Left + logicalPadding.Left + inlineSize + logicalPadding.Right + logicalBorder.Right,
		Height: logicalBorder.Top + logicalPadding.Top + blockSize + logicalPadding.Bottom + logicalBorder.Bottom,
	}
	box.ContentBounds = Rect{
		X:     originX + logicalBorder.Left + logicalPadding.Left,
		Y:     originY + logicalBorder.Top + logicalPadding.Top,
		Width: inlineSize, Height: blockSize,
	}
	box.Padding = logicalPadding
	box.Border = logicalBorder
	box.style = logicalGrid.style
	if _, err := context.layoutGridContainer(logicalGrid, box, inlineSize, &blockSize, &blockSize, false, inherited); err != nil {
		return 0, err
	}

	physicalBlockExtent := box.Bounds.Width
	transformHorizontalLayoutBoxIntoVerticalPlane(box, originX, originY, physicalBlockExtent, parentMode)
	return inlineSize, nil
}

func (context *layoutContext) layoutReversedVerticalGridInVerticalPlane(
	grid *styledNode,
	box *Box,
	contentInlineSize float64,
	definiteBlockSize, repeatBlockSize *float64,
	repeatFulfillsMinimum bool,
	inherited *gridSubgridContext,
	parentMode writingMode,
) (float64, error) {
	if grid == nil || box == nil || grid.style.WritingMode() == writingModeHorizontalTB ||
		parentMode == writingModeHorizontalTB || grid.style.WritingMode() == parentMode {
		return 0, fmt.Errorf("render: invalid reversed vertical grid input")
	}
	logicalGrid := context.cloneStyledNodeWithLayoutAxes(grid, grid.style.WritingMode())
	logicalPadding := context.resolvePadding(logicalGrid.style, contentInlineSize)
	logicalBorder := context.resolveBorder(logicalGrid.style, contentInlineSize)
	box.Padding = logicalPadding
	box.Border = logicalBorder
	box.ContentBounds.X = box.Bounds.X + logicalBorder.Left + logicalPadding.Left
	box.ContentBounds.Y = box.Bounds.Y + logicalBorder.Top + logicalPadding.Top
	box.ContentBounds.Width = contentInlineSize
	box.style = logicalGrid.style
	contentBlockSize, err := context.layoutGridContainer(logicalGrid, box, contentInlineSize, definiteBlockSize, repeatBlockSize, repeatFulfillsMinimum, inherited)
	if err != nil {
		return 0, err
	}
	if definiteBlockSize != nil {
		contentBlockSize = *definiteBlockSize
	}
	box.ContentBounds.Height = math.Max(0, contentBlockSize)
	box.Bounds.Height = logicalBorder.Top + logicalPadding.Top + box.ContentBounds.Height + logicalPadding.Bottom + logicalBorder.Bottom
	transformParallelVerticalLayoutBox(box, box.Bounds.Y, box.Bounds.Height, parentMode)
	return contentBlockSize, nil
}

func (context *layoutContext) cloneStyledNodeWithLayoutAxes(node *styledNode, mode writingMode) *styledNode {
	if node == nil {
		return nil
	}
	key := layoutAxisCloneKey{node: node, mode: mode}
	if context != nil {
		if clone := context.axisCloneCache[key]; clone != nil {
			return clone
		}
	}
	clone := *node
	clone.style = node.style.withLayoutAxes(mode)
	if context != nil {
		if context.axisCloneCache == nil {
			context.axisCloneCache = make(map[layoutAxisCloneKey]*styledNode)
		}
		context.axisCloneCache[key] = &clone
	}
	clone.children = make([]*styledNode, len(node.children))
	for index, child := range node.children {
		clone.children[index] = context.cloneStyledNodeWithLayoutAxes(child, mode)
	}
	return &clone
}

func transformVerticalLayoutBox(box *Box, originX, originY, logicalBlockExtent float64, mode writingMode) {
	if box == nil {
		return
	}
	box.Bounds = transformVerticalRect(box.Bounds, originX, originY, logicalBlockExtent, mode)
	box.ContentBounds = transformVerticalRect(box.ContentBounds, originX, originY, logicalBlockExtent, mode)
	box.Padding = physicalEdgesFromLogical(box.Padding, mode)
	box.Border = physicalEdgesFromLogical(box.Border, mode)
	box.style = box.style.physical()
	if box.hasDecorationBounds {
		box.decorationBounds = transformVerticalRect(box.decorationBounds, originX, originY, logicalBlockExtent, mode)
	}
	if box.hasClipBounds {
		box.clipBounds = transformVerticalRect(box.clipBounds, originX, originY, logicalBlockExtent, mode)
	}
	for index := range box.backgroundRects {
		box.backgroundRects[index] = transformVerticalRect(box.backgroundRects[index], originX, originY, logicalBlockExtent, mode)
	}
	for index := range box.afterPaint {
		box.afterPaint[index].Rect = transformVerticalRect(box.afterPaint[index].Rect, originX, originY, logicalBlockExtent, mode)
		box.afterPaint[index].Edge = physicalBorderEdgeFromLogical(box.afterPaint[index].Edge, mode)
	}
	for index := range box.tableClientRects {
		box.tableClientRects[index] = transformVerticalRect(box.tableClientRects[index], originX, originY, logicalBlockExtent, mode)
	}
	for index := range box.Fragments {
		transformVerticalInlineFragment(&box.Fragments[index], originX, originY, logicalBlockExtent, mode)
	}
	for index := range box.Text {
		transformVerticalTextFragment(&box.Text[index], originX, originY, logicalBlockExtent, mode)
	}
	for index := range box.flow {
		if box.flow[index].box == nil {
			transformVerticalInlineFragment(&box.flow[index].fragment, originX, originY, logicalBlockExtent, mode)
		}
	}
	for _, child := range box.Children {
		transformVerticalLayoutBox(child, originX, originY, logicalBlockExtent, mode)
	}
}

func transformVerticalInlineFragment(fragment *InlineFragment, originX, originY, logicalBlockExtent float64, mode writingMode) {
	if fragment == nil {
		return
	}
	switch fragment.Kind {
	case TextFragmentKind:
		transformVerticalTextFragment(&fragment.Text, originX, originY, logicalBlockExtent, mode)
	case ImageFragmentKind:
		fragment.Image.Bounds = transformVerticalRect(fragment.Image.Bounds, originX, originY, logicalBlockExtent, mode)
	}
}

func transformVerticalTextFragment(fragment *TextFragment, originX, originY, logicalBlockExtent float64, mode writingMode) {
	if fragment == nil {
		return
	}
	logicalBounds := textFragmentBounds(*fragment)
	physicalBounds := transformVerticalRect(logicalBounds, originX, originY, logicalBlockExtent, mode)
	applyQuarterTurnToTextFragment(fragment, physicalBounds)
}

func applyQuarterTurnToTextFragment(fragment *TextFragment, transformedBounds Rect) {
	if fragment == nil {
		return
	}
	if fragment.paintOrientation == textPaintHorizontal {
		fragment.logicalWidth = fragment.Width
		fragment.logicalHeight = fragment.Height
		fragment.logicalBaseline = fragment.BaselineOffset
		fragment.paintOrientation = textPaintSidewaysRight
		fragment.paintBounds = transformedBounds
		fragment.X = transformedBounds.X
		// A sideways baseline runs on the target x axis. Consumers which expose
		// only a y baseline use the conservative bottom edge; paint retains the
		// original metrics above.
		fragment.BaselineY = transformedBounds.Y + transformedBounds.Height
		fragment.BaselineOffset = transformedBounds.Height
		fragment.Width = transformedBounds.Width
		fragment.Height = transformedBounds.Height
		return
	}
	// Crossing a second orthogonal boundary returns the original horizontal
	// glyph run. This is how a horizontal descendant inside a vertical Grid or
	// table remains upright when the outer logical tree is finally projected.
	fragment.paintOrientation = textPaintHorizontal
	fragment.paintBounds = Rect{}
	fragment.X = transformedBounds.X
	fragment.BaselineY = transformedBounds.Y + fragment.logicalBaseline
	fragment.BaselineOffset = fragment.logicalBaseline
	fragment.Width = fragment.logicalWidth
	fragment.Height = fragment.logicalHeight
}

func transformVerticalRect(rect Rect, originX, originY, logicalBlockExtent float64, mode writingMode) Rect {
	inlineOffset := rect.X - originX
	blockOffset := rect.Y - originY
	physicalX := originX + blockOffset
	if mode == writingModeVerticalRL {
		physicalX = originX + logicalBlockExtent - blockOffset - rect.Height
	}
	return Rect{
		X: physicalX, Y: originY + inlineOffset,
		Width: rect.Height, Height: rect.Width,
	}
}

func physicalEdgesFromLogical(edges Edges, mode writingMode) Edges {
	physical := Edges{
		Top: edges.Left, Bottom: edges.Right,
	}
	if mode == writingModeVerticalRL {
		physical.Right = edges.Top
		physical.Left = edges.Bottom
	} else {
		physical.Right = edges.Bottom
		physical.Left = edges.Top
	}
	return physical
}

func physicalBorderEdgeFromLogical(edge borderPaintEdge, mode writingMode) borderPaintEdge {
	switch edge {
	case borderPaintLeft:
		return borderPaintTop
	case borderPaintRight:
		return borderPaintBottom
	case borderPaintTop:
		if mode == writingModeVerticalRL {
			return borderPaintRight
		}
		return borderPaintLeft
	case borderPaintBottom:
		if mode == writingModeVerticalRL {
			return borderPaintLeft
		}
		return borderPaintRight
	default:
		return edge
	}
}

// transformHorizontalLayoutBoxIntoVerticalPlane is the inverse projection
// used by a horizontal formatting root nested in an already-logical vertical
// tree. The outer vertical projection later composes with this one, returning
// horizontal descendants (including text and replaced content) to their
// physical orientation.
func transformHorizontalLayoutBoxIntoVerticalPlane(box *Box, originX, originY, physicalBlockExtent float64, mode writingMode) {
	if box == nil {
		return
	}
	box.Bounds = transformPhysicalRectToVerticalLogical(box.Bounds, originX, originY, physicalBlockExtent, mode)
	box.ContentBounds = transformPhysicalRectToVerticalLogical(box.ContentBounds, originX, originY, physicalBlockExtent, mode)
	box.Padding = physicalEdgesFromLogical(box.Padding, mode)
	box.Border = physicalEdgesFromLogical(box.Border, mode)
	box.style = box.style.withLayoutAxes(mode)
	if box.hasDecorationBounds {
		box.decorationBounds = transformPhysicalRectToVerticalLogical(box.decorationBounds, originX, originY, physicalBlockExtent, mode)
	}
	if box.hasClipBounds {
		box.clipBounds = transformPhysicalRectToVerticalLogical(box.clipBounds, originX, originY, physicalBlockExtent, mode)
	}
	for index := range box.backgroundRects {
		box.backgroundRects[index] = transformPhysicalRectToVerticalLogical(box.backgroundRects[index], originX, originY, physicalBlockExtent, mode)
	}
	for index := range box.afterPaint {
		box.afterPaint[index].Rect = transformPhysicalRectToVerticalLogical(box.afterPaint[index].Rect, originX, originY, physicalBlockExtent, mode)
		box.afterPaint[index].Edge = logicalBorderEdgeFromPhysical(box.afterPaint[index].Edge, mode)
	}
	for index := range box.tableClientRects {
		box.tableClientRects[index] = transformPhysicalRectToVerticalLogical(box.tableClientRects[index], originX, originY, physicalBlockExtent, mode)
	}
	for index := range box.Fragments {
		transformHorizontalInlineFragmentIntoVerticalPlane(&box.Fragments[index], originX, originY, physicalBlockExtent, mode)
	}
	for index := range box.Text {
		transformHorizontalTextFragmentIntoVerticalPlane(&box.Text[index], originX, originY, physicalBlockExtent, mode)
	}
	for index := range box.flow {
		if box.flow[index].box == nil {
			transformHorizontalInlineFragmentIntoVerticalPlane(&box.flow[index].fragment, originX, originY, physicalBlockExtent, mode)
		}
	}
	for _, child := range box.Children {
		transformHorizontalLayoutBoxIntoVerticalPlane(child, originX, originY, physicalBlockExtent, mode)
	}
}

func transformHorizontalInlineFragmentIntoVerticalPlane(fragment *InlineFragment, originX, originY, physicalBlockExtent float64, mode writingMode) {
	if fragment == nil {
		return
	}
	switch fragment.Kind {
	case TextFragmentKind:
		transformHorizontalTextFragmentIntoVerticalPlane(&fragment.Text, originX, originY, physicalBlockExtent, mode)
	case ImageFragmentKind:
		fragment.Image.Bounds = transformPhysicalRectToVerticalLogical(fragment.Image.Bounds, originX, originY, physicalBlockExtent, mode)
	}
}

func transformHorizontalTextFragmentIntoVerticalPlane(fragment *TextFragment, originX, originY, physicalBlockExtent float64, mode writingMode) {
	if fragment == nil {
		return
	}
	bounds := transformPhysicalRectToVerticalLogical(textFragmentBounds(*fragment), originX, originY, physicalBlockExtent, mode)
	applyQuarterTurnToTextFragment(fragment, bounds)
}

func transformPhysicalRectToVerticalLogical(rect Rect, originX, originY, physicalBlockExtent float64, mode writingMode) Rect {
	physicalX := rect.X - originX
	physicalY := rect.Y - originY
	logicalBlock := physicalX
	if mode == writingModeVerticalRL {
		logicalBlock = physicalBlockExtent - physicalX - rect.Width
	}
	return Rect{
		X: originX + physicalY, Y: originY + logicalBlock,
		Width: rect.Height, Height: rect.Width,
	}
}

func logicalBorderEdgeFromPhysical(edge borderPaintEdge, mode writingMode) borderPaintEdge {
	switch edge {
	case borderPaintTop:
		return borderPaintLeft
	case borderPaintBottom:
		return borderPaintRight
	case borderPaintLeft:
		if mode == writingModeVerticalRL {
			return borderPaintBottom
		}
		return borderPaintTop
	case borderPaintRight:
		if mode == writingModeVerticalRL {
			return borderPaintTop
		}
		return borderPaintBottom
	default:
		return edge
	}
}

func transformParallelVerticalLayoutBox(box *Box, originY, blockExtent float64, parentMode writingMode) {
	if box == nil {
		return
	}
	box.Bounds = reflectVerticalBlockRect(box.Bounds, originY, blockExtent)
	box.ContentBounds = reflectVerticalBlockRect(box.ContentBounds, originY, blockExtent)
	box.Padding.Top, box.Padding.Bottom = box.Padding.Bottom, box.Padding.Top
	box.Border.Top, box.Border.Bottom = box.Border.Bottom, box.Border.Top
	box.style = box.style.withLayoutAxes(parentMode)
	if box.hasDecorationBounds {
		box.decorationBounds = reflectVerticalBlockRect(box.decorationBounds, originY, blockExtent)
	}
	if box.hasClipBounds {
		box.clipBounds = reflectVerticalBlockRect(box.clipBounds, originY, blockExtent)
	}
	for index := range box.backgroundRects {
		box.backgroundRects[index] = reflectVerticalBlockRect(box.backgroundRects[index], originY, blockExtent)
	}
	for index := range box.afterPaint {
		box.afterPaint[index].Rect = reflectVerticalBlockRect(box.afterPaint[index].Rect, originY, blockExtent)
		switch box.afterPaint[index].Edge {
		case borderPaintTop:
			box.afterPaint[index].Edge = borderPaintBottom
		case borderPaintBottom:
			box.afterPaint[index].Edge = borderPaintTop
		}
	}
	for index := range box.tableClientRects {
		box.tableClientRects[index] = reflectVerticalBlockRect(box.tableClientRects[index], originY, blockExtent)
	}
	for index := range box.Fragments {
		reflectVerticalBlockInlineFragment(&box.Fragments[index], originY, blockExtent)
	}
	for index := range box.Text {
		reflectVerticalBlockTextFragment(&box.Text[index], originY, blockExtent)
	}
	for index := range box.flow {
		if box.flow[index].box == nil {
			reflectVerticalBlockInlineFragment(&box.flow[index].fragment, originY, blockExtent)
		}
	}
	for _, child := range box.Children {
		transformParallelVerticalLayoutBox(child, originY, blockExtent, parentMode)
	}
}

func reflectVerticalBlockInlineFragment(fragment *InlineFragment, originY, blockExtent float64) {
	if fragment == nil {
		return
	}
	switch fragment.Kind {
	case TextFragmentKind:
		reflectVerticalBlockTextFragment(&fragment.Text, originY, blockExtent)
	case ImageFragmentKind:
		fragment.Image.Bounds = reflectVerticalBlockRect(fragment.Image.Bounds, originY, blockExtent)
	}
}

func reflectVerticalBlockTextFragment(fragment *TextFragment, originY, blockExtent float64) {
	if fragment == nil {
		return
	}
	bounds := reflectVerticalBlockRect(textFragmentBounds(*fragment), originY, blockExtent)
	fragment.X = bounds.X
	if fragment.paintOrientation == textPaintHorizontal {
		fragment.BaselineY = bounds.Y + fragment.BaselineOffset
		return
	}
	fragment.paintBounds = bounds
	fragment.BaselineY = bounds.Y + bounds.Height
}

func reflectVerticalBlockRect(rect Rect, originY, blockExtent float64) Rect {
	rect.Y = originY + blockExtent - (rect.Y - originY) - rect.Height
	return rect
}
