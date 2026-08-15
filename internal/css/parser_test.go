package css_test

import (
	"reflect"
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
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
	if got, want := len(stylesheet.Rules), 4; got != want {
		t.Fatalf("len(Rules) = %d, want %d", got, want)
	}

	body := stylesheet.Rules[0]
	if got, want := body.Order, 0; got != want {
		t.Errorf("body rule Order = %d, want %d", got, want)
	}
	if got, want := len(body.Selectors), 1; got != want {
		t.Fatalf("len(body.Selectors) = %d, want %d", got, want)
	}
	if got, want := body.Selectors[0].Specificity(), (css.Specificity{Types: 1}); got != want {
		t.Errorf("body selector specificity = %#v, want %#v", got, want)
	}
	if !body.Selectors[0].Matches(dom.NewElement("body")) {
		t.Error("body selector did not match a body element")
	}
	if body.Selectors[0].Matches(dom.NewElement("div")) {
		t.Error("body selector matched a div element")
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
	if got, want := len(links.Selectors), 2; got != want {
		t.Fatalf("len(link selectors) = %d, want %d", got, want)
	}
	for index, selector := range links.Selectors {
		if got, want := selector.Specificity(), (css.Specificity{Classes: 1, Types: 1}); got != want {
			t.Errorf("link selector %d specificity = %#v, want %#v", index, got, want)
		}
	}
	link := dom.NewElement("a", dom.Attribute{Name: "href", Value: "/docs"})
	if !links.Selectors[0].Matches(link) {
		t.Error("a:link selector did not match an anchor with href")
	}
	if links.Selectors[1].Matches(link) {
		t.Error("a:visited selector matched without history state")
	}

	responsive := stylesheet.Rules[3]
	if got, want := responsive.Order, 3; got != want {
		t.Errorf("responsive rule Order = %d, want %d", got, want)
	}
	if got, want := responsive.Media, []string{"(max-width: 700px)"}; !reflect.DeepEqual(got, want) {
		t.Errorf("responsive rule Media = %#v, want %#v", got, want)
	}
	if len(responsive.Selectors) != 1 || !responsive.Selectors[0].Matches(dom.NewElement("div")) {
		t.Error("responsive rule did not retain its div selector")
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
	if got, want := len(rule.Selectors), 2; got != want {
		t.Fatalf("len(Selectors) = %d, want %d", got, want)
	}
	if got, want := rule.Selectors[0].Specificity(), (css.Specificity{Classes: 2}); got != want {
		t.Errorf("first selector specificity = %#v, want %#v", got, want)
	}
	featuredCard := dom.NewElement("div", dom.Attribute{Name: "class", Value: "card featured"})
	if !rule.Selectors[0].Matches(featuredCard) {
		t.Error("comment-separated compound selector did not match")
	}
	if got, want := rule.Selectors[1].Specificity(), (css.Specificity{Classes: 1}); got != want {
		t.Errorf("second selector specificity = %#v, want %#v", got, want)
	}
	if rule.Selectors[1].Matches(featuredCard) {
		t.Error("*:hover matched without interaction state")
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
	if got, want := selectors[0].Specificity(), (css.Specificity{IDs: 1, Classes: 4, Types: 1}); got != want {
		t.Errorf("first specificity = %#v, want %#v", got, want)
	}
	if got, want := selectors[1].Specificity(), (css.Specificity{IDs: 1, Classes: 1}); got != want {
		t.Errorf("second specificity = %#v, want %#v", got, want)
	}
	if got := selectors[0].Specificity().Compare(selectors[1].Specificity()); got != 1 {
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
	if got, want := len(stylesheet.Rules), 4; got != want {
		t.Fatalf("len(Rules) = %d, want %d: %#v", got, want, stylesheet.Rules)
	}
	for index, rule := range stylesheet.Rules {
		if rule.Order != index {
			t.Errorf("Rules[%d].Order = %d, want %d", index, rule.Order, index)
		}
	}

	first := stylesheet.Rules[0]
	if got, want := len(first.Selectors), 2; got != want {
		t.Fatalf("len(recovered selectors) = %d, want %d", got, want)
	}
	if got, want := first.Selectors[0].Specificity(), (css.Specificity{Types: 1}); got != want {
		t.Errorf("recovered div specificity = %#v, want %#v", got, want)
	}
	if !first.Selectors[0].Matches(dom.NewElement("div")) {
		t.Error("recovered div selector did not match")
	}
	if got, want := first.Selectors[1].Specificity(), (css.Specificity{Classes: 1}); got != want {
		t.Errorf("recovered class specificity = %#v, want %#v", got, want)
	}
	if !first.Selectors[1].Matches(dom.NewElement("span", dom.Attribute{Name: "class", Value: "usable"})) {
		t.Error("recovered .usable selector did not match")
	}
	wantDeclarations := []css.Declaration{
		{Property: "good", Value: "yes"},
		{Property: "quoted", Value: `"a:b;c"`},
		{Property: "width", Value: "1px", Important: true},
	}
	if got := first.Declarations; !reflect.DeepEqual(got, wantDeclarations) {
		t.Errorf("recovered declarations = %#v, want %#v", got, wantDeclarations)
	}
	section := dom.NewElement("section")
	directParagraph := dom.NewElement("p")
	section.AppendChild(directParagraph)
	if !stylesheet.Rules[1].Selectors[0].Matches(directParagraph) {
		t.Error("section > p selector did not match a direct paragraph child")
	}
	if !stylesheet.Rules[2].Selectors[0].Matches(dom.NewElement("p")) {
		t.Error("third parsed selector did not match p")
	}
	if !stylesheet.Rules[3].Selectors[0].Matches(dom.NewElement("a")) {
		t.Error("unclosed final rule selector did not match a")
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

func TestUnsupportedRulesDoNotDiscardSupportedMediaRule(t *testing.T) {
	t.Parallel()

	stylesheet, err := css.Parse(`@charset "utf-8"; @media print { body { color: black } } section::before { color: red } svg|circle { fill: blue }`)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got, want := len(stylesheet.Rules), 1; got != want {
		t.Fatalf("len(Rules) = %d, want %d", got, want)
	}
	rule := stylesheet.Rules[0]
	if got, want := rule.Media, []string{"print"}; !reflect.DeepEqual(got, want) {
		t.Errorf("media rule Media = %#v, want %#v", got, want)
	}
	if len(rule.Selectors) != 1 || !rule.Selectors[0].Matches(dom.NewElement("body")) {
		t.Error("supported @media rule did not retain its body selector")
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
