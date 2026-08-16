package render_test

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestFontFamilyFlowsThroughInheritanceShorthandAndDisplayList(t *testing.T) {
	t.Parallel()

	document, body := boxModelDocument()
	inheritedText := dom.NewText("inherited")
	shorthandText := dom.NewText("shorthand")
	inherited := dom.NewElement("div", dom.Attribute{Name: "style", Value: `font-family:"Unavailable", monospace`})
	inherited.AppendChild(dom.NewElement("span"))
	inherited.Children[0].AppendChild(inheritedText)
	shorthand := dom.NewElement("div", dom.Attribute{Name: "style", Value: `font:italic bold 16px "Unavailable", monospace`})
	shorthand.AppendChild(shorthandText)
	body.AppendChild(inherited)
	body.AppendChild(shorthand)

	frame, err := render.Render(document, render.Viewport{Width: 320, Height: 160})
	if err != nil {
		t.Fatal(err)
	}
	want := map[*dom.Node]struct {
		style  render.FontStyle
		weight render.FontWeight
	}{
		inheritedText: {style: render.FontStyleNormal, weight: render.FontWeightNormal},
		shorthandText: {style: render.FontStyleItalic, weight: render.FontWeightBold},
	}
	seen := make(map[*dom.Node]bool)
	for _, command := range frame.DisplayList.Commands {
		expected, ok := want[command.Node]
		if !ok || command.Kind != render.DrawTextCommand {
			continue
		}
		seen[command.Node] = true
		if command.FontFamily != render.FontFamilyMonospace || command.FontStyle != expected.style || command.FontWeight != expected.weight {
			t.Errorf("text %q face = family %v style %v weight %v, want monospace/%v/%v", command.Text, command.FontFamily, command.FontStyle, command.FontWeight, expected.style, expected.weight)
		}
	}
	for node := range want {
		if !seen[node] {
			t.Errorf("no text command for node %p", node)
		}
	}
}
