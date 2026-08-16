//go:build v8 && cgo && darwin && arm64

package v8engine

import (
	"context"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
)

func TestStockV8TableTrackCollapseGeometryStaysLive(t *testing.T) {
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatalf("NewWithEngine: %v", err)
	}
	defer browserRuntime.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page, err := browserRuntime.LoadPage(ctx, "https://gossamer.test/table-track-collapse", staticDocumentLoader{
		document: `<!doctype html><html><body style="margin:0"><table id="table" style="width:200px;table-layout:fixed;border-spacing:0"><col id="column" style="width:100px"><col style="width:100px"><tr id="row"><td id="first" style="height:20px;padding:0"></td><td id="second" style="height:20px;padding:0"></td></tr><tr><td style="height:30px;padding:0"></td><td style="height:30px;padding:0"></td></tr></table></body></html>`,
	})
	if err != nil {
		t.Fatalf("LoadPage: %v", err)
	}

	if _, err := page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/table-track-collapse.js",
		Source: `
			(() => {
				const table = document.getElementById("table");
				const column = document.getElementById("column");
				const row = document.getElementById("row");
				const first = document.getElementById("first");
				const second = document.getElementById("second");
				const tableStyle = getComputedStyle(table);
				const columnStyle = getComputedStyle(column);
				const rowStyle = getComputedStyle(row);
				if (tableStyle.width !== "200px" || first.getBoundingClientRect().width !== 100 || second.getBoundingClientRect().left !== 100) {
					throw new Error("initial table tracks: " + [tableStyle.width, first.getBoundingClientRect().width, second.getBoundingClientRect().left]);
				}
				column.style.visibility = "collapse";
				if (column.style.visibility !== "collapse" || columnStyle.visibility !== "collapse" || tableStyle.width !== "100px" || first.getBoundingClientRect().width !== 0 || second.getBoundingClientRect().left !== 0) {
					throw new Error("live column collapse: " + [column.style.visibility, columnStyle.visibility, tableStyle.width, first.getBoundingClientRect().width, second.getBoundingClientRect().left]);
				}
				const height = table.getBoundingClientRect().height;
				row.style.visibility = "collapse";
				if (row.style.visibility !== "collapse" || rowStyle.visibility !== "collapse" || table.getBoundingClientRect().height >= height) {
					throw new Error("live row collapse: " + [row.style.visibility, rowStyle.visibility, height, table.getBoundingClientRect().height]);
				}
			})();
		`,
	}); err != nil {
		t.Fatalf("QueueScript: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("run table track collapse script: %v", err)
	}
}
