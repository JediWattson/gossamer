package render_test

import (
	"image/color"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestRenderAppliesComplexSelectorThroughCascade(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	document.AppendChild(html)
	head := dom.NewElement("head")
	html.AppendChild(head)
	style := dom.NewElement("style")
	style.AppendChild(dom.NewText(`
		body > main[data-layout=primary] > p.note:nth-child(2) { color: #123456; }
		.note { color: #abcdef; }
	`))
	head.AppendChild(style)
	body := dom.NewElement("body")
	html.AppendChild(body)
	main := dom.NewElement("main", dom.Attribute{Name: "data-layout", Value: "primary"})
	body.AppendChild(main)
	first := dom.NewElement("p")
	first.AppendChild(dom.NewText("first"))
	main.AppendChild(first)
	target := dom.NewElement("p", dom.Attribute{Name: "class", Value: "note"})
	target.AppendChild(dom.NewText("complex selector target"))
	main.AppendChild(target)

	frame, err := render.Render(document, render.Viewport{Width: 400, Height: 240})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	fragment := findTextFragment(collectTextFragments(frame.Root), "complex selector target")
	if fragment == nil {
		t.Fatal("target text fragment not found")
	}
	if got, want := fragment.Color, (color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff}); got != want {
		t.Errorf("target color = %#v, want %#v", got, want)
	}
}
