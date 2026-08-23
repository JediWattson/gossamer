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

func TestStrandShadowDOMFoundation(t *testing.T) {
	root, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><head></head><body></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	engine := nativeengine.New(nativeengine.Config{})
	runtime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://parity.gossamer.test/shadow.html")
	page, err := runtime.NewPage(root, location)
	if err != nil {
		t.Fatal(err)
	}

	_, err = page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `
let closedHost = document.createElement("div");
if (!(closedHost instanceof HTMLDivElement) || !(closedHost instanceof HTMLElement)) {
  throw new Error("HTMLDivElement interface parity failed");
}
document.body.appendChild(closedHost);
let closedRoot = closedHost.attachShadow({mode: "closed", delegatesFocus: true});
if (!(closedRoot instanceof ShadowRoot) || !(closedRoot instanceof DocumentFragment) ||
    closedRoot.nodeType !== 11 || closedRoot.nodeName !== "#document-fragment" ||
    closedRoot.parentNode !== null || !closedRoot.isConnected ||
    closedRoot.host !== closedHost || closedRoot.mode !== "closed" ||
    closedRoot.delegatesFocus !== true || closedHost.shadowRoot !== null) {
  throw new Error("closed ShadowRoot identity parity failed");
}
let inside = document.createElement("span");
inside.id = "shadow-only";
closedRoot.append(inside);
if (!inside.isConnected || closedRoot.querySelector("#shadow-only") !== inside ||
    document.getElementById("shadow-only") !== null || closedHost.contains(inside)) {
  throw new Error("shadow tree encapsulation parity failed");
}
let openHost = document.createElement("section");
document.body.appendChild(openHost);
let openRoot = openHost.attachShadow({mode: "open"});
if (openHost.shadowRoot !== openRoot || openRoot.host !== openHost ||
    openRoot.mode !== "open" || openRoot.delegatesFocus !== false) {
  throw new Error("open ShadowRoot exposure parity failed");
}
let duplicateName = "";
try { openHost.attachShadow({mode: "open"}); } catch (error) { duplicateName = error.name; }
if (duplicateName !== "NotSupportedError") {
  throw new Error("duplicate attachShadow exception parity failed: " + duplicateName);
}
let invalidWasTypeError = false;
try { document.createElement("div").attachShadow({mode: "opaque"}); }
catch (error) { invalidWasTypeError = error instanceof TypeError; }
if (!invalidWasTypeError) throw new Error("invalid shadow mode should throw TypeError");
`})
	if err != nil {
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
	if stats := runtime.Ledger().Stats(); stats.LiveObjects != 0 || stats.PersistentObjects != 0 {
		t.Fatalf("shadow parity teardown ownership = %#v", stats)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
}
