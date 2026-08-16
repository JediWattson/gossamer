package css

import (
	"math"
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
	return mediaQueryListMatchesTokens(source, environment)
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

func mediaTypeMatches(candidate string, environment MediaEnvironment) mediaTruth {
	actual := normalizedMediaType(environment.Type)
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
