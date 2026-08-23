package engineparity

import (
	"context"
	"net/url"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/nativeengine"
)

func TestStrandMapSetIterableConstructorParity(t *testing.T) {
	runMapSetIterableConstructorParity(t, nativeengine.New(nativeengine.Config{}))
}

func runMapSetIterableConstructorParity(t *testing.T, engine browser.Engine) {
	t.Helper()
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://parity.gossamer.test/map-set-iterable.html")
	page, err := browserRuntime.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `
function iterableOf(values) {
  return {
    [Symbol.iterator]: function() {
      let index = 0;
      return {next: function() {
        if (index >= values.length) return {done: true};
        let value = values[index];
        index = index + 1;
        return {value: value, done: false};
      }};
    }
  };
}
let mapped = new Map(iterableOf([["alpha", 1], {0: "beta", 1: 2}]));
let copied = new Map(mapped);
let values = new Set(iterableOf(["alpha", "beta", "alpha"]));
let characters = new Set("aba");
let iteratorClosed = false;
let badEntries = {
  [Symbol.iterator]: function() {
    return {
      next: function() { return {value: 7, done: false}; },
      return: function() { iteratorClosed = true; return {done: true}; }
    };
  }
};
let badEntryRejected = false;
try { new Map(badEntries); } catch (error) { badEntryRejected = error instanceof TypeError; }
if (mapped.size !== 2 || mapped.get("alpha") !== 1 || mapped.get("beta") !== 2 ||
    copied.size !== 2 || copied.get("beta") !== 2 ||
    values.size !== 2 || !values.has("alpha") || !values.has("beta") ||
    characters.size !== 2 || !characters.has("a") || !characters.has("b") ||
    !badEntryRejected || !iteratorClosed) {
  throw new Error("Map/Set iterable constructor parity");
}
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
		t.Fatalf("Map/Set iterable teardown ownership = %#v", stats)
	}
	if err := browserRuntime.Close(); err != nil {
		t.Fatal(err)
	}
}
