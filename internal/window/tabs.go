package window

import (
	"context"
	"errors"
	"fmt"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/render"
)

const maximumGraphiteTabs = 8
const maximumClosedGraphiteTabs = 10

type shellTabDrag struct {
	index    int
	startX   int
	dragging bool
	active   bool
}

type closedGraphiteTab struct {
	url string
}

// PageOpener creates one already-rendered blank Page for a new Graphite tab.
// The returned Page must belong to the same Browser as the initial Page so its
// Realm, document generations, and teardown remain under one browser owner.
type PageOpener func(context.Context) (*browser.Page, error)

type shellPageState struct {
	address        string
	lastPageURL    string
	addressFocused bool
	selectAll      bool
	loading        bool
	canGoBack      bool
	canGoForward   bool
	navigation     browser.NavigationID
	navigationView string
	navigationErr  string
}

type graphiteTab struct {
	page  *browser.Page
	state shellPageState
	input inputState
	owned bool
}

func newShellPageState(page *browser.Page, focusAddress bool) shellPageState {
	location := ""
	if page != nil && page.URL() != nil {
		location = page.URL().String()
	}
	return shellPageState{
		address:        location,
		lastPageURL:    location,
		addressFocused: focusAddress,
		selectAll:      focusAddress,
		canGoBack:      page.CanGoBack(),
		canGoForward:   page.CanGoForward(),
		navigationView: shellNavigationLabel(page.Navigation()),
	}
}

func (shell *graphiteShell) capturePageState() shellPageState {
	return shellPageState{
		address:        shell.address,
		lastPageURL:    shell.lastPageURL,
		addressFocused: shell.addressFocused,
		selectAll:      shell.selectAll,
		loading:        shell.loading,
		canGoBack:      shell.canGoBack,
		canGoForward:   shell.canGoForward,
		navigation:     shell.navigation,
		navigationView: shell.navigationView,
		navigationErr:  shell.navigationErr,
	}
}

func (shell *graphiteShell) applyPageState(state shellPageState) {
	shell.address = state.address
	shell.lastPageURL = state.lastPageURL
	shell.addressFocused = state.addressFocused
	shell.selectAll = state.selectAll
	shell.loading = state.loading
	shell.canGoBack = state.canGoBack
	shell.canGoForward = state.canGoForward
	shell.navigation = state.navigation
	shell.navigationView = state.navigationView
	shell.navigationErr = state.navigationErr
}

func (shell *graphiteShell) saveActiveTab() {
	if shell == nil || shell.activeTab < 0 || shell.activeTab >= len(shell.tabs) {
		return
	}
	shell.tabs[shell.activeTab].state = shell.capturePageState()
}

func (shell *graphiteShell) activePage() *browser.Page {
	if shell == nil || shell.activeTab < 0 || shell.activeTab >= len(shell.tabs) {
		return nil
	}
	return shell.tabs[shell.activeTab].page
}

func (shell *graphiteShell) activeInputState() *inputState {
	if shell == nil || shell.activeTab < 0 || shell.activeTab >= len(shell.tabs) {
		return nil
	}
	return &shell.tabs[shell.activeTab].input
}

func (shell *graphiteShell) pages() []*browser.Page {
	if shell == nil {
		return nil
	}
	pages := make([]*browser.Page, 0, len(shell.tabs))
	for _, tab := range shell.tabs {
		if tab.page != nil {
			pages = append(pages, tab.page)
		}
	}
	return pages
}

func (shell *graphiteShell) switchTab(index int) {
	if shell == nil || index < 0 || index >= len(shell.tabs) || index == shell.activeTab {
		return
	}
	shell.saveActiveTab()
	shell.activeTab = index
	shell.applyPageState(shell.tabs[index].state)
	shell.tabOffset = minInt(shell.tabOffset, index)
	shell.revision++
}

func (shell *graphiteShell) moveTab(from, to int) {
	if shell == nil || from < 0 || from >= len(shell.tabs) || to < 0 || to >= len(shell.tabs) || from == to {
		return
	}
	moving := shell.tabs[from]
	if from < to {
		copy(shell.tabs[from:to], shell.tabs[from+1:to+1])
	} else {
		copy(shell.tabs[to+1:from+1], shell.tabs[to:from])
	}
	shell.tabs[to] = moving
	if shell.activeTab == from {
		shell.activeTab = to
	} else if from < shell.activeTab && to >= shell.activeTab {
		shell.activeTab--
	} else if from > shell.activeTab && to <= shell.activeTab {
		shell.activeTab++
	}
	shell.tabDrag.index = to
	shell.hoveredTab = to
	shell.revision++
}

func (shell *graphiteShell) cycleTab(delta int) {
	if shell == nil || len(shell.tabs) < 2 || delta == 0 {
		return
	}
	next := (shell.activeTab + delta) % len(shell.tabs)
	if next < 0 {
		next += len(shell.tabs)
	}
	shell.switchTab(next)
}

func (shell *graphiteShell) openTab(ctx context.Context) error {
	if shell == nil || shell.opener == nil || len(shell.tabs) >= maximumGraphiteTabs {
		return nil
	}
	page, err := shell.opener(ctx)
	if err != nil {
		shell.navigationErr = err.Error()
		shell.revision++
		return nil
	}
	if page == nil || page.Realm == nil || page.Frame() == nil {
		if page != nil {
			_ = page.Close()
		}
		shell.navigationErr = "New tab did not provide a rendered Page"
		shell.revision++
		return nil
	}
	layout := shell.layout()
	if _, err := page.QueueViewportResize(render.Viewport{
		Width: maxInt(1, layout.content.Dx()), Height: maxInt(1, layout.content.Dy()),
	}); err != nil {
		return errors.Join(err, page.Close())
	}
	shell.saveActiveTab()
	state := newShellPageState(page, true)
	shell.tabs = append(shell.tabs, graphiteTab{page: page, state: state, owned: true})
	shell.activeTab = len(shell.tabs) - 1
	shell.applyPageState(state)
	shell.revision++
	return nil
}

func (shell *graphiteShell) closeTab(index int) (last bool, err error) {
	if shell == nil || index < 0 || index >= len(shell.tabs) {
		return false, nil
	}
	shell.saveActiveTab()
	closing := shell.tabs[index]
	closedURL := ""
	if closing.page != nil && closing.page.URL() != nil {
		closedURL = closing.page.URL().String()
	}
	if closedURL != "" {
		shell.closedTabs = append(shell.closedTabs, closedGraphiteTab{url: closedURL})
		if len(shell.closedTabs) > maximumClosedGraphiteTabs {
			shell.closedTabs = append([]closedGraphiteTab(nil), shell.closedTabs[len(shell.closedTabs)-maximumClosedGraphiteTabs:]...)
		}
	}
	shell.tabs = append(shell.tabs[:index], shell.tabs[index+1:]...)
	if closing.page != nil {
		err = closing.page.Close()
	}
	if len(shell.tabs) == 0 {
		shell.activeTab = -1
		shell.revision++
		return true, err
	}
	if index < shell.activeTab {
		shell.activeTab--
	} else if index == shell.activeTab && shell.activeTab >= len(shell.tabs) {
		shell.activeTab = len(shell.tabs) - 1
	}
	if shell.activeTab < 0 || shell.activeTab >= len(shell.tabs) {
		return false, errors.Join(err, fmt.Errorf("window: invalid active tab after close"))
	}
	shell.applyPageState(shell.tabs[shell.activeTab].state)
	shell.hoveredTab = -1
	shell.tabDrag = shellTabDrag{}
	shell.revision++
	return false, err
}

func (shell *graphiteShell) reopenClosedTab(ctx context.Context) error {
	if shell == nil || shell.opener == nil || len(shell.closedTabs) == 0 || len(shell.tabs) >= maximumGraphiteTabs {
		return nil
	}
	last := len(shell.closedTabs) - 1
	closed := shell.closedTabs[last]
	shell.closedTabs = shell.closedTabs[:last]
	if err := shell.openTab(ctx); err != nil {
		shell.closedTabs = append(shell.closedTabs, closed)
		return err
	}
	if closed.url == "" {
		return nil
	}
	return shell.navigate(ctx, shell.activePage(), closed.url)
}

func (shell *graphiteShell) closeOwnedTabs() error {
	if shell == nil {
		return nil
	}
	var result error
	for index := range shell.tabs {
		if shell.tabs[index].owned && shell.tabs[index].page != nil {
			result = errors.Join(result, shell.tabs[index].page.Close())
			shell.tabs[index].page = nil
		}
	}
	return result
}
