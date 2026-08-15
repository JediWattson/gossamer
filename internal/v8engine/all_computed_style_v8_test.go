//go:build v8 && cgo && darwin && arm64

package v8engine

import (
	"context"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
)

func TestStockV8ComputedStyleReflectsAllShorthand(t *testing.T) {
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
		"https://gossamer.test/computed-all",
		staticDocumentLoader{document: `<!doctype html><html><head><style>
			#target { color: #123456; display: block; width: 80px; }
		</style></head><body><div id="target">text</div></body></html>`},
	)
	if err != nil {
		t.Fatalf("LoadPage: %v", err)
	}

	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/computed-all.js",
		Source: `
			(() => {
				const target = document.getElementById("target");
				const retained = getComputedStyle(target);
				if (retained.display !== "block") {
					throw new Error("computed declaration did not capture the initial cascade");
				}
				target.style.cssText = "--keep: #abcdef; all: initial";
				if (retained.color !== "rgb(0, 0, 0)" || retained.display !== "inline" ||
					retained.width !== "auto" || retained.getPropertyValue("--keep") !== "#abcdef") {
					throw new Error("retained computed declaration did not observe all:initial");
				}
				if (retained.getPropertyValue("all") !== "") {
					throw new Error("computed declaration exposed the all shorthand value");
				}
				for (let index = 0; index < retained.length; index++) {
					if (retained.item(index) === "all") {
						throw new Error("computed declaration enumerated the all shorthand");
					}
				}
			})();
		`,
	})
	if err != nil {
		t.Fatalf("QueueScript: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("run computed all script: %v", err)
	}
}
