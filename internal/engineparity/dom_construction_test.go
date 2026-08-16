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

func TestStrandDOMConstructionParity(t *testing.T) {
	runDOMConstructionParity(t, nativeengine.New(nativeengine.Config{}))
}

func runDOMConstructionParity(t *testing.T, engine browser.Engine) {
	t.Helper()
	root, err := htmlparser.Parse(strings.NewReader(`
<!doctype html><html><head></head><body><main id="root"><p id="old">old</p></main></body></html>
`))
	if err != nil {
		t.Fatal(err)
	}
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://parity.gossamer.test/base/page.html")
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

	run("construct", `
if (document.defaultView !== window || document.ownerDocument !== null ||
    document.baseURI !== "https://parity.gossamer.test/base/page.html" ||
    document.readyState !== "complete") {
  throw new Error("document metadata parity failed");
}
let root = document.getElementById("root");
let old = document.getElementById("old");
let fragment = document.createDocumentFragment();
if (fragment.nodeType !== 11 || fragment.nodeName !== "#document-fragment" ||
    fragment.ownerDocument !== document || fragment.isConnected) {
  throw new Error("DocumentFragment identity parity failed");
}
let vector = document.createElementNS("http://www.w3.org/2000/svg", "svg:g");
vector.id = "vector";
if (vector.namespaceURI !== "http://www.w3.org/2000/svg" || vector.prefix !== "svg" ||
    vector.localName !== "g" || vector.ownerDocument !== document || vector.isConnected) {
  throw new Error("namespace construction parity failed");
}
let first = document.createTextNode("abc");
let second = document.createTextNode("d");

let template = document.createElement("template");
template.innerHTML = "<button id=from-template>Count <span>0</span></button>";
if (template.content !== template.content || template.content.nodeType !== 11 ||
    template.firstChild !== null || document.createElement("div").content !== undefined) {
  throw new Error("HTMLTemplateElement content parity failed");
}
let imported = document.importNode(template.content.firstChild, true);
if (imported.id !== "from-template" || imported.firstChild.data !== "Count ") {
  throw new Error("document.importNode/Text.data parity failed");
}
imported.firstChild.data = "Value ";
root.appendChild(imported);
if (imported.textContent !== "Value 0") {
  throw new Error("Text.data mutation parity failed");
}
imported.remove();
if (imported.parentNode !== null || document.getElementById("from-template") !== null) {
  throw new Error("ChildNode.remove parity failed");
}

fragment.appendChild(first);
fragment.appendChild(second);
first.nodeValue = "abc";
let split = first.splitText(1);
if (first.nodeValue !== "a" || split.nodeValue !== "bc" || split.parentNode !== fragment) {
  throw new Error("splitText parity failed");
}
fragment.normalize();
if (fragment.childNodes.length !== 1 || fragment.firstChild !== first || first.nodeValue !== "abcd") {
  throw new Error("normalize parity failed");
}
if (root.replaceChild(vector, old) !== old || old.parentNode !== null || vector.parentNode !== root) {
  throw new Error("replaceChild parity failed");
}
let loose = document.createElementNS(null, "Loose");
loose.id = "loose";
if (loose.namespaceURI !== null || loose.prefix !== null || loose.localName !== "Loose") {
  throw new Error("null namespace parity failed");
}
fragment.appendChild(loose);
root.appendChild(fragment);
if (fragment.firstChild !== null || root.lastChild !== loose || !loose.isConnected || first.parentElement !== root) {
  throw new Error("fragment insertion parity failed");
}
`)

	run("retain", `
if (document.getElementById("vector") !== vector || document.getElementById("loose") !== loose ||
    vector.parentNode !== root || vector.ownerDocument !== document || old.isConnected) {
  throw new Error("cross-task DOM identity parity failed");
}
`)

	if err := page.Close(); err != nil {
		t.Fatalf("close page: %v", err)
	}
	if stats := browserRuntime.Ledger().Stats(); stats.LiveObjects != 0 || stats.PersistentObjects != 0 {
		t.Fatalf("parity teardown ownership = %#v", stats)
	}
	if err := browserRuntime.Close(); err != nil {
		t.Fatalf("close browser: %v", err)
	}
}
