package window_test

import (
	"context"
	"io"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/browser/fake"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/loader"
	"github.com/JediWattson/gossamer/internal/window"
)

func TestRunRoutesNativeEventsThroughPageQueueAndPresentsFrames(t *testing.T) {
	t.Parallel()

	fakeEngine := fake.New()
	browserRuntime, err := browser.NewWithEngine(fakeEngine)
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	location, _ := url.Parse("https://window.gossamer.test/")
	page, err := browserRuntime.LoadPage(context.Background(), location.String(), windowDocumentLoader{response: &loader.Response{
		URL: location, StatusCode: 200,
		Body: io.NopCloser(strings.NewReader(`<!doctype html><html><body style="margin:0">
			<input id="target" style="display:block;width:100px;height:30px">
			<div style="display:block;height:800px">content</div>
		</body></html>`)),
	}})
	if err != nil {
		t.Fatal(err)
	}
	document := page.Document()
	inputID, found := document.ElementByID("target")
	if !found {
		t.Fatal("input has no stable node identity")
	}
	rootID, found, err := document.RelatedNode(document.RootID(), dom.DocumentElement)
	if err != nil || !found {
		t.Fatalf("document element: found=%t err=%v", found, err)
	}
	generation := page.DocumentGeneration()
	input := browser.NodeHandle{Document: generation, Node: inputID}
	root := browser.NodeHandle{Document: generation, Node: rootID}

	realm, ok := fakeEngine.LatestRealm()
	if !ok {
		t.Fatal("fake engine did not create a document realm")
	}
	var dispatched []browser.InputEventType
	bind := func(eventType browser.InputEventType, target browser.NodeHandle) {
		t.Helper()
		callback, callbackErr := realm.RegisterCallback(func(browser.Host) error {
			dispatched = append(dispatched, eventType)
			return nil
		})
		if callbackErr != nil {
			t.Fatal(callbackErr)
		}
		if bindErr := realm.Bind(eventType, target, callback); bindErr != nil {
			t.Fatal(bindErr)
		}
	}
	for _, eventType := range []browser.InputEventType{
		browser.InputPointerDown, browser.InputPointerUp, browser.InputClick,
		browser.InputFocus, browser.InputKeyDown, browser.InputBeforeInput,
		browser.InputInput, browser.InputKeyUp, browser.InputBlur,
	} {
		bind(eventType, input)
	}
	bind(browser.InputResize, root)
	bind(browser.InputScroll, root)

	backend := window.NewMemoryBackend(
		window.Event{Kind: window.EventResize, Width: 320, Height: 240},
		window.Event{Kind: window.EventPointerDown, X: 5, Y: 5, Button: 0, Buttons: 1},
		window.Event{Kind: window.EventPointerUp, X: 5, Y: 5, Button: 0},
		window.Event{Kind: window.EventPointerDown, X: 5, Y: 5, Button: 0, Buttons: 1},
		window.Event{Kind: window.EventPointerUp, X: 150, Y: 50, Button: 0},
		window.Event{Kind: window.EventFocus},
		window.Event{Kind: window.EventKeyDown, Key: "a", Code: "KeyA", Text: "a"},
		window.Event{Kind: window.EventKeyUp, Key: "a", Code: "KeyA"},
		window.Event{Kind: window.EventScroll, DeltaY: 100},
		window.Event{Kind: window.EventBlur},
		window.Event{Kind: window.EventClose},
	)
	if err := window.Run(context.Background(), page, backend, "Gossamer test"); err != nil {
		t.Fatal(err)
	}

	wantEvents := []browser.InputEventType{
		browser.InputResize,
		browser.InputPointerDown,
		browser.InputPointerUp,
		browser.InputClick,
		browser.InputPointerDown,
		browser.InputFocus,
		browser.InputKeyDown,
		browser.InputBeforeInput,
		browser.InputInput,
		browser.InputKeyUp,
		browser.InputScroll,
		browser.InputBlur,
	}
	if !reflect.DeepEqual(dispatched, wantEvents) {
		t.Fatalf("dispatched events = %v, want %v", dispatched, wantEvents)
	}
	if value, valueErr := document.FormValue(inputID); valueErr != nil || value != "a" {
		t.Fatalf("input value = %q, err=%v, want a", value, valueErr)
	}
	viewport, err := page.ViewportGeometry()
	if err != nil {
		t.Fatal(err)
	}
	if viewport.InnerWidth != 320 || viewport.InnerHeight != 240 || viewport.ScrollY != 100 {
		t.Fatalf("viewport = %#v, want 320x240 scrolled to 100", viewport)
	}
	if got := backend.Config(); got.Width != 800 || got.Height != 600 || got.Title != "Gossamer test" {
		t.Fatalf("initial native config = %#v", got)
	}
	frames := backend.Frames()
	if len(frames) < 3 {
		t.Fatalf("presented %d frames, want initial, resize, and scroll frames", len(frames))
	}
	last := frames[len(frames)-1].Bounds()
	if last.Dx() != 320 || last.Dy() != 240 {
		t.Fatalf("last presented frame = %dx%d, want 320x240", last.Dx(), last.Dy())
	}
}

type windowDocumentLoader struct {
	response *loader.Response
}

func (stub windowDocumentLoader) Load(context.Context, string) (*loader.Response, error) {
	return stub.response, nil
}
