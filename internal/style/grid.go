package style

import (
	"math"
	"strconv"
	"strings"

	"github.com/JediWattson/gossamer/internal/css"
)

const (
	maxGridTrackListEntries = 1024
	maxGridLineNames        = 8192
	maxGridTemplateCells    = 1_000_000
)

// GridTrackKind identifies one computed track-breadth form.
type GridTrackKind uint8

const (
	GridTrackAuto GridTrackKind = iota
	GridTrackLength
	GridTrackFraction
	GridTrackMinContent
	GridTrackMaxContent
)

// GridTrackSize is one immutable computed grid track sizing function. A bare
// breadth has the corresponding minimum and maximum, except a flexible breadth
// whose automatic minimum is retained explicitly. minmax() preserves both
// breadths and its authored range syntax for computed-value serialization.
type GridTrackSize struct {
	minKind      GridTrackKind
	minLength    Length
	maxKind      GridTrackKind
	maxLength    Length
	maxFraction  float64
	minmaxSyntax bool
	fitContent   bool
	fitLimit     Length
}

// Kind, Length, and Fraction expose the maximum breadth for compatibility with
// the first Grid slice. MinKind/MinLength expose the range minimum.
func (track GridTrackSize) Kind() GridTrackKind    { return track.maxKind }
func (track GridTrackSize) Length() Length         { return track.maxLength }
func (track GridTrackSize) Fraction() float64      { return track.maxFraction }
func (track GridTrackSize) MinKind() GridTrackKind { return track.minKind }
func (track GridTrackSize) MinLength() Length      { return track.minLength }
func (track GridTrackSize) IsMinMax() bool         { return track.minmaxSyntax }
func (track GridTrackSize) MaxKind() GridTrackKind { return track.maxKind }
func (track GridTrackSize) MaxLength() Length      { return track.maxLength }
func (track GridTrackSize) MaxFraction() float64   { return track.maxFraction }
func (track GridTrackSize) IsFitContent() bool     { return track.fitContent }
func (track GridTrackSize) FitContentLimit() Length {
	return track.fitLimit
}

// GridTrackList is an immutable explicit track list. Tracks returns a copy so
// style snapshots cannot be mutated through CSSOM or renderer callers.
type GridTrackList struct {
	tracks        []GridTrackSize
	lineNames     [][]string
	autoRepeat    *gridAutoRepeatDefinition
	subgridRepeat *gridSubgridAutoRepeatDefinition
	subgridNames  [][]string
	autoStart     int
	autoEnd       int
	subgrid       bool
	serialization string
}

type gridSubgridAutoRepeatDefinition struct {
	prefix, pattern, suffix [][]string
}

// GridAutoRepeatKind identifies the layout-dependent repeat-to-fill form
// retained by a computed track list.
type GridAutoRepeatKind uint8

const (
	GridAutoRepeatNone GridAutoRepeatKind = iota
	GridAutoRepeatFill
	GridAutoRepeatFit
)

type gridAutoRepeatDefinition struct {
	kind                    GridAutoRepeatKind
	prefix, pattern, suffix gridTrackListBuilder
}

// GridNamedArea is one rectangular region in a computed
// grid-template-areas value. End coordinates are exclusive grid lines.
type GridNamedArea struct {
	name                   string
	rowStart, rowEnd       int
	columnStart, columnEnd int
}

func (area GridNamedArea) Name() string     { return area.name }
func (area GridNamedArea) RowStart() int    { return area.rowStart }
func (area GridNamedArea) RowEnd() int      { return area.rowEnd }
func (area GridNamedArea) ColumnStart() int { return area.columnStart }
func (area GridNamedArea) ColumnEnd() int   { return area.columnEnd }

// GridTemplateAreas is an immutable rectangular named-area matrix. Empty
// strings in rows represent null cells.
type GridTemplateAreas struct {
	rows            [][]string
	columns         int
	areas           map[string]GridNamedArea
	areaOrder       []string
	columnLineNames [][]string
	rowLineNames    [][]string
	serialization   string
}

func (template GridTemplateAreas) Rows() int    { return len(template.rows) }
func (template GridTemplateAreas) Columns() int { return template.columns }

func (template GridTemplateAreas) Row(index int) []string {
	if index < 0 || index >= len(template.rows) {
		return nil
	}
	return append([]string(nil), template.rows[index]...)
}

func (template GridTemplateAreas) Area(name string) (GridNamedArea, bool) {
	area, ok := template.areas[name]
	return area, ok
}

func (template GridTemplateAreas) ColumnLineNames(line int) []string {
	if line < 0 || line >= len(template.columnLineNames) {
		return nil
	}
	return append([]string(nil), template.columnLineNames[line]...)
}

func (template GridTemplateAreas) RowLineNames(line int) []string {
	if line < 0 || line >= len(template.rowLineNames) {
		return nil
	}
	return append([]string(nil), template.rowLineNames[line]...)
}

func (list GridTrackList) Len() int { return len(list.tracks) }

func (list GridTrackList) At(index int) (GridTrackSize, bool) {
	if index < 0 || index >= len(list.tracks) {
		return GridTrackSize{}, false
	}
	return list.tracks[index], true
}

func (list GridTrackList) Tracks() []GridTrackSize {
	return append([]GridTrackSize(nil), list.tracks...)
}

// IsSubgrid reports whether this computed track list requests that the used
// axis adopt the tracks spanned in its parent grid.
func (list GridTrackList) IsSubgrid() bool { return list.subgrid }

// ResolvedSubgridLineNames expands the local line-name list to the used number
// of lines in a subgridded axis. Adopted parent names are intentionally absent:
// CSSOM's resolved serialization exposes only names declared on the subgrid.
func (list GridTrackList) ResolvedSubgridLineNames(lineCount int) [][]string {
	if !list.subgrid || lineCount < 1 || lineCount > maxGridTrackListEntries+1 {
		return nil
	}
	names := list.subgridNames
	if repeat := list.subgridRepeat; repeat != nil {
		count := 0
		remaining := lineCount - len(repeat.prefix) - len(repeat.suffix)
		if remaining > 0 && len(repeat.pattern) != 0 {
			count = remaining / len(repeat.pattern)
		}
		if gridLineNameSetCount(repeat.prefix)+count*gridLineNameSetCount(repeat.pattern)+gridLineNameSetCount(repeat.suffix) > maxGridLineNames {
			return nil
		}
		names = make([][]string, 0, len(repeat.prefix)+count*len(repeat.pattern)+len(repeat.suffix))
		names = appendGridLineNameSets(names, repeat.prefix)
		for range count {
			names = appendGridLineNameSets(names, repeat.pattern)
		}
		names = appendGridLineNameSets(names, repeat.suffix)
	}
	resolved := make([][]string, lineCount)
	for index := 0; index < min(lineCount, len(names)); index++ {
		resolved[index] = append([]string(nil), names[index]...)
	}
	return resolved
}

func gridLineNameSetCount(sets [][]string) int {
	count := 0
	for _, names := range sets {
		count += len(names)
	}
	return count
}

func appendGridLineNameSets(destination, source [][]string) [][]string {
	for _, names := range source {
		destination = append(destination, append([]string(nil), names...))
	}
	return destination
}

// LineNames returns a copy of the case-sensitive names assigned to one
// explicit grid line. A non-empty track list has Len()+1 lines.
func (list GridTrackList) LineNames(index int) []string {
	if index < 0 || index >= len(list.lineNames) {
		return nil
	}
	return append([]string(nil), list.lineNames[index]...)
}

// AutoRepeatKind reports whether this computed list contains auto-fill or
// auto-fit. The repetition count remains a used-value decision in layout.
func (list GridTrackList) AutoRepeatKind() GridAutoRepeatKind {
	if list.autoRepeat == nil {
		return GridAutoRepeatNone
	}
	return list.autoRepeat.kind
}

// AutoRepeatRange returns the half-open range occupied by the auto-repeated
// tracks in this expansion. Computed lists retain one repetition; a list
// returned by ExpandAutoRepeat retains the requested bounded count.
func (list GridTrackList) AutoRepeatRange() (int, int, bool) {
	return list.autoStart, list.autoEnd, list.autoRepeat != nil
}

// ExpandAutoRepeat mechanically expands the retained repeat-to-fill fragment.
// Layout owns selection of count; style only preserves immutable track and
// line-name semantics. Expansion fails closed at the existing track/name
// budgets.
func (list GridTrackList) ExpandAutoRepeat(count int) (GridTrackList, bool) {
	if list.autoRepeat == nil {
		return list, count == 1
	}
	if count < 1 {
		return GridTrackList{}, false
	}
	builder, ok := list.autoRepeat.expand(count)
	if !ok {
		return GridTrackList{}, false
	}
	start := len(list.autoRepeat.prefix.tracks)
	return GridTrackList{
		tracks: builder.tracks, lineNames: builder.lineNames,
		autoRepeat: list.autoRepeat, autoStart: start,
		autoEnd:       start + count*len(list.autoRepeat.pattern.tracks),
		serialization: list.serialization,
	}, true
}

// GridAutoFlowAxis is the major axis used by the auto-placement cursor.
type GridAutoFlowAxis uint8

const (
	GridAutoFlowRow GridAutoFlowAxis = iota
	GridAutoFlowColumn
)

type GridAutoFlow struct {
	axis  GridAutoFlowAxis
	dense bool
}

func (flow GridAutoFlow) Axis() GridAutoFlowAxis { return flow.axis }
func (flow GridAutoFlow) Dense() bool            { return flow.dense }

// GridLineKind distinguishes automatic, absolute-line, and span placement.
type GridLineKind uint8

const (
	GridLineAuto GridLineKind = iota
	GridLineNumber
	GridLineSpan
)

type GridLine struct {
	kind           GridLineKind
	number         int
	name           string
	numberExplicit bool
}

func (line GridLine) Kind() GridLineKind { return line.kind }
func (line GridLine) Number() int {
	if line.number == 0 && line.kind != GridLineAuto {
		return 1
	}
	return line.number
}
func (line GridLine) Name() string { return line.name }

// NumberExplicit reports whether the integer was written rather than supplied
// by the grid-line grammar's default occurrence of one.
func (line GridLine) NumberExplicit() bool { return line.numberExplicit }

type gridTrackListBuilder struct {
	tracks    []GridTrackSize
	lineNames [][]string
	nameCount int
}

func newGridTrackListBuilder(capacity int) gridTrackListBuilder {
	return gridTrackListBuilder{
		tracks:    make([]GridTrackSize, 0, capacity),
		lineNames: [][]string{{}},
	}
}

func (builder *gridTrackListBuilder) appendNames(names []string) bool {
	if builder.nameCount+len(names) > maxGridLineNames || len(builder.lineNames) == 0 {
		return false
	}
	last := len(builder.lineNames) - 1
	builder.lineNames[last] = append(builder.lineNames[last], names...)
	builder.nameCount += len(names)
	return true
}

func (builder *gridTrackListBuilder) appendTrack(track GridTrackSize) bool {
	if len(builder.tracks) >= maxGridTrackListEntries {
		return false
	}
	builder.tracks = append(builder.tracks, track)
	builder.lineNames = append(builder.lineNames, nil)
	return true
}

func (builder *gridTrackListBuilder) appendRepeated(repeated gridTrackListBuilder, count int) bool {
	if len(repeated.tracks) == 0 || count < 1 || count > maxGridTrackListEntries/len(repeated.tracks) ||
		len(builder.tracks)+count*len(repeated.tracks) > maxGridTrackListEntries ||
		repeated.nameCount != 0 && count > (maxGridLineNames-builder.nameCount)/repeated.nameCount {
		return false
	}
	for range count {
		if !builder.appendNames(repeated.lineNames[0]) {
			return false
		}
		for index, track := range repeated.tracks {
			if !builder.appendTrack(track) || !builder.appendNames(repeated.lineNames[index+1]) {
				return false
			}
		}
	}
	return true
}

func (builder *gridTrackListBuilder) appendBuilder(other gridTrackListBuilder) bool {
	if len(other.lineNames) == 0 || !builder.appendNames(other.lineNames[0]) {
		return false
	}
	for index, track := range other.tracks {
		if !builder.appendTrack(track) || !builder.appendNames(other.lineNames[index+1]) {
			return false
		}
	}
	return true
}

func cloneGridTrackListBuilder(source gridTrackListBuilder) gridTrackListBuilder {
	cloned := gridTrackListBuilder{
		tracks:    append([]GridTrackSize(nil), source.tracks...),
		lineNames: make([][]string, len(source.lineNames)),
		nameCount: source.nameCount,
	}
	for index := range source.lineNames {
		cloned.lineNames[index] = append([]string(nil), source.lineNames[index]...)
	}
	return cloned
}

func (definition *gridAutoRepeatDefinition) expand(count int) (gridTrackListBuilder, bool) {
	if definition == nil || count < 1 || len(definition.pattern.tracks) == 0 {
		return gridTrackListBuilder{}, false
	}
	builder := cloneGridTrackListBuilder(definition.prefix)
	if !builder.appendRepeated(definition.pattern, count) || !builder.appendBuilder(definition.suffix) {
		return gridTrackListBuilder{}, false
	}
	return builder, true
}

func parseGridTrackList(source string, fontSize float64, viewport Viewport) (GridTrackList, bool) {
	value, ok := parsePropertyValue(source)
	if !ok || len(value.terms) == 0 {
		return GridTrackList{}, false
	}
	if len(value.terms) == 1 {
		if keyword, keywordOK := componentKeyword(value.terms[0]); keywordOK && keyword == "none" {
			return GridTrackList{serialization: "none"}, true
		}
	}
	if keyword, keywordOK := componentKeyword(value.terms[0]); keywordOK && keyword == "subgrid" {
		return parseSubgridTrackList(value.terms[1:])
	}
	builder, automatic, ok := parseExplicitGridTrackSequence(value.terms, value.source, fontSize, viewport)
	if !ok || len(builder.tracks) == 0 {
		return GridTrackList{}, false
	}
	serialization, ok := canonicalGridTrackList(value.terms, value.source, fontSize, viewport)
	if !ok {
		return GridTrackList{}, false
	}
	list := GridTrackList{tracks: builder.tracks, lineNames: builder.lineNames, serialization: serialization}
	if automatic != nil {
		list.autoRepeat = automatic
		list.autoStart = len(automatic.prefix.tracks)
		list.autoEnd = list.autoStart + len(automatic.pattern.tracks)
	}
	return list, true
}

func parseSubgridTrackList(terms []css.ComponentValue) (GridTrackList, bool) {
	list := GridTrackList{subgrid: true, serialization: "subgrid"}
	if len(terms) == 0 {
		return list, true
	}
	current := make([][]string, 0, len(terms))
	parts := make([]string, 0, len(terms)+1)
	parts = append(parts, "subgrid")
	nameCount := 0
	for _, term := range terms {
		if names, ok := parseGridLineNameSet(term); ok {
			if len(current) >= maxGridTrackListEntries+1 || nameCount+len(names) > maxGridLineNames {
				return GridTrackList{}, false
			}
			current = append(current, append([]string(nil), names...))
			nameCount += len(names)
			parts = append(parts, serializeGridLineNameSet(names))
			continue
		}
		if term.Kind != css.ComponentFunction || lowerASCIIValue(term.Token.Value) != "repeat" {
			return GridTrackList{}, false
		}
		repeat, ok := gridRepeatParts(term)
		if !ok || repeat.kind == GridAutoRepeatFit || repeat.kind == GridAutoRepeatFill && list.subgridRepeat != nil {
			return GridTrackList{}, false
		}
		pattern := make([][]string, 0, len(repeat.body))
		patternNames := 0
		bodyParts := make([]string, 0, len(repeat.body))
		for _, component := range repeat.body {
			names, namesOK := parseGridLineNameSet(component)
			if !namesOK {
				return GridTrackList{}, false
			}
			pattern = append(pattern, append([]string(nil), names...))
			patternNames += len(names)
			bodyParts = append(bodyParts, serializeGridLineNameSet(names))
		}
		if len(pattern) == 0 {
			return GridTrackList{}, false
		}
		count := strconv.Itoa(repeat.count)
		if repeat.kind == GridAutoRepeatFill {
			if nameCount+patternNames > maxGridLineNames {
				return GridTrackList{}, false
			}
			count = "auto-fill"
			list.subgridRepeat = &gridSubgridAutoRepeatDefinition{
				prefix:  appendGridLineNameSets(nil, current),
				pattern: pattern,
			}
			current = nil
		} else {
			if repeat.count > (maxGridTrackListEntries+1-len(current))/len(pattern) ||
				patternNames != 0 && repeat.count > (maxGridLineNames-nameCount)/patternNames {
				return GridTrackList{}, false
			}
			for range repeat.count {
				current = appendGridLineNameSets(current, pattern)
			}
			nameCount += repeat.count * patternNames
		}
		parts = append(parts, "repeat("+count+", "+strings.Join(bodyParts, " ")+")")
	}
	if list.subgridRepeat != nil {
		list.subgridRepeat.suffix = appendGridLineNameSets(nil, current)
		if len(list.subgridRepeat.prefix)+len(list.subgridRepeat.suffix) > maxGridTrackListEntries+1 {
			return GridTrackList{}, false
		}
	} else {
		list.subgridNames = appendGridLineNameSets(nil, current)
	}
	list.serialization = strings.Join(parts, " ")
	return list, true
}

func parseExplicitGridTrackSequence(components []css.ComponentValue, source string, fontSize float64, viewport Viewport) (gridTrackListBuilder, *gridAutoRepeatDefinition, bool) {
	prefix := newGridTrackListBuilder(len(components))
	current := &prefix
	var automatic *gridAutoRepeatDefinition
	previousNames := false
	for _, component := range components {
		if names, ok := parseGridLineNameSet(component); ok {
			if previousNames || !current.appendNames(names) {
				return gridTrackListBuilder{}, nil, false
			}
			previousNames = true
			continue
		}
		if component.Kind == css.ComponentBlock && component.Token.Kind == css.TokenOpenSquare {
			return gridTrackListBuilder{}, nil, false
		}
		previousNames = false
		if component.Kind == css.ComponentFunction && lowerASCIIValue(component.Token.Value) == "repeat" {
			repeat, ok := gridRepeatParts(component)
			if !ok {
				return gridTrackListBuilder{}, nil, false
			}
			body, ok := parseGridTrackSequence(repeat.body, source, fontSize, viewport, false)
			if !ok {
				return gridTrackListBuilder{}, nil, false
			}
			if repeat.kind == GridAutoRepeatNone {
				if !current.appendRepeated(body, repeat.count) {
					return gridTrackListBuilder{}, nil, false
				}
				continue
			}
			if automatic != nil || !gridAutoRepeatTracksAreFixed(body.tracks) {
				return gridTrackListBuilder{}, nil, false
			}
			automatic = &gridAutoRepeatDefinition{
				kind: repeat.kind, prefix: cloneGridTrackListBuilder(prefix),
				pattern: body, suffix: newGridTrackListBuilder(len(components)),
			}
			current = &automatic.suffix
			continue
		}
		track, ok := parseGridTrackSizeComponent(component, source, fontSize, viewport)
		if !ok || !current.appendTrack(track) {
			return gridTrackListBuilder{}, nil, false
		}
	}
	if automatic == nil {
		return prefix, nil, len(prefix.tracks) != 0
	}
	if !gridAutoRepeatTracksAreFixed(automatic.prefix.tracks) ||
		!gridAutoRepeatTracksAreFixed(automatic.suffix.tracks) {
		return gridTrackListBuilder{}, nil, false
	}
	expanded, ok := automatic.expand(1)
	return expanded, automatic, ok
}

func defaultGridAutoTrackList() GridTrackList {
	return GridTrackList{tracks: []GridTrackSize{{}}, serialization: "auto"}
}

func parseGridAutoTrackList(source string, fontSize float64, viewport Viewport) (GridTrackList, bool) {
	value, ok := parsePropertyValue(source)
	if !ok || len(value.terms) == 0 || len(value.terms) > maxGridTrackListEntries {
		return GridTrackList{}, false
	}
	tracks := make([]GridTrackSize, 0, len(value.terms))
	parts := make([]string, 0, len(value.terms))
	for _, term := range value.terms {
		parsed, parsedOK := parseGridTrackSizeComponent(term, value.source, fontSize, viewport)
		if !parsedOK {
			return GridTrackList{}, false
		}
		tracks = append(tracks, parsed)
		parts = append(parts, serializeGridTrackSize(parsed))
	}
	return GridTrackList{tracks: tracks, serialization: strings.Join(parts, " ")}, true
}

func parseGridTrackSequence(components []css.ComponentValue, source string, fontSize float64, viewport Viewport, allowRepeat bool) (gridTrackListBuilder, bool) {
	builder := newGridTrackListBuilder(len(components))
	previousNames := false
	for _, component := range components {
		if names, ok := parseGridLineNameSet(component); ok {
			if previousNames || !builder.appendNames(names) {
				return gridTrackListBuilder{}, false
			}
			previousNames = true
			continue
		}
		if component.Kind == css.ComponentBlock && component.Token.Kind == css.TokenOpenSquare {
			return gridTrackListBuilder{}, false
		}
		previousNames = false
		if component.Kind == css.ComponentFunction && lowerASCIIValue(component.Token.Value) == "repeat" {
			if !allowRepeat {
				return gridTrackListBuilder{}, false
			}
			repeat, ok := gridRepeatParts(component)
			if !ok || repeat.kind != GridAutoRepeatNone {
				return gridTrackListBuilder{}, false
			}
			repeated, ok := parseGridTrackSequence(repeat.body, source, fontSize, viewport, false)
			if !ok || !builder.appendRepeated(repeated, repeat.count) {
				return gridTrackListBuilder{}, false
			}
			continue
		}
		track, ok := parseGridTrackSizeComponent(component, source, fontSize, viewport)
		if !ok || !builder.appendTrack(track) {
			return gridTrackListBuilder{}, false
		}
	}
	return builder, len(builder.tracks) != 0
}

func parseGridLineNameSet(component css.ComponentValue) ([]string, bool) {
	if component.Kind != css.ComponentBlock || component.Token.Kind != css.TokenOpenSquare {
		return nil, false
	}
	names := make([]string, 0, len(component.Values))
	for _, child := range component.Values {
		if valueWhitespace(child) {
			continue
		}
		name, ok := gridCustomIdentifier(child)
		if !ok || len(names) >= maxGridLineNames {
			return nil, false
		}
		names = append(names, name)
	}
	return names, true
}

type gridTrackBreadth struct {
	kind     GridTrackKind
	length   Length
	fraction float64
}

func parseGridTrackSizeComponent(component css.ComponentValue, source string, fontSize float64, viewport Viewport) (GridTrackSize, bool) {
	if component.Kind == css.ComponentFunction && lowerASCIIValue(component.Token.Value) == "fit-content" {
		values := nonWhitespaceComponents(trimValueWhitespace(component.Values))
		if len(values) != 1 {
			return GridTrackSize{}, false
		}
		limit, ok := parseLengthComponent(values[0], source, fontSize, viewport)
		if !ok || limit.unit == lengthAuto || !nonNegativeLength(limit) {
			return GridTrackSize{}, false
		}
		track := gridTrackRange(gridTrackBreadth{kind: GridTrackAuto}, gridTrackBreadth{kind: GridTrackMaxContent}, false)
		track.fitContent = true
		track.fitLimit = limit
		return track, true
	}
	if component.Kind == css.ComponentFunction && lowerASCIIValue(component.Token.Value) == "minmax" {
		minimumComponent, maximumComponent, ok := gridMinMaxParts(component)
		if !ok {
			return GridTrackSize{}, false
		}
		minimum, minimumOK := parseGridTrackBreadth(minimumComponent, source, fontSize, viewport, false)
		maximum, maximumOK := parseGridTrackBreadth(maximumComponent, source, fontSize, viewport, true)
		if !minimumOK || !maximumOK {
			return GridTrackSize{}, false
		}
		return gridTrackRange(minimum, maximum, true), true
	}
	breadth, ok := parseGridTrackBreadth(component, source, fontSize, viewport, true)
	if !ok {
		return GridTrackSize{}, false
	}
	minimum := breadth
	if breadth.kind == GridTrackFraction {
		minimum = gridTrackBreadth{kind: GridTrackAuto}
	}
	return gridTrackRange(minimum, breadth, false), true
}

func parseGridTrackBreadth(component css.ComponentValue, source string, fontSize float64, viewport Viewport, allowFlex bool) (gridTrackBreadth, bool) {
	if keyword, ok := componentKeyword(component); ok {
		switch keyword {
		case "auto":
			return gridTrackBreadth{kind: GridTrackAuto}, true
		case "min-content":
			return gridTrackBreadth{kind: GridTrackMinContent}, true
		case "max-content":
			return gridTrackBreadth{kind: GridTrackMaxContent}, true
		default:
			return gridTrackBreadth{}, false
		}
	}
	if token, ok := componentToken(component); ok && token.Kind == css.TokenDimension && lowerASCIIValue(token.Value) == "fr" {
		if !allowFlex || !isFinite(token.Number) || token.Number < 0 {
			return gridTrackBreadth{}, false
		}
		return gridTrackBreadth{kind: GridTrackFraction, fraction: token.Number}, true
	}
	parsed, ok := parseLengthComponent(component, source, fontSize, viewport)
	if !ok || parsed.unit == lengthAuto || !nonNegativeLength(parsed) {
		return gridTrackBreadth{}, false
	}
	return gridTrackBreadth{kind: GridTrackLength, length: parsed}, true
}

func gridTrackRange(minimum, maximum gridTrackBreadth, minmaxSyntax bool) GridTrackSize {
	return GridTrackSize{
		minKind:      minimum.kind,
		minLength:    minimum.length,
		maxKind:      maximum.kind,
		maxLength:    maximum.length,
		maxFraction:  maximum.fraction,
		minmaxSyntax: minmaxSyntax,
	}
}

func gridMinMaxParts(component css.ComponentValue) (css.ComponentValue, css.ComponentValue, bool) {
	values := trimValueWhitespace(component.Values)
	comma := -1
	for index, candidate := range values {
		token, ok := componentToken(candidate)
		if !ok || token.Kind != css.TokenComma {
			continue
		}
		if comma >= 0 {
			return css.ComponentValue{}, css.ComponentValue{}, false
		}
		comma = index
	}
	if comma < 0 {
		return css.ComponentValue{}, css.ComponentValue{}, false
	}
	minimum := nonWhitespaceComponents(values[:comma])
	maximum := nonWhitespaceComponents(values[comma+1:])
	if len(minimum) != 1 || len(maximum) != 1 {
		return css.ComponentValue{}, css.ComponentValue{}, false
	}
	return minimum[0], maximum[0], true
}

type gridRepeatSpec struct {
	kind  GridAutoRepeatKind
	count int
	body  []css.ComponentValue
}

func gridRepeatParts(component css.ComponentValue) (gridRepeatSpec, bool) {
	values := trimValueWhitespace(component.Values)
	comma := -1
	for index, candidate := range values {
		if token, ok := componentToken(candidate); ok && token.Kind == css.TokenComma {
			comma = index
			break
		}
	}
	if comma < 1 {
		return gridRepeatSpec{}, false
	}
	countTerms := nonWhitespaceComponents(values[:comma])
	body := nonWhitespaceComponents(values[comma+1:])
	if len(countTerms) != 1 || len(body) == 0 {
		return gridRepeatSpec{}, false
	}
	if keyword, ok := componentKeyword(countTerms[0]); ok {
		kind := GridAutoRepeatNone
		switch keyword {
		case "auto-fill":
			kind = GridAutoRepeatFill
		case "auto-fit":
			kind = GridAutoRepeatFit
		default:
			return gridRepeatSpec{}, false
		}
		return gridRepeatSpec{kind: kind, count: 1, body: body}, true
	}
	countToken, ok := componentToken(countTerms[0])
	if !ok || countToken.Kind != css.TokenNumber || !countToken.Integer || countToken.Number < 1 || countToken.Number > maxGridTrackListEntries {
		return gridRepeatSpec{}, false
	}
	return gridRepeatSpec{count: int(countToken.Number), body: body}, true
}

func gridAutoRepeatTracksAreFixed(tracks []GridTrackSize) bool {
	for _, track := range tracks {
		if track.fitContent || track.minKind != GridTrackLength && track.maxKind != GridTrackLength {
			return false
		}
	}
	return true
}

func canonicalGridTrackList(terms []css.ComponentValue, source string, fontSize float64, viewport Viewport) (string, bool) {
	parts := make([]string, 0, len(terms))
	for _, term := range terms {
		if names, ok := parseGridLineNameSet(term); ok {
			parts = append(parts, serializeGridLineNameSet(names))
			continue
		}
		if term.Kind == css.ComponentFunction && lowerASCIIValue(term.Token.Value) == "repeat" {
			repeat, ok := gridRepeatParts(term)
			if !ok {
				return "", false
			}
			bodyParts := make([]string, 0, len(repeat.body))
			for _, child := range repeat.body {
				if names, namesOK := parseGridLineNameSet(child); namesOK {
					bodyParts = append(bodyParts, serializeGridLineNameSet(names))
					continue
				}
				track, trackOK := parseGridTrackSizeComponent(child, source, fontSize, viewport)
				if !trackOK {
					return "", false
				}
				bodyParts = append(bodyParts, serializeGridTrackSize(track))
			}
			count := strconv.Itoa(repeat.count)
			if repeat.kind == GridAutoRepeatFill {
				count = "auto-fill"
			} else if repeat.kind == GridAutoRepeatFit {
				count = "auto-fit"
			}
			parts = append(parts, "repeat("+count+", "+strings.Join(bodyParts, " ")+")")
			continue
		}
		track, ok := parseGridTrackSizeComponent(term, source, fontSize, viewport)
		if !ok {
			return "", false
		}
		parts = append(parts, serializeGridTrackSize(track))
	}
	return strings.Join(parts, " "), len(parts) != 0
}

func nonWhitespaceComponents(values []css.ComponentValue) []css.ComponentValue {
	result := make([]css.ComponentValue, 0, len(values))
	for _, value := range values {
		if !valueWhitespace(value) {
			result = append(result, value)
		}
	}
	return result
}

func parseGridTemplateAreas(source string) (GridTemplateAreas, bool) {
	value, ok := parsePropertyValue(source)
	if !ok || len(value.terms) == 0 {
		return GridTemplateAreas{}, false
	}
	if len(value.terms) == 1 {
		if keyword, keywordOK := componentKeyword(value.terms[0]); keywordOK && keyword == "none" {
			return GridTemplateAreas{serialization: "none"}, true
		}
	}
	if len(value.terms) > maxGridTrackListEntries {
		return GridTemplateAreas{}, false
	}
	template := GridTemplateAreas{
		rows:  make([][]string, 0, len(value.terms)),
		areas: make(map[string]GridNamedArea),
	}
	serializedRows := make([]string, 0, len(value.terms))
	for rowIndex, term := range value.terms {
		token, tokenOK := componentToken(term)
		if !tokenOK || token.Kind != css.TokenString || token.Incomplete {
			return GridTemplateAreas{}, false
		}
		row, rowOK := parseGridTemplateAreaRow(token.Value)
		if !rowOK || len(row) > maxGridTrackListEntries ||
			template.columns != 0 && len(row) != template.columns {
			return GridTemplateAreas{}, false
		}
		if template.columns == 0 {
			template.columns = len(row)
		}
		if template.columns > maxGridTemplateCells/(len(template.rows)+1) {
			return GridTemplateAreas{}, false
		}
		template.rows = append(template.rows, row)
		serializedRows = append(serializedRows, serializeGridTemplateAreaRow(row))
		for columnIndex, name := range row {
			if name == "" {
				continue
			}
			area, exists := template.areas[name]
			if !exists {
				area = GridNamedArea{
					name: name, rowStart: rowIndex, rowEnd: rowIndex + 1,
					columnStart: columnIndex, columnEnd: columnIndex + 1,
				}
				template.areaOrder = append(template.areaOrder, name)
			} else {
				area.rowStart = min(area.rowStart, rowIndex)
				area.rowEnd = max(area.rowEnd, rowIndex+1)
				area.columnStart = min(area.columnStart, columnIndex)
				area.columnEnd = max(area.columnEnd, columnIndex+1)
			}
			template.areas[name] = area
		}
	}
	for name, area := range template.areas {
		for row := area.rowStart; row < area.rowEnd; row++ {
			for column := area.columnStart; column < area.columnEnd; column++ {
				if template.rows[row][column] != name {
					return GridTemplateAreas{}, false
				}
			}
		}
	}
	if len(template.areaOrder) > maxGridLineNames/4 {
		return GridTemplateAreas{}, false
	}
	template.columnLineNames = make([][]string, template.columns+1)
	template.rowLineNames = make([][]string, len(template.rows)+1)
	for _, name := range template.areaOrder {
		area := template.areas[name]
		template.columnLineNames[area.columnStart] = append(template.columnLineNames[area.columnStart], name+"-start")
		template.columnLineNames[area.columnEnd] = append(template.columnLineNames[area.columnEnd], name+"-end")
		template.rowLineNames[area.rowStart] = append(template.rowLineNames[area.rowStart], name+"-start")
		template.rowLineNames[area.rowEnd] = append(template.rowLineNames[area.rowEnd], name+"-end")
	}
	template.serialization = strings.Join(serializedRows, " ")
	return template, true
}

func parseGridTemplateAreaRow(source string) ([]string, bool) {
	runes := []rune(source)
	row := make([]string, 0, len(runes))
	for position := 0; position < len(runes); {
		candidate := runes[position]
		switch {
		case gridAreaWhitespace(candidate):
			position++
		case candidate == '.':
			for position < len(runes) && runes[position] == '.' {
				position++
			}
			row = append(row, "")
		case gridAreaIdentCodePoint(candidate):
			start := position
			for position < len(runes) && gridAreaIdentCodePoint(runes[position]) {
				position++
			}
			row = append(row, string(runes[start:position]))
		default:
			return nil, false
		}
		if len(row) > maxGridTrackListEntries {
			return nil, false
		}
	}
	return row, len(row) != 0
}

func gridAreaWhitespace(candidate rune) bool {
	return candidate == '\t' || candidate == '\n' || candidate == '\f' || candidate == '\r' || candidate == ' '
}

func gridAreaIdentCodePoint(candidate rune) bool {
	return candidate >= 'a' && candidate <= 'z' || candidate >= 'A' && candidate <= 'Z' ||
		candidate >= '0' && candidate <= '9' || candidate == '-' || candidate == '_' || candidate >= 0x80
}

func serializeGridTemplateAreaRow(row []string) string {
	serialized := make([]string, len(row))
	for index, name := range row {
		serialized[index] = name
		if name == "" {
			serialized[index] = "."
		}
	}
	return `"` + strings.Join(serialized, " ") + `"`
}

func serializeGridTemplateAreas(template GridTemplateAreas) string {
	if template.serialization != "" {
		return template.serialization
	}
	return "none"
}

func parseGridAutoFlow(source string) (GridAutoFlow, bool) {
	value, ok := parsePropertyValue(source)
	if !ok || len(value.terms) < 1 || len(value.terms) > 2 {
		return GridAutoFlow{}, false
	}
	flow := GridAutoFlow{axis: GridAutoFlowRow}
	seenAxis, seenDense := false, false
	for _, term := range value.terms {
		keyword, keywordOK := componentKeyword(term)
		if !keywordOK {
			return GridAutoFlow{}, false
		}
		switch keyword {
		case "row":
			if seenAxis {
				return GridAutoFlow{}, false
			}
			flow.axis, seenAxis = GridAutoFlowRow, true
		case "column":
			if seenAxis {
				return GridAutoFlow{}, false
			}
			flow.axis, seenAxis = GridAutoFlowColumn, true
		case "dense":
			if seenDense {
				return GridAutoFlow{}, false
			}
			flow.dense, seenDense = true, true
		default:
			return GridAutoFlow{}, false
		}
	}
	return flow, true
}

func parseGridLine(source string) (GridLine, bool) {
	value, ok := parsePropertyValue(source)
	if !ok {
		return GridLine{}, false
	}
	return parseGridLineTerms(value.terms)
}

func parseGridLineTerms(terms []css.ComponentValue) (GridLine, bool) {
	if len(terms) == 1 {
		if keyword, ok := componentKeyword(terms[0]); ok && keyword == "auto" {
			return GridLine{kind: GridLineAuto}, true
		}
	}
	if len(terms) == 0 || len(terms) > 3 {
		return GridLine{}, false
	}
	span, number, numberOK, name := false, 1, false, ""
	for _, term := range terms {
		if keyword, ok := componentKeyword(term); ok && keyword == "span" {
			if span {
				return GridLine{}, false
			}
			span = true
			continue
		}
		if candidate, ok := gridInteger(term); ok {
			if numberOK || candidate == 0 {
				return GridLine{}, false
			}
			number, numberOK = candidate, true
			continue
		}
		candidate, ok := gridCustomIdentifier(term)
		if !ok || name != "" {
			return GridLine{}, false
		}
		name = candidate
	}
	if span {
		if !numberOK && name == "" || number < 1 {
			return GridLine{}, false
		}
		return GridLine{kind: GridLineSpan, number: number, name: name, numberExplicit: numberOK}, true
	}
	if !numberOK && name == "" {
		return GridLine{}, false
	}
	return GridLine{kind: GridLineNumber, number: number, name: name, numberExplicit: numberOK}, true
}

func gridCustomIdentifier(component css.ComponentValue) (string, bool) {
	token, ok := componentToken(component)
	if !ok || token.Kind != css.TokenIdent {
		return "", false
	}
	switch lowerASCIIValue(token.Value) {
	case "auto", "span", "default", "initial", "inherit", "unset", "revert", "revert-layer":
		return "", false
	default:
		return token.Value, token.Value != ""
	}
}

func gridInteger(component css.ComponentValue) (int, bool) {
	token, ok := componentToken(component)
	if !ok || token.Kind != css.TokenNumber || !token.Integer || !isFinite(token.Number) || token.Number < math.MinInt32 || token.Number > math.MaxInt32 {
		return 0, false
	}
	return int(token.Number), true
}

func parseGridLineShorthand(source string) (GridLine, GridLine, bool) {
	value, ok := parsePropertyValue(source)
	if !ok || len(value.terms) == 0 {
		return GridLine{}, GridLine{}, false
	}
	slash := -1
	for index, term := range value.terms {
		token, tokenOK := componentToken(term)
		if !tokenOK || token.Kind != css.TokenDelim || token.Value != "/" {
			continue
		}
		if slash >= 0 {
			return GridLine{}, GridLine{}, false
		}
		slash = index
	}
	if slash < 0 {
		start, startOK := parseGridLineTerms(value.terms)
		end := GridLine{kind: GridLineAuto}
		if startOK && start.kind == GridLineNumber && start.name != "" && !start.numberExplicit {
			end = start
		}
		return start, end, startOK
	}
	if slash == 0 || slash == len(value.terms)-1 {
		return GridLine{}, GridLine{}, false
	}
	start, startOK := parseGridLineTerms(value.terms[:slash])
	end, endOK := parseGridLineTerms(value.terms[slash+1:])
	return start, end, startOK && endOK
}

func parseGridAreaShorthand(source string) (GridLine, GridLine, GridLine, GridLine, bool) {
	value, ok := parsePropertyValue(source)
	if !ok || len(value.terms) == 0 {
		return GridLine{}, GridLine{}, GridLine{}, GridLine{}, false
	}
	segments := make([][]css.ComponentValue, 1, 4)
	for _, term := range value.terms {
		token, tokenOK := componentToken(term)
		if tokenOK && token.Kind == css.TokenDelim && token.Value == "/" {
			if len(segments[len(segments)-1]) == 0 || len(segments) == 4 {
				return GridLine{}, GridLine{}, GridLine{}, GridLine{}, false
			}
			segments = append(segments, nil)
			continue
		}
		segments[len(segments)-1] = append(segments[len(segments)-1], term)
	}
	if len(segments[len(segments)-1]) == 0 {
		return GridLine{}, GridLine{}, GridLine{}, GridLine{}, false
	}
	parsed := make([]GridLine, len(segments))
	for index := range segments {
		parsed[index], ok = parseGridLineTerms(segments[index])
		if !ok {
			return GridLine{}, GridLine{}, GridLine{}, GridLine{}, false
		}
	}
	automatic := GridLine{kind: GridLineAuto}
	rowStart := parsed[0]
	columnStart := automatic
	if len(parsed) > 1 {
		columnStart = parsed[1]
	} else if gridLineIsCustomIdent(rowStart) {
		columnStart = rowStart
	}
	rowEnd := automatic
	if len(parsed) > 2 {
		rowEnd = parsed[2]
	} else if gridLineIsCustomIdent(rowStart) {
		rowEnd = rowStart
	}
	columnEnd := automatic
	if len(parsed) > 3 {
		columnEnd = parsed[3]
	} else if gridLineIsCustomIdent(columnStart) {
		columnEnd = columnStart
	}
	return rowStart, columnStart, rowEnd, columnEnd, true
}

func gridLineIsCustomIdent(line GridLine) bool {
	return line.kind == GridLineNumber && line.name != "" && !line.numberExplicit
}

func serializeGridTrackSize(track GridTrackSize) string {
	if track.fitContent {
		return "fit-content(" + serializeComputedLength(track.fitLimit) + ")"
	}
	if track.minmaxSyntax {
		return "minmax(" + serializeGridTrackBreadth(track.minKind, track.minLength, 0) + ", " +
			serializeGridTrackBreadth(track.maxKind, track.maxLength, track.maxFraction) + ")"
	}
	return serializeGridTrackBreadth(track.maxKind, track.maxLength, track.maxFraction)
}

func serializeGridTrackBreadth(kind GridTrackKind, length Length, fraction float64) string {
	switch kind {
	case GridTrackLength:
		return serializeComputedLength(length)
	case GridTrackFraction:
		return serializeComputedNumber(fraction) + "fr"
	case GridTrackMinContent:
		return "min-content"
	case GridTrackMaxContent:
		return "max-content"
	default:
		return "auto"
	}
}

func serializeGridTrackList(list GridTrackList) string {
	if list.serialization != "" {
		return list.serialization
	}
	if len(list.tracks) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(list.tracks)*2+1)
	for index, track := range list.tracks {
		if names := list.LineNames(index); len(names) != 0 {
			parts = append(parts, serializeGridLineNameSet(names))
		}
		parts = append(parts, serializeGridTrackSize(track))
	}
	if names := list.LineNames(len(list.tracks)); len(names) != 0 {
		parts = append(parts, serializeGridLineNameSet(names))
	}
	return strings.Join(parts, " ")
}

func serializeGridLineNameSet(names []string) string {
	serialized := make([]string, len(names))
	for index, name := range names {
		serialized[index] = serializeGridIdentifier(name)
	}
	return "[" + strings.Join(serialized, " ") + "]"
}

// SerializeGridLineNames returns the canonical bracketed serialization used by
// resolved grid track listings. Names are case-sensitive decoded identifiers.
func SerializeGridLineNames(names []string) string {
	return serializeGridLineNameSet(names)
}

func serializeGridIdentifier(identifier string) string {
	runes := []rune(identifier)
	var result strings.Builder
	result.Grow(len(identifier))
	for index, candidate := range runes {
		switch {
		case candidate == 0:
			result.WriteRune('\uFFFD')
		case candidate >= 1 && candidate <= 0x1f || candidate == 0x7f ||
			index == 0 && candidate >= '0' && candidate <= '9' ||
			index == 1 && runes[0] == '-' && candidate >= '0' && candidate <= '9':
			result.WriteByte('\\')
			result.WriteString(strconv.FormatInt(int64(candidate), 16))
			result.WriteByte(' ')
		case index == 0 && candidate == '-' && len(runes) == 1:
			result.WriteString("\\-")
		case candidate >= 0x80 || candidate == '-' || candidate == '_' ||
			candidate >= '0' && candidate <= '9' ||
			candidate >= 'A' && candidate <= 'Z' || candidate >= 'a' && candidate <= 'z':
			result.WriteRune(candidate)
		default:
			result.WriteByte('\\')
			result.WriteRune(candidate)
		}
	}
	return result.String()
}

func serializeGridAutoFlow(flow GridAutoFlow) string {
	result := "row"
	if flow.axis == GridAutoFlowColumn {
		result = "column"
	}
	if flow.dense {
		result += " dense"
	}
	return result
}

func serializeGridLine(line GridLine) string {
	switch line.kind {
	case GridLineNumber:
		parts := make([]string, 0, 2)
		if line.numberExplicit || line.name == "" {
			parts = append(parts, strconv.Itoa(line.Number()))
		}
		if line.name != "" {
			parts = append(parts, serializeGridIdentifier(line.name))
		}
		return strings.Join(parts, " ")
	case GridLineSpan:
		parts := []string{"span"}
		if line.numberExplicit || line.name == "" {
			parts = append(parts, strconv.Itoa(line.Number()))
		}
		if line.name != "" {
			parts = append(parts, serializeGridIdentifier(line.name))
		}
		return strings.Join(parts, " ")
	default:
		return "auto"
	}
}
