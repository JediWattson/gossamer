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
