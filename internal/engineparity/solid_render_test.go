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

func TestStrandRunsProductionSolidCounterLifecycle(t *testing.T) {
	runProductionSolidCounterParity(t, nativeengine.New(nativeengine.Config{}))
}

func runProductionSolidCounterParity(t *testing.T, engine browser.Engine) {
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
	location, _ := url.Parse("https://gossamer.test/solid/")
	page, err := browserRuntime.NewPage(root, location)
	if err != nil {
		t.Fatal(err)
	}
	runQueuedCheckpoint := func(label string) {
		t.Helper()
		queued := page.Realm.Tasks.Len()
		if queued == 0 {
			t.Fatalf("run %s: no queued tasks", label)
		}
		for index := 0; index < queued; index++ {
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
		runQueuedCheckpoint(label)
	}

	queueScript("solid-counter", string(source))
	queueScript("assert-initial", `
if (globalThis.__solidReady !== true || typeof globalThis.__solidDispose !== "function") {
  throw new Error("production Solid fixture did not initialize");
}
globalThis.__solidCounterNode = document.getElementById("solid-counter");
if (__solidCounterNode === null || __solidCounterNode.textContent !== "Count 0") {
  throw new Error("production Solid counter did not render");
}
`)

	counterID, found := page.Document().ElementByID("solid-counter")
	if !found {
		t.Fatal("production Solid counter is missing from the native DOM")
	}
	if _, err := page.QueueInputEvent(browser.InputEvent{
		Type: browser.InputClick,
		Target: browser.NodeHandle{
			Document: page.DocumentGeneration(),
			Node:     counterID,
		},
	}); err != nil {
		t.Fatal(err)
	}
	runQueuedCheckpoint("production Solid delegated click")
	counterNode, found := page.Document().Resolve(counterID)
	if !found || htmlparser.SerializeChildren(counterNode) != "Count 1" {
		t.Fatal("production Solid delegated click did not update state in place")
	}
	queueScript("assert-update", `
if (document.getElementById("solid-counter") !== __solidCounterNode ||
    __solidCounterNode.textContent !== "Count 1") {
  throw new Error("production Solid update did not preserve DOM identity");
}
`)
	queueScript("dispose", `
globalThis.__solidDispose();
if (globalThis.__solidCleanupCount !== 1 || document.getElementById("solid-root").firstChild !== null) {
  throw new Error("production Solid disposer did not release the component tree");
}
`)

	if err := page.Close(); err != nil {
		t.Fatal(err)
	}
	if stats := browserRuntime.Ledger().Stats(); stats.LiveObjects != 0 || stats.PersistentObjects != 0 {
		t.Fatalf("production Solid teardown ownership = %#v", stats)
	}
	if err := browserRuntime.Close(); err != nil {
		t.Fatal(err)
	}
}
