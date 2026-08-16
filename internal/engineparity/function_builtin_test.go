package engineparity

import (
	"context"
	"net/url"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/nativeengine"
)

func TestStrandFunctionBuiltinParity(t *testing.T) {
	runFunctionBuiltinParity(t, nativeengine.New(nativeengine.Config{}))
}

func runFunctionBuiltinParity(t *testing.T, engine browser.Engine) {
	t.Helper()
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://parity.gossamer.test/function-builtins.html")
	page, err := browserRuntime.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `
function parityTotal(left, right) {
  return this.base + left + right + arguments.length;
}
let parityReceiver = {base: 10};
let parityBound = parityTotal.bind(parityReceiver, 5);
let parityArguments = (function(first) { return arguments[1] + arguments.length; })(1, 7);
if (parityTotal.call(parityReceiver, 1, 2) !== 15 ||
    parityTotal.apply(parityReceiver, [3, 4]) !== 19 ||
    parityBound(6) !== 23 || parityBound.length !== 1 || parityArguments !== 9) {
  throw new Error("Function invocation builtin parity failed");
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
		t.Fatalf("Function builtin teardown ownership = %#v", stats)
	}
	if err := browserRuntime.Close(); err != nil {
		t.Fatalf("close browser: %v", err)
	}
}
