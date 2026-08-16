package css_test

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
)

func TestSupportsConditionMatchesLogicalDeclarationsAndSelectors(t *testing.T) {
	t.Parallel()

	supports := func(declaration css.Declaration) bool {
		return declaration.Property == "display" && declaration.Value == "block" ||
			declaration.Property == "color" && declaration.Value == "red"
	}
	tests := []struct {
		name   string
		source string
		want   bool
	}{
		{name: "declaration", source: `(display: block)`, want: true},
		{name: "escaped declaration", source: `(\64 isplay: block)`, want: true},
		{name: "unsupported declaration", source: `(display: grid)`},
		{name: "and", source: `(display: block) and (color: red)`, want: true},
		{name: "or", source: `(display: grid) or (color: red)`, want: true},
		{name: "not", source: `not (display: grid)`, want: true},
		{name: "nested", source: `((display: block) and (color: red))`, want: true},
		{name: "selector", source: `selector(div > .item)`, want: true},
		{name: "unsupported selector", source: `selector(div::before)`},
		{name: "general enclosed", source: `future(feature)`},
		{name: "negated general enclosed", source: `not future(feature)`, want: true},
		{name: "parenthesized general enclosed", source: `not (future syntax)`, want: true},
		{name: "mixed operators", source: `(display: block) and (color: red) or (display: grid)`},
		{name: "unwrapped declaration", source: `display: block`},
		{name: "empty", source: ``},
		{name: "trailing", source: `(display: block) extra`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := css.SupportsConditionMatches(test.source, supports); got != test.want {
				t.Fatalf("SupportsConditionMatches(%q) = %t, want %t", test.source, got, test.want)
			}
		})
	}
}

func TestSupportsImportConditionAcceptsDeclarationOrCondition(t *testing.T) {
	t.Parallel()

	supports := func(declaration css.Declaration) bool {
		return declaration.Property == "display" && declaration.Value == "block"
	}
	for _, source := range []string{
		`display: block`,
		`(display: block)`,
		`(display: grid) or (display: block)`,
	} {
		if !css.SupportsImportConditionMatches(source, supports) {
			t.Errorf("SupportsImportConditionMatches(%q) = false, want true", source)
		}
	}
	for _, source := range []string{
		`display: grid`,
		`display: block !important`,
		`display: block; color: red`,
		`(display: grid) and (display: block)`,
	} {
		if css.SupportsImportConditionMatches(source, supports) {
			t.Errorf("SupportsImportConditionMatches(%q) = true, want false", source)
		}
	}
}

func TestParseSupportsGroupsRetainsNestedConditions(t *testing.T) {
	t.Parallel()

	stylesheet, err := css.Parse(`
		@supports (display: block) {
			@media screen {
				@supports selector(.item > span) { .item { color: red } }
			}
		}
		@supports display: block { .invalid { color: blue } }
	`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(stylesheet.Rules), 2; got != want {
		t.Fatalf("rules = %d, want %d", got, want)
	}
	first := stylesheet.Rules[0]
	if got, want := first.Supports, []string{`(display: block)`, `selector(.item > span)`}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("nested supports = %q, want %q", got, want)
	}
	if got, want := first.Media, []string{"screen"}; len(got) != 1 || got[0] != want[0] {
		t.Fatalf("nested media = %q, want %q", got, want)
	}
	if css.SupportsConditionMatches(stylesheet.Rules[1].Supports[0], func(css.Declaration) bool { return true }) {
		t.Fatal("invalid unwrapped @supports prelude matched")
	}
}
