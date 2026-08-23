package window

import (
	"context"
	"errors"
	"image"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/browser/fake"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
)

func TestRunBrowserPresentsInitialFrameBeforeQueuedPageWork(t *testing.T) {
	t.Parallel()

	browserRuntime, err := browser.NewWithEngine(fake.New())
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	page, err := browserRuntime.NewBlankPage(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	presented := make(chan struct{})
	backend := &startupOrderBackend{MemoryBackend: NewMemoryBackend(Event{Kind: EventClose}), presented: presented}
	taskRan := false
	if _, err := page.Realm.EnqueueTask(func(*browserruntime.TaskContext) error {
		select {
		case <-presented:
			taskRan = true
			return nil
		default:
			return errors.New("page task ran before the first native presentation")
		}
	}); err != nil {
		t.Fatal(err)
	}

	var milestones []StartupMilestone
	err = RunBrowser(context.Background(), page, backend, ShellConfig{Startup: func(_ *browser.Page, observation StartupObservation) error {
		milestones = append(milestones, observation.Milestone)
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !taskRan {
		t.Fatal("queued page task did not run")
	}
	if len(backend.Frames()) == 0 {
		t.Fatal("native backend received no initial frame")
	}
	if want := []StartupMilestone{StartupWindowOpen, StartupFirstPresent}; !reflect.DeepEqual(milestones, want) {
		t.Fatalf("startup milestones = %v, want %v", milestones, want)
	}
}

func TestStartupTrackerEmitsOrderedMilestonesWhenNavigationSkipsPhases(t *testing.T) {
	t.Parallel()

	var observations []StartupObservation
	tracker := newStartupTracker(time.Now(), nil, func(_ *browser.Page, observation StartupObservation) error {
		observations = append(observations, observation)
		return nil
	})
	navigation := browser.NavigationSnapshot{ID: 7, State: browser.NavigationLoadingDocument}
	if err := tracker.observeNavigation(navigation); err != nil {
		t.Fatal(err)
	}
	navigation.State = browser.NavigationLoadingScripts
	if err := tracker.observeNavigation(navigation); err != nil {
		t.Fatal(err)
	}
	navigation.State = browser.NavigationComplete
	if err := tracker.observeNavigation(navigation); err != nil {
		t.Fatal(err)
	}
	if err := tracker.observe(StartupFirstContentPresent, navigation); err != nil {
		t.Fatal(err)
	}
	if err := tracker.observeNavigation(navigation); err != nil {
		t.Fatal(err)
	}

	got := make([]StartupMilestone, 0, len(observations))
	for index, observation := range observations {
		got = append(got, observation.Milestone)
		if observation.Elapsed < 0 || observation.At.IsZero() {
			t.Fatalf("observation %d timing = %#v", index, observation)
		}
		if index > 0 && observation.At.Before(observations[index-1].At) {
			t.Fatalf("observation times are not monotonic: %#v", observations)
		}
	}
	want := []StartupMilestone{
		StartupNavigationStarted, StartupDocumentReady, StartupResourcesReady,
		StartupScriptsReady, StartupNavigationComplete, StartupFirstContentPresent,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("startup milestones = %v, want %v", got, want)
	}
}

func TestRunBrowserDrivesInitialNavigationAfterFirstPresentation(t *testing.T) {
	t.Parallel()

	browserRuntime, err := browser.NewWithEngine(fake.New())
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	page, err := browserRuntime.NewBlankPage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	client := shellTestLoader{document: `<html><head><title>Ready</title></head><body>content</body></html>`}
	page.SetNavigationLoader(client)
	if _, err := page.Navigate(context.Background(), "https://startup.gossamer.test/", client); err != nil {
		t.Fatal(err)
	}

	backend := &navigationWaitBackend{MemoryBackend: NewMemoryBackend(), page: page}
	var milestones []StartupMilestone
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := RunBrowser(ctx, page, backend, ShellConfig{Loader: client, Startup: func(_ *browser.Page, observation StartupObservation) error {
		milestones = append(milestones, observation.Milestone)
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	if state := page.Navigation().State; state != browser.NavigationComplete {
		t.Fatalf("navigation state = %v, want complete", state)
	}
	if len(backend.Frames()) < 2 {
		t.Fatalf("presented frames = %d, want blank startup and completed content", len(backend.Frames()))
	}
	want := []StartupMilestone{
		StartupNavigationStarted, StartupWindowOpen, StartupFirstPresent,
		StartupDocumentReady, StartupResourcesReady, StartupScriptsReady,
		StartupNavigationComplete, StartupFirstContentPresent,
	}
	if !reflect.DeepEqual(milestones, want) {
		t.Fatalf("startup milestones = %v, want %v", milestones, want)
	}
}

type startupOrderBackend struct {
	*MemoryBackend
	presented chan struct{}
	once      sync.Once
}

func (backend *startupOrderBackend) Present(frame *image.RGBA) error {
	if err := backend.MemoryBackend.Present(frame); err != nil {
		return err
	}
	backend.once.Do(func() { close(backend.presented) })
	return nil
}

type navigationWaitBackend struct {
	*MemoryBackend
	page *browser.Page
}

func (backend *navigationWaitBackend) NextEvent(ctx context.Context) (Event, error) {
	snapshot := backend.page.Navigation()
	if navigationTerminal(snapshot.State) {
		return Event{Kind: EventClose}, nil
	}
	timer := time.NewTimer(time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return Event{}, ctx.Err()
	case <-timer.C:
		return Event{Kind: EventNone}, nil
	}
}
