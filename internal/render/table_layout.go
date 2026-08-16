package render

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/JediWattson/gossamer/internal/dom"
)

const (
	maxTableRows    = 4096
	maxTableColumns = 1024
	maxTableCells   = 16384
	maxTableGridOps = 4_000_000
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
	placement tableCellPlacement
	box       *Box
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
	minimums, preferred, err := context.tableColumnIntrinsicWidths(model, availableWidth)
	if err != nil {
		return intrinsicWidths{}, err
	}
	result := intrinsicWidths{minimum: sumFloat64(minimums), preferred: sumFloat64(preferred)}
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

func (context *layoutContext) tableColumnIntrinsicWidths(model tableModel, availableWidth float64) ([]float64, []float64, error) {
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
		measured, err := context.intrinsicTableCellWidths(placement.node, availableWidth)
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
		distributeTableSpanDeficit(minimums[start:end], entry.measured.minimum)
		distributeTableSpanDeficit(preferred[start:end], entry.measured.preferred)
	}
	for index := range preferred {
		preferred[index] = math.Max(preferred[index], minimums[index])
	}
	return minimums, preferred, nil
}

func (context *layoutContext) intrinsicTableCellWidths(cell *styledNode, availableWidth float64) (intrinsicWidths, error) {
	if cell == nil {
		return intrinsicWidths{}, nil
	}
	style := cell.style
	padding := context.resolvePadding(style, availableWidth)
	border := context.resolveBorder(style, availableWidth)
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
	minimums, preferred, err := context.tableColumnIntrinsicWidths(model, contentWidth)
	if err != nil {
		return 0, 0, err
	}
	columnWidths := distributeTableWidths(minimums, preferred, contentWidth)
	gridWidth := sumFloat64(columnWidths)
	usedWidth := math.Max(contentWidth, gridWidth)
	if len(columnWidths) != 0 && usedWidth > gridWidth {
		columnWidths = distributeTableWidths(minimums, preferred, usedWidth)
		gridWidth = sumFloat64(columnWidths)
	}
	columnOffsets := make([]float64, len(columnWidths)+1)
	for index, width := range columnWidths {
		columnOffsets[index+1] = columnOffsets[index] + width
	}

	cursorY := tableBox.ContentBounds.Y
	for _, caption := range model.captions {
		captionBox, layoutErr := context.layoutBlockSized(caption, tableBox.ContentBounds.X, cursorY, usedWidth, containingHeight, nil, true)
		if layoutErr != nil {
			return 0, 0, layoutErr
		}
		tableBox.Children = append(tableBox.Children, captionBox)
		cursorY = captionBox.Bounds.Y + captionBox.Bounds.Height
	}
	gridY := cursorY

	rowHeights := make([]float64, len(model.rows))
	for index, row := range model.rows {
		if row.node == nil || row.node.style.Height().Unit() == lengthAuto || row.node.style.Height().DependsOnPercent() {
			continue
		}
		rowHeights[index] = math.Max(0, resolveLength(row.node.style.Height(), 0, context.viewport, 0))
	}
	cellLayouts := make([]tableCellLayout, 0, len(model.cells))
	for _, placement := range model.cells {
		spanWidth := columnOffsets[placement.column+placement.columnSpan] - columnOffsets[placement.column]
		padding := context.resolvePadding(placement.node.style, spanWidth)
		border := context.resolveBorder(placement.node.style, spanWidth)
		content := math.Max(0, spanWidth-padding.Left-padding.Right-border.Left-border.Right)
		cellBox, layoutErr := context.layoutBlockSized(
			placement.node,
			tableBox.ContentBounds.X+columnOffsets[placement.column],
			0,
			spanWidth,
			nil,
			&content,
			true,
		)
		if layoutErr != nil {
			return 0, 0, layoutErr
		}
		translateLayoutBox(cellBox, tableBox.ContentBounds.X+columnOffsets[placement.column]-cellBox.Bounds.X, 0)
		if placement.rowSpan == 1 {
			rowHeights[placement.row] = math.Max(rowHeights[placement.row], cellBox.Bounds.Height)
		}
		cellLayouts = append(cellLayouts, tableCellLayout{placement: placement, box: cellBox})
	}
	for _, cell := range cellLayouts {
		if cell.placement.rowSpan == 1 {
			continue
		}
		start := cell.placement.row
		end := start + cell.placement.rowSpan
		deficit := cell.box.Bounds.Height - sumFloat64(rowHeights[start:end])
		if deficit <= 0 {
			continue
		}
		addition := deficit / float64(cell.placement.rowSpan)
		for index := start; index < end; index++ {
			rowHeights[index] += addition
		}
	}
	if containingHeight != nil && len(rowHeights) != 0 {
		captionHeight := gridY - tableBox.ContentBounds.Y
		extra := *containingHeight - captionHeight - sumFloat64(rowHeights)
		if extra > 0 {
			addition := extra / float64(len(rowHeights))
			for index := range rowHeights {
				rowHeights[index] += addition
			}
		}
	}
	rowOffsets := make([]float64, len(rowHeights)+1)
	for index, height := range rowHeights {
		rowOffsets[index+1] = rowOffsets[index] + height
	}
	rowBoxes := make([]*Box, len(model.rows))
	for index, row := range model.rows {
		rowBoxes[index] = tableStructuralBox(row.node, Rect{
			X: tableBox.ContentBounds.X, Y: gridY + rowOffsets[index], Width: gridWidth, Height: rowHeights[index],
		})
	}
	for _, cell := range cellLayouts {
		start := cell.placement.row
		end := start + cell.placement.rowSpan
		targetHeight := rowOffsets[end] - rowOffsets[start]
		if cell.box.Bounds.Height < targetHeight {
			setBoxOuterHeight(cell.box, targetHeight)
		}
		translateLayoutBox(cell.box, 0, gridY+rowOffsets[start]-cell.box.Bounds.Y)
	}

	gridHeight := sumFloat64(rowHeights)
	for _, spec := range model.columnBoxes {
		if columnBox := layoutTableColumnSpec(spec, tableBox.ContentBounds.X, gridY, columnOffsets, gridHeight); columnBox != nil {
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
			X: tableBox.ContentBounds.X, Y: gridY + rowOffsets[index], Width: gridWidth, Height: rowOffsets[end] - rowOffsets[index],
		})
		groupBox.Children = append(groupBox.Children, rowBoxes[index:end]...)
		tableBox.Children = append(tableBox.Children, groupBox)
		index = end
	}
	// CSS table backgrounds layer table -> columns -> row groups -> rows ->
	// cells. Keep cells as the final table children rather than nesting them
	// under rows so a later row background cannot cover a spanning cell.
	for _, cell := range cellLayouts {
		tableBox.Children = append(tableBox.Children, cell.box)
	}
	return usedWidth, cursorY - tableBox.ContentBounds.Y + gridHeight, nil
}

func layoutTableColumnSpec(spec tableColumnSpec, x, y float64, offsets []float64, height float64) *Box {
	if spec.span <= 0 || spec.start < 0 || spec.start+spec.span >= len(offsets) {
		return nil
	}
	box := tableStructuralBox(spec.node, Rect{
		X: x + offsets[spec.start], Y: y, Width: offsets[spec.start+spec.span] - offsets[spec.start], Height: height,
	})
	for _, child := range spec.children {
		if childBox := layoutTableColumnSpec(child, x, y, offsets, height); childBox != nil {
			box.Children = append(box.Children, childBox)
		}
	}
	return box
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
