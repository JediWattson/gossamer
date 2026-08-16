package css_test

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
)

func TestMediaQueryListMatchesTypesModifiersAndAlternatives(t *testing.T) {
	t.Parallel()

	environment := css.MediaEnvironment{Type: "screen", Width: 800, Height: 600}
	tests := []struct {
		name    string
		query   string
		matches bool
	}{
		{name: "empty list", query: "", matches: true},
		{name: "whitespace-only list", query: " \t\n", matches: true},
		{name: "comment-only list", query: "/**/", matches: true},
		{name: "comments separate tokens", query: "screen/**/and/**/(min-width: 800px)", matches: true},
		{name: "escaped media type", query: `\73 creen`, matches: true},
		{name: "escaped and keyword", query: `screen \61 nd (min-width: 800px)`, matches: true},
		{name: "all", query: "all", matches: true},
		{name: "screen", query: "screen", matches: true},
		{name: "print", query: "print", matches: false},
		{name: "only screen", query: "only screen", matches: true},
		{name: "only print", query: "only print", matches: false},
		{name: "only before feature is invalid", query: "only (min-width: 1px)", matches: false},
		{name: "comma list matches any query", query: "print, screen and (min-width: 800px)", matches: true},
		{name: "comma list with no match", query: "print, screen and (min-width: 801px)", matches: false},
		{name: "invalid alternative does not poison later match", query: "only, screen", matches: true},
		{name: "not negates the whole false query", query: "not screen and (max-width: 400px)", matches: true},
		{name: "not negates the whole true query", query: "not screen and (min-width: 700px)", matches: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := css.MediaQueryListMatches(test.query, environment); got != test.matches {
				t.Errorf("MediaQueryListMatches(%q) = %t, want %t", test.query, got, test.matches)
			}
		})
	}
}

func TestMediaQueryListMatchesViewportDimensions(t *testing.T) {
	t.Parallel()

	environment := css.MediaEnvironment{Type: "screen", Width: 800, Height: 600}
	tests := []struct {
		name    string
		query   string
		matches bool
	}{
		{name: "minimum width is inclusive", query: "(min-width: 800px)", matches: true},
		{name: "minimum width above viewport", query: "(min-width: 800.01px)", matches: false},
		{name: "maximum width is inclusive", query: "(max-width: 800px)", matches: true},
		{name: "maximum width below viewport", query: "(max-width: 799px)", matches: false},
		{name: "exact width", query: "(width: 800px)", matches: true},
		{name: "different exact width", query: "(width: 801px)", matches: false},
		{name: "minimum height is inclusive", query: "(min-height: 600px)", matches: true},
		{name: "minimum height above viewport", query: "(min-height: 601px)", matches: false},
		{name: "maximum height is inclusive", query: "(max-height: 600px)", matches: true},
		{name: "maximum height below viewport", query: "(max-height: 599px)", matches: false},
		{name: "exact height", query: "(height: 600px)", matches: true},
		{name: "different exact height", query: "(height: 601px)", matches: false},
		{name: "and combines dimensions", query: "screen and (min-width: 800px) and (max-height: 600px)", matches: true},
		{name: "one false dimension fails conjunction", query: "screen and (min-width: 800px) and (max-height: 599px)", matches: false},
		{name: "em uses initial 16 pixel size", query: "(min-width: 50em)", matches: true},
		{name: "em boundary above viewport", query: "(min-width: 50.01em)", matches: false},
		{name: "rem uses initial 16 pixel size", query: "(max-height: 37.5rem)", matches: true},
		{name: "rem boundary below viewport", query: "(max-height: 37.49rem)", matches: false},
		{name: "escaped feature name", query: `(min-\77 idth: 800px)`, matches: true},
		{name: "escaped dimension unit", query: `(min-width: 50\65 m)`, matches: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := css.MediaQueryListMatches(test.query, environment); got != test.matches {
				t.Errorf("MediaQueryListMatches(%q) = %t, want %t", test.query, got, test.matches)
			}
		})
	}
}

func TestMediaQueryListMatchesOrientation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		environment css.MediaEnvironment
		query       string
		matches     bool
	}{
		{name: "wide viewport is landscape", environment: css.MediaEnvironment{Type: "screen", Width: 800, Height: 600}, query: "(orientation: landscape)", matches: true},
		{name: "wide viewport is not portrait", environment: css.MediaEnvironment{Type: "screen", Width: 800, Height: 600}, query: "(orientation: portrait)", matches: false},
		{name: "tall viewport is portrait", environment: css.MediaEnvironment{Type: "screen", Width: 600, Height: 800}, query: "(orientation: portrait)", matches: true},
		{name: "tall viewport is not landscape", environment: css.MediaEnvironment{Type: "screen", Width: 600, Height: 800}, query: "(orientation: landscape)", matches: false},
		{name: "square viewport is portrait", environment: css.MediaEnvironment{Type: "screen", Width: 600, Height: 600}, query: "(orientation: portrait)", matches: true},
		{name: "escaped feature and value", environment: css.MediaEnvironment{Type: "screen", Width: 800, Height: 600}, query: `(orient\61 tion: land\73 cape)`, matches: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := css.MediaQueryListMatches(test.query, test.environment); got != test.matches {
				t.Errorf("MediaQueryListMatches(%q, %#v) = %t, want %t", test.query, test.environment, got, test.matches)
			}
		})
	}
}

func TestMediaQueryListMatchesRejectsMalformedAndPreservesUnknown(t *testing.T) {
	t.Parallel()

	environment := css.MediaEnvironment{Type: "screen", Width: 800, Height: 600}
	tests := []struct {
		name    string
		query   string
		matches bool
	}{
		{name: "missing condition", query: "screen and", matches: false},
		{name: "missing and", query: "screen (min-width: 1px)", matches: false},
		{name: "unclosed condition", query: "screen and (min-width: 1px", matches: false},
		{name: "unsupported or syntax", query: "screen or print", matches: false},
		{name: "reserved media type is invalid under not", query: "not and", matches: false},
		{name: "not requires whitespace before condition", query: "not(max-width: 400px)", matches: false},
		{name: "and requires whitespace before condition", query: "screen and(min-width: 1px)", matches: false},
		{name: "length unit must touch number", query: "screen and (min-width: 1 px)", matches: false},
		{name: "unsupported length expression", query: "screen and (min-width: calc(1px + 1px))", matches: false},
		{name: "unknown orientation", query: "(orientation: sideways)", matches: false},
		{name: "unknown feature", query: "screen and (made-up: x)", matches: false},
		{name: "not does not invert unknown feature", query: "not screen and (made-up: x)", matches: false},
		{name: "not does not invert feature-only unknown", query: "not (made-up: x)", matches: false},
		{name: "unknown media type does not match", query: "made-up", matches: false},
		{name: "not inverts unknown media type", query: "not made-up", matches: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := css.MediaQueryListMatches(test.query, environment); got != test.matches {
				t.Errorf("MediaQueryListMatches(%q) = %t, want %t", test.query, got, test.matches)
			}
		})
	}
}
