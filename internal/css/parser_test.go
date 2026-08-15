package css_test

import (
	"reflect"
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
)

func TestParseExampleDomainStylesheet(t *testing.T) {
	t.Parallel()

	source := `
body {
    background-color: #f0f0f2;
    margin: 0;
    padding: 0;
    font-family: -apple-system, system-ui, BlinkMacSystemFont, "Segoe UI", "Open Sans", "Helvetica Neue", Helvetica, Arial, sans-serif;
}
div {
    width: 600px;
    margin: 5em auto;
    padding: 2em;
    background-color: #fdfdff;
    border-radius: 0.5em;
    box-shadow: 2px 3px 7px 2px rgba(0,0,0,0.02);
}
a:link, a:visited {
    color: #38488f;
    text-decoration: none;
}
@media (max-width: 700px) {
    div {
        margin: 0 auto;
        width: auto;
    }
}`

	stylesheet, err := css.Parse(source)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got, want := len(stylesheet.Rules), 3; got != want {
		t.Fatalf("len(Rules) = %d, want %d", got, want)
	}

	body := stylesheet.Rules[0]
	if got, want := body.Order, 0; got != want {
		t.Errorf("body rule Order = %d, want %d", got, want)
	}
	if got, want := body.Selectors, []css.Selector{{Tag: "body", Specificity: css.Specificity{Types: 1}}}; !reflect.DeepEqual(got, want) {
		t.Errorf("body selectors = %#v, want %#v", got, want)
	}
	if got, want := body.Declarations[3], (css.Declaration{
		Property: "font-family",
		Value:    `-apple-system, system-ui, BlinkMacSystemFont, "Segoe UI", "Open Sans", "Helvetica Neue", Helvetica, Arial, sans-serif`,
	}); got != want {
		t.Errorf("font-family = %#v, want %#v", got, want)
	}

	div := stylesheet.Rules[1]
	if got, want := div.Order, 1; got != want {
		t.Errorf("div rule Order = %d, want %d", got, want)
	}
	if got, want := div.Declarations[5].Value, "2px 3px 7px 2px rgba(0,0,0,0.02)"; got != want {
		t.Errorf("box-shadow value = %q, want %q", got, want)
	}

	links := stylesheet.Rules[2]
	if got, want := links.Order, 2; got != want {
		t.Errorf("link rule Order = %d, want %d", got, want)
	}
	wantLinkSelectors := []css.Selector{
		{Tag: "a", PseudoClasses: []string{"link"}, Specificity: css.Specificity{Classes: 1, Types: 1}},
		{Tag: "a", PseudoClasses: []string{"visited"}, Specificity: css.Specificity{Classes: 1, Types: 1}},
	}
	if got := links.Selectors; !reflect.DeepEqual(got, wantLinkSelectors) {
		t.Errorf("link selectors = %#v, want %#v", got, wantLinkSelectors)
	}
}

func TestParseCommentsWhitespaceAndRawValues(t *testing.T) {
	t.Parallel()

	source := `
/* leading */
.card/* compound comment */.featured, *:hover {
  COLOR /* around colon */ : rgb(1, 2, 3) /* value comment */ ! IMPORTANT;
  content: "/* text, not a comment */; still text";
  background-image: url("data:image/svg+xml;a,b:c");
  --BrandColor: ReD;
}
`
	stylesheet, err := css.Parse(source)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got, want := len(stylesheet.Rules), 1; got != want {
		t.Fatalf("len(Rules) = %d, want %d", got, want)
	}

	rule := stylesheet.Rules[0]
	wantSelectors := []css.Selector{
		{Classes: []string{"card", "featured"}, Specificity: css.Specificity{Classes: 2}},
		{Tag: "*", PseudoClasses: []string{"hover"}, Specificity: css.Specificity{Classes: 1}},
	}
	if got := rule.Selectors; !reflect.DeepEqual(got, wantSelectors) {
		t.Errorf("Selectors = %#v, want %#v", got, wantSelectors)
	}
	wantDeclarations := []css.Declaration{
		{Property: "color", Value: "rgb(1, 2, 3)", Important: true},
		{Property: "content", Value: `"/* text, not a comment */; still text"`},
		{Property: "background-image", Value: `url("data:image/svg+xml;a,b:c")`},
		{Property: "--BrandColor", Value: "ReD"},
	}
	if got := rule.Declarations; !reflect.DeepEqual(got, wantDeclarations) {
		t.Errorf("Declarations = %#v, want %#v", got, wantDeclarations)
	}
}

func TestParseSpecificity(t *testing.T) {
	t.Parallel()

	stylesheet, err := css.Parse(`article#main.card.featured:hover:focus, #dialog.open { display: block }`)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got, want := len(stylesheet.Rules), 1; got != want {
		t.Fatalf("len(Rules) = %d, want %d", got, want)
	}

	selectors := stylesheet.Rules[0].Selectors
	if got, want := selectors[0].Specificity, (css.Specificity{IDs: 1, Classes: 4, Types: 1}); got != want {
		t.Errorf("first specificity = %#v, want %#v", got, want)
	}
	if got, want := selectors[1].Specificity, (css.Specificity{IDs: 1, Classes: 1}); got != want {
		t.Errorf("second specificity = %#v, want %#v", got, want)
	}
	if got := selectors[0].Specificity.Compare(selectors[1].Specificity); got != 1 {
		t.Errorf("first.Compare(second) = %d, want 1", got)
	}
	if got := (css.Specificity{Classes: 1}).Compare(css.Specificity{Types: 99}); got != 1 {
		t.Errorf("class specificity comparison = %d, want 1", got)
	}
	if got := (css.Specificity{Types: 1}).Compare(css.Specificity{Types: 1}); got != 0 {
		t.Errorf("equal specificity comparison = %d, want 0", got)
	}
}

func TestParseRecoversFromMalformedRulesSelectorsAndDeclarations(t *testing.T) {
	t.Parallel()

	source := `
not a rule;
div, > span, .usable { must-not: apply; }
div, .usable { color red; good: yes; : nope; empty:; quoted: "a:b;c"; width: 1px ! important; }
section > p { ignored: true; }
@supports (display: grid) { .inside { display: grid } }
p { color: blue }
a { text-decoration: none
`
	stylesheet, err := css.Parse(source)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got, want := len(stylesheet.Rules), 3; got != want {
		t.Fatalf("len(Rules) = %d, want %d: %#v", got, want, stylesheet.Rules)
	}
	for index, rule := range stylesheet.Rules {
		if rule.Order != index {
			t.Errorf("Rules[%d].Order = %d, want %d", index, rule.Order, index)
		}
	}

	first := stylesheet.Rules[0]
	wantSelectors := []css.Selector{
		{Tag: "div", Specificity: css.Specificity{Types: 1}},
		{Classes: []string{"usable"}, Specificity: css.Specificity{Classes: 1}},
	}
	if got := first.Selectors; !reflect.DeepEqual(got, wantSelectors) {
		t.Errorf("recovered selectors = %#v, want %#v", got, wantSelectors)
	}
	wantDeclarations := []css.Declaration{
		{Property: "good", Value: "yes"},
		{Property: "quoted", Value: `"a:b;c"`},
		{Property: "width", Value: "1px", Important: true},
	}
	if got := first.Declarations; !reflect.DeepEqual(got, wantDeclarations) {
		t.Errorf("recovered declarations = %#v, want %#v", got, wantDeclarations)
	}
	if got, want := stylesheet.Rules[1].Selectors[0].Tag, "p"; got != want {
		t.Errorf("second parsed selector tag = %q, want %q", got, want)
	}
	if got, want := stylesheet.Rules[2].Selectors[0].Tag, "a"; got != want {
		t.Errorf("unclosed final rule selector tag = %q, want %q", got, want)
	}
}

func TestImportantOnlyMatchesTopLevelTrailingMarker(t *testing.T) {
	t.Parallel()

	stylesheet, err := css.Parse(`p {
  content: "!important";
  image: fn(!important);
  custom: red ! urgent;
  color: red !ImPoRtAnT;
}`)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	want := []css.Declaration{
		{Property: "content", Value: `"!important"`},
		{Property: "image", Value: "fn(!important)"},
		{Property: "custom", Value: "red ! urgent"},
		{Property: "color", Value: "red", Important: true},
	}
	if got := stylesheet.Rules[0].Declarations; !reflect.DeepEqual(got, want) {
		t.Errorf("Declarations = %#v, want %#v", got, want)
	}
}

func TestParseReturnsPartialSheetForUnrecoverableComment(t *testing.T) {
	t.Parallel()

	stylesheet, err := css.Parse(`body { color: black } /* unterminated`)
	if err == nil {
		t.Fatal("Parse() error = nil, want unterminated-comment error")
	}
	if got, want := len(stylesheet.Rules), 1; got != want {
		t.Fatalf("len(Rules) = %d, want %d", got, want)
	}
	if got, want := stylesheet.Rules[0].Declarations[0].Value, "black"; got != want {
		t.Errorf("color value = %q, want %q", got, want)
	}
}

func TestParseReturnsPartialSheetForUnrecoverableString(t *testing.T) {
	t.Parallel()

	stylesheet, err := css.Parse(`body { color: black } p { content: "unterminated`)
	if err == nil {
		t.Fatal("Parse() error = nil, want unterminated-string error")
	}
	if got, want := len(stylesheet.Rules), 1; got != want {
		t.Fatalf("len(Rules) = %d, want %d", got, want)
	}
}

func TestUnsupportedOnlySheetIsEmptyWithoutError(t *testing.T) {
	t.Parallel()

	stylesheet, err := css.Parse(`@charset "utf-8"; @media print { body { color: black } } section > p { color: red }`)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got := len(stylesheet.Rules); got != 0 {
		t.Fatalf("len(Rules) = %d, want 0", got)
	}
}

func TestParseRetainsEmptyQualifiedRulesAndTheirOrder(t *testing.T) {
	t.Parallel()

	stylesheet, err := css.Parse(`body {} p { invalid declaration } a { color: blue }`)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got, want := len(stylesheet.Rules), 3; got != want {
		t.Fatalf("len(Rules) = %d, want %d", got, want)
	}
	for index, rule := range stylesheet.Rules {
		if got, want := rule.Order, index; got != want {
			t.Errorf("Rules[%d].Order = %d, want %d", index, got, want)
		}
	}
	if got := len(stylesheet.Rules[0].Declarations); got != 0 {
		t.Errorf("empty rule declarations = %d, want 0", got)
	}
	if got := len(stylesheet.Rules[1].Declarations); got != 0 {
		t.Errorf("invalid-only rule declarations = %d, want 0", got)
	}
}

func FuzzParseDoesNotPanic(f *testing.F) {
	f.Add("")
	f.Add(`body { color: red }`)
	f.Add(`a:link, a:visited { color: #38488f }`)
	f.Add(`/* comment */ .x { content: "};:" !important }`)
	f.Add(`@media screen { p { color: blue } }`)
	f.Add("p { color: url(data:image/png;base64,a;b:c) }")

	f.Fuzz(func(t *testing.T, source string) {
		_, _ = css.Parse(source)
	})
}
