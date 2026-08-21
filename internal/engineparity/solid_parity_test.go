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

func TestStrandRunsProductionSolidKeyedForAndShow(t *testing.T) {
	runProductionSolidKeyedForAndShow(t, nativeengine.New(nativeengine.Config{}))
}

func runProductionSolidKeyedForAndShow(t *testing.T, engine browser.Engine) {
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
    document.getElementById("solid-hidden") === null || __solidBranchCleanups !== 1) {
  throw new Error("Solid Show did not dispose the visible branch");
}
`)
	click("solid-toggle")
	queueScript("assert-show-visible", `
if (document.getElementById("solid-visible") === null ||
    document.getElementById("solid-hidden") !== null || __solidBranchCleanups !== 1) {
  throw new Error("Solid Show did not restore the visible branch");
}
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
