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
	if afterDispatch.EventsDispatched != 1 || afterDispatch.CallbacksCreated != 0 ||
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
					computed.width !== "25%" || computed.getPropertyValue("--accent") !== "ready" ||
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
		staticDocumentLoader{document: `<!doctype html><html><body><main id="scope"><section id="first" class="card"><span class="leaf"></span></section><section id="second" class="card"></section></main></body></html>`},
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
		staticDocumentLoader{document: `<!doctype html><html><body><input id="text" value="seed"><input id="check" type="checkbox" checked><input id="cancel" type="checkbox"></body></html>`},
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
				const valueDescriptor = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "value");
				const checkedDescriptor = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "checked");
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
				check.addEventListener("click", () => { __checkedDuringClick = check.checked; });
				cancel.addEventListener("click", event => {
					__checkedDuringCanceledClick = cancel.checked;
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
	generation := page.DocumentGeneration()
	for _, event := range []browser.InputEvent{
		{Type: browser.InputClick, Target: browser.NodeHandle{Document: generation, Node: checkID}},
		{Type: browser.InputClick, Target: browser.NodeHandle{Document: generation, Node: cancelID}},
		{Type: browser.InputInput, Target: browser.NodeHandle{Document: generation, Node: textID}, Data: "X", InputType: "insertText"},
	} {
		if _, err := page.QueueInputEvent(event); err != nil {
			t.Fatalf("QueueInputEvent(%s): %v", event.Type, err)
		}
	}
	for range 3 {
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
			if (document.getElementById("text").value !== "userX") {
				throw new Error("input event did not update current value");
			}
			if (document.activeElement !== document.getElementById("check")) {
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
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("dispatch React controlled input: %v", err)
	}
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
	if profile.EventsDispatched != 12 || profile.CallbacksCreated != 0 ||
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
	if afterRenderLedger.ObjectsCreated-baselineLedger.ObjectsCreated != createdNodes+2 ||
		afterRenderLedger.ObjectsDestroyed-baselineLedger.ObjectsDestroyed != 2 ||
		afterRenderLedger.LiveObjects-baselineLedger.LiveObjects != createdNodes ||
		afterRenderLedger.TaskLocalAllocations-baselineLedger.TaskLocalAllocations != createdNodes+1 ||
		afterRenderLedger.PublishOperations-baselineLedger.PublishOperations != 2 ||
		afterRenderLedger.TransferOperations-baselineLedger.TransferOperations != 2 ||
		afterRenderLedger.PersistentObjects-baselineLedger.PersistentObjects != createdNodes {
		t.Fatalf("churn crossed unexpected ARC boundaries: before=%#v after=%#v", baselineLedger, afterRenderLedger)
	}
	afterRenderProfile, err := realm.Profile()
	if err != nil {
		t.Fatalf("Profile after churn: %v", err)
	}
	const createdWrappers = 1 + 1 + iterations*5 + 1
	if got := afterRenderProfile.WrappersCreated - baselineProfile.WrappersCreated; got != createdWrappers {
		t.Fatalf("wrappers created by churn = %d, want %d", got, createdWrappers)
	}
	if afterRenderProfile.WrapperCacheHits-baselineProfile.WrapperCacheHits != 1 {
		t.Fatalf("wrapper cache hits after churn = %d, want 1", afterRenderProfile.WrapperCacheHits-baselineProfile.WrapperCacheHits)
	}

	ledgerBeforeGC := browserRuntime.Ledger().Stats()
	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage: %v", err)
	}
	ledgerAfterGC := browserRuntime.Ledger().Stats()
	const connectedCreatedNodes = 2
	const reclaimedDetachedNodes = createdNodes - connectedCreatedNodes
	if ledgerAfterGC.ObjectsDestroyed-ledgerBeforeGC.ObjectsDestroyed != reclaimedDetachedNodes ||
		ledgerAfterGC.LiveObjects-baselineLedger.LiveObjects != connectedCreatedNodes ||
		ledgerAfterGC.PersistentObjects-baselineLedger.PersistentObjects != connectedCreatedNodes ||
		document.Store().LiveLen()-initialLiveNodes != connectedCreatedNodes {
		t.Fatalf("detached-node collection boundary: before=%#v after=%#v liveNodes=%d",
			ledgerBeforeGC, ledgerAfterGC, document.Store().LiveLen()-initialLiveNodes)
	}
	afterGC, err := realm.Profile()
	if err != nil {
		t.Fatalf("Profile after churn GC: %v", err)
	}
	if afterGC.WrappersCollected-baselineProfile.WrappersCollected != createdWrappers-1 ||
		afterGC.LiveWrappers != baselineProfile.LiveWrappers+1 {
		t.Fatalf("wrapper reclamation after churn = %#v, baseline=%#v", afterGC, baselineProfile)
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
