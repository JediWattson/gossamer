package css

import (
	"math"
	"strconv"
	"strings"
)

// MediaEnvironment describes the media features currently exposed by a
// Gossamer rendering target. An empty Type is treated as screen.
type MediaEnvironment struct {
	Type            string
	Width           float64
	Height          float64
	InitialFontSize float64
}

type mediaTruth uint8

const (
	mediaFalse mediaTruth = iota
	mediaTrue
	mediaUnknown
)

// MatchesMedia reports whether every nested @media query list attached to the
// rule matches environment. Entries are ANDed because nested @media rules must
// all apply; queries within one entry are comma-separated alternatives.
func (rule Rule) MatchesMedia(environment MediaEnvironment) bool {
	for _, queryList := range rule.Media {
		if !MediaQueryListMatches(queryList, environment) {
			return false
		}
	}
	return true
}

// MediaQueryListMatches evaluates the bounded screen-media subset used by the
// renderer: media types, comma alternatives, not/only, width and height
// ranges, and orientation. Recognized headless device preferences have stable
// defaults; unknown features make their query false.
func MediaQueryListMatches(source string, environment MediaEnvironment) bool {
	if strings.TrimSpace(source) == "" {
		return true
	}
	cleaned, err := stripComments(source)
	if err != nil {
		return false
	}
	source = normalizeCommentBoundaries(cleaned)
	if strings.TrimSpace(source) == "" {
		return true
	}
	for _, candidate := range splitTopLevel(source, ',') {
		matched, valid := matchMediaQuery(candidate, environment)
		if valid && matched == mediaTrue {
			return true
		}
	}
	return false
}

func matchMediaQuery(source string, environment MediaEnvironment) (mediaTruth, bool) {
	remaining := strings.TrimSpace(strings.ToLower(source))
	if remaining == "" {
		return mediaFalse, false
	}

	negated := false
	modifier := ""
	if keyword, rest, ok := consumeMediaKeyword(remaining); ok && (keyword == "not" || keyword == "only") {
		if !startsWithMediaWhitespace(rest) {
			return mediaFalse, false
		}
		modifier = keyword
		negated = keyword == "not"
		remaining = strings.TrimSpace(rest)
		if remaining == "" {
			return mediaFalse, false
		}
	}

	matched := mediaTrue
	hasType := !strings.HasPrefix(remaining, "(")
	if modifier == "only" && !hasType {
		return mediaFalse, false
	}
	if hasType {
		mediaType, rest, ok := consumeMediaKeyword(remaining)
		if !ok {
			return mediaFalse, false
		}
		if reservedMediaType(mediaType) {
			return mediaFalse, false
		}
		matched = mediaTypeMatches(mediaType, environment)
		remaining = strings.TrimSpace(rest)
	}

	hasCondition := false
	needsAnd := hasType
	for remaining != "" {
		if needsAnd {
			keyword, rest, ok := consumeMediaKeyword(remaining)
			if !ok || keyword != "and" {
				return mediaFalse, false
			}
			if !startsWithMediaWhitespace(rest) {
				return mediaFalse, false
			}
			remaining = strings.TrimSpace(rest)
		}
		condition, rest, ok := consumeMediaCondition(remaining)
		if !ok {
			return mediaFalse, false
		}
		conditionMatched, conditionValid := mediaFeatureMatches(condition, environment)
		if !conditionValid {
			return mediaFalse, false
		}
		matched = andMediaTruth(matched, conditionMatched)
		hasCondition = true
		needsAnd = true
		remaining = strings.TrimSpace(rest)
	}
	if !hasType && !hasCondition {
		return mediaFalse, false
	}
	if negated {
		matched = notMediaTruth(matched)
	}
	return matched, true
}

func startsWithMediaWhitespace(source string) bool {
	return source != "" && isCSSWhitespace(source[0])
}

func reservedMediaType(candidate string) bool {
	switch candidate {
	case "and", "layer", "not", "only", "or":
		return true
	default:
		return false
	}
}

func andMediaTruth(left, right mediaTruth) mediaTruth {
	if left == mediaFalse || right == mediaFalse {
		return mediaFalse
	}
	if left == mediaUnknown || right == mediaUnknown {
		return mediaUnknown
	}
	return mediaTrue
}

func notMediaTruth(value mediaTruth) mediaTruth {
	switch value {
	case mediaTrue:
		return mediaFalse
	case mediaFalse:
		return mediaTrue
	default:
		return mediaUnknown
	}
}

func consumeMediaKeyword(source string) (string, string, bool) {
	end := 0
	for end < len(source) {
		character := source[end]
		if character < 'a' || character > 'z' {
			if character != '-' {
				break
			}
		}
		end++
	}
	if end == 0 {
		return "", source, false
	}
	return source[:end], source[end:], true
}

func consumeMediaCondition(source string) (string, string, bool) {
	if source == "" || source[0] != '(' {
		return "", source, false
	}
	depth := 0
	quote := byte(0)
	escaped := false
	for position := 0; position < len(source); position++ {
		character := source[position]
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if character == '\\' {
				escaped = true
				continue
			}
			if character == quote {
				quote = 0
			}
			continue
		}
		switch character {
		case '\'', '"':
			quote = character
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return strings.TrimSpace(source[1:position]), source[position+1:], true
			}
		}
	}
	return "", source, false
}

func mediaTypeMatches(candidate string, environment MediaEnvironment) mediaTruth {
	actual := strings.ToLower(strings.TrimSpace(environment.Type))
	if actual == "" {
		actual = "screen"
	}
	switch candidate {
	case "all":
		return mediaTrue
	case "screen", "print":
		if candidate == actual {
			return mediaTrue
		}
		return mediaFalse
	default:
		return mediaFalse
	}
}

func mediaFeatureMatches(source string, environment MediaEnvironment) (mediaTruth, bool) {
	name, value, hasValue := strings.Cut(source, ":")
	name = strings.TrimSpace(name)
	value = strings.TrimSpace(value)
	if name == "" {
		return mediaFalse, false
	}
	if !hasValue {
		switch name {
		case "width":
			return mediaTruthFor(environment.Width > 0), true
		case "height":
			return mediaTruthFor(environment.Height > 0), true
		case "orientation":
			return mediaTruthFor(environment.Width > 0 && environment.Height > 0), true
		case "hover", "any-hover", "pointer", "any-pointer":
			return mediaFalse, true
		default:
			return mediaUnknown, true
		}
	}

	switch name {
	case "width", "min-width", "max-width":
		length, ok := parseMediaLength(value, environment)
		if !ok {
			return mediaUnknown, true
		}
		return mediaTruthFor(compareMediaDimension(environment.Width, length, name)), true
	case "height", "min-height", "max-height":
		length, ok := parseMediaLength(value, environment)
		if !ok {
			return mediaUnknown, true
		}
		return mediaTruthFor(compareMediaDimension(environment.Height, length, name)), true
	case "orientation":
		switch value {
		case "landscape":
			return mediaTruthFor(environment.Width > environment.Height), true
		case "portrait":
			return mediaTruthFor(environment.Height >= environment.Width), true
		default:
			return mediaUnknown, true
		}
	case "hover", "any-hover":
		if value != "none" && value != "hover" {
			return mediaUnknown, true
		}
		return mediaTruthFor(value == "none"), true
	case "pointer", "any-pointer":
		if value != "none" && value != "coarse" && value != "fine" {
			return mediaUnknown, true
		}
		return mediaTruthFor(value == "none"), true
	case "prefers-color-scheme":
		if value != "light" && value != "dark" {
			return mediaUnknown, true
		}
		return mediaTruthFor(value == "light"), true
	case "prefers-reduced-motion":
		if value != "no-preference" && value != "reduce" {
			return mediaUnknown, true
		}
		return mediaTruthFor(value == "no-preference"), true
	case "display-mode":
		if value != "browser" && value != "fullscreen" && value != "standalone" && value != "minimal-ui" {
			return mediaUnknown, true
		}
		return mediaTruthFor(value == "browser"), true
	case "forced-colors":
		if value != "none" && value != "active" {
			return mediaUnknown, true
		}
		return mediaTruthFor(value == "none"), true
	default:
		return mediaUnknown, true
	}
}

func mediaTruthFor(value bool) mediaTruth {
	if value {
		return mediaTrue
	}
	return mediaFalse
}

func compareMediaDimension(actual, expected float64, feature string) bool {
	switch {
	case strings.HasPrefix(feature, "min-"):
		return actual >= expected
	case strings.HasPrefix(feature, "max-"):
		return actual <= expected
	default:
		return math.Abs(actual-expected) < 1e-9
	}
}

func parseMediaLength(source string, environment MediaEnvironment) (float64, bool) {
	value := strings.TrimSpace(source)
	if value == "0" {
		return 0, true
	}
	initialFontSize := environment.InitialFontSize
	if initialFontSize <= 0 || math.IsNaN(initialFontSize) || math.IsInf(initialFontSize, 0) {
		initialFontSize = 16
	}
	units := []struct {
		suffix string
		scale  float64
	}{
		{suffix: "rem", scale: initialFontSize},
		{suffix: "px", scale: 1},
		{suffix: "em", scale: initialFontSize},
		{suffix: "vw", scale: environment.Width / 100},
		{suffix: "vh", scale: environment.Height / 100},
	}
	for _, unit := range units {
		if !strings.HasSuffix(value, unit.suffix) {
			continue
		}
		numericSource := strings.TrimSuffix(value, unit.suffix)
		if strings.TrimSpace(numericSource) != numericSource {
			return 0, false
		}
		numeric, err := strconv.ParseFloat(numericSource, 64)
		resolved := numeric * unit.scale
		if err != nil || numeric < 0 || math.IsNaN(resolved) || math.IsInf(resolved, 0) {
			return 0, false
		}
		return resolved, true
	}
	return 0, false
}
