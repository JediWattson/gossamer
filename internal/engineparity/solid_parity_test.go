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
	engine := nativeengine.New(nativeengine.Config{})
	runProductionSolidParitySequence(t, engine, func(page *browser.Page) error {
		realm, ok := engine.LatestRealm()
		if !ok {
			return nativeengine.ErrRealmClosed
		}
		return realm.CollectGarbage(page)
	})
}

func runProductionSolidParitySequence(t *testing.T, engine browser.Engine, collect func(*browser.Page) error) {
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
	baselineNodes := page.Document().Store().LiveLen()
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
	queueIsolated := func(label, script string) {
		t.Helper()
		queueScript(label, "(() => {\n"+script+"\n})();")
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
	queueIsolated("remember-keyed-nodes", `
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
	queueIsolated("assert-keyed-reconciliation", `
const list = document.getElementById("solid-list");
if (list.children.length !== 3 || list.children[0] !== __solidInitialC ||
    list.children[1] !== __solidInitialA || __solidInitialB.parentNode !== null ||
    document.getElementById("solid-item-new-1") === null || __solidRowCleanups !== 1) {
  throw new Error("Solid keyed reconciliation lost identity or cleanup");
}
`)
	if got := handle("solid-item-a").Node; got != initialA {
		t.Fatalf("keyed A identity = %d, want %d", got, initialA)
	}
	if got := handle("solid-item-c").Node; got != initialC {
		t.Fatalf("keyed C identity = %d, want %d", got, initialC)
	}
	for cycle := 1; cycle < 40; cycle++ {
		click("solid-reorder")
	}
	queueIsolated("assert-reconciliation-churn", `
if (document.getElementById("solid-list").children.length !== 3 || __solidRowCleanups !== 40) {
  throw new Error("Solid reconciliation churn did not dispose exactly one row per cycle");
}
`)
	click("solid-toggle")
	queueIsolated("assert-show-hidden", `
if (document.getElementById("solid-visible") !== null ||
    document.getElementById("solid-hidden") === null || __solidBranchCleanups !== 1 ||
    !document.getElementById("solid-dynamic").hasAttribute("hidden")) {
  throw new Error("Solid Show did not dispose the visible branch");
}
`)
	click("solid-toggle")
	queueIsolated("assert-show-visible", `
if (document.getElementById("solid-visible") === null ||
    document.getElementById("solid-hidden") !== null || __solidBranchCleanups !== 1 ||
    document.getElementById("solid-dynamic").hasAttribute("hidden")) {
  throw new Error("Solid Show did not restore the visible branch");
}
`)
	queueIsolated("position-text-selection", `
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
	queueIsolated("select-two", `document.getElementById("solid-select").value = "two";`)
	queueInput("select change", browser.InputEvent{Type: browser.InputChange, Target: handle("solid-select")})
	queueIsolated("assert-controlled-forms", `
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
	queueIsolated("assert-reactive-dom", `
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
if (__solidEffectRuns < 8 || __solidMutationRecords < 1) {
  throw new Error("Solid effects or MutationObserver did not observe churn");
}
solidDynamic = undefined;
`)
	queueIsolated("dispose-keyed-list", `
const releaseSolid = globalThis.__solidDispose;
globalThis.__solidDispose = undefined;
globalThis.__solidInitialA = undefined;
globalThis.__solidInitialB = undefined;
globalThis.__solidInitialC = undefined;
releaseSolid();
if (__solidCleanupCount !== 1 || __solidRowCleanups !== 43 || __solidBranchCleanups !== 2 ||
    document.getElementById("solid-root").firstChild !== null) {
  throw new Error("Solid keyed list teardown failed");
}
`)
	nodesBeforeCollection := page.Document().Store().LiveLen()
	if err := collect(page); err != nil {
		t.Fatalf("forced garbage collection: %v", err)
	}
	stableNodes := page.Document().Store().LiveLen()
	if stableNodes <= baselineNodes || stableNodes >= nodesBeforeCollection {
		t.Fatalf("Solid GC plateau = %d nodes from %d before collection and %d baseline", stableNodes, nodesBeforeCollection, baselineNodes)
	}
	if err := collect(page); err != nil {
		t.Fatalf("second forced garbage collection: %v", err)
	}
	if got := page.Document().Store().LiveLen(); got != stableNodes {
		t.Fatalf("Solid GC did not stabilize: second plateau %d, first %d", got, stableNodes)
	}
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
