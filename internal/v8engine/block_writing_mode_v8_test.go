//go:build v8 && cgo && darwin && arm64

package v8engine

import (
	"context"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
)

func TestStockV8VerticalBlockGeometryStaysLive(t *testing.T) {
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page, err := browserRuntime.LoadPage(ctx, "https://gossamer.test/vertical-block", staticDocumentLoader{
		document: `<!doctype html><html><body style="margin:0"><section id=flow style="writing-mode:vertical-rl;width:120px;height:200px"><i id=first style="display:block;width:30px;height:50px"></i><i id=second style="display:block;width:40px;height:70px"></i></section></body></html>`,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/vertical-block.js",
		Source: `
			(() => {
				const flow = document.getElementById("flow");
				const first = document.getElementById("first");
				const second = document.getElementById("second");
				const retained = getComputedStyle(flow);
				let a = first.getBoundingClientRect();
				let b = second.getBoundingClientRect();
				if (retained.writingMode !== "vertical-rl" || a.x <= b.x || a.y !== b.y || a.width !== 30 || a.height !== 50) {
					throw new Error("initial vertical block geometry failed: " + JSON.stringify({a, b}));
				}
				flow.style.writingMode = "vertical-lr";
				a = first.getBoundingClientRect();
				b = second.getBoundingClientRect();
				if (retained.writingMode !== "vertical-lr" || a.x >= b.x || a.y !== b.y || a.width !== 30 || a.height !== 50) {
					throw new Error("live vertical block geometry failed: " + JSON.stringify({a, b}));
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
