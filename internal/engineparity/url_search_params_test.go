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

func TestStrandURLSearchParamsParity(t *testing.T) {
	runURLSearchParamsParity(t, nativeengine.New(nativeengine.Config{}))
}

func runURLSearchParamsParity(t *testing.T, engine browser.Engine) {
	t.Helper()
	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://parity.gossamer.test/search?existing=1")
	page, err := browserRuntime.NewPage(document, location)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `
const params = new URLSearchParams({query: "one two", tag: "first"});
params.append("tag", "second");
params.set("empty", "");
if (!(params instanceof URLSearchParams) || params.size !== 4 ||
    params.get("query") !== "one two" || params.get("missing") !== null ||
    params.getAll("tag").join(",") !== "first,second" || !params.has("tag", "second")) {
  throw new Error("URLSearchParams read parity failed");
}
params.delete("tag", "first");
params.sort();
if (String(params) !== "empty=&query=one+two&tag=second") {
  throw new Error("URLSearchParams mutation parity failed: " + String(params));
}
const clone = new URLSearchParams(params);
const entry = clone.entries().next();
if (entry.done || entry.value[0] !== "empty" || entry.value[1] !== "") {
  throw new Error("URLSearchParams iterator parity failed");
}
let visited = "";
clone.forEach((value, key, owner) => { if (owner !== clone) throw new Error("forEach owner"); visited += key + "=" + value + ";"; });
if (visited !== "empty=;query=one two;tag=second;") {
  throw new Error("URLSearchParams forEach parity failed: " + visited);
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
	if err := browserRuntime.Close(); err != nil {
		t.Fatal(err)
	}
}
