package render_test

import (
	"image/color"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestColorFunctionsFlowFromDOMThroughDisplayList(t *testing.T) {
	t.Parallel()

	document, body := boxModelDocument()
	target := dom.NewElement("div", dom.Attribute{Name: "style", Value: `
		display:block;
		width:20px;
		height:10px;
		background-color:hwb(0 20% 30%);
		border:2px solid rgb(0 50% 100%);
		color:hsl(120 100% 25%);
	`})
	target.AppendChild(dom.NewText("color"))
	body.AppendChild(target)

	frame, err := render.Render(document, render.Viewport{Width: 200, Height: 100})
	if err != nil {
		t.Fatal(err)
	}
	background := color.NRGBA{R: 179, G: 51, B: 51, A: 255}
	border := color.NRGBA{G: 128, B: 255, A: 255}
	text := color.NRGBA{G: 128, A: 255}
	if len(borderTestFillCommands(frame.DisplayList.Commands, background)) != 1 {
		t.Errorf("display list does not contain the functional background color")
	}
	if len(borderTestFillCommands(frame.DisplayList.Commands, border)) != 4 {
		t.Errorf("display list does not contain four functional border-color fills")
	}
	if commandIndex(frame.DisplayList.Commands, func(command render.Command) bool {
		return command.Kind == render.DrawTextCommand && command.Text == "color" && command.Color == text
	}) < 0 {
		t.Errorf("display list does not contain text using the functional color")
	}
}
