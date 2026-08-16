//go:build v8 && cgo && darwin && arm64

package v8engine

import (
	"context"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
)

func TestStockV8SubgridComputedStyleAndGeometryStayLive(t *testing.T) {
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatalf("NewWithEngine: %v", err)
	}
	defer browserRuntime.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page, err := browserRuntime.LoadPage(ctx, "https://gossamer.test/subgrid", staticDocumentLoader{
		document: `<!doctype html><html><body style="margin:0"><section id="parent" style="display:grid;width:440px;grid-template-columns:[p1] 100px [p2] 100px [p3] 100px [p4] 100px [p5];column-gap:10px;grid-template-rows:20px"><div id="subgrid" style="display:grid;grid-column:1 / span 4;grid-template-columns:subgrid [a] repeat(auto-fill,[b]) [c];grid-template-rows:20px"><i id="first" style="grid-column:p1 / p2"></i><i id="last" style="grid-column:p4 / p5"></i></div></section></body></html>`,
	})
	if err != nil {
		t.Fatalf("LoadPage: %v", err)
	}

	if _, err := page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/subgrid.js",
		Source: `
			(() => {
				const parent = document.getElementById("parent");
				const subgrid = document.getElementById("subgrid");
				const first = document.getElementById("first");
				const last = document.getElementById("last");
				const retained = getComputedStyle(subgrid);
				const parentStyle = getComputedStyle(parent);
				if (retained.gridTemplateColumns !== "subgrid [a] [b] [b] [b] [c]" || retained.columnGap !== "normal" || last.getBoundingClientRect().left !== 330) {
					throw new Error("initial subgrid: " + [retained.gridTemplateColumns, retained.columnGap, last.getBoundingClientRect().left]);
				}
				subgrid.style.columnGap = "0";
				if (retained.columnGap !== "0px" || last.getBoundingClientRect().left !== 325 || last.getBoundingClientRect().width !== 105) {
					throw new Error("live subgrid gap: " + [retained.columnGap, last.getBoundingClientRect().left, last.getBoundingClientRect().width]);
				}
				parent.style.cssText = "display:grid;width:500px;grid-template-columns:auto auto;justify-content:start;grid-template-rows:20px";
				subgrid.style.cssText = "display:grid;grid-column:1/span 2;grid-template-columns:subgrid;grid-template-rows:20px";
				first.style.cssText = "grid-column:1;width:120px";
				last.style.cssText = "grid-column:2;width:40px";
				if (parentStyle.gridTemplateColumns !== "120px 40px" || last.getBoundingClientRect().left !== 120) {
					throw new Error("intrinsic subgrid tracks: " + [parentStyle.gridTemplateColumns, last.getBoundingClientRect().left]);
				}
				parent.style.display = "block";
				if (retained.gridTemplateColumns !== "none") {
					throw new Error("orphan subgrid fallback: " + retained.gridTemplateColumns);
				}
			})();
		`,
	}); err != nil {
		t.Fatalf("QueueScript: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("run subgrid script: %v", err)
	}
}
