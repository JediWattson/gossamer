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

func TestStrandSpecializedDOMInterfaceParity(t *testing.T) {
	engine := nativeengine.New(nativeengine.Config{})
	runSpecializedDOMInterfaceParity(t, engine, func(page *browser.Page) error {
		realm, ok := engine.LatestRealm()
		if !ok {
			return nativeengine.ErrRealmClosed
		}
		return realm.CollectGarbage(page)
	})
}

func runSpecializedDOMInterfaceParity(t *testing.T, engine browser.Engine, collect func(*browser.Page) error) {
	t.Helper()
	root, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body><form id="form"><input id="input"><textarea id="textarea"></textarea><select id="select"><option id="option">one</option></select><button id="button">go</button></form><template id="template"></template><iframe id="iframe"></iframe><div id="generic"></div></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://parity.gossamer.test/dom-interfaces.html")
	page, err := browserRuntime.NewPage(root, location)
	if err != nil {
		t.Fatal(err)
	}
	baselineLiveNodes := page.Document().Store().LiveLen()
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

	run("interfaces", `
(() => {
  const cases = [
    ["form", HTMLFormElement],
    ["input", HTMLInputElement],
    ["textarea", HTMLTextAreaElement],
    ["select", HTMLSelectElement],
    ["option", HTMLOptionElement],
    ["button", HTMLButtonElement],
    ["template", HTMLTemplateElement],
    ["iframe", HTMLIFrameElement]
  ];
	  for (let entry of cases) {
	    const id = entry[0];
	    const Interface = entry[1];
    const node = document.getElementById(id);
    if (!(node instanceof Interface) || !(node instanceof HTMLElement) ||
        !(node instanceof Element) || !(node instanceof Node) ||
        Object.getPrototypeOf(node) !== Interface.prototype ||
        node.constructor !== Interface || document.querySelector("#" + id) !== node) {
      throw new Error("specialized interface failed for " + id);
    }
  }

  const text = document.createTextNode("text");
  const fragment = document.createDocumentFragment();
  if (!(document instanceof Document) || !(document instanceof Node) ||
      !(text instanceof Text) || !(text instanceof Node) ||
	      !(fragment instanceof DocumentFragment) || !(fragment instanceof Node) ||
	      Object.getPrototypeOf(document) !== Document.prototype ||
	      Object.getPrototypeOf(text) !== Text.prototype ||
	      Object.getPrototypeOf(fragment) !== DocumentFragment.prototype ||
	      Object.getPrototypeOf(HTMLInputElement.prototype) !== HTMLElement.prototype ||
	      Object.getPrototypeOf(HTMLElement.prototype) !== Element.prototype ||
	      Object.getPrototypeOf(Element.prototype) !== Node.prototype) {
    throw new Error("base node interface hierarchy failed");
  }

  const input = document.getElementById("input");
  if (input instanceof HTMLTextAreaElement ||
      document.getElementById("generic").constructor !== HTMLElement ||
      Object.getPrototypeOf(document.createElement("input")) !== HTMLInputElement.prototype ||
      Object.getPrototypeOf(document.createElementNS("urn:test", "input")) !== Element.prototype) {
    throw new Error("interface resolver crossed a namespace or local-name boundary");
  }

  const descriptor = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value");
  if (!descriptor || typeof descriptor.get !== "function" || typeof descriptor.set !== "function" ||
      Object.prototype.hasOwnProperty.call(HTMLElement.prototype, "value")) {
    throw new Error("form value descriptor is not specialized");
  }
  input.value = "typed";
  if (descriptor.get.call(input) !== "typed" ||
      document.getElementById("form").children[0] !== input) {
    throw new Error("specialized accessor or canonical collection identity failed");
  }

	  for (let Interface of [Node, Element, HTMLElement, HTMLInputElement, Text, Document, DocumentFragment]) {
    let rejected = false;
    try { new Interface(); } catch (error) { rejected = true; }
    if (!rejected) throw new Error(Interface.name + " accepted illegal construction");
  }

  input.remove();
  globalThis.__heldSpecializedInput = input;
})();
`)
	if err := collect(page); err != nil {
		t.Fatalf("collect with held specialized wrapper: %v", err)
	}
	if got := page.Document().Store().LiveLen(); got < baselineLiveNodes {
		t.Fatalf("held specialized wrapper lost its native node: got %d, baseline %d", got, baselineLiveNodes)
	}
	run("retained", `
if (!(__heldSpecializedInput instanceof HTMLInputElement) ||
    !(__heldSpecializedInput instanceof HTMLElement) ||
    __heldSpecializedInput.value !== "typed" ||
    Object.getPrototypeOf(__heldSpecializedInput) !== HTMLInputElement.prototype) {
  throw new Error("held specialized wrapper changed identity or state");
}
globalThis.__heldSpecializedInput = undefined;
`)
	if err := collect(page); err != nil {
		t.Fatalf("collect after specialized wrapper release: %v", err)
	}
	if err := page.Close(); err != nil {
		t.Fatal(err)
	}
	if stats := browserRuntime.Ledger().Stats(); stats.LiveObjects != 0 || stats.PersistentObjects != 0 {
		t.Fatalf("specialized interface teardown ownership = %#v", stats)
	}
	if err := browserRuntime.Close(); err != nil {
		t.Fatal(err)
	}
}
