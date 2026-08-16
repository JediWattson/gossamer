//go:build v8 && cgo && darwin && arm64

package v8engine

import (
	"context"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
)

func TestStockV8ComputedStyleResolvesLiveWidthAndSupportedHeight(t *testing.T) {
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatalf("NewWithEngine: %v", err)
	}
	defer func() {
		if err := browserRuntime.Close(); err != nil {
			t.Errorf("Close browser: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page, err := browserRuntime.LoadPage(
		ctx,
		"https://gossamer.test/resolved-computed-style",
		staticDocumentLoader{document: `<!doctype html><html><body style="margin:0">
			<div id="target" style="width:25vw;height:25vh"></div>
			<div id="parent"><div id="percentage" style="height:50%"></div></div>
		</body></html>`},
	)
	if err != nil {
		t.Fatalf("LoadPage: %v", err)
	}

	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/resolved-computed-style.js",
		Source: `
			(() => {
				const target = document.getElementById("target");
				const retained = getComputedStyle(target);
				if (retained.width !== "200px" || retained.height !== "150px") {
					throw new Error("initial used geometry was not resolved: " + retained.width + " x " + retained.height);
				}
				target.style.cssText = "width:50%;height:20px";
				if (retained.width !== "400px" || retained.height !== "20px") {
					throw new Error("retained declaration did not observe live geometry: " + retained.width + " x " + retained.height);
				}
				target.style.height = "50%";
				if (retained.height !== "50%") {
					throw new Error("indefinite percentage height was over-resolved: " + retained.height);
				}
				target.style.cssText = "display:inline;width:50px;height:20px";
				if (retained.width !== "50px" || retained.height !== "20px") {
					throw new Error("inline element did not retain computed dimensions: " + retained.width + " x " + retained.height);
				}

				const parent = document.getElementById("parent");
				const percentage = getComputedStyle(document.getElementById("percentage"));
				if (percentage.height !== "50%") {
					throw new Error("auto-height parent unexpectedly resolved percentage: " + percentage.height);
				}
				parent.style.height = "200px";
				if (percentage.height !== "100px") {
					throw new Error("definite parent did not resolve retained percentage: " + percentage.height);
				}
				parent.style.height = "300px";
				if (percentage.height !== "150px") {
					throw new Error("retained percentage did not observe parent mutation: " + percentage.height);
				}
				parent.style.height = "auto";
				if (percentage.height !== "50%") {
					throw new Error("percentage did not return to indefinite computed value: " + percentage.height);
				}
			})();
		`,
	})
	if err != nil {
		t.Fatalf("QueueScript: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("run resolved computed-style script: %v", err)
	}
}
