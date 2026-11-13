package window

import (
	"fmt"
	"image"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/render"
)

type pageRasterCache struct {
	frame  *render.Frame
	canvas *image.RGBA
}

// pageRasterResult keeps mutable raster ownership inside the session
// compositor while exposing only value metadata to presentation telemetry.
// The canvas may alias the previous result after an incremental update;
// Backend.Present synchronously copies it before pageCanvas can run again.
type pageRasterResult struct {
	canvas                    *image.RGBA
	rasterized                bool
	incremental               bool
	damagePixels              int
	damageRects               int
	plannedDamagePixels       int
	plannedDamageRects        int
	plannedScrollClipArea     int
	plannedRawDamageRects     int
	plannedExactDamagePixels  int
	plannedExactDamageRects   int
	plannedDamageSource       string
	plannedSourceDamagePixels int
	plannedSourceDamageRects  int
	plannedScrollDeltaX       int
	plannedScrollDeltaY       int
	plannedScrollCommands     int
	plannedMovingCommands     int
	retainedUnderlay          bool
	underlayCacheHit          bool
	underlayCacheMiss         string
	overlaySuffixCommands     int
	overlayRegions            int
	underlayCopyPixels        int
	underlayBuild             time.Duration
	scrollCopy                time.Duration
	damageReplay              time.Duration
	underlayCopy              time.Duration
	overlayReplay             time.Duration
	fallback                  string
}

// sessionCompositor keeps the active tab's rasterized immutable Frame. Chrome
// hover, focus, progress, and inspector revisions can recompose over its page
// pixels without replaying the display list, while inactive tabs retain only
// their much smaller immutable frame/display-list state. Its session rasterizer
// may additionally retain one scroll underlay plus bounded paint caches.
type sessionCompositor struct {
	rasterize            func(*render.Frame, float64) (*image.RGBA, error)
	rasterizer           *render.FrameRasterizer
	pages                map[*browser.Page]pageRasterCache
	rasterizations       uint64
	fullRasters          uint64
	incrementalRasters   uint64
	incrementalFallbacks uint64
	damagePixels         uint64
	deviceScale          float64
	closed               bool
}

func newSessionCompositor(rasterize func(*render.Frame) (*image.RGBA, error)) *sessionCompositor {
	if rasterize == nil {
		frameRasterizer := render.NewFrameRasterizer()
		return &sessionCompositor{
			rasterize:  frameRasterizer.RasterizeAtScale,
			rasterizer: frameRasterizer,
			pages:      make(map[*browser.Page]pageRasterCache), deviceScale: 1,
		}
	}
	return &sessionCompositor{
		rasterize: func(frame *render.Frame, _ float64) (*image.RGBA, error) { return rasterize(frame) },
		pages:     make(map[*browser.Page]pageRasterCache), deviceScale: 1,
	}
}

func (compositor *sessionCompositor) close() error {
	if compositor == nil || compositor.closed {
		return nil
	}
	compositor.closed = true
	clear(compositor.pages)
	if compositor.rasterizer == nil {
		return nil
	}
	err := compositor.rasterizer.Close()
	compositor.rasterizer = nil
	return err
}

func (compositor *sessionCompositor) setDeviceScale(scale float64) {
	if compositor == nil || scale <= 0 || compositor.deviceScale == scale {
		return
	}
	compositor.deviceScale = scale
	clear(compositor.pages)
}

func (compositor *sessionCompositor) pageCanvas(page *browser.Page, frame *render.Frame) (pageRasterResult, error) {
	if compositor == nil || page == nil || frame == nil {
		return pageRasterResult{}, fmt.Errorf("window: nil compositor page or frame")
	}
	if compositor.closed {
		return pageRasterResult{}, fmt.Errorf("window: compositor is closed")
	}
	if cached, ok := compositor.pages[page]; ok && cached.frame == frame && cached.canvas != nil {
		return pageRasterResult{canvas: cached.canvas}, nil
	}
	for cachedPage := range compositor.pages {
		if cachedPage != page {
			delete(compositor.pages, cachedPage)
		}
	}
	if cached, ok := compositor.pages[page]; ok && cached.frame != nil && cached.canvas != nil && compositor.rasterizer != nil {
		update, err := compositor.rasterizer.UpdateAtScale(cached.canvas, cached.frame, frame, compositor.deviceScale)
		if err != nil {
			return pageRasterResult{}, err
		}
		if update.Canvas == nil {
			return pageRasterResult{}, fmt.Errorf("window: renderer returned a nil updated canvas")
		}
		if update.DamagePixels < 0 {
			return pageRasterResult{}, fmt.Errorf("window: renderer returned negative damage pixels: %d", update.DamagePixels)
		}
		if update.DamageRects < 0 {
			return pageRasterResult{}, fmt.Errorf("window: renderer returned negative damage rectangles: %d", update.DamageRects)
		}
		if update.PlannedDamagePixels < 0 || update.PlannedDamageRects < 0 || update.PlannedScrollClipArea < 0 ||
			update.PlannedRawDamageRects < 0 || update.PlannedExactDamagePixels < 0 || update.PlannedExactDamageRects < 0 ||
			update.PlannedSourceDamagePixels < 0 || update.PlannedSourceDamageRects < 0 ||
			update.PlannedScrollCommands < 0 || update.PlannedMovingCommands < 0 ||
			update.OverlaySuffixCommands < 0 || update.OverlayRegions < 0 || update.UnderlayCopyPixels < 0 {
			return pageRasterResult{}, fmt.Errorf(
				"window: renderer returned negative planned damage: pixels=%d rectangles=%d clip=%d raw_rectangles=%d exact_pixels=%d exact_rectangles=%d source_pixels=%d source_rectangles=%d",
				update.PlannedDamagePixels, update.PlannedDamageRects, update.PlannedScrollClipArea,
				update.PlannedRawDamageRects, update.PlannedExactDamagePixels, update.PlannedExactDamageRects,
				update.PlannedSourceDamagePixels, update.PlannedSourceDamageRects,
			)
		}
		compositor.pages[page] = pageRasterCache{frame: frame, canvas: update.Canvas}
		compositor.rasterizations++
		if update.Incremental {
			compositor.incrementalRasters++
		} else {
			compositor.fullRasters++
			compositor.incrementalFallbacks++
		}
		if update.DamagePixels > 0 {
			compositor.damagePixels += uint64(update.DamagePixels)
		}
		return pageRasterResult{
			canvas: update.Canvas, rasterized: true,
			incremental: update.Incremental, damagePixels: update.DamagePixels, damageRects: update.DamageRects,
			plannedDamagePixels: update.PlannedDamagePixels, plannedDamageRects: update.PlannedDamageRects,
			plannedScrollClipArea:     update.PlannedScrollClipArea,
			plannedRawDamageRects:     update.PlannedRawDamageRects,
			plannedExactDamagePixels:  update.PlannedExactDamagePixels,
			plannedExactDamageRects:   update.PlannedExactDamageRects,
			plannedDamageSource:       update.PlannedDamageSource,
			plannedSourceDamagePixels: update.PlannedSourceDamagePixels,
			plannedSourceDamageRects:  update.PlannedSourceDamageRects,
			plannedScrollDeltaX:       update.PlannedScrollDeltaX,
			plannedScrollDeltaY:       update.PlannedScrollDeltaY,
			plannedScrollCommands:     update.PlannedScrollCommands,
			plannedMovingCommands:     update.PlannedMovingCommands,
			retainedUnderlay:          update.RetainedUnderlay,
			underlayCacheHit:          update.UnderlayCacheHit,
			underlayCacheMiss:         update.UnderlayCacheMiss,
			overlaySuffixCommands:     update.OverlaySuffixCommands,
			overlayRegions:            update.OverlayRegions,
			underlayCopyPixels:        update.UnderlayCopyPixels,
			underlayBuild:             update.UnderlayBuild,
			scrollCopy:                update.ScrollCopy,
			damageReplay:              update.DamageReplay,
			underlayCopy:              update.UnderlayCopy,
			overlayReplay:             update.OverlayReplay,
			fallback:                  string(update.Fallback),
		}, nil
	}
	canvas, err := compositor.rasterize(frame, compositor.deviceScale)
	if err != nil {
		return pageRasterResult{}, err
	}
	if canvas == nil {
		return pageRasterResult{}, fmt.Errorf("window: renderer returned a nil canvas")
	}
	compositor.pages[page] = pageRasterCache{frame: frame, canvas: canvas}
	compositor.rasterizations++
	compositor.fullRasters++
	damagePixels := canvas.Bounds().Dx() * canvas.Bounds().Dy()
	if damagePixels > 0 {
		compositor.damagePixels += uint64(damagePixels)
	}
	return pageRasterResult{canvas: canvas, rasterized: true, damagePixels: damagePixels, damageRects: 1}, nil
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
