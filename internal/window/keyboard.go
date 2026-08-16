package window

import (
	"context"

	"github.com/JediWattson/gossamer/internal/browser"
)

func (shell *graphiteShell) chromeFocusOrder() []shellFocus {
	order := []shellFocus{
		shellFocusTab, shellFocusBack, shellFocusForward, shellFocusReload, shellFocusAddress,
	}
	if layout := shell.layout(); !layout.newTab.Empty() {
		order = append(order, shellFocusNewTab)
	}
	return append(order,
		shellFocusInspectorDOM, shellFocusInspectorStyles, shellFocusInspectorLayout,
		shellFocusInspectorNetwork, shellFocusInspectorMemory, shellFocusContent,
	)
}

func (shell *graphiteShell) focusChrome(reverse bool) {
	order := shell.chromeFocusOrder()
	if len(order) == 0 {
		return
	}
	index := -1
	for candidate, focus := range order {
		if focus == shell.chromeFocus {
			index = candidate
			break
		}
	}
	if reverse {
		if index < 0 {
			index = 0
		}
		index = (index - 1 + len(order)) % len(order)
	} else {
		index = (index + 1) % len(order)
	}
	shell.addressFocused = order[index] == shellFocusAddress
	if shell.addressFocused {
		shell.selectAll = true
	}
	shell.chromeFocus = order[index]
	shell.revision++
}

func (shell *graphiteShell) handleChromeFocusKey(ctx context.Context, page *browser.Page, event Event) (bool, error) {
	if event.Kind != EventKeyDown {
		return false, nil
	}
	switch event.Key {
	case "Escape":
		if shell.chromeFocus == shellFocusAddress && shell.addressFocused {
			return false, nil
		}
		shell.chromeFocus = shellFocusNone
		shell.addressFocused = false
		shell.selectAll = false
		if shell.inspectorOpen {
			shell.inspectorOpen = false
		}
		shell.revision++
		return true, nil
	case "Tab":
		shell.focusChrome(event.Modifiers.Shift)
		return true, nil
	case "ArrowLeft", "ArrowUp":
		if shell.chromeFocus == shellFocusTab {
			shell.cycleTab(-1)
		} else {
			shell.focusChrome(true)
		}
		return true, nil
	case "ArrowRight", "ArrowDown":
		if shell.chromeFocus == shellFocusTab {
			shell.cycleTab(1)
		} else {
			shell.focusChrome(false)
		}
		return true, nil
	case "Enter", " ", "Spacebar":
		if shell.chromeFocus == shellFocusAddress {
			return false, nil
		}
		return true, shell.activateChromeFocus(ctx, page)
	}
	return false, nil
}

func (shell *graphiteShell) activateChromeFocus(ctx context.Context, page *browser.Page) error {
	switch shell.chromeFocus {
	case shellFocusTab, shellFocusContent, shellFocusNone:
		return nil
	case shellFocusBack:
		return shell.navigateHistory(ctx, page, -1)
	case shellFocusForward:
		return shell.navigateHistory(ctx, page, 1)
	case shellFocusReload:
		if shell.loading {
			return shell.stopNavigation(page)
		}
		return shell.reload(ctx, page)
	case shellFocusAddress:
		shell.focusAddress()
		return nil
	case shellFocusNewTab:
		return shell.openTab(ctx)
	case shellFocusInspectorDOM, shellFocusInspectorStyles, shellFocusInspectorLayout, shellFocusInspectorNetwork, shellFocusInspectorMemory:
		shell.inspectorPanel = inspectorPanel(shell.chromeFocus - shellFocusInspectorDOM)
		shell.inspectorOpen = true
		shell.revision++
		return nil
	default:
		return nil
	}
}
