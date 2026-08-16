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
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := backend.Open(Config{Title: title, Width: frame.Viewport.Width, Height: frame.Viewport.Height}); err != nil {
		return err
	}
	defer func() { result = errors.Join(result, backend.Close()) }()

	var presented *render.Frame
	state := inputState{}
	for {
		if err := pumpPageTasks(ctx, page); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil
			}
			return err
		}
		frame = page.Frame()
		if frame != nil && frame != presented {
			canvas, err := render.Rasterize(frame)
			if err != nil {
				return fmt.Errorf("window: rasterize frame: %w", err)
			}
			if err := backend.Present(canvas); err != nil {
				return fmt.Errorf("window: present frame: %w", err)
			}
			presented = frame
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
