package engineparity

import (
	"context"
	"net/url"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/nativeengine"
)

func TestStrandOptionalChainParity(t *testing.T) {
	runOptionalChainParity(t, nativeengine.New(nativeengine.Config{}))
}

func runOptionalChainParity(t *testing.T, engine browser.Engine) {
	t.Helper()
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://parity.gossamer.test/optional-chain.html")
	page, err := browserRuntime.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `
let reads = 0;
let argumentsRead = 0;
function source(value) { reads++; return value; }
let object = {value: 4, add: function(value) { return this.value + value; }};
let absent = null;
let callable = function(value) { return value + 1; };
let missing;
if (source(object)?.value !== 4 || typeof source(absent)?.child.value !== "undefined" ||
    object?.add(argumentsRead++) !== 4 || typeof absent?.add(argumentsRead++) !== "undefined" ||
    callable?.(4) !== 5 || typeof missing?.(4) !== "undefined" || reads !== 2 || argumentsRead !== 1) {
  throw new Error("optional chain parity failed");
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
		t.Fatalf("optional chain teardown ownership = %#v", stats)
	}
	if err := browserRuntime.Close(); err != nil {
		t.Fatal(err)
	}
}
