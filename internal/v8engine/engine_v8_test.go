//go:build v8 && cgo && darwin && arm64

package v8engine

import (
	"context"
	"image/color"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/loader"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestStockV8EvaluationMicrotasksProfilingAndTeardown(t *testing.T) {
	engine := newTestEngine(t)
	realmValue, err := engine.NewRealm()
	if err != nil {
		t.Fatalf("NewRealm: %v", err)
	}
	realm := realmValue.(*Realm)

	baseline, err := realm.Profile()
	if err != nil {
		t.Fatalf("baseline Profile: %v", err)
	}
	if baseline.Heap.UsedHeapSize == 0 || baseline.Heap.NativeContexts != 1 {
		t.Fatalf("baseline heap = %#v", baseline.Heap)
	}

	evaluate(t, realm, "microtask-start.js", `
		globalThis.__gossamerCheckpoint = 0;
		Promise.resolve().then(() => { globalThis.__gossamerCheckpoint = 42; });
		if (globalThis.__gossamerCheckpoint !== 0) {
			throw new Error("microtask ran without a Gossamer checkpoint");
		}
	`)
	if err := realm.DrainMicrotasks(nil); err != nil {
		t.Fatalf("DrainMicrotasks: %v", err)
	}
	evaluate(t, realm, "microtask-finish.js", `
		if (globalThis.__gossamerCheckpoint !== 42) {
			throw new Error("explicit microtask checkpoint did not run");
		}
	`)
	evaluate(t, realm, "allocation-profile.js", `
		globalThis.__gossamerProfileObjects = Array.from(
			{ length: 25000 },
			(_, index) => ({ index, payload: "gossamer".repeat(16) }),
		);
	`)

	profile, err := realm.Profile()
	if err != nil {
		t.Fatalf("Profile: %v", err)
	}
	if profile.Evaluations != 3 || profile.MicrotaskCheckpoints != 1 {
		t.Fatalf("execution profile = %#v", profile)
	}
	if profile.Heap.TotalAllocatedBytes <= baseline.Heap.TotalAllocatedBytes {
		t.Fatalf("allocated bytes = %d, baseline %d", profile.Heap.TotalAllocatedBytes, baseline.Heap.TotalAllocatedBytes)
	}
	if profile.Sampling.Samples == 0 || profile.Sampling.SampledBytes == 0 {
		t.Fatalf("sampling profile = %#v", profile.Sampling)
	}

	evaluate(t, realm, "allocation-release.js", `
		globalThis.__gossamerProfileObjects = undefined;
	`)
	if err := realm.CollectGarbage(); err != nil {
		t.Fatalf("CollectGarbage: %v", err)
	}
	afterGC, err := realm.Profile()
	if err != nil {
		t.Fatalf("Profile after GC: %v", err)
	}
	if afterGC.GCPrologues == 0 || afterGC.GCEpilogues == 0 || afterGC.MajorGCs == 0 {
		t.Fatalf("GC profile = %#v", afterGC)
	}

	err = realm.Evaluate(nil, browser.ScriptSource{URL: "exception-profile.js", Source: `throw new Error("profile-boom")`})
	if err == nil || !strings.Contains(err.Error(), "profile-boom") || !strings.Contains(err.Error(), "exception-profile.js") {
		t.Fatalf("exception = %v", err)
	}

	if err := realm.Close(); err != nil {
		t.Fatalf("Close realm: %v", err)
	}
	if err := realm.CollectGarbage(); err != ErrRealmClosed {
		t.Fatalf("CollectGarbage after close = %v, want %v", err, ErrRealmClosed)
	}
	engineProfile := engine.Profile()
	if engineProfile.RealmsCreated != 1 || engineProfile.RealmsClosed != 1 {
		t.Fatalf("engine profile = %#v", engineProfile)
	}
	if engineProfile.ClosedRealms.Evaluations != 5 {
		t.Fatalf("closed realm profile = %#v", engineProfile.ClosedRealms)
	}
	if err := engine.Close(); err != nil {
		t.Fatalf("Close engine: %v", err)
	}

	t.Logf(
		"V8 %s allocated=%d used=%d sampled=%d live=%d major_gc=%d gc_time=%s eval_time=%s",
		engineProfile.Version,
		afterGC.Heap.TotalAllocatedBytes,
		afterGC.Heap.UsedHeapSize,
		afterGC.Sampling.SampledBytes,
		afterGC.Sampling.LiveBytes,
		afterGC.MajorGCs,
		afterGC.GCTime,
		afterGC.EvaluationTime,
	)
}

func TestStockV8RunsInBrowserScriptSequence(t *testing.T) {
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatalf("NewWithEngine: %v", err)
	}
	defer func() {
		if err := browserRuntime.Close(); err != nil {
			t.Errorf("Close browser: %v", err)
		}
	}()

	document := `<!doctype html>
		<html><body>
		<script>
			globalThis.__gossamerBrowserCheckpoint = 0;
			Promise.resolve().then(() => { globalThis.__gossamerBrowserCheckpoint = 1; });
		</script>
		<script>
			if (globalThis.__gossamerBrowserCheckpoint !== 1) {
				throw new Error("browser did not own the V8 microtask checkpoint");
			}
		</script>
		<p>stock V8 reached paint</p>
		</body></html>`
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page, err := browserRuntime.LoadPage(ctx, "https://gossamer.test/v8", staticDocumentLoader{document: document})
	if err != nil {
		t.Fatalf("LoadPage: %v", err)
	}
	snapshot := page.Navigation()
	if snapshot.State != browser.NavigationComplete || snapshot.ScriptsTotal != 2 || snapshot.ScriptsFailed != 0 {
		t.Fatalf("navigation = %#v", snapshot)
	}
	if page.Frame() == nil {
		t.Fatal("stock V8 navigation did not publish a frame")
	}

	profile := engine.Profile()
	if profile.RealmsCreated != 2 || profile.RealmsClosed != 1 {
		t.Fatalf("pre-close engine profile = %#v", profile)
	}
}

func TestStockV8HostMicrotaskAndTimerBindingsUseOpaqueHandles(t *testing.T) {
	engine := newTestEngine(t)
	defer engine.Close()
	realmValue, err := engine.NewRealm()
	if err != nil {
		t.Fatalf("NewRealm: %v", err)
	}
	realm := realmValue.(*Realm)
	host := &capturingBindingHost{nextTimer: 40}
	if err := realm.Evaluate(host, browser.ScriptSource{
		URL: "host-bindings.js",
		Source: `
			queueMicrotask(() => { globalThis.__gossamerHostMicrotask = 42; });
			const canceled = setTimeout(() => {
				throw new Error("cleared timer callback ran");
			}, 25);
			clearTimeout(canceled);
		`,
	}); err != nil {
		t.Fatalf("Evaluate host bindings: %v", err)
	}
	if len(host.microtasks) != 1 || len(host.timerCallbacks) != 1 {
		t.Fatalf("published handles = microtasks:%v timers:%v", host.microtasks, host.timerCallbacks)
	}
	if host.timerDelay != 25*time.Millisecond || host.clearedTimer != 41 {
		t.Fatalf("timer boundary = delay:%s cleared:%d", host.timerDelay, host.clearedTimer)
	}
	profile, err := realm.Profile()
	if err != nil {
		t.Fatalf("Profile after host bindings: %v", err)
	}
	if profile.CallbacksCreated != 2 || profile.CallbacksInvoked != 0 || profile.LiveCallbacks != 1 {
		t.Fatalf("callback retention before invocation = %#v", profile)
	}
	if err := realm.Invoke(host, host.microtasks[0]); err != nil {
		t.Fatalf("Invoke microtask handle: %v", err)
	}
	if err := realm.Evaluate(nil, browser.ScriptSource{
		URL:    "host-bindings-check.js",
		Source: `if (globalThis.__gossamerHostMicrotask !== 42) throw new Error("host microtask did not run");`,
	}); err != nil {
		t.Fatalf("Evaluate microtask check: %v", err)
	}
	afterInvoke, err := realm.Profile()
	if err != nil {
		t.Fatalf("Profile after host callback: %v", err)
	}
	if afterInvoke.CallbacksInvoked != 1 || afterInvoke.LiveCallbacks != 0 {
		t.Fatalf("callback retention after invocation = %#v", afterInvoke)
	}
}

func TestStockV8DOMWrapperClickMutatesThroughQueuedCallbackAndPaint(t *testing.T) {
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatalf("NewWithEngine: %v", err)
	}
	defer func() {
		if err := browserRuntime.Close(); err != nil {
			t.Errorf("Close browser: %v", err)
		}
	}()

	documentSource := `<!doctype html>
		<html><body style="margin:0">
		<button id="counter" style="display:block;width:120px;height:40px">0</button>
		<span id="transient">transient wrapper</span>
		<script>
			const counter = document.getElementById("counter");
			if (counter === null || counter !== document.getElementById("counter")) {
				throw new Error("node wrapper identity is not stable");
			}
			document.getElementById("transient");
			counter.addEventListener("click", () => {
				counter.textContent = String(Number(counter.textContent) + 1);
			});
		</script>
		</body></html>`
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page, err := browserRuntime.LoadPage(
		ctx,
		"https://gossamer.test/wrappers",
		staticDocumentLoader{document: documentSource},
	)
	if err != nil {
		t.Fatalf("LoadPage: %v", err)
	}
	realm, ok := engine.LatestRealm()
	if !ok {
		t.Fatal("stock V8 engine has no live document realm")
	}
	profile, err := realm.Profile()
	if err != nil {
		t.Fatalf("Profile after navigation: %v", err)
	}
	if profile.WrappersCreated != 3 || profile.WrapperCacheHits == 0 || profile.EventListeners != 1 {
		t.Fatalf("wrapper profile after navigation = %#v", profile)
	}

	ledgerBeforeGC := browserRuntime.Ledger().Stats()
	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage: %v", err)
	}
	if ledgerAfterGC := browserRuntime.Ledger().Stats(); ledgerAfterGC != ledgerBeforeGC {
		t.Fatalf("connected wrapper collection boundary: before=%#v after=%#v", ledgerBeforeGC, ledgerAfterGC)
	}
	afterGC, err := realm.Profile()
	if err != nil {
		t.Fatalf("Profile after wrapper collection: %v", err)
	}
	if afterGC.WrappersCollected == 0 || afterGC.LiveWrappers != 2 {
		t.Fatalf("weak wrapper cache after GC = %#v", afterGC)
	}

	document := page.Document()
	counterID, found := document.ElementByID("counter")
	if !found {
		t.Fatal("parsed counter element is missing")
	}
	counter, ok := document.Resolve(counterID)
	if !ok {
		t.Fatal("counter stable identity does not resolve")
	}
	counterBox := findV8BoxForNode(page.Frame().Root, counter)
	if counterBox == nil {
		t.Fatal("rendered frame has no counter box")
	}
	x := counterBox.Bounds.X + 2
	y := counterBox.Bounds.Y + 2
	ledgerBeforeClick := browserRuntime.Ledger().Stats()
	if _, err := page.QueueClick(x, y, 0); err != nil {
		t.Fatalf("QueueClick: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("dispatch click: %v", err)
	}
	afterDispatch, err := realm.Profile()
	if err != nil {
		t.Fatalf("Profile after dispatch: %v", err)
	}
	if afterDispatch.EventsDispatched != 3 || afterDispatch.CallbacksCreated != 0 ||
		afterDispatch.CallbacksInvoked != 0 || afterDispatch.LiveCallbacks != 0 {
		t.Fatalf("synchronous event dispatch profile = %#v", afterDispatch)
	}
	if got, err := document.TextContent(counterID); err != nil || got != "1" {
		t.Fatalf("counter after synchronous listener = %q, %v; want 1", got, err)
	}
	if !page.Dirty() || v8FrameContainsText(page.Frame(), "1") {
		t.Fatal("DOM mutation did not wait behind the queued render boundary")
	}

	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("render wrapper mutation: %v", err)
	}
	if page.Dirty() || !v8FrameContainsText(page.Frame(), "1") || v8FrameContainsText(page.Frame(), "0") {
		t.Fatal("queued wrapper mutation did not reach paint")
	}
	ledgerAfterClick := browserRuntime.Ledger().Stats()
	if ledgerAfterClick.LiveObjects != ledgerBeforeClick.LiveObjects ||
		ledgerAfterClick.ObjectsCreated-ledgerBeforeClick.ObjectsCreated < 3 ||
		ledgerAfterClick.ObjectsDestroyed-ledgerBeforeClick.ObjectsDestroyed < 3 {
		t.Fatalf("click ownership boundary: before=%#v after=%#v", ledgerBeforeClick, ledgerAfterClick)
	}
}

func TestStockV8ElementTraversalReflectionInlineStyleAndLifetime(t *testing.T) {
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatalf("NewWithEngine: %v", err)
	}
	defer func() {
		if err := browserRuntime.Close(); err != nil {
			t.Errorf("Close browser: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page, err := browserRuntime.LoadPage(
		ctx,
		"https://gossamer.test/element",
		staticDocumentLoader{document: `<!doctype html><html><body style="margin:0"><main id="mount"></main></body></html>`},
	)
	if err != nil {
		t.Fatalf("LoadPage: %v", err)
	}
	realm, ok := engine.LatestRealm()
	if !ok {
		t.Fatal("stock V8 engine has no live document realm")
	}
	document := page.Document()
	baselineLiveNodes := document.Store().LiveLen()

	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/element.js",
		Source: `
			(() => {
				const mount = document.getElementById("mount");
				if (mount.nodeType !== 1 || mount.nodeName !== "MAIN" ||
					mount.tagName !== "MAIN" || mount.localName !== "main" ||
					!mount.isConnected || mount.parentElement.tagName !== "BODY") {
					throw new Error("connected element metadata is incomplete");
				}

				const row = document.createElement("section");
				if (row.isConnected || row.parentNode !== null || row.nodeValue !== null) {
					throw new Error("detached element metadata is incorrect");
				}
				row.id = "framework-row";
				row.className = "card selected";
				if (row.getAttribute("id") !== "framework-row" ||
					row.getAttribute("class") !== "card selected" ||
					!row.hasAttribute("class")) {
					throw new Error("reflected element attributes diverged");
				}

				if (row.style !== row.style) throw new Error("element.style is not SameObject");
				row.style.cssText = "display:block; color: rgb(1, 2, 3); width: 90px";
				row.style.backgroundColor = "#123456";
				row.style.setProperty("height", "32px", "important");
				if (row.style.length !== 5 || row.style.item(0) !== "display" ||
					row.style.getPropertyValue("background-color") !== "#123456" ||
					row.style.getPropertyPriority("height") !== "important" ||
					row.style.width !== "90px" || !row.getAttribute("style").includes("background-color: #123456")) {
					throw new Error("inline CSS declaration did not reflect through style attribute");
				}
				row.style.setProperty("ignored", "value", "not-a-priority");
				if (row.style.getPropertyValue("ignored") !== "") {
					throw new Error("invalid CSS priority was accepted");
				}

				const first = document.createElement("span");
				first.textContent = "alpha";
				const gap = document.createTextNode("gap");
				const last = document.createElement("strong");
				last.textContent = "omega";
				row.appendChild(first);
				row.appendChild(gap);
				row.appendChild(last);
				if (!row.hasChildNodes() || row.childNodes.length !== 3 ||
					row.children.length !== 2 || row.childElementCount !== 2 ||
					row.firstChild !== first || row.lastChild !== last ||
					first.nextSibling !== gap || gap.previousSibling !== first ||
					first.nextElementSibling !== last || last.previousElementSibling !== first ||
					row.firstElementChild !== first || row.lastElementChild !== last ||
					row.childNodes[1] !== gap || row.children[1] !== last ||
					!row.contains(row) || !row.contains(gap) || row.contains(null)) {
					throw new Error("node traversal or canonical wrapper identity failed");
				}
				gap.data = "middle";
				if (gap.nodeType !== 3 || gap.nodeName !== "#text" ||
					gap.nodeValue !== "middle" || gap.textContent !== "middle") {
					throw new Error("character data reflection failed");
				}
				const replacement = document.createTextNode("replacement");
				if (row.replaceChild(replacement, gap) !== gap || gap.parentNode !== null ||
					replacement.parentNode !== row || row.textContent !== "alphareplacementomega") {
					throw new Error("replaceChild traversal state is incorrect");
				}

				mount.appendChild(row);
				if (!row.isConnected || row.parentNode !== mount || mount.firstElementChild !== row) {
					throw new Error("publication did not update traversal metadata");
				}
				const disposable = document.createElement("aside");
				mount.appendChild(disposable);
				disposable.remove();
				if (disposable.parentNode !== null) throw new Error("Element.remove failed");
			})();
		`,
	})
	if err != nil {
		t.Fatalf("QueueScript: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("run element script: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("render element result: %v", err)
	}
	rowID, found := document.ElementByID("framework-row")
	if !found {
		t.Fatal("reflected id did not publish the element")
	}
	styleAttribute, found, err := document.GetAttribute(rowID, "style")
	if err != nil || !found || styleAttribute != "display: block; color: rgb(1, 2, 3); width: 90px; background-color: #123456; height: 32px !important;" {
		t.Fatalf("style attribute = %q, %t, %v", styleAttribute, found, err)
	}
	row, _ := document.Resolve(rowID)
	rowBox := findV8BoxForNode(page.Frame().Root, row)
	if rowBox == nil || rowBox.Bounds.Width != 90 || rowBox.Bounds.Height != 32 {
		t.Fatalf("inline style layout box = %#v", rowBox)
	}
	wantBackground := color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff}
	paintedBackground := false
	for _, command := range page.Frame().DisplayList.Commands {
		if command.Kind == render.FillRectCommand && command.Color == wantBackground && command.Rect == rowBox.Bounds {
			paintedBackground = true
			break
		}
	}
	if !paintedBackground {
		t.Fatal("element.style background did not reach the display list")
	}

	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/element-detach.js",
		Source: `
			(() => {
				const row = document.getElementById("framework-row");
				globalThis.__heldStyle = row.style;
				row.remove();
			})();
		`,
	})
	if err != nil {
		t.Fatalf("QueueScript detach: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("run detach script: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("render detach result: %v", err)
	}
	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage with held style: %v", err)
	}
	if document.Store().LiveLen() <= baselineLiveNodes {
		t.Fatal("held element.style did not preserve its detached native component")
	}
	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/element-style-after-gc.js",
		Source: `
			__heldStyle.width = "77px";
			if (__heldStyle.width !== "77px" ||
				!__heldStyle.cssText.includes("width: 77px")) {
				throw new Error("held style lost its element after V8 GC");
			}
			globalThis.__heldStyle = undefined;
		`,
	})
	if err != nil {
		t.Fatalf("QueueScript held style: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("run held style script: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("render held style mutation: %v", err)
	}
	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage after style release: %v", err)
	}
	if got := document.Store().LiveLen(); got != baselineLiveNodes {
		t.Fatalf("live nodes after style release = %d, want baseline %d", got, baselineLiveNodes)
	}
}

func TestStockV8GetComputedStyleIsFreshLiveAndReadOnly(t *testing.T) {
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatalf("NewWithEngine: %v", err)
	}
	defer func() {
		if err := browserRuntime.Close(); err != nil {
			t.Errorf("Close browser: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page, err := browserRuntime.LoadPage(
		ctx,
		"https://gossamer.test/computed-style",
		staticDocumentLoader{document: `<!doctype html><html><head><style>
			.parent { color: #123456; }
			.target { display: block; width: 25%; background-color: #010203; --accent: ready; }
		</style></head><body class="parent"><div id="target" class="target">text</div></body></html>`},
	)
	if err != nil {
		t.Fatalf("LoadPage: %v", err)
	}

	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/computed-style.js",
		Source: `
			(() => {
				const target = document.getElementById("target");
				const computed = getComputedStyle(target);
				if (!(computed instanceof CSSStyleDeclaration) ||
					!(target.style instanceof CSSStyleDeclaration) ||
					Object.getPrototypeOf(computed) !== CSSStyleDeclaration.prototype) {
					throw new Error("CSSStyleDeclaration prototype identity is incorrect");
				}
				if (computed === getComputedStyle(target)) {
					throw new Error("getComputedStyle did not return a fresh declaration");
				}
				if (computed.display !== "block" || computed.color !== "rgb(18, 52, 86)" ||
					computed.width !== "196px" || computed.getPropertyValue("--accent") !== "ready" ||
					computed["background-color"] !== "rgb(1, 2, 3)" || computed["--accent"] !== "ready" ||
					computed.getPropertyValue("not-a-property") !== "" ||
					computed.getPropertyPriority("color") !== "" || computed.cssText !== "") {
					throw new Error("computed cascade or serialization is incorrect");
				}
				if (computed.length < 30 || computed.item(computed.length) !== "") {
					throw new Error("computed property enumeration is incomplete");
				}
				if (computed[0] !== computed.item(0) || computed[computed.length] !== undefined ||
					!(0 in computed) || !Object.keys(computed).includes("0") ||
					!("background-color" in computed) || !("--accent" in computed)) {
					throw new Error("computed indexed or dashed-name access is incomplete");
				}
				let foundColor = false;
				let foundAccent = false;
				for (let index = 0; index < computed.length; index++) {
					foundColor ||= computed.item(index) === "color";
					foundAccent ||= computed.item(index) === "--accent";
				}
				if (!foundColor || !foundAccent) {
					throw new Error("computed property names are not exposed");
				}

				target.style["margin-top"] = "3px";
				if (target.style["margin-top"] !== "3px" || computed["margin-top"] !== "3px") {
					throw new Error("dashed inline style access is not live");
				}
				target.style.color = "#abcdef";
				target.style.setProperty("--accent", "updated");
				if (computed.color !== "rgb(171, 205, 239)" ||
					computed.getPropertyValue("--accent") !== "updated") {
					throw new Error("retained computed declaration is not live");
				}

				const mustReject = (operation) => {
					let rejected = false;
					try {
						operation();
					} catch (error) {
						rejected = error instanceof TypeError && /read-only/.test(error.message);
					}
					if (!rejected) throw new Error("computed style mutation was not clearly rejected");
				};
				mustReject(() => { computed.color = "red"; });
				mustReject(() => { computed[0] = "replacement"; });
				mustReject(() => { computed["background-color"] = "red"; });
				mustReject(() => { computed["--accent"] = "blocked"; });
				mustReject(() => { computed.cssText = "color: red"; });
				mustReject(() => computed.setProperty("color", "red"));
				mustReject(() => computed.removeProperty("color"));
				if (computed.color !== "rgb(171, 205, 239)" || computed[0] !== computed.item(0)) {
					throw new Error("rejected mutation changed computed style");
				}

				const pseudo = getComputedStyle(target, "::before");
				if (!(pseudo instanceof CSSStyleDeclaration) || pseudo.length !== 0 ||
					pseudo.getPropertyValue("color") !== "") {
					throw new Error("unsupported pseudo computed style is not an empty live declaration");
				}
				if (getComputedStyle(target, null).color !== "rgb(171, 205, 239)" ||
					getComputedStyle(target, "").color !== "rgb(171, 205, 239)" ||
					getComputedStyle(target, "::gossamer-unsupported").length !== 0 ||
					getComputedStyle(target, ":before").length !== 0) {
					throw new Error("valid computed-style pseudo boundary is incorrect");
				}
				for (const invalidPseudo of [
					"before", ":hover", "::", "::1before", " ::before",
					"::before:hover", "::before trailing", "::part", "::part(icon)",
					"::slotted", "::slotted(*)",
				]) {
					let invalidPseudoRejected = false;
					try {
						getComputedStyle(target, invalidPseudo);
					} catch (error) {
						invalidPseudoRejected = error instanceof TypeError;
					}
					if (!invalidPseudoRejected) {
						throw new Error("getComputedStyle accepted invalid pseudo: " + invalidPseudo);
					}
				}
				let wrongTargetRejected = false;
				try {
					getComputedStyle(document);
				} catch (error) {
					wrongTargetRejected = error instanceof TypeError;
				}
				if (!wrongTargetRejected) throw new Error("getComputedStyle accepted a non-Element");
				globalThis.__heldComputedStyle = computed;
			})();
		`,
	})
	if err != nil {
		t.Fatalf("QueueScript: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("run computed-style script: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("render computed-style mutations: %v", err)
	}
	realm, ok := engine.LatestRealm()
	if !ok {
		t.Fatal("computed-style realm is unavailable")
	}
	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage with held computed style: %v", err)
	}

	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/computed-style-later-task.js",
		Source: `
			(() => {
				const target = document.getElementById("target");
				target.style["background-color"] = "#040506";
				if (__heldComputedStyle["background-color"] !== "rgb(4, 5, 6)" ||
					__heldComputedStyle[0] !== __heldComputedStyle.item(0)) {
					throw new Error("held computed declaration is not live across Page tasks");
				}
				target.remove();
			})();
		`,
	})
	if err != nil {
		t.Fatalf("QueueScript later computed-style task: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("run later computed-style task: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("render computed-style detach: %v", err)
	}
	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage with held detached computed style: %v", err)
	}

	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/computed-style-detached.js",
		Source: `
			if (__heldComputedStyle.length !== 0 || __heldComputedStyle.cssText !== "" ||
				__heldComputedStyle.getPropertyValue("background-color") !== "") {
				throw new Error("held computed declaration lost its detached-node anchor");
			}
			globalThis.__heldComputedStyle = undefined;
		`,
	})
	if err != nil {
		t.Fatalf("QueueScript detached computed-style task: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("run detached computed-style task: %v", err)
	}
	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage after computed-style release: %v", err)
	}
}

func TestStockV8LiveDOMFacadesReflectionIterationAndLifetime(t *testing.T) {
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatalf("NewWithEngine: %v", err)
	}
	defer func() {
		if err := browserRuntime.Close(); err != nil {
			t.Errorf("Close browser: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page, err := browserRuntime.LoadPage(
		ctx,
		"https://gossamer.test/live-facades",
		staticDocumentLoader{document: `<!doctype html><html><body><main id="mount"><span id="first" name="named-first" class="a b" data-user-id="7"></span>gap</main></body></html>`},
	)
	if err != nil {
		t.Fatalf("LoadPage: %v", err)
	}
	realm, ok := engine.LatestRealm()
	if !ok {
		t.Fatal("stock V8 engine has no live document realm")
	}
	documentStore := page.Document().Store()

	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/live-facades/assertions.js",
		Source: `
			(() => {
				const mount = document.getElementById("mount");
				const first = document.getElementById("first");
				const nodes = mount.childNodes;
				const elements = mount.children;
				if (nodes !== mount.childNodes || elements !== mount.children ||
					!(nodes instanceof NodeList) || Object.getPrototypeOf(nodes) !== NodeList.prototype ||
					!(elements instanceof HTMLCollection) ||
					Object.getPrototypeOf(elements) !== HTMLCollection.prototype) {
					throw new Error("live child facades are not SameObject interface instances");
				}
				if (nodes.length !== 2 || elements.length !== 1 ||
					nodes.item(0) !== first || nodes[0] !== first || nodes.item(99) !== null ||
					elements.item(0) !== first || elements.namedItem("first") !== first ||
					elements.namedItem("named-first") !== first || elements.first !== first ||
					elements.namedItem("missing") !== null) {
					throw new Error("indexed or named collection lookup is incorrect");
				}
				if (Object.keys(nodes).join(",") !== "0,1" ||
					[...nodes].map(node => node.nodeName).join(",") !== "SPAN,#text" ||
					[...nodes.keys()].join(",") !== "0,1" ||
					[...nodes.entries()].map(([index, node]) => index + ":" + node.nodeName).join(",") !== "0:SPAN,1:#text") {
					throw new Error("NodeList indexed properties or iterators are incorrect");
				}
				const visited = [];
				nodes.forEach((node, index, owner) => visited.push(index + ":" + node.nodeName + ":" + (owner === nodes)));
				if (visited.join(",") !== "0:SPAN:true,1:#text:true") {
					throw new Error("NodeList forEach is incorrect");
				}

				const liveIterator = nodes.values();
				if (liveIterator[Symbol.iterator]() !== liveIterator || liveIterator.next().value !== first) {
					throw new Error("DOM iterator identity is incorrect");
				}
				const last = document.createElement("strong");
				last.id = "last";
				last.setAttribute("name", "named-last");
				mount.appendChild(last);
				if (nodes.length !== 3 || elements.length !== 2 || nodes[2] !== last ||
					elements[1] !== last || elements.last !== last || elements["named-last"] !== last ||
					liveIterator.next().value.nodeType !== 3 || liveIterator.next().value !== last ||
					!liveIterator.next().done) {
					throw new Error("child collections or an existing iterator did not stay live");
				}
				last.remove();
				if (nodes.length !== 2 || elements.length !== 1 || nodes[2] !== undefined || elements.last !== undefined) {
					throw new Error("live child collections did not observe removal");
				}

				if (first.classList !== first.classList || !(first.classList instanceof DOMTokenList) ||
					first.classList.length !== 2 || first.classList[0] !== "a" ||
					[...first.classList].join(" ") !== "a b" || first.classList.toString() !== "a b") {
					throw new Error("classList identity, indices, or iteration is incorrect");
				}
				const classList = first.classList;
				first.setAttribute("class", "x y");
				if (classList.value !== "x y" || !classList.contains("x")) {
					throw new Error("classList did not observe attribute mutation");
				}
				classList.add("z", "x");
				classList.remove("y");
				if (!classList.toggle("enabled") || classList.toggle("enabled") ||
					!classList.toggle("forced", true) || classList.toggle("forced", false) ||
					!classList.replace("x", "primary") || classList.replace("missing", "nope") ||
					first.getAttribute("class") !== "primary z") {
					throw new Error("classList mutation did not reflect to the class attribute");
				}
				classList.value = "raw  tokens";
				if (first.className !== "raw  tokens" || classList.value !== "raw  tokens" ||
					classList.toString() !== "raw  tokens" || [...classList].join(",") !== "raw,tokens") {
					throw new Error("classList.value did not preserve reflected attribute semantics");
				}

				const initialNames = first.getAttributeNames();
				if (initialNames.join(",") !== "id,name,class,data-user-id") {
					throw new Error("attribute names lost insertion order: " + initialNames.join(","));
				}
				const dataset = first.dataset;
				if (dataset !== first.dataset || !(dataset instanceof DOMStringMap) || dataset.userId !== "7") {
					throw new Error("dataset identity or initial reflection is incorrect");
				}
				dataset.buildNumber = 9;
				first.setAttribute("data-live-state", "ready");
				if (first.getAttribute("data-build-number") !== "9" || dataset.liveState !== "ready" ||
					Object.keys(dataset).join(",") !== "userId,buildNumber,liveState") {
					throw new Error("dataset writes, liveness, or enumeration are incorrect");
				}
				delete dataset.userId;
				if (first.hasAttribute("data-user-id") || dataset.userId !== undefined ||
					first.getAttributeNames().join(",") !== "id,name,class,data-build-number,data-live-state") {
					throw new Error("dataset deletion or attribute-name enumeration is incorrect");
				}
			})();
		`,
	})
	if err != nil {
		t.Fatalf("QueueScript assertions: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("run facade assertions: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("render facade assertions: %v", err)
	}

	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage before facade lifetime baseline: %v", err)
	}
	baselineLiveNodes := documentStore.LiveLen()
	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/live-facades/hold-collection.js",
		Source: `
			globalThis.__heldCollection = (() => {
				const parent = document.createElement("section");
				parent.appendChild(document.createElement("i"));
				return parent.childNodes;
			})();
		`,
	})
	if err != nil {
		t.Fatalf("QueueScript held collection: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("run held collection script: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("render held collection result: %v", err)
	}
	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage with held collection: %v", err)
	}
	if got := documentStore.LiveLen(); got != baselineLiveNodes+2 {
		t.Fatalf("held NodeList native subtree live nodes = %d, want %d", got, baselineLiveNodes+2)
	}
	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/live-facades/release-collection.js",
		Source: `
			(() => {
				if (!(__heldCollection instanceof NodeList) || __heldCollection.length !== 1 ||
					__heldCollection[0].tagName !== "I") {
					throw new Error("held NodeList lost its detached native subtree after GC");
				}
				globalThis.__heldCollection = undefined;
			})();
		`,
	})
	if err != nil {
		t.Fatalf("QueueScript release collection: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("run release collection script: %v", err)
	}
	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage after collection release: %v", err)
	}
	if got := documentStore.LiveLen(); got != baselineLiveNodes {
		t.Fatalf("live nodes after NodeList release = %d, want baseline %d", got, baselineLiveNodes)
	}

	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/live-facades/hold-token-list.js",
		Source: `
			globalThis.__heldTokenList = (() => {
				const element = document.createElement("aside");
				element.className = "held alive";
				return element.classList;
			})();
		`,
	})
	if err != nil {
		t.Fatalf("QueueScript held token list: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("run held token list script: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("render held token list result: %v", err)
	}
	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage with held token list: %v", err)
	}
	if got := documentStore.LiveLen(); got != baselineLiveNodes+1 {
		t.Fatalf("held DOMTokenList native element live nodes = %d, want %d", got, baselineLiveNodes+1)
	}
	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/live-facades/release-token-list.js",
		Source: `
			(() => {
				if (!(__heldTokenList instanceof DOMTokenList) || __heldTokenList.value !== "held alive") {
					throw new Error("held classList lost its detached element after GC");
				}
				__heldTokenList.add("after-gc");
				if (__heldTokenList.value !== "held alive after-gc") {
					throw new Error("held classList could not mutate after GC");
				}
				globalThis.__heldTokenList = undefined;
			})();
		`,
	})
	if err != nil {
		t.Fatalf("QueueScript release token list: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("run release token list script: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("render token list release: %v", err)
	}
	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage after token list release: %v", err)
	}
	if got := documentStore.LiveLen(); got != baselineLiveNodes {
		t.Fatalf("live nodes after DOMTokenList release = %d, want baseline %d", got, baselineLiveNodes)
	}
	profile, err := realm.Profile()
	if err != nil {
		t.Fatalf("Profile after facade GC: %v", err)
	}
	if profile.LiveWrappers != 1 {
		t.Fatalf("canonical document should be the sole live wrapper after facade GC: %#v", profile)
	}

	if err := page.Close(); err != nil {
		t.Fatalf("Close facade page: %v", err)
	}
	if stats := browserRuntime.Ledger().Stats(); stats.LiveObjects != 0 || stats.PersistentObjects != 0 {
		t.Fatalf("facade teardown ownership = %#v", stats)
	}
}

func TestStockV8SelectorTraversalStaticNodeListAndLifetime(t *testing.T) {
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatalf("NewWithEngine: %v", err)
	}
	defer func() {
		if err := browserRuntime.Close(); err != nil {
			t.Errorf("Close browser: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page, err := browserRuntime.LoadPage(
		ctx,
		"https://gossamer.test/selectors",
		staticDocumentLoader{document: `<!doctype html><html><body><main id="scope"><section id="first" class="card"><span class="leaf"></span></section><section id="second" class="card"></section></main><a id="visited-self" href="/selectors#fragment">visited</a><a id="unvisited-link" href="/future">unvisited</a></body></html>`},
	)
	if err != nil {
		t.Fatalf("LoadPage: %v", err)
	}
	realm, ok := engine.LatestRealm()
	if !ok {
		t.Fatal("stock V8 engine has no live document realm")
	}
	store := page.Document().Store()

	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/selectors/assertions.js",
		Source: `
			(() => {
				const scope = document.querySelector("#scope");
				const first = scope.querySelector("section.card:first-child");
				const leaf = document.querySelector("main > section .leaf");
				if (scope !== document.getElementById("scope") || first.id !== "first" ||
					leaf.closest("section") !== first || leaf.closest("#missing") !== null ||
					!leaf.matches("span.leaf") || leaf.matches("section")) {
					throw new Error("selector traversal or canonical identity is incorrect");
				}
				const cards = scope.querySelectorAll("section.card");
				if (!(cards instanceof NodeList) || cards.length !== 2 ||
					cards[0] !== first || cards[1].id !== "second" ||
					[...cards].map(node => node.id).join(",") !== "first,second") {
					throw new Error("querySelectorAll did not return an ordered static NodeList");
				}
				const third = document.createElement("section");
				third.id = "third";
				third.className = "card";
				scope.appendChild(third);
				if (cards.length !== 2 || scope.querySelectorAll("section.card").length !== 3 ||
					scope.querySelector("#third") !== third) {
					throw new Error("querySelectorAll result was live instead of static");
				}
				let invalidRejected = false;
				try { document.querySelectorAll(".card, :unsupported()"); }
				catch (error) { invalidRejected = String(error).includes("invalid selector"); }
				if (!invalidRejected) throw new Error("invalid selector list was accepted");
				const visitedSelf = document.getElementById("visited-self");
				const unvisitedLink = document.getElementById("unvisited-link");
				if (!visitedSelf.matches(":visited") || visitedSelf.matches(":link") ||
					unvisitedLink.matches(":visited") || !unvisitedLink.matches(":link") ||
					document.querySelector(":visited") !== visitedSelf) {
					throw new Error("session-history link selectors are incorrect");
				}
			})();
		`,
	})
	if err != nil {
		t.Fatalf("QueueScript selector assertions: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("run selector assertions: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("render selector assertions: %v", err)
	}
	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage before selector baseline: %v", err)
	}
	baselineLiveNodes := store.LiveLen()

	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/selectors/hold-static-result.js",
		Source: `
			globalThis.__heldQueryResult = (() => {
				const root = document.createElement("div");
				const match = document.createElement("span");
				match.className = "retained-match";
				root.appendChild(match);
				return root.querySelectorAll(".retained-match");
			})();
		`,
	})
	if err != nil {
		t.Fatalf("QueueScript held query result: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("run held query result: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("render held query result: %v", err)
	}
	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage with held query result: %v", err)
	}
	if got := store.LiveLen(); got != baselineLiveNodes+2 {
		t.Fatalf("static NodeList retained native component = %d nodes, want %d", got, baselineLiveNodes+2)
	}
	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/selectors/release-static-result.js",
		Source: `
			(() => {
				if (!(__heldQueryResult instanceof NodeList) || __heldQueryResult.length !== 1 ||
					__heldQueryResult[0].className !== "retained-match" ||
					__heldQueryResult[0].parentNode.tagName !== "DIV") {
					throw new Error("static NodeList lost its detached match after GC");
				}
				globalThis.__heldQueryResult = undefined;
			})();
		`,
	})
	if err != nil {
		t.Fatalf("QueueScript release query result: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("run query result release: %v", err)
	}
	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage after query result release: %v", err)
	}
	if got := store.LiveLen(); got != baselineLiveNodes {
		t.Fatalf("live nodes after static NodeList release = %d, want baseline %d", got, baselineLiveNodes)
	}
	profile, err := realm.Profile()
	if err != nil {
		t.Fatalf("Profile after selector GC: %v", err)
	}
	if profile.LiveWrappers != 1 {
		t.Fatalf("canonical document should be sole wrapper after selector GC: %#v", profile)
	}
}

func TestStockV8DocumentFragmentsMarkupMutationAndLifetime(t *testing.T) {
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatalf("NewWithEngine: %v", err)
	}
	defer func() {
		if err := browserRuntime.Close(); err != nil {
			t.Errorf("Close browser: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page, err := browserRuntime.LoadPage(
		ctx,
		"https://gossamer.test/markup",
		staticDocumentLoader{document: `<!doctype html><html><body><main id="mount"></main></body></html>`},
	)
	if err != nil {
		t.Fatalf("LoadPage: %v", err)
	}
	realm, ok := engine.LatestRealm()
	if !ok {
		t.Fatal("stock V8 engine has no live document realm")
	}
	store := page.Document().Store()

	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/markup/assertions.js",
		Source: `
			(() => {
				const mount = document.getElementById("mount");
				const fragment = document.createDocumentFragment();
				if (!(fragment instanceof DocumentFragment) || !(fragment instanceof Node) ||
					fragment.nodeType !== 11 || fragment.nodeName !== "#document-fragment" ||
					fragment.ownerDocument !== document || fragment.isConnected) {
					throw new Error("DocumentFragment interface metadata failed");
				}
				const first = document.createElement("p");
				first.id = "first";
				first.appendChild(document.createTextNode("one"));
				const second = document.createElement("p");
				second.id = "second";
				second.textContent = "two";
				fragment.appendChild(first);
				fragment.appendChild(second);
				if (fragment.querySelector("#first") !== first ||
					fragment.querySelectorAll("p").length !== 2) {
					throw new Error("DocumentFragment selector traversal failed");
				}
				const deep = fragment.cloneNode(true);
				const shallow = fragment.cloneNode(false);
				if (!(deep instanceof DocumentFragment) || deep === fragment ||
					deep.childNodes.length !== 2 || deep.firstChild === first ||
					deep.firstChild.id !== "first" || deep.textContent !== "onetwo" ||
					shallow.childNodes.length !== 0) {
					throw new Error("cloneNode did not allocate an independent subtree");
				}
				if (mount.appendChild(fragment) !== fragment || fragment.childNodes.length !== 0 ||
					mount.children.length !== 2 || mount.firstElementChild !== first ||
					!first.isConnected || first.ownerDocument !== document) {
					throw new Error("fragment insertion did not splice children");
				}

				mount.innerHTML = '<article data-kind="card">A &amp; <b>B</b></article><!--tail-->';
				if (mount.innerHTML !== '<article data-kind="card">A &amp; <b>B</b></article><!--tail-->' ||
					mount.firstElementChild.tagName !== "ARTICLE" ||
					mount.firstElementChild.textContent !== "A & B") {
					throw new Error("innerHTML parse/serialize round trip failed: " + mount.innerHTML);
				}

				mount.innerHTML = '<i id="target">core</i>';
				const target = document.getElementById("target");
				target.insertAdjacentHTML("beforebegin", '<span id="before">before</span>');
				target.insertAdjacentHTML("afterbegin", '<b id="inside-first">[</b>');
				target.insertAdjacentHTML("beforeend", '<b id="inside-last">]</b>');
				target.insertAdjacentHTML("afterend", '<span id="after">after</span>');
				if (mount.children.length !== 3 || mount.children[0].id !== "before" ||
					mount.children[1] !== target || mount.children[2].id !== "after" ||
					target.children[0].id !== "inside-first" ||
					target.children[1].id !== "inside-last" || target.textContent !== "[core]") {
					throw new Error("insertAdjacentHTML positions were not preserved");
				}

				globalThis.__markupScriptExecuted = false;
				mount.innerHTML = '<script>globalThis.__markupScriptExecuted = true;</script><p>inert</p>';
				if (__markupScriptExecuted || mount.lastElementChild.textContent !== "inert") {
					throw new Error("script inserted through innerHTML was not inert");
				}
			})();
		`,
	})
	if err != nil {
		t.Fatalf("QueueScript markup assertions: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("run markup assertions: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("render markup assertions: %v", err)
	}
	if !v8FrameContainsText(page.Frame(), "inert") {
		t.Fatal("innerHTML mutation did not reach paint")
	}
	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage before clone baseline: %v", err)
	}
	baselineLiveNodes := store.LiveLen()

	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/markup/hold-clone.js",
		Source: `
			globalThis.__heldMarkupClone = (() => {
				const source = document.createElement("section");
				source.innerHTML = "<strong>held</strong><em>subtree</em>";
				return source.cloneNode(true);
			})();
		`,
	})
	if err != nil {
		t.Fatalf("QueueScript held clone: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("run held clone: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("render held clone: %v", err)
	}
	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage with held clone: %v", err)
	}
	if got := store.LiveLen(); got != baselineLiveNodes+5 {
		t.Fatalf("held cloned subtree retained %d live nodes, want %d", got, baselineLiveNodes+5)
	}
	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/markup/release-clone.js",
		Source: `
			if (__heldMarkupClone.innerHTML !== "<strong>held</strong><em>subtree</em>") {
				throw new Error("held cloned subtree changed across GC");
			}
			globalThis.__heldMarkupClone = undefined;
		`,
	})
	if err != nil {
		t.Fatalf("QueueScript clone release: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("run clone release: %v", err)
	}
	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage after clone release: %v", err)
	}
	if got := store.LiveLen(); got != baselineLiveNodes {
		t.Fatalf("live nodes after cloned subtree release = %d, want %d", got, baselineLiveNodes)
	}
}

func TestStockV8FormStateFocusAndCancelableDefaultActions(t *testing.T) {
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatalf("NewWithEngine: %v", err)
	}
	defer func() {
		if err := browserRuntime.Close(); err != nil {
			t.Errorf("Close browser: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page, err := browserRuntime.LoadPage(
		ctx,
		"https://gossamer.test/forms",
		staticDocumentLoader{document: `<!doctype html><html><body><input id="text" value="seed"><input id="check" type="checkbox" checked><input id="cancel" type="checkbox"><input id="radio-a" type="radio" name="pick" checked><input id="radio-b" type="radio" name="pick"><input id="radio-cancel" type="radio" name="pick"></body></html>`},
	)
	if err != nil {
		t.Fatalf("LoadPage: %v", err)
	}

	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/forms/setup.js",
		Source: `
			(() => {
				const text = document.getElementById("text");
				const check = document.getElementById("check");
				const cancel = document.getElementById("cancel");
				const radioA = document.getElementById("radio-a");
				const radioB = document.getElementById("radio-b");
				const radioCancel = document.getElementById("radio-cancel");
				const valueDescriptor = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value");
				const checkedDescriptor = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "checked");
				if (!valueDescriptor || typeof valueDescriptor.get !== "function" ||
					typeof valueDescriptor.set !== "function" || !checkedDescriptor ||
					typeof checkedDescriptor.get !== "function" || typeof checkedDescriptor.set !== "function") {
					throw new Error("form-control property descriptors are incomplete");
				}
				if (document.activeElement !== document.body || text.value !== "seed") {
					throw new Error("initial form value or activeElement is wrong");
				}
				text.value = "user";
				text.defaultValue = "markup";
				if (text.value !== "user" || text.defaultValue !== "markup" ||
					text.getAttribute("value") !== "markup") {
					throw new Error("dirty value stopped following property semantics");
				}
				text.focus();
				if (document.activeElement !== text) throw new Error("focus() did not set activeElement");
				text.blur();
				if (document.activeElement !== document.body) throw new Error("blur() did not restore body focus");
				if (!check.checked || !check.defaultChecked) throw new Error("checked markup default was lost");
				check.checked = false;
				check.defaultChecked = false;
				if (check.checked || check.defaultChecked || check.hasAttribute("checked")) {
					throw new Error("checked and defaultChecked did not separate");
				}
				globalThis.__checkedDuringClick = false;
				globalThis.__checkedDuringCanceledClick = false;
				globalThis.__radioDuringClick = false;
				globalThis.__radioDuringCanceledClick = false;
				check.addEventListener("click", () => { __checkedDuringClick = check.checked; });
				cancel.addEventListener("click", event => {
					__checkedDuringCanceledClick = cancel.checked;
					event.preventDefault();
				});
				radioB.addEventListener("click", () => {
					__radioDuringClick = radioB.checked && !radioA.checked;
				});
				radioCancel.addEventListener("click", event => {
					__radioDuringCanceledClick = radioCancel.checked && !radioB.checked;
					event.preventDefault();
				});
			})();
		`,
	})
	if err != nil {
		t.Fatalf("QueueScript form setup: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("run form setup: %v", err)
	}
	if page.Dirty() {
		if err := page.Realm.RunOne(context.Background()); err != nil {
			t.Fatalf("render form setup: %v", err)
		}
	}

	textID, _ := page.Document().ElementByID("text")
	checkID, _ := page.Document().ElementByID("check")
	cancelID, _ := page.Document().ElementByID("cancel")
	radioBID, _ := page.Document().ElementByID("radio-b")
	radioCancelID, _ := page.Document().ElementByID("radio-cancel")
	generation := page.DocumentGeneration()
	for _, event := range []browser.InputEvent{
		{Type: browser.InputClick, Target: browser.NodeHandle{Document: generation, Node: checkID}},
		{Type: browser.InputClick, Target: browser.NodeHandle{Document: generation, Node: cancelID}},
		{Type: browser.InputClick, Target: browser.NodeHandle{Document: generation, Node: radioBID}},
		{Type: browser.InputClick, Target: browser.NodeHandle{Document: generation, Node: radioCancelID}},
		{Type: browser.InputInput, Target: browser.NodeHandle{Document: generation, Node: textID}, Data: "X", InputType: "insertText"},
	} {
		if _, err := page.QueueInputEvent(event); err != nil {
			t.Fatalf("QueueInputEvent(%s): %v", event.Type, err)
		}
	}
	for range 5 {
		if err := page.Realm.RunOne(context.Background()); err != nil {
			t.Fatalf("run form input: %v", err)
		}
	}

	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/forms/assert.js",
		Source: `
			if (!__checkedDuringClick || !document.getElementById("check").checked) {
				throw new Error("checkbox activation was not visible during click");
			}
			if (!__checkedDuringCanceledClick || document.getElementById("cancel").checked) {
				throw new Error("preventDefault did not roll checkbox activation back");
			}
			if (!__radioDuringClick || !__radioDuringCanceledClick ||
				document.getElementById("radio-a").checked ||
				!document.getElementById("radio-b").checked ||
				document.getElementById("radio-cancel").checked) {
				throw new Error("radio activation or cancel rollback failed");
			}
			if (document.getElementById("text").value !== "userX") {
				throw new Error("input event did not update current value");
			}
			if (document.activeElement !== document.getElementById("radio-b")) {
				throw new Error("uncanceled click did not preserve focus");
			}
		`,
	})
	if err != nil {
		t.Fatalf("QueueScript form assertions: %v", err)
	}
	for page.Realm.Tasks.Len() != 0 {
		if err := page.Realm.RunOne(context.Background()); err != nil {
			t.Fatalf("drain form assertions: %v", err)
		}
	}
}

func TestStockV8RunsRealReactRenderUpdateEventAndUnmount(t *testing.T) {
	reactBundle, err := os.ReadFile("testdata/react-19.2.7.production.js")
	if err != nil {
		t.Fatalf("read React fixture: %v", err)
	}
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatalf("NewWithEngine: %v", err)
	}
	defer func() {
		if err := browserRuntime.Close(); err != nil {
			t.Errorf("Close browser: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page, err := browserRuntime.LoadPage(
		ctx,
		"https://gossamer.test/react",
		staticDocumentLoader{document: `<!doctype html><html><body><main id="root"></main></body></html>`},
	)
	if err != nil {
		t.Fatalf("LoadPage: %v", err)
	}
	realm, ok := engine.LatestRealm()
	if !ok {
		t.Fatal("stock V8 engine has no live document realm")
	}
	initialLiveNodes := page.Document().Store().LiveLen()
	drainRender := func(label string) {
		t.Helper()
		for attempt := 0; attempt < 16 && page.Dirty(); attempt++ {
			if err := page.Realm.RunOne(context.Background()); err != nil {
				t.Fatalf("%s: %v", label, err)
			}
		}
		if page.Dirty() {
			t.Fatalf("%s: page remained dirty after draining queued work", label)
		}
	}
	runUntil := func(label string, condition func() bool) {
		t.Helper()
		for attempt := 0; attempt < 32; attempt++ {
			if condition() {
				return
			}
			if page.Realm.Tasks.Len() == 0 {
				t.Fatalf("%s: task queue emptied before the condition was met", label)
			}
			if err := page.Realm.RunOne(context.Background()); err != nil {
				t.Fatalf("%s: %v", label, err)
			}
		}
		t.Fatalf("%s: condition was not met after draining queued work", label)
	}

	_, err = page.QueueScript(browser.ScriptSource{
		URL:    "https://gossamer.test/vendor/react-19.2.7.production.js",
		Source: string(reactBundle),
	})
	if err != nil {
		t.Fatalf("QueueScript React bundle: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("evaluate React bundle: %v", err)
	}
	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage after React bundle load: %v", err)
	}
	frameworkBaselineLiveNodes := page.Document().Store().LiveLen()
	if frameworkBaselineLiveNodes > initialLiveNodes+1 {
		t.Fatalf("React bundle retained %d probe nodes, want at most one", frameworkBaselineLiveNodes-initialLiveNodes)
	}

	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/react/app.js",
		Source: `
			(() => {
				const h = React.createElement;
				function Counter() {
					const [count, setCount] = React.useState(0);
					const [name, setName] = React.useState("A");
					return h("section", { id: "counter", className: "ready", "data-count": count },
						h("h1", null, "Count ", count),
						h("button", {
							id: "increment",
							style: { display: "block" },
							onClick: () => ReactDOM.flushSync(() => setCount(value => value + 1))
						}, "Increment"),
						h("input", {
							id: "name",
							value: name,
							style: { display: "block" },
							onInput: event => ReactDOM.flushSync(() => setName(event.currentTarget.value))
						}),
						h("p", { id: "name-value" }, name)
					);
				}
				globalThis.__reactCommitError = "";
				const root = ReactDOM.createRoot(document.getElementById("root"), {
					onUncaughtError: error => { globalThis.__reactCommitError = String(error); }
				});
				ReactDOM.flushSync(() => root.render(h(Counter)));
				globalThis.__reactRoot = root;
				if (__reactCommitError) throw new Error("React commit failed: " + __reactCommitError);
				if (document.querySelector("#counter").dataset.count !== "0" ||
					document.querySelector("#counter h1").textContent !== "Count 0" ||
					document.getElementById("name").value !== "A") {
					throw new Error("initial React commit did not reach the Go DOM");
				}
			})();
		`,
	})
	if err != nil {
		t.Fatalf("QueueScript React app: %v", err)
	}
	runUntil("run initial React render", func() bool {
		_, found := page.Document().ElementByID("counter")
		return found
	})
	drainRender("render initial React commit")
	if !v8FrameContainsText(page.Frame(), "Count") || !v8FrameContainsText(page.Frame(), "Increment") {
		t.Fatal("initial React commit did not reach paint")
	}

	buttonID, found := page.Document().ElementByID("increment")
	if !found {
		t.Fatal("React button was not connected")
	}
	button, _ := page.Document().Resolve(buttonID)
	buttonBox := findV8BoxForNode(page.Frame().Root, button)
	if buttonBox == nil && len(button.Children) != 0 {
		buttonBox = findV8BoxForNode(page.Frame().Root, button.Children[0])
	}
	if buttonBox == nil {
		t.Fatal("React button has no rendered box")
	}
	if _, err := page.QueueClick(buttonBox.Bounds.X+2, buttonBox.Bounds.Y+2, 0); err != nil {
		t.Fatalf("QueueClick React button: %v", err)
	}
	runUntil("dispatch React delegated click", func() bool {
		counterID, found := page.Document().ElementByID("counter")
		if !found {
			return false
		}
		value, found, err := page.Document().GetAttribute(counterID, "data-count")
		return err == nil && found && value == "1"
	})
	drainRender("render React state update")
	if !v8FrameContainsText(page.Frame(), "Count") || !v8FrameContainsText(page.Frame(), "1") {
		t.Fatal("React state update did not reach paint")
	}
	counterID, found := page.Document().ElementByID("counter")
	if !found {
		t.Fatal("React counter disappeared after its state update")
	}
	if value, found, err := page.Document().GetAttribute(counterID, "data-count"); err != nil || !found || value != "1" {
		t.Fatalf("React data-count after click = %q, %t, %v", value, found, err)
	}
	nameID, found := page.Document().ElementByID("name")
	if !found {
		t.Fatal("React controlled input disappeared")
	}
	if _, err := page.QueueInputEvent(browser.InputEvent{
		Type:      browser.InputInput,
		Target:    browser.NodeHandle{Document: page.DocumentGeneration(), Node: nameID},
		Data:      "B",
		InputType: "insertText",
	}); err != nil {
		t.Fatalf("QueueInputEvent React controlled input: %v", err)
	}
	runUntil("dispatch React controlled input", func() bool {
		valueID, found := page.Document().ElementByID("name-value")
		if !found {
			return false
		}
		value, readErr := page.Document().TextContent(valueID)
		return readErr == nil && value == "AB"
	})
	valueID, valueFound := page.Document().ElementByID("name-value")
	renderedValue := ""
	if valueFound {
		renderedValue, _ = page.Document().TextContent(valueID)
	}
	formValue, formErr := page.Document().FormValue(nameID)
	if renderedValue != "AB" {
		t.Fatalf("React controlled input state = %q, native value = %q (%v)", renderedValue, formValue, formErr)
	}
	drainRender("render React controlled input")
	if !v8FrameContainsText(page.Frame(), "AB") {
		t.Fatal("React controlled input update did not reach paint")
	}

	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/react/unmount.js",
		Source: `
			ReactDOM.flushSync(() => __reactRoot.unmount());
			if (document.getElementById("root").childNodes.length !== 0) {
				throw new Error("React unmount left native children connected");
			}
			globalThis.__reactRoot = undefined;
		`,
	})
	if err != nil {
		t.Fatalf("QueueScript React unmount: %v", err)
	}
	runUntil("run React unmount", func() bool {
		_, found := page.Document().ElementByID("counter")
		return !found
	})
	drainRender("render React unmount")
	if v8FrameContainsText(page.Frame(), "Count") || v8FrameContainsText(page.Frame(), "Increment") {
		t.Fatal("React unmount did not clear paint output")
	}
	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage after React unmount: %v", err)
	}
	if got := page.Document().Store().LiveLen(); got > frameworkBaselineLiveNodes+9 {
		t.Fatalf("React unmount retained nodes outside its detached component: got %d, framework baseline %d", got, frameworkBaselineLiveNodes)
	}
	if profile, err := realm.Profile(); err != nil {
		t.Fatalf("Profile after React unmount: %v", err)
	} else if profile.LiveWrappers > 4 || profile.LiveCallbacks != 0 || profile.EventListeners == 0 {
		t.Fatalf("React unmount retained state outside its delegated root surface: %#v", profile)
	}
	if err := page.Close(); err != nil {
		t.Fatalf("Close React page: %v", err)
	}
	if _, live := engine.LatestRealm(); live {
		t.Fatal("React realm remained registered after page teardown")
	}
	if stats := browserRuntime.Ledger().Stats(); stats.LiveObjects != 0 || stats.PersistentObjects != 0 {
		t.Fatalf("React realm teardown ownership = %#v", stats)
	}
}

func TestStockV8DOMPrototypesDocumentNamespaceIdentityAndTeardown(t *testing.T) {
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatalf("NewWithEngine: %v", err)
	}
	defer func() {
		if err := browserRuntime.Close(); err != nil {
			t.Errorf("Close browser: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page, err := browserRuntime.LoadPage(
		ctx,
		"https://gossamer.test/interfaces/index.html?mode=prototype",
		staticDocumentLoader{document: `<!doctype html><html><head><title>Interfaces</title></head><body><main id="mount"></main></body></html>`},
	)
	if err != nil {
		t.Fatalf("LoadPage: %v", err)
	}
	realm, ok := engine.LatestRealm()
	if !ok {
		t.Fatal("stock V8 engine has no live document realm")
	}
	documentStore := page.Document().Store()
	baselineLiveNodes := documentStore.LiveLen()

	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/interfaces/prototypes.js",
		Source: `
			(() => {
				if (Object.getPrototypeOf(Node.prototype) !== EventTarget.prototype ||
					Object.getPrototypeOf(Element.prototype) !== Node.prototype ||
					Object.getPrototypeOf(HTMLElement.prototype) !== Element.prototype ||
					Object.getPrototypeOf(Text.prototype) !== Node.prototype ||
					Object.getPrototypeOf(Document.prototype) !== Node.prototype) {
					throw new Error("DOM prototype inheritance is incorrect");
				}
				if (!Object.prototype.hasOwnProperty.call(EventTarget.prototype, "addEventListener") ||
					!Object.prototype.hasOwnProperty.call(Node.prototype, "appendChild") ||
					!Object.prototype.hasOwnProperty.call(Element.prototype, "getAttribute") ||
					!Object.prototype.hasOwnProperty.call(Text.prototype, "data") ||
					!Object.prototype.hasOwnProperty.call(Document.prototype, "createElementNS")) {
					throw new Error("DOM prototype surface is incomplete");
				}

				if (!(document instanceof Document) || !(document instanceof Node) ||
					!(document instanceof EventTarget) || document instanceof Element ||
					Object.getPrototypeOf(document) !== Document.prototype ||
					document.nodeType !== 9 || document.nodeName !== "#document") {
					throw new Error("canonical document wrapper has the wrong interface");
				}
				if (document.ownerDocument !== null || document.defaultView !== window ||
					document.baseURI !== "https://gossamer.test/interfaces/index.html?mode=prototype") {
					throw new Error("document metadata is incorrect");
				}
				const html = document.documentElement;
				if (html !== document.firstElementChild || html.ownerDocument !== document ||
					!(html instanceof HTMLElement) || !(html instanceof Element) ||
					!(html instanceof Node) || !(html instanceof EventTarget) ||
					Object.getPrototypeOf(html) !== HTMLElement.prototype ||
					html.namespaceURI !== "http://www.w3.org/1999/xhtml" ||
					html.prefix !== null || html.localName !== "html" || html.tagName !== "HTML") {
					throw new Error("HTML document element metadata or identity is incorrect");
				}
				if (document.head !== html.firstElementChild ||
					document.body !== html.lastElementChild ||
					document.head.ownerDocument !== document || document.body.ownerDocument !== document) {
					throw new Error("document head/body identity is incorrect");
				}

				const text = document.createTextNode("namespaced text");
				if (!(text instanceof Text) || !(text instanceof Node) ||
					!(text instanceof EventTarget) || text instanceof Element ||
					Object.getPrototypeOf(text) !== Text.prototype || text.ownerDocument !== document) {
					throw new Error("Text wrapper has the wrong interface");
				}
				const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg:rect");
				if (!(svg instanceof Element) || !(svg instanceof Node) ||
					!(svg instanceof EventTarget) || svg instanceof HTMLElement ||
					Object.getPrototypeOf(svg) !== Element.prototype ||
					svg.namespaceURI !== "http://www.w3.org/2000/svg" ||
					svg.prefix !== "svg" || svg.localName !== "rect" ||
					svg.nodeName !== "svg:rect" || svg.tagName !== "svg:rect") {
					throw new Error("namespaced Element wrapper metadata is incorrect");
				}
				const htmlByNamespace = document.createElementNS(
					"http://www.w3.org/1999/xhtml", "x-panel",
				);
				if (!(htmlByNamespace instanceof HTMLElement) || htmlByNamespace.localName !== "x-panel") {
					throw new Error("HTML namespace did not select HTMLElement");
				}
				svg.id = "namespaced-node";
				svg.appendChild(text);
				document.body.appendChild(svg);
				if (document.getElementById("namespaced-node") !== svg ||
					svg.firstChild !== text || text.ownerDocument !== document) {
					throw new Error("one NodeHandle produced multiple wrappers");
				}
				globalThis.__heldNamespacedElement = document.getElementById("namespaced-node");
				svg.remove();
				if (__heldNamespacedElement !== svg || svg.isConnected) {
					throw new Error("detachment changed wrapper identity");
				}
			})();
		`,
	})
	if err != nil {
		t.Fatalf("QueueScript prototypes: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("run prototype script: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("render prototype result: %v", err)
	}
	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage with detached namespaced wrapper: %v", err)
	}
	if got := documentStore.LiveLen(); got <= baselineLiveNodes {
		t.Fatalf("held detached namespaced component live nodes = %d, baseline %d", got, baselineLiveNodes)
	}

	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/interfaces/release.js",
		Source: `
			if (!(__heldNamespacedElement instanceof Element) ||
				__heldNamespacedElement.ownerDocument !== document ||
				__heldNamespacedElement.firstChild.ownerDocument !== document) {
				throw new Error("forced GC changed detached wrapper identity");
			}
			document.body.appendChild(__heldNamespacedElement);
			if (document.getElementById("namespaced-node") !== __heldNamespacedElement) {
				throw new Error("reattachment changed canonical wrapper identity");
			}
			__heldNamespacedElement.remove();
			globalThis.__heldNamespacedElement = undefined;
		`,
	})
	if err != nil {
		t.Fatalf("QueueScript release: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("run release script: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("render release result: %v", err)
	}
	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage after detached release: %v", err)
	}
	if got := documentStore.LiveLen(); got != baselineLiveNodes {
		t.Fatalf("live nodes after detached release = %d, want baseline %d", got, baselineLiveNodes)
	}
	profile, err := realm.Profile()
	if err != nil {
		t.Fatalf("Profile after prototype GC: %v", err)
	}
	if profile.LiveWrappers != 1 {
		t.Fatalf("canonical document should be the sole live wrapper after GC: %#v", profile)
	}

	beforeClose := engine.Profile()
	if err := page.Close(); err != nil {
		t.Fatalf("Close prototype page: %v", err)
	}
	if err := realm.CollectGarbage(page); err != ErrRealmClosed {
		t.Fatalf("CollectGarbage after realm teardown = %v, want %v", err, ErrRealmClosed)
	}
	if _, live := engine.LatestRealm(); live {
		t.Fatal("document realm remained registered after teardown")
	}
	afterClose := engine.Profile()
	if afterClose.RealmsClosed != beforeClose.RealmsClosed+1 {
		t.Fatalf("realm teardown profile = %#v, before %#v", afterClose, beforeClose)
	}
	if stats := browserRuntime.Ledger().Stats(); stats.LiveObjects != 0 || stats.PersistentObjects != 0 {
		t.Fatalf("realm teardown ownership = %#v", stats)
	}
}

func TestStockV8EventPropagationFamiliesAndReactStyleRootDelegation(t *testing.T) {
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatalf("NewWithEngine: %v", err)
	}
	defer func() {
		if err := browserRuntime.Close(); err != nil {
			t.Errorf("Close browser: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page, err := browserRuntime.LoadPage(
		ctx,
		"https://gossamer.test/events",
		staticDocumentLoader{document: `<!doctype html>
			<html><body style="margin:0">
				<div id="root" style="display:block;width:160px;height:60px">
					<button id="leaf" style="display:block;width:120px;height:40px">delegate</button>
					<input id="other">
				</div>
				<script>
					globalThis.__eventLog = [];
					globalThis.__families = [];
					globalThis.__root = document.getElementById("root");
					globalThis.__leaf = document.getElementById("leaf");
					globalThis.__other = document.getElementById("other");

					const record = (label) => (event) => {
						if (event.target !== __leaf) throw new Error(label + " target identity");
						if (event.currentTarget === null) throw new Error(label + " missing currentTarget");
						__eventLog.push(label + ":" + event.eventPhase);
					};
					document.addEventListener("click", record("document-capture"), true);
					__root.addEventListener("click", record("root-capture"), {capture:true});
					__leaf.addEventListener("click", record("leaf-capture"), true);
					__leaf.addEventListener("click", record("leaf-bubble"));
					__root.addEventListener("click", (event) => {
						if (!(event instanceof MouseEvent) || !(event instanceof Event) ||
							event.currentTarget !== __root || event.target !== __leaf ||
							event.eventPhase !== Event.BUBBLING_PHASE || !event.isTrusted ||
							event.composedPath()[0] !== __leaf ||
							event.composedPath().indexOf(__root) < 0 ||
							event.composedPath().indexOf(document) < 0) {
							throw new Error("delegated root listener received the wrong native event");
						}
						globalThis.__lastClick = event;
						__eventLog.push("root-bubble:" + event.eventPhase);
					});
					document.addEventListener("click", record("document-bubble"));

					let customCaptureCount = 0;
					const onceCapture = () => { customCaptureCount++; };
					__root.addEventListener("gossamer-custom", onceCapture, {capture:true, once:true});
					__leaf.addEventListener("gossamer-custom", (event) => event.preventDefault());
					const custom = new Event("gossamer-custom", {bubbles:true, cancelable:true});
					if (__leaf.dispatchEvent(custom) !== false || !custom.defaultPrevented ||
						custom.target !== __leaf || custom.currentTarget !== null ||
						custom.eventPhase !== Event.NONE) {
						throw new Error("synthetic generic Event dispatch state is wrong");
					}
					__leaf.dispatchEvent(new Event("gossamer-custom", {bubbles:true}));
					if (customCaptureCount !== 1) throw new Error("once listener lifecycle failed");

					const removed = () => { throw new Error("removed listener ran"); };
					__leaf.addEventListener("removed", removed, true);
					__leaf.removeEventListener("removed", removed, true);
					__leaf.dispatchEvent(new Event("removed"));
					let passiveEvent = new Event("passive", {cancelable:true});
					__leaf.addEventListener("passive", (event) => event.preventDefault(), {passive:true});
					if (!__leaf.dispatchEvent(passiveEvent) || passiveEvent.defaultPrevented) {
						throw new Error("passive listener canceled its event");
					}
					const stopped = [];
					const stopFirst = (event) => { stopped.push("first"); event.stopPropagation(); };
					const stopSecond = () => stopped.push("second");
					const stopRoot = () => stopped.push("root");
					__leaf.addEventListener("stopped", stopFirst);
					__leaf.addEventListener("stopped", stopSecond);
					__root.addEventListener("stopped", stopRoot);
					__leaf.dispatchEvent(new Event("stopped", {bubbles:true}));
					if (stopped.join("|") !== "first|second") {
						throw new Error("stopPropagation listener order failed");
					}
					__leaf.removeEventListener("stopped", stopFirst);
					__leaf.removeEventListener("stopped", stopSecond);
					__root.removeEventListener("stopped", stopRoot);
					const immediate = [];
					const immediateFirst = (event) => { immediate.push("first"); event.stopImmediatePropagation(); };
					const immediateSecond = () => immediate.push("second");
					const immediateRoot = () => immediate.push("root");
					__leaf.addEventListener("immediate", immediateFirst);
					__leaf.addEventListener("immediate", immediateSecond);
					__root.addEventListener("immediate", immediateRoot);
					__leaf.dispatchEvent(new Event("immediate", {bubbles:true}));
					if (immediate.join("|") !== "first") {
						throw new Error("stopImmediatePropagation listener order failed");
					}
					__leaf.removeEventListener("immediate", immediateFirst);
					__leaf.removeEventListener("immediate", immediateSecond);
					__root.removeEventListener("immediate", immediateRoot);

					for (const type of ["pointerdown", "keydown", "input", "focus", "change"]) {
						__root.addEventListener(type, (event) => {
							if (event.target !== __leaf || event.currentTarget !== __root) {
								throw new Error(type + " delegation identity failed");
							}
							if (type === "pointerdown" && (!(event instanceof PointerEvent) ||
								event.pointerId !== 7 || event.pointerType !== "pen" || !event.isPrimary ||
								event.clientX !== 12 || event.clientY !== 14 || event.buttons !== 1)) {
								throw new Error("pointer event payload failed");
							}
							if (type === "keydown" && (!(event instanceof KeyboardEvent) ||
								event.key !== "Enter" || event.code !== "Enter" || !event.ctrlKey || event.repeat)) {
								throw new Error("keyboard event payload failed");
							}
							if (type === "input" && (!(event instanceof InputEvent) ||
								event.data !== "x" || event.inputType !== "insertText" || !event.isComposing)) {
								throw new Error("input event payload failed");
							}
							if (type === "focus" && (!(event instanceof FocusEvent) ||
								event.relatedTarget !== __other || event.bubbles ||
								event.eventPhase !== Event.CAPTURING_PHASE)) {
								throw new Error("focus event payload failed");
							}
							if (type === "change" && event.constructor !== Event) {
								throw new Error("change event interface failed");
							}
							__families.push(type);
						}, type === "focus");
					}
				</script>
			</body></html>`},
	)
	if err != nil {
		t.Fatalf("LoadPage: %v", err)
	}
	document := page.Document()
	leafID, found := document.ElementByID("leaf")
	if !found {
		t.Fatal("event target is missing")
	}
	otherID, found := document.ElementByID("other")
	if !found {
		t.Fatal("related event target is missing")
	}
	leaf, ok := document.Resolve(leafID)
	if !ok {
		t.Fatal("event target does not resolve")
	}
	leafBox := findV8BoxForNode(page.Frame().Root, leaf)
	if leafBox == nil {
		t.Fatal("event target has no rendered box")
	}
	if _, err := page.QueueClick(leafBox.Bounds.X+2, leafBox.Bounds.Y+2, 0); err != nil {
		t.Fatalf("QueueClick: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("dispatch delegated click: %v", err)
	}

	target := browser.NodeHandle{Document: page.DocumentGeneration(), Node: leafID}
	related := browser.NodeHandle{Document: page.DocumentGeneration(), Node: otherID}
	events := []browser.InputEvent{
		{Type: browser.InputPointerDown, Target: target, X: 12, Y: 14, Button: 0, Buttons: 1, PointerID: 7, PointerType: "pen", IsPrimary: true},
		{Type: browser.InputKeyDown, Target: target, Key: "Enter", Code: "Enter", CtrlKey: true},
		{Type: browser.InputInput, Target: target, Data: "x", InputType: "insertText", IsComposing: true},
		{Type: browser.InputFocus, Target: target, RelatedTarget: related},
		{Type: browser.InputChange, Target: target},
	}
	for _, event := range events {
		if _, err := page.QueueInputEvent(event); err != nil {
			t.Fatalf("QueueInputEvent(%s): %v", event.Type, err)
		}
	}
	for _, event := range events {
		if err := page.Realm.RunOne(context.Background()); err != nil {
			t.Fatalf("dispatch %s: %v", event.Type, err)
		}
	}

	if _, err := page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/events-assert.js",
		Source: `
			const expected = [
				"document-capture:1", "root-capture:1", "leaf-capture:2",
				"leaf-bubble:2", "root-bubble:3", "document-bubble:3"
			];
			if (__eventLog.join("|") !== expected.join("|")) {
				throw new Error("propagation order: " + __eventLog.join("|"));
			}
			if (__families.join("|") !== "pointerdown|keydown|input|focus|change") {
				throw new Error("event families: " + __families.join("|"));
			}
			if (__lastClick.target !== __leaf || __lastClick.currentTarget !== null ||
				__lastClick.eventPhase !== Event.NONE) {
				throw new Error("post-dispatch Event state was not cleared");
			}
		`,
	}); err != nil {
		t.Fatalf("QueueScript assertions: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("event assertions: %v", err)
	}

	realm, ok := engine.LatestRealm()
	if !ok {
		t.Fatal("stock V8 engine has no live event Realm")
	}
	profile, err := realm.Profile()
	if err != nil {
		t.Fatalf("Profile after events: %v", err)
	}
	if profile.EventsDispatched != 14 || profile.CallbacksCreated != 0 ||
		profile.CallbacksInvoked != 0 || profile.LiveCallbacks != 0 ||
		profile.EventListeners != 13 {
		t.Fatalf("event dispatch profile = %#v", profile)
	}
	if err := page.Close(); err != nil {
		t.Fatalf("Close event page: %v", err)
	}
	if stats := browserRuntime.Ledger().Stats(); stats.LiveObjects != 0 || stats.PersistentObjects != 0 {
		t.Fatalf("event document teardown ownership = %#v", stats)
	}
}

func TestStockV8NodeMutationChurnPreservesOwnershipBoundaries(t *testing.T) {
	const iterations = 64

	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatalf("NewWithEngine: %v", err)
	}
	defer func() {
		if err := browserRuntime.Close(); err != nil {
			t.Errorf("Close browser: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page, err := browserRuntime.LoadPage(
		ctx,
		"https://gossamer.test/churn",
		staticDocumentLoader{document: `<!doctype html><html><body style="margin:0"><div id="mount"></div></body></html>`},
	)
	if err != nil {
		t.Fatalf("LoadPage: %v", err)
	}
	realm, ok := engine.LatestRealm()
	if !ok {
		t.Fatal("stock V8 engine has no live document realm")
	}
	document := page.Document()
	initialNodes := document.Store().Len()
	initialLiveNodes := document.Store().LiveLen()
	baselineLedger := browserRuntime.Ledger().Stats()
	baselineProfile, err := realm.Profile()
	if err != nil {
		t.Fatalf("baseline Profile: %v", err)
	}
	baselineNative := page.Realm.Profile().Memory
	if baselineNative.LiveHostObjects != uint64(initialLiveNodes) {
		t.Fatalf("baseline native facades = %#v, live nodes=%d", baselineNative, initialLiveNodes)
	}

	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/churn.js",
		Source: `
			(() => {
				const mount = document.getElementById("mount");
				if (mount !== document.getElementById("mount")) {
					throw new Error("mount wrapper identity changed");
				}
				for (let round = 0; round < 64; round++) {
					const row = document.createElement("div");
					const alias = row;
					if (alias !== row) throw new Error("ordinary alias changed identity");
					row.setAttribute("data-round", String(round));
					row.setAttribute("data-temp", "discard");
					if (row.getAttribute("data-round") !== String(round)) {
						throw new Error("attribute write did not round trip");
					}
					row.removeAttribute("data-temp");
					if (row.getAttribute("data-temp") !== null) {
						throw new Error("attribute removal did not round trip");
					}

					const left = document.createElement("span");
					const leftText = document.createTextNode("L" + round);
					if (left.appendChild(leftText) !== leftText) {
						throw new Error("appendChild did not return its child");
					}
					const right = document.createElement("span");
					right.appendChild(document.createTextNode("R" + round));
					row.appendChild(left);
					row.appendChild(right);
					if (row.insertBefore(right, left) !== right) {
						throw new Error("insertBefore did not return its child");
					}
					if (row.textContent !== "R" + round + "L" + round) {
						throw new Error("child reorder changed text order");
					}
					if (row.removeChild(left) !== left) {
						throw new Error("removeChild did not return its child");
					}
					row.appendChild(left);
					mount.appendChild(row);
					if (mount.removeChild(row) !== row) {
						throw new Error("detachment changed wrapper identity");
					}
				}

				const survivor = document.createElement("div");
				survivor.setAttribute("id", "survivor");
				survivor.setAttribute("data-state", "connected");
				survivor.textContent = "survivor";
				mount.appendChild(survivor);
			})();
		`,
	})
	if err != nil {
		t.Fatalf("QueueScript: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("run churn script: %v", err)
	}
	if !page.Dirty() || page.Realm.Tasks.Len() != 1 {
		t.Fatalf("churn task = dirty:%t queued:%d; want dirty with one render", page.Dirty(), page.Realm.Tasks.Len())
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("render churn result: %v", err)
	}
	if page.Dirty() || !v8FrameContainsText(page.Frame(), "survivor") {
		t.Fatal("churn result did not reach paint")
	}

	const createdNodes = iterations*5 + 2
	if got := document.Store().Len() - initialNodes; got != createdNodes {
		t.Fatalf("document-retained churn nodes = %d, want %d", got, createdNodes)
	}
	mountID, found := document.ElementByID("mount")
	if !found {
		t.Fatal("mount disappeared after churn")
	}
	survivorID, found := document.ElementByID("survivor")
	if !found {
		t.Fatal("survivor was not connected after churn")
	}
	mount, _ := document.Resolve(mountID)
	survivor, _ := document.Resolve(survivorID)
	if len(mount.Children) != 1 || mount.Children[0] != survivor {
		t.Fatalf("connected mount children = %#v, want survivor only", mount.Children)
	}
	if got, err := document.TextContent(survivorID); err != nil || got != "survivor" {
		t.Fatalf("survivor text = %q, %v", got, err)
	}
	if got, found, err := document.GetAttribute(survivorID, "data-state"); err != nil || !found || got != "connected" {
		t.Fatalf("survivor attribute = %q, %t, %v", got, found, err)
	}

	afterRenderLedger := browserRuntime.Ledger().Stats()
	if afterRenderLedger.ObjectsCreated-baselineLedger.ObjectsCreated != createdNodes*2+2 ||
		afterRenderLedger.ObjectsDestroyed-baselineLedger.ObjectsDestroyed != 2 ||
		afterRenderLedger.LiveObjects-baselineLedger.LiveObjects != createdNodes*2 ||
		afterRenderLedger.TaskLocalAllocations-baselineLedger.TaskLocalAllocations != createdNodes+1 ||
		afterRenderLedger.PublishOperations-baselineLedger.PublishOperations != 2 ||
		afterRenderLedger.TransferOperations-baselineLedger.TransferOperations != 2 ||
		afterRenderLedger.PersistentObjects-baselineLedger.PersistentObjects != createdNodes*2 {
		t.Fatalf("churn crossed unexpected ARC boundaries: before=%#v after=%#v", baselineLedger, afterRenderLedger)
	}
	afterRenderProfile, err := realm.Profile()
	if err != nil {
		t.Fatalf("Profile after churn: %v", err)
	}
	// The document wrapper is established by navigation lifecycle dispatch
	// before the baseline; churn adds the mount, temporary nodes, and survivor.
	const createdWrappers = 1 + iterations*5 + 1
	if got := afterRenderProfile.WrappersCreated - baselineProfile.WrappersCreated; got != createdWrappers {
		t.Fatalf("wrappers created by churn = %d, want %d", got, createdWrappers)
	}
	if afterRenderProfile.WrapperCacheHits-baselineProfile.WrapperCacheHits != 1 {
		t.Fatalf("wrapper cache hits after churn = %d, want 1", afterRenderProfile.WrapperCacheHits-baselineProfile.WrapperCacheHits)
	}
	if native := page.Realm.Profile().Memory; native.LiveHostObjects-baselineNative.LiveHostObjects != uint64(createdNodes) {
		t.Fatalf("native facade records after churn = %#v, baseline=%#v", native, baselineNative)
	}

	ledgerBeforeGC := browserRuntime.Ledger().Stats()
	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage: %v", err)
	}
	ledgerAfterGC := browserRuntime.Ledger().Stats()
	const connectedCreatedNodes = 2
	const reclaimedDetachedNodes = createdNodes - connectedCreatedNodes
	if ledgerAfterGC.ObjectsDestroyed-ledgerBeforeGC.ObjectsDestroyed != reclaimedDetachedNodes*2 ||
		ledgerAfterGC.LiveObjects-baselineLedger.LiveObjects != connectedCreatedNodes*2 ||
		ledgerAfterGC.PersistentObjects-baselineLedger.PersistentObjects != connectedCreatedNodes*2 ||
		document.Store().LiveLen()-initialLiveNodes != connectedCreatedNodes {
		t.Fatalf("detached-node collection boundary: before=%#v after=%#v liveNodes=%d",
			ledgerBeforeGC, ledgerAfterGC, document.Store().LiveLen()-initialLiveNodes)
	}
	afterGC, err := realm.Profile()
	if err != nil {
		t.Fatalf("Profile after churn GC: %v", err)
	}
	if afterGC.WrappersCollected-baselineProfile.WrappersCollected != createdWrappers ||
		afterGC.LiveWrappers != baselineProfile.LiveWrappers {
		t.Fatalf("wrapper reclamation after churn = %#v, baseline=%#v", afterGC, baselineProfile)
	}
	if native := page.Realm.Profile().Memory; native.LiveHostObjects-baselineNative.LiveHostObjects != uint64(connectedCreatedNodes) {
		t.Fatalf("native facade records after GC = %#v, baseline=%#v", native, baselineNative)
	}

	if err := page.Close(); err != nil {
		t.Fatalf("Close churn page: %v", err)
	}
	closedProfile := engine.Profile()
	if closedProfile.RealmsCreated != 2 || closedProfile.RealmsClosed != 2 ||
		closedProfile.ClosedRealms.WrappersCreated < createdWrappers ||
		closedProfile.ClosedRealms.LiveWrappers != 1 {
		t.Fatalf("churn teardown profile = %#v", closedProfile)
	}
	if finalLedger := browserRuntime.Ledger().Stats(); finalLedger.LiveObjects != 0 || finalLedger.PersistentObjects != 0 {
		t.Fatalf("churn teardown ownership = %#v", finalLedger)
	}
	if native := page.Realm.Profile().Memory; native.LiveHostObjects != 0 || native.LiveSlots != 0 {
		t.Fatalf("churn teardown native facades = %#v", native)
	}
}

func TestStockV8AtomicConvenienceMutationsAndDOMExceptions(t *testing.T) {
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatalf("NewWithEngine: %v", err)
	}
	defer func() {
		if err := browserRuntime.Close(); err != nil {
			t.Errorf("Close browser: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page, err := browserRuntime.LoadPage(ctx, "https://gossamer.test/dom-mutations", staticDocumentLoader{
		document: `<!doctype html><html><body><main id="root"><p id="first">A</p><p id="second">B</p></main></body></html>`,
	})
	if err != nil {
		t.Fatalf("LoadPage: %v", err)
	}
	realm, ok := engine.LatestRealm()
	if !ok {
		t.Fatal("stock V8 engine has no live document realm")
	}

	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/dom-mutations/assert.js",
		Source: `
			(() => {
				const root = document.getElementById("root");
				const first = document.getElementById("first");
				const second = document.getElementById("second");
				const strong = document.createElement("strong");
				strong.textContent = "S";
				root.prepend("zero", strong);
				first.before("before");
				first.after("after");
				second.replaceWith(first, "tail");
				if (root.textContent !== "zeroSbeforeafterAtail" ||
					root.querySelectorAll("p").length !== 1 ||
					second.parentNode !== null || first.parentNode !== root) {
					throw new Error("convenience mutation order or identity failed: " + root.textContent);
				}

				const before = root.innerHTML;
				const invalid = document.createElement("section");
				let failures = 0;
				try { document.append(invalid); } catch (error) {
					if (!(error instanceof DOMException) || error.name !== "HierarchyRequestError" ||
						error.code !== DOMException.HIERARCHY_REQUEST_ERR ||
						Object.prototype.toString.call(error) !== "[object DOMException]") throw error;
					failures++;
				}
				if (invalid.parentNode !== null || root.innerHTML !== before) {
					throw new Error("failed hierarchy mutation changed the tree");
				}
				try { root.removeChild(document.body); } catch (error) {
					if (!(error instanceof DOMException) || error.name !== "NotFoundError") throw error;
					failures++;
				}
				try { document.createElement("bad name"); } catch (error) {
					if (!(error instanceof DOMException) || error.name !== "InvalidCharacterError") throw error;
					failures++;
				}
				try { root.querySelector("["); } catch (error) {
					if (!(error instanceof DOMException) || error.name !== "SyntaxError") throw error;
					failures++;
				}
				if (failures !== 4) throw new Error("missing typed DOM failures: " + failures);

				root.replaceChildren("done", strong);
				if (root.textContent !== "doneS" || strong.parentNode !== root) {
					throw new Error("replaceChildren failed");
				}
				strong.remove();
				if (root.textContent !== "done" || strong.parentNode !== null) {
					throw new Error("remove failed");
				}
				globalThis.__heldMilestone8Node = strong;
			})();
		`,
	})
	if err != nil {
		t.Fatalf("QueueScript: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("run DOM mutation assertions: %v", err)
	}
	for page.Dirty() {
		if err := page.Realm.RunOne(context.Background()); err != nil {
			t.Fatalf("render DOM mutations: %v", err)
		}
	}
	if !v8FrameContainsText(page.Frame(), "done") {
		t.Fatal("convenience mutation did not reach paint")
	}
	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage with held detached node: %v", err)
	}
	_, err = page.QueueScript(browser.ScriptSource{
		URL:    "https://gossamer.test/dom-mutations/release.js",
		Source: `globalThis.__heldMilestone8Node = undefined;`,
	})
	if err != nil {
		t.Fatalf("QueueScript release: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("release held node: %v", err)
	}
	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage after release: %v", err)
	}
	if err := page.Close(); err != nil {
		t.Fatalf("Close page: %v", err)
	}
	if ledger := browserRuntime.Ledger().Stats(); ledger.LiveObjects != 0 || ledger.PersistentObjects != 0 {
		t.Fatalf("Milestone 8 teardown ownership = %#v", ledger)
	}
}

func TestStockV8SpecializedHTMLElementInterfacesPreserveCanonicalIdentity(t *testing.T) {
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatalf("NewWithEngine: %v", err)
	}
	defer func() {
		if err := browserRuntime.Close(); err != nil {
			t.Errorf("Close browser: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page, err := browserRuntime.LoadPage(ctx, "https://gossamer.test/dom-interfaces", staticDocumentLoader{
		document: `<!doctype html><html><body><form id="form"><input id="input"><textarea id="textarea"></textarea><select id="select"><option id="option">one</option></select><button id="button">go</button></form><template id="template"></template><iframe id="iframe"></iframe><div id="generic"></div></body></html>`,
	})
	if err != nil {
		t.Fatalf("LoadPage: %v", err)
	}
	realm, ok := engine.LatestRealm()
	if !ok {
		t.Fatal("stock V8 engine has no live document realm")
	}
	baselineLiveNodes := page.Document().Store().LiveLen()

	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/dom-interfaces/assert.js",
		Source: `
			(() => {
				const cases = [
					["form", HTMLFormElement],
					["input", HTMLInputElement],
					["textarea", HTMLTextAreaElement],
					["select", HTMLSelectElement],
					["option", HTMLOptionElement],
					["button", HTMLButtonElement],
					["template", HTMLTemplateElement],
					["iframe", HTMLIFrameElement]
				];
				for (const [id, Interface] of cases) {
					const node = document.getElementById(id);
					if (!(node instanceof Interface) || !(node instanceof HTMLElement) ||
						!(node instanceof Element) || !(node instanceof Node) ||
						Object.getPrototypeOf(node) !== Interface.prototype ||
						node.constructor !== Interface || document.querySelector("#" + id) !== node) {
						throw new Error("specialized interface failed for " + id);
					}
				}
				const input = document.getElementById("input");
				if (input instanceof HTMLTextAreaElement ||
					document.getElementById("generic").constructor !== HTMLElement ||
					Object.getPrototypeOf(document.createElement("input")) !== HTMLInputElement.prototype ||
					Object.getPrototypeOf(document.createElementNS("urn:test", "input")) !== Element.prototype) {
					throw new Error("interface resolver crossed a namespace or local-name boundary");
				}
				const descriptor = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value");
				if (!descriptor || typeof descriptor.get !== "function" || typeof descriptor.set !== "function" ||
					Object.prototype.hasOwnProperty.call(HTMLElement.prototype, "value")) {
					throw new Error("form value descriptor is not specialized");
				}
				input.value = "typed";
				if (descriptor.get.call(input) !== "typed" ||
					document.getElementById("form").children[0] !== input) {
					throw new Error("specialized accessor or canonical collection identity failed");
				}
				input.remove();
				globalThis.__heldMilestone9Input = input;
			})();
		`,
	})
	if err != nil {
		t.Fatalf("QueueScript: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("run interface assertions: %v", err)
	}
	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage with held specialized wrapper: %v", err)
	}
	if got := page.Document().Store().LiveLen(); got < baselineLiveNodes {
		t.Fatalf("held specialized wrapper lost its native node: got %d, baseline %d", got, baselineLiveNodes)
	}
	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/dom-interfaces/release.js",
		Source: `
			if (!(__heldMilestone9Input instanceof HTMLInputElement) || __heldMilestone9Input.value !== "typed") {
				throw new Error("held specialized wrapper changed identity or state");
			}
			globalThis.__heldMilestone9Input = undefined;
		`,
	})
	if err != nil {
		t.Fatalf("QueueScript release: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("release specialized wrapper: %v", err)
	}
	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage after specialized release: %v", err)
	}
	if err := page.Close(); err != nil {
		t.Fatalf("Close page: %v", err)
	}
	if ledger := browserRuntime.Ledger().Stats(); ledger.LiveObjects != 0 || ledger.PersistentObjects != 0 {
		t.Fatalf("Milestone 9 teardown ownership = %#v", ledger)
	}
}

func TestStockV8CoordinatedFormStateAndLiveControlCollections(t *testing.T) {
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatalf("NewWithEngine: %v", err)
	}
	defer func() {
		if err := browserRuntime.Close(); err != nil {
			t.Errorf("Close browser: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page, err := browserRuntime.LoadPage(ctx, "https://gossamer.test/forms", staticDocumentLoader{
		document: `<!doctype html><html><body>
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
		</body></html>`,
	})
	if err != nil {
		t.Fatalf("LoadPage: %v", err)
	}
	realm, ok := engine.LatestRealm()
	if !ok {
		t.Fatal("stock V8 engine has no live form realm")
	}

	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/forms/assert.js",
		Source: `
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
					!(options instanceof HTMLCollection) || select.options !== options ||
					options.length !== 2 || options[1] !== two || options.namedItem("one") !== one) {
					throw new Error("live form collection shape or canonical identity failed");
				}
				if (first.form !== form || notes.form !== form || select.form !== form ||
					external.form !== form || document.createElement("input").form !== null) {
					throw new Error("form owner association failed");
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
				globalThis.__heldMilestone10Elements = elements;
				globalThis.__heldMilestone10Options = options;
			})();
		`,
	})
	if err != nil {
		t.Fatalf("QueueScript: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("run form assertions: %v", err)
	}
	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage with live form collections: %v", err)
	}
	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/forms/release.js",
		Source: `
			if (__heldMilestone10Elements.namedItem("notes").value !== "default notes" ||
				__heldMilestone10Options[1].value !== "two") {
				throw new Error("collection-only reachability lost native form state");
			}
			globalThis.__heldMilestone10Elements = undefined;
			globalThis.__heldMilestone10Options = undefined;
		`,
	})
	if err != nil {
		t.Fatalf("QueueScript release: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("release live form collections: %v", err)
	}
	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage after collection release: %v", err)
	}
	if err := page.Close(); err != nil {
		t.Fatalf("Close page: %v", err)
	}
	if ledger := browserRuntime.Ledger().Stats(); ledger.LiveObjects != 0 || ledger.PersistentObjects != 0 {
		t.Fatalf("Milestone 10 teardown ownership = %#v", ledger)
	}
}

func TestStockV8TextSelectionBeforeInputEditingAndComposition(t *testing.T) {
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatalf("NewWithEngine: %v", err)
	}
	defer func() {
		if err := browserRuntime.Close(); err != nil {
			t.Errorf("Close browser: %v", err)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page, err := browserRuntime.LoadPage(ctx, "https://gossamer.test/editing", staticDocumentLoader{
		document: `<!doctype html><html><body><input id="editor" value="A😀BC"><textarea id="area">line</textarea></body></html>`,
	})
	if err != nil {
		t.Fatalf("LoadPage: %v", err)
	}
	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/editing/setup.js",
		Source: `
			(() => {
				const editor = document.getElementById("editor");
				const area = document.getElementById("area");
				for (const name of ["selectionStart", "selectionEnd", "selectionDirection"]) {
					const descriptor = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, name);
					if (!descriptor || typeof descriptor.get !== "function" || typeof descriptor.set !== "function") {
						throw new Error(name + " descriptor is incomplete");
					}
				}
				editor.setSelectionRange(1, 3, "backward");
				if (editor.selectionStart !== 1 || editor.selectionEnd !== 3 ||
					editor.selectionDirection !== "backward") {
					throw new Error("UTF-16 selection range failed");
				}
				area.select();
				if (area.selectionStart !== 0 || area.selectionEnd !== 4) {
					throw new Error("textarea.select() failed");
				}
				globalThis.__editLog = [];
				editor.addEventListener("beforeinput", event => {
					if (!(event instanceof InputEvent) || !event.isTrusted) {
						throw new Error("beforeinput interface failed");
					}
					__editLog.push("before:" + event.inputType + ":" + event.data + ":" + event.isComposing + ":" + editor.value + ":" + editor.selectionStart + "-" + editor.selectionEnd);
					if (event.data === "NO") event.preventDefault();
				});
				editor.addEventListener("input", event => {
					if (!(event instanceof InputEvent) || event.cancelable) {
						throw new Error("input interface failed");
					}
					__editLog.push("input:" + event.inputType + ":" + event.data + ":" + event.isComposing + ":" + editor.value + ":" + editor.selectionStart + "-" + editor.selectionEnd);
				});
				for (const type of ["compositionstart", "compositionupdate", "compositionend"]) {
					editor.addEventListener(type, event => {
						if (!(event instanceof CompositionEvent) || !(event instanceof Event) || !event.isTrusted) {
							throw new Error(type + " interface failed");
						}
						__editLog.push(type + ":" + event.data);
					});
				}
				const synthetic = new CompositionEvent("compositionupdate", {data:"synthetic"});
				if (synthetic.data !== "synthetic" || synthetic.isTrusted) {
					throw new Error("CompositionEvent constructor failed");
				}
			})();
		`,
	})
	if err != nil {
		t.Fatalf("QueueScript setup: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("run editing setup: %v", err)
	}
	editorID, found := page.Document().ElementByID("editor")
	if !found {
		t.Fatal("editor input is missing")
	}
	target := browser.NodeHandle{Document: page.DocumentGeneration(), Node: editorID}
	events := []browser.InputEvent{
		{Type: browser.InputCompositionStart, Target: target, Data: "候"},
		{Type: browser.InputBeforeInput, Target: target, Data: "Z", InputType: "insertText", IsComposing: true},
		{Type: browser.InputBeforeInput, Target: target, Data: "NO", InputType: "insertText", IsComposing: true},
		{Type: browser.InputBeforeInput, Target: target, InputType: "deleteContentBackward", IsComposing: true},
		{Type: browser.InputCompositionUpdate, Target: target, Data: "候補"},
		{Type: browser.InputBeforeInput, Target: target, Data: "候", InputType: "insertCompositionText", IsComposing: true},
		{Type: browser.InputCompositionEnd, Target: target, Data: "候"},
	}
	for _, event := range events {
		if _, err := page.QueueInputEvent(event); err != nil {
			t.Fatalf("QueueInputEvent(%s): %v", event.Type, err)
		}
	}
	for range events {
		if err := page.Realm.RunOne(context.Background()); err != nil {
			t.Fatalf("run editing input: %v", err)
		}
	}
	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/editing/assert.js",
		Source: `
			const editor = document.getElementById("editor");
			if (editor.value !== "A候BC" || editor.selectionStart !== 2 || editor.selectionEnd !== 2) {
				throw new Error("native editing result: " + editor.value + " @ " + editor.selectionStart);
			}
			const expected = [
				"compositionstart:候",
				"before:insertText:Z:true:A😀BC:1-3",
				"input:insertText:Z:true:AZBC:2-2",
				"before:insertText:NO:true:AZBC:2-2",
				"before:deleteContentBackward::true:AZBC:2-2",
				"input:deleteContentBackward::true:ABC:1-1",
				"compositionupdate:候補",
				"before:insertCompositionText:候:true:ABC:1-1",
				"input:insertCompositionText:候:true:A候BC:2-2",
				"compositionend:候",
			];
			if (__editLog.join("|") !== expected.join("|")) {
				throw new Error("editing event order: " + __editLog.join("|"));
			}
		`,
	})
	if err != nil {
		t.Fatalf("QueueScript assertions: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("run editing assertions: %v", err)
	}
	if err := page.Close(); err != nil {
		t.Fatalf("Close page: %v", err)
	}
	if ledger := browserRuntime.Ledger().Stats(); ledger.LiveObjects != 0 || ledger.PersistentObjects != 0 {
		t.Fatalf("Milestone 11 teardown ownership = %#v", ledger)
	}
}

func TestStockV8MutationObserverFiltersRecordsAndOwnsDetachedTargets(t *testing.T) {
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatalf("NewWithEngine: %v", err)
	}
	defer func() {
		if err := browserRuntime.Close(); err != nil {
			t.Errorf("Close browser: %v", err)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page, err := browserRuntime.LoadPage(ctx, "https://gossamer.test/observer", staticDocumentLoader{
		document: `<!doctype html><html><body><div id="root"><span id="old">before</span></div></body></html>`,
	})
	if err != nil {
		t.Fatalf("LoadPage: %v", err)
	}
	realm, ok := engine.LatestRealm()
	if !ok {
		t.Fatal("stock V8 engine has no observer realm")
	}
	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/observer/mutate.js",
		Source: `
			(() => {
				const root = document.getElementById("root");
				const old = document.getElementById("old");
				globalThis.__observerCallbacks = 0;
				globalThis.__observerRecords = [];
				const observer = new MutationObserver((records, deliveredObserver) => {
					if (deliveredObserver !== observer) throw new Error("observer callback identity failed");
					__observerCallbacks++;
					for (const record of records) {
						if (!(record instanceof MutationRecord) || !(record.addedNodes instanceof NodeList) ||
							!(record.removedNodes instanceof NodeList)) {
							throw new Error("MutationRecord interface failed");
						}
						__observerRecords.push({
							type: record.type,
							target: record.target,
							added: Array.from(record.addedNodes),
							removed: Array.from(record.removedNodes),
							attributeName: record.attributeName,
							oldValue: record.oldValue,
						});
					}
				});
				observer.observe(root, {
					childList: true,
					subtree: true,
					attributes: true,
					attributeOldValue: true,
					attributeFilter: ["data-state"],
					characterData: true,
					characterDataOldValue: true,
				});
				old.setAttribute("ignored", "x");
				old.setAttribute("data-state", "one");
				old.setAttribute("data-state", "two");
				old.firstChild.data = "after";
				const added = document.createElement("b");
				added.id = "added";
				root.append(added);
				old.remove();
				root.append(old);

				const manual = new MutationObserver(() => { throw new Error("takeRecords callback ran"); });
				manual.observe(old, {attributes:true, attributeOldValue:true});
				old.setAttribute("title", "manual");
				const taken = manual.takeRecords();
				if (taken.length !== 1 || taken[0].type !== "attributes" ||
					taken[0].target !== old || taken[0].attributeName !== "title" || taken[0].oldValue !== null) {
					throw new Error("takeRecords did not synchronously drain native records");
				}
				manual.disconnect();
				old.setAttribute("title", "disconnected");
				if (__observerCallbacks !== 0) throw new Error("observer delivered before the checkpoint");

				const detached = document.createElement("aside");
				detached.id = "detached-observed";
				globalThis.__detachedObserver = new MutationObserver(records => {
					globalThis.__detachedDelivery = records[0].target.id;
				});
				__detachedObserver.observe(detached, {attributes:true});
				detached.setAttribute("data-held", "yes");
			})();
		`,
	})
	if err != nil {
		t.Fatalf("QueueScript mutations: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("run observer mutations: %v", err)
	}
	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage with observer-only detached target: %v", err)
	}
	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/observer/assert.js",
		Source: `
			if (__observerCallbacks !== 1) throw new Error("observer callback count: " + __observerCallbacks);
			if (__detachedDelivery !== "detached-observed") throw new Error("detached observer target was not retained");
			const attributes = __observerRecords.filter(record => record.type === "attributes");
			const character = __observerRecords.filter(record => record.type === "characterData");
			const children = __observerRecords.filter(record => record.type === "childList");
			if (attributes.length !== 2 || attributes[0].attributeName !== "data-state" ||
				attributes[0].oldValue !== null || attributes[1].oldValue !== "one") {
				throw new Error("attribute filters or oldValue failed");
			}
			if (character.length !== 1 || character[0].oldValue !== "before" ||
				character[0].target.data !== "after") {
				throw new Error("characterData record failed");
			}
			if (children.length !== 3 || children[0].added[0].id !== "added" ||
				children[1].removed[0].id !== "old" || children[2].added[0] !== document.getElementById("old")) {
				throw new Error("childList records or wrapper identity failed");
			}
			__detachedObserver.disconnect();
			globalThis.__detachedObserver = undefined;
		`,
	})
	if err != nil {
		t.Fatalf("QueueScript assertions: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("run observer assertions: %v", err)
	}
	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage after observer disconnect: %v", err)
	}
	if err := page.Close(); err != nil {
		t.Fatalf("Close page: %v", err)
	}
	if ledger := browserRuntime.Ledger().Stats(); ledger.LiveObjects != 0 || ledger.PersistentObjects != 0 {
		t.Fatalf("Milestone 12 teardown ownership = %#v", ledger)
	}
}

func TestStockV8TemplateConstructionRangeAndTraversalObjects(t *testing.T) {
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatalf("NewWithEngine: %v", err)
	}
	defer func() {
		if err := browserRuntime.Close(); err != nil {
			t.Errorf("Close browser: %v", err)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page, err := browserRuntime.LoadPage(ctx, "https://gossamer.test/construction", staticDocumentLoader{
		document: `<!doctype html><html><body><template id="card"><span data-part="inside">one</span></template><div id="range"><i>a</i><b>b</b><u>c</u></div></body></html>`,
	})
	if err != nil {
		t.Fatalf("LoadPage: %v", err)
	}
	realm, ok := engine.LatestRealm()
	if !ok {
		t.Fatal("stock V8 engine has no construction realm")
	}
	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/construction/assert.js",
		Source: `
			(() => {
				const template = document.getElementById("card");
				const content = template.content;
				if (!(content instanceof DocumentFragment) || template.content !== content ||
					template.childNodes.length !== 0 || content.firstElementChild.localName !== "span" ||
					template.innerHTML !== '<span data-part="inside">one</span>') {
					throw new Error("template content parsing or SameObject identity failed");
				}
				template.innerHTML = "<em>two</em><strong>three</strong>";
				if (content.children.length !== 2 || template.content !== content ||
					template.innerHTML !== "<em>two</em><strong>three</strong>") {
					throw new Error("template innerHTML did not mutate the inert fragment");
				}
				const clonedTemplate = template.cloneNode(true);
				if (clonedTemplate.content === content || clonedTemplate.content.children.length !== 2 ||
					clonedTemplate.content.firstElementChild === content.firstElementChild) {
					throw new Error("deep template cloning aliased source content");
				}

				const textHost = document.createElement("div");
				const text = document.createTextNode("A😀B");
				textHost.append(text, document.createTextNode("C"), document.createTextNode(""));
				const tail = text.splitText(3);
				if (text.data !== "A😀" || tail.data !== "B" || text.nextSibling !== tail) {
					throw new Error("Text.splitText did not use UTF-16 offsets");
				}
				textHost.normalize();
				if (textHost.childNodes.length !== 1 || textHost.firstChild !== text || text.data !== "A😀BC") {
					throw new Error("Node.normalize did not merge adjacent text nodes");
				}

				const imported = document.importNode(template, true);
				if (imported === template || imported.content.children.length !== 2) {
					throw new Error("Document.importNode did not clone template content");
				}
				const adoptionParent = document.createElement("section");
				const adopted = document.createElement("aside");
				adoptionParent.append(adopted);
				if (document.adoptNode(adopted) !== adopted || adopted.parentNode !== null ||
					adoptionParent.childNodes.length !== 0) {
					throw new Error("same-document adoptNode did not detach and preserve identity");
				}

				const rangeRoot = document.getElementById("range");
				const range = document.createRange();
				if (!(range instanceof Range)) throw new Error("Range interface missing");
				range.setStart(rangeRoot, 0);
				range.setEnd(rangeRoot, 2);
				if (range.startContainer !== rangeRoot || range.startOffset !== 0 || range.endOffset !== 2 ||
					range.collapsed || range.commonAncestorContainer !== rangeRoot) {
					throw new Error("Range boundary state failed");
				}
				const rangeClone = range.cloneRange();
				const clonedContents = range.cloneContents();
				if (rangeClone.startContainer !== rangeRoot || clonedContents.children.length !== 2 ||
					clonedContents.firstElementChild === rangeRoot.firstElementChild || range.collapsed) {
					throw new Error("Range cloning failed");
				}
				const extractRoot = document.createElement("div");
				extractRoot.innerHTML = "<q>x</q><q>y</q><q>z</q>";
				const extractedY = extractRoot.children[1];
				const extractRange = document.createRange();
				extractRange.setStart(extractRoot, 1);
				extractRange.setEnd(extractRoot, 3);
				const extracted = extractRange.extractContents();
				if (extractRoot.children.length !== 1 || extracted.children.length !== 2 ||
					extracted.firstElementChild !== extractedY || !extractRange.collapsed) {
					throw new Error("Range.extractContents did not move selected children");
				}
				const inserted = document.createElement("mark");
				range.collapse(true);
				range.insertNode(inserted);
				if (rangeRoot.firstElementChild !== inserted) throw new Error("Range.insertNode failed");

				const traversalRoot = document.createElement("div");
				traversalRoot.innerHTML = '<section id="one"><span id="nested"></span></section><section id="skip"><b id="kept"></b></section>';
				const filter = node => node.id === "skip" ? NodeFilter.FILTER_SKIP : NodeFilter.FILTER_ACCEPT;
				const walker = document.createTreeWalker(traversalRoot, NodeFilter.SHOW_ELEMENT, filter);
				if (!(walker instanceof TreeWalker) || walker.root !== traversalRoot ||
					walker.currentNode !== traversalRoot || walker.whatToShow !== NodeFilter.SHOW_ELEMENT ||
					walker.filter !== filter) {
					throw new Error("TreeWalker interface state failed");
				}
				const walked = [];
				for (let node = walker.nextNode(); node; node = walker.nextNode()) walked.push(node.id);
				if (walked.join(",") !== "one,nested,kept") throw new Error("TreeWalker order: " + walked);
				walker.currentNode = traversalRoot.firstElementChild;
				if (walker.firstChild().id !== "nested" || walker.parentNode().id !== "one" ||
					walker.nextSibling().id !== "kept") {
					throw new Error("TreeWalker relation methods failed");
				}
				const iterator = document.createNodeIterator(traversalRoot, NodeFilter.SHOW_ELEMENT);
				if (!(iterator instanceof NodeIterator) || iterator.referenceNode !== traversalRoot ||
					!iterator.pointerBeforeReferenceNode || iterator.nextNode() !== traversalRoot ||
					iterator.pointerBeforeReferenceNode || iterator.nextNode().id !== "one") {
					throw new Error("NodeIterator cursor semantics failed");
				}
				iterator.detach();

				globalThis.__milestone13Content = content;
				globalThis.__milestone13Range = rangeClone;
				globalThis.__milestone13Walker = document.createTreeWalker(traversalRoot, NodeFilter.SHOW_ELEMENT);
			})();
		`,
	})
	if err != nil {
		t.Fatalf("QueueScript assertions: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("run construction assertions: %v", err)
	}
	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage with facade-only roots: %v", err)
	}
	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/construction/gc.js",
		Source: `
			if (__milestone13Content.firstElementChild.localName !== "em" ||
				__milestone13Range.startContainer.id !== "range" ||
				__milestone13Walker.root.children.length !== 2) {
				throw new Error("construction facade lost its native root across GC");
			}
			globalThis.__milestone13Content = undefined;
			globalThis.__milestone13Range = undefined;
			globalThis.__milestone13Walker = undefined;
		`,
	})
	if err != nil {
		t.Fatalf("QueueScript GC assertion: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("run construction GC assertion: %v", err)
	}
	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage after construction release: %v", err)
	}
	if err := page.Close(); err != nil {
		t.Fatalf("Close page: %v", err)
	}
	if ledger := browserRuntime.Ledger().Stats(); ledger.LiveObjects != 0 || ledger.PersistentObjects != 0 {
		t.Fatalf("Milestone 13 teardown ownership = %#v", ledger)
	}
}

func TestStockV8FormSubmissionNavigatesAndTearsDownOldRealm(t *testing.T) {
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatalf("NewWithEngine: %v", err)
	}
	defer func() {
		if err := browserRuntime.Close(); err != nil {
			t.Errorf("Close browser: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page, err := browserRuntime.LoadPage(ctx, "https://gossamer.test/form-start", staticDocumentLoader{
		document: `<!doctype html><html><body>
			<form id="search" action="/results">
				<input id="query" name="q" required>
				<input name="tag" value="memory">
				<input name="tag" value="regions">
				<button id="go" name="commit" value="yes">Go</button>
			</form>
			<form id="novalidate-search" novalidate>
				<input id="novalidate-query" required>
			</form>
		</body></html>`,
	})
	if err != nil {
		t.Fatalf("LoadPage: %v", err)
	}
	oldRealm, ok := engine.LatestRealm()
	if !ok {
		t.Fatal("stock V8 engine has no live form submission realm")
	}
	page.SetFormNavigationLoader(staticDocumentLoader{
		document: `<!doctype html><html><body><p>submitted result</p></body></html>`,
	})
	initialNavigation := page.Navigation().ID

	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/form-start/form-api.js",
		Source: `
			(() => {
				const form = document.getElementById("search");
				const query = document.getElementById("query");
				const button = document.getElementById("go");
				const data = new FormData(form, button);
				if (!(data instanceof FormData) || Object.prototype.toString.call(data) !== "[object FormData]" ||
					data.get("q") !== "" || data.getAll("tag").join(",") !== "memory,regions" ||
					data.get("commit") !== "yes" || !data.has("tag") ||
					JSON.stringify([...data]) !== JSON.stringify([["q", ""], ["tag", "memory"], ["tag", "regions"], ["commit", "yes"]])) {
					throw new Error("FormData did not snapshot successful controls");
				}
				data.append("tag", "queues");
				data.set("q", "gossamer");
				data.delete("commit");
				const visited = [];
				data.forEach((value, name, owner) => {
					if (owner !== data) throw new Error("FormData forEach owner mismatch");
					visited.push(name + "=" + value);
				});
				if (data.get("q") !== "gossamer" || data.has("commit") ||
					data.getAll("tag").join(",") !== "memory,regions,queues" || visited.length !== 4 ||
					[...data.keys()].join(",") !== "q,tag,tag,tag" ||
					[...data.values()].join(",") !== "gossamer,memory,regions,queues") {
					throw new Error("FormData mutation or iteration failed");
				}

				let invalid = 0;
				query.addEventListener("invalid", event => {
					if (event.bubbles || !event.cancelable || event.target !== query) {
						throw new Error("invalid event shape failed");
					}
					invalid++;
				});
				if (query.matches(":user-valid") || query.matches(":user-invalid")) {
					throw new Error("untouched required input matched a user-validity pseudo");
				}
				if (form.checkValidity() || invalid !== 1) throw new Error("checkValidity accepted required input");
				if (query.matches(":user-invalid")) throw new Error("checkValidity changed user validity");
				form.requestSubmit(button);
				if (invalid !== 2) throw new Error("requestSubmit skipped invalid dispatch");
				if (!query.matches(":user-invalid") || query.matches(":user-valid")) {
					throw new Error("requestSubmit did not expose :user-invalid");
				}
				form.reset();
				if (query.matches(":user-valid") || query.matches(":user-invalid")) {
					throw new Error("form reset did not clear user validity");
				}

				const novalidateForm = document.getElementById("novalidate-search");
				const novalidateQuery = document.getElementById("novalidate-query");
				novalidateForm.addEventListener("submit", event => event.preventDefault());
				novalidateForm.requestSubmit();
				if (!novalidateQuery.matches(":user-invalid")) {
					throw new Error("novalidate requestSubmit did not set user validity");
				}

				query.value = "gossamer";
				if (query.matches(":user-valid") || query.matches(":user-invalid")) {
					throw new Error("programmatic value setter changed user validity");
				}
				globalThis.__gossamerFormSubmitted = 0;
				globalThis.__gossamerFormCanceled = 0;
				globalThis.__gossamerFormDefaultStates = [];
				form.addEventListener("submit", event => {
					__gossamerFormSubmitted++;
					__gossamerFormDefaultStates.push(event.defaultPrevented);
				});
				form.addEventListener("submit", event => {
					if (!event.bubbles || !event.cancelable || event.submitter !== button) {
						throw new Error("submit event shape failed");
					}
					__gossamerFormCanceled++;
					event.preventDefault();
				}, {once: true});
				form.requestSubmit(button);
				if (__gossamerFormSubmitted !== 1) throw new Error("cancelable submit event did not run");
				if (!query.matches(":user-valid") || query.matches(":user-invalid")) {
					throw new Error("successful requestSubmit did not expose :user-valid");
				}
			})();
		`,
	})
	if err != nil {
		t.Fatalf("QueueScript form API: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("run form API assertions: %v", err)
	}
	if page.Navigation().ID != initialNavigation {
		t.Fatal("invalid or canceled V8 requestSubmit navigated")
	}
	for page.Realm.Tasks.Len() != 0 {
		if err := page.Realm.RunOne(context.Background()); err != nil {
			t.Fatalf("drain canceled submission paint: %v", err)
		}
	}

	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/form-start/submit.js",
		Source: `
			const form = document.getElementById("search");
			form.requestSubmit(document.getElementById("go"));
			if (__gossamerFormSubmitted !== 2 || __gossamerFormCanceled !== 1 || __gossamerFormDefaultStates[1]) throw new Error("successful submit event did not run");
		`,
	})
	if err != nil {
		t.Fatalf("QueueScript submit: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("run successful requestSubmit: %v", err)
	}
	navigation := page.Navigation().ID
	if navigation == initialNavigation {
		t.Fatal("successful V8 requestSubmit did not schedule navigation")
	}
	if err := page.WaitNavigation(ctx, navigation); err != nil {
		t.Fatalf("WaitNavigation: %v", err)
	}
	if got := page.URL().String(); got != "https://gossamer.test/results?q=gossamer&tag=memory&tag=regions&commit=yes" {
		t.Fatalf("form navigation URL = %q", got)
	}
	if !v8FrameContainsText(page.Frame(), "submitted result") {
		t.Fatal("form navigation response did not reach paint")
	}
	if err := oldRealm.Evaluate(nil, browser.ScriptSource{URL: "old-realm.js", Source: `1`}); err != ErrRealmClosed {
		t.Fatalf("old Realm Evaluate = %v, want %v", err, ErrRealmClosed)
	}
	if err := page.Close(); err != nil {
		t.Fatalf("Close page: %v", err)
	}
	if ledger := browserRuntime.Ledger().Stats(); ledger.LiveObjects != 0 || ledger.PersistentObjects != 0 {
		t.Fatalf("Milestone 25 teardown ownership = %#v", ledger)
	}
}

func TestStockV8ReactDOMCompatibilityGate(t *testing.T) {
	reactBundle, err := os.ReadFile("testdata/react-19.2.7.production.js")
	if err != nil {
		t.Fatalf("read React fixture: %v", err)
	}
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatalf("NewWithEngine: %v", err)
	}
	defer func() {
		if err := browserRuntime.Close(); err != nil {
			t.Errorf("Close browser: %v", err)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	page, err := browserRuntime.LoadPage(ctx, "https://gossamer.test/react-dom-gate", staticDocumentLoader{
		document: `<!doctype html><html><body><main id="root"></main></body></html>`,
	})
	if err != nil {
		t.Fatalf("LoadPage: %v", err)
	}
	realm, ok := engine.LatestRealm()
	if !ok {
		t.Fatal("stock V8 engine has no React DOM gate realm")
	}
	initialLiveNodes := page.Document().Store().LiveLen()
	runOne := func(label string) {
		t.Helper()
		if err := page.Realm.RunOne(context.Background()); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
	}
	queueScript := func(label, source string) {
		t.Helper()
		if _, err := page.QueueScript(browser.ScriptSource{
			URL:    "https://gossamer.test/react-dom-gate/" + label + ".js",
			Source: source,
		}); err != nil {
			t.Fatalf("queue %s: %v", label, err)
		}
		runOne(label)
	}
	drain := func(label string) {
		t.Helper()
		for attempt := 0; attempt < 64 && page.Realm.Tasks.Len() != 0; attempt++ {
			runOne(label)
		}
		if page.Realm.Tasks.Len() != 0 {
			t.Fatalf("%s: task queue did not drain", label)
		}
	}

	queueScript("react", string(reactBundle))
	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage after React load: %v", err)
	}
	frameworkBaselineLiveNodes := page.Document().Store().LiveLen()
	if frameworkBaselineLiveNodes > initialLiveNodes+1 {
		t.Fatalf("React bundle retained %d probe nodes, want at most one", frameworkBaselineLiveNodes-initialLiveNodes)
	}

	queueScript("mount", `
		(() => {
			const h = React.createElement;
			const rootElement = document.getElementById("root");
			globalThis.__reactGateRecords = [];
			globalThis.__reactGateObserver = new MutationObserver(records => {
				for (const record of records) __reactGateRecords.push(record.type + ":" + record.target.nodeName);
			});
			__reactGateObserver.observe(rootElement, {
				childList: true,
				subtree: true,
				attributes: true,
				characterData: true,
			});
			function App() {
				const [model, setModel] = React.useState({
					name: "Ada",
					notes: "hello",
					enabled: false,
					choice: "alpha",
					pick: "one",
					order: ["a", "b", "c"],
					tick: 0,
				});
				globalThis.__reactGateUpdate = update => ReactDOM.flushSync(() => {
					setModel(current => typeof update === "function" ? update(current) : update);
				});
				return h("form", {id: "gate-form"},
					h("input", {
						id: "gate-name", name: "name", value: model.name,
						onInput: event => __reactGateUpdate(current => ({...current, name: event.currentTarget.value})),
					}),
					h("textarea", {
						id: "gate-notes", name: "notes", value: model.notes,
						onInput: event => __reactGateUpdate(current => ({...current, notes: event.currentTarget.value})),
					}),
					h("input", {
						id: "gate-enabled", name: "enabled", type: "checkbox", checked: model.enabled,
						onChange: event => __reactGateUpdate(current => ({...current, enabled: event.currentTarget.checked})),
					}),
					h("input", {
						id: "gate-alpha", name: "choice", type: "radio", value: "alpha",
						checked: model.choice === "alpha",
						onChange: event => event.currentTarget.checked && __reactGateUpdate(current => ({...current, choice: "alpha"})),
					}),
					h("input", {
						id: "gate-beta", name: "choice", type: "radio", value: "beta",
						checked: model.choice === "beta",
						onChange: event => event.currentTarget.checked && __reactGateUpdate(current => ({...current, choice: "beta"})),
					}),
					h("select", {
						id: "gate-pick", name: "pick", value: model.pick,
						onChange: event => __reactGateUpdate(current => ({...current, pick: event.currentTarget.value})),
					},
						h("option", {value: "one"}, "One"),
						h("option", {value: "two"}, "Two")),
					h("output", {id: "gate-state"},
						[model.name, model.notes, model.enabled, model.choice, model.pick].join("|")),
					h("ul", {id: "gate-list"}, model.order.map(key =>
						h("li", {id: "gate-item-" + key, key}, key))),
					h("span", {id: "gate-ephemeral", key: "tick-" + model.tick}, String(model.tick))
				);
			}
			globalThis.__reactGateErrors = [];
			globalThis.__reactGateRoot = ReactDOM.createRoot(rootElement, {
				onUncaughtError: error => __reactGateErrors.push(String(error)),
				onRecoverableError: error => __reactGateErrors.push(String(error)),
			});
			ReactDOM.flushSync(() => __reactGateRoot.render(h(App)));
			if (__reactGateErrors.length || document.getElementById("gate-state").textContent !== "Ada|hello|false|alpha|one" ||
				document.getElementById("gate-form").elements.length !== 6) {
				throw new Error("initial React DOM compatibility render failed: " + __reactGateErrors.join(";"));
			}
			globalThis.__reactGateA = document.getElementById("gate-item-a");
			globalThis.__reactGateB = document.getElementById("gate-item-b");
			globalThis.__reactGateC = document.getElementById("gate-item-c");
		})();
	`)
	drain("initial React compatibility render")

	queueScript("selection", `
		document.getElementById("gate-name").setSelectionRange(3, 3);
		document.getElementById("gate-notes").setSelectionRange(5, 5);
	`)
	for _, input := range []struct {
		id   string
		data string
	}{
		{id: "gate-name", data: "!"},
		{id: "gate-notes", data: "!"},
	} {
		node, found := page.Document().ElementByID(input.id)
		if !found {
			t.Fatalf("React controlled field %s is missing", input.id)
		}
		if _, err := page.QueueInputEvent(browser.InputEvent{
			Type:      browser.InputInput,
			Target:    browser.NodeHandle{Document: page.DocumentGeneration(), Node: node},
			Data:      input.data,
			InputType: "insertText",
		}); err != nil {
			t.Fatalf("queue React input for %s: %v", input.id, err)
		}
		runOne("dispatch React input " + input.id)
	}

	for _, id := range []string{"gate-enabled", "gate-beta"} {
		node, found := page.Document().ElementByID(id)
		if !found {
			t.Fatalf("React click control %s is missing", id)
		}
		if _, err := page.QueueInputEvent(browser.InputEvent{
			Type:   browser.InputClick,
			Target: browser.NodeHandle{Document: page.DocumentGeneration(), Node: node},
		}); err != nil {
			t.Fatalf("queue React click for %s: %v", id, err)
		}
		runOne("dispatch React click " + id)
	}

	queueScript("select-value", `document.getElementById("gate-pick").value = "two";`)
	selectID, found := page.Document().ElementByID("gate-pick")
	if !found {
		t.Fatal("React controlled select is missing")
	}
	if _, err := page.QueueInputEvent(browser.InputEvent{
		Type:   browser.InputChange,
		Target: browser.NodeHandle{Document: page.DocumentGeneration(), Node: selectID},
	}); err != nil {
		t.Fatalf("queue React select change: %v", err)
	}
	runOne("dispatch React select change")

	queueScript("controlled-assert", `
		if (__reactGateErrors.length ||
			document.getElementById("gate-state").textContent !== "Ada!|hello!|true|beta|two" ||
			document.getElementById("gate-name").value !== "Ada!" ||
			document.getElementById("gate-notes").value !== "hello!" ||
			!document.getElementById("gate-enabled").checked ||
			!document.getElementById("gate-beta").checked ||
			document.getElementById("gate-alpha").checked ||
			document.getElementById("gate-pick").value !== "two") {
			throw new Error("controlled React form state diverged: " + document.getElementById("gate-state").textContent + ":" + __reactGateErrors.join(";"));
		}
		if (__reactGateRecords.length === 0) throw new Error("MutationObserver saw no React commits");
		__reactGateRecords.length = 0;
	`)

	queueScript("keyed-reorder", `
		__reactGateUpdate(current => ({...current, order: ["c", "a", "d"], tick: current.tick + 1}));
		const list = document.getElementById("gate-list");
		if (Array.from(list.children).map(node => node.id).join(",") !== "gate-item-c,gate-item-a,gate-item-d" ||
			document.getElementById("gate-item-c") !== __reactGateC ||
			document.getElementById("gate-item-a") !== __reactGateA ||
			__reactGateB.parentNode !== null || document.getElementById("gate-item-b") !== null) {
			throw new Error("keyed React reconciliation lost native identity");
		}
	`)
	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage with held detached keyed node: %v", err)
	}
	queueScript("churn", `
		if (__reactGateB.textContent !== "b") throw new Error("held keyed wrapper lost detached native state");
		globalThis.__reactGateB = undefined;
		for (let tick = 2; tick <= 40; tick++) {
			__reactGateUpdate(current => ({
				...current,
				tick,
				order: tick % 2 === 0 ? ["a", "d", "c"] : ["c", "a", "d"],
			}));
		}
		if (document.getElementById("gate-ephemeral").textContent !== "40" || __reactGateErrors.length) {
			throw new Error("React churn failed: " + __reactGateErrors.join(";"));
		}
	`)
	drain("React compatibility churn")
	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage after React churn: %v", err)
	}

	queueScript("unmount", `
		__reactGateObserver.disconnect();
		globalThis.__reactGateObserver = undefined;
		globalThis.__reactGateRecords = undefined;
		globalThis.__reactGateUpdate = undefined;
		globalThis.__reactGateA = undefined;
		globalThis.__reactGateC = undefined;
		ReactDOM.flushSync(() => __reactGateRoot.unmount());
		globalThis.__reactGateRoot = undefined;
		if (document.getElementById("root").childNodes.length !== 0) {
			throw new Error("React compatibility unmount left native children connected");
		}
	`)
	drain("React compatibility unmount")
	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage after React compatibility unmount: %v", err)
	}
	// React retains one alternate component graph behind its delegated root
	// surface after unmount. The important churn invariant is that forty keyed
	// replacements remain bounded to that one graph; Realm teardown below is
	// the hard zero-ownership boundary.
	if got := page.Document().Store().LiveLen(); got > frameworkBaselineLiveNodes+24 {
		t.Fatalf("React compatibility gate retained detached nodes: got %d, framework baseline %d", got, frameworkBaselineLiveNodes)
	}
	if profile, err := realm.Profile(); err != nil {
		t.Fatalf("Profile after React compatibility unmount: %v", err)
	} else if profile.LiveWrappers > 12 || profile.LiveCallbacks != 0 || profile.EventListeners == 0 {
		t.Fatalf("React compatibility unmount retained unexpected V8 state: %#v", profile)
	}
	if err := page.Close(); err != nil {
		t.Fatalf("Close React compatibility page: %v", err)
	}
	if _, live := engine.LatestRealm(); live {
		t.Fatal("React compatibility Realm remained registered after teardown")
	}
	if ledger := browserRuntime.Ledger().Stats(); ledger.LiveObjects != 0 || ledger.PersistentObjects != 0 {
		t.Fatalf("Milestone 14 teardown ownership = %#v", ledger)
	}
}

func TestStockV8RangeSelectionAndMutationAwareTraversal(t *testing.T) {
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatalf("NewWithEngine: %v", err)
	}
	defer func() {
		if err := browserRuntime.Close(); err != nil {
			t.Errorf("Close browser: %v", err)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page, err := browserRuntime.LoadPage(ctx, "https://gossamer.test/range-selection", staticDocumentLoader{
		document: `<!doctype html><html><body><div id="fixture"></div></body></html>`,
	})
	if err != nil {
		t.Fatalf("LoadPage: %v", err)
	}
	realm, ok := engine.LatestRealm()
	if !ok {
		t.Fatal("stock V8 engine has no Range/Selection realm")
	}
	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/range-selection/assert.js",
		Source: `
			(() => {
				const fixture = document.getElementById("fixture");
				fixture.innerHTML = '<div id="range-root"><p id="left">ab<strong id="strong">cd</strong></p><p id="right"><i id="italic">ef</i>gh</p></div>';
				const rangeRoot = document.getElementById("range-root");
				const left = document.getElementById("left");
				const right = document.getElementById("right");
				const strong = document.getElementById("strong");
				const range = document.createRange();
				range.setStart(left.firstChild, 1);
				range.setEnd(right.lastChild, 1);
				const cloned = range.cloneContents();
				if (cloned.children.length !== 2 || cloned.textContent !== "bcdefg" ||
					cloned.firstElementChild.localName !== "p" ||
					cloned.firstElementChild.children[0].localName !== "strong" ||
					cloned.firstElementChild.children[0] === strong ||
					range.commonAncestorContainer !== rangeRoot) {
					throw new Error("cross-container cloneContents failed");
				}

				const extractRoot = rangeRoot.cloneNode(true);
				fixture.append(extractRoot);
				const movedStrong = extractRoot.children[0].children[0];
				const movedItalic = extractRoot.children[1].children[0];
				const extractRange = document.createRange();
				extractRange.setStart(extractRoot.children[0].firstChild, 1);
				extractRange.setEnd(extractRoot.children[1].lastChild, 1);
				const extracted = extractRange.extractContents();
				if (extracted.textContent !== "bcdefg" ||
					extracted.children[0].children[0] !== movedStrong ||
					extracted.children[1].children[0] !== movedItalic ||
					extractRoot.textContent !== "ah" || !extractRange.collapsed) {
					throw new Error("cross-container extractContents lost structure or identity: " +
						extracted.textContent + ":" +
						(extracted.children[0].children[0] === movedStrong) + ":" +
						(extracted.children[1].children[0] === movedItalic) + ":" +
						extractRoot.textContent + ":" + extractRange.collapsed);
				}

				const insertionHost = document.createElement("div");
				const insertionText = document.createTextNode("A😀B");
				insertionHost.append(insertionText);
				const insertionRange = document.createRange();
				insertionRange.setStart(insertionText, 3);
				insertionRange.collapse(true);
				const mark = document.createElement("mark");
				insertionRange.insertNode(mark);
				if (insertionHost.childNodes.length !== 3 || insertionHost.childNodes[0].data !== "A😀" ||
					insertionHost.childNodes[1] !== mark || insertionHost.childNodes[2].data !== "B") {
					throw new Error("Range.insertNode did not split a UTF-16 text boundary");
				}

				const selection = getSelection();
				if (!(selection instanceof Selection) || selection !== document.getSelection() ||
					selection.rangeCount !== 0 || selection.type !== "None") {
					throw new Error("canonical Selection facade failed");
				}
				const selectionRoot = document.createElement("div");
				selectionRoot.innerHTML = "<span>start</span><b>end</b>";
				const selectionRange = document.createRange();
				selectionRange.setStart(selectionRoot.firstElementChild.firstChild, 1);
				selectionRange.setEnd(selectionRoot.lastElementChild.firstChild, 2);
				selection.addRange(selectionRange);
				if (selection.rangeCount !== 1 || selection.getRangeAt(0) !== selectionRange ||
					selection.anchorNode !== selectionRoot.firstElementChild.firstChild ||
					selection.anchorOffset !== 1 || selection.focusOffset !== 2 ||
					selection.toString() !== "tarten" || selection.type !== "Range") {
					throw new Error("Selection Range projection failed: " + selection.toString());
				}
				let boundsFailure = false;
				try { selection.getRangeAt(1); } catch (error) {
					boundsFailure = error instanceof DOMException && error.name === "IndexSizeError";
				}
				if (!boundsFailure) throw new Error("Selection.getRangeAt bounds were not typed");
				selection.collapse(selectionRoot.firstElementChild.firstChild, 2);
				if (!selection.isCollapsed || selection.type !== "Caret" || selection.anchorOffset !== 2) {
					throw new Error("Selection.collapse failed");
				}
				selection.selectAllChildren(selectionRoot);
				if (selection.toString() !== "startend") throw new Error("Selection.selectAllChildren failed");

				const deleteHost = document.createElement("div");
				const deleteText = document.createTextNode("A😀BC");
				deleteHost.append(deleteText);
				const deleteRange = document.createRange();
				deleteRange.setStart(deleteText, 1);
				deleteRange.setEnd(deleteText, 3);
				selection.removeAllRanges();
				selection.addRange(deleteRange);
				if (selection.toString() !== "😀") throw new Error("Selection UTF-16 text failed");
				selection.deleteFromDocument();
				if (deleteText.data !== "ABC" || !selection.isCollapsed) {
					throw new Error("Selection.deleteFromDocument failed");
				}

				const traversalRoot = document.createElement("div");
				traversalRoot.innerHTML = '<section id="reject"><b id="pruned">pruned</b></section><section id="skip"><i id="lifted">lifted</i></section><p id="keep">keep</p>';
				const filter = {
					acceptNode(node) {
						if (node.id === "reject") return NodeFilter.FILTER_REJECT;
						if (node.id === "skip") return NodeFilter.FILTER_SKIP;
						return NodeFilter.FILTER_ACCEPT;
					}
				};
				const walker = document.createTreeWalker(traversalRoot, NodeFilter.SHOW_ELEMENT, filter);
				if (walker.firstChild().id !== "lifted" || walker.nextSibling().id !== "keep" ||
					document.getElementById("pruned") !== null) {
					throw new Error("TreeWalker reject/skip logical tree failed");
				}
				walker.currentNode = traversalRoot;
				if (walker.nextNode().id !== "lifted") throw new Error("TreeWalker initial order failed");
				const late = document.createElement("aside");
				late.id = "late";
				traversalRoot.append(late);
				if (walker.nextNode().id !== "keep") throw new Error("TreeWalker lost cursor after insertion");
				traversalRoot.querySelector("#keep").remove();
				if (walker.nextNode() !== late) throw new Error("TreeWalker did not adjust a removed cursor");
				const iterator = document.createNodeIterator(traversalRoot, NodeFilter.SHOW_ELEMENT, filter);
				if (iterator.nextNode() !== traversalRoot || iterator.nextNode().id !== "pruned" ||
					iterator.nextNode().id !== "lifted" ||
					iterator.nextNode() !== late || iterator.nextNode() !== null) {
					throw new Error("mutation-aware NodeIterator order failed");
				}
				const textIterator = document.createNodeIterator(traversalRoot, NodeFilter.SHOW_TEXT);
				const firstText = textIterator.nextNode();
				if (!firstText || firstText.nodeType !== 3) {
					throw new Error("NodeIterator incorrectly exposed a filtered root");
				}

				const heldRoot = document.createElement("div");
				heldRoot.textContent = "held";
				const heldRange = document.createRange();
				heldRange.selectNodeContents(heldRoot);
				selection.removeAllRanges();
				selection.addRange(heldRange);
				globalThis.__milestone15Selection = selection;
			})();
		`,
	})
	if err != nil {
		t.Fatalf("QueueScript milestone 15 assertions: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("run milestone 15 assertions: %v", err)
	}
	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage with Selection-only detached root: %v", err)
	}
	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/range-selection/gc.js",
		Source: `
			const selection = getSelection();
			if (selection !== __milestone15Selection || selection.toString() !== "held" ||
				selection.anchorNode.parentNode.textContent !== "held") {
				throw new Error("Selection did not retain its detached Range boundary across GC");
			}
			selection.removeAllRanges();
			globalThis.__milestone15Selection = undefined;
		`,
	})
	if err != nil {
		t.Fatalf("QueueScript milestone 15 GC assertions: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("run milestone 15 GC assertions: %v", err)
	}
	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage after Selection release: %v", err)
	}
	if err := page.Close(); err != nil {
		t.Fatalf("Close milestone 15 page: %v", err)
	}
	if ledger := browserRuntime.Ledger().Stats(); ledger.LiveObjects != 0 || ledger.PersistentObjects != 0 {
		t.Fatalf("Milestone 15 teardown ownership = %#v", ledger)
	}
}

func newTestEngine(t *testing.T) *Engine {
	t.Helper()
	icuData := os.Getenv("GOSSAMER_V8_ICU_DATA")
	if icuData == "" {
		t.Skip("GOSSAMER_V8_ICU_DATA is not set; run tools/v8/test.sh")
	}
	engine, err := New(Config{ICUDataPath: icuData, SamplingInterval: 1024})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if engine.Version() == "" {
		t.Fatal("empty V8 version")
	}
	return engine
}

func evaluate(t *testing.T, realm *Realm, sourceURL, source string) {
	t.Helper()
	if err := realm.Evaluate(nil, browser.ScriptSource{URL: sourceURL, Source: source}); err != nil {
		t.Fatalf("Evaluate %s: %v", sourceURL, err)
	}
}

type staticDocumentLoader struct {
	document string
}

func findV8BoxForNode(box *render.Box, node *dom.Node) *render.Box {
	if box == nil {
		return nil
	}
	if box.Node == node {
		return box
	}
	for _, child := range box.Children {
		if found := findV8BoxForNode(child, node); found != nil {
			return found
		}
	}
	return nil
}

func v8FrameContainsText(frame *render.Frame, text string) bool {
	if frame == nil {
		return false
	}
	for _, command := range frame.DisplayList.Commands {
		if command.Kind == render.DrawTextCommand && strings.Contains(command.Text, text) {
			return true
		}
	}
	return false
}

type capturingBindingHost struct {
	nextTimer      browser.TimerID
	microtasks     []browser.ValueHandle
	timerCallbacks []browser.ValueHandle
	timerDelay     time.Duration
	clearedTimer   browser.TimerID
}

func (*capturingBindingHost) GetElementByID(string) (browser.NodeHandle, bool, error) {
	return browser.NodeHandle{}, false, nil
}

func (*capturingBindingHost) CreateElement(string) (browser.NodeHandle, error) {
	return browser.NodeHandle{}, nil
}

func (*capturingBindingHost) CreateTextNode(string) (browser.NodeHandle, error) {
	return browser.NodeHandle{}, nil
}

func (*capturingBindingHost) TextContent(browser.NodeHandle) (string, error) {
	return "", nil
}

func (*capturingBindingHost) SetTextContent(browser.NodeHandle, string) error {
	return nil
}

func (*capturingBindingHost) AppendChild(browser.NodeHandle, browser.NodeHandle) error {
	return nil
}

func (*capturingBindingHost) InsertBefore(browser.NodeHandle, browser.NodeHandle, browser.NodeHandle) error {
	return nil
}

func (*capturingBindingHost) RemoveChild(browser.NodeHandle, browser.NodeHandle) error {
	return nil
}

func (*capturingBindingHost) GetAttribute(browser.NodeHandle, string) (string, bool, error) {
	return "", false, nil
}

func (*capturingBindingHost) SetAttribute(browser.NodeHandle, string, string) error {
	return nil
}

func (*capturingBindingHost) RemoveAttribute(browser.NodeHandle, string) error {
	return nil
}

func (*capturingBindingHost) AttributeNames(browser.NodeHandle) ([]string, error) {
	return nil, nil
}

func (*capturingBindingHost) QuerySelector(browser.NodeHandle, string, bool) ([]browser.NodeHandle, error) {
	return nil, nil
}

func (*capturingBindingHost) MatchesSelector(browser.NodeHandle, string) (bool, error) {
	return false, nil
}

func (*capturingBindingHost) ClosestSelector(browser.NodeHandle, string) (browser.NodeHandle, bool, error) {
	return browser.NodeHandle{}, false, nil
}

func (*capturingBindingHost) CloneNode(browser.NodeHandle, bool) (browser.NodeHandle, error) {
	return browser.NodeHandle{}, nil
}

func (*capturingBindingHost) InnerHTML(browser.NodeHandle) (string, error) {
	return "", nil
}

func (*capturingBindingHost) SetInnerHTML(browser.NodeHandle, string) error {
	return nil
}

func (*capturingBindingHost) InsertAdjacentHTML(browser.NodeHandle, string, string) error {
	return nil
}

func (*capturingBindingHost) FormValue(browser.NodeHandle) (string, error) {
	return "", nil
}

func (*capturingBindingHost) SetFormValue(browser.NodeHandle, string) error {
	return nil
}

func (*capturingBindingHost) FormChecked(browser.NodeHandle) (bool, error) {
	return false, nil
}

func (*capturingBindingHost) SetFormChecked(browser.NodeHandle, bool) error {
	return nil
}

func (*capturingBindingHost) FormIndeterminate(browser.NodeHandle) (bool, error) {
	return false, nil
}

func (*capturingBindingHost) SetFormIndeterminate(browser.NodeHandle, bool) error {
	return nil
}

func (*capturingBindingHost) Focus(browser.NodeHandle) error { return nil }

func (*capturingBindingHost) Blur(browser.NodeHandle) error { return nil }

func (*capturingBindingHost) ActiveElement() (browser.NodeHandle, bool, error) {
	return browser.NodeHandle{}, false, nil
}

func (*capturingBindingHost) Text(browser.NodeHandle) (string, error) {
	return "", nil
}

func (*capturingBindingHost) SetText(browser.NodeHandle, string) error {
	return nil
}

func (*capturingBindingHost) QueueCallback(browser.ValueHandle) error {
	return nil
}

func (host *capturingBindingHost) QueueMicrotask(callback browser.ValueHandle) error {
	host.microtasks = append(host.microtasks, callback)
	return nil
}

func (host *capturingBindingHost) SetTimeout(callback browser.ValueHandle, delay time.Duration) (browser.TimerID, error) {
	host.nextTimer++
	host.timerCallbacks = append(host.timerCallbacks, callback)
	host.timerDelay = delay
	return host.nextTimer, nil
}

func (host *capturingBindingHost) ClearTimeout(timer browser.TimerID) error {
	host.clearedTimer = timer
	return nil
}

func (fixture staticDocumentLoader) Load(_ context.Context, rawURL string) (*loader.Response, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	return &loader.Response{
		URL:        parsed,
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
		Body:       io.NopCloser(strings.NewReader(fixture.document)),
	}, nil
}
