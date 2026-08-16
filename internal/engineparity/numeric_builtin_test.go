package engineparity

import (
	"context"
	"net/url"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/nativeengine"
)

func TestStrandNumericBuiltinParity(t *testing.T) {
	runNumericBuiltinParity(t, nativeengine.New(nativeengine.Config{}))
}

func runNumericBuiltinParity(t *testing.T, engine browser.Engine) {
	t.Helper()
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://parity.gossamer.test/numeric-builtins.html")
	page, err := browserRuntime.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `
let numericBefore = Date.now();
let numericRandom = Math.random();
let numericAfter = Date.now();
if (Math.clz32(0) !== 32 || Math.clz32(1) !== 31 ||
    Math.floor(-1.2) !== -2 || Math.log(1) !== 0 ||
    Math.min(4, -2, 9) !== -2 || Math.min() !== Infinity ||
    Math.LN2 <= 0.69 || Math.LN2 >= 0.70 ||
    numericRandom < 0 || numericRandom >= 1 || numericBefore > numericAfter ||
    !isNaN("not-a-number") || isNaN("42") || Number("42") !== 42 ||
    (255).toString(16) !== "ff" || numericRandom.toString(36).slice(0, 2) !== "0.") {
  throw new Error("numeric builtin parity failed");
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
		t.Fatalf("numeric builtin teardown ownership = %#v", stats)
	}
	if err := browserRuntime.Close(); err != nil {
		t.Fatalf("close browser: %v", err)
	}
}
