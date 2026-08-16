//go:build v8 && cgo && darwin && arm64

package v8engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
)

func TestStockV8NavigationLifecycleCancellationAndOrdering(t *testing.T) {
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
	page, err := browserRuntime.LoadPage(ctx, "https://gossamer.test/lifecycle/start", staticDocumentLoader{
		document: `<!doctype html><html><body>departure</body></html>`,
	})
	if err != nil {
		t.Fatalf("LoadPage: %v", err)
	}
	oldRealm, ok := engine.LatestRealm()
	if !ok {
		t.Fatal("stock V8 engine has no departure realm")
	}
	generation := page.DocumentGeneration()
	if _, err := page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/lifecycle/setup.js",
		Source: `
			globalThis.__cancelDeparture = true;
			addEventListener("beforeunload", event => {
				if (__cancelDeparture) event.preventDefault();
			});
			addEventListener("pagehide", event => {
				const events = history.state?.events || [];
				events.push("pagehide:" + event.persisted);
				history.replaceState({events}, "", "");
			});
			addEventListener("unload", () => {
				const events = history.state?.events || [];
				events.push("unload");
				history.replaceState({events}, "", "");
			});
		`,
	}); err != nil {
		t.Fatalf("QueueScript setup: %v", err)
	}
	if err := page.Realm.RunOne(ctx); err != nil {
		t.Fatalf("run setup: %v", err)
	}

	destination := staticDocumentLoader{document: `<!doctype html><html><body>
		<script>globalThis.__arrivalEvents=[]; addEventListener("pageshow", event => __arrivalEvents.push("pageshow:" + event.persisted));</script>
		arrival
	</body></html>`}
	canceledNavigation, err := page.Navigate(ctx, "https://gossamer.test/lifecycle/next", destination)
	if err != nil {
		t.Fatalf("Navigate canceled candidate: %v", err)
	}
	if err := page.WaitNavigation(ctx, canceledNavigation); !errors.Is(err, browser.ErrNavigationCanceled) {
		t.Fatalf("canceled navigation error = %v", err)
	}
	if page.DocumentGeneration() != generation || page.URL().String() != "https://gossamer.test/lifecycle/start" {
		t.Fatalf("canceled navigation replaced page generation=%d URL=%q", page.DocumentGeneration(), page.URL())
	}
	if snapshot := page.SessionHistorySnapshot(); snapshot.Length != 1 || snapshot.StateJSON != "null" {
		t.Fatalf("canceled navigation history = %#v", snapshot)
	}
	if err := oldRealm.Evaluate(nil, browser.ScriptSource{URL: "still-live.js", Source: `globalThis.__cancelDeparture = false`}); err != nil {
		t.Fatalf("old Realm after cancellation: %v", err)
	}

	navigation, err := page.Navigate(ctx, "https://gossamer.test/lifecycle/next", destination)
	if err != nil {
		t.Fatalf("Navigate allowed candidate: %v", err)
	}
	if err := page.WaitNavigation(ctx, navigation); err != nil {
		t.Fatalf("allowed navigation: %v", err)
	}
	entries, current := page.History()
	if current != 1 || len(entries) != 2 || entries[0].StateJSON != `{"events":["pagehide:false","unload"]}` {
		t.Fatalf("departure lifecycle history = %#v current=%d", entries, current)
	}
	if err := oldRealm.Evaluate(nil, browser.ScriptSource{URL: "closed.js", Source: `1`}); !errors.Is(err, ErrRealmClosed) {
		t.Fatalf("departed Realm Evaluate = %v, want ErrRealmClosed", err)
	}
	if _, err := page.QueueScript(browser.ScriptSource{
		URL:    "https://gossamer.test/lifecycle/assert.js",
		Source: `if (__arrivalEvents.join("|") !== "pageshow:false") throw new Error("pageshow lifecycle mismatch: " + __arrivalEvents);`,
	}); err != nil {
		t.Fatalf("QueueScript arrival assertion: %v", err)
	}
	if err := page.Realm.RunOne(ctx); err != nil {
		t.Fatalf("run arrival assertion: %v", err)
	}
	if err := page.Close(); err != nil {
		t.Fatalf("Close page: %v", err)
	}
	if ledger := browserRuntime.Ledger().Stats(); ledger.LiveObjects != 0 || ledger.PersistentObjects != 0 {
		t.Fatalf("navigation lifecycle teardown ownership = %#v", ledger)
	}
}
