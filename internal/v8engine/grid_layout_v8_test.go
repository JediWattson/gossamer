//go:build v8 && cgo && darwin && arm64

package v8engine

import (
	"context"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
)

func TestStockV8GridComputedStyleAndGeometryStayLive(t *testing.T) {
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatalf("NewWithEngine: %v", err)
	}
	defer browserRuntime.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page, err := browserRuntime.LoadPage(ctx, "https://gossamer.test/grid-layout", staticDocumentLoader{
		document: `<!doctype html><html><body style="margin:0"><section id="grid" style="display:grid;width:300px;grid-template-columns:50px 1fr 2fr;grid-template-rows:40px;grid-auto-rows:20px;grid-auto-flow:row dense;gap:10px"><div id="first" style="grid-column:1 / span 2"></div><div id="second"></div></section></body></html>`,
	})
	if err != nil {
		t.Fatalf("LoadPage: %v", err)
	}

	if _, err := page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/grid-layout.js",
		Source: `
			(() => {
				const grid = document.getElementById("grid");
				const first = document.getElementById("first");
				const second = document.getElementById("second");
				const retained = getComputedStyle(grid);
				if (retained.display !== "grid" || retained.gridTemplateColumns !== "50px 76.66666666666667px 153.33333333333334px" ||
					retained.gridTemplateRows !== "40px" || retained.gridAutoRows !== "20px" ||
					retained.gridAutoFlow !== "row dense" || retained.gap !== "") {
					throw new Error("grid computed properties: " + [retained.display, retained.gridTemplateColumns, retained.gridTemplateRows, retained.gridAutoRows, retained.gridAutoFlow, retained.gap]);
				}
				if (getComputedStyle(first).gridColumnStart !== "1" || getComputedStyle(first).gridColumnEnd !== "span 2") {
					throw new Error("grid placement properties");
				}
				const initialFirst = first.getBoundingClientRect();
				const initialSecond = second.getBoundingClientRect();
				if (initialFirst.width < 130 || initialSecond.left <= initialFirst.right) {
					throw new Error("initial grid geometry: " + [initialFirst.width, initialSecond.left, initialFirst.right]);
				}
				grid.style.gridTemplateColumns = "100px 1fr 1fr";
				grid.style.columnGap = "20px";
				if (retained.gridTemplateColumns !== "100px 80px 80px" || retained.columnGap !== "20px" || second.getBoundingClientRect().left !== 220) {
					throw new Error("live grid mutation: " + [retained.gridTemplateColumns, retained.columnGap, second.getBoundingClientRect().left]);
				}
				grid.style.gridTemplateColumns = "minmax(160px, 1fr) 1fr";
				grid.style.gridAutoColumns = "fit-content(25%) 20px";
				grid.style.columnGap = "0";
				if (retained.gridTemplateColumns !== "160px 140px" || retained.gridAutoColumns !== "fit-content(25%) 20px") {
					throw new Error("live minmax tracks: " + [retained.gridTemplateColumns, retained.gridAutoColumns]);
				}
			})();
		`,
	}); err != nil {
		t.Fatalf("QueueScript: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("run grid script: %v", err)
	}
}
