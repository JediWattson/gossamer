package engineparity

import (
	"context"
	"net/url"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/nativeengine"
)

func TestStrandProxyAndReflectParity(t *testing.T) {
	browserRuntime, err := browser.NewWithEngine(nativeengine.New(nativeengine.Config{}))
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	location, _ := url.Parse("https://parity.gossamer.test/proxy")
	page, err := browserRuntime.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	defer page.Close()

	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `
const target = { value: 2 };
const handler = {
  get(object, key) { return key === "double" ? object.value * 2 : object[key]; },
  set(object, key, value) { object[key] = value; return true; },
  has(object, key) { return key === "virtual" || key in object; },
  deleteProperty(object, key) { return delete object[key]; },
  ownKeys() { return ["value", "virtual"]; },
  getOwnPropertyDescriptor(object, key) {
    if (key === "virtual") return { value: 9, writable: true, enumerable: true, configurable: true };
    return Reflect.getOwnPropertyDescriptor(object, key);
  }
};
const proxy = new Proxy(target, handler);
if (proxy.double !== 4 || !("virtual" in proxy)) throw new Error("Proxy get/has failed");
proxy.value = 5;
if (target.value !== 5 || proxy.double !== 10) throw new Error("Proxy set failed");
const keys = Reflect.ownKeys(proxy);
if (keys.length !== 2 || Object.keys(proxy).join(",") !== "value,virtual") throw new Error("Proxy ownKeys failed");
const descriptor = Object.getOwnPropertyDescriptor(proxy, "virtual");
if (!descriptor || descriptor.value !== 9 || !descriptor.enumerable) throw new Error("Proxy descriptor failed");
if (!Array.isArray(new Proxy([], {}))) throw new Error("proxied Array identity failed");
const proxiedValues = new Proxy([1, 2, 3], {});
if (proxiedValues.filter(value => value > 1).join(",") !== "2,3" ||
    proxiedValues.map(value => value * 2).join(",") !== "2,4,6") {
  throw new Error("proxied Array methods failed");
}
let proxiedTotal = 0;
proxiedValues.forEach(value => { proxiedTotal += value; });
if (proxiedTotal !== 6) throw new Error("proxied Array forEach failed");
if (proxiedValues.findIndex(value => value === 2) !== 1) {
  throw new Error("proxied Array findIndex failed");
}
if (proxiedValues.reverse() !== proxiedValues || proxiedValues[0] !== 3 ||
    proxiedValues[1] !== 2 || proxiedValues[2] !== 1) {
  throw new Error("proxied Array reverse failed");
}
delete proxy.value;
if ("value" in target) throw new Error("Proxy delete failed");
`}); err != nil {
		t.Fatal(err)
	}
	for page.Realm.Tasks.Len() != 0 {
		if err := page.Realm.RunOne(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
}
