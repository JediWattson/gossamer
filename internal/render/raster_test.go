package render

import (
	"image"
	"image/color"
	"strings"
	"testing"
)

func TestRasterizeAtScaleKeepsCSSGeometryAndProducesDevicePixels(t *testing.T) {
	t.Parallel()
	frame := &Frame{
		Viewport: Viewport{Width: 10, Height: 6},
		DisplayList: DisplayList{
			Viewport: Viewport{Width: 10, Height: 6},
			Commands: []Command{{Kind: FillRectCommand, Rect: Rect{X: 2, Y: 1, Width: 3, Height: 2}, Color: color.NRGBA{R: 0x24, G: 0x68, B: 0xac, A: 0xff}}},
		},
	}
	canvas, err := RasterizeAtScale(frame, 2)
	if err != nil {
		t.Fatal(err)
	}
	if canvas.Bounds() != image.Rect(0, 0, 20, 12) {
		t.Fatalf("scaled canvas = %v, want 20x12", canvas.Bounds())
	}
	if got := color.NRGBAModel.Convert(canvas.At(4, 2)).(color.NRGBA); got != (color.NRGBA{R: 0x24, G: 0x68, B: 0xac, A: 0xff}) {
		t.Fatalf("scaled CSS rect origin = %#v", got)
	}
	if got := canvas.RGBAAt(10, 2); got.A != 0 {
		t.Fatalf("pixel outside scaled rect = %#v, want transparent", got)
	}
	if frame.Viewport != (Viewport{Width: 10, Height: 6}) || frame.DisplayList.Viewport != frame.Viewport {
		t.Fatal("device rasterization mutated CSS-pixel frame geometry")
	}
}

func TestCommandScratchBoundsUsesClipIntersection(t *testing.T) {
	t.Parallel()
	command := Command{
		HasClip: true, Clip: Rect{X: 20, Y: 10, Width: 40, Height: 30},
		RoundedClipCount: 1,
		RoundedClips:     [maxCommandRoundedClips]RoundedClip{{Rect: Rect{X: 30, Y: 5, Width: 20, Height: 20}}},
	}
	if got, want := commandScratchBounds(command, image.Rect(0, 0, 800, 600)), image.Rect(30, 10, 50, 25); got != want {
		t.Fatalf("scratch bounds = %v, want %v", got, want)
	}
}

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

func TestRasterGroupBoundsTightenNestedEffectLayers(t *testing.T) {
	t.Parallel()

	commands := []Command{
		{Kind: BeginOpacityCommand, Opacity: 0.5},
		{Kind: BeginTransformCommand, Transform: AffineTransform{A: 1, D: 1, E: 50}},
		{Kind: FillRectCommand, Rect: Rect{X: 1, Y: 2, Width: 3, Height: 4}, Color: opaqueWhite},
		{Kind: EndTransformCommand},
		{Kind: EndOpacityCommand},
	}
	bounds := rasterGroupBounds(commands, image.Rect(0, 0, 100, 100))
	if got, want := bounds[1], image.Rect(1, 2, 4, 6); got != want {
		t.Fatalf("transform source bounds = %v, want %v", got, want)
	}
	if got, want := bounds[0], image.Rect(51, 2, 54, 6); got != want {
		t.Fatalf("opacity output bounds = %v, want %v", got, want)
	}
}

func TestRasterizeRectangularClipDrawsDirectlyWithinBounds(t *testing.T) {
	t.Parallel()

	fonts, err := newFontBook()
	if err != nil {
		t.Fatal(err)
	}
	defer fonts.Close()
	canvas, err := rasterize(DisplayList{
		Viewport: Viewport{Width: 8, Height: 8},
		Commands: []Command{
			{Kind: BeginClipCommand, Rect: Rect{X: 2, Y: 1, Width: 3, Height: 4}},
			{Kind: FillRectCommand, Rect: Rect{Width: 8, Height: 8}, Color: color.NRGBA{R: 0xff, A: 0xff}},
			{Kind: EndClipCommand},
		},
	}, fonts)
	if err != nil {
		t.Fatal(err)
	}
	if got := canvas.RGBAAt(3, 2); got.R != 0xff || got.A != 0xff {
		t.Fatalf("inside clipped pixel = %#v, want opaque red", got)
	}
	for _, point := range []image.Point{{X: 1, Y: 2}, {X: 5, Y: 2}, {X: 3, Y: 5}} {
		if got := canvas.RGBAAt(point.X, point.Y); got.A != 0 {
			t.Fatalf("outside clipped pixel %v = %#v, want transparent", point, got)
		}
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
