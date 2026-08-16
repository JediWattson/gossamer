package style

import (
	"math"
	"strconv"
	"strings"

	"github.com/JediWattson/gossamer/internal/css"
)

const maxGridTrackListEntries = 1024

// GridTrackKind identifies the supported computed track-breadth forms. This
// first Grid slice retains auto, typed length-percentage, and flexible tracks;
// minmax and intrinsic sizing keywords remain future track functions.
type GridTrackKind uint8

const (
	GridTrackAuto GridTrackKind = iota
	GridTrackLength
	GridTrackFraction
)

// GridTrackSize is one immutable computed grid track breadth.
type GridTrackSize struct {
	kind     GridTrackKind
	length   Length
	fraction float64
}

func (track GridTrackSize) Kind() GridTrackKind { return track.kind }
func (track GridTrackSize) Length() Length      { return track.length }
func (track GridTrackSize) Fraction() float64   { return track.fraction }

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
	if keyword, ok := componentKeyword(component); ok {
		if keyword == "auto" {
			*tracks = append(*tracks, GridTrackSize{kind: GridTrackAuto})
			return true
		}
		return false
	}
	if token, ok := componentToken(component); ok && token.Kind == css.TokenDimension && lowerASCIIValue(token.Value) == "fr" {
		if !isFinite(token.Number) || token.Number < 0 {
			return false
		}
		*tracks = append(*tracks, GridTrackSize{kind: GridTrackFraction, fraction: token.Number})
		return true
	}
	parsed, ok := parseLengthComponent(component, source, fontSize, viewport)
	if !ok || parsed.unit == lengthAuto || !nonNegativeLength(parsed) {
		return false
	}
	*tracks = append(*tracks, GridTrackSize{kind: GridTrackLength, length: parsed})
	return true
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
	switch track.kind {
	case GridTrackLength:
		return serializeComputedLength(track.length)
	case GridTrackFraction:
		return serializeComputedNumber(track.fraction) + "fr"
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
