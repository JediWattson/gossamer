//go:build v8 && cgo && darwin && arm64

package v8engine

import (
	"context"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestStockV8AnimationFramesPerformanceClockAndViewportResize(t *testing.T) {
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	page, err := browserRuntime.LoadPage(context.Background(), "https://gossamer.test/animation", staticDocumentLoader{
		document: `<!doctype html><html><body style="margin:0"><div id="target" style="display:block;width:10px;height:10px"></div></body></html>`,
	})
	if err != nil {
		t.Fatal(err)
	}
	runOne := func(label string) {
		t.Helper()
		if err := page.Realm.RunOne(context.Background()); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
	}
	drain := func(label string) {
		t.Helper()
		for attempt := 0; attempt < 32 && page.Realm.Tasks.Len() != 0; attempt++ {
			runOne(label)
		}
		if page.Realm.Tasks.Len() != 0 {
			t.Fatalf("%s: task queue did not drain", label)
		}
	}
	queue := func(label, source string) {
		t.Helper()
		if _, err := page.QueueScript(browser.ScriptSource{URL: "https://gossamer.test/" + label + ".js", Source: source}); err != nil {
			t.Fatal(err)
		}
		runOne(label)
	}

	queue("schedule-animation", `
		globalThis.__frameTimes = [];
		globalThis.__frameMicrotasks = 0;
		const before = performance.now();
		const canceled = requestAnimationFrame(() => { throw new Error("canceled frame ran"); });
		cancelAnimationFrame(canceled);
		requestAnimationFrame(timestamp => {
			__frameTimes.push(timestamp);
			document.getElementById("target").style.width = "40px";
			queueMicrotask(() => __frameMicrotasks++);
		});
		requestAnimationFrame(timestamp => __frameTimes.push(timestamp));
		if (performance.now() < before || __frameTimes.length !== 0) {
			throw new Error("performance clock or frame scheduling was synchronous");
		}
	`)
	drain("animation batch and render")
	queue("assert-animation", `
		if (__frameTimes.length !== 2 || __frameTimes[0] !== __frameTimes[1] ||
			__frameTimes[0] <= 0 || __frameMicrotasks !== 1 ||
			document.getElementById("target").getBoundingClientRect().width !== 40) {
			throw new Error("animation batch, microtask, or frame render failed");
		}
		globalThis.__resizeEvents = 0;
		globalThis.__resizeFrameWidth = 0;
		document.documentElement.addEventListener("resize", () => {
			__resizeEvents++;
			requestAnimationFrame(() => { __resizeFrameWidth = innerWidth; });
		});
	`)
	if _, err := page.QueueViewportResize(render.Viewport{Width: 640, Height: 360}); err != nil {
		t.Fatal(err)
	}
	drain("viewport resize, animation, and render")
	queue("assert-resize", `
		if (__resizeEvents !== 1 || __resizeFrameWidth !== 640 || innerHeight !== 360) {
			throw new Error("viewport resize ordering failed");
		}
	`)
	if err := page.Close(); err != nil {
		t.Fatal(err)
	}
	if ledger := browserRuntime.Ledger().Stats(); ledger.LiveObjects != 0 || ledger.PersistentObjects != 0 {
		t.Fatalf("animation teardown ownership = %#v", ledger)
	}
}
