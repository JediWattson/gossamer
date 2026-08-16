package window

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/JediWattson/gossamer/internal/browser"
)

type shellSecurityState uint8

const (
	shellSecurityNeutral shellSecurityState = iota
	shellSecuritySecure
	shellSecurityInsecure
	shellSecurityError
)

func shellSecurityFor(page *browser.Page, navigationErr string) shellSecurityState {
	if navigationErr != "" {
		return shellSecurityError
	}
	if page == nil || page.URL() == nil {
		return shellSecurityNeutral
	}
	switch strings.ToLower(page.URL().Scheme) {
	case "https":
		return shellSecuritySecure
	case "http":
		return shellSecurityInsecure
	default:
		return shellSecurityNeutral
	}
}

// shellNavigationProgress maps the kernel's real navigation phases into a
// monotonic visual fraction. Resource and script phases use their exact
// pending totals; the shell does not invent network byte progress.
func shellNavigationProgress(snapshot browser.NavigationSnapshot) float64 {
	switch snapshot.State {
	case browser.NavigationLoadingDocument:
		return 0.12
	case browser.NavigationLoadingResources:
		return phaseProgress(0.20, 0.55, snapshot.ResourcesTotal, snapshot.ResourcesPending)
	case browser.NavigationLoadingScripts:
		return phaseProgress(0.58, 0.82, snapshot.ScriptsTotal, snapshot.ScriptsPending)
	case browser.NavigationRendering:
		return 0.92
	case browser.NavigationComplete:
		return 1
	default:
		return 0
	}
}

func phaseProgress(start, end float64, total, pending int) float64 {
	if total <= 0 {
		return end
	}
	complete := total - pending
	complete = maxInt(0, minInt(total, complete))
	return math.Max(start, math.Min(end, start+(end-start)*float64(complete)/float64(total)))
}

func (shell *graphiteShell) windowTitle() string {
	page := shell.activePage()
	title := shellTabTitle(page)
	if title == "" || title == "New Tab" {
		return "Gossamer"
	}
	return fmt.Sprintf("%s — Gossamer", title)
}

func (shell *graphiteShell) stopNavigation(page *browser.Page) error {
	if shell == nil || page == nil || !shell.loading {
		return nil
	}
	snapshot := page.Navigation()
	if snapshot.ID == 0 || navigationTerminal(snapshot.State) {
		shell.loading = false
		shell.navigation = 0
		shell.revision++
		return nil
	}
	if err := page.CancelNavigation(snapshot.ID); err != nil {
		return err
	}
	shell.loading = false
	shell.navigation = 0
	shell.navigationView = shellNavigationLabel(page.Navigation())
	shell.navigationErr = ""
	shell.revision++
	return nil
}

func (shell *graphiteShell) retryNavigation(ctx context.Context, page *browser.Page) error {
	if shell == nil || page == nil {
		return nil
	}
	return shell.navigate(ctx, page, shell.address)
}
