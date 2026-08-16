package render

import (
	"sort"
	"strings"
	"unicode/utf8"

	computed "github.com/JediWattson/gossamer/internal/style"
)

// unicodeVerticalOrientation is the normative UAX #50 Vertical_Orientation
// property. The renderer currently synthesizes vertical metrics instead of
// applying OpenType vert/vrt2 substitutions, so Tu follows its upright
// fallback and Tr follows its rotated fallback.
type unicodeVerticalOrientation uint8

const (
	unicodeVerticalRotated unicodeVerticalOrientation = iota
	unicodeVerticalUpright
	unicodeVerticalTransformedUpright
	unicodeVerticalTransformedRotated
)

type unicodeVerticalOrientationRange struct {
	first       rune
	last        rune
	orientation unicodeVerticalOrientation
}

type graphemeBreakProperty uint8

const (
	graphemeBreakOther graphemeBreakProperty = iota
	graphemeBreakCR
	graphemeBreakLF
	graphemeBreakControl
	graphemeBreakExtend
	graphemeBreakZWJ
	graphemeBreakRegionalIndicator
	graphemeBreakPrepend
	graphemeBreakSpacingMark
	graphemeBreakL
	graphemeBreakV
	graphemeBreakT
	graphemeBreakLV
	graphemeBreakLVT
)

type unicodeGraphemeBreakRange struct {
	first    rune
	last     rune
	property graphemeBreakProperty
}

type unicodeCodePointRange struct {
	first rune
	last  rune
}

type indicConjunctBreak uint8

const (
	indicConjunctNone indicConjunctBreak = iota
	indicConjunctConsonant
	indicConjunctExtend
	indicConjunctLinker
)

type unicodeIndicConjunctBreakRange struct {
	first    rune
	last     rune
	property indicConjunctBreak
}

type verticalDecodedRune struct {
	value rune
	start int
}

func verticalOrientationForRune(value rune) unicodeVerticalOrientation {
	index := sort.Search(len(unicodeVerticalOrientationRanges), func(index int) bool {
		return unicodeVerticalOrientationRanges[index].last >= value
	})
	if index < len(unicodeVerticalOrientationRanges) {
		candidate := unicodeVerticalOrientationRanges[index]
		if candidate.first <= value {
			return candidate.orientation
		}
	}
	return unicodeVerticalRotated
}

type verticalTextRun struct {
	text        string
	orientation textPaintOrientation
	units       int
}

func verticalTextRuns(source string, orientation computed.TextOrientation) []verticalTextRun {
	if source == "" {
		return nil
	}
	clusters := splitVerticalTextUnits(source)
	runs := make([]verticalTextRun, 0, len(clusters))
	var runText strings.Builder
	current := textPaintHorizontal
	units := 0
	flush := func() {
		if units == 0 {
			return
		}
		runs = append(runs, verticalTextRun{text: runText.String(), orientation: current, units: units})
		runText.Reset()
		units = 0
	}
	for _, cluster := range clusters {
		paint := textPaintSidewaysRight
		switch orientation {
		case computed.TextOrientationUpright:
			paint = textPaintUpright
		case computed.TextOrientationSideways:
			paint = textPaintSidewaysRight
		default:
			value, _ := utf8.DecodeRuneInString(cluster)
			property := verticalOrientationForRune(value)
			if property == unicodeVerticalUpright || property == unicodeVerticalTransformedUpright {
				paint = textPaintUpright
			}
		}
		if units != 0 && current != paint {
			flush()
		}
		current = paint
		runText.WriteString(cluster)
		units++
	}
	flush()
	return runs
}

// splitVerticalTextUnits implements the Unicode 17 extended-grapheme boundary
// rules from UAX #29. It intentionally returns byte slices of the original
// string so invalid UTF-8, DOM text, and paint commands remain lossless.
func splitVerticalTextUnits(source string) []string {
	if source == "" {
		return nil
	}
	decoded := make([]verticalDecodedRune, 0, utf8.RuneCountInString(source))
	for index, value := range source {
		decoded = append(decoded, verticalDecodedRune{value: value, start: index})
	}
	boundaries := []int{0}
	regionalCount := 0
	if len(decoded) != 0 && graphemeBreakForRune(decoded[0].value) == graphemeBreakRegionalIndicator {
		regionalCount = 1
	}
	for index := 1; index < len(decoded); index++ {
		previous := graphemeBreakForRune(decoded[index-1].value)
		current := graphemeBreakForRune(decoded[index].value)
		shouldBreak := shouldBreakVerticalTextUnit(decoded, index, previous, current, regionalCount)
		if shouldBreak {
			boundaries = append(boundaries, decoded[index].start)
		}
		if current == graphemeBreakRegionalIndicator {
			if shouldBreak || previous != graphemeBreakRegionalIndicator {
				regionalCount = 1
			} else {
				regionalCount++
			}
		} else {
			regionalCount = 0
		}
	}
	boundaries = append(boundaries, len(source))
	result := make([]string, 0, len(boundaries)-1)
	for index := 0; index+1 < len(boundaries); index++ {
		result = append(result, source[boundaries[index]:boundaries[index+1]])
	}
	return result
}

func shouldBreakVerticalTextUnit(decoded []verticalDecodedRune, index int, previous, current graphemeBreakProperty, regionalCount int) bool {
	// GB3.
	if previous == graphemeBreakCR && current == graphemeBreakLF {
		return false
	}
	// GB4 and GB5.
	if isGraphemeControl(previous) || isGraphemeControl(current) {
		return true
	}
	// GB6, GB7, and GB8.
	if previous == graphemeBreakL && (current == graphemeBreakL || current == graphemeBreakV || current == graphemeBreakLV || current == graphemeBreakLVT) {
		return false
	}
	if (previous == graphemeBreakLV || previous == graphemeBreakV) && (current == graphemeBreakV || current == graphemeBreakT) {
		return false
	}
	if (previous == graphemeBreakLVT || previous == graphemeBreakT) && current == graphemeBreakT {
		return false
	}
	// GB9, GB9a, and GB9b.
	if current == graphemeBreakExtend || current == graphemeBreakZWJ || current == graphemeBreakSpacingMark || previous == graphemeBreakPrepend {
		return false
	}
	// GB9c.
	if indicConjunctForRune(decoded[index].value) == indicConjunctConsonant && hasIndicConjunctBefore(decoded, index) {
		return false
	}
	// GB11.
	if isExtendedPictographic(decoded[index].value) && previous == graphemeBreakZWJ && hasExtendedPictographicZWJBefore(decoded, index) {
		return false
	}
	// GB12 and GB13.
	if previous == graphemeBreakRegionalIndicator && current == graphemeBreakRegionalIndicator {
		return regionalCount%2 == 0
	}
	return true
}

func isGraphemeControl(property graphemeBreakProperty) bool {
	return property == graphemeBreakCR || property == graphemeBreakLF || property == graphemeBreakControl
}

func graphemeBreakForRune(value rune) graphemeBreakProperty {
	index := sort.Search(len(unicodeGraphemeBreakRanges), func(index int) bool {
		return unicodeGraphemeBreakRanges[index].last >= value
	})
	if index < len(unicodeGraphemeBreakRanges) {
		candidate := unicodeGraphemeBreakRanges[index]
		if candidate.first <= value {
			return candidate.property
		}
	}
	return graphemeBreakOther
}

func indicConjunctForRune(value rune) indicConjunctBreak {
	index := sort.Search(len(unicodeIndicConjunctBreakRanges), func(index int) bool {
		return unicodeIndicConjunctBreakRanges[index].last >= value
	})
	if index < len(unicodeIndicConjunctBreakRanges) {
		candidate := unicodeIndicConjunctBreakRanges[index]
		if candidate.first <= value {
			return candidate.property
		}
	}
	return indicConjunctNone
}

func isExtendedPictographic(value rune) bool {
	index := sort.Search(len(unicodeExtendedPictographicRanges), func(index int) bool {
		return unicodeExtendedPictographicRanges[index].last >= value
	})
	return index < len(unicodeExtendedPictographicRanges) && unicodeExtendedPictographicRanges[index].first <= value
}

func hasIndicConjunctBefore(decoded []verticalDecodedRune, index int) bool {
	sawLinker := false
	for position := index - 1; position >= 0; position-- {
		switch indicConjunctForRune(decoded[position].value) {
		case indicConjunctLinker:
			sawLinker = true
		case indicConjunctExtend:
		default:
			return sawLinker && indicConjunctForRune(decoded[position].value) == indicConjunctConsonant
		}
	}
	return false
}

func hasExtendedPictographicZWJBefore(decoded []verticalDecodedRune, index int) bool {
	// The immediate predecessor is the ZWJ tested by the caller.
	for position := index - 2; position >= 0; position-- {
		if graphemeBreakForRune(decoded[position].value) == graphemeBreakExtend {
			continue
		}
		return isExtendedPictographic(decoded[position].value)
	}
	return false
}
