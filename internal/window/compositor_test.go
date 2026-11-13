package window

import (
	"bytes"
	"context"
	"image"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestSessionCompositorCachesOnlyActivePageFrame(t *testing.T) {
	t.Parallel()

	var calls int
	compositor := newSessionCompositor(func(frame *render.Frame) (*image.RGBA, error) {
		calls++
		return image.NewRGBA(image.Rect(0, 0, frame.Viewport.Width, frame.Viewport.Height)), nil
	})
	firstPage := &browser.Page{}
	secondPage := &browser.Page{}
	firstFrame := &render.Frame{Viewport: render.Viewport{Width: 10, Height: 8}}
	firstFrame.DisplayList.Viewport = firstFrame.Viewport
	secondFrame := &render.Frame{Viewport: render.Viewport{Width: 12, Height: 9}}
	secondFrame.DisplayList.Viewport = secondFrame.Viewport

	firstResult, err := compositor.pageCanvas(firstPage, firstFrame)
	if err != nil {
		t.Fatal(err)
	}
	cachedResult, err := compositor.pageCanvas(firstPage, firstFrame)
	if err != nil {
		t.Fatal(err)
	}
	if firstResult.canvas != cachedResult.canvas || cachedResult.rasterized || calls != 1 || compositor.rasterizations != 1 {
		t.Fatalf("same-frame cache = canvas %p/%p rasterized=%t calls %d rasterizations %d", firstResult.canvas, cachedResult.canvas, cachedResult.rasterized, calls, compositor.rasterizations)
	}
	if _, err := compositor.pageCanvas(firstPage, secondFrame); err != nil {
		t.Fatal(err)
	}
	if _, err := compositor.pageCanvas(secondPage, firstFrame); err != nil {
		t.Fatal(err)
	}
	if calls != 3 || len(compositor.pages) != 1 {
		t.Fatalf("changed frame/tab cache = calls %d pages %d, want 3/1", calls, len(compositor.pages))
	}
	compositor.prune([]*browser.Page{secondPage})
	if len(compositor.pages) != 1 {
		t.Fatalf("pruned cache pages = %d, want 1", len(compositor.pages))
	}
	compositor.prune(nil)
	if len(compositor.pages) != 0 {
		t.Fatalf("fully pruned cache pages = %d, want 0", len(compositor.pages))
	}
}

func TestSessionCompositorInvalidatesRasterWhenDeviceScaleChanges(t *testing.T) {
	t.Parallel()
	compositor := newSessionCompositor(nil)
	page := &browser.Page{}
	frame := &render.Frame{Viewport: render.Viewport{Width: 10, Height: 8}}
	frame.DisplayList.Viewport = frame.Viewport
	first, err := compositor.pageCanvas(page, frame)
	if err != nil {
		t.Fatal(err)
	}
	compositor.setDeviceScale(2)
	if len(compositor.pages) != 0 {
		t.Fatalf("scale change retained %d stale page rasters", len(compositor.pages))
	}
	second, err := compositor.pageCanvas(page, frame)
	if err != nil {
		t.Fatal(err)
	}
	if first.canvas.Bounds() != image.Rect(0, 0, 10, 8) || second.canvas.Bounds() != image.Rect(0, 0, 20, 16) {
		t.Fatalf("raster bounds = %v then %v", first.canvas.Bounds(), second.canvas.Bounds())
	}
	if compositor.rasterizations != 2 {
		t.Fatalf("rasterizations = %d, want 2", compositor.rasterizations)
	}
}

func TestSessionCompositorIncrementalScrollMatchesFullRaster(t *testing.T) {
	t.Parallel()

	page, closePage := newShellTestPage(t, shellTestLoader{document: `<html><body style="margin:0;background:#182432"><div style="height:1800px;color:white">retained scroll pixels</div></body></html>`})
	defer closePage()
	compositor := newSessionCompositor(nil)
	defer compositor.close()
	compositor.setDeviceScale(2)

	initialFrame := page.Frame()
	initial, err := compositor.pageCanvas(page, initialFrame)
	if err != nil {
		t.Fatal(err)
	}
	if initial.incremental || !initial.rasterized || initial.damagePixels != initial.canvas.Bounds().Dx()*initial.canvas.Bounds().Dy() {
		t.Fatalf("initial raster metadata = %#v, bounds=%v", initial, initial.canvas.Bounds())
	}
	if _, err := page.ScrollViewport(0, 24); err != nil {
		t.Fatal(err)
	}
	if _, err := page.QueueRender(); err != nil {
		t.Fatal(err)
	}
	if _, err := pumpPageTasks(context.Background(), page); err != nil {
		t.Fatal(err)
	}
	scrolledFrame := page.Frame()
	if scrolledFrame == initialFrame {
		t.Fatal("scroll render did not publish a projected frame")
	}
	updated, err := compositor.pageCanvas(page, scrolledFrame)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.incremental || !updated.rasterized {
		t.Fatalf("scroll raster metadata = %#v, want incremental update", updated)
	}
	if updated.canvas != initial.canvas {
		t.Fatal("incremental scroll replaced its retained framebuffer")
	}
	fullPixels := updated.canvas.Bounds().Dx() * updated.canvas.Bounds().Dy()
	if updated.damagePixels <= 0 || updated.damagePixels >= fullPixels {
		t.Fatalf("incremental damage pixels = %d, full frame = %d", updated.damagePixels, fullPixels)
	}
	reference, err := render.RasterizeAtScale(scrolledFrame, 2)
	if err != nil {
		t.Fatal(err)
	}
	if updated.canvas.Bounds() != reference.Bounds() || updated.canvas.Stride != reference.Stride || !bytes.Equal(updated.canvas.Pix, reference.Pix) {
		t.Fatal("incremental scroll pixels differ from a full Retina raster")
	}
	if compositor.rasterizations != 2 || compositor.fullRasters != 1 || compositor.incrementalRasters != 1 || compositor.incrementalFallbacks != 0 {
		t.Fatalf("compositor counters = raster %d full %d incremental %d fallback %d", compositor.rasterizations, compositor.fullRasters, compositor.incrementalRasters, compositor.incrementalFallbacks)
	}
}

func TestSessionCompositorFallsBackWhenPaintArtifactChanges(t *testing.T) {
	t.Parallel()

	page, closePage := newShellTestPage(t, shellTestLoader{document: `<html><body style="margin:0"><div id="content" style="height:1800px;background:#335577">before</div></body></html>`})
	defer closePage()
	compositor := newSessionCompositor(nil)
	defer compositor.close()
	compositor.setDeviceScale(2)

	initial, err := compositor.pageCanvas(page, page.Frame())
	if err != nil {
		t.Fatal(err)
	}
	content, found := page.Document().ElementByID("content")
	if !found {
		t.Fatal("content element not indexed")
	}
	if err := page.Document().SetAttribute(content, "style", "height:1800px;background:#773355"); err != nil {
		t.Fatal(err)
	}
	if _, err := page.QueueRender(); err != nil {
		t.Fatal(err)
	}
	if _, err := pumpPageTasks(context.Background(), page); err != nil {
		t.Fatal(err)
	}
	updated, err := compositor.pageCanvas(page, page.Frame())
	if err != nil {
		t.Fatal(err)
	}
	if updated.incremental || !updated.rasterized {
		t.Fatalf("changed-paint raster metadata = %#v, want full fallback", updated)
	}
	if updated.fallback != "artifact" {
		t.Fatalf("changed-paint fallback = %q, want artifact", updated.fallback)
	}
	if updated.canvas == initial.canvas {
		t.Fatal("full fallback mutated the stale framebuffer")
	}
	fullPixels := updated.canvas.Bounds().Dx() * updated.canvas.Bounds().Dy()
	if updated.damagePixels != fullPixels {
		t.Fatalf("full fallback damage = %d, want %d", updated.damagePixels, fullPixels)
	}
	reference, err := render.RasterizeAtScale(page.Frame(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(updated.canvas.Pix, reference.Pix) {
		t.Fatal("full fallback pixels differ from a fresh Retina raster")
	}
	if compositor.fullRasters != 2 || compositor.incrementalRasters != 0 || compositor.incrementalFallbacks != 1 {
		t.Fatalf("fallback counters = full %d incremental %d fallback %d", compositor.fullRasters, compositor.incrementalRasters, compositor.incrementalFallbacks)
	}
}

func TestSessionCompositorCloseReleasesRetainedFramebuffer(t *testing.T) {
	t.Parallel()
	compositor := newSessionCompositor(nil)
	page := &browser.Page{}
	frame := &render.Frame{Viewport: render.Viewport{Width: 10, Height: 8}}
	frame.DisplayList.Viewport = frame.Viewport
	if _, err := compositor.pageCanvas(page, frame); err != nil {
		t.Fatal(err)
	}
	if err := compositor.close(); err != nil {
		t.Fatal(err)
	}
	if len(compositor.pages) != 0 || compositor.rasterizer != nil {
		t.Fatalf("closed compositor retained pages=%d rasterizer=%p", len(compositor.pages), compositor.rasterizer)
	}
	if _, err := compositor.pageCanvas(page, frame); err == nil {
		t.Fatal("pageCanvas() after close error = nil")
	}
}
