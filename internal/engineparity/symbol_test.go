package engineparity

import (
	"context"
	"net/url"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/nativeengine"
)

func TestStrandSymbolParity(t *testing.T) {
	runSymbolParity(t, nativeengine.New(nativeengine.Config{}))
}

func runSymbolParity(t *testing.T, engine browser.Engine) {
	t.Helper()
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://parity.gossamer.test/symbols.html")
	page, err := browserRuntime.NewPage(dom.NewDocument(), location)
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

	run("create-symbols", `
let firstSymbol = Symbol("token");
let secondSymbol = Symbol("token");
let registrySymbol = Symbol.for("gossamer.parity");
let symbolTarget = {};
symbolTarget[firstSymbol] = 1;
symbolTarget[secondSymbol] = 2;
symbolTarget[registrySymbol] = 3;
let rejectedSymbolConstructor = false;
try { new Symbol("nope"); } catch (error) { rejectedSymbolConstructor = error instanceof TypeError; }
`)

	run("verify-symbols", `
if (typeof Symbol !== "function" || typeof firstSymbol !== "symbol" ||
    firstSymbol === secondSymbol || registrySymbol !== Symbol.for("gossamer.parity") ||
    firstSymbol.description !== "token" || String(firstSymbol) !== "Symbol(token)" ||
    symbolTarget[firstSymbol] !== 1 || symbolTarget[secondSymbol] !== 2 ||
    symbolTarget[Symbol.for("gossamer.parity")] !== 3 || Object.keys(symbolTarget).length !== 0 ||
    Symbol.iterator !== Symbol.iterator ||
    Array.prototype[Symbol.iterator] !== Array.prototype.values ||
    "go"[Symbol.iterator]().next().value !== "g" || !rejectedSymbolConstructor) {
  throw new Error("Symbol parity failed");
}
`)

	if err := page.Close(); err != nil {
		t.Fatalf("close page: %v", err)
	}
	if stats := browserRuntime.Ledger().Stats(); stats.LiveObjects != 0 || stats.PersistentObjects != 0 {
		t.Fatalf("Symbol teardown ownership = %#v", stats)
	}
	if err := browserRuntime.Close(); err != nil {
		t.Fatalf("close browser: %v", err)
	}
}
