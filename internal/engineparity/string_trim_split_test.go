package engineparity

import (
	"context"
	"net/url"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/nativeengine"
)

func TestStrandStringTrimSplitCodePointParity(t *testing.T) {
	runStringTrimSplitCodePointParity(t, nativeengine.New(nativeengine.Config{}))
}

func runStringTrimSplitCodePointParity(t *testing.T, engine browser.Engine) {
	t.Helper()
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://parity.gossamer.test/string-trim-split-codepoint.html")
	page, err := browserRuntime.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `
const emoji = "\u{1F600}";
if (emoji.codePointAt() !== 0x1F600 || emoji.codePointAt(0) !== 0x1F600 || emoji.codePointAt(1) !== 0xDE00) {
  throw new Error("codePointAt surrogate pair failed");
}
if ("A".codePointAt(0) !== 65 || String.fromCharCode(0xD83D, 0xDE00).codePointAt(0) !== 0x1F600 ||
    "A".codePointAt(-1) !== undefined || "A".codePointAt(1) !== undefined ||
    String.prototype.codePointAt.length !== 1) {
  throw new Error("codePointAt edge cases failed");
}

const regexSplit = "a, b;  c".split(/[,;]\s*/);
if (JSON.stringify(regexSplit) !== '["a","b","c"]') {
  throw new Error("split RegExp separator failed: " + JSON.stringify(regexSplit));
}
const captureSplit = "a1b".split(/(\d)|([x])/);
if (captureSplit.length !== 4 || captureSplit[0] !== "a" || captureSplit[1] !== "1" ||
    captureSplit[2] !== undefined || captureSplit[3] !== "b") {
  throw new Error("split RegExp captures failed");
}
if (JSON.stringify("abc".split(/(?:)/)) !== '["a","b","c"]' ||
    JSON.stringify("abc".split(/(?:)/, 2)) !== '["a","b"]' ||
    JSON.stringify("".split(/(?:)/)) !== "[]" ||
    JSON.stringify("".split("x")) !== '[""]' ||
    JSON.stringify("a,b,c".split(",", 2)) !== '["a","b"]' ||
    JSON.stringify("abc".split(undefined, 0)) !== "[]") {
  throw new Error("split empty/limit edge cases failed");
}

const padded = " \t\n\u00A0\uFEFFhi\u2028\u2029 \r";
if (padded.trimStart() !== "hi\u2028\u2029 \r" ||
    padded.trimLeft() !== "hi\u2028\u2029 \r" ||
    padded.trimEnd() !== " \t\n\u00A0\uFEFFhi" ||
    padded.trimRight() !== " \t\n\u00A0\uFEFFhi" ||
    padded.trim() !== "hi" ||
    "   \t\n ".trimStart() !== "" ||
    "   \t\n ".trimEnd() !== "" ||
    String.prototype.trimStart.length !== 0 ||
    String.prototype.trimEnd.length !== 0) {
  throw new Error("trimStart/trimEnd failed");
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
		t.Fatalf("String trim/split/codePointAt teardown ownership = %#v", stats)
	}
	if err := browserRuntime.Close(); err != nil {
		t.Fatal(err)
	}
}
