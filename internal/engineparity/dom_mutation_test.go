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

func TestStrandDOMMutationAndExceptionParity(t *testing.T) {
	engine := nativeengine.New(nativeengine.Config{})
	runDOMMutationAndExceptionParity(t, engine, func(page *browser.Page) error {
		realm, ok := engine.LatestRealm()
		if !ok {
			return nativeengine.ErrRealmClosed
		}
		return realm.CollectGarbage(page)
	})
}

func runDOMMutationAndExceptionParity(t *testing.T, engine browser.Engine, collect func(*browser.Page) error) {
	t.Helper()
	root, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body><main id="root"><p id="first">A</p><p id="second">B</p></main></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://parity.gossamer.test/dom-mutations.html")
	page, err := browserRuntime.NewPage(root, location)
	if err != nil {
		t.Fatal(err)
	}
	run := func(label, source string) {
		t.Helper()
		if _, queueErr := page.QueueScript(browser.ScriptSource{
			URL: location.ResolveReference(&url.URL{Path: label + ".js"}).String(), Source: source,
		}); queueErr != nil {
			t.Fatalf("queue %s: %v", label, queueErr)
		}
		for page.Realm.Tasks.Len() != 0 {
			if runErr := page.Realm.RunOne(context.Background()); runErr != nil {
				t.Fatalf("run %s: %v", label, runErr)
			}
		}
	}

	run("mutate", `
(() => {
  const root = document.getElementById("root");
  const first = document.getElementById("first");
  const second = document.getElementById("second");
  const strong = document.createElement("strong");
  strong.textContent = "S";

  root.prepend("zero", strong);
  first.before("before");
  first.after("after");
  second.replaceWith(first, "tail");
  if (root.textContent !== "zeroSbeforeafterAtail" ||
      root.querySelectorAll("p").length !== 1 ||
      second.parentNode !== null || first.parentNode !== root) {
    throw new Error("convenience mutation order or identity failed: " + root.textContent);
  }

  const before = root.innerHTML;
  const invalid = document.createElement("section");
  let failures = 0;
  try { document.append(invalid); } catch (error) {
    if (!(error instanceof DOMException) || error.name !== "HierarchyRequestError" ||
        error.code !== DOMException.HIERARCHY_REQUEST_ERR ||
        Object.prototype.toString.call(error) !== "[object DOMException]") throw error;
    failures++;
  }
  if (invalid.parentNode !== null || root.innerHTML !== before) {
    throw new Error("failed hierarchy mutation changed the tree");
  }
  try { root.removeChild(document.body); } catch (error) {
    if (!(error instanceof DOMException) || error.name !== "NotFoundError") throw error;
    failures++;
  }
  try { document.createElement("bad name"); } catch (error) {
    if (!(error instanceof DOMException) || error.name !== "InvalidCharacterError") throw error;
    failures++;
  }
  try { root.querySelector("["); } catch (error) {
    if (!(error instanceof DOMException) || error.name !== "SyntaxError") throw error;
    failures++;
  }
  if (failures !== 4) throw new Error("missing typed DOM failures: " + failures);

  const constructed = new DOMException("clone failed", "DataCloneError");
	  if (!(constructed instanceof DOMException) || constructed.name !== "DataCloneError" ||
	      constructed.message !== "clone failed" || constructed.code !== 0 ||
	      constructed.toString() !== "DataCloneError: clone failed") {
	    throw new Error("DOMException constructor surface failed");
	  }
	  let rejectedCall = false;
	  try { DOMException("without new"); } catch (error) { rejectedCall = error instanceof TypeError; }
	  if (!rejectedCall) throw new Error("DOMException call without new was accepted");

  root.replaceChildren("done", strong);
  if (root.textContent !== "doneS" || strong.parentNode !== root) {
    throw new Error("replaceChildren failed");
  }
  strong.remove();
  if (root.textContent !== "done" || strong.parentNode !== null) {
    throw new Error("remove failed");
  }
  globalThis.__heldMutationNode = strong;
})();
`)
	if err := collect(page); err != nil {
		t.Fatalf("collect with held detached node: %v", err)
	}
	run("retained", `
if (__heldMutationNode.textContent !== "S" || __heldMutationNode.parentNode !== null) {
  throw new Error("detached node did not survive an explicit GC checkpoint");
}
globalThis.__heldMutationNode = undefined;
`)
	if err := collect(page); err != nil {
		t.Fatalf("collect after detached-node release: %v", err)
	}
	if err := page.Close(); err != nil {
		t.Fatal(err)
	}
	if stats := browserRuntime.Ledger().Stats(); stats.LiveObjects != 0 || stats.PersistentObjects != 0 {
		t.Fatalf("DOM mutation teardown ownership = %#v", stats)
	}
	if err := browserRuntime.Close(); err != nil {
		t.Fatal(err)
	}
}
