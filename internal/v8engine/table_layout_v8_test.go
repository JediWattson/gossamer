//go:build v8 && cgo && darwin && arm64

package v8engine

import (
	"context"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
)

func TestStockV8TableDisplayAndGeometryAreLive(t *testing.T) {
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatalf("NewWithEngine: %v", err)
	}
	defer browserRuntime.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page, err := browserRuntime.LoadPage(ctx, "https://gossamer.test/table-layout", staticDocumentLoader{
		document: `<!doctype html><html><body style="margin:0"><table id="target" style="border-spacing:0;empty-cells:hide;table-layout:fixed;caption-side:bottom"><colgroup><col style="width:40px"><col id="second" style="width:60px"></colgroup><tbody><tr><td id="cell">A</td><td>B</td></tr></tbody></table></body></html>`,
	})
	if err != nil {
		t.Fatalf("LoadPage: %v", err)
	}

	if _, err := page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/table-layout.js",
		Source: `
			(() => {
				const table = document.getElementById("target");
				const cell = document.getElementById("cell");
				const second = document.getElementById("second");
				const retained = getComputedStyle(table);
				if (retained.display !== "table" || getComputedStyle(cell).display !== "table-cell") {
					throw new Error("table display roles: " + retained.display + " / " + getComputedStyle(cell).display);
				}
				if (retained.borderCollapse !== "separate" || retained.borderSpacing !== "0px" ||
					retained.emptyCells !== "hide" || retained.tableLayout !== "fixed" || retained.captionSide !== "bottom") {
					throw new Error("table computed properties: " + [retained.borderCollapse, retained.borderSpacing, retained.emptyCells, retained.tableLayout, retained.captionSide]);
				}
				if (retained.width !== "100px" || table.getBoundingClientRect().width !== 100) {
					throw new Error("initial table width: " + retained.width + " / " + table.getBoundingClientRect().width);
				}
				second.style.width = "80px";
				if (retained.width !== "120px" || table.getBoundingClientRect().width !== 120) {
					throw new Error("live table width: " + retained.width + " / " + table.getBoundingClientRect().width);
				}
				table.style.borderCollapse = "collapse";
				if (retained.borderCollapse !== "collapse" || retained.getPropertyValue("border-collapse") !== "collapse") {
					throw new Error("live collapsed border: " + retained.borderCollapse);
				}
				table.style.borderCollapse = "separate";
				table.style.borderSpacing = "4px 6px";
				if (retained.borderSpacing !== "4px 6px" || retained.getPropertyValue("border-spacing") !== "4px 6px") {
					throw new Error("live border spacing: " + retained.borderSpacing);
				}
			})();
		`,
	}); err != nil {
		t.Fatalf("QueueScript: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("run table script: %v", err)
	}
}
