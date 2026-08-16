package render

import (
	"image/color"
	"strings"
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

func TestRasterizeSidewaysTextRotatesWithinPhysicalBounds(t *testing.T) {
	t.Parallel()

	fonts, err := newFontBook()
	if err != nil {
		t.Fatal(err)
	}
	defer fonts.Close()
	command := Command{
		Kind: DrawTextCommand, Text: "Latin", Color: color.NRGBA{A: 0xff},
		FontSize: 12, FontWeight: FontWeightNormal, FontStyle: FontStyleNormal, FontFamily: FontFamilyMonospace,
		Rect:            Rect{X: 10, Y: 8, Width: 16, Height: 40},
		textOrientation: textPaintSidewaysRight,
		textBounds:      Rect{X: 10, Y: 8, Width: 16, Height: 40},
		textWidth:       40,
		textHeight:      16,
		textBaseline:    13,
	}
	canvas, err := rasterize(DisplayList{Viewport: Viewport{Width: 60, Height: 60}, Commands: []Command{command}}, fonts)
	if err != nil {
		t.Fatal(err)
	}
	minimumX, minimumY, maximumX, maximumY := 60, 60, -1, -1
	for y := 0; y < 60; y++ {
		for x := 0; x < 60; x++ {
			if canvas.RGBAAt(x, y).A == 0 {
				continue
			}
			minimumX, minimumY = min(minimumX, x), min(minimumY, y)
			maximumX, maximumY = max(maximumX, x), max(maximumY, y)
		}
	}
	if maximumX < minimumX || minimumX < 10 || maximumX >= 26 || minimumY < 8 || maximumY >= 48 {
		t.Fatalf("sideways glyph bounds = (%d,%d)-(%d,%d), want inside (10,8)-(26,48)", minimumX, minimumY, maximumX, maximumY)
	}
	if maximumY-minimumY <= maximumX-minimumX {
		t.Fatalf("sideways glyph extent = %dx%d, want a vertically advancing run", maximumX-minimumX+1, maximumY-minimumY+1)
	}
}

func TestRasterizeSidewaysTextBudgetFailsBeforeAllocation(t *testing.T) {
	t.Parallel()

	fonts, err := newFontBook()
	if err != nil {
		t.Fatal(err)
	}
	defer fonts.Close()
	_, err = rasterize(DisplayList{
		Viewport: Viewport{Width: 1, Height: 1},
		Commands: []Command{{
			Kind: DrawTextCommand, Text: "x", textOrientation: textPaintSidewaysRight,
			textWidth: maxSidewaysTextPixels + 1, textHeight: 1,
		}}}, fonts)
	if err == nil || !strings.Contains(err.Error(), "sideways text exceeds") {
		t.Fatalf("sideways text budget error = %v", err)
	}
}

func TestRasterizeUprightTextStacksUnrotatedUnitsWithinPhysicalBounds(t *testing.T) {
	t.Parallel()

	fonts, err := newFontBook()
	if err != nil {
		t.Fatal(err)
	}
	defer fonts.Close()
	command := Command{
		Kind: DrawTextCommand, Text: "AB", Color: color.NRGBA{A: 0xff},
		FontSize: 14, FontWeight: FontWeightNormal, FontStyle: FontStyleNormal, FontFamily: FontFamilyMonospace,
		Rect:            Rect{X: 10, Y: 8, Width: 20, Height: 36},
		textOrientation: textPaintUpright,
		textBounds:      Rect{X: 10, Y: 8, Width: 20, Height: 36},
	}
	canvas, err := rasterize(DisplayList{Viewport: Viewport{Width: 60, Height: 60}, Commands: []Command{command}}, fonts)
	if err != nil {
		t.Fatal(err)
	}
	minimumX, minimumY, maximumX, maximumY := 60, 60, -1, -1
	upperInk, lowerInk := false, false
	for y := 0; y < 60; y++ {
		for x := 0; x < 60; x++ {
			if canvas.RGBAAt(x, y).A == 0 {
				continue
			}
			minimumX, minimumY = min(minimumX, x), min(minimumY, y)
			maximumX, maximumY = max(maximumX, x), max(maximumY, y)
			if y < 26 {
				upperInk = true
			} else {
				lowerInk = true
			}
		}
	}
	if maximumX < minimumX || minimumX < 10 || maximumX >= 30 || minimumY < 8 || maximumY >= 44 {
		t.Fatalf("upright glyph bounds = (%d,%d)-(%d,%d), want inside (10,8)-(30,44)", minimumX, minimumY, maximumX, maximumY)
	}
	if !upperInk || !lowerInk {
		t.Fatalf("upright run ink upper/lower = %t/%t, want both cells painted", upperInk, lowerInk)
	}
}

func TestRasterizeUprightTextBudgetsFailBeforeDrawing(t *testing.T) {
	t.Parallel()

	fonts, err := newFontBook()
	if err != nil {
		t.Fatal(err)
	}
	defer fonts.Close()
	_, err = rasterize(DisplayList{
		Viewport: Viewport{Width: 1, Height: 1},
		Commands: []Command{{
			Kind: DrawTextCommand, Text: strings.Repeat("A", maxUprightTextUnits+1),
			textOrientation: textPaintUpright, textBounds: Rect{Width: 1, Height: 1},
		}},
	}, fonts)
	if err == nil || !strings.Contains(err.Error(), "upright text exceeds") {
		t.Fatalf("upright text budget error = %v", err)
	}
}

func withinOne(got, want uint8) bool {
	difference := int(got) - int(want)
	return difference >= -1 && difference <= 1
}
