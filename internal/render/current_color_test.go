package render_test

import (
	"image/color"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestCurrentColorFlowsThroughBackgroundBorderAndTextPaint(t *testing.T) {
	t.Parallel()

	document, body := boxModelDocument()
	target := dom.NewElement("div", dom.Attribute{Name: "style", Value: `
		display:block;
		width:30px;
		height:16px;
		background-color:currentcolor;
		border:2px solid currentcolor;
		color:rgb(12 34 56);
	`})
	target.AppendChild(dom.NewText("current"))
	body.AppendChild(target)

	frame, err := render.Render(document, render.Viewport{Width: 200, Height: 100})
	if err != nil {
		t.Fatal(err)
	}
	want := color.NRGBA{R: 12, G: 34, B: 56, A: 255}
	if got := len(borderTestFillCommands(frame.DisplayList.Commands, want)); got != 5 {
		t.Errorf("currentcolor fills = %d, want one background plus four borders", got)
	}
	if commandIndex(frame.DisplayList.Commands, func(command render.Command) bool {
		return command.Kind == render.DrawTextCommand && command.Text == "current" && command.Color == want
	}) < 0 {
		t.Error("text did not use the same current color")
	}
}
