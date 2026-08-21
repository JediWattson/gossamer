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

func TestStrandCoordinatedFormStateParity(t *testing.T) {
	engine := nativeengine.New(nativeengine.Config{})
	runCoordinatedFormStateParity(t, engine, func(page *browser.Page) error {
		realm, ok := engine.LatestRealm()
		if !ok {
			return nativeengine.ErrRealmClosed
		}
		return realm.CollectGarbage(page)
	})
}

func runCoordinatedFormStateParity(t *testing.T, engine browser.Engine, collect func(*browser.Page) error) {
	t.Helper()
	root, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body>
<form id="account">
  <input id="first" name="choice" type="radio" checked>
  <input id="second" name="choice" type="radio">
  <textarea id="notes" name="notes">default notes</textarea>
  <select id="kind" name="kind">
    <option id="one" value="one">One</option>
    <option id="two" value="two" selected>Two</option>
  </select>
  <button id="save" name="save">Save</button>
</form>
<input id="external" name="external" form="account" value="outside">
</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://parity.gossamer.test/form-state.html")
	page, err := browserRuntime.NewPage(root, location)
	if err != nil {
		t.Fatal(err)
	}
	run := func(label, source string) {
		t.Helper()
		if _, queueErr := page.QueueScript(browser.ScriptSource{
			URL: location.ResolveReference(&url.URL{Path: label + ".js"}).String(), Source: source,
		}); queueErr != nil {
			t.Fatalf("queue %s: %v", label, queueErr)
		}
		for page.Realm.Tasks.Len() != 0 {
			if runErr := page.Realm.RunOne(context.Background()); runErr != nil {
				t.Fatalf("run %s: %v", label, runErr)
			}
		}
	}

	run("form-state", `
(() => {
  const form = document.getElementById("account");
  const first = document.getElementById("first");
  const second = document.getElementById("second");
  const notes = document.getElementById("notes");
  const select = document.getElementById("kind");
  const one = document.getElementById("one");
  const two = document.getElementById("two");
  const external = document.getElementById("external");
  const elements = form.elements;
  const options = select.options;

  if (!(elements instanceof HTMLCollection) || form.elements !== elements ||
      elements.length !== 6 || elements.namedItem("notes") !== notes ||
      elements.external !== external || elements[3] !== select ||
      elements.item(0) !== first || elements.item(99) !== null ||
      !(options instanceof HTMLCollection) || select.options !== options ||
      options.length !== 2 || options[1] !== two || options.item(1) !== two ||
      options.namedItem("one") !== one || typeof options.keys !== "function" ||
      typeof options.values !== "function" || typeof options.entries !== "function") {
    throw new Error("live form collection shape or canonical identity failed");
  }
  const iteratedOptions = [];
  const optionIterator = options[Symbol.iterator]();
  let optionStep = optionIterator.next();
  while (!optionStep.done) {
    iteratedOptions.push(optionStep.value.value);
    optionStep = optionIterator.next();
  }
  if (iteratedOptions.join(",") !== "one,two") {
    throw new Error("HTMLCollection iteration failed");
  }
  if (first.form !== form || notes.form !== form || select.form !== form ||
      external.form !== form || document.createElement("input").form !== null) {
    throw new Error("form owner association failed");
  }
  if (elements.namedItem("") !== null) {
    throw new Error("empty collection name should not match");
  }
  external.removeAttribute("form");
  if (elements.length !== 5 || external.form !== null) {
    throw new Error("removing explicit form association did not update collection");
  }
  external.setAttribute("form", "account");
  form.id = "renamed";
  if (elements.length !== 5 || external.form !== null) {
    throw new Error("changing form identity did not update collection");
  }
  external.setAttribute("form", "renamed");
  if (elements.length !== 6 || external.form !== form) {
    throw new Error("restoring explicit form association did not update collection");
  }
  if (!first.checked || second.checked || notes.value !== "default notes" ||
      select.value !== "two" || select.selectedIndex !== 1 || one.selected || !two.selected ||
      !two.defaultSelected) {
    throw new Error("initial control state failed");
  }

  second.checked = true;
  if (first.checked || !second.checked) throw new Error("radio coordination failed");
  notes.value = "user notes";
  select.value = "one";
  if (select.selectedIndex !== 0 || !one.selected || two.selected ||
      select.value !== "one" || !two.defaultSelected) {
    throw new Error("select current/default state split failed");
  }
  one.selected = false;
  if (select.selectedIndex !== -1 || select.value !== "") {
    throw new Error("explicit empty selection failed");
  }
  const three = document.createElement("option");
  three.id = "three";
  three.value = "three";
  three.textContent = "Three";
  select.append(three);
  if (options.length !== 3 || options[2] !== three || options.three !== three) {
    throw new Error("select.options is not live");
  }

  form.reset();
  if (!first.checked || second.checked || notes.value !== "default notes" ||
      select.value !== "two" || select.selectedIndex !== 1 || !two.selected) {
    throw new Error("form.reset did not restore markup defaults");
  }

  form.remove();
  if (elements.length !== 5 || elements[2] !== notes || options.length !== 3) {
    throw new Error("detached live form collections lost their owner subtree");
  }
  globalThis.__heldFormElements = elements;
  globalThis.__heldSelectOptions = options;
})();
`)
	if err := collect(page); err != nil {
		t.Fatalf("collect with live form collections: %v", err)
	}
	run("retained", `
if (__heldFormElements.namedItem("notes").value !== "default notes" ||
    __heldSelectOptions[1].value !== "two" ||
    !(__heldFormElements instanceof HTMLCollection)) {
  throw new Error("collection-only reachability lost native form state");
}
globalThis.__heldFormElements = undefined;
globalThis.__heldSelectOptions = undefined;
`)
	if err := collect(page); err != nil {
		t.Fatalf("collect after form collection release: %v", err)
	}
	if err := page.Close(); err != nil {
		t.Fatal(err)
	}
	if stats := browserRuntime.Ledger().Stats(); stats.LiveObjects != 0 || stats.PersistentObjects != 0 {
		t.Fatalf("coordinated form teardown ownership = %#v", stats)
	}
	if err := browserRuntime.Close(); err != nil {
		t.Fatal(err)
	}
}
