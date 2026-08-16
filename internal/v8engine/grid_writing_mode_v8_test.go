//go:build v8 && cgo && darwin && arm64

package v8engine

import (
	"context"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
)

func TestStockV8VerticalGridAndOrthogonalSubgridGeometryStayLive(t *testing.T) {
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page, err := browserRuntime.LoadPage(ctx, "https://gossamer.test/vertical-grid", staticDocumentLoader{
		document: `<!doctype html><html><body style="margin:0">
			<section id=grid style="display:grid;writing-mode:vertical-rl;direction:ltr;width:120px;height:200px;grid-template-columns:50px 70px;grid-template-rows:40px 60px;column-gap:10px;row-gap:20px;justify-content:start;align-content:start"><i id=a style="grid-column:1;grid-row:1"></i><i id=b style="grid-column:2;grid-row:2"></i></section>
			<section id=parent style="display:grid;width:210px;grid-template-columns:80px 120px;grid-template-rows:50px 70px;column-gap:10px;row-gap:10px;justify-content:start;align-content:start"><div id=sub style="display:grid;writing-mode:vertical-rl;grid-column:1/span 2;grid-row:1/span 2;grid-template-columns:subgrid;grid-template-rows:subgrid"><i id=sa style="grid-column:1;grid-row:1"></i><i id=sb style="grid-column:2;grid-row:2"></i></div></section>
		</body></html>`,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/vertical-grid.js",
		Source: `
			(() => {
				const grid = document.getElementById("grid");
				const a = document.getElementById("a");
				const b = document.getElementById("b");
				const retained = getComputedStyle(grid);
				if (retained.gridTemplateColumns !== "50px 70px" || retained.gridTemplateRows !== "40px 60px" ||
					a.getBoundingClientRect().x <= b.getBoundingClientRect().x || a.getBoundingClientRect().y >= b.getBoundingClientRect().y) {
					throw new Error("initial vertical grid geometry failed");
				}
				grid.style.writingMode = "vertical-lr";
				grid.style.direction = "rtl";
				if (retained.writingMode !== "vertical-lr" || a.getBoundingClientRect().x >= b.getBoundingClientRect().x ||
					a.getBoundingClientRect().y <= b.getBoundingClientRect().y) {
					throw new Error("live vertical grid geometry failed");
				}
				const sub = document.getElementById("sub");
				const sa = document.getElementById("sa").getBoundingClientRect();
				const sb = document.getElementById("sb").getBoundingClientRect();
				const subStyle = getComputedStyle(sub);
				if (subStyle.gridTemplateColumns !== "subgrid [] [] []" || subStyle.gridTemplateRows !== "subgrid [] [] []" ||
					sa.x <= sb.x || sa.y >= sb.y || sa.width !== 120 || sa.height !== 50 || sb.width !== 80 || sb.height !== 70) {
					throw new Error("orthogonal subgrid geometry failed: " + JSON.stringify({columns: subStyle.gridTemplateColumns, rows: subStyle.gridTemplateRows, sa, sb}));
				}
			})();
		`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
}
