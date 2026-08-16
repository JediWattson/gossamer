//go:build v8 && cgo && darwin && arm64

package v8engine

import (
	"context"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
)

func TestStockV8ComputedTextOrientationStaysLive(t *testing.T) {
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page, err := browserRuntime.LoadPage(ctx, "https://gossamer.test/text-orientation", staticDocumentLoader{
		document: `<!doctype html><html><body><span id="target" style="display:inline-block;writing-mode:vertical-rl;text-orientation:mixed;font-size:20px;line-height:24px">A漢</span></body></html>`,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/text-orientation.js",
		Source: `
			(() => {
				const target = document.getElementById("target");
				const retained = getComputedStyle(target);
				const initialHeight = target.getBoundingClientRect().height;
				if (retained.textOrientation !== "mixed" || retained.getPropertyValue("text-orientation") !== "mixed") {
					throw new Error("initial text-orientation failed: " + retained.textOrientation);
				}
				target.style.textOrientation = "upright";
				if (retained.textOrientation !== "upright" || !("text-orientation" in retained) || target.getBoundingClientRect().height !== 40) {
					throw new Error("live upright text-orientation failed: " + retained.textOrientation);
				}
				target.style.setProperty("text-orientation", "sideways");
				if (retained.textOrientation !== "sideways" || target.getBoundingClientRect().height >= 40) {
					throw new Error("live sideways text-orientation failed: " + retained.textOrientation);
				}
				target.style.textOrientation = "rotate-left";
				if (retained.textOrientation !== "mixed" || target.getBoundingClientRect().height !== initialHeight) {
					throw new Error("invalid text-orientation was not discarded: " + retained.textOrientation);
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
