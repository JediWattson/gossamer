package nativeengine_test

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	htmlparser "github.com/JediWattson/gossamer/internal/html"
	"github.com/JediWattson/gossamer/internal/nativeengine"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func TestNativeIframeWindowProxyPostsBoundedOriginCheckedMessages(t *testing.T) {
	t.Parallel()

	engine := nativeengine.New(nativeengine.Config{})
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	parentRoot, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body><iframe id="child"></iframe></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	parentLocation, _ := url.Parse("https://parent.test/app")
	parent, err := browserRuntime.NewPage(parentRoot, parentLocation)
	if err != nil {
		t.Fatal(err)
	}
	iframe, found := parent.Document().ElementByID("child")
	if !found {
		t.Fatal("missing iframe")
	}
	iframeHandle := browser.NodeHandle{Document: parent.DocumentGeneration(), Node: iframe}
	childRoot, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	childLocation, _ := url.Parse("https://child.test/widget")
	child, err := parent.AttachChildFrame(iframeHandle, childRoot, childLocation)
	if err != nil {
		t.Fatal(err)
	}

	run := func(page *browser.Page, label, source string) {
		t.Helper()
		if _, err := page.QueueScript(browser.ScriptSource{URL: label, Source: source}); err != nil {
			t.Fatal(err)
		}
		if err := page.Realm.RunOne(context.Background()); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
	}
	run(parent, "https://parent.test/listener.js", `
		let reply = "";
		window.addEventListener("message", function (event) {
			if (event.origin !== "https://child.test") throw new Error("wrong reply origin");
			reply = event.data.ack;
		});
	`)
	run(child, "https://child.test/listener.js", `
		let received = "";
		window.addEventListener("message", function (event) {
			if (event.origin !== "https://parent.test") throw new Error("wrong source origin");
			if (event.source !== window.parent) throw new Error("source proxy lost identity");
			received = event.data.kind + ":" + event.data.count;
			event.source.postMessage({ ack: received }, "https://parent.test");
		});
	`)
	run(parent, "https://parent.test/send.js", `
		const target = document.getElementById("child").contentWindow;
		if (target === null) throw new Error("missing contentWindow");
		target.postMessage({ kind: "ready", count: 3 }, "https://child.test");
	`)
	if err := child.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := parent.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	run(child, "https://child.test/verify.js", `
		if (received !== "ready:3") throw new Error("message data was not cloned");
	`)
	run(parent, "https://parent.test/verify.js", `
		if (reply !== "ready:3") throw new Error("reply did not cross WindowProxy");
	`)

	run(parent, "https://parent.test/drop.js", `
		document.getElementById("child").contentWindow.postMessage({ kind: "blocked" }, "https://other.test");
	`)
	if child.Realm.Tasks.Len() != 0 {
		t.Fatalf("targetOrigin mismatch queued %d child tasks", child.Realm.Tasks.Len())
	}
}

func TestNativeDisconnectedIframeContentWindowIsNull(t *testing.T) {
	t.Parallel()

	engine := nativeengine.New(nativeengine.Config{})
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	location, _ := url.Parse("https://parent.test/")
	page, err := browserRuntime.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `
		const frame = document.createElement("iframe");
		if (frame.contentWindow !== null) throw new Error("disconnected iframe owns a child context");
	`}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestNativeWindowEventTargetMethodsAreGlobalAliases(t *testing.T) {
	t.Parallel()

	engine := nativeengine.New(nativeengine.Config{})
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	location, _ := url.Parse("https://parent.test/")
	page, err := browserRuntime.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `
		if (globalThis !== window || self !== window) throw new Error("global Window identity is inconsistent");
		if (globalThis.Object !== Object || globalThis.Math !== Math || globalThis.JSON !== JSON) {
			throw new Error("ECMAScript globals were not projected onto Window");
		}
		if (addEventListener !== window.addEventListener) throw new Error("addEventListener alias lost identity");
		if (removeEventListener !== window.removeEventListener) throw new Error("removeEventListener alias lost identity");
		if (dispatchEvent !== window.dispatchEvent) throw new Error("dispatchEvent alias lost identity");
		if (postMessage !== window.postMessage) throw new Error("postMessage alias lost identity");
		let observed = 0;
		function listener() { observed++; }
		addEventListener("probe", listener);
		dispatchEvent(new Event("probe"));
		removeEventListener("probe", listener);
		dispatchEvent(new Event("probe"));
		if (observed !== 1) throw new Error("global EventTarget methods did not target Window");
	`}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestNativeIndirectEvalUsesTheRealmGlobalEnvironment(t *testing.T) {
	t.Parallel()

	engine := nativeengine.New(nativeengine.Config{})
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	location, _ := url.Parse("https://parent.test/")
	page, err := browserRuntime.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `
		const indirect = (0, eval);
		if (indirect("1 + 2") !== 3) throw new Error("indirect eval lost its completion value");
		if (indirect("this") !== window) throw new Error("indirect eval did not use the Window global this");
		indirect("globalThis.evalMarker = 7");
		if (window.evalMarker !== 7) throw new Error("indirect eval did not update the Realm global object");
		const object = {};
		if (eval(object) !== object) throw new Error("eval did not preserve a non-string argument");
	`}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestNativeWindowBase64GlobalsUseBinaryStrings(t *testing.T) {
	t.Parallel()

	engine := nativeengine.New(nativeengine.Config{})
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	location, _ := url.Parse("https://parent.test/")
	page, err := browserRuntime.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `
		if (window.atob !== atob || window.btoa !== btoa) throw new Error("base64 globals lost Window identity");
		if (atob("SGV s\nbG8=") !== "Hello") throw new Error("atob did not ignore ASCII whitespace");
		const binary = atob("AAEC/w==");
		if (binary.length !== 4 || binary.charCodeAt(0) !== 0 || binary.charCodeAt(3) !== 255) {
			throw new Error("atob did not return a binary string");
		}
		if (btoa(String.fromCharCode(0, 1, 2, 255)) !== "AAEC/w==") throw new Error("btoa did not encode a binary string");
		let invalid = false;
		try { btoa("\u2713"); } catch (error) { invalid = error.name === "InvalidCharacterError"; }
		if (!invalid) throw new Error("btoa accepted a non-Latin1 string");
	`}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestNativeBigIntGlobalConvertsCorePrimitiveInputs(t *testing.T) {
	t.Parallel()

	engine := nativeengine.New(nativeengine.Config{})
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	location, _ := url.Parse("https://parent.test/")
	page, err := browserRuntime.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `
		if (window.BigInt !== BigInt) throw new Error("BigInt was not projected onto Window");
		if (typeof BigInt("9007199254740993") !== "bigint") throw new Error("BigInt string conversion returned the wrong type");
		if (String(BigInt("9007199254740993")) !== "9007199254740993") throw new Error("BigInt lost decimal precision");
		if (String(BigInt("0x2a")) !== "42" || String(BigInt(42)) !== "42" || String(BigInt(true)) !== "1") {
			throw new Error("BigInt primitive conversion failed");
		}
		if (BigInt(7n) !== 7n) throw new Error("BigInt did not preserve a BigInt argument");
		if (Number(BigInt(42)) !== 42) throw new Error("Number did not explicitly convert BigInt");
		if (String(9n + 4n) !== "13" || String(9n - 4n) !== "5" || String(9n * 4n) !== "36") throw new Error("BigInt arithmetic failed");
		if (String(9n / 4n) !== "2" || String(9n % 4n) !== "1" || String(3n ** 4n) !== "81") throw new Error("BigInt division or power failed");
		if (String(6n & 3n) !== "2" || String(6n | 3n) !== "7" || String(6n ^ 3n) !== "5") throw new Error("BigInt bitwise arithmetic failed");
		if (String(3n << 4n) !== "48" || String(48n >> 4n) !== "3" || String(-8n >> 1n) !== "-4") throw new Error("BigInt shifts failed");
		if (!(9n > 4n) || !(4n < 4.5) || !(4n == "4") || 4n == "4.5") throw new Error("BigInt comparisons failed");
		let mixed = false;
		try { 1n + 1; } catch (_) { mixed = true; }
		if (!mixed) throw new Error("BigInt and Number mixed silently");
		let rejected = false;
		try { BigInt(1.5); } catch (_) { rejected = true; }
		if (!rejected) throw new Error("BigInt accepted a fractional Number");
	`}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestNativeCryptoGetRandomValuesFillsUint8ArrayInPlace(t *testing.T) {
	t.Parallel()

	engine := nativeengine.New(nativeengine.Config{})
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	location, _ := url.Parse("https://parent.test/")
	page, err := browserRuntime.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `
		const values = new Uint8Array(32);
		if (crypto.getRandomValues(values) !== values) throw new Error("getRandomValues did not return its input");
		let nonzero = false;
		for (let index = 0; index < values.length; index++) if (values[index] !== 0) nonzero = true;
		if (!nonzero) throw new Error("getRandomValues did not fill its input");
		let limited = false;
		try { crypto.getRandomValues(new Uint8Array(65537)); } catch (error) { limited = error.name === "QuotaExceededError"; }
		if (!limited) throw new Error("getRandomValues did not enforce the 65536-byte limit");
	`}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestNativePerformanceTimelineExposesNavigationEntry(t *testing.T) {
	t.Parallel()

	engine := nativeengine.New(nativeengine.Config{})
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	location, _ := url.Parse("https://parent.test/")
	root, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><head><style media="screen">body { color: red }</style></head><body></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	page, err := browserRuntime.NewPage(root, location)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `
		const entries = performance.getEntries();
		if (Node.ELEMENT_NODE !== 1 || Node.TEXT_NODE !== 3 || document.createTextNode("x").nodeType !== Node.TEXT_NODE ||
			document.scripts.length !== 0 || document.currentScript !== null) {
			throw new Error("Node constants or Document script reflection failed");
		}
		const sheet = document.styleSheets[0];
		if (document.styleSheets.length !== 1 || sheet.ownerNode.tagName !== "STYLE" || sheet.href !== null ||
			sheet.type !== "text/css" || sheet.media.mediaText !== "screen" || sheet.cssRules.length !== 0) {
			throw new Error("Document styleSheets projection failed");
		}
		if (document.title !== "") throw new Error("initial document title was not empty");
		document.title = "Native title";
		if (document.title !== "Native title") throw new Error("document title did not round trip");
		const reflected = document.createElement("div");
		reflected.setAttribute("data-state", "ready");
		if (document.visibilityState !== "visible" || document.hidden !== false || reflected.tagName !== "DIV") {
			throw new Error("document visibility or Element tagName reflection failed");
		}
		if (reflected.attributes.length !== 1 || reflected.attributes[0].name !== "data-state" ||
			reflected.attributes.item(0).value !== "ready" || reflected.attributes.getNamedItem("data-state").ownerElement !== reflected) {
			throw new Error("Element attributes reflection failed");
		}
		const span = document.createElement("span");
		const emphasis = document.createElement("em");
		span.appendChild(document.createTextNode("text"));
		span.appendChild(emphasis);
		reflected.appendChild(span);
		const walker = document.createTreeWalker(reflected, NodeFilter.SHOW_ELEMENT);
		if (!(walker instanceof TreeWalker) || walker.root !== reflected || walker.currentNode !== reflected ||
			walker.whatToShow !== NodeFilter.SHOW_ELEMENT || walker.filter !== null || walker.nextNode() !== span ||
			walker.nextNode() !== emphasis || walker.nextNode() !== null) {
			throw new Error("TreeWalker element traversal failed");
		}
		walker.currentNode = reflected;
		const filtered = document.createTreeWalker(reflected, NodeFilter.SHOW_ELEMENT, function (node) {
			return node.tagName === "EM" ? NodeFilter.FILTER_ACCEPT : NodeFilter.FILTER_SKIP;
		});
		if (filtered.nextNode() !== emphasis || document.referrer !== "") {
			throw new Error("TreeWalker filtering or document referrer failed");
		}
		const iterator = document.createNodeIterator(reflected, NodeFilter.SHOW_ELEMENT);
		if (!(iterator instanceof NodeIterator) || iterator.root !== reflected || iterator.referenceNode !== reflected ||
			iterator.pointerBeforeReferenceNode !== true || iterator.nextNode() !== reflected ||
			iterator.pointerBeforeReferenceNode !== false || iterator.nextNode() !== span || iterator.nextNode() !== emphasis ||
			iterator.nextNode() !== null || iterator.detach() !== undefined) {
			throw new Error("NodeIterator traversal failed");
		}
		if (entries.length !== 1 || entries[0].entryType !== "navigation" || entries[0].type !== "navigate") {
			throw new Error("performance timeline omitted navigation");
		}
		if (typeof entries[0].requestStart !== "number" || typeof entries[0].responseStart !== "number" ||
			typeof entries[0].responseEnd !== "number" || entries[0].transferSize !== 0 || entries[0].encodedBodySize !== 0) {
			throw new Error("navigation timing fields are incomplete");
		}
		if (performance.getEntriesByType("navigation").length !== 1 || performance.getEntriesByType("resource").length !== 0) {
			throw new Error("performance entry type filtering failed");
		}
		if (performance.getEntriesByName("https://parent.test/").length !== 1 || performance.getEntriesByName("missing").length !== 0) {
			throw new Error("performance entry name filtering failed");
		}
		if (PerformanceObserver.supportedEntryTypes.length !== 1 || PerformanceObserver.supportedEntryTypes[0] !== "navigation") {
			throw new Error("PerformanceObserver supportedEntryTypes is incorrect");
		}
		let callbacks = 0;
		const observer = new PerformanceObserver(function () { callbacks++; });
		observer.observe({ type: "resource", buffered: true });
		if (observer.takeRecords().length !== 0 || callbacks !== 0) {
			throw new Error("PerformanceObserver invented unsupported records");
		}
		observer.disconnect();
		let rejected = false;
		try { observer.observe(); } catch (error) { rejected = error instanceof TypeError; }
		if (!rejected) throw new Error("PerformanceObserver accepted missing options");
	`}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestNativePropertyMissDiagnosticReportsShapeWithoutRef(t *testing.T) {
	var samples []browserruntime.PropertyMissSample
	engine := nativeengine.New(nativeengine.Config{Interpreter: browserruntime.InterpreterConfig{
		PropertyMiss: func(sample browserruntime.PropertyMissSample) {
			samples = append(samples, sample)
		},
	}})
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	location, _ := url.Parse("https://diagnostic.test/")
	page, err := browserRuntime.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `const ignored = ({ present: 1 }).missing;`}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, sample := range samples {
		if sample.Name == "missing" && sample.BaseKind == memory.ValueReference && sample.HeapKind == memory.HeapObject {
			return
		}
	}
	t.Fatalf("property-miss samples = %#v", samples)
}
