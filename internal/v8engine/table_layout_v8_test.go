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
		document: `<!doctype html><html><body style="margin:0"><table id="target"><colgroup><col style="width:40px"><col id="second" style="width:60px"></colgroup><tbody><tr><td id="cell">A</td><td>B</td></tr></tbody></table></body></html>`,
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
				if (retained.width !== "100px" || table.getBoundingClientRect().width !== 100) {
					throw new Error("initial table width: " + retained.width + " / " + table.getBoundingClientRect().width);
				}
				second.style.width = "80px";
				if (retained.width !== "120px" || table.getBoundingClientRect().width !== 120) {
					throw new Error("live table width: " + retained.width + " / " + table.getBoundingClientRect().width);
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
