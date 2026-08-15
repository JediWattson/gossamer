//go:build v8 && cgo && darwin && arm64

package v8engine

import (
	"context"
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
	if profile.WrappersCreated != 2 || profile.WrapperCacheHits == 0 || profile.EventListeners != 1 {
		t.Fatalf("wrapper profile after navigation = %#v", profile)
	}

	ledgerBeforeGC := browserRuntime.Ledger().Stats()
	if err := realm.CollectGarbage(); err != nil {
		t.Fatalf("CollectGarbage: %v", err)
	}
	if ledgerAfterGC := browserRuntime.Ledger().Stats(); ledgerAfterGC != ledgerBeforeGC {
		t.Fatalf("V8 wrapper collection changed Go ARC: before=%#v after=%#v", ledgerBeforeGC, ledgerAfterGC)
	}
	afterGC, err := realm.Profile()
	if err != nil {
		t.Fatalf("Profile after wrapper collection: %v", err)
	}
	if afterGC.WrappersCollected == 0 || afterGC.LiveWrappers != 1 {
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
	if afterDispatch.EventsDispatched != 1 || afterDispatch.CallbacksCreated != 1 || afterDispatch.LiveCallbacks != 1 {
		t.Fatalf("callback publication profile = %#v", afterDispatch)
	}
	if got, err := document.TextContent(counterID); err != nil || got != "0" {
		t.Fatalf("counter before callback = %q, %v; want 0", got, err)
	}

	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("invoke click listener: %v", err)
	}
	afterInvoke, err := realm.Profile()
	if err != nil {
		t.Fatalf("Profile after invoke: %v", err)
	}
	if afterInvoke.CallbacksInvoked != 1 || afterInvoke.LiveCallbacks != 0 {
		t.Fatalf("callback consumption profile = %#v", afterInvoke)
	}
	if got, err := document.TextContent(counterID); err != nil || got != "1" {
		t.Fatalf("counter after callback = %q, %v; want 1", got, err)
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

func (*capturingBindingHost) TextContent(browser.NodeHandle) (string, error) {
	return "", nil
}

func (*capturingBindingHost) SetTextContent(browser.NodeHandle, string) error {
	return nil
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
