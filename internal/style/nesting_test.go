package style_test

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/style"
)

func TestComputeAppliesNestedSelectorsAndGroupDeclarations(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	head := dom.NewElement("head")
	owner := dom.NewElement("style")
	owner.AppendChild(dom.NewText(`
		#card {
			color: black;
			&.active { color: red }
			@media (min-width: 40em) { opacity: .5 }
			@supports (display: block) { background-color: blue }
			> .title { color: inherit }
			width: 120px;
		}
	`))
	head.AppendChild(owner)
	body := dom.NewElement("body")
	card := dom.NewElement("section", dom.Attribute{Name: "id", Value: "card"}, dom.Attribute{Name: "class", Value: "active"})
	title := dom.NewElement("h2", dom.Attribute{Name: "class", Value: "title"})
	card.AppendChild(title)
	body.AppendChild(card)
	html.AppendChild(head)
	html.AppendChild(body)
	document.AppendChild(html)

	snapshot := style.Compute(document, style.Input{Environment: style.Environment{Width: 800, Height: 600, MediaType: "screen", InitialFontSize: 16}})
	cardStyle, ok := snapshot.Lookup(card)
	if !ok {
		t.Fatal("card has no computed style")
	}
	assertComputedLayerValue(t, cardStyle, "color", "rgb(255, 0, 0)")
	assertComputedLayerValue(t, cardStyle, "background-color", "rgb(0, 0, 255)")
	assertComputedLayerValue(t, cardStyle, "opacity", "0.5")
	assertComputedLayerValue(t, cardStyle, "width", "120px")
	titleStyle, ok := snapshot.Lookup(title)
	if !ok {
		t.Fatal("title has no computed style")
	}
	assertComputedLayerValue(t, titleStyle, "color", "rgb(255, 0, 0)")
}

func TestTrailingDeclarationKeepsPositionAfterNestedRule(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	head := dom.NewElement("head")
	owner := dom.NewElement("style")
	owner.AppendChild(dom.NewText(`#target { color: black; & { color: red } color: green; }`))
	head.AppendChild(owner)
	body := dom.NewElement("body")
	target := dom.NewElement("div", dom.Attribute{Name: "id", Value: "target"})
	body.AppendChild(target)
	html.AppendChild(head)
	html.AppendChild(body)
	document.AppendChild(html)
	snapshot := style.Compute(document, style.Input{Environment: style.Environment{Width: 800, Height: 600, MediaType: "screen", InitialFontSize: 16}})
	computed, ok := snapshot.Lookup(target)
	if !ok {
		t.Fatal("target has no computed style")
	}
	assertComputedLayerValue(t, computed, "color", "rgb(0, 128, 0)")
}
