package render_test

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestFontStyleSelectsItalicFacesThroughLonghandAndShorthand(t *testing.T) {
	t.Parallel()

	document, body := boxModelDocument()
	italicText := dom.NewText("italic")
	shorthandText := dom.NewText("shorthand")
	normalText := dom.NewText("normal")
	italic := dom.NewElement("div", dom.Attribute{Name: "style", Value: "font-style:italic"})
	italic.AppendChild(italicText)
	shorthand := dom.NewElement("div", dom.Attribute{Name: "style", Value: "font:italic bold 16px serif"})
	shorthand.AppendChild(shorthandText)
	normal := dom.NewElement("div", dom.Attribute{Name: "style", Value: "font-style:italic"})
	normalChild := dom.NewElement("span", dom.Attribute{Name: "style", Value: "font-style:normal"})
	normalChild.AppendChild(normalText)
	normal.AppendChild(normalChild)
	body.AppendChild(italic)
	body.AppendChild(shorthand)
	body.AppendChild(normal)

	frame, err := render.Render(document, render.Viewport{Width: 300, Height: 180})
	if err != nil {
		t.Fatal(err)
	}
	want := map[*dom.Node]struct {
		style  render.FontStyle
		weight render.FontWeight
	}{
		italicText:    {style: render.FontStyleItalic, weight: render.FontWeightNormal},
		shorthandText: {style: render.FontStyleItalic, weight: render.FontWeightBold},
		normalText:    {style: render.FontStyleNormal, weight: render.FontWeightNormal},
	}
	seen := make(map[*dom.Node]bool)
	for _, command := range frame.DisplayList.Commands {
		expected, ok := want[command.Node]
		if !ok || command.Kind != render.DrawTextCommand {
			continue
		}
		seen[command.Node] = true
		if command.FontStyle != expected.style || command.FontWeight != expected.weight {
			t.Errorf("text %q face = style %v weight %v, want %v/%v", command.Text, command.FontStyle, command.FontWeight, expected.style, expected.weight)
		}
	}
	for node := range want {
		if !seen[node] {
			t.Errorf("no text command for node %p", node)
		}
	}
}
