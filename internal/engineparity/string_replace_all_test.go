package engineparity

import (
	"context"
	"net/url"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/nativeengine"
)

func TestStrandStringReplaceAllParity(t *testing.T) {
	runStringReplaceAllParity(t, nativeengine.New(nativeengine.Config{}))
}

func runStringReplaceAllParity(t *testing.T, engine browser.Engine) {
	t.Helper()
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://parity.gossamer.test/string-replace-all.html")
	page, err := browserRuntime.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `
let replaceAllCalls = [];
let replaceAllCallback = "a-a".replaceAll("a", function(match, index, source) {
  replaceAllCalls.push(match + ":" + index + ":" + source);
  return String(index);
});
let replacementTokens = "[$&][$" + String.fromCharCode(96) + "][$']";
let nonGlobalRejected = false;
try { "aba".replaceAll(/a/, "x"); } catch (error) { nonGlobalRejected = error instanceof TypeError; }
if ("a.b.a".replaceAll(".", "-") !== "a-b-a") throw new Error("replaceAll string search");
if ("aba".replaceAll(/a/g, "x") !== "xbx") throw new Error("replaceAll RegExp search");
if ("aba".replaceAll(/(a)/g, "<$1>") !== "<a>b<a>") throw new Error("replaceAll captures");
if ("ab".replaceAll("", "-") !== "-a-b-") throw new Error("replaceAll empty search");
if ("aba".replaceAll("a", replacementTokens) !== "[a][][ba]b[a][ab][]") throw new Error("replaceAll tokens");
if (replaceAllCallback !== "0-2") throw new Error("replaceAll callback result: " + replaceAllCallback);
if (replaceAllCalls.join("|") !== "a:0:a-a|a:2:a-a") throw new Error("replaceAll callback arguments: " + replaceAllCalls.join("|"));
if (nonGlobalRejected !== true) throw new Error("replaceAll accepted non-global RegExp");
if (String.prototype.replaceAll.length !== 2) throw new Error("replaceAll length");
`}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := page.Close(); err != nil {
		t.Fatal(err)
	}
	if stats := browserRuntime.Ledger().Stats(); stats.LiveObjects != 0 || stats.PersistentObjects != 0 {
		t.Fatalf("String.prototype.replaceAll teardown ownership = %#v", stats)
	}
	if err := browserRuntime.Close(); err != nil {
		t.Fatal(err)
	}
}
