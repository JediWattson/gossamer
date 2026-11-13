package window

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"io"
	"math"
	"strings"
	"unicode/utf16"

	"github.com/JediWattson/gossamer/internal/browser"
	computed "github.com/JediWattson/gossamer/internal/style"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

var graphitePalette = struct {
	ink, top, surface, raised, border, pearl, muted, dim color.NRGBA
	teal, tealDim, violet, danger                        color.NRGBA
}{
	ink:     color.NRGBA{R: 0x08, G: 0x0c, B: 0x11, A: 0xff},
	top:     color.NRGBA{R: 0x0c, G: 0x12, B: 0x19, A: 0xff},
	surface: color.NRGBA{R: 0x11, G: 0x19, B: 0x22, A: 0xff},
	raised:  color.NRGBA{R: 0x18, G: 0x22, B: 0x2c, A: 0xff},
	border:  color.NRGBA{R: 0x32, G: 0x40, B: 0x4d, A: 0xff},
	pearl:   color.NRGBA{R: 0xed, G: 0xf2, B: 0xf4, A: 0xff},
	muted:   color.NRGBA{R: 0x9b, G: 0xa7, B: 0xb0, A: 0xff},
	dim:     color.NRGBA{R: 0x58, G: 0x65, B: 0x70, A: 0xff},
	teal:    color.NRGBA{R: 0x46, G: 0xd8, B: 0xd0, A: 0xff},
	tealDim: color.NRGBA{R: 0x1b, G: 0x70, B: 0x70, A: 0xff},
	violet:  color.NRGBA{R: 0xb6, G: 0x8c, B: 0xff, A: 0xff},
	danger:  color.NRGBA{R: 0xff, G: 0x76, B: 0x83, A: 0xff},
}

type shellFaceKey struct {
	size int
	bold bool
}

type shellFontBook struct {
	regular *opentype.Font
	bold    *opentype.Font
	faces   map[shellFaceKey]font.Face
}

type graphiteTabChromeState struct {
	page          *browser.Page
	title         string
	faviconURL    string
	faviconReady  bool
	faviconBounds image.Rectangle
}

// graphiteStaticChromeState is the complete set of shell-owned values that
// affect the stable tab, toolbar, and rail pixels. Page overlays deliberately
// stay out of this state so scrolling and animation frames can reuse chrome.
type graphiteStaticChromeState struct {
	tabs           [maximumGraphiteTabs]graphiteTabChromeState
	tabCount       int
	activeTab      int
	hoveredTab     int
	tabOffset      int
	showNewTab     bool
	address        string
	addressFocused bool
	selectAll      bool
	inspectorOpen  bool
	inspectorPanel inspectorPanel
	chromeFocus    shellFocus
	loading        bool
	canGoBack      bool
	canGoForward   bool
	navigationErr  string
	security       shellSecurityState
}

type graphiteChromeCacheKey struct {
	width, height int
	scaleBits     uint64
	revision      uint64
}

// graphiteChromeCache retains only the device-pixel chrome strips rather than
// a full-window bitmap. scales is intentionally observable to package tests so
// the page-only fast path has a regression gate.
type graphiteChromeCache struct {
	state      graphiteStaticChromeState
	stateValid bool
	revision   uint64
	key        graphiteChromeCacheKey
	keyValid   bool
	top        *image.RGBA
	rail       *image.RGBA
	scales     uint64
}

func newShellFontBook() (*shellFontBook, error) {
	regular, err := opentype.Parse(goregular.TTF)
	if err != nil {
		return nil, fmt.Errorf("window: parse Graphite regular font: %w", err)
	}
	bold, err := opentype.Parse(gobold.TTF)
	if err != nil {
		return nil, fmt.Errorf("window: parse Graphite bold font: %w", err)
	}
	return &shellFontBook{regular: regular, bold: bold, faces: make(map[shellFaceKey]font.Face)}, nil
}

func (book *shellFontBook) close() error {
	if book == nil {
		return nil
	}
	var result error
	for _, face := range book.faces {
		closer, ok := face.(io.Closer)
		if ok {
			if err := closer.Close(); err != nil && result == nil {
				result = err
			}
		}
	}
	book.faces = nil
	return result
}

func (book *shellFontBook) face(size float64, bold bool) (font.Face, error) {
	key := shellFaceKey{size: int(math.Round(size * 64)), bold: bold}
	if existing := book.faces[key]; existing != nil {
		return existing, nil
	}
	parsed := book.regular
	if bold {
		parsed = book.bold
	}
	face, err := opentype.NewFace(parsed, &opentype.FaceOptions{
		Size: size, DPI: 72, Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, fmt.Errorf("window: create Graphite font face: %w", err)
	}
	book.faces[key] = face
	return face, nil
}

func (book *shellFontBook) draw(dst *image.RGBA, clip image.Rectangle, text string, x, baseline, size int, textColor color.NRGBA, bold bool) error {
	face, err := book.face(float64(size), bold)
	if err != nil {
		return err
	}
	if clip.Empty() {
		return nil
	}
	clipped, ok := dst.SubImage(clip.Intersect(dst.Bounds())).(*image.RGBA)
	if !ok {
		return fmt.Errorf("window: Graphite text destination is not RGBA")
	}
	drawer := font.Drawer{
		Dst:  clipped,
		Src:  image.NewUniform(textColor),
		Face: face,
		Dot:  fixed.P(x, baseline),
	}
	drawer.DrawString(text)
	return nil
}

func (shell *graphiteShell) compose(pageCanvas *image.RGBA, page *browser.Page) (*image.RGBA, error) {
	if shell == nil || shell.fonts == nil {
		return nil, fmt.Errorf("window: nil Graphite shell")
	}
	if shell.width <= 0 || shell.height <= 0 {
		return nil, fmt.Errorf("window: invalid Graphite dimensions %dx%d", shell.width, shell.height)
	}
	layout := shell.layout()
	canvas := image.NewRGBA(layout.window)
	fillRect(canvas, layout.window, graphitePalette.ink)
	if pageCanvas != nil {
		draw.Draw(canvas, layout.content, pageCanvas, pageCanvas.Bounds().Min, draw.Src)
	}
	if err := shell.drawScrollbars(canvas, layout, page); err != nil {
		return nil, err
	}
	if err := shell.drawPageStatus(canvas, layout, page); err != nil {
		return nil, err
	}
	if err := shell.drawInteractionOverlay(canvas, layout, page); err != nil {
		return nil, err
	}
	if err := shell.drawStaticChrome(canvas, layout); err != nil {
		return nil, err
	}
	if shell.inspectorOpen {
		if err := shell.drawInspector(canvas, layout, page); err != nil {
			return nil, err
		}
	}
	shell.drawLoadingProgress(canvas, layout, page)
	return canvas, nil
}

func (shell *graphiteShell) drawStaticChrome(canvas *image.RGBA, layout shellLayout) error {
	fillRect(canvas, image.Rect(0, 0, layout.rail.Min.X, graphiteTabHeight), graphitePalette.top)
	fillRect(canvas, image.Rect(0, graphiteTabHeight, layout.rail.Min.X, graphiteChromeHeight), graphitePalette.surface)
	fillRect(canvas, layout.rail, graphitePalette.top)
	fillRect(canvas, image.Rect(0, graphiteTabHeight-1, layout.rail.Min.X, graphiteTabHeight), graphitePalette.border)
	fillRect(canvas, image.Rect(0, graphiteChromeHeight-1, layout.rail.Min.X, graphiteChromeHeight), graphitePalette.border)
	fillRect(canvas, image.Rect(layout.rail.Min.X, 0, layout.rail.Min.X+1, shell.height), graphitePalette.border)

	if err := shell.drawTabs(canvas, layout); err != nil {
		return err
	}
	if err := shell.drawToolbar(canvas, layout); err != nil {
		return err
	}
	shell.drawRailChrome(canvas, layout)
	return nil
}

func (shell *graphiteShell) drawLoadingProgress(canvas *image.RGBA, layout shellLayout, page *browser.Page) {
	if !shell.loading || page == nil {
		return
	}
	fillRect(canvas, shell.loadingProgressBounds(layout, page), graphitePalette.teal)
}

func (shell *graphiteShell) loadingProgressBounds(layout shellLayout, page *browser.Page) image.Rectangle {
	if !shell.loading || page == nil {
		return image.Rectangle{}
	}
	progress := shellNavigationProgress(page.Navigation())
	lineRight := layout.content.Min.X + int(math.Round(float64(layout.content.Dx())*progress))
	lineRight = maxInt(layout.content.Min.X+2, minInt(layout.content.Max.X, lineRight))
	return image.Rect(0, graphiteChromeHeight-2, lineRight, graphiteChromeHeight).Intersect(layout.window)
}

func (shell *graphiteShell) composeAtScale(pageCanvas *image.RGBA, page *browser.Page, scale float64) (*image.RGBA, error) {
	if scale == 1 {
		return shell.compose(pageCanvas, page)
	}
	if !finiteDeviceScale(scale) {
		return nil, fmt.Errorf("window: invalid Graphite device scale %g", scale)
	}
	layout := shell.layout()
	windowBounds := scaleImageRectangle(layout.window, scale)
	canvas := image.NewRGBA(windowBounds)
	fillRect(canvas, windowBounds, graphitePalette.ink)
	content := scaleImageRectangle(layout.content, scale)
	if pageCanvas != nil && !content.Empty() {
		if pageCanvas.Bounds().Size() == content.Size() {
			draw.Draw(canvas, content, pageCanvas, pageCanvas.Bounds().Min, draw.Src)
		} else {
			xdraw.CatmullRom.Scale(canvas, content, pageCanvas, pageCanvas.Bounds(), draw.Src, nil)
		}
	}
	if err := shell.drawScaledPageOverlays(canvas, layout, page, scale); err != nil {
		return nil, err
	}
	top, rail, err := shell.scaledStaticChrome(layout, scale)
	if err != nil {
		return nil, err
	}
	for _, layer := range []*image.RGBA{top, rail} {
		if layer != nil && !layer.Bounds().Empty() {
			draw.Draw(canvas, layer.Bounds(), layer, layer.Bounds().Min, draw.Src)
		}
	}
	if shell.inspectorOpen && !layout.inspector.Empty() {
		if err := drawScaledShellRegion(canvas, layout.window, layout.inspector, scale, func(layer *image.RGBA) error {
			return shell.drawInspector(layer, layout, page)
		}); err != nil {
			return nil, err
		}
	}
	if progress := shell.loadingProgressBounds(layout, page); !progress.Empty() {
		if err := drawScaledShellRegion(canvas, layout.window, progress, scale, func(layer *image.RGBA) error {
			fillRect(layer, progress, graphitePalette.teal)
			return nil
		}); err != nil {
			return nil, err
		}
	}
	return canvas, nil
}

func (shell *graphiteShell) staticChromeState() graphiteStaticChromeState {
	state := graphiteStaticChromeState{
		tabCount: len(shell.tabs), activeTab: shell.activeTab, hoveredTab: shell.hoveredTab,
		tabOffset: shell.tabOffset, showNewTab: shell.opener != nil && len(shell.tabs) < maximumGraphiteTabs,
		address: shell.address, addressFocused: shell.addressFocused, selectAll: shell.selectAll,
		inspectorOpen: shell.inspectorOpen, inspectorPanel: shell.inspectorPanel, chromeFocus: shell.chromeFocus,
		loading: shell.loading, canGoBack: shell.canGoBack, canGoForward: shell.canGoForward,
		navigationErr: shell.navigationErr, security: shellSecurityFor(shell.activePage(), shell.navigationErr),
	}
	for index := range shell.tabs {
		if index >= len(state.tabs) {
			break
		}
		page := shell.tabs[index].page
		metadata := page.Metadata()
		faviconURL := ""
		if metadata.FaviconURL != nil {
			faviconURL = metadata.FaviconURL.String()
		}
		favicon := page.Favicon()
		tab := graphiteTabChromeState{page: page, title: shellTabTitle(page), faviconURL: faviconURL}
		if favicon != nil {
			tab.faviconReady = true
			tab.faviconBounds = favicon.Bounds()
		}
		state.tabs[index] = tab
	}
	return state
}

func (shell *graphiteShell) scaledStaticChrome(layout shellLayout, scale float64) (*image.RGBA, *image.RGBA, error) {
	state := shell.staticChromeState()
	if !shell.chromeCache.stateValid || shell.chromeCache.state != state {
		shell.chromeCache.state = state
		shell.chromeCache.stateValid = true
		shell.chromeCache.revision++
		shell.chromeCache.keyValid = false
	}
	key := graphiteChromeCacheKey{
		width: shell.width, height: shell.height, scaleBits: math.Float64bits(scale),
		revision: shell.chromeCache.revision,
	}
	if shell.chromeCache.keyValid && shell.chromeCache.key == key {
		return shell.chromeCache.top, shell.chromeCache.rail, nil
	}
	cssChrome := image.NewRGBA(layout.window)
	fillRect(cssChrome, layout.window, graphitePalette.ink)
	if err := shell.drawStaticChrome(cssChrome, layout); err != nil {
		return nil, nil, err
	}
	deviceBounds := scaleImageRectangle(layout.window, scale)
	scaled := image.NewRGBA(deviceBounds)
	xdraw.CatmullRom.Scale(scaled, deviceBounds, cssChrome, cssChrome.Bounds(), draw.Src, nil)
	topBounds := scaleImageRectangle(image.Rect(0, 0, layout.rail.Min.X, graphiteChromeHeight).Intersect(layout.window), scale)
	railBounds := scaleImageRectangle(layout.rail, scale)
	shell.chromeCache.top = cloneRGBARegion(scaled, topBounds)
	shell.chromeCache.rail = cloneRGBARegion(scaled, railBounds)
	shell.chromeCache.key = key
	shell.chromeCache.keyValid = true
	shell.chromeCache.scales++
	return shell.chromeCache.top, shell.chromeCache.rail, nil
}

func cloneRGBARegion(source *image.RGBA, bounds image.Rectangle) *image.RGBA {
	bounds = bounds.Intersect(source.Bounds())
	if bounds.Empty() {
		return nil
	}
	result := image.NewRGBA(bounds)
	draw.Draw(result, bounds, source, bounds.Min, draw.Src)
	return result
}

func drawScaledShellRegion(
	destination *image.RGBA,
	window, bounds image.Rectangle,
	scale float64,
	paint func(*image.RGBA) error,
) error {
	if destination == nil || paint == nil {
		return nil
	}
	// Keep Catmull-Rom's support pixels around the tight paint bounds without
	// scaling the rest of the transparent window-sized overlay.
	bounds = bounds.Inset(-2).Intersect(window)
	if bounds.Empty() {
		return nil
	}
	cssLayer := image.NewRGBA(bounds)
	if err := paint(cssLayer); err != nil {
		return err
	}
	deviceBounds := scaleImageRectangle(bounds, scale)
	deviceLayer := image.NewRGBA(deviceBounds)
	xdraw.CatmullRom.Scale(deviceLayer, deviceBounds, cssLayer, bounds, draw.Src, nil)
	draw.Draw(destination, deviceBounds, deviceLayer, deviceBounds.Min, draw.Over)
	return nil
}

func finiteDeviceScale(scale float64) bool {
	return !math.IsNaN(scale) && !math.IsInf(scale, 0) && scale > 0 && scale <= 8
}

func scaleImageRectangle(rectangle image.Rectangle, scale float64) image.Rectangle {
	return image.Rect(
		int(math.Round(float64(rectangle.Min.X)*scale)),
		int(math.Round(float64(rectangle.Min.Y)*scale)),
		int(math.Round(float64(rectangle.Max.X)*scale)),
		int(math.Round(float64(rectangle.Max.Y)*scale)),
	)
}

type graphiteScrollbarThumbPaint struct {
	bounds image.Rectangle
	color  color.NRGBA
}

func (shell *graphiteShell) drawScrollbars(canvas *image.RGBA, layout shellLayout, page *browser.Page) error {
	thumbs, err := shell.scrollbarThumbPaints(layout, page)
	if err != nil {
		return err
	}
	for _, thumb := range thumbs {
		fillRoundedRect(canvas, thumb.bounds, graphiteScrollbarSize/2, thumb.color)
	}
	return nil
}

func (shell *graphiteShell) scrollbarThumbPaints(layout shellLayout, page *browser.Page) ([]graphiteScrollbarThumbPaint, error) {
	if page == nil {
		return nil, nil
	}
	state := shell.activeInputState()
	if state == nil {
		return nil, nil
	}
	content := visibleScrollbarContent(layout)
	geometry, err := page.ViewportGeometry()
	if err != nil {
		return nil, err
	}
	var result []graphiteScrollbarThumbPaint
	appendThumbs := func(bars shellScrollbars, thumbColor color.NRGBA) {
		for _, thumb := range []image.Rectangle{bars.horizontalThumb, bars.verticalThumb} {
			if !thumb.Empty() {
				result = append(result, graphiteScrollbarThumbPaint{bounds: thumb, color: thumbColor})
			}
		}
	}
	activity := shell.scrollbarActivityActive(state)
	draggingRoot := shell.scrollbarDrag.axis != shellScrollbarNone && shell.scrollbarDrag.target.Node == 0
	if state.scrollbarHoverRoot || draggingRoot || activity && state.scrollbarActivityRoot {
		rootColor := graphitePalette.dim
		if state.scrollbarHoverRoot || draggingRoot {
			rootColor = graphitePalette.muted
		}
		appendThumbs(graphiteScrollbars(content, geometry), rootColor)
	}

	target := state.scrollbarHover
	if activity && state.scrollbarActivity.Node != 0 {
		target = state.scrollbarActivity
	}
	if shell.scrollbarDrag.axis != shellScrollbarNone && shell.scrollbarDrag.target.Node != 0 {
		target = shell.scrollbarDrag.target
	}
	if target.Node != 0 {
		bars, targetErr := elementScrollbars(content, layout, page, target)
		if targetErr == nil {
			thumbColor := graphitePalette.dim
			if target == state.scrollbarHover || target == shell.scrollbarDrag.target {
				thumbColor = graphitePalette.muted
			}
			appendThumbs(bars, thumbColor)
		}
	}
	return result, nil
}

func (shell *graphiteShell) drawScaledPageOverlays(
	canvas *image.RGBA,
	layout shellLayout,
	page *browser.Page,
	scale float64,
) error {
	thumbs, err := shell.scrollbarThumbPaints(layout, page)
	if err != nil {
		return err
	}
	for _, thumb := range thumbs {
		thumb := thumb
		if err := drawScaledShellRegion(canvas, layout.window, thumb.bounds, scale, func(layer *image.RGBA) error {
			fillRoundedRect(layer, thumb.bounds, graphiteScrollbarSize/2, thumb.color)
			return nil
		}); err != nil {
			return err
		}
	}
	if bounds := shell.pageStatusPaintBounds(layout, page); !bounds.Empty() {
		if err := drawScaledShellRegion(canvas, layout.window, bounds, scale, func(layer *image.RGBA) error {
			return shell.drawPageStatus(layer, layout, page)
		}); err != nil {
			return err
		}
	}
	if bounds := shell.interactionOverlayPaintBounds(layout, page); !bounds.Empty() {
		if err := drawScaledShellRegion(canvas, layout.window, bounds, scale, func(layer *image.RGBA) error {
			return shell.drawInteractionOverlay(layer, layout, page)
		}); err != nil {
			return err
		}
	}
	return nil
}

func (shell *graphiteShell) pageStatusPaintBounds(layout shellLayout, page *browser.Page) image.Rectangle {
	if page == nil || page.URL() == nil {
		return layout.content.Inset(24)
	}
	if shell.navigationErr != "" {
		return layout.errorBanner
	}
	return image.Rectangle{}
}

func (shell *graphiteShell) interactionOverlayPaintBounds(layout shellLayout, page *browser.Page) image.Rectangle {
	if shell == nil || page == nil {
		return image.Rectangle{}
	}
	state := shell.activeInputState()
	if state == nil {
		return image.Rectangle{}
	}
	focus := state.focusTarget
	if focus.Node == 0 {
		if active, ok := page.ActiveElementHandle(); ok {
			focus = active
		}
	}
	if focus.Node == 0 {
		return image.Rectangle{}
	}
	if _, err := page.TextControlPresentation(focus); err != nil {
		return image.Rectangle{}
	}
	geometry, err := page.ElementGeometry(focus)
	if err != nil || geometry.Rect.Width <= 2 || geometry.Rect.Height <= 2 {
		return image.Rectangle{}
	}
	return image.Rect(
		layout.content.Min.X+int(math.Round(geometry.Rect.X)),
		layout.content.Min.Y+int(math.Round(geometry.Rect.Y)),
		layout.content.Min.X+int(math.Round(geometry.Rect.X+geometry.Rect.Width)),
		layout.content.Min.Y+int(math.Round(geometry.Rect.Y+geometry.Rect.Height)),
	).Intersect(layout.content)
}

func (shell *graphiteShell) drawTabs(canvas *image.RGBA, layout shellLayout) error {
	for index, tabLayout := range layout.tabs {
		if index >= len(shell.tabs) || tabLayout.body.Empty() {
			continue
		}
		active := index == shell.activeTab
		fill := graphitePalette.ink
		textColor := graphitePalette.muted
		if active {
			fill = graphitePalette.surface
			textColor = graphitePalette.pearl
		} else if index == shell.hoveredTab {
			fill = graphitePalette.raised
			textColor = graphitePalette.pearl
		}
		fillTopRoundedRect(canvas, tabLayout.body, graphiteTabTopRadius, fill)
		if shell.chromeFocus == shellFocusTab && active {
			strokeRoundedRect(canvas, tabLayout.body.Inset(2), graphiteTabTopRadius-2, 1, graphitePalette.teal)
		}
		if active {
			fillRect(canvas, image.Rect(tabLayout.body.Min.X, tabLayout.body.Max.Y-1, tabLayout.body.Max.X, tabLayout.body.Max.Y), graphitePalette.tealDim)
		}
		textLeft := tabLayout.body.Min.X + 8
		if tabLayout.body.Dx() >= 52 {
			if favicon := shell.tabs[index].page.Favicon(); favicon != nil {
				drawScaledImage(canvas, image.Rect(tabLayout.body.Min.X+8, tabLayout.body.Min.Y+6, tabLayout.body.Min.X+24, tabLayout.body.Min.Y+22), favicon)
			} else {
				drawKnot(canvas, image.Pt(tabLayout.body.Min.X+16, tabLayout.body.Min.Y+14), 8)
			}
			textLeft = tabLayout.body.Min.X + 34
		}
		textRight := tabLayout.body.Max.X - 6
		if !tabLayout.close.Empty() {
			textRight = tabLayout.close.Min.X - 4
		}
		titleClip := image.Rect(textLeft, tabLayout.body.Min.Y, textRight, tabLayout.body.Max.Y)
		if err := shell.fonts.draw(canvas, titleClip, shellTabTitle(shell.tabs[index].page), textLeft, tabLayout.body.Min.Y+20, 12, textColor, active); err != nil {
			return err
		}
		if !tabLayout.close.Empty() {
			closeColor := graphitePalette.dim
			if active {
				closeColor = graphitePalette.muted
			}
			drawClose(canvas, tabLayout.close, closeColor)
		}
	}
	if !layout.newTab.Empty() {
		drawPlus(canvas, center(layout.newTab), graphitePalette.muted)
	}
	if !layout.tabOverflowBack.Empty() {
		drawChevron(canvas, center(layout.tabOverflowBack), false, graphitePalette.muted)
		drawChevron(canvas, center(layout.tabOverflowNext), true, graphitePalette.muted)
	}
	return nil
}

func (shell *graphiteShell) drawToolbar(canvas *image.RGBA, layout shellLayout) error {
	backColor := graphitePalette.dim
	if shell.canGoBack {
		backColor = graphitePalette.muted
	}
	forwardColor := graphitePalette.dim
	if shell.canGoForward {
		forwardColor = graphitePalette.muted
	}
	drawChevron(canvas, center(layout.back), false, backColor)
	drawChevron(canvas, center(layout.forward), true, forwardColor)
	for focus, bounds := range map[shellFocus]image.Rectangle{
		shellFocusBack: layout.back, shellFocusForward: layout.forward, shellFocusReload: layout.reload,
	} {
		if shell.chromeFocus == focus {
			strokeRoundedRect(canvas, bounds.Inset(3), 8, 1, graphitePalette.teal)
		}
	}
	if shell.loading {
		drawStop(canvas, center(layout.reload), graphitePalette.muted)
	} else {
		drawReload(canvas, center(layout.reload), graphitePalette.muted)
	}

	addressFill := graphitePalette.ink
	addressBorder := graphitePalette.border
	if shell.addressFocused {
		addressBorder = graphitePalette.teal
	}
	if shell.navigationErr != "" {
		addressBorder = graphitePalette.danger
	}
	fillRoundedRect(canvas, layout.address, graphiteAddressRadius, addressFill)
	strokeRoundedRect(canvas, layout.address, graphiteAddressRadius, 1, addressBorder)
	securityColor := graphitePalette.dim
	switch shellSecurityFor(shell.activePage(), shell.navigationErr) {
	case shellSecuritySecure:
		securityColor = graphitePalette.teal
	case shellSecurityInsecure:
		securityColor = graphitePalette.violet
	case shellSecurityError:
		securityColor = graphitePalette.danger
	}
	drawShield(canvas, image.Pt(layout.address.Min.X+17, center(layout.address).Y), securityColor)
	addressClip := image.Rect(layout.address.Min.X+33, layout.address.Min.Y+2, layout.address.Max.X-12, layout.address.Max.Y-2)
	addressText := shell.address
	textColor := graphitePalette.pearl
	if addressText == "" {
		addressText = "Search or enter address"
		textColor = graphitePalette.dim
	}
	if err := shell.fonts.draw(canvas, addressClip, addressText, addressClip.Min.X, layout.address.Min.Y+22, 13, textColor, false); err != nil {
		return err
	}
	if shell.addressFocused && shell.selectAll {
		fillRectAlpha(canvas, image.Rect(addressClip.Min.X-2, addressClip.Min.Y+4, addressClip.Max.X, addressClip.Max.Y-4), color.NRGBA{R: 0x2b, G: 0x79, B: 0x7b, A: 0x78})
		if err := shell.fonts.draw(canvas, addressClip, addressText, addressClip.Min.X, layout.address.Min.Y+22, 13, graphitePalette.pearl, false); err != nil {
			return err
		}
	}
	return nil
}

func (shell *graphiteShell) drawPageStatus(canvas *image.RGBA, layout shellLayout, page *browser.Page) error {
	if page == nil || page.URL() == nil {
		panel := layout.content.Inset(24)
		if panel.Empty() {
			return nil
		}
		centerPoint := center(panel)
		drawKnot(canvas, image.Pt(centerPoint.X, centerPoint.Y-28), 20)
		if err := shell.fonts.draw(canvas, panel, "NEW STRAND", centerPoint.X-48, centerPoint.Y+14, 14, graphitePalette.pearl, true); err != nil {
			return err
		}
		return shell.fonts.draw(canvas, panel, "Search or enter an address above", centerPoint.X-112, centerPoint.Y+40, 12, graphitePalette.muted, false)
	}
	if shell.navigationErr == "" || layout.errorBanner.Empty() {
		return nil
	}
	fillRectAlpha(canvas, layout.errorBanner, color.NRGBA{R: 0x35, G: 0x13, B: 0x1b, A: 0xf2})
	messageClip := layout.errorBanner.Inset(12)
	messageClip.Max.X = layout.errorRetry.Min.X - 8
	message := shell.navigationErr
	if len(message) > 72 {
		message = message[:69] + "..."
	}
	if err := shell.fonts.draw(canvas, messageClip, message, messageClip.Min.X, layout.errorBanner.Min.Y+31, 12, graphitePalette.pearl, false); err != nil {
		return err
	}
	fillRoundedRect(canvas, layout.errorRetry, 8, graphitePalette.raised)
	return shell.fonts.draw(canvas, layout.errorRetry, "Retry", layout.errorRetry.Min.X+18, layout.errorRetry.Min.Y+21, 12, graphitePalette.pearl, true)
}

func (shell *graphiteShell) drawInteractionOverlay(canvas *image.RGBA, layout shellLayout, page *browser.Page) error {
	if shell == nil || page == nil {
		return nil
	}
	state := shell.activeInputState()
	if state == nil {
		return nil
	}
	focus := state.focusTarget
	if focus.Node == 0 {
		if active, ok := page.ActiveElementHandle(); ok {
			focus = active
		}
	}
	if focus.Node == 0 {
		return nil
	}
	presentation, err := page.TextControlPresentation(focus)
	if err != nil {
		return nil
	}
	geometry, err := page.ElementGeometry(focus)
	if err != nil || geometry.Rect.Width <= 2 || geometry.Rect.Height <= 2 {
		return nil
	}
	control := image.Rect(
		layout.content.Min.X+int(math.Round(geometry.Rect.X)),
		layout.content.Min.Y+int(math.Round(geometry.Rect.Y)),
		layout.content.Min.X+int(math.Round(geometry.Rect.X+geometry.Rect.Width)),
		layout.content.Min.Y+int(math.Round(geometry.Rect.Y+geometry.Rect.Height)),
	).Intersect(layout.content)
	if control.Empty() {
		return nil
	}
	inner := control.Inset(4)
	if inner.Empty() {
		return nil
	}
	value := presentation.Value
	if presentation.Password {
		value = strings.Repeat("•", len([]rune(value)))
	}
	if presentation.Multiline {
		value = strings.Split(value, "\n")[0]
	}
	units := utf16.Encode([]rune(presentation.Value))
	length := maxInt(1, len(units))
	start := maxInt(0, minInt(length, presentation.SelectionStart))
	end := maxInt(0, minInt(length, presentation.SelectionEnd))
	if end < start {
		start, end = end, start
	}
	textWidth := maxInt(1, inner.Dx()-2)
	xForOffset := func(offset int) int {
		return inner.Min.X + int(math.Round(float64(textWidth)*float64(offset)/float64(length)))
	}
	if start != end {
		selection := image.Rect(xForOffset(start), inner.Min.Y, xForOffset(end), inner.Max.Y)
		fillRectAlpha(canvas, selection, color.NRGBA{R: 0x2b, G: 0x79, B: 0x7b, A: 0x88})
	} else {
		caretX := xForOffset(start)
		fillRect(canvas, image.Rect(caretX, inner.Min.Y+1, caretX+1, inner.Max.Y-1), graphitePalette.teal)
	}
	baseline := minInt(inner.Max.Y-2, inner.Min.Y+15)
	return shell.fonts.draw(canvas, inner, value, inner.Min.X, baseline, 12, graphitePalette.pearl, false)
}

func drawScaledImage(destination *image.RGBA, bounds image.Rectangle, source image.Image) {
	if destination == nil || source == nil || bounds.Empty() || source.Bounds().Empty() {
		return
	}
	sourceBounds := source.Bounds()
	scaler := xdraw.Scaler(xdraw.ApproxBiLinear)
	if bounds.Dx() < sourceBounds.Dx() || bounds.Dy() < sourceBounds.Dy() {
		scaler = xdraw.CatmullRom
	}
	scaler.Scale(destination, bounds, source, sourceBounds, draw.Over, nil)
}

func (shell *graphiteShell) drawRailChrome(canvas *image.RGBA, layout shellLayout) {
	centerX := center(layout.rail).X
	drawKnot(canvas, image.Pt(centerX, 22), 9)
	kinds := [...]string{"nodes", "list", "box", "pulse", "memory"}
	for index, bounds := range layout.railPanels {
		stroke := graphitePalette.muted
		if shell.inspectorOpen && shell.inspectorPanel == inspectorPanel(index) {
			stroke = graphitePalette.teal
			fillRect(canvas, image.Rect(bounds.Min.X, bounds.Min.Y, bounds.Min.X+2, bounds.Max.Y), graphitePalette.teal)
		}
		drawRailGlyph(canvas, center(bounds), kinds[index], stroke)
	}
	disclosureColor := graphitePalette.muted
	if shell.inspectorOpen {
		disclosureColor = graphitePalette.teal
		fillRect(canvas, image.Rect(layout.rail.Min.X, layout.railDisclosure.Min.Y, layout.rail.Min.X+2, layout.railDisclosure.Max.Y), graphitePalette.teal)
	}
	drawChevron(canvas, center(layout.railDisclosure), !shell.inspectorOpen, disclosureColor)
	drawGear(canvas, image.Pt(centerX, shell.height-24), graphitePalette.dim)
}

func (shell *graphiteShell) drawInspector(canvas *image.RGBA, layout shellLayout, page *browser.Page) error {
	panel := layout.inspector
	if panel.Empty() {
		return nil
	}
	fillRectAlpha(canvas, panel, color.NRGBA{R: 0x0c, G: 0x12, B: 0x19, A: 0xf6})
	fillRect(canvas, image.Rect(panel.Min.X, panel.Min.Y, panel.Min.X+1, panel.Max.Y), graphitePalette.border)
	content := panel.Inset(18)
	content.Max.Y = panel.Max.Y - 12
	y := panel.Min.Y + 28
	titles := [...]string{"DOM", "COMPUTED STYLE", "LAYOUT", "NETWORK", "MEMORY + OWNERSHIP"}
	drawKnot(canvas, image.Pt(content.Min.X+10, y-5), 10)
	if err := shell.fonts.draw(canvas, content, titles[shell.inspectorPanel], content.Min.X+32, y, 13, graphitePalette.pearl, true); err != nil {
		return err
	}
	y += 34
	target := shell.inspectorTarget(page)
	if node, err := page.InspectorNode(target); err == nil {
		label := "<" + node.Name + ">"
		if err := shell.drawInspectorValue(canvas, content, &y, "Selected", label); err != nil {
			return err
		}
		y += 8
	}
	switch shell.inspectorPanel {
	case inspectorDOM:
		return shell.drawDOMInspector(canvas, content, &y, page)
	case inspectorStyles:
		return shell.drawStyleInspector(canvas, content, &y, page, target)
	case inspectorLayout:
		return shell.drawLayoutInspector(canvas, content, &y, page, target)
	case inspectorNetwork:
		return shell.drawNetworkInspector(canvas, content, &y, page)
	case inspectorMemory:
		return shell.drawMemoryInspector(canvas, content, &y, page)
	}
	return nil
}

func (shell *graphiteShell) inspectorTarget(page *browser.Page) browser.NodeHandle {
	if state := shell.activeInputState(); state != nil {
		if state.hoverTarget.Node != 0 {
			return state.hoverTarget
		}
		if state.focusTarget.Node != 0 {
			return state.focusTarget
		}
	}
	if active, ok := page.ActiveElementHandle(); ok {
		return active
	}
	root, _ := page.DocumentElementHandle()
	return root
}

func (shell *graphiteShell) drawDOMInspector(canvas *image.RGBA, clip image.Rectangle, y *int, page *browser.Page) error {
	lines, err := page.InspectorDOMLines(18)
	if err != nil {
		return shell.drawInspectorError(canvas, clip, y, err)
	}
	for _, line := range lines {
		if *y > clip.Max.Y-8 {
			break
		}
		if len(line) > 42 {
			line = line[:39] + "..."
		}
		if err := shell.fonts.draw(canvas, clip, line, clip.Min.X, *y, 10, graphitePalette.pearl, false); err != nil {
			return err
		}
		*y += 17
	}
	return nil
}

func (shell *graphiteShell) drawStyleInspector(canvas *image.RGBA, clip image.Rectangle, y *int, page *browser.Page, target browser.NodeHandle) error {
	style, err := page.ComputedStyle(target)
	if err != nil {
		return shell.drawInspectorError(canvas, clip, y, err)
	}
	for _, name := range computed.ComputedPropertyNames(style) {
		if *y > clip.Max.Y-8 {
			break
		}
		value, found := computed.ComputedPropertyValue(style, name)
		if !found {
			continue
		}
		if len(value) > 18 {
			value = value[:15] + "..."
		}
		if err := shell.drawInspectorValue(canvas, clip, y, name, value); err != nil {
			return err
		}
	}
	return nil
}

func (shell *graphiteShell) drawLayoutInspector(canvas *image.RGBA, clip image.Rectangle, y *int, page *browser.Page, target browser.NodeHandle) error {
	geometry, err := page.ElementGeometry(target)
	if err != nil {
		return shell.drawInspectorError(canvas, clip, y, err)
	}
	values := [][2]string{
		{"Position", fmt.Sprintf("%.0f, %.0f", geometry.Rect.X, geometry.Rect.Y)},
		{"Size", fmt.Sprintf("%.0f x %.0f", geometry.Rect.Width, geometry.Rect.Height)},
		{"Client", fmt.Sprintf("%.0f x %.0f", geometry.ClientWidth, geometry.ClientHeight)},
		{"Offset", fmt.Sprintf("%.0f x %.0f", geometry.OffsetWidth, geometry.OffsetHeight)},
		{"Scroll size", fmt.Sprintf("%.0f x %.0f", geometry.ScrollWidth, geometry.ScrollHeight)},
		{"Scroll offset", fmt.Sprintf("%.0f, %.0f", geometry.ScrollLeft, geometry.ScrollTop)},
		{"Client rects", fmt.Sprint(len(geometry.ClientRects))},
	}
	return shell.drawInspectorValues(canvas, clip, y, values)
}

func (shell *graphiteShell) drawNetworkInspector(canvas *image.RGBA, clip image.Rectangle, y *int, page *browser.Page) error {
	navigation := page.Navigation()
	location := "about:blank"
	if navigation.URL != nil {
		location = navigation.URL.String()
	} else if page.URL() != nil {
		location = page.URL().String()
	}
	values := [][2]string{
		{"State", shellNavigationLabel(navigation)},
		{"URL", location},
		{"Resources", fmt.Sprintf("%d total / %d pending", navigation.ResourcesTotal, navigation.ResourcesPending)},
		{"Resource errors", fmt.Sprint(navigation.ResourcesFailed)},
		{"Scripts", fmt.Sprintf("%d total / %d pending", navigation.ScriptsTotal, navigation.ScriptsPending)},
		{"Script errors", fmt.Sprint(navigation.ScriptsFailed)},
	}
	if navigation.Err != nil {
		values = append(values, [2]string{"Error", navigation.Err.Error()})
	}
	return shell.drawInspectorValues(canvas, clip, y, values)
}

func (shell *graphiteShell) drawMemoryInspector(canvas *image.RGBA, clip image.Rectangle, y *int, page *browser.Page) error {
	profile := page.Realm.Profile()
	values := [][2]string{
		{"Regions", fmt.Sprint(profile.Memory.LiveRegions)},
		{"Live bytes", fmt.Sprint(profile.Memory.LiveBytes)},
		{"Native objects", fmt.Sprint(profile.Memory.LiveHostObjects)},
		{"Ownership claims", fmt.Sprint(profile.Ownership.LiveObjects)},
		{"Allocations", fmt.Sprint(profile.Memory.Allocations)},
		{"Frees", fmt.Sprint(profile.Memory.Frees)},
		{"Bulk releases", fmt.Sprint(profile.Memory.BulkRegionReleases)},
		{"Promotions", fmt.Sprint(profile.Memory.AutomaticPromotions)},
		{"Queue depth", fmt.Sprint(profile.TaskDepth + profile.MicrotaskDepth)},
	}
	return shell.drawInspectorValues(canvas, clip, y, values)
}

func (shell *graphiteShell) drawInspectorValues(canvas *image.RGBA, clip image.Rectangle, y *int, values [][2]string) error {
	for _, value := range values {
		if *y > clip.Max.Y-8 {
			break
		}
		if len(value[1]) > 24 {
			value[1] = value[1][:21] + "..."
		}
		if err := shell.drawInspectorValue(canvas, clip, y, value[0], value[1]); err != nil {
			return err
		}
	}
	return nil
}

func (shell *graphiteShell) drawInspectorError(canvas *image.RGBA, clip image.Rectangle, y *int, cause error) error {
	message := cause.Error()
	if len(message) > 36 {
		message = message[:33] + "..."
	}
	return shell.fonts.draw(canvas, clip, message, clip.Min.X, *y, 10, graphitePalette.danger, false)
}

func (shell *graphiteShell) drawInspectorLabel(canvas *image.RGBA, clip image.Rectangle, y *int, label string, labelColor color.NRGBA) error {
	if err := shell.fonts.draw(canvas, clip, label, clip.Min.X, *y, 10, labelColor, true); err != nil {
		return err
	}
	*y += 24
	return nil
}

func (shell *graphiteShell) drawInspectorValue(canvas *image.RGBA, clip image.Rectangle, y *int, label, value string) error {
	if err := shell.fonts.draw(canvas, clip, label, clip.Min.X, *y, 11, graphitePalette.muted, false); err != nil {
		return err
	}
	valueX := clip.Min.X + 112
	if err := shell.fonts.draw(canvas, clip, value, valueX, *y, 11, graphitePalette.pearl, false); err != nil {
		return err
	}
	*y += 22
	return nil
}

func fillRect(dst *image.RGBA, rectangle image.Rectangle, fill color.NRGBA) {
	draw.Draw(dst, rectangle.Intersect(dst.Bounds()), image.NewUniform(fill), image.Point{}, draw.Src)
}

func fillRectAlpha(dst *image.RGBA, rectangle image.Rectangle, fill color.NRGBA) {
	draw.Draw(dst, rectangle.Intersect(dst.Bounds()), image.NewUniform(fill), image.Point{}, draw.Over)
}

func fillRoundedRect(dst *image.RGBA, rectangle image.Rectangle, radius int, fill color.NRGBA) {
	rectangle = rectangle.Intersect(dst.Bounds())
	if rectangle.Empty() {
		return
	}
	if radius <= 0 {
		fillRect(dst, rectangle, fill)
		return
	}
	maximum := minInt(rectangle.Dx()/2, rectangle.Dy()/2)
	if radius > maximum {
		radius = maximum
	}
	fillRect(dst, image.Rect(rectangle.Min.X+radius, rectangle.Min.Y, rectangle.Max.X-radius, rectangle.Max.Y), fill)
	fillRect(dst, image.Rect(rectangle.Min.X, rectangle.Min.Y+radius, rectangle.Max.X, rectangle.Max.Y-radius), fill)
	for y := 0; y < radius; y++ {
		for x := 0; x < radius; x++ {
			dx := float64(radius-x) - 0.5
			dy := float64(radius-y) - 0.5
			if dx*dx+dy*dy > float64(radius*radius) {
				continue
			}
			setNRGBA(dst, rectangle.Min.X+x, rectangle.Min.Y+y, fill)
			setNRGBA(dst, rectangle.Max.X-1-x, rectangle.Min.Y+y, fill)
			setNRGBA(dst, rectangle.Min.X+x, rectangle.Max.Y-1-y, fill)
			setNRGBA(dst, rectangle.Max.X-1-x, rectangle.Max.Y-1-y, fill)
		}
	}
}

func fillTopRoundedRect(dst *image.RGBA, rectangle image.Rectangle, radius int, fill color.NRGBA) {
	rectangle = rectangle.Intersect(dst.Bounds())
	if rectangle.Empty() {
		return
	}
	if radius <= 0 {
		fillRect(dst, rectangle, fill)
		return
	}
	maximum := minInt(rectangle.Dx()/2, rectangle.Dy())
	if radius > maximum {
		radius = maximum
	}
	fillRect(dst, image.Rect(rectangle.Min.X+radius, rectangle.Min.Y, rectangle.Max.X-radius, rectangle.Max.Y), fill)
	fillRect(dst, image.Rect(rectangle.Min.X, rectangle.Min.Y+radius, rectangle.Max.X, rectangle.Max.Y), fill)
	for y := 0; y < radius; y++ {
		for x := 0; x < radius; x++ {
			dx := float64(radius-x) - 0.5
			dy := float64(radius-y) - 0.5
			if dx*dx+dy*dy > float64(radius*radius) {
				continue
			}
			setNRGBA(dst, rectangle.Min.X+x, rectangle.Min.Y+y, fill)
			setNRGBA(dst, rectangle.Max.X-1-x, rectangle.Min.Y+y, fill)
		}
	}
}

func strokeRoundedRect(dst *image.RGBA, rectangle image.Rectangle, radius, width int, stroke color.NRGBA) {
	fillRoundedRect(dst, rectangle, radius, stroke)
	inner := rectangle.Inset(width)
	if !inner.Empty() {
		fillRoundedRect(dst, inner, maxInt(0, radius-width), graphitePalette.ink)
	}
}

func setNRGBA(dst *image.RGBA, x, y int, value color.NRGBA) {
	if image.Pt(x, y).In(dst.Bounds()) {
		dst.Set(x, y, value)
	}
}

func center(rectangle image.Rectangle) image.Point {
	return image.Pt((rectangle.Min.X+rectangle.Max.X)/2, (rectangle.Min.Y+rectangle.Max.Y)/2)
}

func drawThickLine(dst *image.RGBA, from, to image.Point, width int, stroke color.NRGBA) {
	dx := to.X - from.X
	dy := to.Y - from.Y
	steps := maxInt(absInt(dx), absInt(dy))
	if steps == 0 {
		fillCircle(dst, from, width/2, stroke)
		return
	}
	for step := 0; step <= steps; step++ {
		x := from.X + dx*step/steps
		y := from.Y + dy*step/steps
		fillCircle(dst, image.Pt(x, y), maxInt(1, width/2), stroke)
	}
}

func fillCircle(dst *image.RGBA, center image.Point, radius int, fill color.NRGBA) {
	for y := -radius; y <= radius; y++ {
		for x := -radius; x <= radius; x++ {
			if x*x+y*y <= radius*radius {
				setNRGBA(dst, center.X+x, center.Y+y, fill)
			}
		}
	}
}

func drawKnot(dst *image.RGBA, center image.Point, radius int) {
	crossing := knotCrossingOffset(radius)
	width := maxInt(2, radius/3)
	gapWidth := width + 2
	gap := graphitePalette.ink
	tealPath := knotCapsulePath(center, radius, false)
	violetPath := knotCapsulePath(center, radius, true)

	// Paint both closed strands, then restore the teal bridge at the north and
	// south crossings. Violet remains above at east and west, producing a real
	// alternating over-under knot rather than two adjacent chain links.
	drawPolyline(dst, tealPath, gapWidth, gap)
	drawPolyline(dst, tealPath, width, graphitePalette.teal)
	drawPolyline(dst, violetPath, gapWidth, gap)
	drawPolyline(dst, violetPath, width, graphitePalette.violet)
	bridge := maxInt(2, width)
	for _, y := range []int{-crossing, crossing} {
		from := center.Add(image.Pt(-bridge, y-bridge))
		to := center.Add(image.Pt(bridge, y+bridge))
		drawThickLine(dst, from, to, gapWidth, gap)
		drawThickLine(dst, from, to, width, graphitePalette.teal)
	}
	pearlRadius := maxInt(1, radius/8)
	for _, offset := range []image.Point{
		image.Pt(0, -crossing), image.Pt(crossing, 0),
		image.Pt(0, crossing), image.Pt(-crossing, 0),
	} {
		fillCircle(dst, center.Add(offset), pearlRadius, graphitePalette.pearl)
	}
}

func knotCrossingOffset(radius int) int {
	return maxInt(2, int(math.Round(knotCapsuleHalfWidth(radius)*math.Sqrt2)))
}

func knotCapsuleHalfWidth(radius int) float64 {
	return math.Max(2, float64(radius)*0.42)
}

func knotCapsulePath(center image.Point, radius int, opposite bool) []image.Point {
	const diagonal = math.Sqrt2 / 2
	directionX := diagonal
	directionY := diagonal
	if opposite {
		directionX = -diagonal
	}
	normalX := -directionY
	normalY := directionX
	halfWidth := knotCapsuleHalfWidth(radius)
	halfLength := math.Max(1, float64(radius)*math.Sqrt2-halfWidth)
	centerX := float64(center.X)
	centerY := float64(center.Y)
	startX := centerX - directionX*halfLength
	startY := centerY - directionY*halfLength
	endX := centerX + directionX*halfLength
	endY := centerY + directionY*halfLength
	startOuter := floatPoint{
		x: startX + normalX*halfWidth,
		y: startY + normalY*halfWidth,
	}
	endOuter := floatPoint{x: endX + normalX*halfWidth, y: endY + normalY*halfWidth}
	endInner := floatPoint{x: endX - normalX*halfWidth, y: endY - normalY*halfWidth}
	startInner := floatPoint{x: startX - normalX*halfWidth, y: startY - normalY*halfWidth}
	endControl := floatPoint{
		x: endX + directionX*halfWidth*2,
		y: endY + directionY*halfWidth*2,
	}
	startControl := floatPoint{
		x: startX - directionX*halfWidth*2,
		y: startY - directionY*halfWidth*2,
	}
	path := []image.Point{roundFloatPoint(startOuter), roundFloatPoint(endOuter)}
	path = appendQuadratic(path, endOuter, endControl, endInner, maxInt(5, radius))
	path = append(path, roundFloatPoint(startInner))
	path = appendQuadratic(path, startInner, startControl, startOuter, maxInt(5, radius))
	if path[len(path)-1] != path[0] {
		path = append(path, path[0])
	}
	return path
}

type floatPoint struct {
	x float64
	y float64
}

func appendQuadratic(path []image.Point, from, control, to floatPoint, steps int) []image.Point {
	for step := 1; step <= steps; step++ {
		t := float64(step) / float64(steps)
		oneMinusT := 1 - t
		point := roundFloatPoint(floatPoint{
			x: oneMinusT*oneMinusT*from.x + 2*oneMinusT*t*control.x + t*t*to.x,
			y: oneMinusT*oneMinusT*from.y + 2*oneMinusT*t*control.y + t*t*to.y,
		})
		if path[len(path)-1] != point {
			path = append(path, point)
		}
	}
	return path
}

func roundFloatPoint(point floatPoint) image.Point {
	return image.Pt(int(math.Round(point.x)), int(math.Round(point.y)))
}

func drawPolyline(dst *image.RGBA, points []image.Point, width int, stroke color.NRGBA) {
	for index := 1; index < len(points); index++ {
		drawThickLine(dst, points[index-1], points[index], width, stroke)
	}
}

func drawClose(dst *image.RGBA, rectangle image.Rectangle, stroke color.NRGBA) {
	middle := center(rectangle)
	drawThickLine(dst, middle.Add(image.Pt(-4, -4)), middle.Add(image.Pt(4, 4)), 1, stroke)
	drawThickLine(dst, middle.Add(image.Pt(4, -4)), middle.Add(image.Pt(-4, 4)), 1, stroke)
}

func drawPlus(dst *image.RGBA, middle image.Point, stroke color.NRGBA) {
	drawThickLine(dst, middle.Add(image.Pt(-5, 0)), middle.Add(image.Pt(5, 0)), 1, stroke)
	drawThickLine(dst, middle.Add(image.Pt(0, -5)), middle.Add(image.Pt(0, 5)), 1, stroke)
}

func drawChevron(dst *image.RGBA, middle image.Point, right bool, stroke color.NRGBA) {
	direction := -1
	if right {
		direction = 1
	}
	drawThickLine(dst, middle.Add(image.Pt(-direction*3, -6)), middle.Add(image.Pt(direction*3, 0)), 2, stroke)
	drawThickLine(dst, middle.Add(image.Pt(direction*3, 0)), middle.Add(image.Pt(-direction*3, 6)), 2, stroke)
}

func drawReload(dst *image.RGBA, middle image.Point, stroke color.NRGBA) {
	for degree := 35; degree < 330; degree += 9 {
		radians := float64(degree) * math.Pi / 180
		point := middle.Add(image.Pt(int(math.Round(math.Cos(radians)*7)), int(math.Round(math.Sin(radians)*7))))
		fillCircle(dst, point, 1, stroke)
	}
	drawThickLine(dst, middle.Add(image.Pt(5, -6)), middle.Add(image.Pt(8, -6)), 2, stroke)
	drawThickLine(dst, middle.Add(image.Pt(8, -6)), middle.Add(image.Pt(8, -2)), 2, stroke)
}

func drawStop(dst *image.RGBA, middle image.Point, fill color.NRGBA) {
	fillRoundedRect(dst, image.Rect(middle.X-5, middle.Y-5, middle.X+5, middle.Y+5), 2, fill)
}

func drawShield(dst *image.RGBA, middle image.Point, stroke color.NRGBA) {
	points := []image.Point{
		middle.Add(image.Pt(0, -7)), middle.Add(image.Pt(6, -4)), middle.Add(image.Pt(5, 3)),
		middle.Add(image.Pt(0, 7)), middle.Add(image.Pt(-5, 3)), middle.Add(image.Pt(-6, -4)), middle.Add(image.Pt(0, -7)),
	}
	drawPolyline(dst, points, 1, stroke)
}

func drawRailGlyph(dst *image.RGBA, middle image.Point, kind string, stroke color.NRGBA) {
	switch strings.ToLower(kind) {
	case "nodes":
		for _, offset := range []image.Point{image.Pt(-5, -5), image.Pt(5, -5), image.Pt(0, 6)} {
			fillCircle(dst, middle.Add(offset), 2, stroke)
		}
		drawThickLine(dst, middle.Add(image.Pt(-5, -4)), middle.Add(image.Pt(0, 5)), 1, stroke)
		drawThickLine(dst, middle.Add(image.Pt(5, -4)), middle.Add(image.Pt(0, 5)), 1, stroke)
	case "list":
		for y := -5; y <= 5; y += 5 {
			fillCircle(dst, middle.Add(image.Pt(-6, y)), 1, stroke)
			drawThickLine(dst, middle.Add(image.Pt(-2, y)), middle.Add(image.Pt(7, y)), 1, stroke)
		}
	case "pulse":
		points := []image.Point{
			middle.Add(image.Pt(-8, 2)), middle.Add(image.Pt(-4, 2)), middle.Add(image.Pt(-1, -6)),
			middle.Add(image.Pt(2, 7)), middle.Add(image.Pt(5, -2)), middle.Add(image.Pt(8, -2)),
		}
		drawPolyline(dst, points, 1, stroke)
	case "box":
		drawPolyline(dst, []image.Point{
			middle.Add(image.Pt(-7, -6)), middle.Add(image.Pt(7, -6)), middle.Add(image.Pt(7, 6)),
			middle.Add(image.Pt(-7, 6)), middle.Add(image.Pt(-7, -6)),
		}, 1, stroke)
		fillRect(dst, image.Rect(middle.X-3, middle.Y-2, middle.X+4, middle.Y+3), stroke)
	case "memory":
		for y := -5; y <= 5; y += 5 {
			drawThickLine(dst, middle.Add(image.Pt(-7, y)), middle.Add(image.Pt(7, y)), 2, stroke)
		}
		drawThickLine(dst, middle.Add(image.Pt(-5, -7)), middle.Add(image.Pt(-5, 7)), 1, stroke)
		drawThickLine(dst, middle.Add(image.Pt(5, -7)), middle.Add(image.Pt(5, 7)), 1, stroke)
	}
}

func drawGear(dst *image.RGBA, middle image.Point, stroke color.NRGBA) {
	for degree := 0; degree < 360; degree += 45 {
		radians := float64(degree) * math.Pi / 180
		inner := middle.Add(image.Pt(int(math.Round(math.Cos(radians)*5)), int(math.Round(math.Sin(radians)*5))))
		outer := middle.Add(image.Pt(int(math.Round(math.Cos(radians)*8)), int(math.Round(math.Sin(radians)*8))))
		drawThickLine(dst, inner, outer, 2, stroke)
	}
	fillCircle(dst, middle, 5, stroke)
	fillCircle(dst, middle, 2, graphitePalette.top)
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
