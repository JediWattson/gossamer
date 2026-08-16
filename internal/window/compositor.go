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

// sessionCompositor keeps one rasterized immutable Frame per live tab. Chrome
// hover, focus, progress, and inspector revisions can then recompose over the
// copied page pixels without replaying the page display list.
type sessionCompositor struct {
	rasterize      func(*render.Frame) (*image.RGBA, error)
	pages          map[*browser.Page]pageRasterCache
	rasterizations uint64
}

func newSessionCompositor(rasterize func(*render.Frame) (*image.RGBA, error)) *sessionCompositor {
	if rasterize == nil {
		rasterize = render.Rasterize
	}
	return &sessionCompositor{
		rasterize: rasterize,
		pages:     make(map[*browser.Page]pageRasterCache),
	}
}

func (compositor *sessionCompositor) pageCanvas(page *browser.Page, frame *render.Frame) (*image.RGBA, error) {
	if compositor == nil || page == nil || frame == nil {
		return nil, fmt.Errorf("window: nil compositor page or frame")
	}
	if cached, ok := compositor.pages[page]; ok && cached.frame == frame && cached.canvas != nil {
		return cached.canvas, nil
	}
	canvas, err := compositor.rasterize(frame)
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
