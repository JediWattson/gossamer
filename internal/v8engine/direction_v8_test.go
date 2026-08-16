//go:build v8 && cgo && darwin && arm64

package v8engine

import (
	"context"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
)

func TestStockV8ComputedDirectionAndRTLTableGeometryStayLive(t *testing.T) {
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page, err := browserRuntime.LoadPage(ctx, "https://gossamer.test/direction", staticDocumentLoader{
		document: `<!doctype html><html><body style="margin:0">
			<table id=target dir=rtl style="table-layout:fixed;width:120px;border-spacing:0"><tr>
				<td id=first>first</td><td id=second>second</td>
			</tr></table><div id=all style="direction:rtl;all:initial"></div>
		</body></html>`,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/direction.js",
		Source: `
			(() => {
				const table = document.getElementById("target");
				const first = document.getElementById("first");
				const second = document.getElementById("second");
				const retained = getComputedStyle(table);
				if (retained.direction !== "rtl") {
					throw new Error("initial computed direction: " + retained.direction);
				}
				if (first.getBoundingClientRect().x <= second.getBoundingClientRect().x) {
					throw new Error("initial RTL table geometry: " + first.getBoundingClientRect().x + "/" + second.getBoundingClientRect().x);
				}
				table.setAttribute("dir", "ltr");
				if (retained.direction !== "ltr" || first.getBoundingClientRect().x >= second.getBoundingClientRect().x) {
					throw new Error("live LTR direction/table geometry failed");
				}
				table.style.direction = "rtl";
				if (retained.direction !== "rtl" || first.getBoundingClientRect().x <= second.getBoundingClientRect().x) {
					throw new Error("author direction override failed");
				}
				if (getComputedStyle(document.getElementById("all")).direction !== "rtl") {
					throw new Error("all reset direction");
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
