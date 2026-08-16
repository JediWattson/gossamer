//go:build v8 && cgo && darwin && arm64

package v8engine

import (
	"context"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
)

func TestStockV8TablePercentageHeightSecondPassStaysLive(t *testing.T) {
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatalf("NewWithEngine: %v", err)
	}
	defer browserRuntime.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page, err := browserRuntime.LoadPage(ctx, "https://gossamer.test/table-percentage-height", staticDocumentLoader{
		document: `<!doctype html><html><body style="margin:0"><table id=table style="width:100px;height:120px;border-spacing:0"><tr id=percent-row style="height:25%"><td id=cell style="padding:0"><div id=child style="height:100%"><div style="height:10px"></div></div></td></tr><tr id=auto-row><td style="padding:0"><div style="height:10px"></div></td></tr></table></body></html>`,
	})
	if err != nil {
		t.Fatalf("LoadPage: %v", err)
	}

	if _, err := page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/table-percentage-height.js",
		Source: `
			(() => {
				const table = document.getElementById("table");
				const cell = document.getElementById("cell");
				const percentRow = document.getElementById("percent-row");
				const autoRow = document.getElementById("auto-row");
				const child = document.getElementById("child");
				const liveChild = getComputedStyle(child);
				const heights = () => [getComputedStyle(percentRow).height, getComputedStyle(autoRow).height, liveChild.height];
				if (heights().join(",") !== "30px,90px,30px") throw new Error("initial heights: " + heights());
				table.style.height = "200px";
				if (heights().join(",") !== "50px,150px,50px") throw new Error("updated heights: " + heights());
				cell.style.height = "100%";
				table.style.height = "auto";
				if (heights().join(",") !== "10px,10px,100%") throw new Error("auto heights: " + heights());
				table.style.height = "100%";
				if (heights().join(",") !== "10px,10px,10px") throw new Error("percentage table heights: " + heights());
			})();
		`,
	}); err != nil {
		t.Fatalf("QueueScript: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("run table percentage-height script: %v", err)
	}
}
