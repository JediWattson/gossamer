package window

import (
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
)

// StartupMilestone identifies one engine-neutral step between beginning the
// initial navigation and presenting its first completed frame.
type StartupMilestone string

const (
	StartupNavigationStarted   StartupMilestone = "navigation-started"
	StartupWindowOpen          StartupMilestone = "window-open"
	StartupFirstPresent        StartupMilestone = "first-present"
	StartupDocumentReady       StartupMilestone = "document-ready"
	StartupResourcesReady      StartupMilestone = "resources-ready"
	StartupScriptsReady        StartupMilestone = "scripts-ready"
	StartupNavigationComplete  StartupMilestone = "navigation-complete"
	StartupNavigationFailed    StartupMilestone = "navigation-failed"
	StartupNavigationCanceled  StartupMilestone = "navigation-canceled"
	StartupFirstContentPresent StartupMilestone = "first-content-present"
)

// StartupObservation is a copied view of a startup milestone. Elapsed is
// measured from entry into the native window session; At is available to
// embedders that need to align startup with another process timeline.
type StartupObservation struct {
	Milestone  StartupMilestone
	At         time.Time
	Elapsed    time.Duration
	Navigation browser.NavigationSnapshot
}

// StartupObserver receives each startup milestone at most once for the Page
// visible at startup. It runs on the window session thread after browser-owned
// work has reached a safe task boundary.
type StartupObserver func(*browser.Page, StartupObservation) error

type startupTracker struct {
	started  time.Time
	observer func(StartupObservation) error
	emitted  map[StartupMilestone]struct{}
}

func newStartupTracker(started time.Time, page *browser.Page, observer StartupObserver) *startupTracker {
	var observe func(StartupObservation) error
	if observer != nil {
		observe = func(observation StartupObservation) error { return observer(page, observation) }
	}
	return &startupTracker{started: started, observer: observe, emitted: make(map[StartupMilestone]struct{})}
}

func (tracker *startupTracker) observe(milestone StartupMilestone, navigation browser.NavigationSnapshot) error {
	if tracker == nil || tracker.observer == nil {
		return nil
	}
	if _, found := tracker.emitted[milestone]; found {
		return nil
	}
	now := time.Now()
	observation := StartupObservation{
		Milestone: milestone, At: now, Elapsed: now.Sub(tracker.started), Navigation: navigation,
	}
	if err := tracker.observer(observation); err != nil {
		return err
	}
	tracker.emitted[milestone] = struct{}{}
	return nil
}

func (tracker *startupTracker) observeNavigation(navigation browser.NavigationSnapshot) error {
	if navigation.ID == 0 {
		return nil
	}
	emit := func(milestones ...StartupMilestone) error {
		for _, milestone := range milestones {
			if err := tracker.observe(milestone, navigation); err != nil {
				return err
			}
		}
		return nil
	}
	switch navigation.State {
	case browser.NavigationLoadingDocument:
		return emit(StartupNavigationStarted)
	case browser.NavigationLoadingResources:
		return emit(StartupNavigationStarted, StartupDocumentReady)
	case browser.NavigationLoadingScripts:
		return emit(StartupNavigationStarted, StartupDocumentReady, StartupResourcesReady)
	case browser.NavigationRendering:
		return emit(StartupNavigationStarted, StartupDocumentReady, StartupResourcesReady, StartupScriptsReady)
	case browser.NavigationComplete:
		return emit(StartupNavigationStarted, StartupDocumentReady, StartupResourcesReady, StartupScriptsReady, StartupNavigationComplete)
	case browser.NavigationFailed:
		return emit(StartupNavigationStarted, StartupNavigationFailed)
	case browser.NavigationCanceled:
		return emit(StartupNavigationStarted, StartupNavigationCanceled)
	default:
		return nil
	}
}
