package browser

import (
	"context"
	"testing"
)

func TestPageSelectorStateInvalidatesAndFeedsComputedStyle(t *testing.T) {
	t.Parallel()

	engine, page, target := computedStyleTestPage(t, `
		<html><head><style>
			#target { display:block; width:10px; height:10px; color:black; background-color:transparent; border:2px solid black; font-style:normal }
			#target:hover { background-color:#ff0000 }
			#target:active { width:40px }
			#target:focus { color:#0000ff }
			#target:focus-visible { font-style:italic }
			body:focus-within #target { height:30px }
			#target:target { border-color:#008000 }
		</style></head><body><button id="target">state</button></body></html>
	`)
	defer engine.Close()
	handle := NodeHandle{Document: page.DocumentGeneration(), Node: target}
	host := &taskHost{page: page, generation: page.DocumentGeneration()}

	page.mutex.Lock()
	page.location.Fragment = "target"
	page.invalidateStyleLocked()
	page.mutex.Unlock()
	assertSelectorStateProperty(t, page, handle, "border-top-color", "rgb(0, 128, 0)")
	if matched, err := host.MatchesSelector(handle, ":target"); err != nil || !matched {
		t.Fatalf("MatchesSelector(:target) = %t, %v", matched, err)
	}
	assertSelectorStateProperty(t, page, handle, "background-color", "rgba(0, 0, 0, 0)")

	if _, err := page.QueueInputEvent(InputEvent{Type: InputPointerDown, Target: handle, PointerID: 1, IsPrimary: true}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertSelectorStateProperty(t, page, handle, "background-color", "rgb(255, 0, 0)")
	assertSelectorStateProperty(t, page, handle, "width", "40px")
	if matched, err := host.MatchesSelector(handle, ":hover:active"); err != nil || !matched {
		t.Fatalf("MatchesSelector(:hover:active) = %t, %v", matched, err)
	}
	if !page.Dirty() {
		t.Fatal("pointer selector-state change did not dirty the page")
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}

	if _, err := page.QueueInputEvent(InputEvent{Type: InputPointerUp, Target: handle, PointerID: 1, IsPrimary: true}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertSelectorStateProperty(t, page, handle, "background-color", "rgb(255, 0, 0)")
	assertSelectorStateProperty(t, page, handle, "width", "10px")
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}

	if _, err := page.QueueInputEvent(InputEvent{Type: InputPointerLeave, Target: handle, PointerID: 1, IsPrimary: true}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertSelectorStateProperty(t, page, handle, "background-color", "rgba(0, 0, 0, 0)")

	if err := host.Focus(handle); err != nil {
		t.Fatal(err)
	}
	assertSelectorStateProperty(t, page, handle, "color", "rgb(0, 0, 255)")
	assertSelectorStateProperty(t, page, handle, "font-style", "italic")
	assertSelectorStateProperty(t, page, handle, "height", "30px")
	if matched, err := host.MatchesSelector(handle, ":focus:focus-visible"); err != nil || !matched {
		t.Fatalf("MatchesSelector(:focus:focus-visible) = %t, %v", matched, err)
	}
	if err := host.Blur(handle); err != nil {
		t.Fatal(err)
	}
	assertSelectorStateProperty(t, page, handle, "color", "rgb(0, 0, 0)")
	assertSelectorStateProperty(t, page, handle, "font-style", "normal")
	assertSelectorStateProperty(t, page, handle, "height", "10px")
}

func assertSelectorStateProperty(t *testing.T, page *Page, handle NodeHandle, property, want string) {
	t.Helper()
	value, found, err := page.ComputedStyleProperty(handle, property)
	if err != nil || !found || value != want {
		t.Fatalf("computed %s = %q, %t, %v; want %q, true, nil", property, value, found, err, want)
	}
}
