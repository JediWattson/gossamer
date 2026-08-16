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
				grid.style.height = "100px";
				grid.style.justifyContent = "center";
				grid.style.alignContent = "end";
				grid.style["justify-items"] = "end";
				grid.style.alignItems = "start";
				first.style.width = "20px";
				first.style.height = "10px";
				const aligned = first.getBoundingClientRect();
				if (retained.justifyContent !== "center" || retained.alignContent !== "end" || retained["align-content"] !== "end" || retained.justifyItems !== "end" || retained["justify-items"] !== "end" || retained.alignItems !== "start" || aligned.left !== 280 || aligned.top !== 30) {
					throw new Error("live grid alignment: " + [retained.justifyContent, retained.alignContent, retained.justifyItems, retained.alignItems, aligned.left, aligned.top]);
				}
				grid.style.gridTemplateColumns = "[first content-start] 100px [middle] 200px [last content-end]";
				first.style.gridColumnStart = "content";
				first.style.gridColumnEnd = "content";
				if (retained.gridTemplateColumns !== "[first content-start] 100px [middle] 200px [last content-end]" ||
					getComputedStyle(first).gridColumnStart !== "content" || getComputedStyle(first)["grid-column-end"] !== "content") {
					throw new Error("named grid lines: " + [retained.gridTemplateColumns, getComputedStyle(first).gridColumnStart, getComputedStyle(first)["grid-column-end"]]);
				}
				grid.style["grid-template-columns"] = "repeat(2, [slot] 150px [edge])";
				first.style.gridColumnStart = "slot";
				first.style.gridColumnEnd = "edge";
				if (retained["grid-template-columns"] !== "[slot] 150px [edge slot] 150px [edge]") {
					throw new Error("repeated named lines: " + retained["grid-template-columns"]);
				}
				grid.style.cssText = 'display:grid;width:300px;grid-template-areas:"head head" "nav main";grid-auto-columns:100px 200px;grid-auto-rows:20px 40px';
				first.style.cssText = "grid-area:main";
				const areaStyle = getComputedStyle(first);
				const areaRect = first.getBoundingClientRect();
				if (retained.gridTemplateAreas !== '"head head" "nav main"' ||
					areaStyle.gridRowStart !== "main" || areaStyle.gridColumnStart !== "main" ||
					areaStyle.gridRowEnd !== "main" || areaStyle.gridColumnEnd !== "main" ||
					areaRect.left !== 100 || areaRect.top !== 20 || areaRect.width !== 200 || areaRect.height !== 40) {
					throw new Error("named grid areas: " + [retained.gridTemplateAreas, areaStyle.gridRowStart, areaStyle.gridColumnStart, areaStyle.gridRowEnd, areaStyle.gridColumnEnd, areaRect.left, areaRect.top, areaRect.width, areaRect.height]);
				}
				grid.style["grid-template-areas"] = '"head main" "head main"';
				const movedArea = first.getBoundingClientRect();
				if (retained["grid-template-areas"] !== '"head main" "head main"' || movedArea.left !== 100 || movedArea.top !== 0 || movedArea.width !== 200 || movedArea.height !== 60) {
					throw new Error("live named grid areas: " + [retained["grid-template-areas"], movedArea.left, movedArea.top, movedArea.width, movedArea.height]);
				}
				grid.style.cssText = "display:grid;width:430px;column-gap:10px;grid-template-columns:repeat(auto-fit,100px)";
				first.style.cssText = "";
				second.style.cssText = "";
				const fitted = second.getBoundingClientRect();
				if (retained.gridTemplateColumns !== "100px 100px 0px 0px" || fitted.left !== 110 || fitted.width !== 100) {
					throw new Error("auto-fit grid tracks: " + [retained.gridTemplateColumns, fitted.left, fitted.width]);
				}
				grid.style.gridTemplateColumns = "repeat(auto-fill,100px)";
				if (retained["grid-template-columns"] !== "100px 100px 100px 100px") {
					throw new Error("live auto-fill grid tracks: " + retained["grid-template-columns"]);
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
