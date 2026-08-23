package engineparity

import (
	"context"
	"net/url"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/nativeengine"
)

func TestStrandStringMatchAllParity(t *testing.T) {
	runStringMatchAllParity(t, nativeengine.New(nativeengine.Config{}))
}

func runStringMatchAllParity(t *testing.T, engine browser.Engine) {
	t.Helper()
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://parity.gossamer.test/string-match-all.html")
	page, err := browserRuntime.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `
let expression = /t(e)(st(\d?))/g;
let iterator = "test1 test2".matchAll(expression);
if (iterator[Symbol.iterator]() !== iterator) throw new Error("matchAll iterator identity");
let matches = [...iterator];
if (matches.length !== 2 || matches[0][0] !== "test1" || matches[0][1] !== "e" ||
    matches[0][2] !== "st1" || matches[0][3] !== "1" || matches[0].index !== 0 ||
    matches[0].input !== "test1 test2" || matches[0].groups !== undefined ||
    matches[1][0] !== "test2" || matches[1].index !== 6 || expression.lastIndex !== 0) {
  throw new Error("matchAll RegExp records");
}
let patternMatches = [..."a1 a2".matchAll("a.")];
if (patternMatches.length !== 2 || patternMatches[0][0] !== "a1" || patternMatches[1][0] !== "a2") {
  throw new Error("matchAll string pattern");
}
let positioned = /a/g;
positioned.lastIndex = 2;
let positionedMatches = [..."aba".matchAll(positioned)];
if (positionedMatches.length !== 1 || positionedMatches[0].index !== 2 || positioned.lastIndex !== 2) {
  throw new Error("matchAll lastIndex clone semantics");
}
let exhausted = /(?:)/g;
exhausted.lastIndex = 99;
if ([..."aba".matchAll(exhausted)].length !== 0 || exhausted.lastIndex !== 99) {
  throw new Error("matchAll exhausted lastIndex");
}
let nonGlobalRejected = false;
try { [..."aba".matchAll(/a/)]; } catch (error) { nonGlobalRejected = error instanceof TypeError; }
if (!nonGlobalRejected || String.prototype.matchAll.length !== 1) throw new Error("matchAll contract");
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
		t.Fatalf("String.prototype.matchAll teardown ownership = %#v", stats)
	}
	if err := browserRuntime.Close(); err != nil {
		t.Fatal(err)
	}
}
