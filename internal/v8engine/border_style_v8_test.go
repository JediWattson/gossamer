//go:build v8 && cgo && darwin && arm64

package v8engine

import (
	"context"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
)

func TestStockV8ComputedBorderLineStylesAndGeometryStayLive(t *testing.T) {
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page, err := browserRuntime.LoadPage(ctx, "https://gossamer.test/border-styles", staticDocumentLoader{
		document: `<!doctype html><html><body style="margin:0"><div id=target style="width:40px;height:20px;border:6px dotted #6480a0"></div></body></html>`,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = page.QueueScript(browser.ScriptSource{
		URL: "https://gossamer.test/border-styles.js",
		Source: `
			(() => {
				const target = document.getElementById("target");
				const retained = getComputedStyle(target);
				if (retained.borderTopStyle !== "dotted" || target.getBoundingClientRect().width !== 52) {
					throw new Error("initial dotted border style/geometry failed");
				}
				target.style.borderStyle = "double dashed groove inset";
				if (retained.borderTopStyle !== "double" || retained.borderRightStyle !== "dashed" ||
					retained.borderBottomStyle !== "groove" || retained.borderLeftStyle !== "inset" ||
					target.getBoundingClientRect().width !== 52) {
					throw new Error("live border-style shorthand failed");
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
