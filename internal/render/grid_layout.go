package render

import (
	"fmt"
	"math"
	"sort"

	"github.com/JediWattson/gossamer/internal/dom"
	computed "github.com/JediWattson/gossamer/internal/style"
)

const (
	maxGridItems         = 100_000
	maxGridTracksPerAxis = 1024
	maxGridOccupiedCells = 1_000_000
	maxGridPlacementOps  = 4_000_000
)

type gridAxisPlacement struct {
	definite bool
	start    int
	span     int
}

type gridLayoutItem struct {
	node          *styledNode
	originalIndex int
	column        gridAxisPlacement
	row           gridAxisPlacement
	box           *Box
	marginTop     float64
	marginRight   float64
	marginBottom  float64
	marginLeft    float64
}

type gridAxisDefinition struct {
	template       computed.GridTrackList
	explicitTracks int
	areaLineNames  [][]string
	subgridNames   [][]string
	autoFitStart   int
	autoFitEnd     int
}

type gridSubgridAxisContext struct {
	starts    []float64
	ends      []float64
	lineNames [][]string
	edgeStart float64
	edgeEnd   float64
}

type gridSubgridContext struct {
	columns *gridSubgridAxisContext
	rows    *gridSubgridAxisContext
}

func gridAxisDefinitionFromTemplate(template computed.GridTrackList) gridAxisDefinition {
	definition := gridAxisDefinition{template: template, explicitTracks: template.Len()}
	if template.AutoRepeatKind() == computed.GridAutoRepeatFit {
		definition.autoFitStart, definition.autoFitEnd, _ = template.AutoRepeatRange()
	}
	return definition
}

func gridAxisDefinitionWithAreas(template computed.GridTrackList, areas computed.GridTemplateAreas, rows bool) gridAxisDefinition {
	explicitTracks := areas.Columns()
	if rows {
		explicitTracks = areas.Rows()
	}
	explicitTracks = max(explicitTracks, template.Len())
	definition := gridAxisDefinition{
		template:       template,
		explicitTracks: explicitTracks,
		areaLineNames:  make([][]string, explicitTracks+1),
	}
	if template.AutoRepeatKind() == computed.GridAutoRepeatFit {
		definition.autoFitStart, definition.autoFitEnd, _ = template.AutoRepeatRange()
	}
	for line := range definition.areaLineNames {
		if rows {
			definition.areaLineNames[line] = areas.RowLineNames(line)
		} else {
			definition.areaLineNames[line] = areas.ColumnLineNames(line)
		}
	}
	return definition
}

func gridAxisDefinitionForSubgrid(template computed.GridTrackList, areas computed.GridTemplateAreas, axis *gridSubgridAxisContext, rows bool) gridAxisDefinition {
	explicitTracks := 0
	if axis != nil {
		explicitTracks = min(len(axis.starts), len(axis.ends))
	}
	definition := gridAxisDefinition{
		template:       template,
		explicitTracks: explicitTracks,
		areaLineNames:  make([][]string, explicitTracks+1),
		subgridNames:   make([][]string, explicitTracks+1),
	}
	local := template.ResolvedSubgridLineNames(explicitTracks + 1)
	for line := 0; line <= explicitTracks; line++ {
		if axis != nil && line < len(axis.lineNames) {
			definition.subgridNames[line] = append(definition.subgridNames[line], axis.lineNames[line]...)
		}
		if line < len(local) {
			definition.subgridNames[line] = append(definition.subgridNames[line], local[line]...)
		}
		if rows {
			definition.areaLineNames[line] = areas.RowLineNames(line)
		} else {
			definition.areaLineNames[line] = areas.ColumnLineNames(line)
		}
	}
	return definition
}

func (definition gridAxisDefinition) lineHasName(line int, name string) bool {
	if line < 0 || line > definition.explicitTracks {
		return false
	}
	for _, candidate := range definition.template.LineNames(line) {
		if candidate == name {
			return true
		}
	}
	if line < len(definition.areaLineNames) {
		for _, candidate := range definition.areaLineNames[line] {
			if candidate == name {
				return true
			}
		}
	}
	if line < len(definition.subgridNames) {
		for _, candidate := range definition.subgridNames[line] {
			if candidate == name {
				return true
			}
		}
	}
	return false
}

func resolveGridAutoRepeat(template computed.GridTrackList, available *float64, fulfillMinimum bool, gap float64, viewport Viewport) computed.GridTrackList {
	start, end, ok := template.AutoRepeatRange()
	if !ok || available == nil || end <= start {
		return template
	}
	patternCount := end - start
	fixedCount := template.Len() - patternCount
	maximum := (maxGridTracksPerAxis - fixedCount) / patternCount
	if maximum < 1 {
		maximum = 1
	}
	fixedSize := 0.0
	patternSize := 0.0
	for index := 0; index < template.Len(); index++ {
		size, definite := gridAutoRepeatTrackSize(template, index, *available, viewport)
		if !definite {
			return template
		}
		if index >= start && index < end {
			patternSize += size
		} else {
			fixedSize += size
		}
	}
	total := func(count int) float64 {
		trackCount := fixedCount + count*patternCount
		return fixedSize + float64(count)*patternSize + gap*float64(max(0, trackCount-1))
	}
	denominator := patternSize + gap*float64(patternCount)
	numerator := *available - fixedSize - gap*float64(fixedCount-1)
	count := 1
	if denominator > 0 {
		if fulfillMinimum {
			count = int(math.Ceil(numerator / denominator))
		} else {
			count = int(math.Floor(numerator / denominator))
		}
	}
	count = min(max(1, count), maximum)
	if fulfillMinimum {
		for count > 1 && total(count-1) >= *available {
			count--
		}
		for count < maximum && total(count) < *available {
			count++
		}
	} else {
		for count > 1 && total(count) > *available {
			count--
		}
		for count < maximum && total(count+1) <= *available {
			count++
		}
	}
	if expanded, expandedOK := template.ExpandAutoRepeat(count); expandedOK {
		return expanded
	}
	low, high := 1, count
	for low < high {
		middle := low + (high-low+1)/2
		if _, expandedOK := template.ExpandAutoRepeat(middle); expandedOK {
			low = middle
		} else {
			high = middle - 1
		}
	}
	expanded, _ := template.ExpandAutoRepeat(low)
	return expanded
}

func gridAutoRepeatTrackSize(template computed.GridTrackList, index int, available float64, viewport Viewport) (float64, bool) {
	track, ok := template.At(index)
	if !ok {
		return 0, false
	}
	minimum, minimumOK := definiteGridTrackBreadth(track.MinKind(), track.MinLength(), available, viewport)
	maximum, maximumOK := definiteGridTrackBreadth(track.MaxKind(), track.MaxLength(), available, viewport)
	if !minimumOK && !maximumOK {
		return 0, false
	}
	size := minimum
	if maximumOK {
		size = maximum
		if minimumOK {
			size = math.Max(size, minimum)
		}
	}
	// The repeat-to-fill divisor is floored to one CSS pixel so zero-sized
	// tracks cannot request an unbounded repetition count.
	return math.Max(1, size), true
}

func definiteGridTrackBreadth(kind computed.GridTrackKind, length computed.Length, available float64, viewport Viewport) (float64, bool) {
	if kind != computed.GridTrackLength {
		return 0, false
	}
	return math.Max(0, resolveLength(length, available, viewport, 0)), true
}

type gridLayoutModel struct {
	items         []gridLayoutItem
	columns       int
	rows          int
	explicitCols  int
	explicitRows  int
	columnAxis    gridAxisDefinition
	rowAxis       gridAxisDefinition
	columnOffset  int
	rowOffset     int
	placementOps  int
	occupied      map[gridCell]struct{}
	columnSubgrid bool
	rowSubgrid    bool
}

type gridCell struct {
	row    int
	column int
}

func (model *gridLayoutModel) collapsedAutoFitTracks(rows bool) []bool {
	axis := model.columnAxis
	count := model.columns
	offset := model.columnOffset
	if rows {
		axis = model.rowAxis
		count = model.rows
		offset = model.rowOffset
	}
	if axis.autoFitEnd <= axis.autoFitStart || count == 0 {
		return nil
	}
	collapsed := make([]bool, count)
	start := max(0, axis.autoFitStart+offset)
	end := min(count, axis.autoFitEnd+offset)
	for index := start; index < end; index++ {
		collapsed[index] = true
	}
	for _, item := range model.items {
		placement := item.column
		if rows {
			placement = item.row
		}
		if !placement.definite {
			continue
		}
		for index := max(start, placement.start); index < min(end, placement.start+placement.span); index++ {
			collapsed[index] = false
		}
	}
	return collapsed
}

func (context *layoutContext) layoutGridContainer(node *styledNode, box *Box, contentWidth float64, definiteHeight, repeatHeight *float64, repeatFulfillsMinimum bool, inherited *gridSubgridContext) (float64, error) {
	columnGap := math.Max(0, resolveLength(node.style.ColumnGap(), contentWidth, context.viewport, 0))
	rowGap := math.Max(0, resolveLength(node.style.RowGap(), contentWidth, context.viewport, 0))
	columnTemplate := node.style.GridTemplateColumns()
	rowTemplate := node.style.GridTemplateRows()
	columnSubgrid := columnTemplate.IsSubgrid() && inherited != nil && inherited.columns != nil
	rowSubgrid := rowTemplate.IsSubgrid() && inherited != nil && inherited.rows != nil
	if !columnSubgrid {
		if columnTemplate.IsSubgrid() {
			columnTemplate = computed.GridTrackList{}
		} else {
			columnTemplate = resolveGridAutoRepeat(columnTemplate, &contentWidth, false, columnGap, context.viewport)
		}
	}
	if !rowSubgrid {
		if rowTemplate.IsSubgrid() {
			rowTemplate = computed.GridTrackList{}
		} else {
			rowTemplate = resolveGridAutoRepeat(rowTemplate, repeatHeight, repeatFulfillsMinimum, rowGap, context.viewport)
		}
	}
	model, err := context.buildGridLayoutModel(node, columnTemplate, rowTemplate, inherited, columnSubgrid, rowSubgrid)
	if err != nil {
		return 0, err
	}
	collapsedColumns := model.collapsedAutoFitTracks(false)
	collapsedRows := model.collapsedAutoFitTracks(true)
	var columnSizes, columnStarts, columnEnds []float64
	if columnSubgrid {
		columnSizes, columnStarts, columnEnds = subgridAxisGeometry(inherited.columns, columnGap, node.style.ColumnGapNormal())
		box.gridColumnSubgrid = true
	} else {
		columnTracks := gridTrackSizes(model.columnAxis.template, node.style.GridAutoColumns(), model.columns, model.columnOffset)
		columnSizes, err = context.sizeGridColumns(&model, columnTracks, collapsedColumns, contentWidth, columnGap, contentAlignmentStretches(node.style.JustifyContent()))
		if err != nil {
			return 0, err
		}
		columnStarts, columnEnds, _ = alignedGridTrackGeometry(columnSizes, collapsedColumns, columnGap, contentWidth, node.style.JustifyContent(), node.style.JustifyContentOverflow())
	}
	box.gridColumnSizes = append([]float64(nil), columnSizes...)
	if columnSubgrid {
		box.gridColumnLineNames = columnTemplate.ResolvedSubgridLineNames(len(columnSizes) + 1)
	} else {
		box.gridColumnLineNames = gridUsedLineNames(model.columnAxis.template, len(columnSizes), model.columnOffset)
	}
	columnPlacementNames := gridUsedAxisLineNames(model.columnAxis, len(columnSizes), model.columnOffset)

	// The first item pass obtains max-content row contributions. Once rows are
	// sized, a second bounded pass supplies each area's definite height so
	// percentage heights and stretch use the actual grid-area containing block.
	if err := context.measureGridItems(&model, contentWidth, columnStarts, columnEnds, columnPlacementNames); err != nil {
		return 0, err
	}
	var rowSizes, rowStarts, rowEnds []float64
	gridHeight := 0.0
	if rowSubgrid {
		rowSizes, rowStarts, rowEnds = subgridAxisGeometry(inherited.rows, rowGap, node.style.RowGapNormal())
		if len(rowEnds) != 0 {
			gridHeight = rowEnds[len(rowEnds)-1]
		}
		box.gridRowSubgrid = true
	} else {
		rowTracks := gridTrackSizes(model.rowAxis.template, node.style.GridAutoRows(), model.rows, model.rowOffset)
		rowSizes, err = context.sizeGridRows(&model, rowTracks, collapsedRows, definiteHeight, contentWidth, rowGap, contentAlignmentStretches(node.style.AlignContent()), node.style.AlignItems())
		if err != nil {
			return 0, err
		}
		rowAvailable := sumFloat64(rowSizes) + gridGapTotal(collapsedRows, len(rowSizes), rowGap)
		if definiteHeight != nil {
			rowAvailable = *definiteHeight
		}
		rowStarts, rowEnds, gridHeight = alignedGridTrackGeometry(rowSizes, collapsedRows, rowGap, rowAvailable, node.style.AlignContent(), node.style.AlignContentOverflow())
	}
	box.gridRowSizes = append([]float64(nil), rowSizes...)
	if rowSubgrid {
		box.gridRowLineNames = rowTemplate.ResolvedSubgridLineNames(len(rowSizes) + 1)
	} else {
		box.gridRowLineNames = gridUsedLineNames(model.rowAxis.template, len(rowSizes), model.rowOffset)
	}
	rowPlacementNames := gridUsedAxisLineNames(model.rowAxis, len(rowSizes), model.rowOffset)
	if err := context.placeGridItems(node, box, &model, columnStarts, columnEnds, rowStarts, rowEnds, columnPlacementNames, rowPlacementNames); err != nil {
		return 0, err
	}
	return gridHeight, nil
}

func (context *layoutContext) buildGridLayoutModel(container *styledNode, columnTemplate, rowTemplate computed.GridTrackList, inherited *gridSubgridContext, columnSubgrid, rowSubgrid bool) (gridLayoutModel, error) {
	model := gridLayoutModel{occupied: make(map[gridCell]struct{}), columnSubgrid: columnSubgrid, rowSubgrid: rowSubgrid}
	model.items = context.gridItems(container)
	if len(model.items) > maxGridItems {
		return gridLayoutModel{}, fmt.Errorf("render: grid exceeds %d items", maxGridItems)
	}
	if columnSubgrid {
		model.columnAxis = gridAxisDefinitionForSubgrid(columnTemplate, container.style.GridTemplateAreas(), inherited.columns, false)
	} else {
		model.columnAxis = gridAxisDefinitionWithAreas(columnTemplate, container.style.GridTemplateAreas(), false)
	}
	if rowSubgrid {
		model.rowAxis = gridAxisDefinitionForSubgrid(rowTemplate, container.style.GridTemplateAreas(), inherited.rows, true)
	} else {
		model.rowAxis = gridAxisDefinitionWithAreas(rowTemplate, container.style.GridTemplateAreas(), true)
	}
	model.explicitCols = model.columnAxis.explicitTracks
	model.explicitRows = model.rowAxis.explicitTracks
	model.columns = model.explicitCols
	model.rows = model.explicitRows
	if len(model.items) != 0 && model.columns == 0 && container.style.GridAutoFlow().Axis() == computed.GridAutoFlowRow {
		model.columns = 1
	}
	if len(model.items) != 0 && model.rows == 0 && container.style.GridAutoFlow().Axis() == computed.GridAutoFlowColumn {
		model.rows = 1
	}

	minColumn, minRow := 0, 0
	for index := range model.items {
		item := &model.items[index]
		item.column = resolveGridAxis(item.node.style.GridColumnStart(), item.node.style.GridColumnEnd(), model.columnAxis)
		item.row = resolveGridAxis(item.node.style.GridRowStart(), item.node.style.GridRowEnd(), model.rowAxis)
		if columnSubgrid {
			normalizeSubgridPlacement(&item.column, model.explicitCols)
		}
		if rowSubgrid {
			normalizeSubgridPlacement(&item.row, model.explicitRows)
		}
		if item.column.definite {
			minColumn = min(minColumn, item.column.start)
		}
		if item.row.definite {
			minRow = min(minRow, item.row.start)
		}
	}
	if !columnSubgrid {
		model.columnOffset = -minColumn
	}
	if !rowSubgrid {
		model.rowOffset = -minRow
	}
	model.columns += model.columnOffset
	model.rows += model.rowOffset
	for index := range model.items {
		item := &model.items[index]
		if item.column.definite {
			item.column.start += model.columnOffset
			if !columnSubgrid {
				model.columns = max(model.columns, item.column.start+item.column.span)
			}
		}
		if item.row.definite {
			item.row.start += model.rowOffset
			if !rowSubgrid {
				model.rows = max(model.rows, item.row.start+item.row.span)
			}
		}
	}
	if err := model.checkBounds(); err != nil {
		return gridLayoutModel{}, err
	}

	// Fully definite items are allowed to overlap and establish occupancy before
	// the auto-placement cursor considers the remaining order-modified children.
	for index := range model.items {
		item := &model.items[index]
		if item.column.definite && item.row.definite {
			if err := model.occupy(item.row.start, item.column.start, item.row.span, item.column.span); err != nil {
				return gridLayoutModel{}, err
			}
		}
	}
	flow := container.style.GridAutoFlow()
	// Grid auto-placement has an axis-locked phase before the general cursor:
	// row-flow places definite-row items first, and column-flow does the mirror.
	// This prevents an earlier fully-auto item from consuming a cell that the
	// specification requires a later axis-locked item to consider first.
	for index := range model.items {
		item := &model.items[index]
		locked := item.row.definite
		if flow.Axis() == computed.GridAutoFlowColumn {
			locked = item.column.definite
		}
		if item.column.definite && item.row.definite || !locked {
			continue
		}
		row, column, err := model.findPosition(*item, flow.Axis(), 0, 0)
		if err != nil {
			row, column, err = model.subgridOverflowPosition(*item, err)
		}
		if err != nil {
			return gridLayoutModel{}, err
		}
		item.row.start, item.row.definite = row, true
		item.column.start, item.column.definite = column, true
		item.row.span, item.column.span = model.itemSpansAt(*item, row, column)
		if !rowSubgrid {
			model.rows = max(model.rows, row+item.row.span)
		}
		if !columnSubgrid {
			model.columns = max(model.columns, column+item.column.span)
		}
		if err := model.checkBounds(); err != nil {
			return gridLayoutModel{}, err
		}
		if err := model.occupy(row, column, item.row.span, item.column.span); err != nil {
			return gridLayoutModel{}, err
		}
	}
	cursorRow, cursorColumn := 0, 0
	for index := range model.items {
		item := &model.items[index]
		locked := item.row.definite
		if flow.Axis() == computed.GridAutoFlowColumn {
			locked = item.column.definite
		}
		if item.column.definite && item.row.definite || locked {
			continue
		}
		if flow.Dense() {
			cursorRow, cursorColumn = 0, 0
		}
		row, column, err := model.findPosition(*item, flow.Axis(), cursorRow, cursorColumn)
		if err != nil {
			row, column, err = model.subgridOverflowPosition(*item, err)
		}
		if err != nil {
			return gridLayoutModel{}, err
		}
		item.row.start, item.row.definite = row, true
		item.column.start, item.column.definite = column, true
		item.row.span, item.column.span = model.itemSpansAt(*item, row, column)
		if !rowSubgrid {
			model.rows = max(model.rows, row+item.row.span)
		}
		if !columnSubgrid {
			model.columns = max(model.columns, column+item.column.span)
		}
		if err := model.checkBounds(); err != nil {
			return gridLayoutModel{}, err
		}
		if err := model.occupy(row, column, item.row.span, item.column.span); err != nil {
			return gridLayoutModel{}, err
		}
		if !flow.Dense() {
			if flow.Axis() == computed.GridAutoFlowColumn {
				cursorRow, cursorColumn = row+item.row.span, column
				if cursorRow >= max(1, model.rows) {
					cursorRow, cursorColumn = 0, column+1
				}
			} else {
				cursorRow, cursorColumn = row, column+item.column.span
				if cursorColumn >= max(1, model.columns) {
					cursorRow, cursorColumn = row+1, 0
				}
			}
		}
	}
	if len(model.items) != 0 {
		model.columns = max(1, model.columns)
		model.rows = max(1, model.rows)
	}
	return model, model.checkBounds()
}

func (model *gridLayoutModel) subgridOverflowPosition(item gridLayoutItem, placementErr error) (int, int, error) {
	if model == nil || !model.columnSubgrid && !model.rowSubgrid {
		return 0, 0, placementErr
	}
	row, column := 0, 0
	if item.row.definite {
		row = item.row.start
	}
	if item.column.definite {
		column = item.column.start
	}
	if model.rowSubgrid {
		row = min(max(0, row), max(0, model.rows-item.row.span))
	}
	if model.columnSubgrid {
		column = min(max(0, column), max(0, model.columns-item.column.span))
	}
	return row, column, nil
}

func normalizeSubgridPlacement(placement *gridAxisPlacement, trackCount int) {
	if placement == nil || trackCount < 1 {
		return
	}
	if placement.definite {
		placement.start = min(max(0, placement.start), trackCount-1)
		placement.span = min(max(1, placement.span), trackCount-placement.start)
		return
	}
	placement.span = min(max(1, placement.span), trackCount)
}

func (context *layoutContext) gridItems(container *styledNode) []gridLayoutItem {
	items := make([]gridLayoutItem, 0, len(container.children))
	for index := 0; index < len(container.children); {
		child := container.children[index]
		if child == nil || child.style.Display() == displayNone || isOutOfFlow(child.style.Position()) {
			index++
			continue
		}
		if child.node != nil && child.node.Type == dom.ElementNode {
			blockified := *child
			blockified.style = child.style.WithBlockifiedDisplay()
			items = append(items, gridLayoutItem{node: &blockified, originalIndex: index})
			index++
			continue
		}
		end := index + 1
		for end < len(container.children) {
			candidate := container.children[end]
			if candidate == nil || candidate.style.Display() == displayNone || isOutOfFlow(candidate.style.Position()) {
				break
			}
			if candidate.node != nil && candidate.node.Type == dom.ElementNode {
				break
			}
			end++
		}
		anonymous := &styledNode{
			style:    container.style.WithAnonymousGridItem(),
			children: append([]*styledNode(nil), container.children[index:end]...),
		}
		if context.gridAnonymousProducesLayout(anonymous.children) {
			items = append(items, gridLayoutItem{node: anonymous, originalIndex: index})
		}
		index = end
	}
	sort.SliceStable(items, func(left, right int) bool {
		return items[left].node.style.Order() < items[right].node.style.Order()
	})
	return items
}

func (context *layoutContext) gridAnonymousProducesLayout(children []*styledNode) bool {
	builder := inlineTokenBuilder{images: context.images}
	for _, child := range children {
		builder.add(child, 1)
	}
	return len(builder.tokens) != 0
}

func resolveGridAxis(startLine, endLine computed.GridLine, definition gridAxisDefinition) gridAxisPlacement {
	if startLine.Kind() == computed.GridLineSpan && endLine.Kind() == computed.GridLineSpan {
		endLine = computed.GridLine{}
	}
	span := 1
	if startLine.Kind() == computed.GridLineSpan {
		if startLine.Name() == "" {
			span = max(1, startLine.Number())
		}
	}
	if endLine.Kind() == computed.GridLineSpan {
		if endLine.Name() == "" {
			span = max(1, endLine.Number())
		}
	}
	start, hasStart := gridLineCoordinate(startLine, definition, true)
	end, hasEnd := gridLineCoordinate(endLine, definition, false)
	switch {
	case hasStart && hasEnd:
		if end < start {
			start, end = end, start
		}
		if end == start {
			end++
		}
		return gridAxisPlacement{definite: true, start: start, span: end - start}
	case hasStart && endLine.Kind() == computed.GridLineSpan:
		end = gridSpanCoordinate(start, 1, endLine, definition)
		return gridAxisPlacement{definite: true, start: start, span: max(1, end-start)}
	case hasEnd && startLine.Kind() == computed.GridLineSpan:
		start = gridSpanCoordinate(end, -1, startLine, definition)
		return gridAxisPlacement{definite: true, start: start, span: max(1, end-start)}
	case hasStart:
		return gridAxisPlacement{definite: true, start: start, span: span}
	case hasEnd:
		return gridAxisPlacement{definite: true, start: end - span, span: span}
	default:
		return gridAxisPlacement{span: span}
	}
}

func gridLineCoordinate(line computed.GridLine, definition gridAxisDefinition, startEdge bool) (int, bool) {
	if line.Kind() != computed.GridLineNumber {
		return 0, false
	}
	explicitTracks := definition.explicitTracks
	if line.Name() != "" {
		name := line.Name()
		if !line.NumberExplicit() {
			areaName := name + "-end"
			if startEdge {
				areaName = name + "-start"
			}
			if coordinate, ok := explicitNamedGridLine(definition, areaName, 1); ok {
				return coordinate, true
			}
		}
		return namedGridLineCoordinate(definition, name, line.Number()), true
	}
	if line.Number() > 0 {
		return line.Number() - 1, true
	}
	return explicitTracks + 1 + line.Number(), true
}

func explicitNamedGridLine(definition gridAxisDefinition, name string, occurrence int) (int, bool) {
	if occurrence == 0 {
		return 0, false
	}
	if occurrence > 0 {
		for line := 0; line <= definition.explicitTracks; line++ {
			if definition.lineHasName(line, name) {
				occurrence--
				if occurrence == 0 {
					return line, true
				}
			}
		}
		return 0, false
	}
	for line := definition.explicitTracks; line >= 0; line-- {
		if definition.lineHasName(line, name) {
			occurrence++
			if occurrence == 0 {
				return line, true
			}
		}
	}
	return 0, false
}

func namedGridLineCoordinate(definition gridAxisDefinition, name string, occurrence int) int {
	if coordinate, ok := explicitNamedGridLine(definition, name, occurrence); ok {
		return coordinate
	}
	if occurrence > 0 {
		matches := 0
		for line := 0; line <= definition.explicitTracks; line++ {
			if definition.lineHasName(line, name) {
				matches++
			}
		}
		return definition.explicitTracks + occurrence - matches
	}
	matches := 0
	for line := definition.explicitTracks; line >= 0; line-- {
		if definition.lineHasName(line, name) {
			matches++
		}
	}
	return occurrence + matches
}

func gridSpanCoordinate(opposite, direction int, line computed.GridLine, definition gridAxisDefinition) int {
	occurrence := max(1, line.Number())
	return namedGridSpanCoordinate(opposite, direction, occurrence, line.Name(), definition)
}

func namedGridSpanCoordinate(opposite, direction, occurrence int, name string, definition gridAxisDefinition) int {
	if name == "" {
		return opposite + direction*occurrence
	}
	remaining := occurrence
	for candidate := opposite + direction; candidate >= -maxGridTracksPerAxis && candidate <= maxGridTracksPerAxis*2; candidate += direction {
		implicitMatch := direction > 0 && candidate > definition.explicitTracks || direction < 0 && candidate < 0
		if implicitMatch || candidate >= 0 && candidate <= definition.explicitTracks && definition.lineHasName(candidate, name) {
			remaining--
			if remaining == 0 {
				return candidate
			}
		}
	}
	return opposite + direction*occurrence
}

func (model *gridLayoutModel) checkBounds() error {
	if model.columns < 0 || model.rows < 0 || model.columns > maxGridTracksPerAxis || model.rows > maxGridTracksPerAxis {
		return fmt.Errorf("render: grid exceeds %d tracks per axis", maxGridTracksPerAxis)
	}
	if model.columns != 0 && model.rows > maxGridOccupiedCells/model.columns {
		return fmt.Errorf("render: grid exceeds %d occupied cells", maxGridOccupiedCells)
	}
	return nil
}

func (model *gridLayoutModel) occupy(row, column, rowSpan, columnSpan int) error {
	if row < 0 || column < 0 || rowSpan < 1 || columnSpan < 1 || row+rowSpan > maxGridTracksPerAxis || column+columnSpan > maxGridTracksPerAxis {
		return fmt.Errorf("render: grid placement exceeds %d tracks per axis", maxGridTracksPerAxis)
	}
	if rowSpan > maxGridOccupiedCells/columnSpan {
		return fmt.Errorf("render: grid item exceeds %d occupied cells", maxGridOccupiedCells)
	}
	for candidateRow := row; candidateRow < row+rowSpan; candidateRow++ {
		for candidateColumn := column; candidateColumn < column+columnSpan; candidateColumn++ {
			model.placementOps++
			if model.placementOps > maxGridPlacementOps {
				return fmt.Errorf("render: grid exceeds %d placement operations", maxGridPlacementOps)
			}
			model.occupied[gridCell{row: candidateRow, column: candidateColumn}] = struct{}{}
		}
	}
	return nil
}

func (model *gridLayoutModel) areaFree(row, column, rowSpan, columnSpan int) (bool, error) {
	if row < 0 || column < 0 || row+rowSpan > maxGridTracksPerAxis || column+columnSpan > maxGridTracksPerAxis {
		return false, nil
	}
	if model.rowSubgrid && row+rowSpan > model.rows || model.columnSubgrid && column+columnSpan > model.columns {
		return false, nil
	}
	for candidateRow := row; candidateRow < row+rowSpan; candidateRow++ {
		for candidateColumn := column; candidateColumn < column+columnSpan; candidateColumn++ {
			model.placementOps++
			if model.placementOps > maxGridPlacementOps {
				return false, fmt.Errorf("render: grid exceeds %d placement operations", maxGridPlacementOps)
			}
			if _, occupied := model.occupied[gridCell{row: candidateRow, column: candidateColumn}]; occupied {
				return false, nil
			}
		}
	}
	return true, nil
}

func (model *gridLayoutModel) findPosition(item gridLayoutItem, axis computed.GridAutoFlowAxis, cursorRow, cursorColumn int) (int, int, error) {
	if axis == computed.GridAutoFlowColumn {
		return model.findColumnFlowPosition(item, cursorRow, cursorColumn)
	}
	return model.findRowFlowPosition(item, cursorRow, cursorColumn)
}

func (model *gridLayoutModel) itemSpansAt(item gridLayoutItem, row, column int) (int, int) {
	return item.row.span, item.column.span
}

func (model *gridLayoutModel) findRowFlowPosition(item gridLayoutItem, cursorRow, cursorColumn int) (int, int, error) {
	if item.row.definite {
		row := item.row.start
		startColumn := 0
		if cursorRow == row {
			startColumn = cursorColumn
		}
		for column := startColumn; column < maxGridTracksPerAxis; column++ {
			if item.column.definite {
				column = item.column.start
			}
			rowSpan, columnSpan := model.itemSpansAt(item, row, column)
			free, err := model.areaFree(row, column, rowSpan, columnSpan)
			if err != nil || free {
				return row, column, err
			}
			if item.column.definite {
				break
			}
		}
		return 0, 0, fmt.Errorf("render: grid row auto-placement exhausted")
	}
	_, initialColumnSpan := model.itemSpansAt(item, cursorRow, 0)
	columns := max(model.columns, initialColumnSpan)
	if item.column.definite {
		columns = max(columns, item.column.start+item.column.span)
	}
	for row := cursorRow; row < maxGridTracksPerAxis; row++ {
		startColumn := 0
		if row == cursorRow {
			startColumn = cursorColumn
		}
		endColumn := columns - 1
		if item.column.definite {
			startColumn, endColumn = item.column.start, item.column.start
		}
		for column := startColumn; column <= endColumn; column++ {
			rowSpan, columnSpan := model.itemSpansAt(item, row, column)
			if row+rowSpan > maxGridTracksPerAxis || column+columnSpan > columns {
				continue
			}
			free, err := model.areaFree(row, column, rowSpan, columnSpan)
			if err != nil || free {
				return row, column, err
			}
		}
	}
	return 0, 0, fmt.Errorf("render: grid row auto-placement exhausted")
}

func (model *gridLayoutModel) findColumnFlowPosition(item gridLayoutItem, cursorRow, cursorColumn int) (int, int, error) {
	if item.column.definite {
		column := item.column.start
		startRow := 0
		if cursorColumn == column {
			startRow = cursorRow
		}
		for row := startRow; row < maxGridTracksPerAxis; row++ {
			if item.row.definite {
				row = item.row.start
			}
			rowSpan, columnSpan := model.itemSpansAt(item, row, column)
			free, err := model.areaFree(row, column, rowSpan, columnSpan)
			if err != nil || free {
				return row, column, err
			}
			if item.row.definite {
				break
			}
		}
		return 0, 0, fmt.Errorf("render: grid column auto-placement exhausted")
	}
	initialRowSpan, _ := model.itemSpansAt(item, 0, cursorColumn)
	rows := max(model.rows, initialRowSpan)
	if item.row.definite {
		rows = max(rows, item.row.start+item.row.span)
	}
	for column := cursorColumn; column < maxGridTracksPerAxis; column++ {
		startRow := 0
		if column == cursorColumn {
			startRow = cursorRow
		}
		endRow := rows - 1
		if item.row.definite {
			startRow, endRow = item.row.start, item.row.start
		}
		for row := startRow; row <= endRow; row++ {
			rowSpan, columnSpan := model.itemSpansAt(item, row, column)
			if row+rowSpan > rows || column+columnSpan > maxGridTracksPerAxis {
				continue
			}
			free, err := model.areaFree(row, column, rowSpan, columnSpan)
			if err != nil || free {
				return row, column, err
			}
		}
	}
	return 0, 0, fmt.Errorf("render: grid column auto-placement exhausted")
}

func gridTrackSizes(template, automatic computed.GridTrackList, count, offset int) []computed.GridTrackSize {
	tracks := make([]computed.GridTrackSize, count)
	explicitCount := template.Len()
	automaticCount := automatic.Len()
	for index := range tracks {
		if explicit, ok := template.At(index - offset); ok {
			tracks[index] = explicit
			continue
		}
		if automaticCount == 0 {
			continue
		}
		patternIndex := 0
		if index < offset {
			distance := offset - 1 - index
			patternIndex = automaticCount - 1 - distance%automaticCount
		} else {
			distance := index - offset - explicitCount
			patternIndex = distance % automaticCount
		}
		if implicit, ok := automatic.At(patternIndex); ok {
			tracks[index] = implicit
		}
	}
	return tracks
}

func gridUsedLineNames(template computed.GridTrackList, count, offset int) [][]string {
	lines := make([][]string, count+1)
	for explicitLine := 0; explicitLine <= template.Len(); explicitLine++ {
		usedLine := explicitLine + offset
		if usedLine >= 0 && usedLine < len(lines) {
			lines[usedLine] = template.LineNames(explicitLine)
		}
	}
	return lines
}

func gridUsedAxisLineNames(definition gridAxisDefinition, count, offset int) [][]string {
	lines := make([][]string, count+1)
	for usedLine := range lines {
		explicitLine := usedLine - offset
		if explicitLine < 0 || explicitLine > definition.explicitTracks {
			continue
		}
		lines[usedLine] = append(lines[usedLine], definition.template.LineNames(explicitLine)...)
		if explicitLine < len(definition.areaLineNames) {
			lines[usedLine] = append(lines[usedLine], definition.areaLineNames[explicitLine]...)
		}
		if explicitLine < len(definition.subgridNames) {
			lines[usedLine] = append(lines[usedLine], definition.subgridNames[explicitLine]...)
		}
	}
	return lines
}

func subgridAxisGeometry(axis *gridSubgridAxisContext, gap float64, normalGap bool) (sizes, starts, ends []float64) {
	if axis == nil {
		return nil, nil, nil
	}
	count := min(len(axis.starts), len(axis.ends))
	if count == 0 {
		return nil, nil, nil
	}
	starts = append([]float64(nil), axis.starts[:count]...)
	ends = append([]float64(nil), axis.ends[:count]...)
	origin := starts[0]
	for index := range starts {
		starts[index] -= origin + axis.edgeStart
		ends[index] -= origin + axis.edgeStart
	}
	starts[0] = 0
	ends[count-1] = math.Max(starts[count-1], ends[count-1]-axis.edgeEnd)
	if !normalGap {
		for index := 0; index+1 < count; index++ {
			parentGap := starts[index+1] - ends[index]
			delta := (parentGap - gap) / 2
			ends[index] += delta
			starts[index+1] -= delta
		}
	}
	sizes = make([]float64, count)
	for index := range sizes {
		sizes[index] = math.Max(0, ends[index]-starts[index])
	}
	return sizes, starts, ends
}

func gridSubgridAxisForPlacement(starts, ends []float64, lineNames [][]string, placement gridAxisPlacement) *gridSubgridAxisContext {
	if !placement.definite || placement.start < 0 || placement.span < 1 || placement.start+placement.span > len(starts) || placement.start+placement.span > len(ends) {
		return nil
	}
	origin := starts[placement.start]
	axis := &gridSubgridAxisContext{
		starts:    make([]float64, placement.span),
		ends:      make([]float64, placement.span),
		lineNames: make([][]string, placement.span+1),
	}
	for index := range placement.span {
		axis.starts[index] = starts[placement.start+index] - origin
		axis.ends[index] = ends[placement.start+index] - origin
	}
	for index := range axis.lineNames {
		source := placement.start + index
		if source < len(lineNames) {
			axis.lineNames[index] = append([]string(nil), lineNames[source]...)
		}
	}
	return axis
}

func gridSubgridContextForItem(item *gridLayoutItem, columnStarts, columnEnds, rowStarts, rowEnds []float64, columnNames, rowNames [][]string) *gridSubgridContext {
	if item == nil || item.node == nil || item.node.style.Display().Inside() != computed.DisplayInsideGrid || isOutOfFlow(item.node.style.Position()) {
		return nil
	}
	result := &gridSubgridContext{}
	if item.node.style.GridTemplateColumns().IsSubgrid() {
		result.columns = gridSubgridAxisForPlacement(columnStarts, columnEnds, columnNames, item.column)
	}
	if item.node.style.GridTemplateRows().IsSubgrid() {
		result.rows = gridSubgridAxisForPlacement(rowStarts, rowEnds, rowNames, item.row)
	}
	if result.columns == nil && result.rows == nil {
		return nil
	}
	return result
}

func (context *layoutContext) applySubgridEdgeInsets(item *gridLayoutItem, subgrid *gridSubgridContext, availableWidth float64) {
	if item == nil || item.node == nil || subgrid == nil {
		return
	}
	padding := context.resolvePadding(item.node.style, availableWidth)
	border := context.resolveBorder(item.node.style, availableWidth)
	if subgrid.columns != nil {
		subgrid.columns.edgeStart = item.marginLeft + padding.Left + border.Left
		subgrid.columns.edgeEnd = item.marginRight + padding.Right + border.Right
	}
	if subgrid.rows != nil {
		subgrid.rows.edgeStart = item.marginTop + padding.Top + border.Top
		subgrid.rows.edgeEnd = item.marginBottom + padding.Bottom + border.Bottom
	}
}

func (context *layoutContext) sizeGridColumns(model *gridLayoutModel, tracks []computed.GridTrackSize, collapsed []bool, availableWidth, gap float64, stretchAuto bool) ([]float64, error) {
	budget := maxGridItems
	contributions, err := context.gridColumnContributions(model, 0, availableWidth, gap, 0, &budget)
	if err != nil {
		return nil, err
	}
	height := availableWidth
	return sizeGridTrackAxis(tracks, contributions, collapsed, &height, gap, context.viewport, stretchAuto), nil
}

const maxSubgridNesting = 32

func (context *layoutContext) gridColumnContributions(model *gridLayoutModel, baseStart int, availableWidth, parentGap float64, depth int, budget *int) ([]gridTrackContribution, error) {
	if model == nil || budget == nil || *budget < 0 || depth > maxSubgridNesting {
		return nil, fmt.Errorf("render: subgrid intrinsic contribution budget exceeded")
	}
	contributions := make([]gridTrackContribution, 0, len(model.items))
	lineNames := gridUsedAxisLineNames(model.columnAxis, model.columns, model.columnOffset)
	for index := range model.items {
		item := &model.items[index]
		if item.node.style.Display().Inside() == computed.DisplayInsideGrid && item.node.style.GridTemplateColumns().IsSubgrid() {
			if depth == maxSubgridNesting {
				return nil, fmt.Errorf("render: subgrid exceeds nesting depth %d", maxSubgridNesting)
			}
			axis := gridSubgridAxisForNames(lineNames, item.column)
			if axis == nil {
				continue
			}
			rowTemplate := item.node.style.GridTemplateRows()
			if rowTemplate.IsSubgrid() {
				rowTemplate = computed.GridTrackList{}
			} else {
				rowTemplate = resolveGridAutoRepeat(rowTemplate, nil, false, 0, context.viewport)
			}
			nested, err := context.buildGridLayoutModel(item.node, item.node.style.GridTemplateColumns(), rowTemplate, &gridSubgridContext{columns: axis}, true, false)
			if err != nil {
				return nil, err
			}
			nestedGap := parentGap
			if !item.node.style.ColumnGapNormal() {
				nestedGap = math.Max(0, resolveLength(item.node.style.ColumnGap(), availableWidth, context.viewport, 0))
			}
			nestedContributions, err := context.gridColumnContributions(&nested, baseStart+item.column.start, availableWidth, nestedGap, depth+1, budget)
			if err != nil {
				return nil, err
			}
			context.adjustSubgridColumnContributions(nestedContributions, baseStart+item.column.start, item.column.span, item.node, availableWidth, parentGap, nestedGap)
			contributions = append(contributions, nestedContributions...)
			continue
		}
		*budget--
		if *budget < 0 {
			return nil, fmt.Errorf("render: grid exceeds %d intrinsic items", maxGridItems)
		}
		intrinsic, err := context.gridItemIntrinsicWidths(item.node, availableWidth)
		if err != nil {
			return nil, err
		}
		contributions = append(contributions, gridTrackContribution{
			start:     baseStart + item.column.start,
			span:      item.column.span,
			minimum:   intrinsic.minimum,
			preferred: intrinsic.preferred,
		})
	}
	return contributions, nil
}

func gridSubgridAxisForNames(lineNames [][]string, placement gridAxisPlacement) *gridSubgridAxisContext {
	if !placement.definite || placement.start < 0 || placement.span < 1 || placement.start+placement.span >= len(lineNames) {
		return nil
	}
	axis := &gridSubgridAxisContext{
		starts:    make([]float64, placement.span),
		ends:      make([]float64, placement.span),
		lineNames: make([][]string, placement.span+1),
	}
	for index := range axis.starts {
		axis.starts[index] = float64(index)
		axis.ends[index] = float64(index + 1)
	}
	for index := range axis.lineNames {
		axis.lineNames[index] = append([]string(nil), lineNames[placement.start+index]...)
	}
	return axis
}

func (context *layoutContext) adjustSubgridColumnContributions(contributions []gridTrackContribution, start, span int, node *styledNode, availableWidth, parentGap, subgridGap float64) {
	if node == nil || span < 1 {
		return
	}
	padding := context.resolvePadding(node.style, availableWidth)
	border := context.resolveBorder(node.style, availableWidth)
	startEdge := resolveLength(node.style.MarginLeft(), availableWidth, context.viewport, 0) + padding.Left + border.Left
	endEdge := resolveLength(node.style.MarginRight(), availableWidth, context.viewport, 0) + padding.Right + border.Right
	halfGapDifference := (subgridGap - parentGap) / 2
	for index := range contributions {
		contribution := &contributions[index]
		extra := 0.0
		if contribution.start == start {
			extra += startEdge
		} else {
			extra += halfGapDifference
		}
		if contribution.start+contribution.span == start+span {
			extra += endEdge
		} else {
			extra += halfGapDifference
		}
		contribution.minimum = math.Max(0, contribution.minimum+extra)
		contribution.preferred = math.Max(contribution.minimum, contribution.preferred+extra)
	}
}

type gridTrackContribution struct {
	start     int
	span      int
	minimum   float64
	preferred float64
}

type gridTrackState struct {
	minKind       computed.GridTrackKind
	maxKind       computed.GridTrackKind
	base          float64
	limit         float64
	flexIntrinsic float64
	fraction      float64
	fitContent    bool
	fitLimit      float64
	fitLimitKnown bool
	collapsed     bool
}

func sizeGridTrackAxis(tracks []computed.GridTrackSize, contributions []gridTrackContribution, collapsed []bool, available *float64, gap float64, viewport Viewport, stretchAuto bool) []float64 {
	states := initializeGridTrackStates(tracks, collapsed, available, viewport)
	applyGridTrackContributions(states, contributions, gap)
	sizes := make([]float64, len(states))
	for index := range states {
		sizes[index] = states[index].base
	}
	if available == nil {
		return indefiniteGridTrackSizes(states)
	}
	maximizeGridTracks(sizes, states, *available, gap)
	distributeGridFlexibleSpace(sizes, states, *available, gap)
	if stretchAuto {
		stretchGridAutoTracks(sizes, states, *available, gap)
	}
	return sizes
}

func initializeGridTrackStates(tracks []computed.GridTrackSize, collapsed []bool, available *float64, viewport Viewport) []gridTrackState {
	states := make([]gridTrackState, len(tracks))
	percentageBase := 0.0
	if available != nil {
		percentageBase = *available
	}
	for index, track := range tracks {
		if index < len(collapsed) && collapsed[index] {
			states[index] = gridTrackState{collapsed: true}
			continue
		}
		minKind := effectiveGridBreadthKind(track.MinKind(), track.MinLength(), available != nil)
		maxKind := effectiveGridBreadthKind(track.MaxKind(), track.MaxLength(), available != nil)
		state := gridTrackState{minKind: minKind, maxKind: maxKind, fraction: track.MaxFraction(), fitContent: track.IsFitContent()}
		if minKind == computed.GridTrackLength {
			state.base = math.Max(0, resolveLength(track.MinLength(), percentageBase, viewport, 0))
		}
		if maxKind == computed.GridTrackLength {
			state.limit = math.Max(0, resolveLength(track.MaxLength(), percentageBase, viewport, 0))
		}
		if track.IsFitContent() && (available != nil || !track.FitContentLimit().DependsOnPercent()) {
			state.fitLimit = math.Max(0, resolveLength(track.FitContentLimit(), percentageBase, viewport, 0))
			state.fitLimitKnown = true
		}
		state.limit = math.Max(state.limit, state.base)
		state.flexIntrinsic = state.base
		states[index] = state
	}
	return states
}

func effectiveGridBreadthKind(kind computed.GridTrackKind, length computed.Length, percentageDefinite bool) computed.GridTrackKind {
	if kind == computed.GridTrackLength && length.DependsOnPercent() && !percentageDefinite {
		return computed.GridTrackAuto
	}
	return kind
}

func applyGridTrackContributions(states []gridTrackState, contributions []gridTrackContribution, gap float64) {
	base := make([]float64, len(states))
	for index := range states {
		base[index] = states[index].base
	}
	for _, contribution := range contributions {
		distributeGridValue(base, states, contribution.start, contribution.span, contribution.minimum, gap, func(state gridTrackState) bool {
			return state.minKind == computed.GridTrackAuto || state.minKind == computed.GridTrackMinContent || state.minKind == computed.GridTrackMaxContent
		})
		distributeGridValue(base, states, contribution.start, contribution.span, contribution.preferred, gap, func(state gridTrackState) bool {
			return state.minKind == computed.GridTrackMaxContent
		})
	}
	for index := range states {
		states[index].base = base[index]
		states[index].limit = math.Max(states[index].limit, base[index])
		states[index].flexIntrinsic = math.Max(states[index].flexIntrinsic, base[index])
	}
	limits := make([]float64, len(states))
	flexIntrinsic := make([]float64, len(states))
	for index := range states {
		limits[index] = states[index].limit
		flexIntrinsic[index] = states[index].flexIntrinsic
	}
	for _, contribution := range contributions {
		distributeGridValue(limits, states, contribution.start, contribution.span, contribution.minimum, gap, func(state gridTrackState) bool {
			return state.maxKind == computed.GridTrackMinContent
		})
		distributeGridValue(limits, states, contribution.start, contribution.span, contribution.preferred, gap, func(state gridTrackState) bool {
			return state.maxKind == computed.GridTrackAuto || state.maxKind == computed.GridTrackMaxContent
		})
		distributeGridValue(flexIntrinsic, states, contribution.start, contribution.span, contribution.preferred, gap, func(state gridTrackState) bool {
			return state.maxKind == computed.GridTrackFraction
		})
	}
	for index := range states {
		limit := limits[index]
		if states[index].fitContent && states[index].fitLimitKnown {
			limit = math.Min(limit, states[index].fitLimit)
		}
		states[index].limit = math.Max(limit, states[index].base)
		states[index].flexIntrinsic = math.Max(flexIntrinsic[index], states[index].base)
	}
}

func distributeGridValue(values []float64, states []gridTrackState, start, span int, contribution, gap float64, eligible func(gridTrackState) bool) {
	if start < 0 || span < 1 || start+span > len(values) {
		return
	}
	current := sumFloat64(values[start:start+span]) + gridStateGapTotal(states[start:start+span], gap)
	deficit := contribution - current
	if deficit <= 0 {
		return
	}
	indices := make([]int, 0, span)
	for index := start; index < start+span; index++ {
		if !states[index].collapsed && eligible(states[index]) {
			indices = append(indices, index)
		}
	}
	if len(indices) == 0 {
		return
	}
	addition := deficit / float64(len(indices))
	for _, index := range indices {
		values[index] += addition
	}
}

func maximizeGridTracks(sizes []float64, states []gridTrackState, available, gap float64) {
	free := available - gridStateGapTotal(states, gap) - sumFloat64(sizes)
	if free <= 0 {
		return
	}
	active := make([]int, 0, len(states))
	for index, state := range states {
		if !state.collapsed && state.maxKind != computed.GridTrackFraction && state.limit > sizes[index] {
			active = append(active, index)
		}
	}
	for free > 0 && len(active) != 0 {
		share := free / float64(len(active))
		next := active[:0]
		consumed := 0.0
		for _, index := range active {
			growth := math.Min(share, states[index].limit-sizes[index])
			if growth > 0 {
				sizes[index] += growth
				consumed += growth
			}
			if sizes[index] < states[index].limit {
				next = append(next, index)
			}
		}
		if consumed <= 0 {
			return
		}
		free -= consumed
		active = next
	}
}

func distributeGridFlexibleSpace(sizes []float64, states []gridTrackState, available, gap float64) {
	gapTotal := gridStateGapTotal(states, gap)
	nonFrTotal := 0.0
	active := make([]int, 0, len(states))
	for index, state := range states {
		if !state.collapsed && state.maxKind == computed.GridTrackFraction && state.fraction > 0 {
			active = append(active, index)
		} else {
			nonFrTotal += sizes[index]
		}
	}
	if len(active) != 0 {
		frozen := nonFrTotal
		unit := 0.0
		for len(active) != 0 {
			factorTotal := 0.0
			for _, index := range active {
				factorTotal += states[index].fraction
			}
			unit = math.Max(0, available-gapTotal-frozen) / math.Max(1, factorTotal)
			remaining := active[:0]
			frozeTrack := false
			for _, index := range active {
				if sizes[index] > unit*states[index].fraction {
					frozen += sizes[index]
					frozeTrack = true
					continue
				}
				remaining = append(remaining, index)
			}
			active = remaining
			if !frozeTrack {
				break
			}
		}
		for _, index := range active {
			sizes[index] = unit * states[index].fraction
		}
	}
}

func stretchGridAutoTracks(sizes []float64, states []gridTrackState, available, gap float64) {
	gapTotal := gridStateGapTotal(states, gap)
	used := sumFloat64(sizes) + gapTotal
	leftover := available - used
	if leftover <= 0 {
		return
	}
	autoCount := 0
	for _, state := range states {
		if !state.collapsed && state.maxKind == computed.GridTrackAuto {
			autoCount++
		}
	}
	if autoCount == 0 {
		return
	}
	addition := leftover / float64(autoCount)
	for index, state := range states {
		if !state.collapsed && state.maxKind == computed.GridTrackAuto {
			sizes[index] += addition
		}
	}
}

func indefiniteGridTrackSizes(states []gridTrackState) []float64 {
	sizes := make([]float64, len(states))
	for index, state := range states {
		switch state.maxKind {
		case computed.GridTrackFraction:
			sizes[index] = math.Max(state.base, state.flexIntrinsic)
		default:
			sizes[index] = math.Max(state.base, state.limit)
		}
	}
	expandIntrinsicGridFractions(sizes, states)
	return sizes
}

func gridStateGapTotal(states []gridTrackState, gap float64) float64 {
	if gap <= 0 || len(states) < 2 {
		return 0
	}
	total := 0.0
	for index := 0; index+1 < len(states); index++ {
		if !states[index].collapsed && !states[index+1].collapsed {
			total += gap
		}
	}
	return total
}

func (context *layoutContext) measureGridItems(model *gridLayoutModel, contentWidth float64, columnStarts, columnEnds []float64, columnNames [][]string) error {
	for index := range model.items {
		item := &model.items[index]
		cellWidth := gridTrackSpan(columnStarts, columnEnds, item.column.start, item.column.span)
		item.marginTop = resolveLength(item.node.style.MarginTop(), contentWidth, context.viewport, 0)
		item.marginRight = resolveLength(item.node.style.MarginRight(), contentWidth, context.viewport, 0)
		item.marginBottom = resolveLength(item.node.style.MarginBottom(), contentWidth, context.viewport, 0)
		item.marginLeft = resolveLength(item.node.style.MarginLeft(), contentWidth, context.viewport, 0)
		subgrid := gridSubgridContextForItem(item, columnStarts, columnEnds, nil, nil, columnNames, nil)
		context.applySubgridEdgeInsets(item, subgrid, cellWidth)
		box, err := context.layoutBlockSizedWithSubgrid(item.node, 0, 0, cellWidth, nil, nil, true, subgrid, blockLayoutOverrides{})
		if err != nil {
			return err
		}
		item.box = box
	}
	return nil
}

func (context *layoutContext) sizeGridRows(model *gridLayoutModel, tracks []computed.GridTrackSize, collapsed []bool, definiteHeight *float64, availableWidth, gap float64, stretchAuto bool, parentAlignment computed.AlignItems) ([]float64, error) {
	firstGroups, lastGroups := gridRowBaselineGroups(model.items, parentAlignment)
	budget := maxGridItems
	contributions, err := context.gridRowContributions(model, 0, availableWidth, gap, 0, &budget, firstGroups, lastGroups, parentAlignment)
	if err != nil {
		return nil, err
	}
	return sizeGridTrackAxis(tracks, contributions, collapsed, definiteHeight, gap, context.viewport, stretchAuto), nil
}

func (context *layoutContext) gridRowContributions(model *gridLayoutModel, baseStart int, availableWidth, parentGap float64, depth int, budget *int, firstGroups, lastGroups map[int]baselineAlignmentGroup, parentAlignment computed.AlignItems) ([]gridTrackContribution, error) {
	if model == nil || budget == nil || *budget < 0 || depth > maxSubgridNesting {
		return nil, fmt.Errorf("render: subgrid intrinsic contribution budget exceeded")
	}
	contributions := make([]gridTrackContribution, 0, len(model.items))
	lineNames := gridUsedAxisLineNames(model.rowAxis, model.rows, model.rowOffset)
	for index := range model.items {
		item := &model.items[index]
		if item.node.style.Display().Inside() == computed.DisplayInsideGrid && item.node.style.GridTemplateRows().IsSubgrid() {
			if depth == maxSubgridNesting {
				return nil, fmt.Errorf("render: subgrid exceeds nesting depth %d", maxSubgridNesting)
			}
			axis := gridSubgridAxisForNames(lineNames, item.row)
			if axis == nil {
				continue
			}
			columnTemplate := item.node.style.GridTemplateColumns()
			if columnTemplate.IsSubgrid() {
				columnTemplate = computed.GridTrackList{}
			} else {
				columnTemplate = resolveGridAutoRepeat(columnTemplate, nil, false, 0, context.viewport)
			}
			nested, err := context.buildGridLayoutModel(item.node, columnTemplate, item.node.style.GridTemplateRows(), &gridSubgridContext{rows: axis}, false, true)
			if err != nil {
				return nil, err
			}
			for nestedIndex := range nested.items {
				nestedItem := &nested.items[nestedIndex]
				nestedItem.box = findGridItemBox(item.box, nestedItem.node)
				nestedItem.marginTop = resolveLength(nestedItem.node.style.MarginTop(), availableWidth, context.viewport, 0)
				nestedItem.marginBottom = resolveLength(nestedItem.node.style.MarginBottom(), availableWidth, context.viewport, 0)
			}
			nestedGap := parentGap
			if !item.node.style.RowGapNormal() {
				nestedGap = math.Max(0, resolveLength(item.node.style.RowGap(), availableWidth, context.viewport, 0))
			}
			nestedContributions, err := context.gridRowContributions(&nested, baseStart+item.row.start, availableWidth, nestedGap, depth+1, budget, nil, nil, computed.AlignNormal)
			if err != nil {
				return nil, err
			}
			context.adjustSubgridRowContributions(nestedContributions, baseStart+item.row.start, item.row.span, item.node, availableWidth, parentGap, nestedGap)
			contributions = append(contributions, nestedContributions...)
			continue
		}
		(*budget)--
		if *budget < 0 {
			return nil, fmt.Errorf("render: grid exceeds %d intrinsic items", maxGridItems)
		}
		itemBox := item.box
		if itemBox == nil {
			var err error
			itemBox, err = context.layoutBlockSized(item.node, 0, 0, availableWidth, nil, nil, true)
			if err != nil {
				return nil, err
			}
		}
		contribution := itemBox.Bounds.Height + item.marginTop + item.marginBottom
		if depth == 0 {
			alignment := resolvedSelfAlignment(item.node.style.AlignSelf(), parentAlignment)
			switch alignment {
			case computed.AlignBaseline:
				distances := boxBaselineDistances(itemBox, item.marginTop, item.marginBottom, false)
				contribution += math.Max(0, firstGroups[item.row.start].start-distances.start)
			case computed.AlignLastBaseline:
				distances := boxBaselineDistances(itemBox, item.marginTop, item.marginBottom, true)
				key := item.row.start + item.row.span - 1
				contribution += math.Max(0, lastGroups[key].end-distances.end)
			}
		}
		contributions = append(contributions, gridTrackContribution{
			start: baseStart + item.row.start, span: item.row.span,
			minimum: contribution, preferred: contribution,
		})
	}
	return contributions, nil
}

func findGridItemBox(root *Box, node *styledNode) *Box {
	if root == nil || node == nil {
		return nil
	}
	if node.node != nil && root.Node == node.node && root.Pseudo == node.pseudo {
		return root
	}
	for _, child := range root.Children {
		if found := findGridItemBox(child, node); found != nil {
			return found
		}
	}
	return nil
}

func (context *layoutContext) adjustSubgridRowContributions(contributions []gridTrackContribution, start, span int, node *styledNode, availableWidth, parentGap, subgridGap float64) {
	if node == nil || span < 1 {
		return
	}
	padding := context.resolvePadding(node.style, availableWidth)
	border := context.resolveBorder(node.style, availableWidth)
	startEdge := resolveLength(node.style.MarginTop(), availableWidth, context.viewport, 0) + padding.Top + border.Top
	endEdge := resolveLength(node.style.MarginBottom(), availableWidth, context.viewport, 0) + padding.Bottom + border.Bottom
	halfGapDifference := (subgridGap - parentGap) / 2
	for index := range contributions {
		contribution := &contributions[index]
		extra := 0.0
		if contribution.start == start {
			extra += startEdge
		} else {
			extra += halfGapDifference
		}
		if contribution.start+contribution.span == start+span {
			extra += endEdge
		} else {
			extra += halfGapDifference
		}
		contribution.minimum = math.Max(0, contribution.minimum+extra)
		contribution.preferred = math.Max(contribution.minimum, contribution.preferred+extra)
	}
}

func (context *layoutContext) placeGridItems(container *styledNode, box *Box, model *gridLayoutModel, columnStarts, columnEnds, rowStarts, rowEnds []float64, columnNames, rowNames [][]string) error {
	for index := range model.items {
		item := &model.items[index]
		cellWidth := gridTrackSpan(columnStarts, columnEnds, item.column.start, item.column.span)
		cellHeight := gridTrackSpan(rowStarts, rowEnds, item.row.start, item.row.span)
		horizontalAlignment := resolvedSelfAlignment(item.node.style.JustifySelf(), container.style.JustifyItems())
		verticalAlignment := resolvedSelfAlignment(item.node.style.AlignSelf(), container.style.AlignItems())
		subgrid := gridSubgridContextForItem(item, columnStarts, columnEnds, rowStarts, rowEnds, columnNames, rowNames)
		context.applySubgridEdgeInsets(item, subgrid, cellWidth)
		columnSubgrid := subgrid != nil && subgrid.columns != nil
		rowSubgrid := subgrid != nil && subgrid.rows != nil
		childContainingWidth := cellWidth
		if !columnSubgrid && !alignmentStretches(horizontalAlignment) && item.node.style.Width().Unit() == lengthAuto {
			intrinsic, intrinsicErr := context.gridItemIntrinsicWidths(item.node, cellWidth)
			if intrinsicErr != nil {
				return intrinsicErr
			}
			childContainingWidth = math.Min(cellWidth, intrinsic.preferred)
		}
		var forcedContentWidth *float64
		if columnSubgrid {
			padding := context.resolvePadding(item.node.style, cellWidth)
			border := context.resolveBorder(item.node.style, cellWidth)
			forced := math.Max(0, cellWidth-item.marginLeft-item.marginRight-padding.Left-padding.Right-border.Left-border.Right)
			forcedContentWidth = &forced
		}
		childBox, err := context.layoutBlockSizedWithSubgrid(item.node, 0, 0, childContainingWidth, &cellHeight, forcedContentWidth, true, subgrid, blockLayoutOverrides{})
		if err != nil {
			return err
		}
		availableHeight := math.Max(0, cellHeight-item.marginTop-item.marginBottom)
		if rowSubgrid || item.node.style.Height().Unit() == lengthAuto && alignmentStretches(verticalAlignment) {
			setBoxOuterHeight(childBox, availableHeight)
		}
		item.box = childBox
	}

	firstGroups, lastGroups := gridRowBaselineGroups(model.items, container.style.AlignItems())
	for index := range model.items {
		item := &model.items[index]
		childBox := item.box
		cellX := box.ContentBounds.X + columnStarts[item.column.start]
		cellY := box.ContentBounds.Y + rowStarts[item.row.start]
		cellWidth := gridTrackSpan(columnStarts, columnEnds, item.column.start, item.column.span)
		cellHeight := gridTrackSpan(rowStarts, rowEnds, item.row.start, item.row.span)
		horizontalAlignment := resolvedSelfAlignment(item.node.style.JustifySelf(), container.style.JustifyItems())
		horizontalOverflow := resolvedSelfOverflow(item.node.style.JustifySelf(), item.node.style.JustifySelfOverflow(), container.style.JustifyItemsOverflow())
		verticalAlignment := resolvedSelfAlignment(item.node.style.AlignSelf(), container.style.AlignItems())
		verticalOverflow := resolvedSelfOverflow(item.node.style.AlignSelf(), item.node.style.AlignSelfOverflow(), container.style.AlignItemsOverflow())
		availableHeight := math.Max(0, cellHeight-item.marginTop-item.marginBottom)
		verticalFree := availableHeight - childBox.Bounds.Height
		yOffset := 0.0
		switch verticalAlignment {
		case computed.AlignBaseline:
			distances := boxBaselineDistances(childBox, item.marginTop, item.marginBottom, false)
			yOffset = firstGroups[item.row.start].start - distances.start
		case computed.AlignLastBaseline:
			distances := boxBaselineDistances(childBox, item.marginTop, item.marginBottom, true)
			key := item.row.start + item.row.span - 1
			yOffset = cellHeight - lastGroups[key].end - distances.start
		default:
			yOffset = alignFlexOffset(verticalAlignment, verticalOverflow, verticalFree)
		}
		availableWidth := math.Max(0, cellWidth-item.marginLeft-item.marginRight)
		xOffset := alignFlexOffset(horizontalAlignment, horizontalOverflow, availableWidth-childBox.Bounds.Width)
		translateLayoutBox(childBox, cellX+xOffset, cellY+item.marginTop+yOffset-childBox.Bounds.Y)
		box.Children = append(box.Children, childBox)
		box.flow = append(box.flow, flowItem{box: childBox})
	}
	return nil
}

func gridRowBaselineGroups(items []gridLayoutItem, parentAlignment computed.AlignItems) (map[int]baselineAlignmentGroup, map[int]baselineAlignmentGroup) {
	var first map[int]baselineAlignmentGroup
	var last map[int]baselineAlignmentGroup
	for index := range items {
		item := &items[index]
		alignment := resolvedSelfAlignment(item.node.style.AlignSelf(), parentAlignment)
		switch alignment {
		case computed.AlignBaseline:
			if first == nil {
				first = make(map[int]baselineAlignmentGroup)
			}
			group := first[item.row.start]
			group.include(boxBaselineDistances(item.box, item.marginTop, item.marginBottom, false))
			first[item.row.start] = group
		case computed.AlignLastBaseline:
			if last == nil {
				last = make(map[int]baselineAlignmentGroup)
			}
			key := item.row.start + item.row.span - 1
			group := last[key]
			group.include(boxBaselineDistances(item.box, item.marginTop, item.marginBottom, true))
			last[key] = group
		}
	}
	return first, last
}

func gridTrackGeometry(sizes []float64, gap float64) ([]float64, []float64, float64) {
	available := sumFloat64(sizes) + gap*float64(max(0, len(sizes)-1))
	return alignedGridTrackGeometry(sizes, nil, gap, available, computed.JustifyStart, computed.OverflowAlignmentDefault)
}

func alignedGridTrackGeometry(sizes []float64, collapsed []bool, gap, available float64, alignment computed.JustifyContent, overflow computed.OverflowAlignment) ([]float64, []float64, float64) {
	starts := make([]float64, len(sizes))
	ends := make([]float64, len(sizes))
	used := sumFloat64(sizes) + gridGapTotal(collapsed, len(sizes), gap)
	visible := len(sizes)
	if len(collapsed) == len(sizes) {
		visible = 0
		for _, isCollapsed := range collapsed {
			if !isCollapsed {
				visible++
			}
		}
	}
	start, extraGap := gridContentSpace(alignment, overflow, available-used, visible)
	cursor := start
	for index, size := range sizes {
		starts[index] = cursor
		ends[index] = cursor + math.Max(0, size)
		cursor = ends[index]
		if gridGapAfter(collapsed, len(sizes), index) {
			cursor += gap
		}
		if !gridTrackIsCollapsed(collapsed, len(sizes), index) && gridHasVisibleTrackAfter(collapsed, len(sizes), index) {
			cursor += extraGap
		}
	}
	return starts, ends, cursor
}

func gridGapTotal(collapsed []bool, count int, gap float64) float64 {
	if gap <= 0 || count < 2 {
		return 0
	}
	total := 0.0
	for index := 0; index+1 < count; index++ {
		if gridGapAfter(collapsed, count, index) {
			total += gap
		}
	}
	return total
}

func gridGapAfter(collapsed []bool, count, index int) bool {
	if index < 0 || index+1 >= count {
		return false
	}
	return !gridTrackIsCollapsed(collapsed, count, index) && !gridTrackIsCollapsed(collapsed, count, index+1)
}

func gridTrackIsCollapsed(collapsed []bool, count, index int) bool {
	return len(collapsed) == count && index >= 0 && index < count && collapsed[index]
}

func gridHasVisibleTrackAfter(collapsed []bool, count, index int) bool {
	for candidate := index + 1; candidate < count; candidate++ {
		if !gridTrackIsCollapsed(collapsed, count, candidate) {
			return true
		}
	}
	return false
}

func gridContentSpace(alignment computed.JustifyContent, overflow computed.OverflowAlignment, free float64, count int) (start, extraGap float64) {
	if count == 0 {
		return 0, 0
	}
	if free < 0 {
		if overflow == computed.OverflowAlignmentSafe {
			return 0, 0
		}
		switch alignment {
		case computed.JustifyEnd, computed.JustifyFlexEnd:
			return free, 0
		case computed.JustifyCenter:
			return free / 2, 0
		default:
			return 0, 0
		}
	}
	if free == 0 {
		return 0, 0
	}
	switch alignment {
	case computed.JustifyEnd, computed.JustifyFlexEnd, computed.JustifyLastBaseline:
		return free, 0
	case computed.JustifyCenter:
		return free / 2, 0
	case computed.JustifySpaceBetween:
		if count > 1 {
			return 0, free / float64(count-1)
		}
	case computed.JustifySpaceAround:
		space := free / float64(count)
		return space / 2, space
	case computed.JustifySpaceEvenly:
		space := free / float64(count+1)
		return space, space
	}
	return 0, 0
}

func contentAlignmentStretches(alignment computed.JustifyContent) bool {
	return alignment == computed.JustifyNormal || alignment == computed.JustifyStretch
}

func gridTrackSpan(starts, ends []float64, start, span int) float64 {
	if start < 0 || span < 1 || start+span > len(ends) {
		return 0
	}
	return math.Max(0, ends[start+span-1]-starts[start])
}

func (context *layoutContext) intrinsicGridContentWidths(node *styledNode, availableWidth float64) (intrinsicWidths, error) {
	columnTemplate := node.style.GridTemplateColumns()
	if columnTemplate.IsSubgrid() {
		columnTemplate = computed.GridTrackList{}
	} else {
		columnTemplate = resolveGridAutoRepeat(columnTemplate, nil, false, 0, context.viewport)
	}
	rowTemplate := node.style.GridTemplateRows()
	if rowTemplate.IsSubgrid() {
		rowTemplate = computed.GridTrackList{}
	} else {
		rowTemplate = resolveGridAutoRepeat(rowTemplate, nil, false, 0, context.viewport)
	}
	model, err := context.buildGridLayoutModel(node, columnTemplate, rowTemplate, nil, false, false)
	if err != nil {
		return intrinsicWidths{}, err
	}
	gap := math.Max(0, resolveLength(node.style.ColumnGap(), availableWidth, context.viewport, 0))
	tracks := gridTrackSizes(model.columnAxis.template, node.style.GridAutoColumns(), model.columns, model.columnOffset)
	contributions := make([]gridTrackContribution, 0, len(model.items))
	for index := range model.items {
		item := &model.items[index]
		intrinsic, measureErr := context.gridItemIntrinsicWidths(item.node, availableWidth)
		if measureErr != nil {
			return intrinsicWidths{}, measureErr
		}
		contributions = append(contributions, gridTrackContribution{
			start:     item.column.start,
			span:      item.column.span,
			minimum:   intrinsic.minimum,
			preferred: intrinsic.preferred,
		})
	}
	states := initializeGridTrackStates(tracks, nil, nil, context.viewport)
	applyGridTrackContributions(states, contributions, gap)
	minimum := make([]float64, len(states))
	for index := range states {
		minimum[index] = states[index].base
	}
	preferred := indefiniteGridTrackSizes(states)
	gapTotal := gap * float64(max(0, len(tracks)-1))
	return intrinsicWidths{minimum: sumFloat64(minimum) + gapTotal, preferred: sumFloat64(preferred) + gapTotal}, nil
}

func expandIntrinsicGridFractions(sizes []float64, states []gridTrackState) {
	unit := 0.0
	for index, state := range states {
		if state.maxKind != computed.GridTrackFraction || state.fraction <= 0 {
			continue
		}
		candidate := sizes[index]
		if state.fraction > 1 {
			candidate /= state.fraction
		}
		unit = math.Max(unit, candidate)
	}
	for index, state := range states {
		if state.maxKind == computed.GridTrackFraction {
			sizes[index] = math.Max(sizes[index], unit*state.fraction)
		}
	}
}

func (context *layoutContext) gridItemIntrinsicWidths(node *styledNode, availableWidth float64) (intrinsicWidths, error) {
	intrinsic, err := context.intrinsicContentWidths(node, availableWidth)
	if err != nil {
		return intrinsicWidths{}, err
	}
	padding := context.resolvePadding(node.style, availableWidth)
	border := context.resolveBorder(node.style, availableWidth)
	margins := resolveLength(node.style.MarginLeft(), availableWidth, context.viewport, 0) +
		resolveLength(node.style.MarginRight(), availableWidth, context.viewport, 0)
	decoration := padding.Left + padding.Right + border.Left + border.Right
	intrinsic.minimum += decoration + margins
	intrinsic.preferred += decoration + margins
	if node.style.Width().Unit() != lengthAuto {
		specified := math.Max(0, resolveLength(node.style.Width(), availableWidth, context.viewport, 0)) + margins
		if node.style.BoxSizing() != boxSizingBorderBox {
			specified += decoration
		}
		intrinsic.minimum = math.Max(intrinsic.minimum, specified)
		intrinsic.preferred = math.Max(intrinsic.preferred, specified)
	}
	return intrinsic, nil
}
