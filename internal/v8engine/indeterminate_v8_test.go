//go:build v8 && cgo && darwin && arm64

package v8engine

import (
	"context"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
)

func TestStockV8IndeterminateStateIsLiveAndRestylesSynchronously(t *testing.T) {
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatalf("NewWithEngine: %v", err)
	}
	defer func() {
		if err := browserRuntime.Close(); err != nil {
			t.Errorf("Close browser: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page, err := browserRuntime.LoadPage(ctx, "https://gossamer.test/indeterminate", staticDocumentLoader{document: `<!doctype html>
		<html><head><style>
			#check { color: black }
			#check:indeterminate { color: green }
			input[type=radio] { width: 10px }
			input[type=radio]:indeterminate { width: 30px }
			progress { height: 10px }
			progress:indeterminate { height: 30px }
		</style></head><body>
			<input id="check" type="checkbox">
			<input id="first" type="radio" name="pick">
			<input id="second" type="radio" name="pick">
			<progress id="progress"></progress>
		</body></html>`})
	if err != nil {
		t.Fatalf("LoadPage: %v", err)
	}

	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/indeterminate/assert.js",
		Source: `
			(() => {
				const check = document.getElementById("check");
				const first = document.getElementById("first");
				const second = document.getElementById("second");
				const progress = document.getElementById("progress");
				const descriptor = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "indeterminate");
				if (!descriptor || typeof descriptor.get !== "function" || typeof descriptor.set !== "function") {
					throw new Error("HTMLInputElement.indeterminate descriptor is incomplete");
				}
				if (check.indeterminate || !first.matches(":indeterminate") ||
					!second.matches(":indeterminate") || !progress.matches(":indeterminate")) {
					throw new Error("initial indeterminate state is wrong");
				}
				check.indeterminate = true;
				if (!check.indeterminate || !check.matches(":indeterminate") ||
					getComputedStyle(check).color !== "rgb(0, 128, 0)") {
					throw new Error("live checkbox indeterminateness did not restyle");
				}
				globalThis.__indeterminateDuringCanceledClick = true;
				check.addEventListener("click", event => {
					__indeterminateDuringCanceledClick = check.indeterminate;
					event.preventDefault();
				});
				first.checked = true;
				if (first.matches(":indeterminate") || second.matches(":indeterminate") ||
					getComputedStyle(second).width !== "10px") {
					throw new Error("live radio checkedness did not update group state");
				}
				progress.setAttribute("value", "0.5");
				if (progress.matches(":indeterminate") || getComputedStyle(progress).height !== "10px") {
					throw new Error("progress value presence did not update indeterminate state");
				}
			})();
		`,
	})
	if err != nil {
		t.Fatalf("QueueScript: %v", err)
	}
	if err := page.Realm.RunOne(ctx); err != nil {
		t.Fatalf("run indeterminate assertions: %v", err)
	}
	if page.Dirty() {
		if err := page.Realm.RunOne(ctx); err != nil {
			t.Fatalf("render indeterminate assertions: %v", err)
		}
	}
	checkboxID, ok := page.Document().ElementByID("check")
	if !ok {
		t.Fatal("checkbox has no stable ID")
	}
	if _, err := page.QueueInputEvent(browser.InputEvent{
		Type:   browser.InputClick,
		Target: browser.NodeHandle{Document: page.DocumentGeneration(), Node: checkboxID},
	}); err != nil {
		t.Fatalf("QueueInputEvent: %v", err)
	}
	if err := page.Realm.RunOne(ctx); err != nil {
		t.Fatalf("run canceled checkbox click: %v", err)
	}
	if page.Dirty() {
		if err := page.Realm.RunOne(ctx); err != nil {
			t.Fatalf("render canceled checkbox click: %v", err)
		}
	}
	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/indeterminate/click-assert.js",
		Source: `
			const check = document.getElementById("check");
			if (__indeterminateDuringCanceledClick || !check.indeterminate ||
				!check.matches(":indeterminate")) {
				throw new Error("checkbox activation did not clear then restore canceled indeterminateness");
			}
		`,
	})
	if err != nil {
		t.Fatalf("QueueScript click assertion: %v", err)
	}
	if err := page.Realm.RunOne(ctx); err != nil {
		t.Fatalf("run click assertion: %v", err)
	}
}
