package render

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"sync"
	"testing"

	"golang.org/x/image/font/gofont/goregular"
)

func TestStreamingScaleMatchesMaterializedDisplayList(t *testing.T) {
	t.Parallel()

	frame := rasterParityFrame()
	rasterizer := NewFrameRasterizer()
	defer rasterizer.Close()
	for _, test := range []struct {
		name  string
		scale float64
	}{{name: "scale_1", scale: 1}, {name: "scale_1_5", scale: 1.5}, {name: "scale_2", scale: 2}} {
		t.Run(test.name, func(t *testing.T) {
			got, err := rasterizer.RasterizeAtScale(frame, test.scale)
			if err != nil {
				t.Fatalf("streaming RasterizeAtScale() error = %v", err)
			}

			fonts, err := newFontBook(frame.Fonts)
			if err != nil {
				t.Fatal(err)
			}
			defer fonts.Close()
			reference := materializedScaleReference(frame.DisplayList, test.scale)
			want, err := rasterize(reference, fonts)
			if err != nil {
				t.Fatalf("materialized rasterize() error = %v", err)
			}
			assertRGBAEqual(t, got, want)
		})
	}
}

func TestFrameRasterizerRetainsAndRebuildsFontBook(t *testing.T) {
	t.Parallel()

	frame := retainedFontFrame([]WebFontFace{{
		Family: "Retained Fixture", WeightMin: 400, WeightMax: 400,
		Source: "fixture.ttf", Data: goregular.TTF,
	}})
	rasterizer := NewFrameRasterizer()
	if _, err := rasterizer.RasterizeAtScale(frame, 2); err != nil {
		t.Fatal(err)
	}
	first := rasterizer.fonts
	if first == nil || len(first.faces) == 0 {
		t.Fatal("first replay did not retain its constructed web-font face")
	}
	rasterizer.overlayPaint.layers.putOpacity(1, 2, 1, image.Rect(0, 0, 2, 2), 128, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	if rasterizer.overlayPaint.layers.bytes == 0 {
		t.Fatal("fixture did not seed retained opacity paint")
	}

	copyOfDescriptors := append([]WebFontFace(nil), frame.Fonts...)
	sameFrame := retainedFontFrame(copyOfDescriptors)
	if _, err := rasterizer.RasterizeAtScale(sameFrame, 1.5); err != nil {
		t.Fatal(err)
	}
	if rasterizer.fonts != first {
		t.Fatal("equivalent immutable font set rebuilt its font book")
	}

	changedDescriptors := append([]WebFontFace(nil), frame.Fonts...)
	changedDescriptors[0].Family = "Replacement Fixture"
	changedFrame := retainedFontFrame(changedDescriptors)
	if _, err := rasterizer.Rasterize(changedFrame); err != nil {
		t.Fatal(err)
	}
	if rasterizer.fonts == first {
		t.Fatal("changed immutable font descriptor reused the old font book")
	}
	if rasterizer.overlayPaint.layers.bytes != 0 {
		t.Fatal("changed immutable font descriptor retained font-dependent opacity paint")
	}
	if first.faces != nil {
		t.Fatal("replaced font book still retains closed font faces")
	}

	second := rasterizer.fonts
	changedData := append([]byte(nil), changedDescriptors[0].Data...)
	changedDescriptors = append([]WebFontFace(nil), changedDescriptors...)
	changedDescriptors[0].Data = changedData
	if _, err := rasterizer.Rasterize(retainedFontFrame(changedDescriptors)); err != nil {
		t.Fatal(err)
	}
	if rasterizer.fonts != second {
		t.Fatal("identical immutable font bytes on new backing storage rebuilt the font book")
	}
	changedData = append([]byte(nil), changedData...)
	changedData[len(changedData)-1] ^= 0xff
	changedDescriptors[0].Data = changedData
	if _, err := rasterizer.Rasterize(retainedFontFrame(changedDescriptors)); err != nil {
		t.Fatal(err)
	}
	if rasterizer.fonts == second {
		t.Fatal("changed immutable font bytes reused the old font book")
	}

	if err := rasterizer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := rasterizer.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if _, err := rasterizer.Rasterize(frame); err == nil {
		t.Fatal("Rasterize() after Close() error = nil")
	}
}

func TestFrameRasterizerSerializesConcurrentReplayAndClose(t *testing.T) {
	frame := retainedFontFrame(nil)
	rasterizer := NewFrameRasterizer()

	const workers = 8
	errors := make(chan error, workers)
	var wait sync.WaitGroup
	wait.Add(workers)
	for index := 0; index < workers; index++ {
		go func(scale float64) {
			defer wait.Done()
			_, err := rasterizer.RasterizeAtScale(frame, scale)
			errors <- err
		}(1 + float64(index%3)/2)
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatalf("concurrent replay error = %v", err)
		}
	}
	if err := rasterizer.Close(); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkFrameRasterizerTransientVersusRetained(b *testing.B) {
	frame := retainedFontFrame([]WebFontFace{{
		Family: "Benchmark Fixture", WeightMin: 400, WeightMax: 400,
		Source: "benchmark.ttf", Data: goregular.TTF,
	}})

	b.Run("transient", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			if _, err := RasterizeAtScale(frame, 2); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("retained", func(b *testing.B) {
		rasterizer := NewFrameRasterizer()
		defer rasterizer.Close()
		b.ReportAllocs()
		for range b.N {
			if _, err := rasterizer.RasterizeAtScale(frame, 2); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkRasterizeAtScaleStreamingProjection(b *testing.B) {
	frame := rasterBenchmarkFrame()
	viewport, err := scaledViewport(frame.DisplayList.Viewport, 2)
	if err != nil {
		b.Fatal(err)
	}

	b.Run("materialized baseline", func(b *testing.B) {
		fonts, err := newFontBook(frame.Fonts)
		if err != nil {
			b.Fatal(err)
		}
		defer fonts.Close()
		b.ReportAllocs()
		for range b.N {
			displayList := materializedScaleReference(frame.DisplayList, 2)
			if _, err := rasterize(displayList, fonts); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("streaming", func(b *testing.B) {
		fonts, err := newFontBook(frame.Fonts)
		if err != nil {
			b.Fatal(err)
		}
		defer fonts.Close()
		b.ReportAllocs()
		for range b.N {
			if _, err := rasterizeAtScale(frame.DisplayList, fonts, 2, viewport); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func rasterBenchmarkFrame() *Frame {
	frame := retainedFontFrame([]WebFontFace{{
		Family: "Benchmark Fixture", WeightMin: 400, WeightMax: 400,
		Source: "benchmark.ttf", Data: goregular.TTF,
	}})
	for index := 0; index < 512; index++ {
		frame.DisplayList.Commands = append(frame.DisplayList.Commands, Command{
			Kind:  FillRectCommand,
			Rect:  Rect{X: float64(index % 32 * 10), Y: float64(index / 32 * 5), Width: 9.5, Height: 4.5},
			Color: color.NRGBA{R: uint8(index), G: 0x68, B: 0xac, A: 0xff},
		})
	}
	return frame
}

func rasterParityFrame() *Frame {
	imageFixture := image.NewRGBA(image.Rect(0, 0, 4, 3))
	draw.Draw(imageFixture, imageFixture.Bounds(), image.NewUniform(color.NRGBA{R: 0x35, G: 0xa8, B: 0xe0, A: 0xff}), image.Point{}, draw.Src)
	imageFixture.SetRGBA(1, 1, color.RGBA{R: 0xff, G: 0x44, B: 0x66, A: 0xc0})
	radii := CornerRadii{
		TopLeft: CornerRadius{X: 4.25, Y: 3.5}, TopRight: CornerRadius{X: 6.5, Y: 2.75},
		BottomRight: CornerRadius{X: 3.25, Y: 5.5}, BottomLeft: CornerRadius{X: 2.5, Y: 4.25},
	}
	roundedClip := RoundedClip{Rect: Rect{X: 8.25, Y: 7.5, Width: 63.5, Height: 45.75}, Radii: radii}
	gradient := Command{
		Kind: DrawGradientCommand, Rect: Rect{X: 11.25, Y: 10.5, Width: 52.5, Height: 37.25},
		GradientKind: LinearGradient, GradientAngle: 0.61, GradientStopCount: 3,
		GradientStops: [maxPaintGradientStops]GradientStop{
			{Offset: 0, Color: color.NRGBA{R: 0x21, G: 0x35, B: 0x64, A: 0xff}},
			{Offset: 0.45, Color: color.NRGBA{R: 0x79, G: 0x54, B: 0xb8, A: 0xe8}},
			{Offset: 1, Color: color.NRGBA{R: 0xe2, G: 0x84, B: 0x4f, A: 0xff}},
		},
		RoundedClipCount: 1, RoundedClips: [maxCommandRoundedClips]RoundedClip{roundedClip},
	}
	return &Frame{
		Viewport: Viewport{Width: 96, Height: 72},
		DisplayList: DisplayList{
			Viewport: Viewport{Width: 96, Height: 72},
			Commands: []Command{
				{Kind: FillRectCommand, Rect: Rect{Width: 96, Height: 72}, Color: color.NRGBA{R: 0x0d, G: 0x13, B: 0x20, A: 0xff}},
				{Kind: BeginOpacityCommand, Opacity: 0.82},
				{Kind: BeginTransformCommand, Transform: AffineTransform{A: 0.98, B: 0.03, C: -0.02, D: 1.01, E: 4.25, F: 2.75}},
				{Kind: BeginClipCommand, Rect: Rect{X: 7.5, Y: 6.25, Width: 68.5, Height: 51.25}, Radii: radii},
				gradient,
				{Kind: FillEllipseCommand, Rect: Rect{X: 19.5, Y: 15.25, Width: 17.75, Height: 13.5}, Color: color.NRGBA{R: 0xef, G: 0xc8, B: 0x55, A: 0xd7}},
				{Kind: DrawImageCommand, Rect: Rect{X: 41.25, Y: 13.5, Width: 21.5, Height: 18.25}, Image: imageFixture, Opacity: 0.73},
				{Kind: DrawRoundedBorderCommand, Rect: Rect{X: 14.5, Y: 34.25, Width: 48.75, Height: 15.5}, Radii: radii,
					BorderWidths: Edges{Top: 1.25, Right: 2.5, Bottom: 1.75, Left: 2.25},
					BorderColors: [4]color.NRGBA{{R: 0xff, A: 0xff}, {G: 0xff, A: 0xff}, {B: 0xff, A: 0xff}, {R: 0xff, G: 0xff, A: 0xff}},
					BorderStyles: [4]borderStyle{borderStyleSolid, borderStyleSolid, borderStyleSolid, borderStyleSolid}},
				{Kind: DrawTextCommand, Text: "Retina", X: 20.25, BaselineY: 47.5, FontSize: 11.5,
					FontFamily: FontFamilySansSerif, FontWeight: FontWeightBold, FontStyle: FontStyleItalic,
					Color: color.NRGBA{R: 0xf4, G: 0xf7, B: 0xff, A: 0xff}, textBounds: Rect{X: 20, Y: 36, Width: 41, Height: 14}},
				{Kind: EndClipCommand},
				{Kind: EndTransformCommand},
				{Kind: EndOpacityCommand},
				{Kind: DrawBoxShadowCommand, Rect: Rect{X: 70, Y: 53, Width: 12, Height: 8}, Radii: radii,
					Color: color.NRGBA{R: 0x11, G: 0x22, B: 0x44, A: 0x90}, ShadowOffsetX: 1.5, ShadowOffsetY: 2.25, ShadowBlur: 3.5, ShadowSpread: 0.75},
			},
		},
	}
}

func retainedFontFrame(fonts []WebFontFace) *Frame {
	command := Command{
		Kind: DrawTextCommand, Text: "retained", X: 4.5, BaselineY: 19.25, FontSize: 14,
		FontFamily: FontFamilySansSerif, FontWeight: FontWeightNormal, FontStyle: FontStyleNormal,
		Color:      color.NRGBA{R: 0xee, G: 0xf2, B: 0xff, A: 0xff},
		textBounds: Rect{X: 4, Y: 5, Width: 64, Height: 18},
	}
	if len(fonts) != 0 {
		command.WebFont = 1
	}
	return &Frame{
		Viewport: Viewport{Width: 320, Height: 80}, Fonts: fonts,
		DisplayList: DisplayList{
			Viewport: Viewport{Width: 320, Height: 80},
			Commands: []Command{
				{Kind: FillRectCommand, Rect: Rect{Width: 320, Height: 80}, Color: color.NRGBA{R: 0x17, G: 0x20, B: 0x31, A: 0xff}},
				command,
			},
		},
	}
}

// materializedScaleReference preserves the previous full-display-list
// projection independently of scaleRasterCommand. It is intentionally test
// only: production must never allocate this scaled command slice.
func materializedScaleReference(displayList DisplayList, scale float64) DisplayList {
	result := displayList
	result.Viewport = Viewport{
		Width:  int(math.Round(float64(displayList.Viewport.Width) * scale)),
		Height: int(math.Round(float64(displayList.Viewport.Height) * scale)),
	}
	result.Commands = make([]Command, len(displayList.Commands))
	for index, source := range displayList.Commands {
		command := source
		command.Rect = scaleRect(command.Rect, scale)
		command.Radii = scaleCornerRadii(command.Radii, scale)
		command.BorderWidths = scaleEdges(command.BorderWidths, scale)
		command.ShadowOffsetX *= scale
		command.ShadowOffsetY *= scale
		command.ShadowBlur *= scale
		command.ShadowSpread *= scale
		command.Transform.E *= scale
		command.Transform.F *= scale
		command.X *= scale
		command.BaselineY *= scale
		command.FontSize *= scale
		command.Clip = scaleRect(command.Clip, scale)
		for clipIndex := 0; clipIndex < int(command.RoundedClipCount); clipIndex++ {
			command.RoundedClips[clipIndex].Rect = scaleRect(command.RoundedClips[clipIndex].Rect, scale)
			command.RoundedClips[clipIndex].Radii = scaleCornerRadii(command.RoundedClips[clipIndex].Radii, scale)
		}
		command.textBounds = scaleRect(command.textBounds, scale)
		command.textWidth *= scale
		command.textHeight *= scale
		command.textBaseline *= scale
		result.Commands[index] = command
	}
	return result
}
