package render

import (
	"image/color"
	"testing"
)

func TestRasterizeCompositesOpacityAsAGroup(t *testing.T) {
	t.Parallel()

	fonts, err := newFontBook()
	if err != nil {
		t.Fatalf("newFontBook() error = %v", err)
	}
	defer fonts.Close()

	viewport := Viewport{Width: 1, Height: 1}
	fullCanvas := Rect{Width: 1, Height: 1}
	displayList := DisplayList{
		Viewport: viewport,
		Commands: []Command{
			{Kind: FillRectCommand, Rect: fullCanvas, Color: opaqueWhite},
			{Kind: BeginOpacityCommand, Opacity: 0.5},
			{Kind: FillRectCommand, Rect: fullCanvas, Color: color.NRGBA{R: 0xff, A: 0xff}},
			// Overlap deliberately: the group should become opaque red first and
			// then be composited once at 50 percent.
			{Kind: FillRectCommand, Rect: fullCanvas, Color: color.NRGBA{R: 0xff, A: 0xff}},
			{Kind: EndOpacityCommand},
		},
	}

	canvas, err := rasterize(displayList, fonts)
	if err != nil {
		t.Fatalf("rasterize() error = %v", err)
	}
	got := color.NRGBAModel.Convert(canvas.At(0, 0)).(color.NRGBA)
	if got.R != 0xff || got.A != 0xff || !withinOne(got.G, 0x7f) || !withinOne(got.B, 0x7f) {
		t.Errorf("grouped overlap pixel = %#v, want approximately #ff7f7f", got)
	}
}

func TestRasterizeRejectsUnbalancedOpacityGroups(t *testing.T) {
	t.Parallel()

	fonts, err := newFontBook()
	if err != nil {
		t.Fatalf("newFontBook() error = %v", err)
	}
	defer fonts.Close()

	viewport := Viewport{Width: 1, Height: 1}
	for name, commands := range map[string][]Command{
		"unmatched end":  {{Kind: EndOpacityCommand}},
		"unclosed begin": {{Kind: BeginOpacityCommand, Opacity: 0.5}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := rasterize(DisplayList{Viewport: viewport, Commands: commands}, fonts); err == nil {
				t.Error("rasterize() error = nil, want opacity-group error")
			}
		})
	}
}

func withinOne(got, want uint8) bool {
	difference := int(got) - int(want)
	return difference >= -1 && difference <= 1
}
