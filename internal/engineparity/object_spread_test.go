package engineparity

import (
	"context"
	"net/url"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/nativeengine"
)

func TestStrandObjectSpreadParity(t *testing.T) {
	runObjectSpreadParity(t, nativeengine.New(nativeengine.Config{}))
}

func runObjectSpreadParity(t *testing.T, engine browser.Engine) {
	t.Helper()
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://parity.gossamer.test/object-spread.html")
	page, err := browserRuntime.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `
let reads = 0;
let source = {a: 1, get copied() { reads++; return 2; }};
let symbol = Symbol("spread");
source[symbol] = 5;
Object.defineProperty(source, "hidden", {value: 9, enumerable: false});
let key = "chosen";
let result = {a: 0, ...source, [key]: 3, a: 4, ...null};
if (result.a !== 4 || result.copied !== 2 || result.chosen !== 3 || result[symbol] !== 5 ||
    typeof result.hidden !== "undefined" || reads !== 1) {
  throw new Error("object spread parity failed");
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
		t.Fatalf("object spread teardown ownership = %#v", stats)
	}
	if err := browserRuntime.Close(); err != nil {
		t.Fatal(err)
	}
}
