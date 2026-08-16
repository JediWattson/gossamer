//go:build v8 && cgo && darwin && arm64

package v8engine

import (
	"context"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
)

func TestStockV8DocumentTitleFeedsLiveTabMetadata(t *testing.T) {
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
	page, err := browserRuntime.LoadPage(ctx, "https://gossamer.test/app/index.html", staticDocumentLoader{
		document: `<!doctype html><html><head><base href="/assets/"><link rel="icon" href="app.png"></head><body></body></html>`,
	})
	if err != nil {
		t.Fatalf("LoadPage: %v", err)
	}
	realm, ok := engine.LatestRealm()
	if !ok {
		t.Fatal("stock V8 engine has no presentation realm")
	}
	if _, err := page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/app/title.js",
		Source: `
			if (document.title !== "") throw new Error("unexpected initial title");
			document.title = "  Live   Gossamer App  ";
			if (document.title !== "Live Gossamer App") throw new Error("document.title is not live");
			const title = document.querySelector("title");
			if (!title || title.textContent !== "  Live   Gossamer App  ") throw new Error("title element was not created");
		`,
	}); err != nil {
		t.Fatalf("QueueScript: %v", err)
	}
	if err := page.Realm.RunOne(ctx); err != nil {
		t.Fatalf("run title script: %v", err)
	}
	metadata := page.Metadata()
	if metadata.Title != "Live Gossamer App" {
		t.Fatalf("tab metadata title = %q", metadata.Title)
	}
	if metadata.FaviconURL == nil || metadata.FaviconURL.String() != "https://gossamer.test/assets/app.png" {
		t.Fatalf("tab metadata favicon = %v", metadata.FaviconURL)
	}
	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage: %v", err)
	}
	if err := page.Close(); err != nil {
		t.Fatalf("Close page: %v", err)
	}
	if ledger := browserRuntime.Ledger().Stats(); ledger.LiveObjects != 0 || ledger.PersistentObjects != 0 {
		t.Fatalf("presentation teardown ownership = %#v", ledger)
	}
}
