package css_test

import (
	"reflect"
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
)

func TestParseRawDeclarationListWithSourcesDecodesNamesAndRetainsAuthoredRanges(t *testing.T) {
	t.Parallel()

	source := ` /* lead */ CO\4c OR /* name boundary */ : rgb(1, 2, 3) /**/ ! \69mportant ; --\54 heme:/**/dark; broken`
	got, err := css.ParseRawDeclarationListWithSources(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("declarations = %#v, want 2", got)
	}
	if want := (css.Declaration{Property: "color", Value: "rgb(1, 2, 3)", Important: true}); got[0].Declaration != want {
		t.Errorf("first declaration = %#v, want %#v", got[0].Declaration, want)
	}
	if want := (css.Declaration{Property: "--Theme", Value: "dark"}); got[1].Declaration != want {
		t.Errorf("second declaration = %#v, want %#v", got[1].Declaration, want)
	}

	assertDeclarationSourceSlices(t, source, got[0].Source,
		`CO\4c OR /* name boundary */ : rgb(1, 2, 3) /**/ ! \69mportant`,
		`CO\4c OR`, `rgb(1, 2, 3)`)
	assertDeclarationSourceSlices(t, source, got[1].Source,
		`--\54 heme:/**/dark`, `--\54 heme`, `dark`)
}

func TestStylesheetDeclarationSourcesAreAbsoluteThroughNestedRules(t *testing.T) {
	t.Parallel()

	source := `/* prefix */ @layer theme { @media all { p { COLOR: red; --tone: blue } } }`
	stylesheet, err := css.Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(stylesheet.Rules) != 1 {
		t.Fatalf("rules = %#v, want one", stylesheet.Rules)
	}
	rule := stylesheet.Rules[0]
	if got, want := rule.Declarations, []css.Declaration{{Property: "color", Value: "red"}, {Property: "--tone", Value: "blue"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("declarations = %#v, want %#v", got, want)
	}
	if len(rule.DeclarationSources) != len(rule.Declarations) {
		t.Fatalf("source count = %d, declaration count = %d", len(rule.DeclarationSources), len(rule.Declarations))
	}
	assertDeclarationSourceSlices(t, source, rule.DeclarationSources[0], `COLOR: red`, `COLOR`, `red`)
	assertDeclarationSourceSlices(t, source, rule.DeclarationSources[1], `--tone: blue`, `--tone`, `blue`)
}

func TestDeclarationComponentParsingRecoversAfterBadStringLine(t *testing.T) {
	t.Parallel()

	declarations, err := css.ParseRawDeclarationListWithSources("color: red; content: \"bad\n; width: 2px")
	if err != nil {
		t.Fatalf("newline bad-string should recover without a fatal error: %v", err)
	}
	if got, want := sourcedDeclarationValues(declarations), []css.Declaration{{Property: "color", Value: "red"}, {Property: "width", Value: "2px"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("recovered declarations = %#v, want %#v", got, want)
	}
}

func assertDeclarationSourceSlices(t *testing.T, source string, got css.DeclarationSource, span, name, value string) {
	t.Helper()
	if !got.Span.Valid(len(source)) || !got.NameSpan.Valid(len(source)) || !got.ValueSpan.Valid(len(source)) {
		t.Fatalf("invalid declaration source %#v for %d bytes", got, len(source))
	}
	if actual := got.Span.Slice(source); actual != span {
		t.Errorf("Span = %q, want %q", actual, span)
	}
	if actual := got.NameSpan.Slice(source); actual != name {
		t.Errorf("NameSpan = %q, want %q", actual, name)
	}
	if actual := got.ValueSpan.Slice(source); actual != value {
		t.Errorf("ValueSpan = %q, want %q", actual, value)
	}
}

func sourcedDeclarationValues(declarations []css.SourcedDeclaration) []css.Declaration {
	result := make([]css.Declaration, len(declarations))
	for index := range declarations {
		result[index] = declarations[index].Declaration
	}
	return result
}
