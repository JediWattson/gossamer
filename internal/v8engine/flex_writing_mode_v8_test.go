//go:build v8 && cgo && darwin && arm64

package v8engine

import (
	"context"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
)

func TestStockV8VerticalFlexGeometryStaysLive(t *testing.T) {
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page, err := browserRuntime.LoadPage(ctx, "https://gossamer.test/vertical-flex", staticDocumentLoader{
		document: `<!doctype html><html><body style="margin:0">
			<section id=flex style="display:flex;writing-mode:vertical-rl;direction:ltr;width:120px;height:200px;align-items:flex-start"><i id=first style="flex:none;width:30px;height:70px"></i><i id=second style="flex:none;width:40px;height:50px"></i></section>
		</body></html>`,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/vertical-flex.js",
		Source: `
			(() => {
				const flex = document.getElementById("flex");
				const first = document.getElementById("first");
				const second = document.getElementById("second");
				const retained = getComputedStyle(flex);
				let a = first.getBoundingClientRect();
				let b = second.getBoundingClientRect();
				if (retained.writingMode !== "vertical-rl" || a.x <= b.x || a.y >= b.y || a.width !== 30 || a.height !== 70) {
					throw new Error("initial vertical Flex geometry failed: " + JSON.stringify({a, b}));
				}
				flex.style.writingMode = "vertical-lr";
				flex.style.direction = "rtl";
				a = first.getBoundingClientRect();
				b = second.getBoundingClientRect();
				if (retained.writingMode !== "vertical-lr" || a.x !== b.x || a.y <= b.y || a.width !== 30 || a.height !== 70) {
					throw new Error("live vertical Flex geometry failed: " + JSON.stringify({a, b}));
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
