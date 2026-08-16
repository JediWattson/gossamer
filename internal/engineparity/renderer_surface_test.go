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

func TestStrandRendererSurfaceParity(t *testing.T) {
	runRendererSurfaceParity(t, nativeengine.New(nativeengine.Config{}))
}

func runRendererSurfaceParity(t *testing.T, engine browser.Engine) {
	t.Helper()
	root, err := htmlparser.Parse(strings.NewReader(`
<!doctype html><html><head></head><body><main id="root"><span id="seed">seed</span></main></body></html>
`))
	if err != nil {
		t.Fatal(err)
	}
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://parity.gossamer.test/renderer.html")
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

	run("renderer", `
let root = document.getElementById("root");
let seed = document.getElementById("seed");
let rootChildren = root.children;
let rootNodes = root.childNodes;
if (root.children !== rootChildren || root.childNodes !== rootNodes ||
    rootChildren.length !== 1 || rootNodes.length !== 1) {
  throw new Error("same-object collection parity failed");
}

let item = document.createElement("div");
let itemChildren = item.children;
let itemNodes = item.childNodes;
item.className = "one";
let classes = item.classList;
if (item.classList !== classes || classes.value !== "one" || classes.length !== 1) {
  throw new Error("classList identity parity failed");
}
classes.add("two", "three");
classes.remove("two");
if (item.className !== "one three" || !classes.contains("three") || classes.item(1) !== "three") {
  throw new Error("classList mutation parity failed");
}
if (classes.toggle("three") !== false || classes.toggle("four") !== true ||
    classes.toString() !== "one four") {
  throw new Error("classList toggle parity failed");
}

let dataset = item.dataset;
if (item.dataset !== dataset) throw new Error("dataset identity parity failed");
dataset.owner = "strand";
dataset.renderCount = 2;
if (item.getAttribute("data-owner") !== "strand" || dataset.renderCount !== "2") {
  throw new Error("dataset write parity failed");
}
delete dataset.owner;
if (item.hasAttribute("data-owner") || dataset.owner !== undefined) {
  throw new Error("dataset delete parity failed");
}

let style = item.style;
if (item.style !== style) throw new Error("style identity parity failed");
style.cssText = "color: blue";
style.backgroundColor = "green";
style.setProperty("margin-top", "4px", "important");
if (style.getPropertyValue("color") !== "blue" || style.backgroundColor !== "green" ||
    style.getPropertyPriority("margin-top") !== "important" || style.length !== 3) {
  throw new Error("inline style mutation parity failed");
}
if (style.removeProperty("background-color") !== "green" ||
    style.getPropertyValue("background-color") !== "" || style.length !== 2 ||
    style.item(0) !== "color") {
  throw new Error("inline style delete parity failed");
}

item.innerHTML = "<b id=bold>bold</b><i>italics</i>";
if (itemChildren.length !== 2 || itemNodes.length !== 2 ||
    item.querySelector("#bold").textContent !== "bold") {
  throw new Error("innerHTML live collection parity failed");
}
root.appendChild(item);
if (rootChildren.length !== 2 || rootNodes.length !== 2 || rootChildren[1] !== item) {
  throw new Error("append live collection parity failed");
}
item.insertAdjacentHTML("beforeend", "<em id=emphasis>!</em>");
if (itemChildren.length !== 3 || document.getElementById("emphasis").parentNode !== item) {
  throw new Error("insertAdjacentHTML parity failed");
}
item.innerHTML = "<u id=updated>new</u>";
if (itemChildren.length !== 1 || itemNodes.length !== 1 ||
    document.getElementById("bold") !== null || item.firstChild.id !== "updated") {
  throw new Error("innerHTML replacement parity failed");
}
root.removeChild(seed);
if (rootChildren.length !== 1 || rootNodes.length !== 1) {
  throw new Error("remove live collection parity failed");
}
if (item.getAttributeNames().join(",") !== "class,data-render-count,style") {
  throw new Error("attribute name parity failed");
}
`)

	run("renderer-retain", `
if (root.children !== rootChildren || root.childNodes !== rootNodes ||
    item.children !== itemChildren || item.childNodes !== itemNodes ||
    item.classList !== classes || item.dataset !== dataset || item.style !== style ||
    rootChildren.length !== 1 || itemChildren.length !== 1 ||
    dataset.renderCount !== "2" || style.getPropertyValue("color") !== "blue") {
  throw new Error("cross-task renderer identity parity failed");
}
`)

	if err := page.Close(); err != nil {
		t.Fatalf("close page: %v", err)
	}
	if stats := browserRuntime.Ledger().Stats(); stats.LiveObjects != 0 || stats.PersistentObjects != 0 {
		t.Fatalf("renderer teardown ownership = %#v", stats)
	}
	if err := browserRuntime.Close(); err != nil {
		t.Fatalf("close browser: %v", err)
	}
}
