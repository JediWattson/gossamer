package engineparity

import (
	"context"
	"net/url"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/nativeengine"
)

func TestStrandStringConcatParity(t *testing.T) {
	runStringConcatParity(t, nativeengine.New(nativeengine.Config{}))
}

func runStringConcatParity(t *testing.T, engine browser.Engine) {
	t.Helper()
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	location, _ := url.Parse("https://parity.gossamer.test/string-concat.html")
	page, err := browserRuntime.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	defer page.Close()

	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `
const order = [];
const converted = {
  toString() { order.push("converted"); return "value"; }
};
if ("prefix:".concat(converted, ":", null, ":", undefined) !== "prefix:value:null:undefined" ||
    order.join(",") !== "converted" || String.prototype.concat.call(12, "px") !== "12px" ||
    String.prototype.concat.length !== 1) {
  throw new Error("String.prototype.concat conversion failed");
}
let failures = 0;
try { String.prototype.concat.call(null, "x"); } catch (error) { if (error instanceof TypeError) failures++; }
try { "x".concat(Symbol("y")); } catch (error) { if (error instanceof TypeError) failures++; }
if (failures !== 2) throw new Error("String.prototype.concat TypeError contract failed");
`}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
}
