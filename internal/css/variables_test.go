package css_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
)

func TestResolveCustomPropertiesInheritsAndHandlesCSSWideKeywords(t *testing.T) {
	t.Parallel()

	parent := css.ResolveCustomProperties(css.CustomProperties{}, map[string]string{
		"--Theme": "red",
		"--empty": "",
		"--gone":  "blue",
	})
	child := css.ResolveCustomProperties(parent, map[string]string{
		"--Theme":     " /* before */ InHeRiT /* after */ ",
		"--dependent": "var(--gone,fallback)",
		"--empty":     "unset",
		"--gone":      "INITIAL",
		"--theme":     "green",
	})

	assertCustomProperty(t, child, "--Theme", "red", true)
	assertCustomProperty(t, child, "--dependent", "fallback", true)
	assertCustomProperty(t, child, "--empty", "", true)
	assertCustomProperty(t, child, "--gone", "", false)
	assertCustomProperty(t, child, "--theme", "green", true)
	assertCustomProperty(t, parent, "--gone", "blue", true)
}

func TestResolveCustomPropertiesResolvesForwardNestedAndMultipleReferences(t *testing.T) {
	t.Parallel()

	properties := css.ResolveCustomProperties(css.CustomProperties{}, map[string]string{
		"--a":      "var(--b)",
		"--b":      "value",
		"--empty":  "",
		"--pair":   "var(--a)/var(--b)var(--empty)",
		"--nested": "var(--missing,var(--pair,fallback))",
	})

	assertCustomProperty(t, properties, "--a", "value", true)
	assertCustomProperty(t, properties, "--pair", "value/value", true)
	assertCustomProperty(t, properties, "--nested", "value/value", true)

	got, ok := properties.Substitute("before var(--a) var(--missing,var(--b)) var(--empty) after")
	if !ok || got != "before value value  after" {
		t.Fatalf("Substitute() = %q, %t, want %q, true", got, ok, "before value value  after")
	}
}

func TestResolveCustomPropertiesDetectsCyclesIncludingUnusedFallbacks(t *testing.T) {
	t.Parallel()

	properties := css.ResolveCustomProperties(css.CustomProperties{}, map[string]string{
		"--base":     "available",
		"--direct":   "var(--direct,fallback)",
		"--first":    "var(--second)",
		"--second":   "var(--first,fallback)",
		"--unused":   "var(--base,var(--unused))",
		"--consumer": "var(--direct,recovered)",
	})

	for _, name := range []string{"--direct", "--first", "--second", "--unused"} {
		assertCustomProperty(t, properties, name, "", false)
	}
	assertCustomProperty(t, properties, "--base", "available", true)
	assertCustomProperty(t, properties, "--consumer", "recovered", true)
}

func TestCustomPropertySubstitutionUnderstandsStringsCommentsAndBlocks(t *testing.T) {
	t.Parallel()

	properties := css.ResolveCustomProperties(css.CustomProperties{}, map[string]string{
		"--x": "replacement",
	})
	source := `fn("var(--x)", /* var(--x) */ var(--x), var(--missing,(a,b[c,d]{e,f})))`
	want := `fn("var(--x)", /* var(--x) */ replacement, (a,b[c,d]{e,f}))`
	got, ok := properties.Substitute(source)
	if !ok || got != want {
		t.Fatalf("Substitute() = %q, %t, want %q, true", got, ok, want)
	}
}

func TestCustomPropertySubstitutionMissingMalformedAndEmptyFallbacks(t *testing.T) {
	t.Parallel()

	properties := css.ResolveCustomProperties(css.CustomProperties{}, nil)
	tests := []struct {
		name   string
		source string
		want   string
		ok     bool
	}{
		{name: "missing", source: "var(--missing)"},
		{name: "fallback", source: "var(--missing,fallback)", want: "fallback", ok: true},
		{name: "empty fallback", source: "before var(--missing,) after", want: "before  after", ok: true},
		{name: "invalid name", source: "var(color,red)"},
		{name: "extra name tokens", source: "var(--x other,red)"},
		{name: "implicitly closed at EOF", source: "var(--missing,red", want: "red", ok: true},
		{name: "mismatched block", source: "var(--missing,[red))"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, ok := properties.Substitute(test.source)
			if ok != test.ok || got != test.want {
				t.Errorf("Substitute(%q) = %q, %t, want %q, %t", test.source, got, ok, test.want, test.ok)
			}
		})
	}
}

func TestCustomPropertySubstitutionPreservesTokenBoundaries(t *testing.T) {
	t.Parallel()

	properties := css.ResolveCustomProperties(css.CustomProperties{}, map[string]string{
		"--cdo-suffix": "--",
		"--empty":      "",
		"--gap":        "20",
		"--slash":      `\`,
		"--slashes":    `\\\`,
		"--unit":       "px",
	})
	tests := []struct {
		source string
		want   string
	}{
		{source: "var(--gap)px", want: "20 px"},
		{source: "var(--gap)%", want: "20 %"},
		{source: "#var(--unit)", want: "#var(--unit)"},
		{source: "@var(--unit)", want: "@var(--unit)"},
		{source: "1var(--unit)", want: "1var(--unit)"},
		{source: "var(--unit)var(--unit)", want: "px px"},
		{source: "a var(--empty)b", want: "a b"},
		{source: "calc(var(--gap) * 1px)", want: "calc(20 * 1px)"},
		{source: "<!var(--cdo-suffix)", want: "<! --"},
		{source: "var(--slash)#x", want: `\ #x`},
		{source: "var(--slashes)#x", want: `\\\ #x`},
	}
	for _, test := range tests {
		got, ok := properties.Substitute(test.source)
		if !ok || got != test.want {
			t.Errorf("Substitute(%q) = %q, %t, want %q, true", test.source, got, ok, test.want)
		}
	}
}

func TestVariableFunctionsDecodeEscapesAndRespectTokenBoundaries(t *testing.T) {
	t.Parallel()

	properties := css.ResolveCustomProperties(css.CustomProperties{}, map[string]string{
		`--\78`: "escaped-name",
	})
	for _, source := range []string{`\76 ar(--x)`, `v\61 r(--\78)`} {
		got, ok := properties.Substitute(source)
		if !ok || got != "escaped-name" {
			t.Errorf("Substitute(%q) = %q, %t, want %q, true", source, got, ok, "escaped-name")
		}
		if !css.ContainsVarFunction(source) {
			t.Errorf("ContainsVarFunction(%q) = false, want true", source)
		}
	}
	if css.ContainsVarFunction(`x\ var(--x)`) {
		t.Fatal(`ContainsVarFunction("x\\ var(--x)") = true, want false`)
	}

	cyclic := css.ResolveCustomProperties(css.CustomProperties{}, map[string]string{
		"--cycle": `\76 ar(--cycle,fallback)`,
	})
	assertCustomProperty(t, cyclic, "--cycle", "", false)

	initial := css.ResolveCustomProperties(properties, map[string]string{
		"--x": `\69 nitial`,
	})
	assertCustomProperty(t, initial, "--x", "", false)
}

func TestContainsVarFunctionIgnoresStringsCommentsAndIdentifierSuffixes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		source string
		want   bool
	}{
		{source: "var(--x)", want: true},
		{source: "calc(VAR(--x) + 1px)", want: true},
		{source: "var(--unclosed", want: true},
		{source: `"var(--x)"`},
		{source: "/* var(--x) */"},
		{source: "notvar(--x)"},
		{source: "--var(--x)"},
		{source: "var (--x)"},
		{source: "#var(--x)"},
		{source: `#\76 ar(--x)`},
		{source: "#123var(--x)"},
		{source: "@var(--x)"},
		{source: `@\76 ar(--x)`},
		{source: "1var(--x)"},
		{source: ".5var(--x)"},
		{source: "-1e2var(--x)"},
		{source: `1\76 ar(--x)`},
		{source: "url(var(--x))"},
		{source: `url("var(--x)")`},
		{source: "url(foo var(--x))"},
		{source: "#fff var(--x)", want: true},
		{source: "@media var(--x)", want: true},
		{source: "1px var(--x)", want: true},
		{source: "# var(--x)", want: true},
		{source: "<!--var(--x)", want: true},
		{source: "-->var(--x)", want: true},
	}
	for _, test := range tests {
		if got := css.ContainsVarFunction(test.source); got != test.want {
			t.Errorf("ContainsVarFunction(%q) = %t, want %t", test.source, got, test.want)
		}
	}
}

func TestValidCustomPropertyValue(t *testing.T) {
	t.Parallel()

	valid := []string{
		"",
		" /* empty */ ",
		"plain value",
		`"quoted value"`,
		"calc(1px + var(--gap))",
		"var(--missing,[!;])",
		"url(example.png)",
		`url("example.png")`,
		`url(foo\ bar)`,
		"url(/*comment*/path)",
		"function(unclosed at EOF",
		`\! \;`,
		"<!--",
		"-->",
		"before<!--middle-->after",
		"#var(--not-a-function)",
		"@var(--not-a-function)",
		"1var(--not-a-function)",
	}
	for _, source := range valid {
		if !css.ValidCustomPropertyValue(source) {
			t.Errorf("ValidCustomPropertyValue(%q) = false, want true", source)
		}
	}

	invalid := []string{
		"red !important",
		"red; blue",
		")",
		"]",
		"}",
		"([)]",
		"\"line\nbreak\"",
		`"unterminated`,
		"/* unterminated",
		`url("unterminated)`,
		"url(foo bar)",
		`url(foo"bar)`,
		"url(foo(bar)",
		`url(/*comment*/"quoted")`,
		"url(foo\\\nbar)",
		"url(var(--inside))",
		"var(color)",
		"var(--x,[red))",
		"var(--x,url(var(--inside)))",
	}
	for _, source := range invalid {
		if css.ValidCustomPropertyValue(source) {
			t.Errorf("ValidCustomPropertyValue(%q) = true, want false", source)
		}
	}

	parent := css.ResolveCustomProperties(css.CustomProperties{}, map[string]string{"--x": "parent"})
	for _, source := range []string{"child; discarded", "child ! discarded", "url(var(--x))"} {
		child := css.ResolveCustomProperties(parent, map[string]string{"--x": source})
		assertCustomProperty(t, child, "--x", "parent", true)
	}
}

func TestCustomPropertyReferencesUsesCSSFunctionTokens(t *testing.T) {
	t.Parallel()

	source := `#var(--hash) @var(--at) 1var(--dimension) url("var(--url)") ` +
		`var(--first, calc(var(--second) + var(--first))) var(--\74 hird)`
	got, ok := css.CustomPropertyReferences(source)
	want := []string{"--first", "--second", "--first", "--third"}
	if !ok || fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("CustomPropertyReferences() = %v, %t, want %v, true", got, ok, want)
	}

	for _, source := range []string{"var(color)", "red; blue", "url(var(--x))"} {
		if got, ok := css.CustomPropertyReferences(source); ok || got != nil {
			t.Errorf("CustomPropertyReferences(%q) = %v, %t, want nil, false", source, got, ok)
		}
	}

	tooMany := strings.Repeat("var(--x) ", 4097)
	if got, ok := css.CustomPropertyReferences(tooMany); ok || got != nil {
		t.Errorf("over-limit CustomPropertyReferences() = %d refs, %t, want nil, false", len(got), ok)
	}
}

func TestCSSWideKeywordUsesOneIdentifierToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		source string
		want   string
	}{
		{source: "initial", want: "initial"},
		{source: " /* before */ InHeRiT /* after */ ", want: "inherit"},
		{source: `\75 nset`, want: "unset"},
		{source: "REVERT", want: "revert"},
		{source: `revert\2d layer`, want: "revert-layer"},
		{source: "initial red"},
		{source: "#initial"},
		{source: `"initial"`},
		{source: "initial()"},
		{source: "var(--keyword)"},
		{source: "/* unterminated"},
	}
	for _, test := range tests {
		if got := css.CSSWideKeyword(test.source); got != test.want {
			t.Errorf("CSSWideKeyword(%q) = %q, want %q", test.source, got, test.want)
		}
	}
}

func TestValidVariableFunctionsDistinguishesParseAndComputedInvalidity(t *testing.T) {
	t.Parallel()

	valid := []string{
		"plain value",
		"var(--x)",
		"var(--missing,red, blue)",
		"var(--missing,)",
		`\76 ar(--\78,fallback)`,
		"var(--missing,fallback",
		"#var(color)",
		"@var(color)",
		"1var(color)",
		`url("var(color)")`,
	}
	for _, source := range valid {
		if !css.ValidVariableFunctions(source) {
			t.Errorf("ValidVariableFunctions(%q) = false, want true", source)
		}
	}
	invalid := []string{
		"var(color,red)",
		"var(--x other,red)",
		"var(--x,[red))",
		"var(--x,var(color))",
		"var(--x,;)",
		"var(--x,!)",
		"var(--x,var(--y,!))",
		`"unterminated`,
		"/* unterminated",
		strings.Repeat("var(--x,", 140) + "fallback" + strings.Repeat(")", 140),
	}
	for _, source := range invalid {
		if css.ValidVariableFunctions(source) {
			t.Errorf("ValidVariableFunctions(%q) = true, want false", source)
		}
	}

	parent := css.ResolveCustomProperties(css.CustomProperties{}, map[string]string{"--x": "parent"})
	parseInvalid := css.ResolveCustomProperties(parent, map[string]string{"--x": "var(color)"})
	assertCustomProperty(t, parseInvalid, "--x", "parent", true)

	computedInvalid := css.ResolveCustomProperties(parent, map[string]string{"--x": "var(--missing)"})
	assertCustomProperty(t, computedInvalid, "--x", "", false)

	got, ok := css.ResolveCustomProperties(css.CustomProperties{}, nil).Substitute("var(--missing,red, blue)")
	if !ok || got != "red, blue" {
		t.Fatalf("fallback containing comma = %q, %t, want %q, true", got, ok, "red, blue")
	}
}

func TestResolveCustomPropertiesRejectsInvalidNamesAndCanonicalizesEscapes(t *testing.T) {
	t.Parallel()

	properties := css.ResolveCustomProperties(css.CustomProperties{}, map[string]string{
		"--":      "reserved",
		"--bad)":  "invalid",
		"not-var": "invalid",
		`--\78`:   "escaped",
	})
	assertCustomProperty(t, properties, "--", "", false)
	assertCustomProperty(t, properties, "--bad)", "", false)
	assertCustomProperty(t, properties, "not-var", "", false)
	assertCustomProperty(t, properties, "--x", "escaped", true)
	assertCustomProperty(t, properties, `--\78`, "escaped", true)
}

func TestResolveCustomPropertiesInvalidatesEveryMemberOfLongCycle(t *testing.T) {
	t.Parallel()

	const cycleLength = 130
	specified := make(map[string]string, cycleLength)
	for index := 0; index < cycleLength; index++ {
		name := fmt.Sprintf("--cycle-%03d", index)
		next := fmt.Sprintf("--cycle-%03d", (index+1)%cycleLength)
		specified[name] = "var(" + next + ",fallback)"
	}
	properties := css.ResolveCustomProperties(css.CustomProperties{}, specified)
	for index := 0; index < cycleLength; index++ {
		name := fmt.Sprintf("--cycle-%03d", index)
		assertCustomProperty(t, properties, name, "", false)
	}
}

func TestResolveCustomPropertiesBoundsAggregateMaterialization(t *testing.T) {
	// Keep this serial: it intentionally materializes the resolver-wide budget.
	large := strings.Repeat("x", 1<<20)
	specified := map[string]string{"--alias-000": large}
	for index := 1; index <= 20; index++ {
		name := fmt.Sprintf("--alias-%03d", index)
		previous := fmt.Sprintf("--alias-%03d", index-1)
		specified[name] = "var(" + previous + ")"
	}
	properties := css.ResolveCustomProperties(css.CustomProperties{}, specified)
	assertCustomProperty(t, properties, "--alias-015", large, true)
	assertCustomProperty(t, properties, "--alias-016", "", false)
	assertCustomProperty(t, properties, "--alias-020", "", false)
}

func TestCustomPropertyResolutionBoundsDepthAndExpansion(t *testing.T) {
	t.Parallel()

	deep := make(map[string]string)
	for index := 0; index < 140; index++ {
		deep[fmt.Sprintf("--v%d", index)] = fmt.Sprintf("var(--v%d)", index+1)
	}
	deep["--v140"] = "end"
	deepProperties := css.ResolveCustomProperties(css.CustomProperties{}, deep)
	if _, ok := deepProperties.Value("--v0"); ok {
		t.Fatal("over-depth custom property resolved, want it invalid")
	}

	expanding := map[string]string{"--v0": "xx"}
	for index := 1; index < 24; index++ {
		previous := fmt.Sprintf("--v%d", index-1)
		expanding[fmt.Sprintf("--v%d", index)] = "var(" + previous + ")var(" + previous + ")"
	}
	expandingProperties := css.ResolveCustomProperties(css.CustomProperties{}, expanding)
	if _, ok := expandingProperties.Value("--v23"); ok {
		t.Fatal("over-limit custom property expansion resolved, want it invalid")
	}

	oversized := strings.Repeat("x", 1<<20+1)
	if _, ok := expandingProperties.Substitute(oversized); ok {
		t.Fatal("oversized substitution succeeded")
	}
}

func FuzzCustomPropertyResolverDoesNotPanic(f *testing.F) {
	for _, seed := range []string{
		"",
		"var(--x)",
		"var(--missing,var(--fallback,red))",
		`"var(--quoted)" /* var(--commented) */`,
		"var(--x,[a,{b:(c,d)}])",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, source string) {
		properties := css.ResolveCustomProperties(css.CustomProperties{}, map[string]string{
			"--x":        source,
			"--fallback": "fallback",
		})
		_, _ = properties.Substitute(source)
		_, _ = properties.Value("--x")
		_ = css.ContainsVarFunction(source)
		_ = css.ValidVariableFunctions(source)
		_ = css.ValidCustomPropertyValue(source)
		_, _ = css.CustomPropertyReferences(source)
		_ = css.CSSWideKeyword(source)
	})
}

func assertCustomProperty(t *testing.T, properties css.CustomProperties, name, want string, wantOK bool) {
	t.Helper()
	got, ok := properties.Value(name)
	if ok != wantOK || got != want {
		t.Errorf("Value(%q) = %q, %t, want %q, %t", name, got, ok, want, wantOK)
	}
}
