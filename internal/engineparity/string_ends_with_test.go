package engineparity

import (
	"context"
	"net/url"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/nativeengine"
)

func TestStrandStringEndsWithParity(t *testing.T) {
	runStringEndsWithParity(t, nativeengine.New(nativeengine.Config{}))
}

func runStringEndsWithParity(t *testing.T, engine browser.Engine) {
	t.Helper()
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://parity.gossamer.test/string-ends-with.html")
	page, err := browserRuntime.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `
let regexpRejected = false;
try { "chunk.js".endsWith(/\.js/); } catch (error) { regexpRejected = error instanceof TypeError; }
let invalidCodePointRejected = false;
try { String.fromCodePoint(0x110000); } catch (error) { invalidCodePointRejected = error instanceof RangeError; }
if (!"chunk.js".endsWith(".js") ||
    "chunk.js".endsWith("chunk", 5) !== true ||
    "chunk.js".endsWith("chunk", -1) !== false ||
    "chunk.js".endsWith("js", 999) !== true ||
    "chunk.js".endsWith(".css") !== false ||
    "strand".substring(1, 4) !== "tra" ||
    "strand".substring(4, 1) !== "tra" ||
    "strand".substring(-5, 2) !== "st" ||
    "strand".substring(2) !== "rand" ||
    "strand".charCodeAt(0) !== 115 ||
    "😀".charCodeAt(0) !== 55357 || "😀".charCodeAt(1) !== 56832 ||
    !Number.isNaN("strand".charCodeAt(99)) ||
    String.fromCharCode(65, 66) !== "AB" ||
    String.fromCodePoint(0x1F1FA, 0x1F1F8) !== "🇺🇸" ||
    invalidCodePointRejected !== true ||
    "ababa".lastIndexOf("ba") !== 3 ||
    "ababa".lastIndexOf("ba", 2) !== 1 ||
    "ababa".lastIndexOf("z") !== -1 ||
    regexpRejected !== true) {
  throw new Error("String.prototype.endsWith parity");
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
		t.Fatalf("String.prototype.endsWith teardown ownership = %#v", stats)
	}
	if err := browserRuntime.Close(); err != nil {
		t.Fatal(err)
	}
}
