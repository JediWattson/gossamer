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
	"github.com/JediWattson/gossamer/internal/loader"
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
