package engineparity

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	htmlparser "github.com/JediWattson/gossamer/internal/html"
	"github.com/JediWattson/gossamer/internal/nativeengine"
)

func TestStrandFormLayoutParity(t *testing.T) {
	runFormLayoutParity(t, nativeengine.New(nativeengine.Config{}))
}

func runFormLayoutParity(t *testing.T, engine browser.Engine) {
	t.Helper()
	root, err := htmlparser.Parse(strings.NewReader(`
<!doctype html><html><head></head><body style="margin:0">
<input id="field" value="start"><input id="check" type="checkbox">
<select id="choice"><option id="zero">zero</option><option id="one" selected>one</option></select>
<div id="box" style="display:block;width:120px;height:20px;color:#123456;background-color:#010203">box</div>
</body></html>
`))
	if err != nil {
		t.Fatal(err)
	}
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://parity.gossamer.test/forms-layout.html")
	page, err := browserRuntime.NewPage(root, location)
	if err != nil {
		t.Fatal(err)
	}

	run := func(label, source string) {
		t.Helper()
		if _, queueErr := page.QueueScript(browser.ScriptSource{
			URL: "https://parity.gossamer.test/" + label + ".js", Source: source,
		}); queueErr != nil {
			t.Fatalf("queue %s: %v", label, queueErr)
		}
		for page.Realm.Tasks.Len() != 0 {
			if runErr := page.Realm.RunOne(context.Background()); runErr != nil {
				t.Fatalf("run %s: %v", label, runErr)
			}
		}
	}

	run("forms-layout", `
let field = document.getElementById("field");
let check = document.getElementById("check");
let choice = document.getElementById("choice");
let zero = document.getElementById("zero");
let one = document.getElementById("one");
if (field.value !== "start" || check.checked || choice.selectedIndex !== 1 ||
    zero.selected || !one.selected) {
  throw new Error("initial form state parity failed");
}
field.value = "strand";
field.setSelectionRange(1, 5, "forward");
if (field.value !== "strand" || field.selectionStart !== 1 ||
    field.selectionEnd !== 5 || field.selectionDirection !== "forward") {
  throw new Error("form value or selection parity failed");
}
field.selectionStart = 2;
field.selectionEnd = 4;
if (field.selectionStart !== 2 || field.selectionEnd !== 4) {
  throw new Error("selection accessor parity failed");
}
check.checked = true;
choice.selectedIndex = 0;
if (!check.checked || choice.selectedIndex !== 0 || !zero.selected || one.selected) {
  throw new Error("checked or selected parity failed");
}
field.focus();
if (document.activeElement !== field) throw new Error("focus parity failed");
field.blur();
if (document.activeElement !== document.body) throw new Error("blur parity failed");
`)

	run("computed-style", `
let box = document.getElementById("box");
let computed = getComputedStyle(box);
if (computed === getComputedStyle(box) || computed.cssText !== "" ||
    computed.display !== "block" || computed.width !== "120px" ||
    computed.color !== "rgb(18, 52, 86)" ||
    computed.getPropertyValue("background-color") !== "rgb(1, 2, 3)" ||
    computed.getPropertyPriority("color") !== "" || computed.length < 20 ||
    computed[0] !== computed.item(0)) {
  throw new Error("computed style parity failed");
}
box.style.width = "140px";
if (computed.width !== "140px") throw new Error("live computed style parity failed");
`)

	run("geometry", `
let rect = box.getBoundingClientRect();
let rectJSON = rect.toJSON();
if (rect === box.getBoundingClientRect() || rect.width !== 140 || rect.height !== 20 ||
    rect.right !== rect.x + rect.width || rect.bottom !== rect.y + rect.height ||
    rectJSON.width !== 140 || box.getClientRects().length !== 1) {
  throw new Error("DOMRect parity failed");
}
`)

	run("element-geometry", `
if (box.clientWidth !== 140 || box.clientHeight !== 20 ||
    box.offsetWidth !== 140 || box.offsetHeight !== 20) {
  throw new Error("element geometry parity failed");
}
`)

	run("viewport-geometry", `
if (window.innerWidth !== 800 || window.innerHeight !== 600 ||
    window.scrollX !== 0 || window.scrollY !== 0 ||
    window.pageXOffset !== window.scrollX || window.pageYOffset !== window.scrollY ||
    document.scrollingElement !== document.documentElement) {
  throw new Error("viewport geometry parity failed");
}
`)

	run("forms-layout-retain", `
box.style.width = "160px";
if (field.value !== "strand" || !check.checked || choice.selectedIndex !== 0 ||
    computed.width !== "160px" || rect.width !== 140 || rect.toJSON().height !== 20 ||
    box.getBoundingClientRect().width !== 160) {
  throw new Error("cross-task form, style, or geometry parity failed");
}
`)

	if err := page.Close(); err != nil {
		t.Fatalf("close page: %v", err)
	}
	if stats := browserRuntime.Ledger().Stats(); stats.LiveObjects != 0 || stats.PersistentObjects != 0 {
		t.Fatalf("form/layout teardown ownership = %#v", stats)
	}
	if err := browserRuntime.Close(); err != nil {
		t.Fatalf("close browser: %v", err)
	}
}
