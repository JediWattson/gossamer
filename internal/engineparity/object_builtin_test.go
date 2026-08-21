package engineparity

import (
	"context"
	"net/url"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/nativeengine"
)

func TestStrandObjectBuiltinParity(t *testing.T) {
	runObjectBuiltinParity(t, nativeengine.New(nativeengine.Config{}))
}

func runObjectBuiltinParity(t *testing.T, engine browser.Engine) {
	t.Helper()
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://parity.gossamer.test/object-builtins.html")
	page, err := browserRuntime.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `
let copyKey = Symbol("copy");
let copySource = {visible: 7};
copySource[copyKey] = 8;
Object.defineProperty(copySource, "hidden", {value: 9, enumerable: false});
let copyTarget = Object.assign({base: 6}, null, copySource, undefined);
let ownNames = Object.getOwnPropertyNames(copySource);
let stringTarget = Object.assign({}, "go");
let descriptorReads = [];
let descriptorBag = {};
Object.defineProperty(descriptorBag, "visible", {enumerable: true, get: function() {
  descriptorReads.push("visible");
  return {value: 11, writable: true, enumerable: true, configurable: true};
}});
Object.defineProperty(descriptorBag, copyKey, {enumerable: true, get: function() {
  descriptorReads.push("symbol");
  return {get: function() { return 12; }, enumerable: false, configurable: true};
}});
let defined = Object.defineProperties({}, descriptorBag);
let rejectedTarget = {};
let rejected = false;
try {
  Object.defineProperties(rejectedTarget, {
    first: {value: 1},
    bad: {get: 1}
  });
} catch (error) { rejected = error instanceof TypeError; }
globalThis.dynamicGlobal = 23;
if (globalThis !== window || dynamicGlobal !== 23 || typeof HTMLIFrameElement !== "function" ||
    document instanceof window.HTMLIFrameElement || copyTarget.base !== 6 || copyTarget.visible !== 7 || copyTarget[copyKey] !== 8 ||
    copyTarget.hidden !== undefined || ownNames.join(",") !== "visible,hidden" ||
    !copySource.hasOwnProperty("visible") || !copySource.hasOwnProperty(copyKey) ||
    copySource.hasOwnProperty("missing") || !Array.isArray([]) || Array.isArray({}) ||
    !Object.is(NaN, NaN) || Object.is(0, -0) || !Object.is(copySource, copySource) ||
    stringTarget[0] !== "g" || stringTarget[1] !== "o" ||
    defined.visible !== 11 || defined[copyKey] !== 12 || descriptorReads.join(",") !== "visible,symbol" ||
    Object.keys(defined).join(",") !== "visible" || !rejected || typeof rejectedTarget.first !== "undefined" ||
    typeof definitelyMissing !== "undefined") {
  throw new Error("Object and Array bootstrap builtin parity failed");
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
		t.Fatalf("Object builtin teardown ownership = %#v", stats)
	}
	if err := browserRuntime.Close(); err != nil {
		t.Fatalf("close browser: %v", err)
	}
}
