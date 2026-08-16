//go:build v8 && cgo && darwin && arm64

package v8engine

import (
	"context"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestStockV8ResizeAndIntersectionObserversRetainTargetsAndTeardown(t *testing.T) {
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	page, err := browserRuntime.LoadPage(context.Background(), "https://gossamer.test/observers", staticDocumentLoader{
		document: `<!doctype html><html><body style="margin:0"><div style="height:120px"></div><div id="target" style="display:block;width:40px;height:20px">target</div><div style="height:120px"></div></body></html>`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := page.SetViewport(render.Viewport{Width: 200, Height: 100}); err != nil {
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
		if _, err := page.QueueScript(browser.ScriptSource{URL: "https://gossamer.test/observers/" + label + ".js", Source: source}); err != nil {
			t.Fatal(err)
		}
		runOne(label)
	}

	queue("observe", `
		globalThis.__resizeEntries = [];
		globalThis.__intersectionEntries = [];
		globalThis.__observedTarget = document.getElementById("target");
		globalThis.__resizeObserver = new ResizeObserver((entries, observer) => {
			if (observer !== __resizeObserver) throw new Error("ResizeObserver callback identity failed");
			for (const entry of entries) {
				if ((__observedTarget !== undefined && entry.target !== __observedTarget) || !(entry.contentRect instanceof DOMRect) ||
					entry.contentBoxSize[0].inlineSize !== entry.contentRect.width ||
					entry.borderBoxSize.length !== 1) {
					throw new Error("ResizeObserverEntry shape failed");
				}
				__resizeEntries.push([entry.contentRect.width, entry.contentRect.height]);
			}
		});
		globalThis.__intersectionObserver = new IntersectionObserver((entries, observer) => {
			if (observer !== __intersectionObserver) throw new Error("IntersectionObserver callback identity failed");
			for (const entry of entries) {
				if ((__observedTarget !== undefined && entry.target !== __observedTarget) || !(entry.boundingClientRect instanceof DOMRect) ||
					!(entry.rootBounds instanceof DOMRect) || !(entry.intersectionRect instanceof DOMRect)) {
					throw new Error("IntersectionObserverEntry shape failed: " +
						(__observedTarget === undefined || entry.target === __observedTarget) + "," +
						(entry.boundingClientRect instanceof DOMRect) + "," +
						(entry.rootBounds instanceof DOMRect) + "," +
						(entry.intersectionRect instanceof DOMRect));
				}
				__intersectionEntries.push([entry.isIntersecting, entry.intersectionRatio, entry.time]);
			}
		}, {threshold: [0, 0.5, 1]});
		if (__intersectionObserver.root !== null ||
			__intersectionObserver.rootMargin !== "0px 0px 0px 0px" ||
			__intersectionObserver.thresholds.join(",") !== "0,0.5,1") {
			throw new Error("IntersectionObserver option reflection failed");
		}
		__resizeObserver.observe(__observedTarget);
		__intersectionObserver.observe(__observedTarget);
	`)
	queue("assert-initial", `
		if (__resizeEntries.length !== 1 || __resizeEntries[0][0] !== 40 || __resizeEntries[0][1] !== 20 ||
			__intersectionEntries.length !== 1 || __intersectionEntries[0][0] !== false ||
			__resizeObserver.takeRecords().length !== 0 || __intersectionObserver.takeRecords().length !== 0) {
			throw new Error("initial observer delivery failed");
		}
		__observedTarget.style.width = "80px";
		__observedTarget.style.height = "30px";
	`)
	queue("assert-resize", `
		if (__resizeEntries.length !== 2 || __resizeEntries[1][0] !== 80 || __resizeEntries[1][1] !== 30) {
			throw new Error("resize observer did not see same-task geometry");
		}
		__observedTarget.scrollIntoView();
	`)
	drain("observer scroll and render")
	queue("assert-intersection", `
		const last = __intersectionEntries[__intersectionEntries.length - 1];
		if (__intersectionEntries.length < 2 || last[0] !== true || last[1] <= 0 || last[2] < 0) {
			throw new Error("intersection observer did not follow root scrolling");
		}
		__observedTarget.remove();
		globalThis.__observedTarget = undefined;
	`)
	if realm, ok := engine.LatestRealm(); !ok {
		t.Fatal("observer realm missing")
	} else if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage with observer-only target: %v", err)
	}
	queue("disconnect", `
		__resizeObserver.disconnect();
		__intersectionObserver.disconnect();
		globalThis.__resizeObserver = undefined;
		globalThis.__intersectionObserver = undefined;
		globalThis.__resizeEntries = undefined;
		globalThis.__intersectionEntries = undefined;
	`)
	if realm, ok := engine.LatestRealm(); ok {
		if err := realm.CollectGarbage(page); err != nil {
			t.Fatalf("CollectGarbage after observer disconnect: %v", err)
		}
	}
	if err := page.Close(); err != nil {
		t.Fatal(err)
	}
	if ledger := browserRuntime.Ledger().Stats(); ledger.LiveObjects != 0 || ledger.PersistentObjects != 0 {
		t.Fatalf("layout observer teardown ownership = %#v", ledger)
	}
}
