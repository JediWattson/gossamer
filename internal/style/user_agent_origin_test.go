package style_test

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/style"
)

func TestUserAgentRelativeValuesComputeAgainstTheWinningFontSize(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	head := dom.NewElement("head")
	styleElement := dom.NewElement("style")
	styleElement.AppendChild(dom.NewText(`h1 { font-size:20px }`))
	head.AppendChild(styleElement)
	body := dom.NewElement("body")
	heading := dom.NewElement("h1")
	heading.AppendChild(dom.NewText("heading"))
	body.AppendChild(heading)
	html.AppendChild(head)
	html.AppendChild(body)
	document.AppendChild(html)

	snapshot := style.Compute(document, style.Input{Environment: style.Environment{
		Width: 640, Height: 480, MediaType: "screen", InitialFontSize: 16,
	}})
	computed, ok := snapshot.Lookup(heading)
	if !ok {
		t.Fatal("snapshot does not contain heading")
	}
	value, ok := style.ComputedPropertyValue(computed, "margin-top")
	if !ok {
		t.Fatal("margin-top is unsupported")
	}
	if value != "13.4px" {
		t.Fatalf("margin-top = %q, want UA .67em resolved against author 20px font size", value)
	}
	explanation, ok := snapshot.Explain(heading, "margin-top")
	if !ok {
		t.Fatal("snapshot has no margin-top explanation")
	}
	if explanation.Controller.Origin != style.CascadeOriginUserAgent || explanation.Controller.Kind != style.SourceUserAgentRule {
		t.Fatalf("margin controller = %#v, want built-in user-agent rule", explanation.Controller)
	}
}

func TestBuiltInUserAgentRulesRetainSelectorSpecificity(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	container := dom.NewElement("div")
	link := dom.NewElement("a", dom.Attribute{Name: "href", Value: "/next"})
	container.AppendChild(link)
	document.AppendChild(container)
	environment := style.Environment{Width: 640, Height: 480, MediaType: "screen", InitialFontSize: 16}

	lowerSpecificity, err := css.Parse(`* { display:inline } a { color:red }`)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := style.Compute(document, style.Input{
		Environment:          environment,
		UserAgentStylesheets: []css.Stylesheet{lowerSpecificity},
	})
	containerStyle, ok := snapshot.Lookup(container)
	if !ok {
		t.Fatal("snapshot does not contain container")
	}
	if got, _ := style.ComputedPropertyValue(containerStyle, "display"); got != "block" {
		t.Errorf("div display = %q, want built-in type selector to beat later UA universal selector", got)
	}
	linkStyle, ok := snapshot.Lookup(link)
	if !ok {
		t.Fatal("snapshot does not contain link")
	}
	if got, _ := style.ComputedPropertyValue(linkStyle, "color"); got != "rgb(0, 0, 238)" {
		t.Errorf("link color = %q, want built-in a[href] selector to beat later UA a selector", got)
	}
	linkExplanation, ok := snapshot.Explain(link, "color")
	if !ok || linkExplanation.Controller.Specificity != (css.Specificity{Classes: 1, Types: 1}) {
		t.Fatalf("link provenance = %#v, %t; want a[href] specificity", linkExplanation, ok)
	}

	equalSpecificity, err := css.Parse(`div { display:inline }`)
	if err != nil {
		t.Fatal(err)
	}
	snapshot = style.Compute(document, style.Input{
		Environment:          environment,
		UserAgentStylesheets: []css.Stylesheet{equalSpecificity},
	})
	containerStyle, _ = snapshot.Lookup(container)
	if got, _ := style.ComputedPropertyValue(containerStyle, "display"); got != "inline" {
		t.Errorf("div display = %q, want later equal-specificity configurable UA rule", got)
	}
}

func TestStylesheetSourceOrderIsStableAcrossMatchAndValidation(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	matchingInvalid := dom.NewElement("div", dom.Attribute{Name: "class", Value: "invalid"})
	nonMatching := dom.NewElement("span")
	document.AppendChild(matchingInvalid)
	document.AppendChild(nonMatching)
	stylesheet, err := css.Parse(`.invalid { opacity:bogus } * { opacity:.5 }`)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := style.Compute(document, style.Input{
		Environment:          style.Environment{Width: 640, Height: 480, MediaType: "screen", InitialFontSize: 16},
		UserAgentStylesheets: []css.Stylesheet{stylesheet},
	})
	left, ok := snapshot.Explain(matchingInvalid, "opacity")
	if !ok {
		t.Fatal("matching-invalid element has no opacity explanation")
	}
	right, ok := snapshot.Explain(nonMatching, "opacity")
	if !ok {
		t.Fatal("non-matching element has no opacity explanation")
	}
	if left.Controller != right.Controller {
		t.Fatalf("identical UA declaration has node-dependent provenance:\nleft  %#v\nright %#v", left.Controller, right.Controller)
	}
	if left.Value != "0.5" || right.Value != "0.5" {
		t.Errorf("opacity values = %q/%q, want 0.5/0.5", left.Value, right.Value)
	}
}
