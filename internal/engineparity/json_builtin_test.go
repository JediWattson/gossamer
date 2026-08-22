package engineparity

import (
	"context"
	"net/url"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/nativeengine"
)

func TestStrandJSONBuiltinParity(t *testing.T) {
	runJSONBuiltinParity(t, nativeengine.New(nativeengine.Config{}))
}

func runJSONBuiltinParity(t *testing.T, engine browser.Engine) {
	t.Helper()
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://parity.gossamer.test/json-builtins.html")
	page, err := browserRuntime.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `
let parsed = JSON.parse('{"name":"strand","items":[1,true,null]}');
let encoded = JSON.stringify({name: parsed.name, omitted: undefined, items: [1, undefined, NaN, {ok: true}]});
let proxyEncoded = JSON.stringify(new Proxy({visible: 7, hidden: 8}, {
  ownKeys() { return ["visible", "hidden"]; },
  getOwnPropertyDescriptor(target, key) { return {enumerable: key === "visible", configurable: true}; }
}));
let rootUndefined = JSON.stringify(undefined);
let cyclic = {}; cyclic.self = cyclic;
let rejectedCycle = false;
try { JSON.stringify(cyclic); } catch (error) { rejectedCycle = error instanceof TypeError; }
if (parsed.name !== "strand" || parsed.items[0] !== 1 || parsed.items[1] !== true || parsed.items[2] !== null ||
    encoded !== '{"name":"strand","items":[1,null,null,{"ok":true}]}' ||
    proxyEncoded !== '{"visible":7}' || rootUndefined !== undefined || !rejectedCycle) {
  throw new Error("JSON builtin parity failed");
}
`}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := page.Close(); err != nil {
		t.Fatalf("close page: %v", err)
	}
	if stats := browserRuntime.Ledger().Stats(); stats.LiveObjects != 0 || stats.PersistentObjects != 0 {
		t.Fatalf("JSON builtin teardown ownership = %#v", stats)
	}
	if err := browserRuntime.Close(); err != nil {
		t.Fatalf("close browser: %v", err)
	}
}
