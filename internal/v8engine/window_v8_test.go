//go:build v8 && cgo && darwin && arm64

package v8engine

import (
	"context"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/window"
)

func TestStockV8InteractiveWindowRoutesNativeInputAndTeardown(t *testing.T) {
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	page, err := browserRuntime.LoadPage(context.Background(), "https://gossamer.test/window", staticDocumentLoader{
		document: `<!doctype html><html><body style="margin:0">
			<input id="target" style="display:block;width:100px;height:30px">
			<div style="display:block;height:800px">content</div>
		</body></html>`,
	})
	if err != nil {
		t.Fatal(err)
	}
	queue := func(label, source string) {
		t.Helper()
		if _, queueErr := page.QueueScript(browser.ScriptSource{
			URL: "https://gossamer.test/window/" + label + ".js", Source: source,
		}); queueErr != nil {
			t.Fatal(queueErr)
		}
		for attempt := 0; attempt < 64 && page.Realm.Tasks.Len() != 0; attempt++ {
			if runErr := page.Realm.RunOne(context.Background()); runErr != nil {
				t.Fatalf("%s: %v", label, runErr)
			}
		}
		if page.Realm.Tasks.Len() != 0 {
			t.Fatalf("%s: task queue did not drain", label)
		}
	}
	queue("listen", `
		globalThis.__windowEvents = [];
		const target = document.getElementById("target");
		const record = event => __windowEvents.push({
			type: event.type,
			target: event.target.id || event.target.nodeName,
			key: event.key || "",
			code: event.code || "",
			data: event.data || "",
			inputType: event.inputType || "",
			value: target.value,
		});
		for (const type of ["pointerdown", "pointerup", "click", "focus", "keydown", "beforeinput", "input", "keyup", "blur"])
			target.addEventListener(type, record);
		document.documentElement.addEventListener("resize", record);
		document.documentElement.addEventListener("scroll", record);
	`)

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
	if err := window.Run(context.Background(), page, backend, "Gossamer V8 window test"); err != nil {
		t.Fatal(err)
	}
	queue("assert", `
		const types = __windowEvents.map(event => event.type).join(",");
		if (types !== "resize,pointerdown,pointerup,click,pointerdown,focus,keydown,beforeinput,input,keyup,scroll,blur")
			throw new Error("native event order diverged: " + types);
		const key = __windowEvents.find(event => event.type === "keydown");
		const before = __windowEvents.find(event => event.type === "beforeinput");
		const input = __windowEvents.find(event => event.type === "input");
		if (key.key !== "a" || key.code !== "KeyA" || before.data !== "a" ||
			before.inputType !== "insertText" || input.value !== "a" ||
			innerWidth !== 320 || innerHeight !== 240 || scrollY !== 100 ||
			document.activeElement === document.getElementById("target"))
			throw new Error("native payload, edit, viewport, scroll, or blur state diverged");
		globalThis.__windowEvents = undefined;
	`)
	if len(backend.Frames()) < 3 {
		t.Fatalf("interactive backend presented %d frames, want at least 3", len(backend.Frames()))
	}
	if realm, ok := engine.LatestRealm(); ok {
		if err := realm.CollectGarbage(page); err != nil {
			t.Fatal(err)
		}
	}
	if err := page.Close(); err != nil {
		t.Fatal(err)
	}
	if ledger := browserRuntime.Ledger().Stats(); ledger.LiveObjects != 0 || ledger.PersistentObjects != 0 {
		t.Fatalf("interactive window teardown ownership = %#v", ledger)
	}
}
