package style

import (
	"math"
	"strconv"
	"strings"

	"github.com/JediWattson/gossamer/internal/css"
)

const maxGridTrackListEntries = 1024

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
	serialization string
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
// Named lines are intentionally deferred until template line names arrive.
type GridLineKind uint8

const (
	GridLineAuto GridLineKind = iota
	GridLineNumber
	GridLineSpan
)

type GridLine struct {
	kind   GridLineKind
	number int
}

func (line GridLine) Kind() GridLineKind { return line.kind }
func (line GridLine) Number() int        { return line.number }

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
	tracks := make([]GridTrackSize, 0, len(value.terms))
	for _, term := range value.terms {
		if !appendGridTrackComponent(&tracks, term, value.source, fontSize, viewport, true) {
			return GridTrackList{}, false
		}
	}
	if len(tracks) == 0 || len(tracks) > maxGridTrackListEntries {
		return GridTrackList{}, false
	}
	serialization, ok := canonicalGridTrackList(value.terms, value.source, fontSize, viewport)
	if !ok {
		return GridTrackList{}, false
	}
	return GridTrackList{tracks: tracks, serialization: serialization}, true
}

func parseGridAutoTrack(source string, fontSize float64, viewport Viewport) (GridTrackSize, bool) {
	value, ok := parsePropertyValue(source)
	if !ok || len(value.terms) != 1 {
		return GridTrackSize{}, false
	}
	tracks := make([]GridTrackSize, 0, 1)
	if !appendGridTrackComponent(&tracks, value.terms[0], value.source, fontSize, viewport, false) || len(tracks) != 1 {
		return GridTrackSize{}, false
	}
	return tracks[0], true
}

func appendGridTrackComponent(tracks *[]GridTrackSize, component css.ComponentValue, source string, fontSize float64, viewport Viewport, allowRepeat bool) bool {
	if len(*tracks) >= maxGridTrackListEntries {
		return false
	}
	if component.Kind == css.ComponentFunction && lowerASCIIValue(component.Token.Value) == "repeat" {
		if !allowRepeat {
			return false
		}
		count, body, ok := gridRepeatParts(component)
		if !ok {
			return false
		}
		if count > maxGridTrackListEntries/len(body) || len(*tracks)+count*len(body) > maxGridTrackListEntries {
			return false
		}
		parsed := make([]GridTrackSize, 0, len(body))
		for _, child := range body {
			if !appendGridTrackComponent(&parsed, child, source, fontSize, viewport, false) {
				return false
			}
		}
		for range count {
			*tracks = append(*tracks, parsed...)
		}
		return true
	}
	parsed, ok := parseGridTrackSizeComponent(component, source, fontSize, viewport)
	if !ok {
		return false
	}
	*tracks = append(*tracks, parsed)
	return true
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

func gridRepeatParts(component css.ComponentValue) (int, []css.ComponentValue, bool) {
	values := trimValueWhitespace(component.Values)
	comma := -1
	for index, candidate := range values {
		if token, ok := componentToken(candidate); ok && token.Kind == css.TokenComma {
			comma = index
			break
		}
	}
	if comma < 1 {
		return 0, nil, false
	}
	countTerms := nonWhitespaceComponents(values[:comma])
	body := nonWhitespaceComponents(values[comma+1:])
	if len(countTerms) != 1 || len(body) == 0 {
		return 0, nil, false
	}
	countToken, ok := componentToken(countTerms[0])
	if !ok || countToken.Kind != css.TokenNumber || !countToken.Integer || countToken.Number < 1 || countToken.Number > maxGridTrackListEntries {
		return 0, nil, false
	}
	return int(countToken.Number), body, true
}

func canonicalGridTrackList(terms []css.ComponentValue, source string, fontSize float64, viewport Viewport) (string, bool) {
	parts := make([]string, 0, len(terms))
	for _, term := range terms {
		if term.Kind == css.ComponentFunction && lowerASCIIValue(term.Token.Value) == "repeat" {
			count, body, ok := gridRepeatParts(term)
			if !ok {
				return "", false
			}
			bodyParts := make([]string, 0, len(body))
			for _, child := range body {
				tracks := make([]GridTrackSize, 0, 1)
				if !appendGridTrackComponent(&tracks, child, source, fontSize, viewport, false) || len(tracks) != 1 {
					return "", false
				}
				bodyParts = append(bodyParts, serializeGridTrackSize(tracks[0]))
			}
			parts = append(parts, "repeat("+strconv.Itoa(count)+", "+strings.Join(bodyParts, " ")+")")
			continue
		}
		tracks := make([]GridTrackSize, 0, 1)
		if !appendGridTrackComponent(&tracks, term, source, fontSize, viewport, false) || len(tracks) != 1 {
			return "", false
		}
		parts = append(parts, serializeGridTrackSize(tracks[0]))
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
		if number, ok := gridInteger(terms[0]); ok && number != 0 {
			return GridLine{kind: GridLineNumber, number: number}, true
		}
		return GridLine{}, false
	}
	if len(terms) == 2 {
		keyword, keywordIndex := "", -1
		number, numberOK := 0, false
		for index, term := range terms {
			if candidate, ok := componentKeyword(term); ok {
				keyword, keywordIndex = candidate, index
				continue
			}
			if candidate, ok := gridInteger(term); ok {
				number, numberOK = candidate, true
				continue
			}
			return GridLine{}, false
		}
		if keyword == "span" && keywordIndex >= 0 && numberOK && number > 0 {
			return GridLine{kind: GridLineSpan, number: number}, true
		}
	}
	return GridLine{}, false
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
		return start, GridLine{kind: GridLineAuto}, startOK
	}
	if slash == 0 || slash == len(value.terms)-1 {
		return GridLine{}, GridLine{}, false
	}
	start, startOK := parseGridLineTerms(value.terms[:slash])
	end, endOK := parseGridLineTerms(value.terms[slash+1:])
	return start, end, startOK && endOK
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
	parts := make([]string, len(list.tracks))
	for index, track := range list.tracks {
		parts[index] = serializeGridTrackSize(track)
	}
	return strings.Join(parts, " ")
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
		return strconv.Itoa(line.number)
	case GridLineSpan:
		return "span " + strconv.Itoa(line.number)
	default:
		return "auto"
	}
}
