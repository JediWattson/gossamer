package engineparity

import (
	"context"
	"net/url"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/nativeengine"
)

func TestStrandRegExpBuiltinParity(t *testing.T) {
	runRegExpBuiltinParity(t, nativeengine.New(nativeengine.Config{}))
}

func runRegExpBuiltinParity(t *testing.T, engine browser.Engine) {
	t.Helper()
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://parity.gossamer.test/regexp-builtins.html")
	page, err := browserRuntime.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `
let parityExpression = RegExp("^go+$", "i");
let parityGlobalExpression = new RegExp("a", "g");
let parityFirstMatch = parityGlobalExpression.test("ba");
let parityFirstIndex = parityGlobalExpression.lastIndex;
let paritySecondMatch = parityGlobalExpression.test("ba");
let parityUnicodeExpression = RegExp("^[\\u00C0-\\u00D6]+$");
if (!parityExpression.test("GOO") || parityExpression.test("stop") ||
    parityExpression.source !== "^go+$" || parityExpression.flags !== "i" ||
    parityExpression.toString() !== "/^go+$/i" || !parityFirstMatch ||
    parityFirstIndex !== 2 || paritySecondMatch || parityGlobalExpression.lastIndex !== 0 ||
    !parityUnicodeExpression.test("ÀÖ") || parityUnicodeExpression.test("AZ") ||
    parityUnicodeExpression.source !== "^[\\u00C0-\\u00D6]+$") {
  throw new Error("RegExp builtin parity failed");
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
		t.Fatalf("RegExp builtin teardown ownership = %#v", stats)
	}
	if err := browserRuntime.Close(); err != nil {
		t.Fatalf("close browser: %v", err)
	}
}
