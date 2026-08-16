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

func TestStrandRendersProductionReactElement(t *testing.T) {
	runProductionReactRenderParity(t, nativeengine.New(nativeengine.Config{}))
}

func runProductionReactRenderParity(t *testing.T, engine browser.Engine) {
	runProductionReactParity(t, engine, true)
}

func runProductionReactRootParity(t *testing.T, engine browser.Engine) {
	runProductionReactParity(t, engine, false)
}

func runProductionReactParity(t *testing.T, engine browser.Engine, render bool) {
	t.Helper()
	source, err := os.ReadFile("../v8engine/testdata/react-19.2.7.production.js")
	if err != nil {
		t.Fatal(err)
	}
	root, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body><main id="root"></main></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://gossamer.test/react/")
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
			if err := page.Realm.RunOne(context.Background()); err != nil {
				t.Fatalf("run %s: %v", label, err)
			}
		}
	}
	preflightSource := `
if (typeof document !== "object" || typeof document.getElementById !== "function") {
  throw new Error("document facade was not initialized");
}
`
	if render {
		preflightSource += `
// Keep this fixture on React's timer fallback. The separate stock-V8 async
// checkpoint gate owns queueMicrotask and Promise transport parity.
globalThis.queueMicrotask = undefined;
queueMicrotask = undefined;
globalThis.Promise = undefined;
Promise = undefined;
`
	}
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.ResolveReference(&url.URL{Path: "preflight.js"}).String(), Source: preflightSource}); err != nil {
		t.Fatal(err)
	}
	runQueuedCheckpoint("document preflight")
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.ResolveReference(&url.URL{Path: "react.js"}).String(), Source: string(source)}); err != nil {
		t.Fatal(err)
	}
	runQueuedCheckpoint("production React bundle")
	assertionSource := `
if (typeof React !== "object" || typeof React.createElement !== "function" ||
    typeof ReactDOM !== "object" || typeof ReactDOM.createRoot !== "function" ||
    typeof ReactDOM.flushSync !== "function") {
  throw new Error("production React globals were not initialized");
}

globalThis.__strandReactRoot = ReactDOM.createRoot(document.getElementById("root"));
if (typeof __strandReactRoot !== "object" || typeof __strandReactRoot.render !== "function") {
  throw new Error("production React root was not created");
}
`
	if render {
		assertionSource += `
ReactDOM.flushSync(() => {
  __strandReactRoot.render(React.createElement("p", { id: "message" }, "Hello from Strand"));
});
const message = document.getElementById("message");
if (message === null || message.textContent !== "Hello from Strand") {
  throw new Error("production React element did not commit to the native DOM");
}

let updateCounter;
function Counter() {
  const state = React.useState(0);
  updateCounter = state[1];
  return React.createElement("button", {
    id: "counter",
    onClick: () => ReactDOM.flushSync(() => updateCounter(value => value + 1))
  }, "Count " + state[0]);
}
ReactDOM.flushSync(() => {
  __strandReactRoot.render(React.createElement(Counter));
});
const counter = document.getElementById("counter");
if (counter === null || counter.textContent !== "Count 0") {
  throw new Error("production React initial component state did not commit");
}
ReactDOM.flushSync(() => updateCounter(value => value + 1));
if (counter.textContent !== "Count 1" || document.getElementById("counter") !== counter) {
  throw new Error("production React state update did not reconcile in place");
}
`
	}
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.ResolveReference(&url.URL{Path: "assert.js"}).String(), Source: assertionSource}); err != nil {
		t.Fatal(err)
	}
	runQueuedCheckpoint("production React assertion")
	if !render {
		if err := closeProductionReactPage(t, browserRuntime, page, "root"); err != nil {
			t.Fatal(err)
		}
		return
	}
	counterID, found := page.Document().ElementByID("counter")
	if !found {
		t.Fatal("production React counter is missing from the native DOM")
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
	runQueuedCheckpoint("production React delegated click")
	counterNode, found := page.Document().Resolve(counterID)
	if !found || htmlparser.SerializeChildren(counterNode) != "Count 2" {
		t.Fatal("production React delegated click did not update state")
	}
	if err := closeProductionReactPage(t, browserRuntime, page, "render"); err != nil {
		t.Fatal(err)
	}
}

func closeProductionReactPage(t *testing.T, browserRuntime *browser.Browser, page *browser.Page, label string) error {
	t.Helper()
	if err := page.Close(); err != nil {
		return err
	}
	if stats := browserRuntime.Ledger().Stats(); stats.LiveObjects != 0 || stats.PersistentObjects != 0 {
		t.Fatalf("production React %s teardown ownership = %#v", label, stats)
	}
	return browserRuntime.Close()
}
