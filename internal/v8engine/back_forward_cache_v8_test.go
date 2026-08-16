//go:build v8 && cgo && darwin && arm64

package v8engine

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/loader"
)

func TestStockV8BackForwardCacheRestoresRealmRegionAndPersistedEvents(t *testing.T) {
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
	client := &countingV8HistoryLoader{documents: map[string]string{
		"/a": `<!doctype html><html><body><main id="state">A</main></body></html>`,
		"/b": `<!doctype html><html><body><main>B</main></body></html>`,
	}}
	page, err := browserRuntime.LoadPage(ctx, "https://gossamer.test/a", client)
	if err != nil {
		t.Fatalf("LoadPage A: %v", err)
	}
	aRealm, ok := engine.LatestRealm()
	if !ok {
		t.Fatal("stock V8 engine has no A realm")
	}
	aGeneration := page.DocumentGeneration()
	if _, err := page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/a/cache.js",
		Source: `
			globalThis.__cacheToken = {identity: 42};
			globalThis.__cacheEvents = [];
			document.getElementById("state").textContent = "A mutated";
			addEventListener("pagehide", event => __cacheEvents.push("pagehide:" + event.persisted));
			addEventListener("pageshow", event => __cacheEvents.push("pageshow:" + event.persisted));
		`,
	}); err != nil {
		t.Fatalf("QueueScript A state: %v", err)
	}
	if err := page.Realm.RunOne(ctx); err != nil {
		t.Fatalf("run A state: %v", err)
	}

	navigation, err := page.Navigate(ctx, "https://gossamer.test/b", client)
	if err != nil {
		t.Fatalf("Navigate B: %v", err)
	}
	if err := page.WaitNavigation(ctx, navigation); err != nil {
		t.Fatalf("WaitNavigation B: %v", err)
	}
	if page.BackForwardCacheSize() != 1 {
		t.Fatalf("cache size after A -> B = %d", page.BackForwardCacheSize())
	}
	if err := aRealm.Evaluate(nil, browser.ScriptSource{
		URL:    "cached-a.js",
		Source: `if (__cacheToken.identity !== 42 || __cacheEvents.join("|") !== "pagehide:true") throw new Error("cached A realm state mismatch")`,
	}); err != nil {
		t.Fatalf("cached A Realm Evaluate: %v", err)
	}

	back, err := page.Back(ctx, nil)
	if err != nil {
		t.Fatalf("Back: %v", err)
	}
	if err := page.WaitNavigation(ctx, back); err != nil {
		t.Fatalf("WaitNavigation back: %v", err)
	}
	if page.DocumentGeneration() != aGeneration {
		t.Fatalf("restored A generation = %d, want %d", page.DocumentGeneration(), aGeneration)
	}
	if got := client.loadCount("/a"); got != 1 {
		t.Fatalf("A network loads = %d, want 1", got)
	}
	if _, err := page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/a/assert-cache.js",
		Source: `
			if (__cacheToken.identity !== 42 || document.getElementById("state").textContent !== "A mutated" ||
				__cacheEvents.join("|") !== "pagehide:true|pageshow:true") {
				throw new Error("restored A realm/DOM/event state mismatch: " + __cacheEvents.join("|"));
			}
		`,
	}); err != nil {
		t.Fatalf("QueueScript restored assertion: %v", err)
	}
	if err := page.Realm.RunOne(ctx); err != nil {
		t.Fatalf("run restored assertion: %v", err)
	}
	if err := page.Close(); err != nil {
		t.Fatalf("Close page: %v", err)
	}
	if profile := engine.Profile(); profile.RealmsCreated != profile.RealmsClosed {
		t.Fatalf("V8 realms after BFCache teardown created=%d closed=%d", profile.RealmsCreated, profile.RealmsClosed)
	}
	if ledger := browserRuntime.Ledger().Stats(); ledger.LiveObjects != 0 || ledger.PersistentObjects != 0 {
		t.Fatalf("BFCache V8 teardown ownership = %#v", ledger)
	}
}

type countingV8HistoryLoader struct {
	mutex     sync.Mutex
	documents map[string]string
	loads     map[string]int
}

func (client *countingV8HistoryLoader) Load(_ context.Context, rawURL string) (*loader.Response, error) {
	location, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	client.mutex.Lock()
	if client.loads == nil {
		client.loads = make(map[string]int)
	}
	client.loads[location.Path]++
	document := client.documents[location.Path]
	client.mutex.Unlock()
	return &loader.Response{
		URL: location, StatusCode: http.StatusOK,
		Header: http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
		Body:   io.NopCloser(strings.NewReader(document)),
	}, nil
}

func (client *countingV8HistoryLoader) loadCount(path string) int {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	return client.loads[path]
}
