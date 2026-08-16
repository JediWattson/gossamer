package nativeengine_test

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	htmlparser "github.com/JediWattson/gossamer/internal/html"
	"github.com/JediWattson/gossamer/internal/nativeengine"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func TestBrowserTasksExecuteNativeScriptsAcrossRealmCheckpoints(t *testing.T) {
	t.Parallel()

	scriptEngine := nativeengine.New(nativeengine.Config{})
	browserRuntime, err := browser.NewWithEngine(scriptEngine)
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	location, _ := url.Parse("https://gossamer.test/native/")
	page, err := browserRuntime.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	scriptRealm, ok := scriptEngine.LatestRealm()
	if !ok {
		t.Fatal("native engine did not create a JSRealm")
	}

	run := func(label, source string) {
		t.Helper()
		if _, err := page.QueueScript(browser.ScriptSource{URL: location.ResolveReference(&url.URL{Path: label + ".js"}).String(), Source: source}); err != nil {
			t.Fatalf("QueueScript(%s): %v", label, err)
		}
		if err := page.Realm.RunOne(context.Background()); err != nil {
			t.Fatalf("RunOne(%s): %v", label, err)
		}
	}

	run("first", `
let counter = 40;
function add(value) { counter = counter + value; }
queueMicrotask(function () { add(2); });
`)
	firstProfile := scriptRealm.Profile()
	firstStats := page.Realm.Store().Stats()
	run("second", `
if (counter !== 42) { throw new Error("global or microtask state was lost"); }
Promise.resolve(5).then(add);
`)
	secondProfile := scriptRealm.Profile()
	secondStats := page.Realm.Store().Stats()
	run("third", `
if (counter !== 47) { throw new Error("Promise checkpoint was not retained"); }
counter;
`)
	thirdStats := page.Realm.Store().Stats()
	if firstProfile.PersistentRegion == 0 || secondProfile.PersistentRegion == firstProfile.PersistentRegion {
		t.Fatalf("persistent region did not rotate: first=%#v second=%#v", firstProfile, secondProfile)
	}
	released, err := page.Realm.Store().Region(firstProfile.PersistentRegion)
	if err != nil {
		t.Fatal(err)
	}
	if released.State != memory.RegionDestroyed {
		t.Fatalf("replaced persistent region state = %v, want destroyed", released.State)
	}

	profile := scriptRealm.Profile()
	if profile.Evaluations != 3 || profile.Checkpoints != 3 || profile.ActiveTask != 0 || profile.PersistentRegion == 0 {
		t.Fatalf("native Realm profile = %#v", profile)
	}
	if firstStats.LiveRegions != secondStats.LiveRegions || secondStats.LiveRegions != thirdStats.LiveRegions {
		t.Fatalf("task checkpoints retained regions: first=%#v second=%#v third=%#v", firstStats, secondStats, thirdStats)
	}
	if secondStats.LiveSlots != thirdStats.LiveSlots {
		t.Fatalf("unreachable script graph survived checkpoint: second=%#v third=%#v", secondStats, thirdStats)
	}
	if err := page.Realm.Store().CheckInvariants(); err != nil {
		t.Fatal(err)
	}

	persistentRegion := profile.PersistentRegion
	store := page.Realm.Store()
	if err := page.Close(); err != nil {
		t.Fatal(err)
	}
	region, err := store.Region(persistentRegion)
	if err != nil {
		t.Fatal(err)
	}
	if region.State != memory.RegionDestroyed {
		t.Fatalf("persistent region state after Page.Close = %v, want destroyed", region.State)
	}
	engineProfile := scriptEngine.Profile()
	if engineProfile.RealmsCreated != 1 || engineProfile.RealmsClosed != 1 || engineProfile.LiveRealms != 0 || engineProfile.Evaluations != 3 || engineProfile.Checkpoints != 3 {
		t.Fatalf("native Engine profile = %#v", engineProfile)
	}
}

func TestSymbolRegistryAndKeysSurviveRealmCheckpoints(t *testing.T) {
	t.Parallel()

	engine := nativeengine.New(nativeengine.Config{})
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	location, _ := url.Parse("https://gossamer.test/symbols/")
	page, err := browserRuntime.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	defer page.Close()

	run := func(label, source string) {
		t.Helper()
		if _, err := page.QueueScript(browser.ScriptSource{URL: label + ".js", Source: source}); err != nil {
			t.Fatalf("QueueScript(%s): %v", label, err)
		}
		if err := page.Realm.RunOne(context.Background()); err != nil {
			t.Fatalf("RunOne(%s): %v", label, err)
		}
	}
	run("create", `
let registrySymbol = Symbol.for("gossamer.checkpoint");
let localSymbol = Symbol("local");
let symbolTarget = {};
symbolTarget[registrySymbol] = 41;
symbolTarget[localSymbol] = 42;
`)
	run("verify", `
if (Symbol.for("gossamer.checkpoint") !== registrySymbol ||
    symbolTarget[Symbol.for("gossamer.checkpoint")] !== 41 ||
    symbolTarget[localSymbol] !== 42 ||
    Symbol.iterator !== Symbol.iterator) {
  throw new Error("Symbol identity was lost across the Realm checkpoint");
}
`)
	if err := page.Realm.Store().CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func TestNativeRealmRejectsCallsWithoutRuntimeTaskHost(t *testing.T) {
	t.Parallel()

	engine := nativeengine.New(nativeengine.Config{})
	defer engine.Close()
	created, err := engine.NewRealm()
	if err != nil {
		t.Fatal(err)
	}
	realm := created.(*nativeengine.Realm)
	err = realm.Evaluate(nil, browser.ScriptSource{URL: "missing-host.js", Source: "1 + 1;"})
	if !errors.Is(err, nativeengine.ErrNativeTaskHost) {
		t.Fatalf("Evaluate error = %v, want ErrNativeTaskHost", err)
	}
}

func TestNativeRealmBindsCanonicalGoDOMWrappersAcrossTasks(t *testing.T) {
	t.Parallel()

	root, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><head></head><body></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	scriptEngine := nativeengine.New(nativeengine.Config{})
	browserRuntime, err := browser.NewWithEngine(scriptEngine)
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	location, _ := url.Parse("https://gossamer.test/native-dom/")
	page, err := browserRuntime.NewPage(root, location)
	if err != nil {
		t.Fatal(err)
	}
	defer page.Close()

	run := func(label, source string) {
		t.Helper()
		if _, err := page.QueueScript(browser.ScriptSource{URL: label + ".js", Source: source}); err != nil {
			t.Fatalf("QueueScript(%s): %v", label, err)
		}
		if err := page.Realm.RunOne(context.Background()); err != nil {
			t.Fatalf("RunOne(%s): %v", label, err)
		}
	}

	run("create", `
if (window !== self || window.document !== document) {
  throw new Error("window identity was not canonical");
}
let body = document.body;
if (body.nodeName !== "BODY" || body.parentNode !== document.documentElement) {
  throw new Error("document traversal failed");
}
let element = document.createElement("div");
element.id = "native";
element.setAttribute("data-state", "ready");
element.textContent = "hello";
body.appendChild(element);
if (document.getElementById("native") !== element || body.lastChild !== element) {
  throw new Error("wrapper identity was not canonical");
}
let suffix = document.createTextNode("!");
element.appendChild(suffix);
queueMicrotask(function () { element.textContent = element.textContent + " world"; });
`)

	run("reuse", `
let element = document.querySelector("#native");
if (element !== document.getElementById("native")) {
  throw new Error("wrapper identity was lost across tasks");
}
if (element.textContent !== "hello! world" || element.getAttribute("data-state") !== "ready") {
  throw new Error("Go DOM state was not retained");
}
if (!document.body.contains(element) || !element.matches("#native") || element.closest("body") !== document.body) {
  throw new Error("DOM queries failed");
}
let clone = element.cloneNode(true);
clone.id = "clone";
document.body.insertBefore(clone, element);
if (document.querySelectorAll("div").length !== 2 || clone.nextSibling !== element) {
  throw new Error("DOM clone or insertion failed");
}
document.body.removeChild(clone);
if (document.getElementById("clone") !== null) {
  throw new Error("DOM removal failed");
}
`)

	if err := page.Realm.Store().CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func TestNativeRealmRetainsTimerCallbacksAcrossTaskCheckpoints(t *testing.T) {
	t.Parallel()

	scriptEngine := nativeengine.New(nativeengine.Config{})
	browserRuntime, err := browser.NewWithEngine(scriptEngine)
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	location, _ := url.Parse("https://gossamer.test/native-timer/")
	page, err := browserRuntime.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	defer page.Close()

	if _, err := page.QueueScript(browser.ScriptSource{URL: "schedule.js", Source: `
let timerValue = 0;
let canceled = setTimeout(function () { timerValue = 1000; }, 60000);
clearTimeout(canceled);
let captured = 40;
setTimeout(function () {
  timerValue = captured + 2;
  queueMicrotask(function () { timerValue = timerValue + 1; });
}, 0);
`}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}

	timerContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := page.Realm.RunOne(timerContext); err != nil {
		t.Fatalf("run timer callback: %v", err)
	}
	if _, err := page.QueueScript(browser.ScriptSource{URL: "verify.js", Source: `
if (timerValue !== 43) {
  throw new Error("timer closure or microtask checkpoint was lost");
}
`}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.Store().CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func TestNativeRealmDispatchesGoDOMEventsToPersistentListeners(t *testing.T) {
	t.Parallel()

	root, err := htmlparser.Parse(strings.NewReader(`
<!doctype html><html><body><section id="outer"><button id="target">go</button></section></body></html>
`))
	if err != nil {
		t.Fatal(err)
	}
	scriptEngine := nativeengine.New(nativeengine.Config{})
	browserRuntime, err := browser.NewWithEngine(scriptEngine)
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	location, _ := url.Parse("https://gossamer.test/native-events/")
	page, err := browserRuntime.NewPage(root, location)
	if err != nil {
		t.Fatal(err)
	}
	defer page.Close()

	if _, err := page.QueueScript(browser.ScriptSource{URL: "listeners.js", Source: `
let order = "";
let microtasks = 0;
let passiveCanceled = true;
let lastEvent = null;
let delegatedCurrentTarget = null;
let target = document.getElementById("target");
let outer = document.getElementById("outer");
window.addEventListener("click", function (event) {
  if (this !== window || event.eventPhase !== 1) throw new Error("window capture receiver or phase");
  order = order + "w";
}, true);
document.addEventListener("click", function (event) {
  if (this !== document || event.eventPhase !== 1) throw new Error("document capture receiver or phase");
  order = order + "d";
}, true);
outer.addEventListener("click", function () { order = order + "o"; }, {capture: true});
target.addEventListener("click", function (event) {
  if (this !== target || event.target !== target || event.currentTarget !== target || event.eventPhase !== 2) {
    throw new Error("target capture identity or phase");
  }
  order = order + "c";
}, true);
target.addEventListener("click", function (event) {

  event.preventDefault();
  passiveCanceled = event.defaultPrevented;
}, {passive: true});
target.addEventListener("click", function (event) {
  event.preventDefault();
  if (!event.defaultPrevented) throw new Error("preventDefault did not update the Event");
  lastEvent = event;
  order = order + "t";
  queueMicrotask(function () { microtasks = microtasks + 1; });
});
outer.addEventListener("click", function () { order = order + "b"; }, {once: true});
document.addEventListener("click", function (event) {
  Object.defineProperty(event, "currentTarget", {
    configurable: true,
    get: function () { return delegatedCurrentTarget; }
  });
  delegatedCurrentTarget = document;
  if (event.currentTarget !== document) throw new Error("delegated currentTarget getter");
  delegatedCurrentTarget = null;
});
window.addEventListener("click", function (event) {
  if (event.eventPhase !== 3) throw new Error("window bubble phase");
  order = order + "W";
});
function removed() { throw new Error("removed listener ran"); }
target.addEventListener("click", removed);
target.removeEventListener("click", removed);
target.addEventListener("keydown", function (event) {
  order = order + "k";
  lastEvent = event;
  event.stopPropagation();
});
target.addEventListener("keydown", function () { order = order + "s"; });
outer.addEventListener("keydown", function () { throw new Error("stopped event reached ancestor"); });
`}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}

	targetID, found := page.Document().ElementByID("target")
	if !found {
		t.Fatal("target node is missing")
	}
	target := browser.NodeHandle{Document: page.DocumentGeneration(), Node: targetID}
	for index := 0; index < 2; index++ {
		if _, err := page.QueueInputEvent(browser.InputEvent{Type: browser.InputClick, Target: target}); err != nil {
			t.Fatal(err)
		}
		if err := page.Realm.RunOne(context.Background()); err != nil {
			t.Fatalf("dispatch click %d: %v", index+1, err)
		}
	}
	if _, err := page.QueueInputEvent(browser.InputEvent{Type: browser.InputKeyDown, Target: target, Key: "Enter", Code: "Enter"}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("dispatch keydown: %v", err)
	}

	if _, err := page.QueueScript(browser.ScriptSource{URL: "verify-events.js", Source: `
if (order !== "wdoctbWwdoctWks") throw new Error("event propagation order: " + order);
if (microtasks !== 2) throw new Error("listener microtasks did not drain");
if (passiveCanceled) throw new Error("passive listener canceled an event");
if (lastEvent.currentTarget !== null || lastEvent.eventPhase !== 0) {
  throw new Error("completed Event retained dispatch-only state");
}
`}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.Store().CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func TestNativeBrowserBindingsReleaseCompleteRealmGraphOnClose(t *testing.T) {
	t.Parallel()

	root, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	scriptEngine := nativeengine.New(nativeengine.Config{})
	browserRuntime, err := browser.NewWithEngine(scriptEngine)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://gossamer.test/native-teardown/")
	page, err := browserRuntime.NewPage(root, location)
	if err != nil {
		t.Fatal(err)
	}
	store := page.Realm.Store()
	ledger := browserRuntime.Ledger()

	if _, err := page.QueueScript(browser.ScriptSource{URL: "retain.js", Source: `
let detached = document.createElement("div");
detached.textContent = "retained";
detached.addEventListener("click", function () { detached.textContent = "fired"; });
let pendingTimer = setTimeout(function () { detached.textContent = "timer"; }, 60000);
`}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := store.Stats()
	if before.LiveRegions == 0 || before.LiveSlots == 0 || before.LiveFunctions == 0 || before.LiveHostObjects == 0 {
		t.Fatalf("native graph was not retained before close: %#v", before)
	}
	if err := browserRuntime.Close(); err != nil {
		t.Fatal(err)
	}
	after := store.Stats()
	if after.LiveRegions != 0 || after.LiveSlots != 0 || after.LiveBytes != 0 {
		t.Fatalf("native graph survived Browser.Close: %#v", after)
	}
	if stats := ledger.Stats(); stats.LiveObjects != 0 || stats.PersistentObjects != 0 {
		t.Fatalf("semantic ownership survived Browser.Close: %#v", stats)
	}
	profile := scriptEngine.Profile()
	if profile.LiveRealms != 0 || profile.RealmsCreated != 1 || profile.RealmsClosed != 1 {
		t.Fatalf("native engine profile after close = %#v", profile)
	}
}
