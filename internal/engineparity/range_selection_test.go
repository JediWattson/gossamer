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

func TestStrandRangeSelectionParity(t *testing.T) {
	runRangeSelectionParity(t, nativeengine.New(nativeengine.Config{}))
}

func runRangeSelectionParity(t *testing.T, engine browser.Engine) {
	t.Helper()
	root, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body><div id="fixture"></div></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://parity.gossamer.test/range-selection.html")
	page, err := browserRuntime.NewPage(root, location)
	if err != nil {
		t.Fatal(err)
	}
	run := func(label, source string) {
		t.Helper()
		if _, queueErr := page.QueueScript(browser.ScriptSource{URL: location.ResolveReference(&url.URL{Path: label + ".js"}).String(), Source: source}); queueErr != nil {
			t.Fatalf("queue %s: %v", label, queueErr)
		}
		for page.Realm.Tasks.Len() != 0 {
			if runErr := page.Realm.RunOne(context.Background()); runErr != nil {
				t.Fatalf("run %s: %v", label, runErr)
			}
		}
	}

	run("range", `
let fixture = document.getElementById("fixture");
fixture.innerHTML = '<div id="range-root"><p id="left">ab<strong>cd</strong></p><p id="right"><i>ef</i>gh</p></div>';
let rangeRoot = document.getElementById("range-root");
let left = document.getElementById("left");
let right = document.getElementById("right");
let range = document.createRange();
if (!(range instanceof Range)) throw new Error("Range identity failed");
range.setStart(left.firstChild, 1);
range.setEnd(right.lastChild, 1);
let cloned = range.cloneContents();
if (cloned.textContent !== "bcdefg" || cloned.children.length !== 2 ||
    range.commonAncestorContainer !== rangeRoot || range.collapsed) {
  throw new Error("Range clone projection failed: " + cloned.textContent);
}
let copied = range.cloneRange();
if (copied.startContainer !== range.startContainer || copied.startOffset !== 1 || copied.endOffset !== 1) {
  throw new Error("Range cloneRange failed");
}

let extractRoot = rangeRoot.cloneNode(true);
fixture.appendChild(extractRoot);
let movedStrong = extractRoot.children[0].children[0];
let extractRange = document.createRange();
extractRange.setStart(extractRoot.children[0].firstChild, 1);
extractRange.setEnd(extractRoot.children[1].lastChild, 1);
let extracted = extractRange.extractContents();
if (extracted.textContent !== "bcdefg" || extracted.children[0].children[0] !== movedStrong ||
    extractRoot.textContent !== "ah" || !extractRange.collapsed) {
  throw new Error("Range extractContents failed");
}

let insertionHost = document.createElement("div");
let insertionText = document.createTextNode("A😀B");
insertionHost.appendChild(insertionText);
let insertionRange = document.createRange();
insertionRange.setStart(insertionText, 3);
insertionRange.collapse(true);
let mark = document.createElement("mark");
insertionRange.insertNode(mark);
if (insertionHost.childNodes.length !== 3 || insertionHost.childNodes[0].data !== "A😀" ||
    insertionHost.childNodes[1] !== mark || insertionHost.childNodes[2].data !== "B") {
  throw new Error("Range insertNode UTF-16 split failed");
}

let selection = getSelection();
if (!(selection instanceof Selection) || selection !== document.getSelection() ||
    selection !== window.getSelection() || selection.rangeCount !== 0 || selection.type !== "None") {
  throw new Error("canonical Selection failed");
}
let selectionRoot = document.createElement("div");
selectionRoot.innerHTML = "<span>start</span><b>end</b>";
let selectionRange = document.createRange();
selectionRange.setStart(selectionRoot.firstElementChild.firstChild, 1);
selectionRange.setEnd(selectionRoot.lastElementChild.firstChild, 2);
selection.addRange(selectionRange);
if (selection.getRangeAt(0) !== selectionRange || selection.anchorOffset !== 1 ||
    selection.focusOffset !== 2 || selection.toString() !== "tarten" || selection.type !== "Range") {
  throw new Error("Selection projection failed: " + selection.toString());
}
selection.collapse(selectionRoot.firstElementChild.firstChild, 2);
if (!selection.isCollapsed || selection.type !== "Caret" || selection.anchorOffset !== 2) {
  throw new Error("Selection collapse failed");
}
selection.selectAllChildren(selectionRoot);
if (selection.toString() !== "startend") throw new Error("Selection selectAllChildren failed");

let deleteHost = document.createElement("div");
let deleteText = document.createTextNode("A😀BC");
deleteHost.appendChild(deleteText);
let deleteRange = document.createRange();
deleteRange.setStart(deleteText, 1);
deleteRange.setEnd(deleteText, 3);
selection.removeAllRanges();
selection.addRange(deleteRange);
selection.deleteFromDocument();
if (deleteText.data !== "ABC" || !selection.isCollapsed) throw new Error("Selection delete failed");

let heldRoot = document.createElement("div");
heldRoot.textContent = "held";
let heldRange = document.createRange();
heldRange.selectNodeContents(heldRoot);
selection.removeAllRanges();
selection.addRange(heldRange);
globalThis.__heldSelection = selection;
`)

	run("retained", `
let retained = getSelection();
if (retained !== __heldSelection || retained.toString() !== "held" ||
    retained.anchorNode.textContent !== "held") {
  throw new Error("Selection did not retain detached boundaries");
}
retained.removeAllRanges();
globalThis.__heldSelection = undefined;
`)

	if err := page.Close(); err != nil {
		t.Fatal(err)
	}
	if stats := browserRuntime.Ledger().Stats(); stats.LiveObjects != 0 || stats.PersistentObjects != 0 {
		t.Fatalf("Range/Selection teardown ownership = %#v", stats)
	}
	if err := browserRuntime.Close(); err != nil {
		t.Fatal(err)
	}
}
