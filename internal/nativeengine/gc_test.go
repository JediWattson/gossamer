package nativeengine

import (
	"context"
	"errors"
	"net/url"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func TestCheckpointHeapInvariantVerificationIsDisabledByDefault(t *testing.T) {
	realm, page := newCheckpointCollectionRealm(t, Config{})

	var scans int
	realm.heapInvariantVerifier = func(*memory.Store) error {
		scans++
		return errors.New("unexpected heap scan")
	}
	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage() = %v", err)
	}
	if scans != 0 {
		t.Fatalf("default checkpoint collection ran %d full heap scans", scans)
	}
	if err := realm.runtime.Store().CheckInvariants(); err != nil {
		t.Fatalf("default checkpoint collection left an invalid heap: %v", err)
	}
}

func TestCheckpointHeapInvariantVerificationReportsDiagnosticFailure(t *testing.T) {
	realm, page := newCheckpointCollectionRealm(t, Config{VerifyHeapInvariants: true})

	diagnosticErr := errors.New("diagnostic invariant failure")
	var scans int
	realm.heapInvariantVerifier = func(*memory.Store) error {
		scans++
		return diagnosticErr
	}
	err := realm.CollectGarbage(page)
	if !errors.Is(err, diagnosticErr) {
		t.Fatalf("CollectGarbage() = %v, want diagnostic failure", err)
	}
	if scans != 1 {
		t.Fatalf("enabled checkpoint collection ran %d full heap scans, want 1", scans)
	}
	if err := realm.runtime.Store().CheckInvariants(); err != nil {
		t.Fatalf("diagnostic failure changed the valid heap: %v", err)
	}
}

func TestCheckpointHeapInvariantVerificationAuditsValidHeap(t *testing.T) {
	realm, page := newCheckpointCollectionRealm(t, Config{VerifyHeapInvariants: true})

	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage() = %v", err)
	}
}

func TestDetachedCanvasFacadeKeepsOwningCanvasAliveAcrossGC(t *testing.T) {
	realm, page := newCheckpointCollectionRealm(t, Config{VerifyHeapInvariants: true})
	baselineNodes := page.Document().Store().LiveLen()

	if _, err := page.QueueScript(browser.ScriptSource{URL: "https://gossamer.test/facade-gc-1", Source: `
globalThis.holder = {
  ctx: (function () {
    const c = document.createElement('canvas');
    c.width = 64;
    c.__marker = 'alive';
    return c.getContext('2d');
  })(),
  computed: getComputedStyle(document.createElement('div')),
};
(function () {
  const dropped = document.createElement('canvas');
  dropped.getContext('2d');
})();
`}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("first CollectGarbage() = %v", err)
	}
	if got := page.Document().Store().LiveLen(); got != baselineNodes+2 {
		t.Fatalf("live nodes after first GC = %d, want %d (baseline %d + 2 retained facades)", got, baselineNodes+2, baselineNodes)
	}

	if _, err := page.QueueScript(browser.ScriptSource{URL: "https://gossamer.test/facade-gc-2", Source: `
globalThis.holder.ctx.font = '16px sans-serif';
if (!(globalThis.holder.ctx.measureText('hello').width > 0)) {
  throw new Error('measureText returned non-positive width');
}
if (globalThis.holder.ctx.canvas.__marker !== 'alive') {
  throw new Error('canvas wrapper lost expando across GC');
}
if (typeof globalThis.holder.computed.color !== 'string') {
  throw new Error('computed style lost backing element');
}
delete globalThis.holder;
`}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Run a follow-up no-op script task so no completed-task register still
	// points at the released globals before the second GC checkpoint.
	if _, err := page.QueueScript(browser.ScriptSource{URL: "https://gossamer.test/facade-gc-3", Source: `void 0;`}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("second CollectGarbage() = %v", err)
	}
	if got := len(realm.canvas2DStates); got != 0 {
		t.Fatalf("len(realm.canvas2DStates) = %d, want 0", got)
	}
	if got := page.Document().Store().LiveLen(); got != baselineNodes {
		t.Fatalf("live nodes after dropping facades = %d, want baseline %d", got, baselineNodes)
	}
}

func BenchmarkCheckpointHeapInvariantVerification(b *testing.B) {
	for _, verify := range []bool{false, true} {
		name := "disabled"
		if verify {
			name = "enabled"
		}
		b.Run(name, func(b *testing.B) {
			realm, page := newCheckpointCollectionRealm(b, Config{VerifyHeapInvariants: verify})

			b.ResetTimer()
			for range b.N {
				if err := realm.CollectGarbage(page); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func newCheckpointCollectionRealm(tb testing.TB, config Config) (*Realm, *browser.Page) {
	tb.Helper()
	engine := New(config)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() {
		if err := browserRuntime.Close(); err != nil {
			tb.Errorf("close browser: %v", err)
		}
	})
	location := &url.URL{Scheme: "https", Host: "gossamer.test", Path: "/gc-verification"}
	page, err := browserRuntime.NewPage(dom.NewDocument(), location)
	if err != nil {
		tb.Fatal(err)
	}
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `
let retainedForCollection = [];
for (let index = 0; index < 256; index++) {
  retainedForCollection.push({index, label: "retained-" + index});
}
`}); err != nil {
		tb.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		tb.Fatal(err)
	}
	realm, found := engine.LatestRealm()
	if !found {
		tb.Fatal("native Realm was not retained")
	}
	return realm, page
}
