//go:build v8 && cgo && darwin && arm64

package v8engine

import (
	"context"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
)

func TestStockV8TableTrackMergingGeometryStaysLive(t *testing.T) {
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatalf("NewWithEngine: %v", err)
	}
	defer browserRuntime.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page, err := browserRuntime.LoadPage(ctx, "https://gossamer.test/table-track-merging", staticDocumentLoader{
		document: `<!doctype html><html><body style="margin:0"><table id=table style="border:10px solid #808080;border-spacing:20px"><col id=columns span=10 style="width:0"><tr><td id=first style="width:50px;height:50px;padding:0"></td><td id=second style="width:50px;height:50px;padding:0"></td></tr><tr><td style="width:50px;height:50px;padding:0"></td><td style="width:50px;height:50px;padding:0"></td></tr></table></body></html>`,
	})
	if err != nil {
		t.Fatalf("LoadPage: %v", err)
	}

	if _, err := page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/table-track-merging.js",
		Source: `
			(() => {
				const table = document.getElementById("table");
				const columns = document.getElementById("columns");
				const first = document.getElementById("first");
				const second = document.getElementById("second");
				const style = getComputedStyle(table);
				if (style.width !== "160px" || table.getBoundingClientRect().width !== 180 || first.getBoundingClientRect().width !== 50 || second.getBoundingClientRect().left - first.getBoundingClientRect().left !== 70) {
					throw new Error("initial merged tracks: " + [style.width, table.getBoundingClientRect().width, first.getBoundingClientRect().width, second.getBoundingClientRect().left]);
				}
				columns.style.width = "30px";
				if (columns.style.width !== "30px" || getComputedStyle(columns).width !== "520px" || style.width !== "560px" || table.getBoundingClientRect().width !== 580) {
					throw new Error("constrained tracks: " + [getComputedStyle(columns).width, style.width, table.getBoundingClientRect().width]);
				}
				columns.style.width = "0px";
				if (style.width !== "160px" || table.getBoundingClientRect().width !== 180) {
					throw new Error("remerged tracks: " + style.width);
				}
			})();
		`,
	}); err != nil {
		t.Fatalf("QueueScript: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("run table track merging script: %v", err)
	}
}
