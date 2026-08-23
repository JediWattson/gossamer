package window

import (
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

	firstCanvas, err := compositor.pageCanvas(firstPage, firstFrame)
	if err != nil {
		t.Fatal(err)
	}
	cachedCanvas, err := compositor.pageCanvas(firstPage, firstFrame)
	if err != nil {
		t.Fatal(err)
	}
	if firstCanvas != cachedCanvas || calls != 1 || compositor.rasterizations != 1 {
		t.Fatalf("same-frame cache = canvas %p/%p calls %d rasterizations %d", firstCanvas, cachedCanvas, calls, compositor.rasterizations)
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
	second, err := compositor.pageCanvas(page, frame)
	if err != nil {
		t.Fatal(err)
	}
	if first.Bounds() != image.Rect(0, 0, 10, 8) || second.Bounds() != image.Rect(0, 0, 20, 16) {
		t.Fatalf("raster bounds = %v then %v", first.Bounds(), second.Bounds())
	}
	if compositor.rasterizations != 2 {
		t.Fatalf("rasterizations = %d, want 2", compositor.rasterizations)
	}
}
