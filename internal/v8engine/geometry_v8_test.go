//go:build v8 && cgo && darwin && arm64

package v8engine

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
)

func TestStockV8CSSOMViewRootScrollAndReactMeasurement(t *testing.T) {
	reactBundle, err := os.ReadFile("testdata/react-19.2.7.production.js")
	if err != nil {
		t.Fatalf("read React fixture: %v", err)
	}
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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	page, err := browserRuntime.LoadPage(ctx, "https://gossamer.test/cssom-view", staticDocumentLoader{
		document: `<!doctype html><html><body style="margin:0"><main id="root"></main></body></html>`,
	})
	if err != nil {
		t.Fatalf("LoadPage: %v", err)
	}
	realm, ok := engine.LatestRealm()
	if !ok {
		t.Fatal("stock V8 engine has no CSSOM View realm")
	}
	runOne := func(label string) {
		t.Helper()
		if err := page.Realm.RunOne(context.Background()); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
	}
	queueScript := func(label, source string) {
		t.Helper()
		if _, err := page.QueueScript(browser.ScriptSource{
			URL:    "https://gossamer.test/cssom-view/" + label + ".js",
			Source: source,
		}); err != nil {
			t.Fatalf("queue %s: %v", label, err)
		}
		runOne(label)
	}
	drain := func(label string) {
		t.Helper()
		for attempt := 0; attempt < 64 && page.Realm.Tasks.Len() != 0; attempt++ {
			runOne(label)
		}
		if page.Realm.Tasks.Len() != 0 {
			t.Fatalf("%s: task queue did not drain", label)
		}
	}

	queueScript("react", string(reactBundle))
	queueScript("mount", `
		(() => {
			const h = React.createElement;
			globalThis.__scrollEvents = 0;
			globalThis.__scrollMicrotasks = 0;
			globalThis.__scrollListener = event => {
				if (event.target !== document.documentElement || event.bubbles || event.cancelable) {
					throw new Error("root scroll event shape diverged");
				}
				__scrollEvents++;
				queueMicrotask(() => __scrollMicrotasks++);
			};
			document.documentElement.addEventListener("scroll", __scrollListener);
			function MeasuredList() {
				React.useLayoutEffect(() => {
					const last = document.getElementById("measure-item-29");
					const before = last.getBoundingClientRect();
					if (before === last.getBoundingClientRect()) throw new Error("DOMRect was cached");
					if (!(before instanceof DOMRect) || before.height !== 40 ||
						before.bottom !== before.y + before.height || before.toJSON().height !== 40 ||
						last.getClientRects().length !== 1) {
						throw new Error("DOMRect geometry surface failed");
					}
					globalThis.__heldGeometryRect = before;
					globalThis.__beforeScrollY = before.y;
					last.scrollIntoView();
					globalThis.__syncScrollY = scrollY;
					globalThis.__afterScrollY = last.getBoundingClientRect().y;
				}, []);
				return h("section", {id: "measured-list"},
					Array.from({length: 30}, (_, index) => h("div", {
						id: "measure-item-" + index,
						key: index,
						style: {display: "block", height: "40px"},
					}, "row " + index)));
			}
			globalThis.__geometryRoot = ReactDOM.createRoot(document.getElementById("root"));
			ReactDOM.flushSync(() => __geometryRoot.render(h(MeasuredList)));
			if (innerWidth !== 800 || innerHeight !== 600 || __syncScrollY <= 0 ||
				pageYOffset !== scrollY || document.documentElement.scrollTop !== scrollY ||
				document.scrollingElement !== document.documentElement ||
				__afterScrollY >= __beforeScrollY || document.documentElement.clientHeight !== innerHeight ||
				document.documentElement.scrollHeight <= innerHeight) {
				throw new Error("synchronous React measurement or root scrolling failed");
			}
			window.scrollBy(0, -20);
			if (scrollY !== __syncScrollY - 20) throw new Error("scrollBy failed");
			if (__scrollEvents !== 0) throw new Error("scroll event fired inside the mutating task");
		})();
	`)
	drain("scroll event and render")
	queueScript("assert", `
		if (__scrollEvents !== 1 || __scrollMicrotasks !== 1 ||
			__heldGeometryRect.height !== 40 || __heldGeometryRect.toJSON().width !== 800) {
			throw new Error("coalesced scroll delivery or retained DOMRect failed");
		}
	`)
	queueScript("nested-scroll", `
		(() => {
			const scroller = document.createElement("div");
			scroller.id = "nested-scroller";
			scroller.setAttribute("style", "display:block;height:40px;overflow:auto");
			const spacer = document.createElement("div");
			spacer.setAttribute("style", "display:block;height:60px");
			const target = document.createElement("button");
			target.id = "nested-target";
			target.setAttribute("style", "display:block;height:30px");
			target.textContent = "nested";
			scroller.append(spacer, target);
			document.body.appendChild(scroller);
			globalThis.__nestedScrollEvents = 0;
			globalThis.__nestedScroller = scroller;
			globalThis.__nestedTarget = target;
			scroller.addEventListener("scroll", event => {
				if (event.target !== scroller || event.bubbles || event.cancelable) {
					throw new Error("element scroll event shape diverged");
				}
				__nestedScrollEvents++;
			});
			const before = target.getBoundingClientRect().y;
			scroller.scrollTop = 60;
			if (scroller.scrollTop !== 50 || target.getBoundingClientRect().y !== before - 50 ||
				scroller.clientHeight !== 40 || scroller.scrollHeight < 90 || __nestedScrollEvents !== 0) {
				throw new Error("synchronous element scrolling failed");
			}
		})();
	`)
	drain("element scroll event and render")
	queueScript("nested-assert", `
		if (__nestedScrollEvents !== 1 || __nestedScroller.scrollTop !== 50) {
			throw new Error("element scroll delivery was not coalesced");
		}
	`)
	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage with retained DOMRect: %v", err)
	}
	queueScript("after-gc", `
		if (!(__heldGeometryRect instanceof DOMRect) || __heldGeometryRect.height !== 40) {
			throw new Error("DOMRect did not survive independently of its source wrapper");
		}
		document.documentElement.removeEventListener("scroll", __scrollListener);
		globalThis.__scrollListener = undefined;
		globalThis.__heldGeometryRect = undefined;
		globalThis.__nestedScroller.remove();
		globalThis.__nestedScroller = undefined;
		globalThis.__nestedTarget = undefined;
		ReactDOM.flushSync(() => __geometryRoot.unmount());
		globalThis.__geometryRoot = undefined;
	`)
	drain("React geometry unmount")
	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage after geometry unmount: %v", err)
	}
	if err := page.Close(); err != nil {
		t.Fatalf("Close CSSOM View page: %v", err)
	}
	if ledger := browserRuntime.Ledger().Stats(); ledger.LiveObjects != 0 || ledger.PersistentObjects != 0 {
		t.Fatalf("CSSOM View teardown ownership = %#v", ledger)
	}
}
