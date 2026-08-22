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
let parityUnmatchedCapture = "/global".replace(/^\/+|(\/)\/+$/g, "$1");
let parityCaptures = "abc".replace(/(a)(b)(c)/, "$3$2$1");
let parityTwoDigitFallback = "ab".replace(/(a)(b)/, "$12");
let parityContextReplacement = "abc".replace(/(b)/, "<$&-$'");
let parityLookaheadExpression = /(^|\n)BEGIN(?:[^\n]*)?\n([\s\S]*?)\nEND(?=\n|$)/;
let parityLookaheadMatch = parityLookaheadExpression.exec("before\nBEGIN js\nconst x = 1;\nEND\nafter");
let parityLookaheadReplace = "ab".replace(/a(?=b)/, "x");
let parityLookaheadGlobal = "abab".match(/a(?=b)/g);
let parityNegativeLookahead = /a(?!b)/.test("ac") && !/a(?!b)/.test("ab");
let parityUnicodeIndexExpression = /https:/g;
let parityUnicodeIndexMatch = parityUnicodeIndexExpression.exec("éhttps:");
let parityScanText = "@abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_.-";
let parityScanIndex = 1;
let parityScanIterations = 0;
for (; parityScanIndex < parityScanText.length && /[A-Za-z0-9_.-]/.test(parityScanText[parityScanIndex] ?? "");) {
  parityScanIndex += 1;
  parityScanIterations += 1;
  if (parityScanIterations > parityScanText.length) {
    throw new Error("RegExp for-condition did not terminate");
  }
}
if (!parityExpression.test("GOO") || parityExpression.test("stop") ||
    parityExpression.source !== "^go+$" || parityExpression.flags !== "i" ||
    parityExpression.toString() !== "/^go+$/i" || !parityFirstMatch ||
    parityFirstIndex !== 2 || paritySecondMatch || parityGlobalExpression.lastIndex !== 0 ||
    !parityUnicodeExpression.test("ÀÖ") || parityUnicodeExpression.test("AZ") ||
    parityUnicodeExpression.source !== "^[\\u00C0-\\u00D6]+$" ||
    parityUnmatchedCapture !== "global" || parityCaptures !== "cba" ||
    parityTwoDigitFallback !== "a2" || parityContextReplacement !== "a<b-cc" ||
    parityLookaheadMatch[0] !== "\nBEGIN js\nconst x = 1;\nEND" ||
    parityLookaheadMatch[1] !== "\n" || parityLookaheadMatch[2] !== "const x = 1;" ||
    parityLookaheadMatch.index !== 6 || parityLookaheadReplace !== "xb" ||
    parityLookaheadGlobal.join(",") !== "a,a" || !parityNegativeLookahead ||
    parityUnicodeIndexMatch.index !== 1 || parityUnicodeIndexExpression.lastIndex !== 7 ||
    parityScanIndex !== 66 || parityScanIterations !== 65 ||
    "éhttps:".slice(parityUnicodeIndexMatch.index, parityUnicodeIndexExpression.lastIndex) !== "https:") {
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
