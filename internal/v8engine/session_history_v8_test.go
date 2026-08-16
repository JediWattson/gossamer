//go:build v8 && cgo && darwin && arm64

package v8engine

import (
	"context"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
)

func TestStockV8SessionHistoryLocationAndWindowEvents(t *testing.T) {
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
	page, err := browserRuntime.LoadPage(
		ctx,
		"https://gossamer.test/history?base=1#start",
		staticDocumentLoader{document: `<!doctype html><html><body><main>history</main></body></html>`},
	)
	if err != nil {
		t.Fatalf("LoadPage: %v", err)
	}
	realm, ok := engine.LatestRealm()
	if !ok {
		t.Fatal("stock V8 engine has no history realm")
	}
	generation := page.DocumentGeneration()

	if _, err := page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/history/setup.js",
		Source: `
			if (history !== window.history || location !== window.location ||
				document.location !== location || !(history instanceof History) ||
				!(location instanceof Location)) {
				throw new Error("History/Location identity or prototype mismatch");
			}
			if (location.href !== "https://gossamer.test/history?base=1#start" ||
				location.origin !== "https://gossamer.test" ||
				location.protocol !== "https:" || location.host !== "gossamer.test" ||
				location.hostname !== "gossamer.test" || location.port !== "" ||
				location.pathname !== "/history" || location.search !== "?base=1" ||
				location.hash !== "#start" || String(location) !== location.href) {
				throw new Error("Location component mismatch");
			}
			let circular = {}; circular.self = circular;
			try { history.pushState(circular, "", ""); throw new Error("missing DataCloneError"); }
			catch (error) { if (error.name !== "DataCloneError") throw error; }
			try { history.pushState(null, "", "https://other.test/"); throw new Error("missing SecurityError"); }
			catch (error) { if (error.name !== "SecurityError") throw error; }
			globalThis.__historyEvents = [];
			addEventListener("popstate", event => {
				if (event.target !== window || event.currentTarget !== window) throw new Error("popstate Window target mismatch");
				__historyEvents.push("pop:" + event.state.step);
			});
			window.addEventListener("hashchange", event => {
				if (event.target !== window || event.currentTarget !== window) throw new Error("hashchange Window target mismatch");
				__historyEvents.push("hash:" + event.oldURL + ">" + event.newURL);
			});
			history.pushState({step: 1}, "", "?route=one#one");
			history.pushState({step: 2}, "", "#two");
			if (history.length !== 3 || history.state.step !== 2 ||
				location.href !== "https://gossamer.test/history?route=one#two") {
				throw new Error("pushState did not synchronously update live state");
			}
			history.back();
		`,
	}); err != nil {
		t.Fatalf("QueueScript setup: %v", err)
	}
	if err := page.Realm.RunOne(ctx); err != nil {
		t.Fatalf("run setup: %v", err)
	}
	navigation := page.Navigation().ID
	if navigation == 0 {
		t.Fatal("history.back did not schedule traversal")
	}
	if err := page.WaitNavigation(ctx, navigation); err != nil {
		t.Fatalf("WaitNavigation(back): %v", err)
	}
	if page.DocumentGeneration() != generation {
		t.Fatal("same-document V8 history traversal replaced the document generation")
	}

	if _, err := page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/history/assert.js",
		Source: `
			const expected = [
				"pop:1",
				"hash:https://gossamer.test/history?route=one#two>https://gossamer.test/history?route=one#one",
			];
			if (__historyEvents.length !== expected.length ||
				__historyEvents.some((value, index) => value !== expected[index])) {
				throw new Error("history event order mismatch: " + __historyEvents.join("|"));
			}
			if (history.state.step !== 1 || location.hash !== "#one" ||
				history.scrollRestoration !== "auto") {
				throw new Error("traversal did not restore live History/Location state");
			}
			history.scrollRestoration = "manual";
			if (history.scrollRestoration !== "manual") throw new Error("scrollRestoration setter failed");
		`,
	}); err != nil {
		t.Fatalf("QueueScript assertions: %v", err)
	}
	if err := page.Realm.RunOne(ctx); err != nil {
		t.Fatalf("run assertions: %v", err)
	}
	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage: %v", err)
	}
	if err := page.Close(); err != nil {
		t.Fatalf("Close page: %v", err)
	}
	if ledger := browserRuntime.Ledger().Stats(); ledger.LiveObjects != 0 || ledger.PersistentObjects != 0 {
		t.Fatalf("session-history teardown ownership = %#v", ledger)
	}
}
