package render

import (
	"fmt"
	"image/color"
	"math"
	"sort"
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
	captions []*styledNode
	rows     []tableRowRecord
	columns  []*styledNode
	// columnExplicit distinguishes a column box which starts a track from the
	// extra anonymous tracks produced by its span attribute. Tracks carrying a
	// nonzero specified column width are explicit throughout the span. CSS
	// Tables may merge only the remaining anonymous tracks in automatic layout.
	columnExplicit []bool
	columnBoxes    []tableColumnSpec
	columnCount    int
	cells          []tableCellPlacement
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

type tableColumnMeasure struct {
	minimum            float64
	maximum            float64
	nonSpanningMaximum float64
	percentage         float64
	constrained        bool
	originating        bool
}

type tableCellMeasure struct {
	minimum    float64
	maximum    float64
	percentage float64
}

type tableRowHeightMeasure struct {
	base      float64
	reference float64
	auto      bool
}

type tableRowSpanRequirement struct {
	start  int
	span   int
	height float64
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
		model.columnExplicit = append(model.columnExplicit, make([]bool, model.columnCount-len(model.columnExplicit))...)
	}
	if err := mergeAnonymousTableColumns(&model, tableUsesFixedLayout(table)); err != nil {
		return tableModel{}, err
	}
	if err := appendMissingTableCells(&model); err != nil {
		return tableModel{}, err
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
		constrained := tableColumnDefinesTracks(group)
		for offset := range span {
			model.columns = append(model.columns, group)
			model.columnExplicit = append(model.columnExplicit, offset == 0 || constrained)
		}
	}
	spec.span = len(model.columns) - spec.start
	if tableColumnDefinesTracks(group) {
		for index := spec.start; index < spec.start+spec.span; index++ {
			model.columnExplicit[index] = true
		}
	}
	return spec, nil
}

func appendTableColumn(model *tableModel, column *styledNode) (tableColumnSpec, error) {
	span := tableSpan(column.node, "span", 1, maxTableColumns)
	if len(model.columns)+span > maxTableColumns {
		return tableColumnSpec{}, fmt.Errorf("render: table exceeds %d columns", maxTableColumns)
	}
	spec := tableColumnSpec{node: column, start: len(model.columns), span: span}
	constrained := tableColumnDefinesTracks(column)
	for offset := range span {
		model.columns = append(model.columns, column)
		model.columnExplicit = append(model.columnExplicit, offset == 0 || constrained)
	}
	return spec, nil
}

func tableUsesFixedLayout(table *styledNode) bool {
	return table != nil && table.style.TableLayout() == tableLayoutFixed && table.style.Width().Unit() != lengthAuto
}

func tableColumnDefinesTracks(column *styledNode) bool {
	if column == nil {
		return false
	}
	width := column.style.Width()
	return width.Unit() != lengthAuto && !(width.Unit() == lengthPX && width.Value() == 0)
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

// mergeAnonymousTableColumns implements CSS Tables 3 track merging after the
// HTML grid has been dimensioned and before missing cells are synthesized.
// A track is mergeable only when it has no originating cell or explicit
// column definition and it is covered by exactly the same set of spanning
// cells as its predecessor. Fixed layout preserves every column track.
func mergeAnonymousTableColumns(model *tableModel, fixed bool) error {
	if model == nil || fixed || model.columnCount < 2 {
		return nil
	}
	count := model.columnCount
	originating := make([]bool, count)
	wordCount := (len(model.cells) + 63) / 64
	coverage := make([]uint64, count*wordCount)
	operations := 0
	for cellIndex, placement := range model.cells {
		if placement.column >= 0 && placement.column < count {
			originating[placement.column] = true
		}
		if placement.columnSpan <= 1 {
			continue
		}
		end := min(count, placement.column+placement.columnSpan)
		for column := max(0, placement.column); column < end; column++ {
			operations++
			if operations > maxTableGridOps {
				return fmt.Errorf("render: table exceeds %d track merge operations", maxTableGridOps)
			}
			coverage[column*wordCount+cellIndex/64] |= uint64(1) << uint(cellIndex%64)
		}
	}

	mapping := make([]int, count)
	newCount := 1
	for column := 1; column < count; column++ {
		explicit := column < len(model.columnExplicit) && model.columnExplicit[column]
		if !explicit && !originating[column] && tableTrackCoverageEqual(coverage, wordCount, column-1, column) {
			mapping[column] = newCount - 1
			continue
		}
		mapping[column] = newCount
		newCount++
	}
	if newCount == count {
		return nil
	}

	for index := range model.cells {
		placement := &model.cells[index]
		last := min(count-1, placement.column+placement.columnSpan-1)
		placement.column = mapping[placement.column]
		placement.columnSpan = mapping[last] - placement.column + 1
	}
	columns := make([]*styledNode, newCount)
	explicit := make([]bool, newCount)
	for old := 0; old < count; old++ {
		mapped := mapping[old]
		if columns[mapped] == nil && old < len(model.columns) {
			columns[mapped] = model.columns[old]
		}
		if old < len(model.columnExplicit) {
			explicit[mapped] = explicit[mapped] || model.columnExplicit[old]
		}
	}
	for index := range model.columnBoxes {
		remapTableColumnSpec(&model.columnBoxes[index], mapping)
	}
	model.columns = columns
	model.columnExplicit = explicit
	model.columnCount = newCount
	return nil
}

func tableTrackCoverageEqual(coverage []uint64, wordCount, left, right int) bool {
	if wordCount == 0 {
		return true
	}
	leftStart := left * wordCount
	rightStart := right * wordCount
	for word := 0; word < wordCount; word++ {
		if coverage[leftStart+word] != coverage[rightStart+word] {
			return false
		}
	}
	return true
}

func remapTableColumnSpec(spec *tableColumnSpec, mapping []int) {
	if spec == nil || spec.span <= 0 || spec.start < 0 || spec.start >= len(mapping) {
		return
	}
	last := min(len(mapping)-1, spec.start+spec.span-1)
	start := mapping[spec.start]
	spec.start = start
	spec.span = mapping[last] - start + 1
	for index := range spec.children {
		remapTableColumnSpec(&spec.children[index], mapping)
	}
}

// appendMissingTableCells fills every uncovered grid slot with one anonymous
// cell owned by that row. The cells are renderer-private and preserve only the
// inherited style of their row; the DOM and immutable style snapshot remain
// unchanged.
func appendMissingTableCells(model *tableModel) error {
	if model == nil || len(model.rows) == 0 || model.columnCount == 0 {
		return nil
	}
	if len(model.rows) > maxTableGridOps/model.columnCount {
		return fmt.Errorf("render: table exceeds %d missing cell operations", maxTableGridOps)
	}
	occupied := make([]bool, len(model.rows)*model.columnCount)
	operations := 0
	byRow := make([][]tableCellPlacement, len(model.rows))
	for _, placement := range model.cells {
		if placement.row >= 0 && placement.row < len(byRow) {
			byRow[placement.row] = append(byRow[placement.row], placement)
		}
		for row := max(0, placement.row); row < min(len(model.rows), placement.row+placement.rowSpan); row++ {
			for column := max(0, placement.column); column < min(model.columnCount, placement.column+placement.columnSpan); column++ {
				operations++
				if operations > maxTableGridOps {
					return fmt.Errorf("render: table exceeds %d missing cell operations", maxTableGridOps)
				}
				occupied[row*model.columnCount+column] = true
			}
		}
	}

	result := make([]tableCellPlacement, 0, len(model.cells))
	for rowIndex, row := range model.rows {
		result = append(result, byRow[rowIndex]...)
		for column := 0; column < model.columnCount; column++ {
			operations++
			if operations > maxTableGridOps {
				return fmt.Errorf("render: table exceeds %d missing cell operations", maxTableGridOps)
			}
			if occupied[rowIndex*model.columnCount+column] {
				continue
			}
			if len(result) >= maxTableCells {
				return fmt.Errorf("render: table exceeds %d cells after missing-cell fixup", maxTableCells)
			}
			result = append(result, tableCellPlacement{
				node: anonymousTableNode(row.node, displayTableCell, nil),
				row:  rowIndex, column: column, rowSpan: 1, columnSpan: 1,
			})
		}
	}
	model.cells = result
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
	spacing := totalTableSpacing(model.columnCount, horizontalSpacing)
	result := intrinsicWidths{}
	if tableUsesFixedLayout(table) {
		tableWidth := math.Max(0, resolveLength(table.style.Width(), availableWidth, context.viewport, 0))
		if table.style.BoxSizing() == boxSizingBorderBox {
			padding := context.resolvePadding(table.style, availableWidth)
			border := context.resolveBorder(table.style, availableWidth)
			if collapsed != nil {
				padding = Edges{}
				border = collapsed.outerHalfEdges()
			}
			tableWidth = math.Max(0, tableWidth-padding.Left-padding.Right-border.Left-border.Right)
		}
		widths, widthErr := context.fixedTableColumnWidths(model, math.Max(0, tableWidth-spacing), tableWidth, horizontalSpacing, collapsed)
		if widthErr != nil {
			return intrinsicWidths{}, widthErr
		}
		result.minimum = sumFloat64(widths) + spacing
		result.preferred = result.minimum
	} else {
		measures, measureErr := context.tableColumnMeasures(model, availableWidth, horizontalSpacing, collapsed)
		if measureErr != nil {
			return intrinsicWidths{}, measureErr
		}
		for _, measure := range measures {
			result.minimum += measure.minimum
			result.preferred += measure.maximum
		}
		result.minimum += spacing
		result.preferred += spacing
		result.preferred = math.Max(result.preferred, tableIntrinsicPercentagePreferred(measures, spacing, availableWidth, result.minimum))
	}
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

func tableIntrinsicPercentagePreferred(measures []tableColumnMeasure, spacing, availableWidth, minimum float64) float64 {
	hasPercentage := false
	for _, measure := range measures {
		hasPercentage = hasPercentage || measure.percentage > 0
	}
	if !hasPercentage {
		return minimum
	}
	limit := math.Max(minimum, availableWidth)
	target := minimum
	// Each pass can only change whether a percentage track is held by its
	// min-content floor. There are at most len(measures) such transitions; the
	// extra pass proves a stable partition without an unbounded fixed point.
	for range len(measures) + 2 {
		fixed := spacing
		percentage := 0.0
		for _, measure := range measures {
			if measure.percentage <= 0 {
				fixed += measure.maximum
				continue
			}
			if target*measure.percentage/100 < measure.minimum {
				fixed += measure.minimum
				continue
			}
			percentage += measure.percentage / 100
		}
		candidate := fixed
		if percentage >= 1 {
			if fixed > 0 {
				candidate = limit
			}
		} else {
			candidate = fixed / (1 - percentage)
		}
		candidate = clamp(math.Max(minimum, candidate), minimum, limit)
		if math.Abs(candidate-target) < 1e-7 {
			return candidate
		}
		target = candidate
	}
	return target
}

func (context *layoutContext) tableColumnMeasures(model tableModel, availableWidth, horizontalSpacing float64, collapsed *collapsedBorderGrid) ([]tableColumnMeasure, error) {
	measures := make([]tableColumnMeasure, model.columnCount)
	columnNodes, err := tableColumnStyleNodes(model)
	if err != nil {
		return nil, err
	}
	for column, nodes := range columnNodes {
		for _, node := range tableEffectiveColumnNodes(nodes) {
			if node == nil {
				continue
			}
			track := context.tableTrackMeasure(node.style, availableWidth)
			measures[column].minimum = math.Max(measures[column].minimum, track.minimum)
			measures[column].maximum = math.Max(measures[column].maximum, track.maximum)
			measures[column].percentage = math.Max(measures[column].percentage, track.percentage)
			measures[column].constrained = measures[column].constrained || tableWidthConstrainsColumn(node.style.Width())
		}
	}
	for _, placement := range model.cells {
		if placement.column < 0 || placement.column >= len(measures) {
			continue
		}
		measures[placement.column].originating = true
		if placement.columnSpan == 1 && placement.node != nil && tableWidthConstrainsColumn(placement.node.style.Width()) {
			measures[placement.column].constrained = true
		}
	}

	spanning := make([]struct {
		placement tableCellPlacement
		measured  tableCellMeasure
	}, 0)
	operations := 0
	for _, placement := range model.cells {
		operations++
		if operations > maxTableGridOps {
			return nil, fmt.Errorf("render: table exceeds %d sizing operations", maxTableGridOps)
		}
		constrained := placement.columnSpan == 1 && measures[placement.column].constrained
		measured, err := context.tableCellMeasure(placement, collapsed, availableWidth, constrained)
		if err != nil {
			return nil, err
		}
		if placement.columnSpan == 1 {
			column := &measures[placement.column]
			column.minimum = math.Max(column.minimum, measured.minimum)
			column.maximum = math.Max(column.maximum, measured.maximum)
			column.percentage = math.Max(column.percentage, measured.percentage)
			continue
		}
		spanning = append(spanning, struct {
			placement tableCellPlacement
			measured  tableCellMeasure
		}{placement: placement, measured: measured})
	}
	for index := range measures {
		measures[index].maximum = math.Max(measures[index].maximum, measures[index].minimum)
		measures[index].nonSpanningMaximum = measures[index].maximum
	}
	sort.SliceStable(spanning, func(left, right int) bool {
		return spanning[left].placement.columnSpan < spanning[right].placement.columnSpan
	})
	for start := 0; start < len(spanning); {
		end := start + 1
		span := spanning[start].placement.columnSpan
		for end < len(spanning) && spanning[end].placement.columnSpan == span {
			end++
		}
		base := append([]tableColumnMeasure(nil), measures...)
		next := append([]tableColumnMeasure(nil), measures...)
		for _, entry := range spanning[start:end] {
			operations += entry.placement.columnSpan
			if operations > maxTableGridOps {
				return nil, fmt.Errorf("render: table exceeds %d sizing operations", maxTableGridOps)
			}
			applySpanningTableCellMeasure(base, next, entry.placement, entry.measured, horizontalSpacing)
		}
		measures = next
		start = end
	}
	remainingPercentage := 100.0
	for index := range measures {
		measures[index].percentage = clamp(measures[index].percentage, 0, remainingPercentage)
		remainingPercentage -= measures[index].percentage
		measures[index].maximum = math.Max(measures[index].maximum, measures[index].minimum)
	}
	return measures, nil
}

func tableEffectiveColumnNodes(nodes []*styledNode) []*styledNode {
	// A concrete column width overrides its containing colgroup width. When the
	// concrete column remains auto, the group supplies the track constraint.
	// This matches the HTML/CSS table mapping exercised by the column-measure
	// WPTs while still retaining both boxes for paint and border conflict work.
	if len(nodes) > 1 {
		leaf := nodes[len(nodes)-1]
		if leaf != nil && leaf.style.Display() == displayTableColumn && leaf.style.Width().Unit() != lengthAuto {
			return nodes[len(nodes)-1:]
		}
	}
	return nodes
}

func tableColumnStyleNodes(model tableModel) ([][]*styledNode, error) {
	result := make([][]*styledNode, model.columnCount)
	operations := 0
	var appendSpec func(tableColumnSpec) error
	appendSpec = func(spec tableColumnSpec) error {
		start := max(0, spec.start)
		end := min(model.columnCount, spec.start+spec.span)
		for column := start; column < end; column++ {
			operations++
			if operations > maxTableGridOps {
				return fmt.Errorf("render: table exceeds %d column style operations", maxTableGridOps)
			}
			result[column] = append(result[column], spec.node)
		}
		for _, child := range spec.children {
			if err := appendSpec(child); err != nil {
				return err
			}
		}
		return nil
	}
	for _, spec := range model.columnBoxes {
		if err := appendSpec(spec); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func tableWidthConstrainsColumn(value length) bool {
	return value.Unit() != lengthAuto && !value.IsPercent() && !value.DependsOnPercent()
}

func tablePercentageContribution(style computedStyle) float64 {
	percentage := 0.0
	if style.Width().IsPercent() {
		percentage = math.Max(0, style.Width().Value())
	}
	if style.MaxWidth().IsPercent() {
		percentage = math.Min(percentage, math.Max(0, style.MaxWidth().Value()))
	}
	return percentage
}

func (context *layoutContext) tableTrackMeasure(style computedStyle, availableWidth float64) tableCellMeasure {
	contentBox := style.BoxSizing() == boxSizingContentBox
	minimum := tableDefiniteOuterLength(style.MinWidth(), contentBox, 0, availableWidth, context.viewport)
	width := tableDefiniteOuterLength(style.Width(), contentBox, 0, availableWidth, context.viewport)
	maximum := math.Inf(1)
	if style.MaxWidth().Unit() != lengthAuto && !style.MaxWidth().DependsOnPercent() {
		maximum = tableDefiniteOuterLength(style.MaxWidth(), contentBox, 0, availableWidth, context.viewport)
	}
	return tableCellMeasure{
		minimum:    math.Max(minimum, width),
		maximum:    math.Max(minimum, math.Min(maximum, width)),
		percentage: tablePercentageContribution(style),
	}
}

func (context *layoutContext) tableCellMeasure(placement tableCellPlacement, collapsed *collapsedBorderGrid, availableWidth float64, constrained bool) (tableCellMeasure, error) {
	cell := placement.node
	if cell == nil {
		return tableCellMeasure{}, nil
	}
	style := cell.style
	padding := context.resolvePadding(style, availableWidth)
	border := context.resolveBorder(style, availableWidth)
	if collapsed != nil {
		border = collapsed.cellHalfEdges(placement)
	}
	insets := padding.Left + padding.Right + border.Left + border.Right
	content, err := context.intrinsicContentWidths(cell, availableWidth)
	if err != nil {
		return tableCellMeasure{}, err
	}
	contentMinimum := math.Max(0, content.minimum+insets)
	contentMaximum := math.Max(contentMinimum, content.preferred+insets)
	contentBox := style.BoxSizing() == boxSizingContentBox
	minimumWidth := tableDefiniteOuterLength(style.MinWidth(), contentBox, insets, availableWidth, context.viewport)
	width := tableDefiniteOuterLength(style.Width(), contentBox, insets, availableWidth, context.viewport)
	maximumWidth := math.Inf(1)
	if style.MaxWidth().Unit() != lengthAuto && !style.MaxWidth().DependsOnPercent() {
		maximumWidth = tableDefiniteOuterLength(style.MaxWidth(), contentBox, insets, availableWidth, context.viewport)
	}
	outerMinimum := math.Max(minimumWidth, contentMinimum)
	preferredContent := contentMaximum
	if constrained && width > 0 {
		preferredContent = width
	}
	outerMaximum := max(minimumWidth, width, contentMinimum, math.Min(maximumWidth, preferredContent))
	return tableCellMeasure{
		minimum: outerMinimum, maximum: math.Max(outerMinimum, outerMaximum),
		percentage: tablePercentageContribution(style),
	}, nil
}

func tableDefiniteOuterLength(value length, contentBox bool, insets, availableWidth float64, viewport Viewport) float64 {
	if value.Unit() == lengthAuto || value.DependsOnPercent() {
		return 0
	}
	resolved := math.Max(0, resolveLength(value, availableWidth, viewport, 0))
	if contentBox {
		return resolved + insets
	}
	return math.Max(resolved, insets)
}

func applySpanningTableCellMeasure(base, next []tableColumnMeasure, placement tableCellPlacement, cell tableCellMeasure, spacing float64) {
	start := placement.column
	end := min(len(base), placement.column+placement.columnSpan)
	if start < 0 || start >= end {
		return
	}
	internalSpacing := spacing * float64(max(0, end-start-1))
	minimumTarget := math.Max(0, cell.minimum-internalSpacing)
	maximumTarget := math.Max(minimumTarget, cell.maximum-internalSpacing)
	minimumTotal, maximumTotal := 0.0, 0.0
	for column := start; column < end; column++ {
		minimumTotal += base[column].minimum
		maximumTotal += base[column].maximum
	}
	flexible := math.Max(0, maximumTotal-minimumTotal)
	within := clamp(minimumTarget-minimumTotal, 0, flexible)
	above := math.Max(0, minimumTarget-maximumTotal)
	maximumExcess := math.Max(0, maximumTarget-maximumTotal)
	for column := start; column < end; column++ {
		flexShare := 0.0
		if flexible > 0 {
			flexShare = (base[column].maximum - base[column].minimum) / flexible
		}
		maximumShare := tableProportionalShare(base, start, end, column, func(measure tableColumnMeasure) float64 {
			return measure.maximum
		})
		candidateMinimum := base[column].minimum + within*flexShare + above*maximumShare
		candidateMaximum := base[column].maximum + maximumExcess*maximumShare
		next[column].minimum = math.Max(next[column].minimum, candidateMinimum)
		next[column].maximum = max(next[column].maximum, candidateMaximum, next[column].minimum)
	}

	currentPercentage := 0.0
	for column := start; column < end; column++ {
		currentPercentage += base[column].percentage
	}
	remaining := math.Max(0, cell.percentage-currentPercentage)
	if remaining == 0 {
		return
	}
	for column := start; column < end; column++ {
		if base[column].percentage != 0 {
			continue
		}
		share := tableProportionalShare(base, start, end, column, func(measure tableColumnMeasure) float64 {
			if measure.percentage != 0 {
				return -1
			}
			return measure.nonSpanningMaximum
		})
		if share >= 0 {
			next[column].percentage = math.Max(next[column].percentage, remaining*share)
		}
	}
}

func tableProportionalShare(measures []tableColumnMeasure, start, end, column int, weight func(tableColumnMeasure) float64) float64 {
	total := 0.0
	count := 0
	selected := weight(measures[column])
	if selected < 0 {
		return -1
	}
	for index := start; index < end; index++ {
		candidate := weight(measures[index])
		if candidate < 0 {
			continue
		}
		total += candidate
		count++
	}
	if total > 0 {
		return selected / total
	}
	if count == 0 {
		return -1
	}
	return 1 / float64(count)
}

func distributeTableWidths(measures []tableColumnMeasure, target float64) []float64 {
	if len(measures) == 0 {
		return nil
	}
	guesses := [4][]float64{}
	for index := range guesses {
		guesses[index] = make([]float64, len(measures))
	}
	for index, measure := range measures {
		guesses[0][index] = measure.minimum
		percentageWidth := math.Max(measure.minimum, target*measure.percentage/100)
		if measure.percentage > 0 {
			guesses[1][index] = percentageWidth
			guesses[2][index] = percentageWidth
			guesses[3][index] = percentageWidth
		} else {
			guesses[1][index] = measure.minimum
			guesses[2][index] = measure.minimum
			if measure.constrained {
				guesses[2][index] = measure.maximum
			}
			guesses[3][index] = measure.maximum
		}
		for guess := 1; guess < len(guesses); guess++ {
			guesses[guess][index] = math.Max(guesses[guess][index], guesses[guess-1][index])
		}
	}
	target = math.Max(target, sumFloat64(guesses[0]))
	for guess := 1; guess < len(guesses); guess++ {
		upperTotal := sumFloat64(guesses[guess])
		if target <= upperTotal {
			return interpolateTableWidthGuesses(guesses[guess-1], guesses[guess], target)
		}
	}
	widths := append([]float64(nil), guesses[3]...)
	distributeTableExcess(widths, measures, math.Max(0, target-sumFloat64(widths)))
	return widths
}

func interpolateTableWidthGuesses(lower, upper []float64, target float64) []float64 {
	result := append([]float64(nil), lower...)
	lowerTotal, upperTotal := sumFloat64(lower), sumFloat64(upper)
	if upperTotal <= lowerTotal {
		return append(result[:0], upper...)
	}
	ratio := clamp((target-lowerTotal)/(upperTotal-lowerTotal), 0, 1)
	for index := range result {
		result[index] += (upper[index] - lower[index]) * ratio
	}
	return result
}

func distributeTableExcess(widths []float64, measures []tableColumnMeasure, excess float64) {
	if excess <= 0 || len(widths) == 0 {
		return
	}
	type candidateGroup struct {
		matches func(tableColumnMeasure) bool
		weight  func(tableColumnMeasure) float64
	}
	groups := []candidateGroup{
		{func(m tableColumnMeasure) bool {
			return !m.constrained && m.percentage == 0 && m.originating && m.maximum > 0
		}, func(m tableColumnMeasure) float64 { return m.maximum }},
		{func(m tableColumnMeasure) bool { return !m.constrained && m.percentage == 0 && m.originating }, func(tableColumnMeasure) float64 { return 1 }},
		{func(m tableColumnMeasure) bool { return m.constrained && m.percentage == 0 && m.maximum > 0 }, func(m tableColumnMeasure) float64 { return m.maximum }},
		{func(m tableColumnMeasure) bool { return m.percentage > 0 }, func(m tableColumnMeasure) float64 { return m.percentage }},
		{func(m tableColumnMeasure) bool { return m.originating }, func(tableColumnMeasure) float64 { return 1 }},
		{func(tableColumnMeasure) bool { return true }, func(tableColumnMeasure) float64 { return 1 }},
	}
	for _, group := range groups {
		total := 0.0
		for _, measure := range measures {
			if group.matches(measure) {
				total += group.weight(measure)
			}
		}
		if total <= 0 {
			continue
		}
		for index, measure := range measures {
			if group.matches(measure) {
				widths[index] += excess * group.weight(measure) / total
			}
		}
		return
	}
}

func (context *layoutContext) tableSpecifiedOuterHeight(style computedStyle, percentageBase, verticalInsets float64, resolvePercent bool) (float64, bool) {
	value := style.Height()
	if value.Unit() == lengthAuto || value.DependsOnPercent() && !resolvePercent {
		return 0, false
	}
	resolved, ok := value.Resolve(percentageBase, float64(context.viewport.Width), float64(context.viewport.Height))
	if !ok {
		return 0, false
	}
	resolved = math.Max(0, resolved)
	if style.BoxSizing() == boxSizingBorderBox {
		return math.Max(resolved, verticalInsets), true
	}
	return resolved + verticalInsets, true
}

func (context *layoutContext) tableRowSpecifiedHeight(row *styledNode, percentageBase float64, resolvePercent bool) (float64, bool) {
	if row == nil {
		return 0, false
	}
	value := row.style.Height()
	if value.Unit() == lengthAuto || value.DependsOnPercent() && !resolvePercent {
		return 0, false
	}
	resolved, ok := value.Resolve(percentageBase, float64(context.viewport.Width), float64(context.viewport.Height))
	return math.Max(0, resolved), ok
}

func applyTableRowSpanRequirements(values []float64, requirements []tableRowSpanRequirement, spacing float64) ([]float64, error) {
	if len(requirements) == 0 {
		return values, nil
	}
	sort.SliceStable(requirements, func(left, right int) bool {
		return requirements[left].span < requirements[right].span
	})
	operations := 0
	for groupStart := 0; groupStart < len(requirements); {
		groupEnd := groupStart + 1
		span := requirements[groupStart].span
		for groupEnd < len(requirements) && requirements[groupEnd].span == span {
			groupEnd++
		}
		base := append([]float64(nil), values...)
		next := append([]float64(nil), values...)
		for _, requirement := range requirements[groupStart:groupEnd] {
			start := max(0, requirement.start)
			end := min(len(base), requirement.start+requirement.span)
			if start >= end {
				continue
			}
			operations += end - start
			if operations > maxTableGridOps {
				return nil, fmt.Errorf("render: table exceeds %d row sizing operations", maxTableGridOps)
			}
			occupied := sumFloat64(base[start:end]) + spacing*float64(max(0, end-start-1))
			deficit := requirement.height - occupied
			if deficit <= 0 {
				continue
			}
			addition := deficit / float64(end-start)
			for index := start; index < end; index++ {
				next[index] = math.Max(next[index], base[index]+addition)
			}
		}
		values = next
		groupStart = groupEnd
	}
	return values, nil
}

func distributeTableHeights(measures []tableRowHeightMeasure, target float64) []float64 {
	if len(measures) == 0 {
		return nil
	}
	baseTotal, referenceTotal := 0.0, 0.0
	for index := range measures {
		measures[index].base = math.Max(0, measures[index].base)
		measures[index].reference = math.Max(measures[index].base, measures[index].reference)
		baseTotal += measures[index].base
		referenceTotal += measures[index].reference
	}
	target = math.Max(target, baseTotal)
	result := make([]float64, len(measures))
	if target <= referenceTotal {
		ratio := 0.0
		if referenceTotal > baseTotal {
			ratio = clamp((target-baseTotal)/(referenceTotal-baseTotal), 0, 1)
		}
		for index, measure := range measures {
			result[index] = measure.base + (measure.reference-measure.base)*ratio
		}
		return result
	}
	for index, measure := range measures {
		result[index] = measure.reference
	}
	extra := target - referenceTotal
	autoRows := 0
	for _, measure := range measures {
		if measure.auto {
			autoRows++
		}
	}
	if autoRows != 0 {
		addition := extra / float64(autoRows)
		for index, measure := range measures {
			if measure.auto {
				result[index] += addition
			}
		}
		return result
	}
	addition := extra / float64(len(result))
	for index := range result {
		result[index] += addition
	}
	return result
}

func tableCellRequiresPercentageRelayout(table, cell *styledNode) bool {
	if table != nil && table.style.Height().Unit() != lengthAuto {
		return true
	}
	if cell == nil {
		return false
	}
	height := cell.style.Height()
	return height.Unit() != lengthAuto && !height.DependsOnPercent()
}

func (context *layoutContext) layoutTableContainer(
	table *styledNode,
	wrapper, tableBox *Box,
	contentWidth float64,
	tableContentHeight, containingHeight *float64,
	specifiedContentHeight float64,
	hasDefiniteHeight bool,
	verticalInsets float64,
) (float64, error) {
	model, err := buildTableModel(table)
	if err != nil {
		return 0, err
	}
	collapsed, err := context.resolveCollapsedTableBorders(table, model)
	if err != nil {
		return 0, err
	}
	horizontalSpacing, verticalSpacing := tableBorderSpacing(table.style, context.viewport)
	collapsedColumns := tableCollapsedColumns(model)
	collapsedRows := tableCollapsedRows(model)
	tableBox.tableRowsCollapsed = tableHasCollapsedTrack(collapsedRows)
	clipStructuralBackgrounds := horizontalSpacing > 0 || verticalSpacing > 0
	if clipStructuralBackgrounds {
		if count := tableStructuralBackgroundRectCount(model); count > maxTableBackgroundRects {
			return 0, fmt.Errorf("render: table exceeds %d structural background rectangles", maxTableBackgroundRects)
		}
	}
	spacingWidth := totalTableSpacing(model.columnCount, horizontalSpacing)
	assignableWidth := math.Max(0, contentWidth-spacingWidth)

	var columnWidths []float64
	fixed := tableUsesFixedLayout(table)
	if fixed {
		columnWidths, err = context.fixedTableColumnWidths(model, assignableWidth, contentWidth, horizontalSpacing, collapsed)
	} else {
		var measures []tableColumnMeasure
		measures, err = context.tableColumnMeasures(model, contentWidth, horizontalSpacing, collapsed)
		if err == nil {
			columnWidths = distributeTableWidths(measures, assignableWidth)
			gridWidth := sumFloat64(columnWidths) + spacingWidth
			usedWidth := math.Max(contentWidth, gridWidth)
			if len(columnWidths) != 0 && usedWidth > gridWidth {
				columnWidths = distributeTableWidths(measures, math.Max(0, usedWidth-spacingWidth))
			}
		}
	}
	if err != nil {
		return 0, err
	}
	logicalColumnStarts, logicalColumnEnds, _ := tableTrackGeometry(columnWidths, horizontalSpacing, nil)
	columnStarts, columnEnds, gridWidth := tableTrackGeometry(columnWidths, horizontalSpacing, collapsedColumns)
	usedWidth := math.Max(contentWidth, gridWidth)
	if tableHasCollapsedTrack(collapsedColumns) {
		usedWidth = gridWidth
	}
	tableBox.ContentBounds.Width = usedWidth
	tableBox.Bounds.Width = tableBox.Border.Left + tableBox.Padding.Left + usedWidth + tableBox.Padding.Right + tableBox.Border.Right
	wrapper.Bounds.Width = tableBox.Bounds.Width
	wrapper.ContentBounds = tableBox.ContentBounds

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
		pending := marginStrut{}
		for _, caption := range captions {
			pending = pending.add(resolveLength(caption.style.MarginTop(), wrapper.Bounds.Width, context.viewport, 0))
			cursor += pending.value()
			captionBox, layoutErr := context.layoutBlockSized(caption, wrapper.Bounds.X, cursor, wrapper.Bounds.Width, containingHeight, nil, true)
			if layoutErr != nil {
				return nil, 0, layoutErr
			}
			boxes = append(boxes, captionBox)
			cursor = captionBox.Bounds.Y + captionBox.Bounds.Height
			pending = marginStrut{}.add(resolveLength(caption.style.MarginBottom(), wrapper.Bounds.Width, context.viewport, 0))
		}
		cursor += pending.value()
		return boxes, cursor - startY, nil
	}
	topBoxes, topCaptionHeight, err := layoutCaptions(topCaptions, wrapper.Bounds.Y)
	if err != nil {
		return 0, err
	}
	tableBox.Bounds.Y = wrapper.Bounds.Y + topCaptionHeight
	tableBox.ContentBounds.X = tableBox.Bounds.X + tableBox.Border.Left + tableBox.Padding.Left
	tableBox.ContentBounds.Y = tableBox.Bounds.Y + tableBox.Border.Top + tableBox.Padding.Top
	gridY := tableBox.ContentBounds.Y

	rowMeasures := make([]tableRowHeightMeasure, len(model.rows))
	for index, row := range model.rows {
		rowMeasures[index].auto = row.node == nil || row.node.style.Height().Unit() == lengthAuto
		if specified, ok := context.tableRowSpecifiedHeight(row.node, 0, false); ok {
			rowMeasures[index].base = math.Max(rowMeasures[index].base, specified)
		}
	}
	layoutCell := func(placement tableCellPlacement, childContainingHeight *float64) (tableCellLayout, error) {
		spanWidth := tableTrackSpan(logicalColumnStarts, logicalColumnEnds, placement.column, placement.columnSpan)
		usedSpanWidth := tableTrackSpan(columnStarts, columnEnds, placement.column, placement.columnSpan)
		padding := context.resolvePadding(placement.node.style, spanWidth)
		border := context.resolveBorder(placement.node.style, spanWidth)
		if collapsed != nil {
			border = collapsed.cellHalfEdges(placement)
		}
		content := math.Max(0, spanWidth-padding.Left-padding.Right-border.Left-border.Right)
		cellX := tableBox.ContentBounds.X + columnStarts[placement.column]
		cellBox, layoutErr := context.layoutBlockSizedWithSubgrid(
			placement.node, cellX, 0, spanWidth, nil, &content, true, nil,
			blockLayoutOverrides{
				ignoreSpecifiedHeight: true, childContainingHeight: childContainingHeight,
				tableCellFirstPass: childContainingHeight == nil,
			},
		)
		if layoutErr != nil {
			return tableCellLayout{}, layoutErr
		}
		if collapsed != nil {
			adjustCollapsedTableCellBox(cellBox, cellX, usedSpanWidth, padding, border)
			cellBox.suppressBorders = true
		} else {
			adjustTableCellBox(cellBox, cellX, usedSpanWidth, padding, border)
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
		return layout, nil
	}
	cellLayouts := make([]tableCellLayout, 0, len(model.cells))
	rowBaselines := make([]float64, len(model.rows))
	baseSpans := make([]tableRowSpanRequirement, 0, len(model.cells))
	for _, placement := range model.cells {
		layout, layoutErr := layoutCell(placement, nil)
		if layoutErr != nil {
			return 0, layoutErr
		}
		for row := placement.row; row < min(len(rowMeasures), placement.row+placement.rowSpan); row++ {
			if placement.node.style.Height().Unit() != lengthAuto {
				rowMeasures[row].auto = false
			}
		}
		verticalInsets := layout.box.Padding.Top + layout.box.Padding.Bottom + layout.box.Border.Top + layout.box.Border.Bottom
		specified, _ := context.tableSpecifiedOuterHeight(placement.node.style, 0, verticalInsets, false)
		required := math.Max(layout.naturalHeight, specified)
		if placement.rowSpan == 1 {
			rowMeasures[placement.row].base = math.Max(rowMeasures[placement.row].base, required)
			if tableCellUsesBaseline(placement.node.style.VerticalAlignment()) && layout.hasBaseline {
				rowBaselines[placement.row] = math.Max(rowBaselines[placement.row], layout.baseline)
			}
		} else {
			baseSpans = append(baseSpans, tableRowSpanRequirement{start: placement.row, span: placement.rowSpan, height: required})
		}
		cellLayouts = append(cellLayouts, layout)
	}
	for _, cell := range cellLayouts {
		if cell.placement.rowSpan != 1 || !tableCellUsesBaseline(cell.placement.node.style.VerticalAlignment()) || !cell.hasBaseline {
			continue
		}
		required := rowBaselines[cell.placement.row] + cell.naturalHeight - cell.baseline
		rowMeasures[cell.placement.row].base = math.Max(rowMeasures[cell.placement.row].base, required)
	}
	baseHeights := make([]float64, len(rowMeasures))
	for index := range rowMeasures {
		baseHeights[index] = rowMeasures[index].base
	}
	baseHeights, err = applyTableRowSpanRequirements(baseHeights, baseSpans, verticalSpacing)
	if err != nil {
		return 0, err
	}
	for index := range rowMeasures {
		rowMeasures[index].base = baseHeights[index]
		rowMeasures[index].reference = baseHeights[index]
	}
	spacingHeight := totalTableSpacing(len(rowMeasures), verticalSpacing)
	targetRowsHeight := sumFloat64(baseHeights)
	if tableContentHeight != nil {
		// The table height sizes the table-root grid; captions occupy the
		// anonymous wrapper and do not consume that specified height.
		targetRowsHeight = math.Max(targetRowsHeight, *tableContentHeight-spacingHeight)
	}
	for index, row := range model.rows {
		if specified, ok := context.tableRowSpecifiedHeight(row.node, targetRowsHeight, true); ok {
			rowMeasures[index].reference = math.Max(rowMeasures[index].reference, specified)
		}
	}
	referenceSpans := make([]tableRowSpanRequirement, 0, len(cellLayouts))
	for _, cell := range cellLayouts {
		verticalInsets := cell.box.Padding.Top + cell.box.Padding.Bottom + cell.box.Border.Top + cell.box.Border.Bottom
		specified, _ := context.tableSpecifiedOuterHeight(cell.placement.node.style, targetRowsHeight, verticalInsets, true)
		required := math.Max(cell.naturalHeight, specified)
		if cell.placement.rowSpan == 1 {
			rowMeasures[cell.placement.row].reference = math.Max(rowMeasures[cell.placement.row].reference, required)
		} else {
			referenceSpans = append(referenceSpans, tableRowSpanRequirement{start: cell.placement.row, span: cell.placement.rowSpan, height: required})
		}
	}
	referenceHeights := make([]float64, len(rowMeasures))
	for index := range rowMeasures {
		referenceHeights[index] = rowMeasures[index].reference
	}
	referenceHeights, err = applyTableRowSpanRequirements(referenceHeights, referenceSpans, verticalSpacing)
	if err != nil {
		return 0, err
	}
	for index := range rowMeasures {
		rowMeasures[index].reference = referenceHeights[index]
	}
	rowHeights := distributeTableHeights(rowMeasures, targetRowsHeight)
	logicalRowStarts, logicalRowEnds, _ := tableTrackGeometry(rowHeights, verticalSpacing, nil)
	for index := range cellLayouts {
		cell := &cellLayouts[index]
		if !tableCellRequiresPercentageRelayout(table, cell.placement.node) {
			continue
		}
		logicalHeight := tableTrackSpan(logicalRowStarts, logicalRowEnds, cell.placement.row, cell.placement.rowSpan)
		verticalInsets := cell.box.Padding.Top + cell.box.Padding.Bottom + cell.box.Border.Top + cell.box.Border.Bottom
		childContainingHeight := math.Max(0, logicalHeight-verticalInsets)
		relayout, layoutErr := layoutCell(cell.placement, &childContainingHeight)
		if layoutErr != nil {
			return 0, layoutErr
		}
		// Height distribution does not change the row baseline established by
		// the first pass, even though descendants now receive their final
		// percentage base.
		relayout.baseline = cell.baseline
		relayout.hasBaseline = cell.hasBaseline
		*cell = relayout
	}
	rowStarts, rowEnds, gridHeight := tableTrackGeometry(rowHeights, verticalSpacing, collapsedRows)
	rowBoxes := make([]*Box, len(model.rows))
	for index, row := range model.rows {
		rowBoxes[index] = tableStructuralBox(row.node, Rect{
			X: tableBox.ContentBounds.X, Y: gridY + rowStarts[index], Width: gridWidth, Height: rowEnds[index] - rowStarts[index],
		})
		if row.node != nil {
			rowBoxes[index].percentHeightResolved = row.node.style.Height().DependsOnPercent()
		}
		if clipStructuralBackgrounds && tableNodeHasBackground(row.node) {
			rowBoxes[index].backgroundRects = tableRowBackgroundRects(tableBox.ContentBounds.X, gridY, columnStarts, columnEnds, rowStarts[index], rowEnds[index])
		}
	}
	for _, cell := range cellLayouts {
		start := cell.placement.row
		logicalHeight := tableTrackSpan(logicalRowStarts, logicalRowEnds, start, cell.placement.rowSpan)
		usedHeight := tableTrackSpan(rowStarts, rowEnds, start, cell.placement.rowSpan)
		setBoxOuterHeight(cell.box, logicalHeight)
		translateLayoutBox(cell.box, 0, gridY+rowStarts[start]-cell.box.Bounds.Y)
		shift := tableCellContentShift(cell, logicalHeight, rowBaselines[start])
		translateBoxContents(cell.box, 0, shift)
		setBoxOuterHeight(cell.box, usedHeight)
		cell.box.percentHeightResolved = cell.placement.node.style.Height().DependsOnPercent()
		if tableSpanTouchesCollapsed(collapsedColumns, cell.placement.column, cell.placement.columnSpan) ||
			tableSpanTouchesCollapsed(collapsedRows, cell.placement.row, cell.placement.rowSpan) {
			cell.box.hasClipBounds = true
			cell.box.clipBounds = cell.box.Bounds
		}
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
	rootContentHeight := gridHeight
	if hasDefiniteHeight && !tableBox.tableRowsCollapsed {
		rootContentHeight = specifiedContentHeight
	}
	if !tableBox.tableRowsCollapsed {
		rootContentHeight = context.constrainHeight(table.style, rootContentHeight, verticalInsets, containingHeight)
	}
	tableBox.ContentBounds.Height = rootContentHeight
	tableBox.Bounds.Height = tableBox.Border.Top + tableBox.Padding.Top + rootContentHeight + tableBox.Padding.Bottom + tableBox.Border.Bottom

	bottomBoxes, bottomCaptionHeight, err := layoutCaptions(bottomCaptions, tableBox.Bounds.Y+tableBox.Bounds.Height)
	if err != nil {
		return 0, err
	}
	wrapper.Bounds.Height = topCaptionHeight + tableBox.Bounds.Height + bottomCaptionHeight
	wrapper.ContentBounds = tableBox.ContentBounds
	// Paint the table-root background and internal layers before captions while
	// retaining captions as wrapper siblings rather than table-root children.
	wrapper.Children = append(wrapper.Children, tableBox)
	wrapper.Children = append(wrapper.Children, topBoxes...)
	wrapper.Children = append(wrapper.Children, bottomBoxes...)
	wrapper.tableClientRects = append(wrapper.tableClientRects, tableBox.Bounds)
	captionBoxes := make(map[*styledNode]*Box, len(model.captions))
	for index, caption := range topCaptions {
		captionBoxes[caption] = topBoxes[index]
	}
	for index, caption := range bottomCaptions {
		captionBoxes[caption] = bottomBoxes[index]
	}
	for _, caption := range model.captions {
		if captionBox := captionBoxes[caption]; captionBox != nil {
			wrapper.tableClientRects = append(wrapper.tableClientRects, captionBox.Bounds)
		}
	}
	if collapsed != nil && collapsed.rows != 0 && collapsed.columns != 0 {
		tableBox.suppressBorders = true
		tableBox.afterPaint = collapsed.paintRects(tableBox.ContentBounds.X, gridY, columnStarts, columnEnds, rowStarts, rowEnds)
	}
	return usedWidth, nil
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

func tableTrackGeometry(widths []float64, spacing float64, collapsed []bool) ([]float64, []float64, float64) {
	starts := make([]float64, len(widths))
	ends := make([]float64, len(widths))
	if tableVisibleTrackCount(widths, collapsed) == 0 {
		return starts, ends, 0
	}
	cursor := spacing
	for index, width := range widths {
		if tableTrackCollapsed(collapsed, len(widths), index) {
			starts[index], ends[index] = cursor, cursor
			continue
		}
		starts[index] = cursor
		ends[index] = cursor + math.Max(0, width)
		cursor = ends[index] + spacing
	}
	return starts, ends, cursor
}

func tableCollapsedRows(model tableModel) []bool {
	result := make([]bool, len(model.rows))
	for index, row := range model.rows {
		result[index] = row.node != nil && row.node.style.Visibility() == visibilityCollapse ||
			row.group != nil && row.group.style.Visibility() == visibilityCollapse
	}
	return result
}

func tableCollapsedColumns(model tableModel) []bool {
	result := make([]bool, model.columnCount)
	var mark func(tableColumnSpec, bool)
	mark = func(spec tableColumnSpec, inherited bool) {
		collapsed := inherited || spec.node != nil && spec.node.style.Visibility() == visibilityCollapse
		if collapsed {
			for column := max(0, spec.start); column < min(len(result), spec.start+spec.span); column++ {
				result[column] = true
			}
		}
		for _, child := range spec.children {
			mark(child, collapsed)
		}
	}
	for _, spec := range model.columnBoxes {
		mark(spec, false)
	}
	return result
}

func tableTrackCollapsed(collapsed []bool, count, index int) bool {
	return len(collapsed) == count && index >= 0 && index < count && collapsed[index]
}

func tableHasCollapsedTrack(collapsed []bool) bool {
	for _, value := range collapsed {
		if value {
			return true
		}
	}
	return false
}

func tableVisibleTrackCount(widths []float64, collapsed []bool) int {
	count := 0
	for index := range widths {
		if !tableTrackCollapsed(collapsed, len(widths), index) {
			count++
		}
	}
	return count
}

func tableSpanTouchesCollapsed(collapsed []bool, start, span int) bool {
	if span <= 0 || start < 0 {
		return false
	}
	for index := start; index < min(len(collapsed), start+span); index++ {
		if collapsed[index] {
			return true
		}
	}
	return false
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
	percentageAssigned := make([]bool, model.columnCount)
	columnNodes, err := tableColumnStyleNodes(model)
	if err != nil {
		return nil, err
	}
	for index, nodes := range columnNodes {
		for _, column := range tableEffectiveColumnNodes(nodes) {
			if index >= len(widths) || column == nil || column.style.Width().Unit() == lengthAuto ||
				column.style.Width().DependsOnPercent() && !column.style.Width().IsPercent() {
				continue
			}
			width := resolveLength(column.style.Width(), tableWidth, context.viewport, 0)
			if !isFinite(width) || width < 0 {
				continue
			}
			if !assigned[index] || width > widths[index] {
				widths[index], assigned[index] = width, true
				percentageAssigned[index] = column.style.Width().IsPercent()
			}
		}
	}
	operations := 0
	for _, placement := range model.cells {
		if placement.row != 0 || placement.node == nil || placement.node.style.Width().Unit() == lengthAuto ||
			placement.node.style.Width().DependsOnPercent() && !placement.node.style.Width().IsPercent() {
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
		if placement.node.style.BoxSizing() == boxSizingContentBox && !placement.node.style.Width().IsPercent() {
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
			percentageAssigned[column] = placement.node.style.Width().IsPercent()
		}
	}
	// Percentage tracks are resolved after definite-length tracks. If their
	// requested widths over-subscribe the remaining assignable width, normalize
	// them proportionally instead of allowing a 160% declaration to recursively
	// inflate the table's own fixed width.
	pixelTotal, percentageTotal := 0.0, 0.0
	for index, width := range widths {
		if !assigned[index] {
			continue
		}
		if percentageAssigned[index] {
			percentageTotal += width
		} else {
			pixelTotal += width
		}
	}
	percentageAvailable := math.Max(0, assignableWidth-pixelTotal)
	if percentageTotal > percentageAvailable && percentageTotal > 0 {
		scale := percentageAvailable / percentageTotal
		for index := range widths {
			if percentageAssigned[index] {
				widths[index] *= scale
			}
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
		weightTotal := 0.0
		for index, width := range widths {
			if assigned[index] && !percentageAssigned[index] && width > 0 {
				weightTotal += width
			}
		}
		usePercentages := weightTotal == 0
		if usePercentages {
			for index, width := range widths {
				if assigned[index] && percentageAssigned[index] && width > 0 {
					weightTotal += width
				}
			}
		}
		if weightTotal == 0 {
			share := remaining / float64(len(widths))
			for index := range widths {
				widths[index] += share
			}
		} else {
			for index, width := range widths {
				eligible := assigned[index] && width > 0 && ((!usePercentages && !percentageAssigned[index]) || (usePercentages && percentageAssigned[index]))
				if eligible {
					widths[index] += remaining * width / weightTotal
				}
			}
		}
	}
	return widths, nil
}

func adjustCollapsedTableCellBox(box *Box, x, width float64, padding, border Edges) {
	adjustTableCellBox(box, x, width, padding, border)
}

func adjustTableCellBox(box *Box, x, width float64, padding, border Edges) {
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
