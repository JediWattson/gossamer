//go:build v8 && cgo && darwin && arm64

package v8engine

import (
	"context"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
)

func TestStockV8JustifiedAtomicGeometryIsLive(t *testing.T) {
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatalf("NewWithEngine: %v", err)
	}
	defer browserRuntime.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page, err := browserRuntime.LoadPage(ctx, "https://gossamer.test/text-justify", staticDocumentLoader{
		document: `<!doctype html><html><body style="margin:0"><div id="container" style="width:60px;text-align:justify;font:10px monospace">a <span id="target" style="display:inline-block;width:10px;height:10px"></span> xxxxxxxx</div></body></html>`,
	})
	if err != nil {
		t.Fatalf("LoadPage: %v", err)
	}

	if _, err := page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/text-justify.js",
		Source: `
			(() => {
				const container = document.getElementById("container");
				const target = document.getElementById("target");
				const justified = target.getBoundingClientRect();
				if (justified.right !== 60) {
					throw new Error("justified atomic right edge = " + justified.right);
				}
				container.style.textAlign = "left";
				const left = target.getBoundingClientRect();
				if (!(left.x < justified.x)) {
					throw new Error("live left alignment did not move atomic box: " + left.x + " vs " + justified.x);
				}
				container.style.textAlign = "justify";
				if (target.getBoundingClientRect().right !== 60) {
					throw new Error("live re-justification failed");
				}
			})();
		`,
	}); err != nil {
		t.Fatalf("QueueScript: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("run justification script: %v", err)
	}
}
