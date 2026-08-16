package window

import (
	"context"
	"errors"
	"fmt"
	"image"
	"math"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/render"
)

const (
	graphiteTabHeight     = 36
	graphiteToolbarHeight = 48
	graphiteChromeHeight  = graphiteTabHeight + graphiteToolbarHeight
	graphiteRailWidth     = 48
	graphiteAddressRadius = 16
	graphiteTabTopRadius  = 16
	graphiteScrollbarSize = 8
	graphiteScrollbarGap  = 2
	graphiteScrollbarMin  = 24
	minimumContentWidth   = 160
	minimumContentHeight  = 120
)

// ShellConfig configures Gossamer's browser-owned Graphite chrome. Loader is
// reused for address-bar and reload navigation; nil keeps Page's default HTTP
// loader behavior.
type ShellConfig struct {
	Title   string
	Loader  browser.DocumentLoader
	OpenTab PageOpener
}

type shellLayout struct {
	window         image.Rectangle
	tabs           []shellTabLayout
	newTab         image.Rectangle
	back           image.Rectangle
	forward        image.Rectangle
	reload         image.Rectangle
	address        image.Rectangle
	content        image.Rectangle
	rail           image.Rectangle
	railDisclosure image.Rectangle
	inspector      image.Rectangle
}

type shellTabLayout struct {
	body  image.Rectangle
	close image.Rectangle
}

type shellScrollbarAxis uint8

const (
	shellScrollbarNone shellScrollbarAxis = iota
	shellScrollbarHorizontal
	shellScrollbarVertical
)

type shellScrollbarDrag struct {
	axis          shellScrollbarAxis
	pointerOffset int
}

type shellScrollbars struct {
	horizontalTrack image.Rectangle
	horizontalThumb image.Rectangle
	verticalTrack   image.Rectangle
	verticalThumb   image.Rectangle
	maximumX        float64
	maximumY        float64
}

type graphiteShell struct {
	loader    browser.DocumentLoader
	opener    PageOpener
	fonts     *shellFontBook
	tabs      []graphiteTab
	activeTab int

	width  int
	height int

	address        string
	lastPageURL    string
	addressFocused bool
	selectAll      bool
	inspectorOpen  bool
	loading        bool
	canGoBack      bool
	canGoForward   bool
	navigation     browser.NavigationID
	navigationView string
	navigationErr  string
	suppressedKeys map[string]bool
	scrollbarDrag  shellScrollbarDrag
	revision       uint64
}

func newGraphiteShell(page *browser.Page, config ShellConfig) (*graphiteShell, error) {
	fonts, err := newShellFontBook()
	if err != nil {
		return nil, err
	}
	location := ""
	if page != nil && page.URL() != nil {
		location = page.URL().String()
	}
	shell := &graphiteShell{
		loader:         config.Loader,
		opener:         config.OpenTab,
		fonts:          fonts,
		address:        location,
		lastPageURL:    location,
		canGoBack:      page.CanGoBack(),
		canGoForward:   page.CanGoForward(),
		navigationView: shellNavigationLabel(page.Navigation()),
		suppressedKeys: make(map[string]bool),
		revision:       1,
		activeTab:      0,
	}
	shell.tabs = []graphiteTab{{page: page, state: shell.capturePageState()}}
	return shell, nil
}

func (shell *graphiteShell) close() error {
	if shell == nil {
		return nil
	}
	return errors.Join(shell.closeOwnedTabs(), shell.fonts.close())
}

func (shell *graphiteShell) initialWindowSize(viewport render.Viewport) (int, int) {
	width := viewport.Width + graphiteRailWidth
	height := viewport.Height + graphiteChromeHeight
	if width < minimumContentWidth+graphiteRailWidth {
		width = minimumContentWidth + graphiteRailWidth
	}
	if height < minimumContentHeight+graphiteChromeHeight {
		height = minimumContentHeight + graphiteChromeHeight
	}
	shell.width = width
	shell.height = height
	return width, height
}

func (shell *graphiteShell) layout() shellLayout {
	return graphiteLayout(shell.width, shell.height, shell.inspectorOpen, len(shell.tabs), shell.opener != nil && len(shell.tabs) < maximumGraphiteTabs)
}

func graphiteLayout(width, height int, inspectorOpen bool, tabCount int, showNewTab bool) shellLayout {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	contentRight := width - graphiteRailWidth
	if contentRight < 1 {
		contentRight = 1
	}
	chromeBottom := graphiteChromeHeight
	if chromeBottom >= height {
		chromeBottom = height - 1
		if chromeBottom < 0 {
			chromeBottom = 0
		}
	}
	addressLeft := 154
	addressRight := contentRight - 16
	if addressRight < addressLeft+80 {
		addressLeft = 112
		addressRight = contentRight - 8
	}
	layout := shellLayout{
		window:         image.Rect(0, 0, width, height),
		back:           image.Rect(12, 46, 42, 76),
		forward:        image.Rect(48, 46, 78, 76),
		reload:         image.Rect(84, 46, 114, 76),
		address:        image.Rect(addressLeft, 44, addressRight, 78),
		content:        image.Rect(0, chromeBottom, contentRight, height),
		rail:           image.Rect(contentRight, 0, width, height),
		railDisclosure: image.Rect(contentRight, 208, width, 252),
	}
	if tabCount < 1 {
		tabCount = 1
	}
	tabStart := 10
	tabGap := 4
	newTabWidth := 0
	if showNewTab {
		newTabWidth = 38
	}
	available := contentRight - tabStart - 10 - newTabWidth - tabGap*(tabCount-1)
	if available < tabCount {
		available = tabCount
	}
	tabWidth := available / tabCount
	if tabWidth > 272 {
		tabWidth = 272
	}
	layout.tabs = make([]shellTabLayout, tabCount)
	x := tabStart
	for index := 0; index < tabCount; index++ {
		bodyLeft := minInt(x, contentRight)
		bodyRight := minInt(x+tabWidth, contentRight)
		if bodyRight < bodyLeft {
			bodyRight = bodyLeft
		}
		body := image.Rect(bodyLeft, 5, bodyRight, 34)
		close := image.Rectangle{}
		if body.Dx() >= 28 {
			close = image.Rect(body.Max.X-24, 9, body.Max.X-2, 32)
		}
		layout.tabs[index] = shellTabLayout{body: body, close: close}
		x += tabWidth + tabGap
	}
	if showNewTab && x < contentRight {
		layout.newTab = image.Rect(x+2, 7, minInt(x+32, contentRight-4), 33)
	}
	if inspectorOpen {
		panelWidth := 280
		if panelWidth > contentRight-24 {
			panelWidth = maxInt(120, contentRight-24)
		}
		layout.inspector = image.Rect(contentRight-panelWidth, chromeBottom, contentRight, height)
	}
	return layout
}

func (shell *graphiteShell) syncPage(page *browser.Page) {
	if shell == nil || page == nil {
		return
	}
	snapshot := page.Navigation()
	loading := snapshot.ID != 0 && !navigationTerminal(snapshot.State)
	changed := shell.loading != loading
	shell.loading = loading
	canGoBack := page.CanGoBack()
	canGoForward := page.CanGoForward()
	if shell.canGoBack != canGoBack || shell.canGoForward != canGoForward {
		shell.canGoBack = canGoBack
		shell.canGoForward = canGoForward
		changed = true
	}
	navigationView := shellNavigationLabel(snapshot)
	if navigationView != shell.navigationView {
		shell.navigationView = navigationView
		changed = true
	}
	if shell.navigation != 0 && snapshot.ID == shell.navigation && navigationTerminal(snapshot.State) {
		shell.navigation = 0
		if snapshot.Err != nil {
			shell.navigationErr = snapshot.Err.Error()
		} else {
			shell.navigationErr = ""
		}
		changed = true
	}
	location := page.URL()
	current := ""
	if location != nil {
		current = location.String()
	}
	if current != shell.lastPageURL {
		shell.lastPageURL = current
		if !shell.addressFocused {
			shell.address = current
		}
		changed = true
	}
	if changed {
		shell.revision++
	}
}

func navigationTerminal(state browser.NavigationState) bool {
	return state == browser.NavigationComplete || state == browser.NavigationFailed ||
		state == browser.NavigationCanceled
}

func (shell *graphiteShell) handleEvent(
	ctx context.Context,
	page *browser.Page,
	event Event,
	state *inputState,
) (handled bool, translated Event, closeWindow bool, err error) {
	if shell == nil {
		return false, event, false, nil
	}
	layout := shell.layout()
	translated = event
	if event.Kind == EventResize {
		if event.Width <= 0 || event.Height <= 0 {
			return true, translated, false, nil
		}
		shell.width = event.Width
		shell.height = event.Height
		shell.revision++
		content := shell.layout().content
		width := maxInt(1, content.Dx())
		height := maxInt(1, content.Dy())
		var resizeErr error
		for _, tabPage := range shell.pages() {
			_, queueErr := tabPage.QueueViewportResize(render.Viewport{Width: width, Height: height})
			resizeErr = errors.Join(resizeErr, queueErr)
		}
		return true, translated, false, resizeErr
	}

	if event.Kind == EventKeyUp && event.Code != "" && shell.suppressedKeys[event.Code] {
		delete(shell.suppressedKeys, event.Code)
		return true, translated, false, nil
	}
	if shell.addressFocused {
		switch event.Kind {
		case EventTextInput:
			return true, translated, false, shell.insertAddressText(event.Text)
		case EventCompositionStart, EventCompositionUpdate:
			return true, translated, false, nil
		case EventCompositionEnd:
			return true, translated, false, shell.insertAddressText(event.Text)
		}
	}
	if event.Kind == EventKeyDown {
		if event.Modifiers.Meta && strings.EqualFold(event.Key, "t") {
			shell.suppress(event)
			return true, translated, false, shell.openTab(ctx)
		}
		if event.Modifiers.Meta && strings.EqualFold(event.Key, "w") {
			shell.suppress(event)
			last, closeErr := shell.closeTab(shell.activeTab)
			return true, translated, last, closeErr
		}
		if event.Modifiers.Ctrl && !event.Modifiers.Shift && event.Key == "Tab" || event.Modifiers.Meta && event.Modifiers.Shift && event.Key == "]" {
			shell.cycleTab(1)
			shell.suppress(event)
			return true, translated, false, nil
		}
		if event.Modifiers.Ctrl && event.Modifiers.Shift && event.Key == "Tab" || event.Modifiers.Meta && event.Modifiers.Shift && event.Key == "[" {
			shell.cycleTab(-1)
			shell.suppress(event)
			return true, translated, false, nil
		}
		if event.Modifiers.Meta && len(event.Key) == 1 && event.Key[0] >= '1' && event.Key[0] <= '8' {
			shell.switchTab(int(event.Key[0] - '1'))
			shell.suppress(event)
			return true, translated, false, nil
		}
		if (event.Modifiers.Meta && event.Key == "[") || (event.Modifiers.Alt && event.Key == "ArrowLeft") {
			shell.suppress(event)
			return true, translated, false, shell.navigateHistory(ctx, page, -1)
		}
		if (event.Modifiers.Meta && event.Key == "]") || (event.Modifiers.Alt && event.Key == "ArrowRight") {
			shell.suppress(event)
			return true, translated, false, shell.navigateHistory(ctx, page, 1)
		}
		if event.Modifiers.Meta && strings.EqualFold(event.Key, "l") {
			shell.focusAddress()
			shell.suppress(event)
			return true, translated, false, nil
		}
		if event.Modifiers.Meta && strings.EqualFold(event.Key, "r") {
			shell.suppress(event)
			return true, translated, false, shell.reload(ctx, page)
		}
		if shell.addressFocused {
			shell.suppress(event)
			return true, translated, false, shell.handleAddressKey(ctx, page, event, state)
		}
	}

	if isPointerEvent(event.Kind) {
		point := image.Pt(int(event.X), int(event.Y))
		if handled, scrollbarErr := shell.handleScrollbarEvent(page, layout, point, event.Kind); handled || scrollbarErr != nil {
			return handled, translated, false, scrollbarErr
		}
		if point.In(layout.content) && !point.In(layout.inspector) {
			if event.Kind == EventPointerDown && shell.addressFocused {
				shell.addressFocused = false
				shell.selectAll = false
				shell.revision++
			}
			translated.X -= float64(layout.content.Min.X)
			translated.Y -= float64(layout.content.Min.Y)
			return false, translated, false, nil
		}
		if event.Kind == EventPointerDown || event.Kind == EventPointerUp {
			state.pressed = false
			state.pressedTarget = browser.NodeHandle{}
			state.pressedButton = 0
		}
		if event.Kind == EventPointerDown {
			for index, tab := range layout.tabs {
				if !tab.close.Empty() && point.In(tab.close) {
					last, closeErr := shell.closeTab(index)
					return true, translated, last, closeErr
				}
				if point.In(tab.body) {
					shell.switchTab(index)
					return true, translated, false, nil
				}
			}
			if !layout.newTab.Empty() && point.In(layout.newTab) {
				return true, translated, false, shell.openTab(ctx)
			}
			if point.In(layout.address) {
				shell.focusAddress()
				return true, translated, false, nil
			}
			if point.In(layout.back) {
				return true, translated, false, shell.navigateHistory(ctx, page, -1)
			}
			if point.In(layout.forward) {
				return true, translated, false, shell.navigateHistory(ctx, page, 1)
			}
			if point.In(layout.reload) {
				return true, translated, false, shell.reload(ctx, page)
			}
			if point.In(layout.railDisclosure) {
				shell.inspectorOpen = !shell.inspectorOpen
				shell.revision++
				return true, translated, false, nil
			}
		}
		return true, translated, false, nil
	}

	if event.Kind == EventScroll && shell.addressFocused {
		return true, translated, false, nil
	}
	return false, translated, false, nil
}

func (shell *graphiteShell) insertAddressText(value string) error {
	if value == "" {
		return nil
	}
	if shell.selectAll {
		shell.address = value
		shell.selectAll = false
	} else {
		shell.address += value
	}
	shell.revision++
	return nil
}

func (shell *graphiteShell) handleScrollbarEvent(page *browser.Page, layout shellLayout, point image.Point, kind EventKind) (bool, error) {
	if shell == nil || page == nil {
		return false, nil
	}
	if kind == EventPointerUp && shell.scrollbarDrag.axis != shellScrollbarNone {
		shell.scrollbarDrag = shellScrollbarDrag{}
		return true, nil
	}
	if shell.scrollbarDrag.axis == shellScrollbarNone && kind != EventPointerDown {
		return false, nil
	}
	visibleContent := layout.content
	if !layout.inspector.Empty() && layout.inspector.Min.X < visibleContent.Max.X {
		visibleContent.Max.X = layout.inspector.Min.X
	}
	if shell.scrollbarDrag.axis == shellScrollbarNone &&
		point.X < visibleContent.Max.X-graphiteScrollbarGap-graphiteScrollbarSize &&
		point.Y < visibleContent.Max.Y-graphiteScrollbarGap-graphiteScrollbarSize {
		return false, nil
	}
	geometry, err := page.ViewportGeometry()
	if err != nil {
		return false, err
	}
	bars := graphiteScrollbars(visibleContent, geometry)
	scroll := func(axis shellScrollbarAxis, coordinate, offset int) error {
		track := bars.horizontalTrack
		thumb := bars.horizontalThumb
		maximum := bars.maximumX
		currentX, currentY := geometry.ScrollX, geometry.ScrollY
		if axis == shellScrollbarVertical {
			track = bars.verticalTrack
			thumb = bars.verticalThumb
			maximum = bars.maximumY
		}
		travel := track.Dx() - thumb.Dx()
		trackStart := track.Min.X
		if axis == shellScrollbarVertical {
			travel = track.Dy() - thumb.Dy()
			trackStart = track.Min.Y
		}
		if travel <= 0 || maximum <= 0 {
			return nil
		}
		position := float64(coordinate-trackStart-offset) / float64(travel)
		position = math.Max(0, math.Min(1, position))
		if axis == shellScrollbarHorizontal {
			currentX = position * maximum
		} else {
			currentY = position * maximum
		}
		_, queueErr := page.QueueViewportScrollTo(currentX, currentY)
		return queueErr
	}
	if shell.scrollbarDrag.axis != shellScrollbarNone && kind == EventPointerMove {
		coordinate := point.X
		if shell.scrollbarDrag.axis == shellScrollbarVertical {
			coordinate = point.Y
		}
		return true, scroll(shell.scrollbarDrag.axis, coordinate, shell.scrollbarDrag.pointerOffset)
	}
	if kind != EventPointerDown {
		return false, nil
	}
	startDrag := func(axis shellScrollbarAxis, track, thumb image.Rectangle, coordinate int) (bool, error) {
		if track.Empty() || !point.In(track) {
			return false, nil
		}
		offset := thumb.Dx() / 2
		if axis == shellScrollbarVertical {
			offset = thumb.Dy() / 2
		}
		if point.In(thumb) {
			if axis == shellScrollbarHorizontal {
				offset = point.X - thumb.Min.X
			} else {
				offset = point.Y - thumb.Min.Y
			}
		}
		shell.scrollbarDrag = shellScrollbarDrag{axis: axis, pointerOffset: offset}
		return true, scroll(axis, coordinate, offset)
	}
	if handled, dragErr := startDrag(shellScrollbarVertical, bars.verticalTrack, bars.verticalThumb, point.Y); handled || dragErr != nil {
		return handled, dragErr
	}
	return startDrag(shellScrollbarHorizontal, bars.horizontalTrack, bars.horizontalThumb, point.X)
}

func graphiteScrollbars(content image.Rectangle, geometry browser.DOMViewportGeometry) shellScrollbars {
	maximumX := math.Max(0, geometry.ScrollWidth-geometry.InnerWidth)
	maximumY := math.Max(0, geometry.ScrollHeight-geometry.InnerHeight)
	result := shellScrollbars{maximumX: maximumX, maximumY: maximumY}
	hasHorizontal := maximumX > 0 && content.Dx() >= graphiteScrollbarMin+graphiteScrollbarGap*2
	hasVertical := maximumY > 0 && content.Dy() >= graphiteScrollbarMin+graphiteScrollbarGap*2
	if hasVertical {
		bottom := content.Max.Y - graphiteScrollbarGap
		if hasHorizontal {
			bottom -= graphiteScrollbarSize + graphiteScrollbarGap
		}
		result.verticalTrack = image.Rect(
			content.Max.X-graphiteScrollbarGap-graphiteScrollbarSize,
			content.Min.Y+graphiteScrollbarGap,
			content.Max.X-graphiteScrollbarGap,
			bottom,
		)
		result.verticalThumb = scrollbarThumb(result.verticalTrack, geometry.InnerHeight, geometry.ScrollHeight, geometry.ScrollY, true)
	}
	if hasHorizontal {
		right := content.Max.X - graphiteScrollbarGap
		if hasVertical {
			right -= graphiteScrollbarSize + graphiteScrollbarGap
		}
		result.horizontalTrack = image.Rect(
			content.Min.X+graphiteScrollbarGap,
			content.Max.Y-graphiteScrollbarGap-graphiteScrollbarSize,
			right,
			content.Max.Y-graphiteScrollbarGap,
		)
		result.horizontalThumb = scrollbarThumb(result.horizontalTrack, geometry.InnerWidth, geometry.ScrollWidth, geometry.ScrollX, false)
	}
	return result
}

func scrollbarThumb(track image.Rectangle, viewport, extent, offset float64, vertical bool) image.Rectangle {
	trackLength := track.Dx()
	if vertical {
		trackLength = track.Dy()
	}
	if trackLength <= 0 || viewport <= 0 || extent <= viewport {
		return image.Rectangle{}
	}
	thumbLength := int(math.Round(float64(trackLength) * viewport / extent))
	thumbLength = maxInt(graphiteScrollbarMin, minInt(trackLength, thumbLength))
	maximum := extent - viewport
	position := int(math.Round(float64(trackLength-thumbLength) * math.Max(0, math.Min(maximum, offset)) / maximum))
	if vertical {
		return image.Rect(track.Min.X, track.Min.Y+position, track.Max.X, track.Min.Y+position+thumbLength)
	}
	return image.Rect(track.Min.X+position, track.Min.Y, track.Min.X+position+thumbLength, track.Max.Y)
}

func (shell *graphiteShell) focusAddress() {
	shell.addressFocused = true
	shell.selectAll = true
	shell.navigationErr = ""
	shell.revision++
}

func (shell *graphiteShell) suppress(event Event) {
	if event.Code != "" {
		shell.suppressedKeys[event.Code] = true
	}
}

func (shell *graphiteShell) handleAddressKey(
	ctx context.Context,
	page *browser.Page,
	event Event,
	state *inputState,
) error {
	switch event.Key {
	case "Enter":
		shell.addressFocused = false
		shell.selectAll = false
		*state = inputState{}
		return shell.navigate(ctx, page, shell.address)
	case "Escape":
		shell.address = shell.lastPageURL
		shell.addressFocused = false
		shell.selectAll = false
		shell.navigationErr = ""
		shell.revision++
		return nil
	case "Backspace", "Delete":
		if shell.selectAll {
			shell.address = ""
			shell.selectAll = false
		} else if shell.address != "" {
			_, size := utf8.DecodeLastRuneInString(shell.address)
			shell.address = shell.address[:len(shell.address)-size]
		}
		shell.revision++
		return nil
	}
	if event.Text == "" || event.Modifiers.Meta || event.Modifiers.Ctrl {
		return nil
	}
	return shell.insertAddressText(event.Text)
}

func (shell *graphiteShell) navigate(ctx context.Context, page *browser.Page, raw string) error {
	raw = normalizeShellAddress(raw)
	if raw == "" {
		shell.navigationErr = "Enter an HTTP or HTTPS address"
		shell.addressFocused = true
		shell.revision++
		return nil
	}
	navigation, err := page.Navigate(ctx, raw, shell.loader)
	if err != nil {
		shell.navigationErr = err.Error()
		shell.addressFocused = true
		shell.revision++
		return nil
	}
	shell.address = raw
	shell.navigation = navigation
	shell.navigationErr = ""
	shell.loading = true
	shell.revision++
	return nil
}

func (shell *graphiteShell) navigateHistory(ctx context.Context, page *browser.Page, delta int) error {
	if delta < 0 && !shell.canGoBack || delta > 0 && !shell.canGoForward {
		return nil
	}
	navigation, err := page.Go(ctx, delta, shell.loader)
	return shell.trackNavigation(navigation, err, false)
}

func (shell *graphiteShell) reload(ctx context.Context, page *browser.Page) error {
	navigation, err := page.Reload(ctx, shell.loader)
	return shell.trackNavigation(navigation, err, false)
}

func (shell *graphiteShell) trackNavigation(navigation browser.NavigationID, navigationErr error, focusAddress bool) error {
	if navigationErr != nil {
		shell.navigationErr = navigationErr.Error()
		if focusAddress {
			shell.addressFocused = true
		}
		shell.revision++
		return nil
	}
	shell.navigation = navigation
	shell.navigationErr = ""
	shell.loading = true
	shell.revision++
	return nil
}

func normalizeShellAddress(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err == nil && parsed.IsAbs() {
		return raw
	}
	return "https://" + raw
}

func isPointerEvent(kind EventKind) bool {
	return kind == EventPointerMove || kind == EventPointerDown || kind == EventPointerUp
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func shellTabTitle(page *browser.Page) string {
	if page == nil {
		return "New Tab"
	}
	if title := page.Metadata().Title; title != "" {
		return title
	}
	if page.URL() == nil {
		return "New Tab"
	}
	location := page.URL()
	if location.Hostname() != "" {
		return location.Hostname()
	}
	if location.String() != "" {
		return location.String()
	}
	return "Gossamer"
}

func shellNavigationLabel(snapshot browser.NavigationSnapshot) string {
	switch snapshot.State {
	case browser.NavigationLoadingDocument:
		return "Loading document"
	case browser.NavigationLoadingResources:
		return fmt.Sprintf("Resources %d/%d", snapshot.ResourcesTotal-snapshot.ResourcesPending, snapshot.ResourcesTotal)
	case browser.NavigationLoadingScripts:
		return fmt.Sprintf("Scripts %d/%d", snapshot.ScriptsTotal-snapshot.ScriptsPending, snapshot.ScriptsTotal)
	case browser.NavigationRendering:
		return "Rendering"
	case browser.NavigationComplete:
		return "Complete"
	case browser.NavigationFailed:
		return "Failed"
	case browser.NavigationCanceled:
		return "Canceled"
	default:
		return "Idle"
	}
}
