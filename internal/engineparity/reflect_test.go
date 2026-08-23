package engineparity

import (
	"context"
	"net/url"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/nativeengine"
)

func TestStrandReflectGetApplyParity(t *testing.T) {
	runReflectGetApplyParity(t, nativeengine.New(nativeengine.Config{}))
}

func runReflectGetApplyParity(t *testing.T, engine browser.Engine) {
	t.Helper()
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	location, _ := url.Parse("https://parity.gossamer.test/reflect.html")
	page, err := browserRuntime.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	defer page.Close()

	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `
if (Reflect.get.length !== 2 || Reflect.apply.length !== 3) throw new Error("Reflect arity mismatch");

const receiver = { base: 7 };
const prototype = {
  value: 3,
  get doubled() { return this.base * 2; }
};
const target = Object.create(prototype);
target.direct = 5;
if (Reflect.get(target, "direct") !== 5 || Reflect.get(target, "value") !== 3 ||
    Reflect.get(target, "missing") !== undefined || Reflect.get(target, "doubled", receiver) !== 14) {
  throw new Error("Reflect.get ordinary lookup failed");
}

const key = { toString() { return "direct"; } };
if (Reflect.get(target, key) !== 5) throw new Error("Reflect.get property-key conversion failed");

let trappedReceiver;
const proxy = new Proxy(target, {
  get(object, property, suppliedReceiver) {
    trappedReceiver = suppliedReceiver;
    return Reflect.get(object, property, suppliedReceiver);
  }
});
if (Reflect.get(proxy, "doubled", receiver) !== 14 || trappedReceiver !== receiver) {
  throw new Error("Reflect.get Proxy receiver failed");
}

function combine(left, right) { return this.prefix + left + right; }
if (Reflect.apply(combine, { prefix: "result:" }, { 0: "a", 1: "b", length: 2 }) !== "result:ab") {
  throw new Error("Reflect.apply array-like call failed");
}

let failures = 0;
try { Reflect.get(1, "value"); } catch (error) { if (error instanceof TypeError) failures++; }
try { Reflect.apply({}, null, []); } catch (error) { if (error instanceof TypeError) failures++; }
try { Reflect.apply(combine, null, null); } catch (error) { if (error instanceof TypeError) failures++; }
if (failures !== 3) throw new Error("Reflect TypeError contract failed");
`}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
}
