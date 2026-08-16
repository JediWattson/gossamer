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

type gridLayoutModel struct {
	items        []gridLayoutItem
	columns      int
	rows         int
	explicitCols int
	explicitRows int
	columnTracks computed.GridTrackList
	rowTracks    computed.GridTrackList
	columnOffset int
	rowOffset    int
	placementOps int
	occupied     map[gridCell]struct{}
}

type gridCell struct {
	row    int
	column int
}

func (context *layoutContext) layoutGridContainer(node *styledNode, box *Box, contentWidth float64, definiteHeight *float64) (float64, error) {
	model, err := context.buildGridLayoutModel(node)
	if err != nil {
		return 0, err
	}
	columnGap := math.Max(0, resolveLength(node.style.ColumnGap(), contentWidth, context.viewport, 0))
	rowGap := math.Max(0, resolveLength(node.style.RowGap(), contentWidth, context.viewport, 0))
	columnTracks := gridTrackSizes(node.style.GridTemplateColumns(), node.style.GridAutoColumns(), model.columns, model.columnOffset)
	columnSizes, err := context.sizeGridColumns(model.items, columnTracks, contentWidth, columnGap, contentAlignmentStretches(node.style.JustifyContent()))
	if err != nil {
		return 0, err
	}
	columnStarts, columnEnds, _ := alignedGridTrackGeometry(columnSizes, columnGap, contentWidth, node.style.JustifyContent())
	box.gridColumnSizes = append([]float64(nil), columnSizes...)
	box.gridColumnLineNames = gridUsedLineNames(node.style.GridTemplateColumns(), len(columnSizes), model.columnOffset)

	// The first item pass obtains max-content row contributions. Once rows are
	// sized, a second bounded pass supplies each area's definite height so
	// percentage heights and stretch use the actual grid-area containing block.
	if err := context.measureGridItems(&model, contentWidth, columnStarts, columnEnds); err != nil {
		return 0, err
	}
	rowTracks := gridTrackSizes(node.style.GridTemplateRows(), node.style.GridAutoRows(), model.rows, model.rowOffset)
	rowSizes := context.sizeGridRows(model.items, rowTracks, definiteHeight, rowGap, contentAlignmentStretches(node.style.AlignContent()))
	box.gridRowSizes = append([]float64(nil), rowSizes...)
	box.gridRowLineNames = gridUsedLineNames(node.style.GridTemplateRows(), len(rowSizes), model.rowOffset)
	rowAvailable := sumFloat64(rowSizes) + rowGap*float64(max(0, len(rowSizes)-1))
	if definiteHeight != nil {
		rowAvailable = *definiteHeight
	}
	rowStarts, rowEnds, gridHeight := alignedGridTrackGeometry(rowSizes, rowGap, rowAvailable, node.style.AlignContent())
	if err := context.placeGridItems(node, box, &model, columnStarts, columnEnds, rowStarts, rowEnds); err != nil {
		return 0, err
	}
	return gridHeight, nil
}

func (context *layoutContext) buildGridLayoutModel(container *styledNode) (gridLayoutModel, error) {
	model := gridLayoutModel{occupied: make(map[gridCell]struct{})}
	model.items = context.gridItems(container)
	if len(model.items) > maxGridItems {
		return gridLayoutModel{}, fmt.Errorf("render: grid exceeds %d items", maxGridItems)
	}
	model.explicitCols = container.style.GridTemplateColumns().Len()
	model.explicitRows = container.style.GridTemplateRows().Len()
	model.columnTracks = container.style.GridTemplateColumns()
	model.rowTracks = container.style.GridTemplateRows()
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
		item.column = resolveGridAxis(item.node.style.GridColumnStart(), item.node.style.GridColumnEnd(), model.columnTracks)
		item.row = resolveGridAxis(item.node.style.GridRowStart(), item.node.style.GridRowEnd(), model.rowTracks)
		if item.column.definite {
			minColumn = min(minColumn, item.column.start)
		}
		if item.row.definite {
			minRow = min(minRow, item.row.start)
		}
	}
	model.columnOffset = -minColumn
	model.rowOffset = -minRow
	model.columns += model.columnOffset
	model.rows += model.rowOffset
	for index := range model.items {
		item := &model.items[index]
		if item.column.definite {
			item.column.start += model.columnOffset
			model.columns = max(model.columns, item.column.start+item.column.span)
		}
		if item.row.definite {
			item.row.start += model.rowOffset
			model.rows = max(model.rows, item.row.start+item.row.span)
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
			return gridLayoutModel{}, err
		}
		item.row.start, item.row.definite = row, true
		item.column.start, item.column.definite = column, true
		item.row.span, item.column.span = model.itemSpansAt(*item, row, column)
		model.rows = max(model.rows, row+item.row.span)
		model.columns = max(model.columns, column+item.column.span)
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
			return gridLayoutModel{}, err
		}
		item.row.start, item.row.definite = row, true
		item.column.start, item.column.definite = column, true
		item.row.span, item.column.span = model.itemSpansAt(*item, row, column)
		model.rows = max(model.rows, row+item.row.span)
		model.columns = max(model.columns, column+item.column.span)
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
			blockified.style = child.style.WithAnonymousDisplay(computed.DisplayBlock)
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

func resolveGridAxis(startLine, endLine computed.GridLine, template computed.GridTrackList) gridAxisPlacement {
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
	start, hasStart := gridLineCoordinate(startLine, template, true)
	end, hasEnd := gridLineCoordinate(endLine, template, false)
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
		end = gridSpanCoordinate(start, 1, endLine, template)
		return gridAxisPlacement{definite: true, start: start, span: max(1, end-start)}
	case hasEnd && startLine.Kind() == computed.GridLineSpan:
		start = gridSpanCoordinate(end, -1, startLine, template)
		return gridAxisPlacement{definite: true, start: start, span: max(1, end-start)}
	case hasStart:
		return gridAxisPlacement{definite: true, start: start, span: span}
	case hasEnd:
		return gridAxisPlacement{definite: true, start: end - span, span: span}
	default:
		return gridAxisPlacement{span: span}
	}
}

func gridLineCoordinate(line computed.GridLine, template computed.GridTrackList, startEdge bool) (int, bool) {
	if line.Kind() != computed.GridLineNumber {
		return 0, false
	}
	explicitTracks := template.Len()
	if line.Name() != "" {
		name := line.Name()
		if !line.NumberExplicit() {
			areaName := name + "-end"
			if startEdge {
				areaName = name + "-start"
			}
			if coordinate, ok := explicitNamedGridLine(template, areaName, 1); ok {
				return coordinate, true
			}
		}
		return namedGridLineCoordinate(template, name, line.Number()), true
	}
	if line.Number() > 0 {
		return line.Number() - 1, true
	}
	return explicitTracks + 1 + line.Number(), true
}

func explicitNamedGridLine(template computed.GridTrackList, name string, occurrence int) (int, bool) {
	if occurrence == 0 {
		return 0, false
	}
	if occurrence > 0 {
		for line := 0; line <= template.Len(); line++ {
			if gridLineHasName(template, line, name) {
				occurrence--
				if occurrence == 0 {
					return line, true
				}
			}
		}
		return 0, false
	}
	for line := template.Len(); line >= 0; line-- {
		if gridLineHasName(template, line, name) {
			occurrence++
			if occurrence == 0 {
				return line, true
			}
		}
	}
	return 0, false
}

func namedGridLineCoordinate(template computed.GridTrackList, name string, occurrence int) int {
	if coordinate, ok := explicitNamedGridLine(template, name, occurrence); ok {
		return coordinate
	}
	if occurrence > 0 {
		matches := 0
		for line := 0; line <= template.Len(); line++ {
			if gridLineHasName(template, line, name) {
				matches++
			}
		}
		return template.Len() + occurrence - matches
	}
	matches := 0
	for line := template.Len(); line >= 0; line-- {
		if gridLineHasName(template, line, name) {
			matches++
		}
	}
	return occurrence + matches
}

func gridSpanCoordinate(opposite, direction int, line computed.GridLine, template computed.GridTrackList) int {
	occurrence := max(1, line.Number())
	return namedGridSpanCoordinate(opposite, direction, occurrence, line.Name(), template)
}

func namedGridSpanCoordinate(opposite, direction, occurrence int, name string, template computed.GridTrackList) int {
	if name == "" {
		return opposite + direction*occurrence
	}
	remaining := occurrence
	for candidate := opposite + direction; candidate >= -maxGridTracksPerAxis && candidate <= maxGridTracksPerAxis*2; candidate += direction {
		implicitMatch := direction > 0 && candidate > template.Len() || direction < 0 && candidate < 0
		if implicitMatch || candidate >= 0 && candidate <= template.Len() && gridLineHasName(template, candidate, name) {
			remaining--
			if remaining == 0 {
				return candidate
			}
		}
	}
	return opposite + direction*occurrence
}

func gridLineHasName(template computed.GridTrackList, line int, name string) bool {
	for _, candidate := range template.LineNames(line) {
		if candidate == name {
			return true
		}
	}
	return false
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

func (context *layoutContext) sizeGridColumns(items []gridLayoutItem, tracks []computed.GridTrackSize, availableWidth, gap float64, stretchAuto bool) ([]float64, error) {
	contributions := make([]gridTrackContribution, 0, len(items))
	for index := range items {
		item := &items[index]
		intrinsic, err := context.gridItemIntrinsicWidths(item.node, availableWidth)
		if err != nil {
			return nil, err
		}
		contributions = append(contributions, gridTrackContribution{
			start:     item.column.start,
			span:      item.column.span,
			minimum:   intrinsic.minimum,
			preferred: intrinsic.preferred,
		})
	}
	height := availableWidth
	return sizeGridTrackAxis(tracks, contributions, &height, gap, context.viewport, stretchAuto), nil
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
}

func sizeGridTrackAxis(tracks []computed.GridTrackSize, contributions []gridTrackContribution, available *float64, gap float64, viewport Viewport, stretchAuto bool) []float64 {
	states := initializeGridTrackStates(tracks, available, viewport)
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

func initializeGridTrackStates(tracks []computed.GridTrackSize, available *float64, viewport Viewport) []gridTrackState {
	states := make([]gridTrackState, len(tracks))
	percentageBase := 0.0
	if available != nil {
		percentageBase = *available
	}
	for index, track := range tracks {
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
	current := sumFloat64(values[start:start+span]) + gap*float64(max(0, span-1))
	deficit := contribution - current
	if deficit <= 0 {
		return
	}
	indices := make([]int, 0, span)
	for index := start; index < start+span; index++ {
		if eligible(states[index]) {
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
	free := available - gap*float64(max(0, len(sizes)-1)) - sumFloat64(sizes)
	if free <= 0 {
		return
	}
	active := make([]int, 0, len(states))
	for index, state := range states {
		if state.maxKind != computed.GridTrackFraction && state.limit > sizes[index] {
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
	gapTotal := gap * float64(max(0, len(sizes)-1))
	nonFrTotal := 0.0
	active := make([]int, 0, len(states))
	for index, state := range states {
		if state.maxKind == computed.GridTrackFraction && state.fraction > 0 {
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
	gapTotal := gap * float64(max(0, len(sizes)-1))
	used := sumFloat64(sizes) + gapTotal
	leftover := available - used
	if leftover <= 0 {
		return
	}
	autoCount := 0
	for _, state := range states {
		if state.maxKind == computed.GridTrackAuto {
			autoCount++
		}
	}
	if autoCount == 0 {
		return
	}
	addition := leftover / float64(autoCount)
	for index, state := range states {
		if state.maxKind == computed.GridTrackAuto {
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

func (context *layoutContext) measureGridItems(model *gridLayoutModel, contentWidth float64, columnStarts, columnEnds []float64) error {
	for index := range model.items {
		item := &model.items[index]
		cellWidth := gridTrackSpan(columnStarts, columnEnds, item.column.start, item.column.span)
		box, err := context.layoutBlockSized(item.node, 0, 0, cellWidth, nil, nil, true)
		if err != nil {
			return err
		}
		item.box = box
		item.marginTop = resolveLength(item.node.style.MarginTop(), contentWidth, context.viewport, 0)
		item.marginRight = resolveLength(item.node.style.MarginRight(), contentWidth, context.viewport, 0)
		item.marginBottom = resolveLength(item.node.style.MarginBottom(), contentWidth, context.viewport, 0)
		item.marginLeft = resolveLength(item.node.style.MarginLeft(), contentWidth, context.viewport, 0)
	}
	return nil
}

func (context *layoutContext) sizeGridRows(items []gridLayoutItem, tracks []computed.GridTrackSize, definiteHeight *float64, gap float64, stretchAuto bool) []float64 {
	contributions := make([]gridTrackContribution, 0, len(items))
	for index := range items {
		item := &items[index]
		contribution := item.box.Bounds.Height + item.marginTop + item.marginBottom
		contributions = append(contributions, gridTrackContribution{
			start:     item.row.start,
			span:      item.row.span,
			minimum:   contribution,
			preferred: contribution,
		})
	}
	return sizeGridTrackAxis(tracks, contributions, definiteHeight, gap, context.viewport, stretchAuto)
}

func (context *layoutContext) placeGridItems(container *styledNode, box *Box, model *gridLayoutModel, columnStarts, columnEnds, rowStarts, rowEnds []float64) error {
	for index := range model.items {
		item := &model.items[index]
		cellX := box.ContentBounds.X + columnStarts[item.column.start]
		cellY := box.ContentBounds.Y + rowStarts[item.row.start]
		cellWidth := gridTrackSpan(columnStarts, columnEnds, item.column.start, item.column.span)
		cellHeight := gridTrackSpan(rowStarts, rowEnds, item.row.start, item.row.span)
		horizontalAlignment := resolvedSelfAlignment(item.node.style.JustifySelf(), container.style.JustifyItems())
		verticalAlignment := resolvedSelfAlignment(item.node.style.AlignSelf(), container.style.AlignItems())
		childContainingWidth := cellWidth
		if !alignmentStretches(horizontalAlignment) && item.node.style.Width().Unit() == lengthAuto {
			intrinsic, intrinsicErr := context.gridItemIntrinsicWidths(item.node, cellWidth)
			if intrinsicErr != nil {
				return intrinsicErr
			}
			childContainingWidth = math.Min(cellWidth, intrinsic.preferred)
		}
		childBox, err := context.layoutBlockSized(item.node, 0, 0, childContainingWidth, &cellHeight, nil, true)
		if err != nil {
			return err
		}
		availableHeight := math.Max(0, cellHeight-item.marginTop-item.marginBottom)
		if item.node.style.Height().Unit() == lengthAuto && alignmentStretches(verticalAlignment) {
			setBoxOuterHeight(childBox, availableHeight)
		}
		verticalFree := math.Max(0, availableHeight-childBox.Bounds.Height)
		yOffset := alignFlexOffset(verticalAlignment, verticalFree)
		availableWidth := math.Max(0, cellWidth-item.marginLeft-item.marginRight)
		xOffset := alignFlexOffset(horizontalAlignment, math.Max(0, availableWidth-childBox.Bounds.Width))
		translateLayoutBox(childBox, cellX+xOffset, cellY+item.marginTop+yOffset-childBox.Bounds.Y)
		item.box = childBox
		box.Children = append(box.Children, childBox)
		box.flow = append(box.flow, flowItem{box: childBox})
	}
	return nil
}

func gridTrackGeometry(sizes []float64, gap float64) ([]float64, []float64, float64) {
	available := sumFloat64(sizes) + gap*float64(max(0, len(sizes)-1))
	return alignedGridTrackGeometry(sizes, gap, available, computed.JustifyStart)
}

func alignedGridTrackGeometry(sizes []float64, gap, available float64, alignment computed.JustifyContent) ([]float64, []float64, float64) {
	starts := make([]float64, len(sizes))
	ends := make([]float64, len(sizes))
	used := sumFloat64(sizes) + gap*float64(max(0, len(sizes)-1))
	start, extraGap := gridContentSpace(alignment, math.Max(0, available-used), len(sizes))
	cursor := start
	for index, size := range sizes {
		starts[index] = cursor
		ends[index] = cursor + math.Max(0, size)
		cursor = ends[index]
		if index+1 < len(sizes) {
			cursor += gap + extraGap
		}
	}
	return starts, ends, cursor
}

func gridContentSpace(alignment computed.JustifyContent, free float64, count int) (start, extraGap float64) {
	if count == 0 || free <= 0 {
		return 0, 0
	}
	switch alignment {
	case computed.JustifyEnd, computed.JustifyFlexEnd:
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
	model, err := context.buildGridLayoutModel(node)
	if err != nil {
		return intrinsicWidths{}, err
	}
	gap := math.Max(0, resolveLength(node.style.ColumnGap(), availableWidth, context.viewport, 0))
	tracks := gridTrackSizes(node.style.GridTemplateColumns(), node.style.GridAutoColumns(), model.columns, model.columnOffset)
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
	states := initializeGridTrackStates(tracks, nil, context.viewport)
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
