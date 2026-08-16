//go:build v8 && cgo && darwin && arm64

package v8engine

import (
	"context"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
)

func TestStockV8VerticalTableGeometryAndComputedDimensionsStayLive(t *testing.T) {
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page, err := browserRuntime.LoadPage(ctx, "https://gossamer.test/vertical-table", staticDocumentLoader{
		document: `<!doctype html><html><body style="margin:0">
			<table id=target style="writing-mode:vertical-rl;direction:ltr;width:70px;height:130px;border-spacing:10px 5px">
				<col style="height:40px"><col style="height:60px">
				<tr id=first style="width:25px"><td id=a>A</td><td id=b>B</td></tr>
				<tr id=second style="width:30px"><td>C</td><td>D</td></tr>
			</table>
		</body></html>`,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/vertical-table.js",
		Source: `
			(() => {
				const table = document.getElementById("target");
				const first = document.getElementById("first");
				const second = document.getElementById("second");
				const a = document.getElementById("a");
				const b = document.getElementById("b");
				const retained = getComputedStyle(table);
				let rect = table.getBoundingClientRect();
				if (retained.writingMode !== "vertical-rl" || retained.width !== "70px" || retained.height !== "130px" ||
					rect.width !== 70 || rect.height !== 130 || first.getBoundingClientRect().x <= second.getBoundingClientRect().x ||
					a.getBoundingClientRect().y >= b.getBoundingClientRect().y) {
					throw new Error("initial vertical table geometry failed");
				}
				table.style.writingMode = "vertical-lr";
				table.style.direction = "rtl";
				rect = table.getBoundingClientRect();
				if (retained.writingMode !== "vertical-lr" || rect.width !== 70 || rect.height !== 130 ||
					first.getBoundingClientRect().x >= second.getBoundingClientRect().x ||
					a.getBoundingClientRect().y <= b.getBoundingClientRect().y) {
					throw new Error("live vertical table geometry failed");
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
