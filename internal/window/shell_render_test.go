package window

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/draw"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/browser/fake"
	xdraw "golang.org/x/image/draw"
)

func TestGraphiteRetinaChromeCacheSurvivesPageOnlyFrames(t *testing.T) {
	t.Parallel()

	page, closePage := newShellTestPage(t, shellTestLoader{document: `<html><body style="margin:0"><div style="width:1600px;height:1800px">scrollable</div></body></html>`})
	defer closePage()
	shell, err := newGraphiteShell(page, ShellConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer shell.close()
	shell.initialWindowSize(page.Frame().Viewport)
	shell.now = func() time.Time { return time.Unix(100, 0) }

	compose := func(scale float64, fill color.NRGBA) *image.RGBA {
		t.Helper()
		content := scaleImageRectangle(shell.layout().content, scale)
		pageCanvas := image.NewRGBA(image.Rect(0, 0, content.Dx(), content.Dy()))
		draw.Draw(pageCanvas, pageCanvas.Bounds(), image.NewUniform(fill), image.Point{}, draw.Src)
		canvas, composeErr := shell.composeAtScale(pageCanvas, page, scale)
		if composeErr != nil {
			t.Fatal(composeErr)
		}
		return canvas
	}
	assertScales := func(want uint64) {
		t.Helper()
		if got := shell.chromeCache.scales; got != want {
			t.Fatalf("static chrome scales = %d, want %d", got, want)
		}
	}

	firstColor := color.NRGBA{R: 0x31, G: 0x72, B: 0xa4, A: 0xff}
	secondColor := color.NRGBA{R: 0xb4, G: 0x4a, B: 0x67, A: 0xff}
	compose(2, firstColor)
	assertScales(1)
	second := compose(2, secondColor)
	assertScales(1)
	contentSample := scaleImageRectangle(image.Rect(8, graphiteChromeHeight+8, 9, graphiteChromeHeight+9), 2).Min
	if got := color.NRGBAModel.Convert(second.At(contentSample.X, contentSample.Y)).(color.NRGBA); got != secondColor {
		t.Fatalf("page-only frame content = %#v, want %#v", got, secondColor)
	}

	// Scrollbar activity is a live overlay revision; it must not force the
	// static tabs, toolbar, and rail through Catmull-Rom again.
	shell.markScrollbarActivity(shell.activeInputState(), browserRootHandle())
	compose(2, secondColor)
	assertScales(1)

	shell.address = "https://changed.gossamer.test/"
	compose(2, secondColor)
	assertScales(2)
	shell.hoveredTab = 0
	compose(2, secondColor)
	assertScales(3)
	shell.tabs = append(shell.tabs, shell.tabs[0])
	compose(2, secondColor)
	assertScales(4)
	shell.loading = true
	compose(2, secondColor)
	assertScales(5)
	shell.width += 20
	compose(2, secondColor)
	assertScales(6)
	compose(1.5, secondColor)
	assertScales(7)
}

func TestGraphiteRetinaCachedChromeMatchesFullScalePixels(t *testing.T) {
	t.Parallel()

	page, closePage := newShellTestPage(t, shellTestLoader{document: `<html><head><title>Scale fidelity</title></head><body>page</body></html>`})
	defer closePage()
	shell, err := newGraphiteShell(page, ShellConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer shell.close()
	shell.initialWindowSize(page.Frame().Viewport)

	const scale = 2.0
	layout := shell.layout()
	content := scaleImageRectangle(layout.content, scale)
	pageCanvas := image.NewRGBA(image.Rect(0, 0, content.Dx(), content.Dy()))
	draw.Draw(pageCanvas, pageCanvas.Bounds(), image.NewUniform(color.NRGBA{R: 0x65, G: 0x27, B: 0x91, A: 0xff}), image.Point{}, draw.Src)

	// This is the former full-window static scaling result. With no active
	// overlays, the retained strips must be pixel-identical to it.
	cssChrome, err := shell.compose(nil, page)
	if err != nil {
		t.Fatal(err)
	}
	wantBounds := scaleImageRectangle(layout.window, scale)
	want := image.NewRGBA(wantBounds)
	xdraw.CatmullRom.Scale(want, wantBounds, cssChrome, cssChrome.Bounds(), draw.Src, nil)
	draw.Draw(want, content, pageCanvas, pageCanvas.Bounds().Min, draw.Src)

	got, err := shell.composeAtScale(pageCanvas, page, scale)
	if err != nil {
		t.Fatal(err)
	}
	if got.Bounds() != want.Bounds() || !bytes.Equal(got.Pix, want.Pix) {
		t.Fatal("retained Retina chrome differs from the full-scale reference")
	}
	if shell.chromeCache.top == nil || shell.chromeCache.rail == nil {
		t.Fatal("Retina compose did not retain both static chrome strips")
	}
	retainedBytes := len(shell.chromeCache.top.Pix) + len(shell.chromeCache.rail.Pix)
	if retainedBytes >= len(got.Pix)/2 {
		t.Fatalf("retained chrome bytes = %d, want less than half of full canvas %d", retainedBytes, len(got.Pix))
	}
}

func TestGraphiteRetinaInspectorOverlaysDevicePagePixels(t *testing.T) {
	t.Parallel()

	page, closePage := newShellTestPage(t, shellTestLoader{document: `<html><body><main>inspect me</main></body></html>`})
	defer closePage()
	shell, err := newGraphiteShell(page, ShellConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer shell.close()
	shell.initialWindowSize(page.Frame().Viewport)
	shell.inspectorOpen = true

	const scale = 2.0
	layout := shell.layout()
	content := scaleImageRectangle(layout.content, scale)
	pageColor := color.NRGBA{R: 0xe8, G: 0x31, B: 0x52, A: 0xff}
	pageCanvas := image.NewRGBA(image.Rect(0, 0, content.Dx(), content.Dy()))
	draw.Draw(pageCanvas, pageCanvas.Bounds(), image.NewUniform(pageColor), image.Point{}, draw.Src)
	canvas, err := shell.composeAtScale(pageCanvas, page, scale)
	if err != nil {
		t.Fatal(err)
	}
	panelSample := scaleImageRectangle(image.Rect(layout.inspector.Min.X+4, layout.inspector.Max.Y-20, layout.inspector.Min.X+5, layout.inspector.Max.Y-19), scale).Min
	if got := color.NRGBAModel.Convert(canvas.At(panelSample.X, panelSample.Y)).(color.NRGBA); got == pageColor {
		t.Fatal("device-pixel page overwrote the open inspector overlay")
	}
}

// browserRootHandle uses the zero Node identity expected by root scrollbar
// activity while keeping that representation out of the cache test itself.
func browserRootHandle() browser.NodeHandle {
	return browser.NodeHandle{}
}

func BenchmarkGraphiteRetinaChrome(b *testing.B) {
	engine := fake.New()
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if closeErr := browserRuntime.Close(); closeErr != nil {
			b.Errorf("close browser runtime: %v", closeErr)
		}
	})
	page, err := browserRuntime.LoadPage(context.Background(), "https://retina-benchmark.gossamer.test/", shellTestLoader{
		document: `<html><head><title>Retina benchmark</title></head><body>page</body></html>`,
	})
	if err != nil {
		b.Fatal(err)
	}
	shell, err := newGraphiteShell(page, ShellConfig{})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if closeErr := shell.close(); closeErr != nil {
			b.Errorf("close Graphite shell: %v", closeErr)
		}
	})
	shell.initialWindowSize(page.Frame().Viewport)
	const scale = 2.0
	content := scaleImageRectangle(shell.layout().content, scale)
	pageCanvas := image.NewRGBA(image.Rect(0, 0, content.Dx(), content.Dy()))
	draw.Draw(pageCanvas, pageCanvas.Bounds(), image.NewUniform(color.NRGBA{R: 0x31, G: 0x72, B: 0xa4, A: 0xff}), image.Point{}, draw.Src)
	if _, err := shell.composeAtScale(pageCanvas, page, scale); err != nil {
		b.Fatal(err)
	}

	b.Run("retained-page-only", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			if _, composeErr := shell.composeAtScale(pageCanvas, page, scale); composeErr != nil {
				b.Fatal(composeErr)
			}
		}
	})
	b.Run("forced-full-static-rescale", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			shell.chromeCache.keyValid = false
			if _, composeErr := shell.composeAtScale(pageCanvas, page, scale); composeErr != nil {
				b.Fatal(composeErr)
			}
		}
	})
}
