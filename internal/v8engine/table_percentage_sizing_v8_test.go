//go:build v8 && cgo && darwin && arm64

package v8engine

import (
	"context"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
)

func TestStockV8TablePercentageSizingStaysLive(t *testing.T) {
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatalf("NewWithEngine: %v", err)
	}
	defer browserRuntime.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page, err := browserRuntime.LoadPage(ctx, "https://gossamer.test/table-percentage-sizing", staticDocumentLoader{
		document: `<!doctype html><html><body style="margin:0"><table id=table style="width:400px;border-spacing:0"><tr><td id=percent style="width:25%;height:10px;padding:0"></td><td id=pixel style="width:100px;height:10px;padding:0"></td><td id=auto style="height:10px;padding:0"></td></tr></table><div id=outer style="width:500px"><div style="width:10%"><div id=inline-table style="display:inline-table;border-spacing:0"><div style="display:table-row"><div id=inline-percent style="display:table-cell;width:100%;padding:0"><span style="display:inline-block;width:100%;height:10px"></span></div><div id=inline-fixed style="display:table-cell;padding:0"><span style="display:inline-block;width:10px;height:10px"></span></div></div></div></div></div></body></html>`,
	})
	if err != nil {
		t.Fatalf("LoadPage: %v", err)
	}

	if _, err := page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/table-percentage-sizing.js",
		Source: `
			(() => {
				const percent = document.getElementById("percent");
				const pixel = document.getElementById("pixel");
				const auto = document.getElementById("auto");
				const live = getComputedStyle(percent);
				const widths = () => [live.width, getComputedStyle(pixel).width, getComputedStyle(auto).width];
				if (widths().join(",") !== "100px,100px,200px") throw new Error("initial sizing: " + widths());
				percent.style.width = "50%";
				if (widths().join(",") !== "200px,100px,100px") throw new Error("updated sizing: " + widths());
				percent.style.cssText = "width:80%;max-width:25%;height:10px;padding:0";
				if (widths().join(",") !== "100px,100px,200px") throw new Error("clamped sizing: " + widths());
				const outer = document.getElementById("outer");
				const inlineTable = document.getElementById("inline-table");
				const inlinePercent = document.getElementById("inline-percent");
				const inlineFixed = document.getElementById("inline-fixed");
				const inlineWidths = () => [getComputedStyle(inlineTable).width, getComputedStyle(inlinePercent).width, getComputedStyle(inlineFixed).width];
				if (inlineWidths().join(",") !== "50px,40px,10px") throw new Error("initial inline sizing: " + inlineWidths());
				outer.style.width = "1000px";
				if (inlineWidths().join(",") !== "100px,90px,10px") throw new Error("updated inline sizing: " + inlineWidths());
			})();
		`,
	}); err != nil {
		t.Fatalf("QueueScript: %v", err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatalf("run table percentage sizing script: %v", err)
	}
}
