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

func TestStrandWeakWrapperParity(t *testing.T) {
	engine := nativeengine.New(nativeengine.Config{})
	runWeakWrapperParity(t, engine, func(page *browser.Page) error {
		realm, ok := engine.LatestRealm()
		if !ok {
			return nativeengine.ErrRealmClosed
		}
		return realm.CollectGarbage(page)
	})
}

func TestStrandResizeObserverTargetLifetimeParity(t *testing.T) {
	engine := nativeengine.New(nativeengine.Config{})
	runResizeObserverTargetLifetimeParity(t, engine, func(page *browser.Page) error {
		realm, ok := engine.LatestRealm()
		if !ok {
			return nativeengine.ErrRealmClosed
		}
		return realm.CollectGarbage(page)
	})
}

func runResizeObserverTargetLifetimeParity(t *testing.T, engine browser.Engine, collect func(*browser.Page) error) {
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
	location, _ := url.Parse("https://parity.gossamer.test/observer-targets.html")
	page, err := browserRuntime.NewPage(root, location)
	if err != nil {
		t.Fatal(err)
	}
	baselineNodes := page.Document().Store().LiveLen()

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

	run("observe-detached-targets", `
globalThis.__targetDeliveries = [];
globalThis.__targetObserver = new ResizeObserver(function(entries) {
  __targetDeliveries.push(entries.length);
});
(function () {
  const first = document.createElement("div");
  const second = document.createElement("aside");
  first.style.width = "40px";
  second.style.width = "80px";
  __targetObserver.observe(first);
  __targetObserver.observe(second);
})();
`)
	if err := collect(page); err != nil {
		t.Fatalf("collect live observer targets: %v", err)
	}
	if got := page.Document().Store().LiveLen(); got != baselineNodes+2 {
		t.Fatalf("live ResizeObserver retained %d native nodes, want %d", got, baselineNodes+2)
	}
	run("use-retained-observer-targets", `
if (__targetDeliveries.join(",") !== "2") {
  throw new Error("detached ResizeObserver targets were not delivered: " + __targetDeliveries.join(","));
}
`)
	run("disconnect-retained-observer-targets", `
__targetObserver.disconnect();
`)
	if err := collect(page); err != nil {
		t.Fatalf("collect disconnected observer targets: %v", err)
	}
	if got := page.Document().Store().LiveLen(); got != baselineNodes {
		t.Fatalf("disconnected ResizeObserver retained %d native nodes, want %d", got, baselineNodes)
	}

	run("observe-detached-mutation-target", `
globalThis.__mutationDeliveries = [];
globalThis.__targetMutationObserver = new MutationObserver(function(records) {
  __mutationDeliveries.push(records.length);
});
(function () {
  const target = document.createElement("section");
  __targetMutationObserver.observe(target, {childList:true, subtree:true});
  target.appendChild(document.createElement("i"));
})();
`)
	if err := collect(page); err != nil {
		t.Fatalf("collect live mutation observer target: %v", err)
	}
	if got := page.Document().Store().LiveLen(); got != baselineNodes+2 {
		t.Fatalf("live MutationObserver retained %d native nodes, want %d", got, baselineNodes+2)
	}
	run("scan-with-retained-mutation-target", `
if (__mutationDeliveries.join(",") !== "1") {
  throw new Error("detached MutationObserver target was not delivered: " + __mutationDeliveries.join(","));
}
document.getElementById("root").appendChild(document.createElement("b"));
`)
	run("disconnect-retained-mutation-target", `
__targetMutationObserver.disconnect();
`)
	if err := collect(page); err != nil {
		t.Fatalf("collect disconnected mutation observer target: %v", err)
	}
	if got := page.Document().Store().LiveLen(); got != baselineNodes+1 {
		t.Fatalf("disconnected MutationObserver retained %d native nodes, want %d", got, baselineNodes+1)
	}

	run("orphan-observer-target", `
(function () {
  const target = document.createElement("footer");
  const observer = new ResizeObserver(function() {});
  observer.observe(target);
})();
`)
	if err := collect(page); err != nil {
		t.Fatalf("collect orphan observer and target: %v", err)
	}
	if got := page.Document().Store().LiveLen(); got != baselineNodes+1 {
		t.Fatalf("collected observer retained %d native nodes, want %d", got, baselineNodes+1)
	}

	if err := page.Close(); err != nil {
		t.Fatalf("close page: %v", err)
	}
	if stats := browserRuntime.Ledger().Stats(); stats.LiveObjects != 0 || stats.PersistentObjects != 0 {
		t.Fatalf("observer-target teardown ownership = %#v", stats)
	}
	if err := browserRuntime.Close(); err != nil {
		t.Fatalf("close browser: %v", err)
	}
}

func runWeakWrapperParity(t *testing.T, engine browser.Engine, collect func(*browser.Page) error) {
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
	location, _ := url.Parse("https://parity.gossamer.test/wrappers.html")
	page, err := browserRuntime.NewPage(root, location)
	if err != nil {
		t.Fatal(err)
	}
	baselineNodes := page.Document().Store().LiveLen()

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

	run("hold-wrapper", `
var keptWrapper = document.createElement("div");
keptWrapper.id = "kept";
var keptAlias = keptWrapper;
function allocateDoomedWrapper() {
  var doomed = document.createElement("aside");
  doomed.id = "doomed";
}
allocateDoomedWrapper();
if (keptWrapper !== keptAlias) throw new Error("initial wrapper identity diverged");
`)
	if got := page.Document().Store().LiveLen(); got != baselineNodes+2 {
		t.Fatalf("detached nodes before collection = %d, want %d", got, baselineNodes+2)
	}
	if err := collect(page); err != nil {
		t.Fatalf("collect with wrapper alias: %v", err)
	}
	if got := page.Document().Store().LiveLen(); got != baselineNodes+1 {
		t.Fatalf("detached nodes with one live wrapper = %d, want %d", got, baselineNodes+1)
	}
	run("assert-wrapper", `
if (keptWrapper !== keptAlias || keptWrapper.id !== "kept") {
  throw new Error("canonical wrapper identity did not survive GC");
}
keptWrapper = undefined;
keptAlias = undefined;
`)
	if err := collect(page); err != nil {
		t.Fatalf("collect released wrapper: %v", err)
	}
	if got := page.Document().Store().LiveLen(); got != baselineNodes {
		t.Fatalf("detached nodes after wrapper release = %d, want %d", got, baselineNodes)
	}

	run("hold-collection", `
var heldChildren;
function allocateHeldCollection() {
  var parent = document.createElement("section");
  var child = document.createElement("i");
  child.id = "held-child";
  parent.appendChild(child);
  heldChildren = parent.childNodes;
}
allocateHeldCollection();
`)
	if err := collect(page); err != nil {
		t.Fatalf("collect with held collection: %v", err)
	}
	if got := page.Document().Store().LiveLen(); got != baselineNodes+2 {
		t.Fatalf("held collection native component = %d nodes, want %d", got, baselineNodes+2)
	}
	run("assert-collection-length", `if (heldChildren.length !== 1) throw new Error("held collection length");`)
	run("assert-collection-child", `if (heldChildren[0].id !== "held-child") throw new Error("held collection child");`)
	run("release-collection", `
if (heldChildren[0].parentNode.nodeName !== "SECTION") throw new Error("held collection parent");
heldChildren = undefined;
`)
	if err := collect(page); err != nil {
		t.Fatalf("collect released collection: %v", err)
	}
	if got := page.Document().Store().LiveLen(); got != baselineNodes {
		t.Fatalf("native nodes after collection release = %d, want %d", got, baselineNodes)
	}

	if err := page.Close(); err != nil {
		t.Fatalf("close page: %v", err)
	}
	if stats := browserRuntime.Ledger().Stats(); stats.LiveObjects != 0 || stats.PersistentObjects != 0 {
		t.Fatalf("wrapper teardown ownership = %#v", stats)
	}
	if err := browserRuntime.Close(); err != nil {
		t.Fatalf("close browser: %v", err)
	}
}
