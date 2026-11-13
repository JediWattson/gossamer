package render

import (
	"image"
	"image/color"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
)

func TestRoundedOverflowClipsDescendantPaintAndHitTesting(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body", dom.Attribute{Name: "style", Value: "margin:0"})
	parent := dom.NewElement("div", dom.Attribute{Name: "style", Value: "width:40px;height:40px;border-radius:15px;overflow:hidden;background:red"})
	child := dom.NewElement("div", dom.Attribute{Name: "style", Value: "width:40px;height:40px;background:blue"})
	parent.AppendChild(child)
	body.AppendChild(parent)
	html.AppendChild(body)
	document.AppendChild(html)

	frame, err := Render(document, Viewport{Width: 50, Height: 50})
	if err != nil {
		t.Fatal(err)
	}
	canvas, err := Rasterize(frame)
	if err != nil {
		t.Fatal(err)
	}
	assertEffectPixel(t, canvas, 0, 0, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff})
	assertEffectPixel(t, canvas, 20, 20, color.NRGBA{B: 0xff, A: 0xff})
	if hit := HitTest(frame, 1, 1); hit == child {
		t.Fatal("rounded overflow corner hit clipped descendant")
	}
	if hit := HitTest(frame, 20, 20); hit != child {
		t.Fatalf("rounded overflow center hit = %p, want child", hit)
	}
}

func TestSolidRoundedBorderPaintsAnAnnularStroke(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body", dom.Attribute{Name: "style", Value: "margin:0"})
	target := dom.NewElement("div", dom.Attribute{Name: "style", Value: "width:20px;height:20px;border:3px solid red;border-radius:10px;background:blue"})
	body.AppendChild(target)
	html.AppendChild(body)
	document.AppendChild(html)

	frame, err := Render(document, Viewport{Width: 30, Height: 30})
	if err != nil {
		t.Fatal(err)
	}
	canvas, err := Rasterize(frame)
	if err != nil {
		t.Fatal(err)
	}
	assertEffectPixel(t, canvas, 0, 0, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff})
	assertEffectPixel(t, canvas, 10, 1, color.NRGBA{R: 0xff, A: 0xff})
	assertEffectPixel(t, canvas, 10, 5, color.NRGBA{B: 0xff, A: 0xff})
}

func TestRoundedBorderShadowGradientTransformAndOpacityPaintTogether(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body", dom.Attribute{Name: "style", Value: "margin:0"})
	target := dom.NewElement("div", dom.Attribute{Name: "style", Value: `
		width:20px;height:20px;margin-left:10px;margin-top:10px;
		border:3px solid #ff0000;border-radius:8px;
		background-image:linear-gradient(90deg,#00ff00,#0000ff);
		box-shadow:0 0 0 4px #ff00ff;
		transform-origin:left top;translate:20px 0;scale:1.5;
		opacity:.5
	`})
	body.AppendChild(target)
	html.AppendChild(body)
	document.AppendChild(html)

	frame, err := Render(document, Viewport{Width: 90, Height: 55})
	if err != nil {
		t.Fatal(err)
	}
	var hasBorder, hasShadow, hasGradient, hasTransform bool
	for _, command := range frame.DisplayList.Commands {
		hasBorder = hasBorder || command.Kind == DrawRoundedBorderCommand
		hasShadow = hasShadow || command.Kind == DrawBoxShadowCommand
		hasGradient = hasGradient || command.Kind == DrawGradientCommand
		hasTransform = hasTransform || command.Kind == BeginTransformCommand
	}
	if !hasBorder || !hasShadow || !hasGradient || !hasTransform {
		t.Fatalf("effect commands border/shadow/gradient/transform = %t/%t/%t/%t", hasBorder, hasShadow, hasGradient, hasTransform)
	}
	canvas, err := Rasterize(frame)
	if err != nil {
		t.Fatal(err)
	}
	// The translated/scaled target begins near x=30. Its magenta ring is
	// composited through the element's 50 percent opacity group.
	pixel := color.NRGBAModel.Convert(canvas.At(27, 20)).(color.NRGBA)
	if pixel.R < 240 || pixel.B < 240 || pixel.G > 220 {
		t.Fatalf("shadow/ring pixel = %#v, want translucent magenta over white", pixel)
	}
	center := color.NRGBAModel.Convert(canvas.At(48, 25)).(color.NRGBA)
	if center.G <= 100 && center.B <= 100 {
		t.Fatalf("gradient center pixel = %#v, want green/blue content", center)
	}
	if hit := HitTest(frame, 15, 20); hit == target {
		t.Fatal("untransformed location still hit transformed target")
	}
	if hit := HitTest(frame, 45, 20); hit != target {
		t.Fatalf("transformed hit = %p, want target", hit)
	}
}

func TestURLBackgroundUsesBrowserOwnedDecodedImage(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body", dom.Attribute{Name: "style", Value: "margin:0"})
	target := dom.NewElement("div", dom.Attribute{Name: "style", Value: `width:12px;height:12px;border-radius:4px;background-image:url("asset.png")`})
	body.AppendChild(target)
	html.AppendChild(body)
	document.AppendChild(html)
	decoded := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	decoded.SetNRGBA(0, 0, color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff})

	frame, err := RenderWithResources(document, Viewport{Width: 20, Height: 20}, Resources{
		BackgroundImages: map[*dom.Node]map[string]image.Image{target: {"asset.png": decoded}},
	})
	if err != nil {
		t.Fatal(err)
	}
	canvas, err := Rasterize(frame)
	if err != nil {
		t.Fatal(err)
	}
	assertEffectPixel(t, canvas, 6, 6, color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff})
	assertEffectPixel(t, canvas, 0, 0, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff})
}

func TestRepeatingLinearGradientRepeatsItsResolvedStopRange(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body", dom.Attribute{Name: "style", Value: "margin:0"})
	target := dom.NewElement("div", dom.Attribute{Name: "style", Value: "width:20px;height:4px;background-image:repeating-linear-gradient(90deg,red 0,red 2px,blue 2px,blue 4px)"})
	body.AppendChild(target)
	html.AppendChild(body)
	document.AppendChild(html)

	frame, err := Render(document, Viewport{Width: 24, Height: 8})
	if err != nil {
		t.Fatal(err)
	}
	canvas, err := Rasterize(frame)
	if err != nil {
		t.Fatal(err)
	}
	first := color.NRGBAModel.Convert(canvas.At(0, 1)).(color.NRGBA)
	second := color.NRGBAModel.Convert(canvas.At(3, 1)).(color.NRGBA)
	repeated := color.NRGBAModel.Convert(canvas.At(4, 1)).(color.NRGBA)
	if first.R < 200 || first.B > 20 || second.B < 200 || second.R > 20 || repeated.R < 200 || repeated.B > 20 {
		t.Fatalf("repeating gradient pixels = %#v %#v %#v", first, second, repeated)
	}
}

func assertEffectPixel(t *testing.T, canvas image.Image, x, y int, want color.NRGBA) {
	t.Helper()
	got := color.NRGBAModel.Convert(canvas.At(x, y)).(color.NRGBA)
	if got != want {
		t.Fatalf("pixel (%d,%d) = %#v, want %#v", x, y, got, want)
	}
}

func TestBackgroundSizePositionAndRepeatSliceSpriteSheet(t *testing.T) {
	t.Parallel()

	// 48x48 sprite sheet with 3x3 16x16 tiles: top-left red, center green, bottom-right blue.
	sprite := image.NewNRGBA(image.Rect(0, 0, 48, 48))
	for y := 0; y < 48; y++ {
		for x := 0; x < 48; x++ {
			c := color.NRGBA{R: 0x22, G: 0x22, B: 0x22, A: 0xff}
			switch {
			case x < 16 && y < 16:
				c = color.NRGBA{R: 0xff, A: 0xff}
			case x >= 16 && x < 32 && y >= 16 && y < 32:
				c = color.NRGBA{G: 0xff, A: 0xff}
			case x >= 32 && y >= 32:
				c = color.NRGBA{B: 0xff, A: 0xff}
			}
			sprite.SetNRGBA(x, y, c)
		}
	}

	document := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body", dom.Attribute{Name: "style", Value: "margin:0"})
	badge := dom.NewElement("div", dom.Attribute{
		Name:  "style",
		Value: `width:16px;height:16px;background-image:url("badges.webp");background-position:-16px -16px;background-size:48px 48px;background-repeat:no-repeat`,
	})
	body.AppendChild(badge)
	html.AppendChild(body)
	document.AppendChild(html)

	frame, err := RenderWithResources(document, Viewport{Width: 48, Height: 48}, Resources{
		BackgroundImages: map[*dom.Node]map[string]image.Image{badge: {"badges.webp": sprite}},
	})
	if err != nil {
		t.Fatal(err)
	}
	canvas, err := Rasterize(frame)
	if err != nil {
		t.Fatal(err)
	}
	// Badge viewport (0..15, 0..15) shows center green tile; pixels at x=20 stay white.
	assertEffectPixel(t, canvas, 8, 8, color.NRGBA{G: 0xff, A: 0xff})
	assertEffectPixel(t, canvas, 20, 8, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff})
	assertEffectPixel(t, canvas, 8, 20, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff})
}
