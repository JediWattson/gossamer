package window_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/browser/fake"
	"github.com/JediWattson/gossamer/internal/loader"
	"github.com/JediWattson/gossamer/internal/window"
)

func TestRunBrowserReportsValueOnlyFrameTimings(t *testing.T) {
	t.Parallel()
	engine := fake.New()
	runtime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	page, err := runtime.LoadPage(context.Background(), "https://frame-timing.gossamer.test/", windowDocumentLoader{response: &loader.Response{
		URL: mustWindowURL(t, "https://frame-timing.gossamer.test/"), StatusCode: 200,
		Body: io.NopCloser(strings.NewReader(`<!doctype html><html><body style="margin:0"><div style="height:1600px">content</div></body></html>`)),
	}})
	if err != nil {
		t.Fatal(err)
	}
	backend := window.NewMemoryBackend(
		window.Event{Kind: window.EventScroll, DeltaY: 120},
		window.Event{Kind: window.EventClose},
	)
	backend.SetDeviceScaleFactor(2)
	var timings []window.FrameTiming
	err = window.RunBrowser(context.Background(), page, backend, window.ShellConfig{
		Title: "Frame timing test",
		FrameTiming: func(observation window.FrameTiming) error {
			timings = append(timings, observation)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(timings) < 2 {
		t.Fatalf("frame timings = %d, want initial and scrolled presentations", len(timings))
	}
	for index, timing := range timings {
		if timing.Sequence != uint64(index+1) {
			t.Fatalf("timing %d sequence = %d", index, timing.Sequence)
		}
		if timing.PresentedAt.IsZero() || timing.Tasks < 0 || timing.Raster < 0 || timing.Composite < 0 || timing.Present < 0 || timing.Total < 0 {
			t.Fatalf("invalid timing %d: %#v", index, timing)
		}
		if index == 0 && timing.SincePrevious != 0 {
			t.Fatalf("initial since-previous = %s, want zero", timing.SincePrevious)
		}
		if index > 0 && timing.SincePrevious <= 0 {
			t.Fatalf("timing %d since-previous = %s, want positive", index, timing.SincePrevious)
		}
	}
	if !timings[0].PageChanged || !timings[0].ShellChanged {
		t.Fatalf("initial flags = page %t shell %t", timings[0].PageChanged, timings[0].ShellChanged)
	}
	if timings[0].ScrollFrame || !timings[1].ScrollFrame {
		t.Fatalf("scroll flags = initial %t scrolled %t, want false then true", timings[0].ScrollFrame, timings[1].ScrollFrame)
	}
	if timings[0].IncrementalRaster || timings[0].DamagePixels == 0 || timings[0].DamageRects != 1 {
		t.Fatalf("initial raster telemetry = incremental %t damage %d rects %d", timings[0].IncrementalRaster, timings[0].DamagePixels, timings[0].DamageRects)
	}
	if timings[0].RasterFallback != "" {
		t.Fatalf("initial raster fallback = %q, want empty cold-raster reason", timings[0].RasterFallback)
	}
	fullPagePixels := uint64(page.Frame().Viewport.Width * 2 * page.Frame().Viewport.Height * 2)
	if !timings[1].IncrementalRaster || timings[1].DamagePixels == 0 || timings[1].DamagePixels >= fullPagePixels || timings[1].DamageRects == 0 {
		t.Fatalf("scroll raster telemetry = incremental %t damage %d rects %d, full page %d", timings[1].IncrementalRaster, timings[1].DamagePixels, timings[1].DamageRects, fullPagePixels)
	}
	if timings[1].RasterFallback != "" {
		t.Fatalf("incremental scroll fallback = %q, want empty", timings[1].RasterFallback)
	}
}
