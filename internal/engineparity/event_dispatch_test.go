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

func TestStrandConstructedEventDispatchParity(t *testing.T) {
	runConstructedEventDispatchParity(t, nativeengine.New(nativeengine.Config{}))
}

func runConstructedEventDispatchParity(t *testing.T, engine browser.Engine) {
	t.Helper()
	root, err := htmlparser.Parse(strings.NewReader(`
<!doctype html><html><body><section id="outer"><button id="target">go</button></section></body></html>
`))
	if err != nil {
		t.Fatal(err)
	}
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://parity.gossamer.test/event-dispatch.html")
	page, err := browserRuntime.NewPage(root, location)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `
const outer = document.getElementById("outer");
const target = document.getElementById("target");
let order = "";
outer.addEventListener("strand-ready", event => {
	if (event.eventPhase !== Event.CAPTURING_PHASE || event.currentTarget !== outer) {
		throw new Error("constructed Event capture state");
	}
	const path = event.composedPath();
	if (path[0] !== target || path[path.length - 1] !== window || path.indexOf(outer) < 0) {
		throw new Error("constructed Event composed path");
	}
	order += "c";
}, true);
target.addEventListener("strand-ready", event => {
  if (event.eventPhase !== Event.AT_TARGET || event.target !== target || event.currentTarget !== target) {
    throw new Error("constructed Event target state");
  }
  order += "t";
  event.preventDefault();
});
outer.addEventListener("strand-ready", event => {
  if (event.eventPhase !== Event.BUBBLING_PHASE) throw new Error("constructed Event bubble state");
  order += "b";
});
window.addEventListener("strand-ready", () => { order += "w"; });

const event = new Event("strand-ready", {bubbles: true, cancelable: true, composed: true});
if (!(event instanceof Event) || event.type !== "strand-ready" || event.target !== null ||
    event.currentTarget !== null || event.eventPhase !== Event.NONE || !event.bubbles ||
    !event.cancelable || !event.composed || event.defaultPrevented || event.isTrusted) {
  throw new Error("constructed Event initialization");
}
if (target.dispatchEvent(event) !== false || order !== "ctbw" || !event.defaultPrevented ||
	event.target !== target || event.currentTarget !== null || event.eventPhase !== Event.NONE ||
	event.composedPath().length !== 0) {
  throw new Error("constructed Event completion: " + order);
}

const passive = new Event("plain");
passive.preventDefault();
if (passive.defaultPrevented || target.dispatchEvent(passive) !== true || passive.target !== target) {
  throw new Error("non-cancelable Event behavior");
}

const detail = {task: "solid-app"};
let customSeen = false;
target.addEventListener("task-selected", event => {
  customSeen = event instanceof CustomEvent && event instanceof Event && event.detail === detail;
});
const custom = new CustomEvent("task-selected", {detail, bubbles: true});
if (custom.detail !== detail || target.dispatchEvent(custom) !== true || !customSeen) {
  throw new Error("CustomEvent detail dispatch");
}
globalThis.__crossTaskEventDetail = {task: "persisted"};
globalThis.__crossTaskEvent = new CustomEvent("cross-task", {
  detail: __crossTaskEventDetail,
  bubbles: true,
  cancelable: true
});
globalThis.__crossTaskEventSeen = false;
target.addEventListener("cross-task", event => {
  globalThis.__crossTaskEventSeen = event.detail === globalThis.__crossTaskEventDetail;
  event.preventDefault();
});
`}); err != nil {
		t.Fatal(err)
	}
	for page.Realm.Tasks.Len() != 0 {
		if err := page.Realm.RunOne(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String() + "?cross-task", Source: `
const crossTaskTarget = document.getElementById("target");
if (crossTaskTarget.dispatchEvent(globalThis.__crossTaskEvent) !== false ||
    !globalThis.__crossTaskEventSeen || !globalThis.__crossTaskEvent.defaultPrevented ||
    globalThis.__crossTaskEvent.target !== crossTaskTarget ||
    globalThis.__crossTaskEvent.detail !== globalThis.__crossTaskEventDetail) {
  throw new Error("constructed Event did not survive the Realm task boundary");
}
`}); err != nil {
		t.Fatal(err)
	}
	for page.Realm.Tasks.Len() != 0 {
		if err := page.Realm.RunOne(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if err := page.Close(); err != nil {
		t.Fatal(err)
	}
	if stats := browserRuntime.Ledger().Stats(); stats.LiveObjects != 0 || stats.PersistentObjects != 0 {
		t.Fatalf("constructed Event teardown ownership = %#v", stats)
	}
	if err := browserRuntime.Close(); err != nil {
		t.Fatal(err)
	}
}
