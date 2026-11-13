package window

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
)

const maximumTasksPerPump = 4096

// Run presents page and routes native input until the backend closes or the
// context is canceled. It serializes native UI access on the caller's OS
// thread while Page tasks continue to own all DOM, script, and frame changes.
func Run(ctx context.Context, page *browser.Page, backend Backend, title string) (result error) {
	return runSession(ctx, page, backend, Config{Title: title}, nil, nil, nil)
}

// RunBrowser presents page inside Gossamer's browser-owned Graphite shell.
// The backend still receives only copied pixels and value-only native events;
// chrome input is handled here before content coordinates cross into Page.
func RunBrowser(ctx context.Context, page *browser.Page, backend Backend, config ShellConfig) (result error) {
	shell, err := newGraphiteShell(page, config)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, shell.close()) }()
	if err := shell.loadSession(ctx); err != nil {
		return fmt.Errorf("window: load session: %w", err)
	}
	title := config.Title
	if title == "" {
		title = "Gossamer"
	}
	result = runSession(ctx, page, backend, Config{Title: title}, shell, config.Startup, config.FrameTiming)
	return errors.Join(result, shell.saveSession(context.WithoutCancel(ctx)))
}

func runSession(
	ctx context.Context,
	page *browser.Page,
	backend Backend,
	config Config,
	shell *graphiteShell,
	startupObserver StartupObserver,
	frameObserver FrameTimingObserver,
) (result error) {
	if page == nil || page.Realm == nil {
		return fmt.Errorf("window: nil page")
	}
	if backend == nil {
		return fmt.Errorf("window: nil backend")
	}
	frame := page.Frame()
	if frame == nil {
		return fmt.Errorf("window: page has no initial frame")
	}
	width := frame.Viewport.Width
	height := frame.Viewport.Height
	if shell != nil {
		width, height = shell.initialWindowSize(frame.Viewport)
		if err := shell.restoreSession(ctx); err != nil {
			return fmt.Errorf("window: restore session: %w", err)
		}
	}
	startupPage := page
	if shell != nil {
		startupPage = shell.activePage()
	}
	if startupPage == nil || startupPage.Frame() == nil {
		return fmt.Errorf("window: browser shell has no initial frame")
	}
	startupFrame := startupPage.Frame()
	startup := newStartupTracker(time.Now(), startupPage, startupObserver)
	if err := startup.observeNavigation(startupPage.Navigation()); err != nil {
		return fmt.Errorf("window: observe startup navigation: %w", err)
	}
	config.Width = width
	config.Height = height
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := backend.Open(config); err != nil {
		return err
	}
	defer func() { result = errors.Join(result, backend.Close()) }()
	deviceScale, err := backendDeviceScale(backend)
	if err != nil {
		return err
	}
	if err := startup.observe(StartupWindowOpen, startupPage.Navigation()); err != nil {
		return fmt.Errorf("window: observe window open: %w", err)
	}

	var presented *render.Frame
	var presentedShellRevision uint64
	var accessibilityRevision uint64
	var accessibilityFrame *render.Frame
	compositor := newSessionCompositor(nil)
	defer func() { result = errors.Join(result, compositor.close()) }()
	compositor.setDeviceScale(deviceScale)
	var frameSequence uint64
	var lastPresentedAt time.Time
	var pendingFrameWorkStarted time.Time
	observeFrame := func(timing FrameTiming) error {
		if frameObserver == nil {
			return nil
		}
		frameSequence++
		timing.Sequence = frameSequence
		if !lastPresentedAt.IsZero() {
			timing.SincePrevious = timing.PresentedAt.Sub(lastPresentedAt)
		}
		lastPresentedAt = timing.PresentedAt
		return frameObserver(timing)
	}
	if err := setPageDeviceScale(startupPage, deviceScale); err != nil {
		return err
	}
	lastWindowTitle := config.Title
	lastCursor := CursorDefault
	state := inputState{}

	// Present the already-rendered initial Page before running any queued page
	// work. In particular, a production navigation can no longer keep the
	// native window invisible while its first large script task executes.
	activePage := startupPage
	if shell != nil {
		shell.syncPage(activePage)
	}
	initialStarted := time.Time{}
	if frameObserver != nil {
		initialStarted = time.Now()
	}
	frame = activePage.Frame()
	rasterStarted := time.Time{}
	if frameObserver != nil {
		rasterStarted = time.Now()
	}
	pageRaster, err := compositor.pageCanvas(activePage, frame)
	if err != nil {
		return fmt.Errorf("window: rasterize initial frame: %w", err)
	}
	canvas := pageRaster.canvas
	rasterDuration := time.Duration(0)
	compositeStarted := time.Time{}
	if frameObserver != nil {
		rasterDuration = time.Since(rasterStarted)
		compositeStarted = time.Now()
	}
	if shell != nil {
		canvas, err = shell.composeAtScale(canvas, activePage, deviceScale)
		if err != nil {
			return fmt.Errorf("window: compose initial Graphite shell: %w", err)
		}
	}
	compositeDuration := time.Duration(0)
	presentStarted := time.Time{}
	if frameObserver != nil {
		compositeDuration = time.Since(compositeStarted)
		presentStarted = time.Now()
	}
	if err := backend.Present(canvas); err != nil {
		return fmt.Errorf("window: present initial frame: %w", err)
	}
	if frameObserver != nil {
		presentedAt := time.Now()
		if err := observeFrame(FrameTiming{
			PresentedAt: presentedAt,
			Raster:      rasterDuration, Composite: compositeDuration,
			Present: presentedAt.Sub(presentStarted), Total: presentedAt.Sub(initialStarted),
			PageChanged: true, ShellChanged: shell != nil,
			IncrementalRaster: pageRaster.incremental, DamagePixels: uint64(maxInt(0, pageRaster.damagePixels)),
			DamageRects:               uint64(maxInt(0, pageRaster.damageRects)),
			PlannedDamagePixels:       uint64(maxInt(0, pageRaster.plannedDamagePixels)),
			PlannedDamageRects:        uint64(maxInt(0, pageRaster.plannedDamageRects)),
			PlannedScrollClipArea:     uint64(maxInt(0, pageRaster.plannedScrollClipArea)),
			PlannedRawDamageRects:     uint64(maxInt(0, pageRaster.plannedRawDamageRects)),
			PlannedExactDamagePixels:  uint64(maxInt(0, pageRaster.plannedExactDamagePixels)),
			PlannedExactDamageRects:   uint64(maxInt(0, pageRaster.plannedExactDamageRects)),
			PlannedDamageSource:       pageRaster.plannedDamageSource,
			PlannedSourceDamagePixels: uint64(maxInt(0, pageRaster.plannedSourceDamagePixels)),
			PlannedSourceDamageRects:  uint64(maxInt(0, pageRaster.plannedSourceDamageRects)),
			PlannedScrollDeltaX:       int64(pageRaster.plannedScrollDeltaX),
			PlannedScrollDeltaY:       int64(pageRaster.plannedScrollDeltaY),
			PlannedScrollCommands:     uint64(maxInt(0, pageRaster.plannedScrollCommands)),
			PlannedMovingCommands:     uint64(maxInt(0, pageRaster.plannedMovingCommands)),
			RetainedUnderlay:          pageRaster.retainedUnderlay,
			UnderlayCacheHit:          pageRaster.underlayCacheHit,
			UnderlayCacheMiss:         pageRaster.underlayCacheMiss,
			OverlaySuffixCommands:     uint64(maxInt(0, pageRaster.overlaySuffixCommands)),
			OverlayRegions:            uint64(maxInt(0, pageRaster.overlayRegions)),
			UnderlayCopyPixels:        uint64(maxInt(0, pageRaster.underlayCopyPixels)),
			UnderlayBuild:             pageRaster.underlayBuild,
			ScrollCopy:                pageRaster.scrollCopy,
			DamageReplay:              pageRaster.damageReplay,
			UnderlayCopy:              pageRaster.underlayCopy,
			OverlayReplay:             pageRaster.overlayReplay,
			RasterFallback:            pageRaster.fallback,
		}); err != nil {
			return fmt.Errorf("window: observe initial frame timing: %w", err)
		}
	}
	presented = frame
	if shell != nil {
		presentedShellRevision = shell.revision
	}
	if err := startup.observe(StartupFirstPresent, startupPage.Navigation()); err != nil {
		return fmt.Errorf("window: observe first presentation: %w", err)
	}

	for {
		iterationStarted := time.Time{}
		tasksStarted := time.Time{}
		if frameObserver != nil {
			iterationStarted = pendingFrameWorkStarted
			pendingFrameWorkStarted = time.Time{}
			if iterationStarted.IsZero() {
				iterationStarted = time.Now()
			}
			tasksStarted = iterationStarted
		}
		activePage = page
		activeState := &state
		pages := []*browser.Page{page}
		if shell != nil {
			activePage = shell.activePage()
			activeState = shell.activeInputState()
			pages = shell.pages()
		}
		if activePage == nil || activeState == nil {
			return fmt.Errorf("window: browser shell has no active tab")
		}
		for _, currentPage := range pages {
			if err := setPageDeviceScale(currentPage, deviceScale); err != nil {
				return err
			}
		}
		compositor.prune(pages)
		yieldedActiveFrame := false
		for _, currentPage := range pages {
			var afterTask func() error
			if currentPage == startupPage {
				afterTask = func() error { return startup.observeNavigation(currentPage.Navigation()) }
			}
			var stopAfterScrollRevision *uint64
			if currentPage == activePage {
				revision := currentPage.ScrollPresentationRevision()
				stopAfterScrollRevision = &revision
			}
			_, yieldedFrame, err := pumpPageTasksObservedUntilScrollPresentation(ctx, currentPage, afterTask, stopAfterScrollRevision)
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return nil
				}
				return err
			}
			if currentPage == startupPage {
				if err := startup.observeNavigation(currentPage.Navigation()); err != nil {
					return fmt.Errorf("window: observe startup navigation: %w", err)
				}
			}
			if yieldedFrame {
				yieldedActiveFrame = true
				break
			}
			if shell != nil && shell.checkpoint != nil {
				if err := shell.checkpoint(currentPage); err != nil {
					return fmt.Errorf("window: observe page checkpoint: %w", err)
				}
			}
		}
		if shell != nil {
			shell.syncPage(activePage)
			if title := shell.windowTitle(); title != lastWindowTitle {
				if titleBackend, ok := backend.(TitleBackend); ok {
					if err := titleBackend.SetTitle(title); err != nil {
						return fmt.Errorf("window: update title: %w", err)
					}
				}
				lastWindowTitle = title
			}
		}
		frame = activePage.Frame()
		if shell != nil && (shell.revision != accessibilityRevision || frame != accessibilityFrame) {
			if accessibilityBackend, ok := backend.(AccessibilityBackend); ok {
				if err := accessibilityBackend.UpdateAccessibility(shell.accessibilitySnapshot()); err != nil {
					return fmt.Errorf("window: update accessibility: %w", err)
				}
			}
			accessibilityRevision = shell.revision
			accessibilityFrame = frame
		}
		tasksDuration := time.Duration(0)
		if frameObserver != nil {
			tasksDuration = time.Since(tasksStarted)
		}
		shellChanged := shell != nil && shell.revision != presentedShellRevision
		presentedDuringIteration := false
		if frame != nil && (frame != presented || shellChanged) {
			pageChanged := frame != presented
			rasterStarted := time.Time{}
			if frameObserver != nil {
				rasterStarted = time.Now()
			}
			pageRaster, err := compositor.pageCanvas(activePage, frame)
			if err != nil {
				return fmt.Errorf("window: rasterize frame: %w", err)
			}
			canvas := pageRaster.canvas
			rasterDuration := time.Duration(0)
			compositeStarted := time.Time{}
			if frameObserver != nil {
				rasterDuration = time.Since(rasterStarted)
				compositeStarted = time.Now()
			}
			if shell != nil {
				canvas, err = shell.composeAtScale(canvas, activePage, deviceScale)
				if err != nil {
					return fmt.Errorf("window: compose Graphite shell: %w", err)
				}
			}
			compositeDuration := time.Duration(0)
			presentStarted := time.Time{}
			if frameObserver != nil {
				compositeDuration = time.Since(compositeStarted)
				presentStarted = time.Now()
			}
			if err := backend.Present(canvas); err != nil {
				return fmt.Errorf("window: present frame: %w", err)
			}
			if frameObserver != nil {
				presentedAt := time.Now()
				if err := observeFrame(FrameTiming{
					PresentedAt: presentedAt, Tasks: tasksDuration,
					Raster: rasterDuration, Composite: compositeDuration,
					Present: presentedAt.Sub(presentStarted), Total: presentedAt.Sub(iterationStarted),
					PageChanged: pageChanged, ShellChanged: shellChanged, ScrollFrame: yieldedActiveFrame,
					IncrementalRaster: pageRaster.incremental, DamagePixels: uint64(maxInt(0, pageRaster.damagePixels)),
					DamageRects:               uint64(maxInt(0, pageRaster.damageRects)),
					PlannedDamagePixels:       uint64(maxInt(0, pageRaster.plannedDamagePixels)),
					PlannedDamageRects:        uint64(maxInt(0, pageRaster.plannedDamageRects)),
					PlannedScrollClipArea:     uint64(maxInt(0, pageRaster.plannedScrollClipArea)),
					PlannedRawDamageRects:     uint64(maxInt(0, pageRaster.plannedRawDamageRects)),
					PlannedExactDamagePixels:  uint64(maxInt(0, pageRaster.plannedExactDamagePixels)),
					PlannedExactDamageRects:   uint64(maxInt(0, pageRaster.plannedExactDamageRects)),
					PlannedDamageSource:       pageRaster.plannedDamageSource,
					PlannedSourceDamagePixels: uint64(maxInt(0, pageRaster.plannedSourceDamagePixels)),
					PlannedSourceDamageRects:  uint64(maxInt(0, pageRaster.plannedSourceDamageRects)),
					PlannedScrollDeltaX:       int64(pageRaster.plannedScrollDeltaX),
					PlannedScrollDeltaY:       int64(pageRaster.plannedScrollDeltaY),
					PlannedScrollCommands:     uint64(maxInt(0, pageRaster.plannedScrollCommands)),
					PlannedMovingCommands:     uint64(maxInt(0, pageRaster.plannedMovingCommands)),
					RetainedUnderlay:          pageRaster.retainedUnderlay,
					UnderlayCacheHit:          pageRaster.underlayCacheHit,
					UnderlayCacheMiss:         pageRaster.underlayCacheMiss,
					OverlaySuffixCommands:     uint64(maxInt(0, pageRaster.overlaySuffixCommands)),
					OverlayRegions:            uint64(maxInt(0, pageRaster.overlayRegions)),
					UnderlayCopyPixels:        uint64(maxInt(0, pageRaster.underlayCopyPixels)),
					UnderlayBuild:             pageRaster.underlayBuild,
					ScrollCopy:                pageRaster.scrollCopy,
					DamageReplay:              pageRaster.damageReplay,
					UnderlayCopy:              pageRaster.underlayCopy,
					OverlayReplay:             pageRaster.overlayReplay,
					RasterFallback:            pageRaster.fallback,
				}); err != nil {
					return fmt.Errorf("window: observe frame timing: %w", err)
				}
			}
			presented = frame
			if shell != nil {
				presentedShellRevision = shell.revision
			}
			presentedDuringIteration = true
			if activePage == startupPage && frame != startupFrame && startupPage.Navigation().State == browser.NavigationComplete {
				if err := startup.observe(StartupFirstContentPresent, startupPage.Navigation()); err != nil {
					return fmt.Errorf("window: observe first content presentation: %w", err)
				}
			}
		}
		if yieldedActiveFrame && presentedDuringIteration {
			// Let a newly rendered active-page frame reach the native window
			// before draining follow-up listener and reconciliation tasks.
			continue
		}

		event, err := backend.NextEvent(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil
			}
			return fmt.Errorf("window: native event: %w", err)
		}
		if event.Kind == EventClose {
			return nil
		}
		if frameObserver != nil {
			// Carry the admitted event's start into the next loop so synchronous
			// input routing and page-task work are included in the presentation.
			pendingFrameWorkStarted = time.Now()
		}
		if event.DeviceScaleFactor > 0 && event.DeviceScaleFactor != deviceScale {
			if !finiteDeviceScale(event.DeviceScaleFactor) {
				return fmt.Errorf("window: invalid backend device scale %g", event.DeviceScaleFactor)
			}
			deviceScale = event.DeviceScaleFactor
			compositor.setDeviceScale(deviceScale)
			for _, currentPage := range pages {
				if err := setPageDeviceScale(currentPage, deviceScale); err != nil {
					return err
				}
			}
			presented = nil
		}
		if shell != nil && event.Kind == EventPointerDown && event.Button == 1 {
			handled, contextErr := shell.handleContextMenu(ctx, activePage, backend, event)
			if contextErr != nil {
				return contextErr
			}
			if handled {
				continue
			}
		}
		if event.Kind == EventPointerMove {
			cursor := pageCursorAt(activePage, event.X, event.Y)
			if shell != nil {
				cursor = shell.cursorAt(activePage, event)
			}
			if cursor != lastCursor {
				if cursorBackend, ok := backend.(CursorBackend); ok {
					if err := cursorBackend.SetCursor(cursor); err != nil {
						return fmt.Errorf("window: update cursor: %w", err)
					}
				}
				lastCursor = cursor
			}
		}
		if shell != nil {
			handled, translated, closeWindow, err := shell.handleEvent(ctx, activePage, event, activeState)
			if err != nil {
				return err
			}
			if closeWindow {
				return nil
			}
			if handled {
				continue
			}
			event = translated
		}
		if handled, err := routeClipboardShortcut(activePage, backend, event, activeState); err != nil {
			return err
		} else if handled {
			continue
		}
		if err := routeEvent(activePage, event, activeState); err != nil {
			return err
		}
	}
}

func backendDeviceScale(backend Backend) (float64, error) {
	scale := 1.0
	if scaled, ok := backend.(DeviceScaleBackend); ok {
		var err error
		scale, err = scaled.DeviceScaleFactor()
		if err != nil {
			return 0, fmt.Errorf("window: read backend device scale: %w", err)
		}
	}
	if !finiteDeviceScale(scale) {
		return 0, fmt.Errorf("window: invalid backend device scale %g", scale)
	}
	return scale, nil
}

func setPageDeviceScale(page *browser.Page, scale float64) error {
	if page == nil || page.DeviceScaleFactor() == scale {
		return nil
	}
	if err := page.SetDeviceScaleFactor(scale); err != nil {
		return fmt.Errorf("window: set page device scale: %w", err)
	}
	return nil
}

func routeClipboardShortcut(page *browser.Page, backend Backend, event Event, state *inputState) (bool, error) {
	if event.Kind != EventKeyDown || !event.Modifiers.Meta || event.Modifiers.Ctrl || event.Modifiers.Alt {
		return false, nil
	}
	clipboard, ok := backend.(ClipboardBackend)
	if !ok {
		return false, nil
	}
	target, ok := keyboardTarget(page, state.focusTarget)
	if !ok {
		return false, nil
	}
	key := event.Key
	if key != "c" && key != "C" && key != "x" && key != "X" && key != "v" && key != "V" {
		return false, nil
	}
	state.focusTarget = target
	selected, selectionErr := page.SelectedText(target)
	if errors.Is(selectionErr, dom.ErrWrongNodeKind) {
		return false, nil
	}
	if selectionErr != nil {
		return true, selectionErr
	}
	if err := routeEvent(page, event, state); err != nil {
		return true, err
	}
	queueEdit := func(data, inputType string) error {
		_, err := page.QueueInputEvent(browser.InputEvent{
			Type: browser.InputBeforeInput, Target: target, Data: data, InputType: inputType,
			MetaKey: true, ShiftKey: event.Modifiers.Shift,
		})
		return err
	}
	switch key {
	case "c", "C", "x", "X":
		if err := clipboard.WriteClipboardText(selected); err != nil {
			return true, err
		}
		if key == "x" || key == "X" {
			return true, queueEdit("", "deleteByCut")
		}
		return true, nil
	case "v", "V":
		value, err := clipboard.ReadClipboardText()
		if err != nil {
			return true, err
		}
		return true, queueEdit(value, "insertFromPaste")
	default:
		return false, nil
	}
}

type inputState struct {
	focusTarget            browser.NodeHandle
	hoverTarget            browser.NodeHandle
	pressedTarget          browser.NodeHandle
	scrollbarActivity      browser.NodeHandle
	scrollbarActivityUntil time.Time
	scrollbarActivityRoot  bool
	scrollbarHover         browser.NodeHandle
	scrollbarHoverRoot     bool
	pressedButton          int
	pressed                bool
}

func pumpPageTasks(ctx context.Context, page *browser.Page) (int, error) {
	return pumpPageTasksObserved(ctx, page, nil)
}

func pumpPageTasksObserved(ctx context.Context, page *browser.Page, afterTask func() error) (int, error) {
	count, _, err := pumpPageTasksObservedUntilScrollPresentation(ctx, page, afterTask, nil)
	return count, err
}

func pumpPageTasksObservedUntilScrollPresentation(
	ctx context.Context,
	page *browser.Page,
	afterTask func() error,
	stopAfterRevision *uint64,
) (int, bool, error) {
	count := 0
	for ; page.Realm.Tasks.Len() != 0; count++ {
		if count >= maximumTasksPerPump {
			return count, false, fmt.Errorf("window: page task pump exceeded %d tasks", maximumTasksPerPump)
		}
		if err := page.Realm.RunOne(ctx); err != nil {
			return count, false, err
		}
		if afterTask != nil {
			if err := afterTask(); err != nil {
				return count + 1, false, fmt.Errorf("window: observe page task: %w", err)
			}
		}
		if stopAfterRevision != nil && page.ScrollPresentationRevision() != *stopAfterRevision {
			return count + 1, true, nil
		}
	}
	if _, err := page.CollectScriptMemoryAtCheckpoint(); err != nil {
		return count, false, fmt.Errorf("window: collect script memory at task checkpoint: %w", err)
	}
	return count, false, ctx.Err()
}

func routeEvent(page *browser.Page, event Event, state *inputState) error {
	modifiers := event.Modifiers
	queue := func(input browser.InputEvent) error {
		input.AltKey = modifiers.Alt
		input.CtrlKey = modifiers.Ctrl
		input.MetaKey = modifiers.Meta
		input.ShiftKey = modifiers.Shift
		_, err := page.QueueInputEvent(input)
		return err
	}
	pointer := func(kind browser.InputEventType, target browser.NodeHandle) error {
		return queue(browser.InputEvent{
			Type: kind, Target: target, X: event.X, Y: event.Y, Button: event.Button,
			Buttons: event.Buttons, PointerID: 1, PointerType: "mouse", IsPrimary: true,
		})
	}

	switch event.Kind {
	case EventNone:
		return nil
	case EventResize:
		if event.Width <= 0 || event.Height <= 0 {
			return nil
		}
		_, err := page.QueueViewportResize(render.Viewport{Width: event.Width, Height: event.Height})
		return err
	case EventPointerMove:
		return pointer(browser.InputPointerMove, browser.NodeHandle{})
	case EventPointerDown:
		target, ok := page.HitTest(event.X, event.Y)
		state.pressed = ok
		state.pressedTarget = target
		state.pressedButton = event.Button
		if ok {
			state.focusTarget = target
		}
		return pointer(browser.InputPointerDown, target)
	case EventPointerUp:
		target, _ := page.HitTest(event.X, event.Y)
		upErr := pointer(browser.InputPointerUp, target)
		shouldClick := state.pressed && state.pressedButton == 0 && event.Button == 0 && target == state.pressedTarget
		state.pressed = false
		state.pressedTarget = browser.NodeHandle{}
		if shouldClick {
			return errors.Join(upErr, pointer(browser.InputClick, target))
		}
		return upErr
	case EventScroll:
		_, err := page.QueueScrollAt(event.X, event.Y, event.DeltaX, event.DeltaY)
		return err
	case EventKeyDown, EventKeyUp:
		target, ok := keyboardTarget(page, state.focusTarget)
		if !ok {
			return nil
		}
		state.focusTarget = target
		kind := browser.InputKeyDown
		if event.Kind == EventKeyUp {
			kind = browser.InputKeyUp
		}
		err := queue(browser.InputEvent{
			Type: kind, Target: target, Key: event.Key, Code: event.Code,
			Repeat: event.Repeat, IsComposing: event.Composing,
		})
		if event.Kind == EventKeyDown && event.Text != "" && !modifiers.Meta && !modifiers.Ctrl {
			err = errors.Join(err, queue(browser.InputEvent{
				Type: browser.InputBeforeInput, Target: target, Data: event.Text,
				InputType: "insertText",
			}))
		}
		return err
	case EventTextInput, EventCompositionStart, EventCompositionUpdate, EventCompositionEnd:
		target, ok := keyboardTarget(page, state.focusTarget)
		if !ok {
			return nil
		}
		state.focusTarget = target
		if event.Kind == EventTextInput {
			return queue(browser.InputEvent{
				Type: browser.InputBeforeInput, Target: target, Data: event.Text,
				InputType: "insertText",
			})
		}
		kind := browser.InputCompositionStart
		if event.Kind == EventCompositionUpdate {
			kind = browser.InputCompositionUpdate
		} else if event.Kind == EventCompositionEnd {
			kind = browser.InputCompositionEnd
		}
		err := queue(browser.InputEvent{
			Type: kind, Target: target, Data: event.Text, IsComposing: event.Kind != EventCompositionEnd,
		})
		if event.Kind == EventCompositionEnd && event.Text != "" {
			err = errors.Join(err, queue(browser.InputEvent{
				Type: browser.InputBeforeInput, Target: target, Data: event.Text,
				InputType: "insertCompositionText",
			}))
		}
		return err
	case EventFocus, EventBlur:
		target, ok := keyboardTarget(page, state.focusTarget)
		if !ok {
			return nil
		}
		kind := browser.InputFocus
		if event.Kind == EventBlur {
			kind = browser.InputBlur
		}
		return queue(browser.InputEvent{Type: kind, Target: target})
	default:
		return fmt.Errorf("window: unsupported native event %d", event.Kind)
	}
}

func keyboardTarget(page *browser.Page, preferred browser.NodeHandle) (browser.NodeHandle, bool) {
	if preferred.Node != dom.InvalidNodeID && preferred.Document == page.DocumentGeneration() {
		return preferred, true
	}
	if active, ok := page.ActiveElementHandle(); ok {
		return active, true
	}
	document := page.Document()
	if document == nil {
		return browser.NodeHandle{}, false
	}
	body, found, err := document.RelatedNode(document.RootID(), dom.DocumentBody)
	if err != nil || !found {
		return browser.NodeHandle{}, false
	}
	return browser.NodeHandle{Document: page.DocumentGeneration(), Node: body}, true
}
