package window

import (
	"context"
	"fmt"
	"image"
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
	minimumContentWidth   = 160
	minimumContentHeight  = 120
)

// ShellConfig configures Gossamer's browser-owned Graphite chrome. Loader is
// reused for address-bar and reload navigation; nil keeps Page's default HTTP
// loader behavior.
type ShellConfig struct {
	Title  string
	Loader browser.DocumentLoader
}

type shellLayout struct {
	window         image.Rectangle
	tab            image.Rectangle
	tabClose       image.Rectangle
	back           image.Rectangle
	forward        image.Rectangle
	reload         image.Rectangle
	address        image.Rectangle
	content        image.Rectangle
	rail           image.Rectangle
	railDisclosure image.Rectangle
	inspector      image.Rectangle
}

type graphiteShell struct {
	loader browser.DocumentLoader
	fonts  *shellFontBook

	width  int
	height int

	address        string
	lastPageURL    string
	addressFocused bool
	selectAll      bool
	inspectorOpen  bool
	loading        bool
	navigation     browser.NavigationID
	navigationView string
	navigationErr  string
	suppressedKeys map[string]bool
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
	return &graphiteShell{
		loader:         config.Loader,
		fonts:          fonts,
		address:        location,
		lastPageURL:    location,
		navigationView: shellNavigationLabel(page.Navigation()),
		suppressedKeys: make(map[string]bool),
		revision:       1,
	}, nil
}

func (shell *graphiteShell) close() error {
	if shell == nil || shell.fonts == nil {
		return nil
	}
	return shell.fonts.close()
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
	return graphiteLayout(shell.width, shell.height, shell.inspectorOpen)
}

func graphiteLayout(width, height int, inspectorOpen bool) shellLayout {
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
		tab:            image.Rect(10, 5, minInt(282, contentRight-52), 34),
		tabClose:       image.Rect(minInt(252, contentRight-82), 10, minInt(276, contentRight-58), 31),
		back:           image.Rect(12, 46, 42, 76),
		forward:        image.Rect(48, 46, 78, 76),
		reload:         image.Rect(84, 46, 114, 76),
		address:        image.Rect(addressLeft, 44, addressRight, 78),
		content:        image.Rect(0, chromeBottom, contentRight, height),
		rail:           image.Rect(contentRight, 0, width, height),
		railDisclosure: image.Rect(contentRight, 208, width, 252),
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
		_, resizeErr := page.QueueViewportResize(render.Viewport{Width: width, Height: height})
		return true, translated, false, resizeErr
	}

	if event.Kind == EventKeyUp && event.Code != "" && shell.suppressedKeys[event.Code] {
		delete(shell.suppressedKeys, event.Code)
		return true, translated, false, nil
	}
	if event.Kind == EventKeyDown {
		if event.Modifiers.Meta && strings.EqualFold(event.Key, "l") {
			shell.focusAddress()
			shell.suppress(event)
			return true, translated, false, nil
		}
		if event.Modifiers.Meta && strings.EqualFold(event.Key, "r") {
			shell.suppress(event)
			return true, translated, false, shell.navigate(ctx, page, shell.lastPageURL)
		}
		if shell.addressFocused {
			shell.suppress(event)
			return true, translated, false, shell.handleAddressKey(ctx, page, event, state)
		}
	}

	if isPointerEvent(event.Kind) {
		point := image.Pt(int(event.X), int(event.Y))
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
			if point.In(layout.tabClose) {
				return true, translated, true, nil
			}
			if point.In(layout.address) {
				shell.focusAddress()
				return true, translated, false, nil
			}
			if point.In(layout.reload) {
				return true, translated, false, shell.navigate(ctx, page, shell.lastPageURL)
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
	if shell.selectAll {
		shell.address = event.Text
		shell.selectAll = false
	} else {
		shell.address += event.Text
	}
	shell.revision++
	return nil
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
	if page == nil || page.URL() == nil {
		return "Gossamer"
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
