package window

import (
	"context"
	"errors"
	"fmt"
	"runtime"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
)

const maximumTasksPerPump = 4096

// Run presents page and routes native input until the backend closes or the
// context is canceled. It serializes native UI access on the caller's OS
// thread while Page tasks continue to own all DOM, script, and frame changes.
func Run(ctx context.Context, page *browser.Page, backend Backend, title string) (result error) {
	return runSession(ctx, page, backend, Config{Title: title}, nil)
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
	title := config.Title
	if title == "" {
		title = "Gossamer"
	}
	return runSession(ctx, page, backend, Config{Title: title}, shell)
}

func runSession(ctx context.Context, page *browser.Page, backend Backend, config Config, shell *graphiteShell) (result error) {
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
	}
	config.Width = width
	config.Height = height
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := backend.Open(config); err != nil {
		return err
	}
	defer func() { result = errors.Join(result, backend.Close()) }()

	var presented *render.Frame
	var presentedShellRevision uint64
	state := inputState{}
	for {
		if err := pumpPageTasks(ctx, page); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil
			}
			return err
		}
		if shell != nil {
			shell.syncPage(page)
		}
		frame = page.Frame()
		shellChanged := shell != nil && shell.revision != presentedShellRevision
		if frame != nil && (frame != presented || shellChanged) {
			canvas, err := render.Rasterize(frame)
			if err != nil {
				return fmt.Errorf("window: rasterize frame: %w", err)
			}
			if shell != nil {
				canvas, err = shell.compose(canvas, page)
				if err != nil {
					return fmt.Errorf("window: compose Graphite shell: %w", err)
				}
			}
			if err := backend.Present(canvas); err != nil {
				return fmt.Errorf("window: present frame: %w", err)
			}
			presented = frame
			if shell != nil {
				presentedShellRevision = shell.revision
			}
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
		if shell != nil {
			handled, translated, closeWindow, err := shell.handleEvent(ctx, page, event, &state)
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
		if err := routeEvent(page, event, &state); err != nil {
			return err
		}
	}
}

type inputState struct {
	focusTarget   browser.NodeHandle
	pressedTarget browser.NodeHandle
	pressedButton int
	pressed       bool
}

func pumpPageTasks(ctx context.Context, page *browser.Page) error {
	for count := 0; page.Realm.Tasks.Len() != 0; count++ {
		if count >= maximumTasksPerPump {
			return fmt.Errorf("window: page task pump exceeded %d tasks", maximumTasksPerPump)
		}
		if err := page.Realm.RunOne(ctx); err != nil {
			return err
		}
	}
	return ctx.Err()
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
		_, err := page.QueueViewportScrollBy(event.DeltaX, event.DeltaY)
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
			Repeat: event.Repeat, IsComposing: false,
		})
		if event.Kind == EventKeyDown && event.Text != "" && !modifiers.Meta && !modifiers.Ctrl {
			err = errors.Join(err, queue(browser.InputEvent{
				Type: browser.InputBeforeInput, Target: target, Data: event.Text,
				InputType: "insertText",
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
