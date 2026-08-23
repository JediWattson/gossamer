package window

import (
	"fmt"
	"image"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/render"
)

type pageRasterCache struct {
	frame  *render.Frame
	canvas *image.RGBA
}

// sessionCompositor keeps the active tab's rasterized immutable Frame. Chrome
// hover, focus, progress, and inspector revisions can recompose over its page
// pixels without replaying the display list, while inactive tabs retain only
// their much smaller immutable frame/display-list state.
type sessionCompositor struct {
	rasterize      func(*render.Frame, float64) (*image.RGBA, error)
	pages          map[*browser.Page]pageRasterCache
	rasterizations uint64
	deviceScale    float64
}

func newSessionCompositor(rasterize func(*render.Frame) (*image.RGBA, error)) *sessionCompositor {
	if rasterize == nil {
		return &sessionCompositor{
			rasterize: render.RasterizeAtScale,
			pages:     make(map[*browser.Page]pageRasterCache), deviceScale: 1,
		}
	}
	return &sessionCompositor{
		rasterize: func(frame *render.Frame, _ float64) (*image.RGBA, error) { return rasterize(frame) },
		pages:     make(map[*browser.Page]pageRasterCache), deviceScale: 1,
	}
}

func (compositor *sessionCompositor) setDeviceScale(scale float64) {
	if compositor == nil || scale <= 0 || compositor.deviceScale == scale {
		return
	}
	compositor.deviceScale = scale
	clear(compositor.pages)
}

func (compositor *sessionCompositor) pageCanvas(page *browser.Page, frame *render.Frame) (*image.RGBA, error) {
	if compositor == nil || page == nil || frame == nil {
		return nil, fmt.Errorf("window: nil compositor page or frame")
	}
	if cached, ok := compositor.pages[page]; ok && cached.frame == frame && cached.canvas != nil {
		return cached.canvas, nil
	}
	for cachedPage := range compositor.pages {
		if cachedPage != page {
			delete(compositor.pages, cachedPage)
		}
	}
	canvas, err := compositor.rasterize(frame, compositor.deviceScale)
	if err != nil {
		return nil, err
	}
	compositor.pages[page] = pageRasterCache{frame: frame, canvas: canvas}
	compositor.rasterizations++
	return canvas, nil
}

func (compositor *sessionCompositor) prune(livePages []*browser.Page) {
	if compositor == nil || len(compositor.pages) == 0 {
		return
	}
	live := make(map[*browser.Page]struct{}, len(livePages))
	for _, page := range livePages {
		if page != nil {
			live[page] = struct{}{}
		}
	}
	for page := range compositor.pages {
		if _, ok := live[page]; !ok {
			delete(compositor.pages, page)
		}
	}
}
