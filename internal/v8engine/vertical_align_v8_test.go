//go:build v8 && cgo && darwin && arm64

package v8engine

import (
	"context"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
)

func TestStockV8VerticalAlignComputedStyleAndGeometryAreLive(t *testing.T) {
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatalf("NewWithEngine: %v", err)
	}
	defer browserRuntime.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page, err := browserRuntime.LoadPage(ctx, "https://gossamer.test/vertical-align", staticDocumentLoader{
		document: `<!doctype html><html><body style="margin:0"><div style="font:20px/40px monospace"><span>strut</span><span id="target" style="display:inline-block;width:10px;height:10px;vertical-align:top"></span></div></body></html>`,
	})
	if err != nil {
		t.Fatalf("LoadPage: %v", err)
	}

	if _, err := page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/vertical-align.js",
		Source: `
			(() => {
				const target = document.getElementById("target");
				const retained = getComputedStyle(target);
				if (retained.verticalAlign !== "top" || retained.getPropertyValue("vertical-align") !== "top") {
					throw new Error("initial vertical-align = " + retained.verticalAlign);
				}
				const top = target.getBoundingClientRect();
				target.style.verticalAlign = "bottom";
				if (retained.verticalAlign !== "bottom") {
					throw new Error("retained vertical-align = " + retained.verticalAlign);
				}
				const bottom = target.getBoundingClientRect();
				if (!(bottom.y > top.y)) {
					throw new Error("bottom alignment did not move the atomic box: " + top.y + " -> " + bottom.y);
				}
			})();
		`,
	}); err != nil {
		t.Fatalf("QueueScript: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("run vertical-align script: %v", err)
	}
}
