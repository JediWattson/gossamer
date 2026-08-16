//go:build v8 && cgo && darwin && arm64

package v8engine

import (
	"context"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
)

func TestStockV8ComputedWritingModeIsLiveAndParticipatesInAll(t *testing.T) {
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page, err := browserRuntime.LoadPage(ctx, "https://gossamer.test/writing-mode", staticDocumentLoader{
		document: `<!doctype html><html><body>
			<section id=target style="writing-mode:vertical-rl"><div id=child></div></section>
			<div id=all style="writing-mode:vertical-rl;all:initial"></div>
		</body></html>`,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/writing-mode.js",
		Source: `
			(() => {
				const target = document.getElementById("target");
				const child = document.getElementById("child");
				const retained = getComputedStyle(child);
				if (retained.writingMode !== "vertical-rl" || retained.getPropertyValue("writing-mode") !== "vertical-rl") {
					throw new Error("initial/inherited writing-mode failed: " + retained.writingMode);
				}
				target.style.writingMode = "vertical-lr";
				if (retained.writingMode !== "vertical-lr" || target.style["writing-mode"] !== "vertical-lr") {
					throw new Error("live writing-mode mutation failed: " + retained.writingMode);
				}
				if (getComputedStyle(document.getElementById("all")).writingMode !== "horizontal-tb") {
					throw new Error("all did not reset writing-mode");
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
