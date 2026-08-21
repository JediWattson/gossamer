package engineparity

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	htmlparser "github.com/JediWattson/gossamer/internal/html"
	"github.com/JediWattson/gossamer/internal/nativeengine"
)

func TestStrandAsyncCheckpointParity(t *testing.T) {
	runAsyncCheckpointParity(t, nativeengine.New(nativeengine.Config{}))
}

func TestStrandAsyncFunctionParity(t *testing.T) {
	runAsyncFunctionParity(t, nativeengine.New(nativeengine.Config{}))
}

func runAsyncFunctionParity(t *testing.T, engine browser.Engine) {
	t.Helper()
	root, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://parity.gossamer.test/async-functions.html")
	page, err := browserRuntime.NewPage(root, location)
	if err != nil {
		t.Fatal(err)
	}
	run := func(label, source string) {
		t.Helper()
		if _, queueErr := page.QueueScript(browser.ScriptSource{URL: location.ResolveReference(&url.URL{Path: label + ".js"}).String(), Source: source}); queueErr != nil {
			t.Fatalf("queue %s: %v", label, queueErr)
		}
		for page.Realm.Tasks.Len() != 0 {
			if runErr := page.Realm.RunOne(context.Background()); runErr != nil {
				t.Fatalf("run %s: %v", label, runErr)
			}
		}
	}
	run("schedule-async-functions", `
globalThis.__asyncValues = [];
async function compute(value) { return (await Promise.resolve(value + 1)) * 2; }
const named = async function named(value) { return await compute(value); };
async function rejected() { return await Promise.reject(new Error("boom")); }
async function thrown() { return missingAsyncBinding; }
const promise = compute(2);
if (!(promise instanceof Promise)) throw new Error("async Function did not return a Promise");
promise.then(value => __asyncValues.push(value));
named(4).then(value => __asyncValues.push(value));
rejected().catch(error => __asyncValues.push(error.message));
thrown().catch(error => __asyncValues.push(error instanceof ReferenceError));
__asyncValues.push("sync");
`)
	run("assert-async-functions", `
if (__asyncValues[0] !== "sync" || __asyncValues.length !== 5 ||
    __asyncValues.indexOf(6) < 1 || __asyncValues.indexOf(10) < 1 ||
    __asyncValues.indexOf("boom") < 1 || __asyncValues.indexOf(true) < 1) {
  throw new Error("async Function results: " + __asyncValues.join(","));
}
`)
	if err := page.Close(); err != nil {
		t.Fatal(err)
	}
	if stats := browserRuntime.Ledger().Stats(); stats.LiveObjects != 0 || stats.PersistentObjects != 0 {
		t.Fatalf("async Function teardown ownership = %#v", stats)
	}
	if err := browserRuntime.Close(); err != nil {
		t.Fatal(err)
	}
}

func runAsyncCheckpointParity(t *testing.T, engine browser.Engine) {
	t.Helper()
	root, err := htmlparser.Parse(strings.NewReader(`
<!doctype html><html><head></head><body><main id="root"></main></body></html>
`))
	if err != nil {
		t.Fatal(err)
	}
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://parity.gossamer.test/async.html")
	page, err := browserRuntime.NewPage(root, location)
	if err != nil {
		t.Fatal(err)
	}

	run := func(label, source string) {
		t.Helper()
		if _, queueErr := page.QueueScript(browser.ScriptSource{
			URL: "https://parity.gossamer.test/" + label + ".js", Source: source,
		}); queueErr != nil {
			t.Fatalf("queue %s: %v", label, queueErr)
		}
		for page.Realm.Tasks.Len() != 0 {
			if runErr := page.Realm.RunOne(context.Background()); runErr != nil {
				t.Fatalf("run %s: %v", label, runErr)
			}
		}
	}

	run("schedule", `
if (performance !== window.performance || requestAnimationFrame !== window.requestAnimationFrame ||
    cancelAnimationFrame !== window.cancelAnimationFrame) {
  throw new Error("async global identity parity failed");
}
let before = performance.now();
let asyncEvents = [];
let root = document.getElementById("root");
let observer;
observer = new MutationObserver(function(records, delivered) {
  if (delivered !== observer || records.length !== 1 || records[0].type !== "childList" ||
      records[0].target !== root || records[0].addedNodes.length !== 1 ||
      records[0].addedNodes[0].id !== "added" || records[0].removedNodes.length !== 0) {
    throw new Error("MutationObserver record parity failed");
  }
  asyncEvents.push("mutation");
  queueMicrotask(function() { asyncEvents.push("observer-microtask"); });
});
observer.observe(root, {childList:true});
let added = document.createElement("b");
added.id = "added";
root.appendChild(added);
queueMicrotask(function() { asyncEvents.push("script-microtask"); });
let canceled = requestAnimationFrame(function() { asyncEvents.push("canceled-frame"); });
cancelAnimationFrame(canceled);
requestAnimationFrame(function(timestamp) {
  if (timestamp < before || timestamp > performance.now()) {
    throw new Error("animation timestamp parity failed");
  }
  asyncEvents.push("frame");
  queueMicrotask(function() { asyncEvents.push("frame-microtask"); });
});
if (asyncEvents.length !== 0) throw new Error("async work ran synchronously");
`)

	run("assert-order", `
if (asyncEvents.join(",") !== "mutation,script-microtask,observer-microtask,frame,frame-microtask") {
  throw new Error("async checkpoint order diverged: " + asyncEvents.join(","));
}
observer.disconnect();
let disconnected = document.createElement("i");
root.appendChild(disconnected);
`)

	run("take-records", `
if (asyncEvents.length !== 5) throw new Error("disconnected observer still delivered");
let manualCalls = 0;
let manual = new MutationObserver(function() { manualCalls++; });
manual.observe(root, {attributes:true, attributeOldValue:true});
root.setAttribute("data-state", "ready");
let records = manual.takeRecords();
if (records.length !== 1 || records[0].type !== "attributes" ||
    records[0].target !== root || records[0].attributeName !== "data-state" ||
    records[0].oldValue !== null) {
  throw new Error("MutationObserver takeRecords parity failed");
}
manual.disconnect();
`)

	run("take-records-assert", `
if (manualCalls !== 0) throw new Error("takeRecords callback ran at checkpoint");
`)

	if err := page.Close(); err != nil {
		t.Fatalf("close page: %v", err)
	}
	if stats := browserRuntime.Ledger().Stats(); stats.LiveObjects != 0 || stats.PersistentObjects != 0 {
		t.Fatalf("async teardown ownership = %#v", stats)
	}
	if err := browserRuntime.Close(); err != nil {
		t.Fatalf("close browser: %v", err)
	}
}
