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

	logicalTable := cloneStyledNodeWithLayoutAxes(table, mode)
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
	inlineSize = context.constrainVerticalTableInlineSize(logicalTable.style, inlineSize, inlineAvailable, inlineInsets, inlineSizeDefinite)
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

func (context *layoutContext) constrainVerticalTableInlineSize(style computedStyle, size, available, insets float64, percentageBaseDefinite bool) float64 {
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

func cloneStyledNodeWithLayoutAxes(node *styledNode, mode writingMode) *styledNode {
	if node == nil {
		return nil
	}
	clone := *node
	clone.style = node.style.withLayoutAxes(mode)
	clone.children = make([]*styledNode, len(node.children))
	for index, child := range node.children {
		clone.children[index] = cloneStyledNodeWithLayoutAxes(child, mode)
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
	fragment.logicalWidth = fragment.Width
	fragment.logicalHeight = fragment.Height
	fragment.logicalBaseline = fragment.BaselineOffset
	fragment.paintOrientation = textPaintSidewaysRight
	fragment.paintBounds = physicalBounds
	fragment.X = physicalBounds.X
	// The internal vertical baseline runs on the physical x axis. Existing
	// horizontal-flow consumers only accept a y baseline, so expose the
	// conservative bottom edge when the entire table participates atomically in
	// a horizontal line while paint retains the real logical baseline above.
	fragment.BaselineY = physicalBounds.Y + physicalBounds.Height
	fragment.BaselineOffset = physicalBounds.Height
	fragment.Width = physicalBounds.Width
	fragment.Height = physicalBounds.Height
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
