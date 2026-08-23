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

func TestStrandVirtualizedResizeObserverParity(t *testing.T) {
	runVirtualizedResizeObserverParity(t, nativeengine.New(nativeengine.Config{}))
}

func TestStrandObserverExceptionBoundaryParity(t *testing.T) {
	runObserverExceptionBoundaryParity(t, nativeengine.New(nativeengine.Config{}))
}

func runObserverExceptionBoundaryParity(t *testing.T, engine browser.Engine) {
	t.Helper()
	root, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body><div id="target"></div></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	page, err := browserRuntime.NewPage(root, &url.URL{Scheme: "https", Host: "parity.gossamer.test"})
	if err != nil {
		t.Fatal(err)
	}
	defer page.Close()
	run := func(label, source string) {
		t.Helper()
		if _, err := page.QueueScript(browser.ScriptSource{URL: label + ".js", Source: source}); err != nil {
			t.Fatal(err)
		}
		for page.Realm.Tasks.Len() != 0 {
			if err := page.Realm.RunOne(context.Background()); err != nil {
				t.Fatalf("run %s: %v", label, err)
			}
		}
	}
	run("observer-throw", `
globalThis.__observerAfterThrow = [];
globalThis.__throwingResizeObserver = new ResizeObserver(function () {
  throw new Error("reported resize observer exception");
});
globalThis.__followingResizeObserver = new ResizeObserver(function () {
  __observerAfterThrow.push("following");
});
const observerTarget = document.getElementById("target");
__throwingResizeObserver.observe(observerTarget);
__followingResizeObserver.observe(observerTarget);
`)
	run("observer-continued", `
if (__observerAfterThrow.join(",") !== "following") {
  throw new Error("observer exception stopped later delivery: " + __observerAfterThrow.join(","));
}
__throwingResizeObserver.disconnect();
__followingResizeObserver.disconnect();
`)
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
globalThis.__finallyValues = [];
async function compute(value) { return (await Promise.resolve(value + 1)) * 2; }
const named = async function named(value) { return await compute(value); };
async function rejected() { return await Promise.reject(new Error("boom")); }
async function thrown() { return missingAsyncBinding; }
async function nestedSession() {
  const session = await Promise.resolve({ userUuid: "anon-id", isAnonymous: true });
  if (!session.isAnonymous) return session;
  try {
    if (!(await Promise.resolve(false))) return session;
    return await Promise.resolve({ userUuid: "member-id" });
  } catch (error) {
    return session;
  }
}
const promise = compute(2);
if (!(promise instanceof Promise)) throw new Error("async Function did not return a Promise");
promise.then(value => __asyncValues.push(value));
named(4).then(value => __asyncValues.push(value));
rejected().catch(error => __asyncValues.push(error.message));
thrown().catch(error => __asyncValues.push(error instanceof ReferenceError));
nestedSession().then(session => __asyncValues.push(session?.userUuid));
Promise.resolve("kept")
  .finally(() => Promise.resolve(__finallyValues.push("fulfilled-cleanup")))
  .then(value => __finallyValues.push(value));
Promise.reject("reason")
  .finally(() => Promise.resolve(__finallyValues.push("rejected-cleanup")))
  .catch(reason => __finallyValues.push(reason));
__asyncValues.push("sync");
`)
	run("assert-async-functions", `
if (__asyncValues[0] !== "sync" || __asyncValues.length !== 6 ||
    __asyncValues.indexOf(6) < 1 || __asyncValues.indexOf(10) < 1 ||
    __asyncValues.indexOf("boom") < 1 || __asyncValues.indexOf(true) < 1 ||
    __asyncValues.indexOf("anon-id") < 1) {
  throw new Error("async Function results: " + __asyncValues.join(","));
}
if (__finallyValues.join(",") !== "fulfilled-cleanup,rejected-cleanup,kept,reason") {
  throw new Error("Promise.finally results: " + __finallyValues.join(","));
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
let resizeObserver;
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
resizeObserver = new ResizeObserver(function(entries, delivered) {
  if (delivered !== resizeObserver || entries.length !== 1 || entries[0].target !== root ||
      entries[0].contentRect.width < 0 || entries[0].contentRect.height < 0 ||
      entries[0].contentBoxSize.length !== 1 || entries[0].borderBoxSize.length !== 1) {
    throw new Error("ResizeObserver entry parity failed");
  }
  asyncEvents.push("resize");
});
resizeObserver.observe(root);
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
if (asyncEvents.join(",") !== "mutation,resize,script-microtask,observer-microtask,frame,frame-microtask") {
  throw new Error("async checkpoint order diverged: " + asyncEvents.join(","));
}
observer.disconnect();
resizeObserver.unobserve(root);
resizeObserver.disconnect();
let disconnected = document.createElement("i");
root.appendChild(disconnected);
`)

	run("take-records", `
if (asyncEvents.length !== 6) throw new Error("disconnected observer still delivered");
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

	run("large-mutation-checkpoint", `
let largeMutationCalls = 0;
let largeMutationRecords = 0;
let largeObserver = new MutationObserver(function(records) {
  largeMutationCalls++;
  largeMutationRecords += records.length;
});
largeObserver.observe(root, {attributes:true});
for (let index = 0; index < 4353; index++) {
  root.setAttribute("data-large-mutation", String(index));
}
`)

	run("large-mutation-assert", `
if (largeMutationCalls !== 1 || largeMutationRecords !== 4353 ||
    root.getAttribute("data-large-mutation") !== "4352") {
  throw new Error("large MutationObserver checkpoint lost records: " +
    largeMutationCalls + "," + largeMutationRecords);
}
largeObserver.disconnect();
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

func runVirtualizedResizeObserverParity(t *testing.T, engine browser.Engine) {
	t.Helper()
	root, err := htmlparser.Parse(strings.NewReader(`
<!doctype html><html><head></head><body style="margin:0">
<div id="scroller" style="position:relative;width:320px;height:200px;overflow:auto">
  <div id="canvas" style="position:relative;height:9600px">
    <div id="row" style="position:absolute;top:480px;left:0;width:100%;height:96px">row</div>
  </div>
</div>
</body></html>
`))
	if err != nil {
		t.Fatal(err)
	}
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://parity.gossamer.test/virtualized-resize.html")
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

	run("observe-virtualized-layout", `
let scroller = document.getElementById("scroller");
let canvas = document.getElementById("canvas");
let row = document.getElementById("row");
if (scroller.clientWidth !== 320 || scroller.clientHeight !== 200 ||
    scroller.scrollHeight < 9600 || canvas.clientHeight !== 9600 ||
    row.clientWidth !== 320 || row.clientHeight !== 96 ||
    row.getBoundingClientRect().height !== 96) {
  throw new Error("initial virtualized geometry parity failed");
}
globalThis.__virtualizedResizeEntries = [];
globalThis.__virtualizedResizeObserver = new ResizeObserver(function(entries, delivered) {
  if (delivered !== __virtualizedResizeObserver) {
    throw new Error("virtualized ResizeObserver identity failed");
  }
  __virtualizedResizeEntries.push(entries.map(function(entry) {
    return entry.target.id + ":" + entry.contentRect.width + "x" + entry.contentRect.height;
  }).join(","));
});
__virtualizedResizeObserver.observe(scroller);
__virtualizedResizeObserver.observe(canvas);
__virtualizedResizeObserver.observe(row);
`)
	run("assert-initial-virtualized-measurement", `
if (__virtualizedResizeEntries.length !== 1 ||
    __virtualizedResizeEntries[0] !== "scroller:320x200,canvas:320x9600,row:320x96") {
  throw new Error("initial virtualized ResizeObserver delivery failed: " + __virtualizedResizeEntries.join("|"));
}
canvas.style.height = "12400px";
row.style.height = "128px";
`)
	run("assert-mutated-virtualized-measurement", `
if (__virtualizedResizeEntries.length !== 2 ||
    __virtualizedResizeEntries[1] !== "canvas:320x12400,row:320x128" ||
    scroller.scrollHeight < 12400 || row.getBoundingClientRect().height !== 128) {
  throw new Error("mutated virtualized ResizeObserver delivery failed: " + __virtualizedResizeEntries.join("|"));
}
__virtualizedResizeObserver.disconnect();
`)

	if err := page.Close(); err != nil {
		t.Fatalf("close page: %v", err)
	}
	if stats := browserRuntime.Ledger().Stats(); stats.LiveObjects != 0 || stats.PersistentObjects != 0 {
		t.Fatalf("virtualized ResizeObserver teardown ownership = %#v", stats)
	}
	if err := browserRuntime.Close(); err != nil {
		t.Fatalf("close browser: %v", err)
	}
}
