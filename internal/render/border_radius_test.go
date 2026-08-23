package render

import (
	"image/color"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
)

func TestRenderPaintsBorderRadiusAgainstTheBorderBox(t *testing.T) {
	t.Parallel()

	document := radiusTestDocument("width:100px;height:50px;background:#ff0000;border-radius:20px")
	frame, err := Render(document, Viewport{Width: 120, Height: 70})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	canvas, err := Rasterize(frame)
	if err != nil {
		t.Fatalf("Rasterize() error = %v", err)
	}
	if got := color.NRGBAModel.Convert(canvas.At(0, 0)).(color.NRGBA); got != (color.NRGBA{R: 255, G: 255, B: 255, A: 255}) {
		t.Errorf("rounded corner pixel = %#v, want white canvas", got)
	}
	if got := color.NRGBAModel.Convert(canvas.At(20, 5)).(color.NRGBA); got != (color.NRGBA{R: 255, A: 255}) {
		t.Errorf("rounded interior pixel = %#v, want red", got)
	}

	found := false
	for _, command := range frame.DisplayList.Commands {
		if command.Kind == FillRectCommand && command.Color == (color.NRGBA{R: 255, A: 255}) {
			found = true
			if command.Radii.TopLeft != (CornerRadius{X: 20, Y: 20}) {
				t.Errorf("top-left used radius = %#v, want 20px", command.Radii.TopLeft)
			}
		}
	}
	if !found {
		t.Fatal("rounded background command was not emitted")
	}
}

func TestUsedCornerRadiiScaleOverlappingPercentages(t *testing.T) {
	t.Parallel()

	document := radiusTestDocument("width:100px;height:40px;background:red;border-radius:80%")
	frame, err := Render(document, Viewport{Width: 120, Height: 60})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	for _, command := range frame.DisplayList.Commands {
		if command.Kind == FillRectCommand && command.Color.R == 255 && command.Color.G == 0 {
			if command.Radii.TopLeft.X != 50 || command.Radii.TopLeft.Y != 20 {
				t.Fatalf("normalized radius = %#v, want 50x20", command.Radii.TopLeft)
			}
			return
		}
	}
	t.Fatal("rounded background command was not emitted")
}

func radiusTestDocument(targetStyle string) *dom.Node {
	document := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body", dom.Attribute{Name: "style", Value: "margin:0"})
	target := dom.NewElement("div", dom.Attribute{Name: "style", Value: targetStyle})
	body.AppendChild(target)
	html.AppendChild(body)
	document.AppendChild(html)
	return document
}
