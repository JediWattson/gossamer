package engineparity

import (
	"context"
	"net/url"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/nativeengine"
)

func TestStrandIntlBuiltinParity(t *testing.T) {
	runIntlBuiltinParity(t, nativeengine.New(nativeengine.Config{}))
}

func runIntlBuiltinParity(t *testing.T, engine browser.Engine) {
	t.Helper()
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://parity.gossamer.test/intl-builtins.html")
	page, err := browserRuntime.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `
const regionNames = new Intl.DisplayNames(["en"], {type: "region"});
let invalidDisplayTypeRejected = false;
try { new Intl.DisplayNames(["en"], {type: "unsupported"}); } catch (error) { invalidDisplayTypeRejected = error instanceof RangeError; }
if (regionNames.of("US") !== "United States" || regionNames.of("DE") !== "Germany" || regionNames.of("de") !== "de" ||
    invalidDisplayTypeRejected !== true) {
  throw new Error("Intl builtin parity failed");
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
		t.Fatalf("Intl builtin teardown ownership = %#v", stats)
	}
	if err := browserRuntime.Close(); err != nil {
		t.Fatalf("close browser: %v", err)
	}
}
