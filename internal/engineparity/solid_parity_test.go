package engineparity

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	htmlparser "github.com/JediWattson/gossamer/internal/html"
	"github.com/JediWattson/gossamer/internal/nativeengine"
)

func TestStrandRunsProductionSolidParitySequence(t *testing.T) {
	runProductionSolidParitySequence(t, nativeengine.New(nativeengine.Config{}))
}

func runProductionSolidParitySequence(t *testing.T, engine browser.Engine) {
	t.Helper()
	source, err := os.ReadFile("testdata/vite-solid/dist/solid-counter-1.9.14.production.js")
	if err != nil {
		t.Fatal(err)
	}
	root, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body><main id="solid-root"></main></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://gossamer.test/solid-for/")
	page, err := browserRuntime.NewPage(root, location)
	if err != nil {
		t.Fatal(err)
	}
	runQueued := func(label string) {
		t.Helper()
		if page.Realm.Tasks.Len() == 0 {
			t.Fatalf("run %s: no queued tasks", label)
		}
		for page.Realm.Tasks.Len() != 0 {
			if runErr := page.Realm.RunOne(context.Background()); runErr != nil {
				t.Fatalf("run %s: %v", label, runErr)
			}
		}
	}
	queueScript := func(label, script string) {
		t.Helper()
		if _, queueErr := page.QueueScript(browser.ScriptSource{
			URL: location.ResolveReference(&url.URL{Path: label + ".js"}).String(), Source: script,
		}); queueErr != nil {
			t.Fatalf("queue %s: %v", label, queueErr)
		}
		runQueued(label)
	}
	handle := func(id string) browser.NodeHandle {
		t.Helper()
		node, found := page.Document().ElementByID(id)
		if !found {
			t.Fatalf("element %q is missing", id)
		}
		return browser.NodeHandle{Document: page.DocumentGeneration(), Node: node}
	}
	click := func(id string) {
		t.Helper()
		if _, queueErr := page.QueueInputEvent(browser.InputEvent{Type: browser.InputClick, Target: handle(id)}); queueErr != nil {
			t.Fatal(queueErr)
		}
		runQueued("click " + id)
	}
	queueInput := func(label string, event browser.InputEvent) {
		t.Helper()
		if _, queueErr := page.QueueInputEvent(event); queueErr != nil {
			t.Fatalf("queue %s: %v", label, queueErr)
		}
		runQueued(label)
	}

	queueScript("solid-for", string(source))
	queueScript("remember-keyed-nodes", `
globalThis.__solidInitialA = document.getElementById("solid-item-a");
globalThis.__solidInitialB = document.getElementById("solid-item-b");
globalThis.__solidInitialC = document.getElementById("solid-item-c");
if (!__solidInitialA || !__solidInitialB || !__solidInitialC) {
  throw new Error("Solid For did not render its initial rows");
}
`)
	initialA := handle("solid-item-a").Node
	initialC := handle("solid-item-c").Node
	click("solid-reorder")
	queueScript("assert-keyed-reconciliation", `
const list = document.getElementById("solid-list");
if (list.children.length !== 3 || list.children[0] !== __solidInitialC ||
    list.children[1] !== __solidInitialA || __solidInitialB.parentNode !== null ||
    document.getElementById("solid-item-d") === null || __solidRowCleanups !== 1) {
  throw new Error("Solid keyed reconciliation lost identity or cleanup");
}
`)
	if got := handle("solid-item-a").Node; got != initialA {
		t.Fatalf("keyed A identity = %d, want %d", got, initialA)
	}
	if got := handle("solid-item-c").Node; got != initialC {
		t.Fatalf("keyed C identity = %d, want %d", got, initialC)
	}
	click("solid-toggle")
	queueScript("assert-show-hidden", `
if (document.getElementById("solid-visible") !== null ||
    document.getElementById("solid-hidden") === null || __solidBranchCleanups !== 1 ||
    !document.getElementById("solid-dynamic").hasAttribute("hidden")) {
  throw new Error("Solid Show did not dispose the visible branch");
}
`)
	click("solid-toggle")
	queueScript("assert-show-visible", `
if (document.getElementById("solid-visible") === null ||
    document.getElementById("solid-hidden") !== null || __solidBranchCleanups !== 1 ||
    document.getElementById("solid-dynamic").hasAttribute("hidden")) {
  throw new Error("Solid Show did not restore the visible branch");
}
`)
	queueScript("position-text-selection", `
var solidText = document.getElementById("solid-text");
solidText.setSelectionRange(solidText.value.length, solidText.value.length);
solidText = undefined;
`)
	queueInput("text beforeinput", browser.InputEvent{
		Type: browser.InputBeforeInput, Target: handle("solid-text"), Data: "X", InputType: "insertText",
	})
	click("solid-check")
	queueInput("checkbox change", browser.InputEvent{Type: browser.InputChange, Target: handle("solid-check")})
	click("solid-radio-beta")
	queueInput("radio change", browser.InputEvent{Type: browser.InputChange, Target: handle("solid-radio-beta")})
	queueScript("select-two", `document.getElementById("solid-select").value = "two";`)
	queueInput("select change", browser.InputEvent{Type: browser.InputChange, Target: handle("solid-select")})
	queueScript("assert-controlled-forms", `
if (document.getElementById("solid-text").value !== "seedX" ||
    document.getElementById("solid-name").textContent !== "seedX" ||
    document.getElementById("solid-check").checked !== true ||
    document.getElementById("solid-radio-alpha").checked !== false ||
    document.getElementById("solid-radio-beta").checked !== true ||
    document.getElementById("solid-select").value !== "two" ||
    document.getElementById("solid-form-state").textContent !== "seedX:enabled:beta:two") {
  throw new Error("Solid controlled form state diverged from Go input/change events");
}
`)
	click("solid-counter")
	queueScript("assert-reactive-dom", `
var solidDynamic = document.getElementById("solid-dynamic");
if (!solidDynamic.classList.contains("active") ||
    solidDynamic.style.getPropertyValue("color") !== "red" ||
    solidDynamic.style.getPropertyValue("--solid-count") !== "1" ||
    solidDynamic.getAttribute("data-state") !== "on" ||
    solidDynamic.getAttribute("title") !== "count-1" ||
    solidDynamic.hasAttribute("hidden") ||
    document.getElementById("solid-counter").textContent !== "Count 1") {
  throw new Error("Solid reactive DOM bindings did not converge");
}
solidDynamic = undefined;
`)
	queueScript("dispose-keyed-list", `
globalThis.__solidDispose();
if (__solidCleanupCount !== 1 || __solidRowCleanups !== 4 || __solidBranchCleanups !== 2 ||
    document.getElementById("solid-root").firstChild !== null) {
  throw new Error("Solid keyed list teardown failed");
}
`)
	if err := page.Close(); err != nil {
		t.Fatal(err)
	}
	if stats := browserRuntime.Ledger().Stats(); stats.LiveObjects != 0 || stats.PersistentObjects != 0 {
		t.Fatalf("Solid keyed teardown ownership = %#v", stats)
	}
	if err := browserRuntime.Close(); err != nil {
		t.Fatal(err)
	}
}
