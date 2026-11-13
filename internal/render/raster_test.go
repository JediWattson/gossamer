package render

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"strings"
	"testing"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/math/f64"
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

func TestVectorPaintBoundsTightenToVisibleShapePixels(t *testing.T) {
	t.Parallel()

	shape := Rect{X: 721.25, Y: 413.75, Width: 31.5, Height: 18.25}
	if got, want := vectorPaintBounds(image.Rect(100, 200, 1300, 1000), shape), image.Rect(721, 413, 753, 432); got != want {
		t.Fatalf("vector paint bounds = %v, want %v", got, want)
	}
	if got, want := vectorPaintBounds(image.Rect(730, 420, 740, 428), shape), image.Rect(730, 420, 740, 428); got != want {
		t.Fatalf("clipped vector paint bounds = %v, want %v", got, want)
	}
	if got := vectorPaintBounds(image.Rect(0, 0, 20, 20), shape); !got.Empty() {
		t.Fatalf("offscreen vector paint bounds = %v, want empty", got)
	}
	tight := image.Rect(721, 413, 753, 432)
	if got, want := newShapeRasterizer(tight, image.Rect(100, 200, 1300, 1000)).Size(), (image.Point{X: 513, Y: 19}); got != want {
		t.Fatalf("large-target rasterizer size = %v, want %v", got, want)
	}
	if got, want := newShapeRasterizer(tight, image.Rect(700, 400, 800, 500)).Size(), tight.Size(); got != want {
		t.Fatalf("small-target rasterizer size = %v, want %v", got, want)
	}
}

func TestTightVectorRasterizationMatchesFullTargetPixels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		clip  image.Rectangle
		paint func(target draw.Image, tight bool)
	}{
		{
			name: "fractional ellipse on non-zero canvas",
			clip: image.Rect(100, 200, 1300, 1000),
			paint: func(target draw.Image, tight bool) {
				rectangle := Rect{X: 721.25, Y: 413.75, Width: 31.5, Height: 18.25}
				fill := color.NRGBA{R: 0xe4, G: 0x61, B: 0x8d, A: 0xb7}
				if tight {
					paintEllipse(target, rectangle, fill)
				} else {
					paintEllipseWithin(target, rectangle, fill, target.Bounds())
				}
			},
		},
		{
			name: "rounded rectangle through subimage clip",
			clip: image.Rect(714, 410, 744, 429),
			paint: func(target draw.Image, tight bool) {
				rectangle := Rect{X: 708.5, Y: 404.25, Width: 48.75, Height: 32.5}
				radii := CornerRadii{
					TopLeft: CornerRadius{X: 11.25, Y: 8.5}, TopRight: CornerRadius{X: 7.5, Y: 12.25},
					BottomRight: CornerRadius{X: 13.25, Y: 6.75}, BottomLeft: CornerRadius{X: 4.5, Y: 9.25},
				}
				fill := color.NRGBA{R: 0x37, G: 0xb8, B: 0x73, A: 0xcf}
				if tight {
					paintRoundedRect(target, rectangle, radii, fill)
				} else {
					paintRoundedRectWithin(target, rectangle, radii, fill, target.Bounds())
				}
			},
		},
		{
			name: "fractional rounded rectangle on non-zero canvas",
			clip: image.Rect(100, 200, 1300, 1000),
			paint: func(target draw.Image, tight bool) {
				rectangle := Rect{X: 811.375, Y: 602.625, Width: 43.25, Height: 27.5}
				radii := CornerRadii{
					TopLeft: CornerRadius{X: 9.5, Y: 7.25}, TopRight: CornerRadius{X: 4.75, Y: 10.5},
					BottomRight: CornerRadius{X: 12.25, Y: 5.5}, BottomLeft: CornerRadius{X: 6.5, Y: 8.75},
				}
				fill := color.NRGBA{R: 0x37, G: 0xb8, B: 0x73, A: 0xcf}
				if tight {
					paintRoundedRect(target, rectangle, radii, fill)
				} else {
					paintRoundedRectWithin(target, rectangle, radii, fill, target.Bounds())
				}
			},
		},
	}

	canvasBounds := image.Rect(100, 200, 1300, 1000)
	background := image.NewUniform(color.NRGBA{R: 0x19, G: 0x2d, B: 0x4a, A: 0xd3})
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tight := image.NewRGBA(canvasBounds)
			full := image.NewRGBA(canvasBounds)
			draw.Draw(tight, canvasBounds, background, image.Point{}, draw.Src)
			draw.Draw(full, canvasBounds, background, image.Point{}, draw.Src)
			test.paint(tight.SubImage(test.clip).(*image.RGBA), true)
			test.paint(full.SubImage(test.clip).(*image.RGBA), false)
			assertRGBAEqual(t, tight, full)
		})
	}
}

func TestVectorDamageCropPreservesStableLayerOrigin(t *testing.T) {
	t.Parallel()

	layerBounds := image.Rect(7, 6, 113, 100)
	damageBounds := image.Rect(5, 59, 115, 86).Intersect(layerBounds)
	background := image.NewUniform(color.NRGBA{R: 0x19, G: 0x2d, B: 0x4a, A: 0xd3})
	tests := []struct {
		name  string
		paint func(draw.Image)
	}{
		{
			name: "rounded rectangle",
			paint: func(target draw.Image) {
				paintRoundedRectWithMode(target, Rect{X: 7, Y: 6, Width: 106, Height: 78}, CornerRadii{
					TopLeft: CornerRadius{X: 14, Y: 12}, TopRight: CornerRadius{X: 10, Y: 16},
					BottomRight: CornerRadius{X: 16, Y: 11}, BottomLeft: CornerRadius{X: 12, Y: 15},
				}, color.NRGBA{R: 0x34, G: 0x42, B: 0x5c, A: 0xff}, layerBounds)
			},
		},
		{
			name: "fractional ellipse",
			paint: func(target draw.Image) {
				paintEllipseWithMode(target, Rect{X: 23.25, Y: 28.5, Width: 67.5, Height: 52.75}, color.NRGBA{R: 0xd8, G: 0x78, B: 0x49, A: 0xc9}, layerBounds)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reference := image.NewRGBA(layerBounds)
			actual := image.NewRGBA(layerBounds)
			draw.Draw(reference, layerBounds, background, image.Point{}, draw.Src)
			draw.Draw(actual, layerBounds, background, image.Point{}, draw.Src)
			test.paint(reference)
			test.paint(actual.SubImage(damageBounds).(*image.RGBA))
			assertRGBARegionEqual(t, actual, reference, damageBounds)
		})
	}
}

func TestRoundedAlphaDamageCropPreservesStableLayerOrigin(t *testing.T) {
	t.Parallel()

	layerBounds := image.Rect(7, 6, 113, 100)
	damageBounds := image.Rect(5, 59, 115, 86).Intersect(layerBounds)
	rectangle := Rect{X: 7, Y: 6, Width: 106, Height: 78}
	radii := CornerRadii{
		TopLeft: CornerRadius{X: 14, Y: 12}, TopRight: CornerRadius{X: 10, Y: 16},
		BottomRight: CornerRadius{X: 16, Y: 11}, BottomLeft: CornerRadius{X: 12, Y: 15},
	}
	reference := image.NewAlpha(layerBounds)
	actual := image.NewAlpha(layerBounds)
	background := image.NewUniform(color.Alpha{A: 0x35})
	draw.Draw(reference, layerBounds, background, image.Point{}, draw.Src)
	draw.Draw(actual, layerBounds, background, image.Point{}, draw.Src)
	paintRoundedRectWithMode(reference, rectangle, radii, color.NRGBA{A: 0xc7}, layerBounds)
	paintRoundedRectWithMode(actual.SubImage(damageBounds).(*image.Alpha), rectangle, radii, color.NRGBA{A: 0xc7}, layerBounds)
	for y := damageBounds.Min.Y; y < damageBounds.Max.Y; y++ {
		for x := damageBounds.Min.X; x < damageBounds.Max.X; x++ {
			if got, want := actual.AlphaAt(x, y), reference.AlphaAt(x, y); got != want {
				t.Fatalf("alpha at (%d,%d) = %#v, want %#v", x, y, got, want)
			}
		}
	}
}

func TestRoundedRectAlphaMaskPreservesFullTargetPixels(t *testing.T) {
	t.Parallel()

	bounds := image.Rect(300, 400, 1500, 1200)
	rectangle := Rect{X: 811.375, Y: 602.625, Width: 43.25, Height: 27.5}
	radii := CornerRadii{
		TopLeft: CornerRadius{X: 9.5, Y: 7.25}, TopRight: CornerRadius{X: 4.75, Y: 10.5},
		BottomRight: CornerRadius{X: 12.25, Y: 5.5}, BottomLeft: CornerRadius{X: 6.5, Y: 8.75},
	}
	tight := image.NewAlpha(bounds)
	full := image.NewAlpha(bounds)
	paintRoundedRect(tight, rectangle, radii, color.NRGBA{A: 0xc7})
	paintRoundedRectWithin(full, rectangle, radii, color.NRGBA{A: 0xc7}, full.Bounds())
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if got, want := tight.AlphaAt(x, y), full.AlphaAt(x, y); got != want {
				t.Fatalf("alpha at (%d,%d) = %#v, want %#v", x, y, got, want)
			}
		}
	}
}

func BenchmarkPaintTightVectorShapes(b *testing.B) {
	canvas := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	b.Run("rounded rectangle", func(b *testing.B) {
		rectangle := Rect{X: 900.25, Y: 500.75, Width: 48.5, Height: 28.25}
		radii := CornerRadii{
			TopLeft: CornerRadius{X: 8, Y: 8}, TopRight: CornerRadius{X: 8, Y: 8},
			BottomRight: CornerRadius{X: 8, Y: 8}, BottomLeft: CornerRadius{X: 8, Y: 8},
		}
		b.Run("tight", func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				paintRoundedRect(canvas, rectangle, radii, opaqueWhite)
			}
		})
		b.Run("full target baseline", func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				paintRoundedRectWithin(canvas, rectangle, radii, opaqueWhite, canvas.Bounds())
			}
		})
	})
	b.Run("ellipse", func(b *testing.B) {
		rectangle := Rect{X: 900.25, Y: 500.75, Width: 32.5, Height: 32.5}
		b.Run("tight", func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				paintEllipse(canvas, rectangle, opaqueWhite)
			}
		})
		b.Run("full target baseline", func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				paintEllipseWithin(canvas, rectangle, opaqueWhite, canvas.Bounds())
			}
		})
	})
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

func TestDrawRGBAOpacityOverMatchesDrawMask(t *testing.T) {
	t.Parallel()
	bounds := image.Rect(3, 5, 29, 18)
	destination := image.NewRGBA(bounds)
	source := image.NewRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			destination.SetRGBA(x, y, color.RGBA{
				R: uint8((x*7 + y*3) % 96), G: uint8((x*5 + y*11) % 128),
				B: uint8((x*13 + y) % 160), A: 191,
			})
			source.SetRGBA(x, y, color.RGBA{
				R: uint8((x*3 + y*5) % 128), G: uint8((x + y*7) % 96),
				B: uint8((x*9 + y*3) % 144), A: 173,
			})
		}
	}

	for _, alpha := range []uint8{0, 1, 63, 127, 242, 254, 255} {
		alpha := alpha
		t.Run(fmt.Sprintf("alpha_%d", alpha), func(t *testing.T) {
			got := cloneRGBA(destination)
			want := cloneRGBA(destination)
			drawRGBAOpacityOver(got, bounds, source, bounds.Min, alpha)
			draw.DrawMask(want, bounds, source, bounds.Min, image.NewUniform(color.Alpha{A: alpha}), bounds.Min, draw.Over)
			assertRGBAEqual(t, got, want)
		})
	}
}

func TestCachedRGBA64TransformSamplesMatchDirectApproxBiLinearOver(t *testing.T) {
	t.Parallel()

	sourceBounds := image.Rect(7, 5, 43, 31)
	destinationBounds := image.Rect(0, 0, 96, 80)
	source := image.NewRGBA(sourceBounds)
	background := image.NewRGBA(destinationBounds)
	for y := sourceBounds.Min.Y; y < sourceBounds.Max.Y; y++ {
		for x := sourceBounds.Min.X; x < sourceBounds.Max.X; x++ {
			alpha := uint8(40 + (x*17+y*29)%216)
			source.SetRGBA(x, y, color.RGBA{
				R: uint8((x*11 + y*7) % (int(alpha) + 1)),
				G: uint8((x*5 + y*13) % (int(alpha) + 1)),
				B: uint8((x*19 + y*3) % (int(alpha) + 1)), A: alpha,
			})
		}
	}
	for y := destinationBounds.Min.Y; y < destinationBounds.Max.Y; y++ {
		for x := destinationBounds.Min.X; x < destinationBounds.Max.X; x++ {
			alpha := uint8(80 + (x*23+y*31)%176)
			background.SetRGBA(x, y, color.RGBA{
				R: uint8((x*7 + y*17) % (int(alpha) + 1)),
				G: uint8((x*3 + y*11) % (int(alpha) + 1)),
				B: uint8((x*13 + y*5) % (int(alpha) + 1)), A: alpha,
			})
		}
	}
	transforms := []AffineTransform{
		{A: 1, D: 1, E: 18.25, F: 13.5},
		{A: 0.93, B: 0.21, C: -0.17, D: 1.04, E: 24.75, F: 8.125},
		{A: 1.22, B: -0.08, C: 0.14, D: 0.81, E: 11.375, F: 20.625},
	}
	for _, transform := range transforms {
		matrix := f64.Aff3{
			transform.A, transform.C, transform.E,
			transform.B, transform.D, transform.F,
		}
		cacheBounds := expandDamageRect(transformedGroupBounds(sourceBounds, transform), 2).Intersect(destinationBounds)
		cached := image.NewRGBA64(cacheBounds)
		xdraw.ApproxBiLinear.Transform(cached, matrix, source, source.Bounds(), draw.Src, nil)
		for _, crop := range []image.Rectangle{
			destinationBounds,
			image.Rect(9, 7, 71, 59),
			image.Rect(35, 18, 88, 73),
		} {
			direct := cloneRGBA(background)
			xdraw.ApproxBiLinear.Transform(direct.SubImage(crop).(*image.RGBA), matrix, source, source.Bounds(), draw.Over, nil)
			got := cloneRGBA(background)
			drawRGBA64Over(got.SubImage(crop).(*image.RGBA), cached)
			assertRGBAEqual(t, got, direct)
		}
	}
}

func withinOne(got, want uint8) bool {
	difference := int(got) - int(want)
	return difference >= -1 && difference <= 1
}

func assertRGBAEqual(t *testing.T, got, want *image.RGBA) {
	t.Helper()
	if got.Bounds() != want.Bounds() {
		t.Fatalf("image bounds = %v, want %v", got.Bounds(), want.Bounds())
	}
	bounds := got.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if gotPixel, wantPixel := got.RGBAAt(x, y), want.RGBAAt(x, y); gotPixel != wantPixel {
				t.Fatalf("pixel at (%d,%d) = %#v, want %#v", x, y, gotPixel, wantPixel)
			}
		}
	}
}

func assertRGBARegionEqual(t *testing.T, got, want *image.RGBA, bounds image.Rectangle) {
	t.Helper()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if gotPixel, wantPixel := got.RGBAAt(x, y), want.RGBAAt(x, y); gotPixel != wantPixel {
				t.Fatalf("pixel at (%d,%d) = %#v, want %#v", x, y, gotPixel, wantPixel)
			}
		}
	}
}
