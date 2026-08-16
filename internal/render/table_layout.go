package render

import (
	"fmt"
	"image/color"
	"math"
	"strconv"
	"strings"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
)

const (
	maxTableRows    = 4096
	maxTableColumns = 1024
	maxTableCells   = 16384
	maxTableGridOps = 4_000_000
	// Collapsed borders retain two compact segment planes. Keep their combined
	// size independently bounded so a sparse but very wide table cannot turn
	// border harmonization into an unbounded allocation.
	maxTableCollapsedSegments = 500_000
	maxTableBackgroundRects   = 500_000
)

type tableModel struct {
	captions    []*styledNode
	rows        []tableRowRecord
	columns     []*styledNode
	columnBoxes []tableColumnSpec
	columnCount int
	cells       []tableCellPlacement
}

type tableRowRecord struct {
	node     *styledNode
	group    *styledNode
	groupEnd int
}

type tableCellPlacement struct {
	node       *styledNode
	row        int
	column     int
	rowSpan    int
	columnSpan int
}

type tableColumnSpec struct {
	node     *styledNode
	start    int
	span     int
	children []tableColumnSpec
}

type tableCellLayout struct {
	placement     tableCellPlacement
	box           *Box
	naturalHeight float64
	baseline      float64
	hasBaseline   bool
}

type collapsedBorderSource uint8

const (
	collapsedBorderTable collapsedBorderSource = iota + 1
	collapsedBorderColumnGroup
	collapsedBorderColumn
	collapsedBorderRowGroup
	collapsedBorderRow
	collapsedBorderCell
)

type collapsedBorder struct {
	width  float64
	style  borderStyle
	color  color.NRGBA
	node   *dom.Node
	pseudo css.PseudoElement
	source collapsedBorderSource
}

type collapsedBorderGrid struct {
	rows       int
	columns    int
	vertical   []collapsedBorder
	horizontal []collapsedBorder
}

func (grid *collapsedBorderGrid) verticalAt(line, row int) *collapsedBorder {
	if grid == nil || line < 0 || line > grid.columns || row < 0 || row >= grid.rows {
		return nil
	}
	return &grid.vertical[row*(grid.columns+1)+line]
}

func (grid *collapsedBorderGrid) horizontalAt(line, column int) *collapsedBorder {
	if grid == nil || line < 0 || line > grid.rows || column < 0 || column >= grid.columns {
		return nil
	}
	return &grid.horizontal[line*grid.columns+column]
}

func buildTableModel(table *styledNode) (tableModel, error) {
	model := tableModel{}
	for _, child := range table.children {
		if child == nil || child.style.Display() == displayNone {
			continue
		}
		switch child.style.Display() {
		case displayCaption:
			model.captions = append(model.captions, child)
		case displayColumnGroup:
			spec, err := appendTableColumnGroup(&model, child)
			if err != nil {
				return tableModel{}, err
			}
			model.columnBoxes = append(model.columnBoxes, spec)
		case displayTableColumn:
			spec, err := appendTableColumn(&model, child)
			if err != nil {
				return tableModel{}, err
			}
			model.columnBoxes = append(model.columnBoxes, spec)
		case displayRowGroup, displayHeaderGroup, displayFooterGroup:
			if err := appendTableRowGroup(&model, child); err != nil {
				return tableModel{}, err
			}
		case displayTableRow:
			if len(model.rows) >= maxTableRows {
				return tableModel{}, fmt.Errorf("render: table exceeds %d rows", maxTableRows)
			}
			model.rows = append(model.rows, tableRowRecord{node: child})
		}
	}
	assignDirectRowGroupEnds(model.rows)
	if err := placeTableCells(&model); err != nil {
		return tableModel{}, err
	}
	if model.columnCount > len(model.columns) {
		model.columns = append(model.columns, make([]*styledNode, model.columnCount-len(model.columns))...)
	}
	return model, nil
}

func (context *layoutContext) resolveCollapsedTableBorders(table *styledNode, model tableModel) (*collapsedBorderGrid, error) {
	if table == nil || table.style.BorderCollapse() != borderCollapseCollapse {
		return nil, nil
	}
	verticalCount := len(model.rows) * (model.columnCount + 1)
	horizontalCount := (len(model.rows) + 1) * model.columnCount
	if verticalCount < 0 || horizontalCount < 0 || verticalCount+horizontalCount > maxTableCollapsedSegments {
		return nil, fmt.Errorf("render: collapsed table exceeds %d border segments", maxTableCollapsedSegments)
	}
	grid := &collapsedBorderGrid{
		rows: len(model.rows), columns: model.columnCount,
		vertical: make([]collapsedBorder, verticalCount), horizontal: make([]collapsedBorder, horizontalCount),
	}
	context.considerCollapsedBox(grid, table, collapsedBorderTable, 0, model.columnCount, 0, len(model.rows))
	var considerColumnSpec func(tableColumnSpec)
	considerColumnSpec = func(spec tableColumnSpec) {
		source := collapsedBorderColumn
		if spec.node != nil && spec.node.style.Display() == displayColumnGroup {
			source = collapsedBorderColumnGroup
		}
		context.considerCollapsedBox(grid, spec.node, source, spec.start, spec.start+spec.span, 0, len(model.rows))
		for _, child := range spec.children {
			considerColumnSpec(child)
		}
	}
	for _, spec := range model.columnBoxes {
		considerColumnSpec(spec)
	}
	for row := 0; row < len(model.rows); {
		group := model.rows[row].group
		end := row + 1
		for end < len(model.rows) && model.rows[end].group == group {
			end++
		}
		if group != nil {
			context.considerCollapsedBox(grid, group, collapsedBorderRowGroup, 0, model.columnCount, row, end)
		}
		for index := row; index < end; index++ {
			context.considerCollapsedBox(grid, model.rows[index].node, collapsedBorderRow, 0, model.columnCount, index, index+1)
		}
		row = end
	}
	for _, placement := range model.cells {
		context.considerCollapsedBox(
			grid, placement.node, collapsedBorderCell,
			placement.column, placement.column+placement.columnSpan,
			placement.row, placement.row+placement.rowSpan,
		)
	}
	return grid, nil
}

func (context *layoutContext) considerCollapsedBox(grid *collapsedBorderGrid, node *styledNode, source collapsedBorderSource, columnStart, columnEnd, rowStart, rowEnd int) {
	if grid == nil || node == nil || columnStart < 0 || rowStart < 0 || columnEnd > grid.columns || rowEnd > grid.rows || columnStart >= columnEnd || rowStart >= rowEnd {
		return
	}
	top := context.collapsedBorderCandidate(node, node.style.BorderTop(), source)
	right := context.collapsedBorderCandidate(node, node.style.BorderRight(), source)
	bottom := context.collapsedBorderCandidate(node, node.style.BorderBottom(), source)
	left := context.collapsedBorderCandidate(node, node.style.BorderLeft(), source)
	for column := columnStart; column < columnEnd; column++ {
		considerCollapsedBorder(grid.horizontalAt(rowStart, column), top)
		considerCollapsedBorder(grid.horizontalAt(rowEnd, column), bottom)
	}
	for row := rowStart; row < rowEnd; row++ {
		considerCollapsedBorder(grid.verticalAt(columnStart, row), left)
		considerCollapsedBorder(grid.verticalAt(columnEnd, row), right)
	}
}

func (context *layoutContext) collapsedBorderCandidate(node *styledNode, side borderSide, source collapsedBorderSource) collapsedBorder {
	borderColor, explicit := side.Color()
	if !explicit {
		borderColor = node.style.Color()
	}
	if node.style.Visibility() != visibilityVisible {
		borderColor = color.NRGBA{}
	}
	width := math.Max(0, resolveLength(side.Width(), 0, context.viewport, 0))
	if side.Style() == borderStyleNone {
		width = 0
	}
	return collapsedBorder{
		width: width, style: side.Style(), color: borderColor,
		node: node.node, pseudo: node.pseudo, source: source,
	}
}

func considerCollapsedBorder(current *collapsedBorder, candidate collapsedBorder) {
	if current == nil || !collapsedBorderMoreSpecific(candidate, *current) {
		return
	}
	*current = candidate
}

func collapsedBorderMoreSpecific(candidate, current collapsedBorder) bool {
	if candidate.style == borderStyleHidden {
		return current.style != borderStyleHidden
	}
	if current.style == borderStyleHidden {
		return false
	}
	if candidate.style == borderStyleNone {
		return false
	}
	if current.style == borderStyleNone {
		return true
	}
	if candidate.width != current.width {
		return candidate.width > current.width
	}
	// The supported style subset has only solid between hidden and none. Once
	// widths and styles tie, CSS gives the element role priority. Equal-role
	// conflicts retain the first top/left candidate by traversal order.
	return candidate.source > current.source
}

func (grid *collapsedBorderGrid) outerHalfEdges() Edges {
	if grid == nil {
		return Edges{}
	}
	edges := Edges{}
	for column := 0; column < grid.columns; column++ {
		if top := grid.horizontalAt(0, column); top != nil {
			edges.Top = math.Max(edges.Top, top.width/2)
		}
		if bottom := grid.horizontalAt(grid.rows, column); bottom != nil {
			edges.Bottom = math.Max(edges.Bottom, bottom.width/2)
		}
	}
	for row := 0; row < grid.rows; row++ {
		if left := grid.verticalAt(0, row); left != nil {
			edges.Left = math.Max(edges.Left, left.width/2)
		}
		if right := grid.verticalAt(grid.columns, row); right != nil {
			edges.Right = math.Max(edges.Right, right.width/2)
		}
	}
	return edges
}

func (grid *collapsedBorderGrid) cellHalfEdges(placement tableCellPlacement) Edges {
	if grid == nil {
		return Edges{}
	}
	edges := Edges{}
	for column := placement.column; column < placement.column+placement.columnSpan; column++ {
		if top := grid.horizontalAt(placement.row, column); top != nil {
			edges.Top = math.Max(edges.Top, top.width/2)
		}
		if bottom := grid.horizontalAt(placement.row+placement.rowSpan, column); bottom != nil {
			edges.Bottom = math.Max(edges.Bottom, bottom.width/2)
		}
	}
	for row := placement.row; row < placement.row+placement.rowSpan; row++ {
		if left := grid.verticalAt(placement.column, row); left != nil {
			edges.Left = math.Max(edges.Left, left.width/2)
		}
		if right := grid.verticalAt(placement.column+placement.columnSpan, row); right != nil {
			edges.Right = math.Max(edges.Right, right.width/2)
		}
	}
	return edges
}

func appendTableRowGroup(model *tableModel, group *styledNode) error {
	start := len(model.rows)
	for _, child := range group.children {
		if child == nil || child.style.Display() != displayTableRow {
			continue
		}
		if len(model.rows) >= maxTableRows {
			return fmt.Errorf("render: table exceeds %d rows", maxTableRows)
		}
		model.rows = append(model.rows, tableRowRecord{node: child, group: group})
	}
	end := len(model.rows)
	for index := start; index < end; index++ {
		model.rows[index].groupEnd = end
	}
	return nil
}

func assignDirectRowGroupEnds(rows []tableRowRecord) {
	for start := 0; start < len(rows); {
		if rows[start].group != nil {
			start++
			continue
		}
		end := start + 1
		for end < len(rows) && rows[end].group == nil {
			end++
		}
		for index := start; index < end; index++ {
			rows[index].groupEnd = end
		}
		start = end
	}
}

func appendTableColumnGroup(model *tableModel, group *styledNode) (tableColumnSpec, error) {
	spec := tableColumnSpec{node: group, start: len(model.columns)}
	for _, child := range group.children {
		if child == nil || child.style.Display() != displayTableColumn {
			continue
		}
		column, err := appendTableColumn(model, child)
		if err != nil {
			return tableColumnSpec{}, err
		}
		spec.children = append(spec.children, column)
	}
	if len(spec.children) == 0 {
		span := tableSpan(group.node, "span", 1, maxTableColumns)
		if len(model.columns)+span > maxTableColumns {
			return tableColumnSpec{}, fmt.Errorf("render: table exceeds %d columns", maxTableColumns)
		}
		for range span {
			model.columns = append(model.columns, group)
		}
	}
	spec.span = len(model.columns) - spec.start
	return spec, nil
}

func appendTableColumn(model *tableModel, column *styledNode) (tableColumnSpec, error) {
	span := tableSpan(column.node, "span", 1, maxTableColumns)
	if len(model.columns)+span > maxTableColumns {
		return tableColumnSpec{}, fmt.Errorf("render: table exceeds %d columns", maxTableColumns)
	}
	spec := tableColumnSpec{node: column, start: len(model.columns), span: span}
	for range span {
		model.columns = append(model.columns, column)
	}
	return spec, nil
}

func placeTableCells(model *tableModel) error {
	occupiedUntil := make([]int, len(model.columns))
	operations := 0
	for rowIndex, row := range model.rows {
		column := 0
		for _, child := range row.node.children {
			if child == nil || child.style.Display() != displayTableCell {
				continue
			}
			if len(model.cells) >= maxTableCells {
				return fmt.Errorf("render: table exceeds %d cells", maxTableCells)
			}
			columnSpan := tableSpan(child.node, "colspan", 1, maxTableColumns)
			rowSpan := tableRowSpan(child.node, rowIndex, row.groupEnd)
			for {
				operations++
				if operations > maxTableGridOps {
					return fmt.Errorf("render: table exceeds %d grid operations", maxTableGridOps)
				}
				if column+columnSpan > maxTableColumns {
					return fmt.Errorf("render: table exceeds %d columns", maxTableColumns)
				}
				if column+columnSpan > len(occupiedUntil) {
					occupiedUntil = append(occupiedUntil, make([]int, column+columnSpan-len(occupiedUntil))...)
				}
				available := true
				for candidate := column; candidate < column+columnSpan; candidate++ {
					operations++
					if operations > maxTableGridOps {
						return fmt.Errorf("render: table exceeds %d grid operations", maxTableGridOps)
					}
					if occupiedUntil[candidate] > rowIndex {
						available = false
						break
					}
				}
				if available {
					break
				}
				column++
			}
			endRow := rowIndex + rowSpan
			for candidate := column; candidate < column+columnSpan; candidate++ {
				operations++
				if operations > maxTableGridOps {
					return fmt.Errorf("render: table exceeds %d grid operations", maxTableGridOps)
				}
				occupiedUntil[candidate] = endRow
			}
			model.cells = append(model.cells, tableCellPlacement{
				node: child, row: rowIndex, column: column, rowSpan: rowSpan, columnSpan: columnSpan,
			})
			column += columnSpan
			model.columnCount = max(model.columnCount, column)
		}
	}
	model.columnCount = max(model.columnCount, len(model.columns))
	return nil
}

func tableSpan(node *dom.Node, name string, fallback, maximum int) int {
	if node == nil {
		return fallback
	}
	raw, ok := attribute(node, name)
	if !ok {
		return fallback
	}
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return fallback
	}
	return min(value, maximum)
}

func tableRowSpan(node *dom.Node, row, groupEnd int) int {
	if groupEnd <= row {
		return 1
	}
	if node == nil {
		return 1
	}
	raw, ok := attribute(node, "rowspan")
	if !ok {
		return 1
	}
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < 0 {
		return 1
	}
	if value == 0 {
		return groupEnd - row
	}
	return min(value, groupEnd-row)
}

func (context *layoutContext) intrinsicTableContentWidths(table *styledNode, availableWidth float64) (intrinsicWidths, error) {
	model, err := buildTableModel(table)
	if err != nil {
		return intrinsicWidths{}, err
	}
	horizontalSpacing, _ := tableBorderSpacing(table.style, context.viewport)
	collapsed, err := context.resolveCollapsedTableBorders(table, model)
	if err != nil {
		return intrinsicWidths{}, err
	}
	minimums, preferred, err := context.tableColumnIntrinsicWidths(model, availableWidth, horizontalSpacing, collapsed)
	if err != nil {
		return intrinsicWidths{}, err
	}
	spacing := totalTableSpacing(model.columnCount, horizontalSpacing)
	result := intrinsicWidths{minimum: sumFloat64(minimums) + spacing, preferred: sumFloat64(preferred) + spacing}
	for _, caption := range model.captions {
		measured, measureErr := context.intrinsicOuterWidths(caption, availableWidth)
		if measureErr != nil {
			return intrinsicWidths{}, measureErr
		}
		result.minimum = math.Max(result.minimum, measured.minimum)
		result.preferred = math.Max(result.preferred, measured.preferred)
	}
	if result.preferred < result.minimum {
		result.preferred = result.minimum
	}
	return result, nil
}

func (context *layoutContext) tableColumnIntrinsicWidths(model tableModel, availableWidth, horizontalSpacing float64, collapsed *collapsedBorderGrid) ([]float64, []float64, error) {
	minimums := make([]float64, model.columnCount)
	preferred := make([]float64, model.columnCount)
	for index, column := range model.columns {
		if index >= len(minimums) || column == nil || column.style.Width().Unit() == lengthAuto || column.style.Width().DependsOnPercent() {
			continue
		}
		width := math.Max(0, resolveLength(column.style.Width(), availableWidth, context.viewport, 0))
		minimums[index], preferred[index] = width, width
	}
	spanning := make([]struct {
		placement tableCellPlacement
		measured  intrinsicWidths
	}, 0)
	operations := 0
	for _, placement := range model.cells {
		operations++
		if operations > maxTableGridOps {
			return nil, nil, fmt.Errorf("render: table exceeds %d sizing operations", maxTableGridOps)
		}
		measured, err := context.intrinsicTableCellWidths(placement, collapsed, availableWidth)
		if err != nil {
			return nil, nil, err
		}
		if placement.columnSpan == 1 {
			minimums[placement.column] = math.Max(minimums[placement.column], measured.minimum)
			preferred[placement.column] = math.Max(preferred[placement.column], measured.preferred)
			continue
		}
		spanning = append(spanning, struct {
			placement tableCellPlacement
			measured  intrinsicWidths
		}{placement: placement, measured: measured})
	}
	for _, entry := range spanning {
		start := entry.placement.column
		end := start + entry.placement.columnSpan
		operations += entry.placement.columnSpan
		if operations > maxTableGridOps {
			return nil, nil, fmt.Errorf("render: table exceeds %d sizing operations", maxTableGridOps)
		}
		internalSpacing := horizontalSpacing * float64(max(0, entry.placement.columnSpan-1))
		distributeTableSpanDeficit(minimums[start:end], math.Max(0, entry.measured.minimum-internalSpacing))
		distributeTableSpanDeficit(preferred[start:end], math.Max(0, entry.measured.preferred-internalSpacing))
	}
	for index := range preferred {
		preferred[index] = math.Max(preferred[index], minimums[index])
	}
	return minimums, preferred, nil
}

func (context *layoutContext) intrinsicTableCellWidths(placement tableCellPlacement, collapsed *collapsedBorderGrid, availableWidth float64) (intrinsicWidths, error) {
	cell := placement.node
	if cell == nil {
		return intrinsicWidths{}, nil
	}
	style := cell.style
	padding := context.resolvePadding(style, availableWidth)
	border := context.resolveBorder(style, availableWidth)
	if collapsed != nil {
		border = collapsed.cellHalfEdges(placement)
	}
	insets := padding.Left + padding.Right + border.Left + border.Right
	if style.Width().Unit() != lengthAuto && !style.Width().DependsOnPercent() {
		width := math.Max(0, resolveLength(style.Width(), availableWidth, context.viewport, 0))
		if style.BoxSizing() == boxSizingContentBox {
			width += insets
		}
		return intrinsicWidths{minimum: width, preferred: width}, nil
	}
	content, err := context.intrinsicContentWidths(cell, availableWidth)
	if err != nil {
		return intrinsicWidths{}, err
	}
	content.minimum += insets
	content.preferred += insets
	return content, nil
}

func distributeTableSpanDeficit(widths []float64, required float64) {
	if len(widths) == 0 {
		return
	}
	deficit := required - sumFloat64(widths)
	if deficit <= 0 {
		return
	}
	addition := deficit / float64(len(widths))
	for index := range widths {
		widths[index] += addition
	}
}

func distributeTableWidths(minimums, preferred []float64, target float64) []float64 {
	widths := append([]float64(nil), minimums...)
	if len(widths) == 0 {
		return widths
	}
	minimumTotal := sumFloat64(minimums)
	preferredTotal := sumFloat64(preferred)
	target = math.Max(target, minimumTotal)
	if preferredTotal > minimumTotal && target < preferredTotal {
		ratio := (target - minimumTotal) / (preferredTotal - minimumTotal)
		for index := range widths {
			widths[index] += (preferred[index] - minimums[index]) * ratio
		}
		return widths
	}
	copy(widths, preferred)
	if target > preferredTotal {
		addition := (target - preferredTotal) / float64(len(widths))
		for index := range widths {
			widths[index] += addition
		}
	}
	return widths
}

func (context *layoutContext) layoutTableContainer(table *styledNode, tableBox *Box, contentWidth float64, containingHeight *float64) (float64, float64, error) {
	model, err := buildTableModel(table)
	if err != nil {
		return 0, 0, err
	}
	collapsed, err := context.resolveCollapsedTableBorders(table, model)
	if err != nil {
		return 0, 0, err
	}
	horizontalSpacing, verticalSpacing := tableBorderSpacing(table.style, context.viewport)
	clipStructuralBackgrounds := horizontalSpacing > 0 || verticalSpacing > 0
	if clipStructuralBackgrounds {
		if count := tableStructuralBackgroundRectCount(model); count > maxTableBackgroundRects {
			return 0, 0, fmt.Errorf("render: table exceeds %d structural background rectangles", maxTableBackgroundRects)
		}
	}
	spacingWidth := totalTableSpacing(model.columnCount, horizontalSpacing)
	assignableWidth := math.Max(0, contentWidth-spacingWidth)

	var columnWidths []float64
	fixed := table.style.TableLayout() == tableLayoutFixed && table.style.Width().Unit() != lengthAuto
	if fixed {
		columnWidths, err = context.fixedTableColumnWidths(model, assignableWidth, contentWidth, horizontalSpacing, collapsed)
	} else {
		var minimums, preferred []float64
		minimums, preferred, err = context.tableColumnIntrinsicWidths(model, contentWidth, horizontalSpacing, collapsed)
		if err == nil {
			columnWidths = distributeTableWidths(minimums, preferred, assignableWidth)
			gridWidth := sumFloat64(columnWidths) + spacingWidth
			usedWidth := math.Max(contentWidth, gridWidth)
			if len(columnWidths) != 0 && usedWidth > gridWidth {
				columnWidths = distributeTableWidths(minimums, preferred, math.Max(0, usedWidth-spacingWidth))
			}
		}
	}
	if err != nil {
		return 0, 0, err
	}
	columnStarts, columnEnds, gridWidth := tableTrackGeometry(columnWidths, horizontalSpacing)
	usedWidth := math.Max(contentWidth, gridWidth)

	topCaptions := make([]*styledNode, 0, len(model.captions))
	bottomCaptions := make([]*styledNode, 0, len(model.captions))
	for _, caption := range model.captions {
		if caption.style.CaptionSide() == captionSideBottom {
			bottomCaptions = append(bottomCaptions, caption)
		} else {
			topCaptions = append(topCaptions, caption)
		}
	}
	layoutCaptions := func(captions []*styledNode, startY float64) ([]*Box, float64, error) {
		boxes := make([]*Box, 0, len(captions))
		cursor := startY
		for _, caption := range captions {
			captionBox, layoutErr := context.layoutBlockSized(caption, tableBox.ContentBounds.X, cursor, usedWidth, containingHeight, nil, true)
			if layoutErr != nil {
				return nil, 0, layoutErr
			}
			boxes = append(boxes, captionBox)
			cursor = captionBox.Bounds.Y + captionBox.Bounds.Height
		}
		return boxes, cursor - startY, nil
	}
	topBoxes, topCaptionHeight, err := layoutCaptions(topCaptions, tableBox.ContentBounds.Y)
	if err != nil {
		return 0, 0, err
	}
	bottomBoxes, bottomCaptionHeight, err := layoutCaptions(bottomCaptions, 0)
	if err != nil {
		return 0, 0, err
	}
	tableBox.Children = append(tableBox.Children, topBoxes...)
	gridY := tableBox.ContentBounds.Y + topCaptionHeight

	rowHeights := make([]float64, len(model.rows))
	for index, row := range model.rows {
		if row.node == nil || row.node.style.Height().Unit() == lengthAuto || row.node.style.Height().DependsOnPercent() {
			continue
		}
		rowHeights[index] = math.Max(0, resolveLength(row.node.style.Height(), 0, context.viewport, 0))
	}
	cellLayouts := make([]tableCellLayout, 0, len(model.cells))
	rowBaselines := make([]float64, len(model.rows))
	for _, placement := range model.cells {
		spanWidth := tableTrackSpan(columnStarts, columnEnds, placement.column, placement.columnSpan)
		padding := context.resolvePadding(placement.node.style, spanWidth)
		border := context.resolveBorder(placement.node.style, spanWidth)
		if collapsed != nil {
			border = collapsed.cellHalfEdges(placement)
		}
		content := math.Max(0, spanWidth-padding.Left-padding.Right-border.Left-border.Right)
		cellX := tableBox.ContentBounds.X + columnStarts[placement.column]
		cellBox, layoutErr := context.layoutBlockSized(placement.node, cellX, 0, spanWidth, nil, &content, true)
		if layoutErr != nil {
			return 0, 0, layoutErr
		}
		if collapsed != nil {
			adjustCollapsedTableCellBox(cellBox, cellX, spanWidth, padding, border)
			cellBox.suppressBorders = true
		} else {
			translateLayoutBox(cellBox, cellX-cellBox.Bounds.X, 0)
		}
		baseline, hasBaseline := firstBoxBaseline(cellBox)
		if hasBaseline {
			baseline -= cellBox.Bounds.Y
		}
		layout := tableCellLayout{
			placement: placement, box: cellBox, naturalHeight: cellBox.Bounds.Height,
			baseline: baseline, hasBaseline: hasBaseline,
		}
		if table.style.BorderCollapse() == borderCollapseSeparate &&
			placement.node.style.EmptyCells() == emptyCellsHide && tableCellIsEmpty(placement.node) {
			cellBox.suppressDecorations = true
		}
		if placement.rowSpan == 1 {
			rowHeights[placement.row] = math.Max(rowHeights[placement.row], cellBox.Bounds.Height)
			if tableCellUsesBaseline(placement.node.style.VerticalAlignment()) && hasBaseline {
				rowBaselines[placement.row] = math.Max(rowBaselines[placement.row], baseline)
			}
		}
		cellLayouts = append(cellLayouts, layout)
	}
	for _, cell := range cellLayouts {
		if cell.placement.rowSpan != 1 || !tableCellUsesBaseline(cell.placement.node.style.VerticalAlignment()) || !cell.hasBaseline {
			continue
		}
		required := rowBaselines[cell.placement.row] + cell.naturalHeight - cell.baseline
		rowHeights[cell.placement.row] = math.Max(rowHeights[cell.placement.row], required)
	}
	for _, cell := range cellLayouts {
		if cell.placement.rowSpan == 1 {
			continue
		}
		start := cell.placement.row
		end := start + cell.placement.rowSpan
		occupied := sumFloat64(rowHeights[start:end]) + verticalSpacing*float64(max(0, cell.placement.rowSpan-1))
		deficit := cell.box.Bounds.Height - occupied
		if deficit <= 0 {
			continue
		}
		addition := deficit / float64(cell.placement.rowSpan)
		for index := start; index < end; index++ {
			rowHeights[index] += addition
		}
	}
	spacingHeight := totalTableSpacing(len(rowHeights), verticalSpacing)
	if containingHeight != nil && len(rowHeights) != 0 {
		extra := *containingHeight - topCaptionHeight - bottomCaptionHeight - spacingHeight - sumFloat64(rowHeights)
		if extra > 0 {
			addition := extra / float64(len(rowHeights))
			for index := range rowHeights {
				rowHeights[index] += addition
			}
		}
	}
	rowStarts, rowEnds, gridHeight := tableTrackGeometry(rowHeights, verticalSpacing)
	rowBoxes := make([]*Box, len(model.rows))
	for index, row := range model.rows {
		rowBoxes[index] = tableStructuralBox(row.node, Rect{
			X: tableBox.ContentBounds.X, Y: gridY + rowStarts[index], Width: gridWidth, Height: rowHeights[index],
		})
		if clipStructuralBackgrounds && tableNodeHasBackground(row.node) {
			rowBoxes[index].backgroundRects = tableRowBackgroundRects(tableBox.ContentBounds.X, gridY, columnStarts, columnEnds, rowStarts[index], rowEnds[index])
		}
	}
	for _, cell := range cellLayouts {
		start := cell.placement.row
		targetHeight := tableTrackSpan(rowStarts, rowEnds, start, cell.placement.rowSpan)
		setBoxOuterHeight(cell.box, targetHeight)
		translateLayoutBox(cell.box, 0, gridY+rowStarts[start]-cell.box.Bounds.Y)
		shift := tableCellContentShift(cell, targetHeight, rowBaselines[start])
		translateBoxContents(cell.box, 0, shift)
	}

	for _, spec := range model.columnBoxes {
		if columnBox := layoutTableColumnSpec(spec, tableBox.ContentBounds.X, gridY, columnStarts, columnEnds, rowStarts, rowEnds, clipStructuralBackgrounds); columnBox != nil {
			tableBox.Children = append(tableBox.Children, columnBox)
		}
	}
	for index := 0; index < len(model.rows); {
		group := model.rows[index].group
		if group == nil {
			tableBox.Children = append(tableBox.Children, rowBoxes[index])
			index++
			continue
		}
		end := index + 1
		for end < len(model.rows) && model.rows[end].group == group {
			end++
		}
		groupBox := tableStructuralBox(group, Rect{
			X: tableBox.ContentBounds.X, Y: gridY + rowStarts[index], Width: gridWidth,
			Height: rowEnds[end-1] - rowStarts[index],
		})
		if clipStructuralBackgrounds && tableNodeHasBackground(group) {
			for row := index; row < end; row++ {
				groupBox.backgroundRects = append(groupBox.backgroundRects,
					tableRowBackgroundRects(tableBox.ContentBounds.X, gridY, columnStarts, columnEnds, rowStarts[row], rowEnds[row])...)
			}
		}
		groupBox.Children = append(groupBox.Children, rowBoxes[index:end]...)
		tableBox.Children = append(tableBox.Children, groupBox)
		index = end
	}
	// CSS table backgrounds layer table -> columns -> row groups -> rows ->
	// cells. Keep cells as the final grid children rather than nesting them
	// under rows so a later row background cannot cover a spanning cell.
	for _, cell := range cellLayouts {
		tableBox.Children = append(tableBox.Children, cell.box)
	}
	bottomY := gridY + gridHeight
	for _, captionBox := range bottomBoxes {
		translateLayoutBox(captionBox, 0, bottomY-captionBox.Bounds.Y)
		tableBox.Children = append(tableBox.Children, captionBox)
		bottomY = captionBox.Bounds.Y + captionBox.Bounds.Height
	}
	if len(model.rows) != 0 || model.columnCount != 0 || len(topCaptions) != 0 || len(bottomCaptions) != 0 {
		tableBox.hasDecorationBounds = true
		tableBox.decorationBounds = Rect{
			X:      tableBox.ContentBounds.X - tableBox.Padding.Left - tableBox.Border.Left,
			Y:      gridY - tableBox.Padding.Top - tableBox.Border.Top,
			Width:  usedWidth + tableBox.Padding.Left + tableBox.Padding.Right + tableBox.Border.Left + tableBox.Border.Right,
			Height: gridHeight + tableBox.Padding.Top + tableBox.Padding.Bottom + tableBox.Border.Top + tableBox.Border.Bottom,
		}
	}
	if collapsed != nil && collapsed.rows != 0 && collapsed.columns != 0 {
		tableBox.suppressBorders = true
		tableBox.afterPaint = collapsed.paintRects(tableBox.ContentBounds.X, gridY, columnStarts, columnEnds, rowStarts, rowEnds)
	}
	return usedWidth, topCaptionHeight + gridHeight + bottomCaptionHeight, nil
}

func layoutTableColumnSpec(spec tableColumnSpec, x, y float64, columnStarts, columnEnds, rowStarts, rowEnds []float64, clipBackground bool) *Box {
	if spec.span <= 0 || spec.start < 0 || spec.start+spec.span > len(columnEnds) {
		return nil
	}
	height := 0.0
	if len(rowEnds) != 0 {
		height = rowEnds[len(rowEnds)-1]
	}
	box := tableStructuralBox(spec.node, Rect{
		X: x + columnStarts[spec.start], Y: y,
		Width: columnEnds[spec.start+spec.span-1] - columnStarts[spec.start], Height: height,
	})
	if clipBackground && tableNodeHasBackground(spec.node) {
		for row := range rowStarts {
			for column := spec.start; column < spec.start+spec.span; column++ {
				box.backgroundRects = append(box.backgroundRects, Rect{
					X: x + columnStarts[column], Y: y + rowStarts[row],
					Width: columnEnds[column] - columnStarts[column], Height: rowEnds[row] - rowStarts[row],
				})
			}
		}
	}
	for _, child := range spec.children {
		if childBox := layoutTableColumnSpec(child, x, y, columnStarts, columnEnds, rowStarts, rowEnds, clipBackground); childBox != nil {
			box.Children = append(box.Children, childBox)
		}
	}
	return box
}

func tableBorderSpacing(style computedStyle, viewport Viewport) (float64, float64) {
	if style.BorderCollapse() == borderCollapseCollapse {
		return 0, 0
	}
	spacing := style.BorderSpacing()
	horizontal := math.Max(0, resolveLength(spacing.Horizontal(), 0, viewport, 0))
	vertical := math.Max(0, resolveLength(spacing.Vertical(), 0, viewport, 0))
	return horizontal, vertical
}

func (context *layoutContext) collapsedTableOuterEdges(table *styledNode) (Edges, error) {
	if table == nil || table.style.BorderCollapse() != borderCollapseCollapse {
		return Edges{}, nil
	}
	model, err := buildTableModel(table)
	if err != nil {
		return Edges{}, err
	}
	grid, err := context.resolveCollapsedTableBorders(table, model)
	if err != nil {
		return Edges{}, err
	}
	// With no cell grid there are no border segments to harmonize. The table's
	// own collapsed border still contributes half its specified width to the
	// table box, and is painted by the ordinary box-border path.
	if grid.rows == 0 || grid.columns == 0 {
		border := context.resolveBorder(table.style, 0)
		return Edges{
			Top: border.Top / 2, Right: border.Right / 2,
			Bottom: border.Bottom / 2, Left: border.Left / 2,
		}, nil
	}
	return grid.outerHalfEdges(), nil
}

func totalTableSpacing(trackCount int, spacing float64) float64 {
	if trackCount <= 0 {
		return 0
	}
	return float64(trackCount+1) * spacing
}

func tableTrackGeometry(widths []float64, spacing float64) ([]float64, []float64, float64) {
	starts := make([]float64, len(widths))
	ends := make([]float64, len(widths))
	if len(widths) == 0 {
		return starts, ends, 0
	}
	cursor := spacing
	for index, width := range widths {
		starts[index] = cursor
		ends[index] = cursor + math.Max(0, width)
		cursor = ends[index] + spacing
	}
	return starts, ends, cursor
}

func tableTrackSpan(starts, ends []float64, start, span int) float64 {
	if span <= 0 || start < 0 || start >= len(starts) || start+span > len(ends) {
		return 0
	}
	return math.Max(0, ends[start+span-1]-starts[start])
}

func (context *layoutContext) fixedTableColumnWidths(model tableModel, assignableWidth, tableWidth, horizontalSpacing float64, collapsed *collapsedBorderGrid) ([]float64, error) {
	widths := make([]float64, model.columnCount)
	assigned := make([]bool, model.columnCount)
	for index, column := range model.columns {
		if index >= len(widths) || column == nil || column.style.Width().Unit() == lengthAuto {
			continue
		}
		width := resolveLength(column.style.Width(), tableWidth, context.viewport, 0)
		if !isFinite(width) || width < 0 {
			continue
		}
		widths[index], assigned[index] = width, true
	}
	operations := 0
	for _, placement := range model.cells {
		if placement.row != 0 || placement.node == nil || placement.node.style.Width().Unit() == lengthAuto {
			continue
		}
		operations += placement.columnSpan
		if operations > maxTableGridOps {
			return nil, fmt.Errorf("render: table exceeds %d fixed-sizing operations", maxTableGridOps)
		}
		padding := context.resolvePadding(placement.node.style, tableWidth)
		border := context.resolveBorder(placement.node.style, tableWidth)
		if collapsed != nil {
			border = collapsed.cellHalfEdges(placement)
		}
		required := resolveLength(placement.node.style.Width(), tableWidth, context.viewport, 0)
		if placement.node.style.BoxSizing() == boxSizingContentBox {
			required += padding.Left + padding.Right + border.Left + border.Right
		}
		required = math.Max(0, required-horizontalSpacing*float64(max(0, placement.columnSpan-1)))
		start, end := placement.column, placement.column+placement.columnSpan
		current := sumFloat64(widths[start:end])
		if required <= current {
			continue
		}
		unassigned := make([]int, 0, placement.columnSpan)
		for column := start; column < end; column++ {
			if !assigned[column] {
				unassigned = append(unassigned, column)
			}
		}
		if len(unassigned) == 0 {
			continue
		}
		addition := (required - current) / float64(len(unassigned))
		for _, column := range unassigned {
			widths[column] += addition
			assigned[column] = true
		}
	}
	remainingColumns := make([]int, 0, len(widths))
	for index := range widths {
		if !assigned[index] {
			remainingColumns = append(remainingColumns, index)
		}
	}
	remaining := assignableWidth - sumFloat64(widths)
	if len(remainingColumns) != 0 {
		share := math.Max(0, remaining) / float64(len(remainingColumns))
		for _, column := range remainingColumns {
			widths[column] = share
		}
	} else if remaining > 0 && len(widths) != 0 {
		share := remaining / float64(len(widths))
		for index := range widths {
			widths[index] += share
		}
	}
	return widths, nil
}

func adjustCollapsedTableCellBox(box *Box, x, width float64, padding, border Edges) {
	if box == nil {
		return
	}
	oldContentX, oldContentY := box.ContentBounds.X, box.ContentBounds.Y
	contentHeight := box.ContentBounds.Height
	box.Bounds.X = x
	box.Bounds.Width = math.Max(0, width)
	box.Bounds.Height = border.Top + padding.Top + contentHeight + padding.Bottom + border.Bottom
	box.Padding = padding
	box.Border = border
	box.ContentBounds = Rect{
		X:      x + border.Left + padding.Left,
		Y:      box.Bounds.Y + border.Top + padding.Top,
		Width:  math.Max(0, width-border.Left-padding.Left-padding.Right-border.Right),
		Height: contentHeight,
	}
	translateBoxContents(box, box.ContentBounds.X-oldContentX, box.ContentBounds.Y-oldContentY)
}

func (grid *collapsedBorderGrid) paintRects(x, y float64, columnStarts, columnEnds, rowStarts, rowEnds []float64) []boxPaintRect {
	if grid == nil || len(columnStarts) == 0 || len(rowStarts) == 0 {
		return nil
	}
	xLine := func(line int) float64 {
		switch {
		case line <= 0:
			return x + columnStarts[0]
		case line >= len(columnEnds):
			return x + columnEnds[len(columnEnds)-1]
		default:
			return x + (columnEnds[line-1]+columnStarts[line])/2
		}
	}
	yLine := func(line int) float64 {
		switch {
		case line <= 0:
			return y + rowStarts[0]
		case line >= len(rowEnds):
			return y + rowEnds[len(rowEnds)-1]
		default:
			return y + (rowEnds[line-1]+rowStarts[line])/2
		}
	}
	result := make([]boxPaintRect, 0, len(grid.horizontal)+len(grid.vertical))
	for line := 0; line <= grid.rows; line++ {
		for column := 0; column < grid.columns; column++ {
			border := grid.horizontalAt(line, column)
			if border == nil || border.style != borderStyleSolid || border.width <= 0 || border.color.A == 0 {
				continue
			}
			left, right := xLine(column), xLine(column+1)
			result = append(result, boxPaintRect{
				Node: border.node, Pseudo: border.pseudo, Color: border.color,
				Rect: Rect{X: left, Y: yLine(line) - border.width/2, Width: math.Max(0, right-left), Height: border.width},
			})
		}
	}
	for line := 0; line <= grid.columns; line++ {
		for row := 0; row < grid.rows; row++ {
			border := grid.verticalAt(line, row)
			if border == nil || border.style != borderStyleSolid || border.width <= 0 || border.color.A == 0 {
				continue
			}
			top, bottom := yLine(row), yLine(row+1)
			result = append(result, boxPaintRect{
				Node: border.node, Pseudo: border.pseudo, Color: border.color,
				Rect: Rect{X: xLine(line) - border.width/2, Y: top, Width: border.width, Height: math.Max(0, bottom-top)},
			})
		}
	}
	return result
}

func tableStructuralBackgroundRectCount(model tableModel) int {
	count := 0
	add := func(value int) bool {
		if value < 0 || count > maxTableBackgroundRects-value {
			count = maxTableBackgroundRects + 1
			return false
		}
		count += value
		return true
	}
	for _, row := range model.rows {
		if tableNodeHasBackground(row.node) && !add(model.columnCount) {
			return count
		}
	}
	for start := 0; start < len(model.rows); {
		group := model.rows[start].group
		end := start + 1
		for end < len(model.rows) && model.rows[end].group == group {
			end++
		}
		if group != nil && tableNodeHasBackground(group) && !add((end-start)*model.columnCount) {
			return count
		}
		start = end
	}
	var addColumnSpec func(tableColumnSpec) bool
	addColumnSpec = func(spec tableColumnSpec) bool {
		if tableNodeHasBackground(spec.node) && !add(spec.span*len(model.rows)) {
			return false
		}
		for _, child := range spec.children {
			if !addColumnSpec(child) {
				return false
			}
		}
		return true
	}
	for _, spec := range model.columnBoxes {
		if !addColumnSpec(spec) {
			return count
		}
	}
	return count
}

func tableNodeHasBackground(node *styledNode) bool {
	if node == nil || node.style.Visibility() != visibilityVisible {
		return false
	}
	_, hasBackground := node.style.Background()
	return hasBackground
}

func tableRowBackgroundRects(x, y float64, columnStarts, columnEnds []float64, rowStart, rowEnd float64) []Rect {
	if rowEnd <= rowStart {
		return nil
	}
	result := make([]Rect, 0, len(columnStarts))
	for column := range columnStarts {
		width := columnEnds[column] - columnStarts[column]
		if width <= 0 {
			continue
		}
		result = append(result, Rect{
			X: x + columnStarts[column], Y: y + rowStart,
			Width: width, Height: rowEnd - rowStart,
		})
	}
	return result
}

func tableCellUsesBaseline(alignment verticalAlignment) bool {
	switch alignment.Mode() {
	case verticalAlignTop, verticalAlignTextTop, verticalAlignMiddle, verticalAlignBottom, verticalAlignTextBottom:
		return false
	default:
		return true
	}
}

func tableCellContentShift(cell tableCellLayout, targetHeight, rowBaseline float64) float64 {
	free := math.Max(0, targetHeight-cell.naturalHeight)
	switch cell.placement.node.style.VerticalAlignment().Mode() {
	case verticalAlignMiddle:
		return free / 2
	case verticalAlignBottom, verticalAlignTextBottom:
		return free
	case verticalAlignTop, verticalAlignTextTop:
		return 0
	default:
		if cell.hasBaseline {
			return clamp(rowBaseline-cell.baseline, 0, free)
		}
		return 0
	}
}

func translateBoxContents(box *Box, deltaX, deltaY float64) {
	if box == nil || (deltaX == 0 && deltaY == 0) {
		return
	}
	for index := range box.Fragments {
		translateInlineFragment(&box.Fragments[index], deltaX, deltaY)
	}
	for index := range box.Text {
		box.Text[index].X += deltaX
		box.Text[index].BaselineY += deltaY
	}
	for index := range box.flow {
		if box.flow[index].box == nil {
			translateInlineFragment(&box.flow[index].fragment, deltaX, deltaY)
		}
	}
	for _, child := range box.Children {
		translateLayoutBox(child, deltaX, deltaY)
	}
}

func tableCellIsEmpty(cell *styledNode) bool {
	if cell == nil || cell.style.Visibility() != visibilityVisible {
		return true
	}
	var visible func(*styledNode, bool) bool
	visible = func(node *styledNode, root bool) bool {
		if node == nil || node.style.Display() == displayNone || node.style.Visibility() != visibilityVisible {
			return false
		}
		if !root && isOutOfFlow(node.style.Position()) {
			return false
		}
		if node.generated && tableTextHasVisibleContent(node.generatedText, node.style.WhiteSpace()) {
			return true
		}
		if node.node != nil {
			switch node.node.Type {
			case dom.TextNode:
				return tableTextHasVisibleContent(node.node.Data, node.style.WhiteSpace())
			case dom.ElementNode:
				if !root {
					// Empty in-flow elements still count as visible content.
					return true
				}
			}
		}
		for _, child := range node.children {
			if visible(child, false) {
				return true
			}
		}
		return false
	}
	return !visible(cell, true)
}

func tableTextHasVisibleContent(source string, mode whiteSpaceMode) bool {
	if source == "" {
		return false
	}
	switch mode {
	case whiteSpacePre, whiteSpacePreWrap, whiteSpaceBreak:
		return true
	case whiteSpacePreLine:
		return strings.TrimSpace(source) != "" || strings.ContainsAny(source, "\n\r\f")
	default:
		return strings.TrimSpace(source) != ""
	}
}

func tableStructuralBox(node *styledNode, bounds Rect) *Box {
	if node == nil {
		return &Box{Bounds: bounds, ContentBounds: bounds}
	}
	return &Box{
		Node: node.node, Pseudo: node.pseudo, Bounds: bounds, ContentBounds: bounds,
		style: node.style, hasStyle: node.node != nil,
	}
}

func sumFloat64(values []float64) float64 {
	total := 0.0
	for _, value := range values {
		total += value
	}
	return total
}
