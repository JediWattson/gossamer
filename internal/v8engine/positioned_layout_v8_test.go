//go:build v8 && cgo && darwin && arm64

package v8engine

import (
	"context"
	"os"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
)

func TestStockV8ReactPositionedLayoutStackingAndFixedGeometry(t *testing.T) {
	reactBundle, err := os.ReadFile("testdata/react-19.2.7.production.js")
	if err != nil {
		t.Fatal(err)
	}
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	page, err := browserRuntime.LoadPage(context.Background(), "https://gossamer.test/positioned", staticDocumentLoader{
		document: `<!doctype html><html><body style="margin:0"><main id="root"></main></body></html>`,
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
	queue := func(label, source string) {
		t.Helper()
		if _, err := page.QueueScript(browser.ScriptSource{URL: "https://gossamer.test/positioned/" + label + ".js", Source: source}); err != nil {
			t.Fatal(err)
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

	queue("react", string(reactBundle))
	queue("mount", `
		(() => {
			const h = React.createElement;
			globalThis.__positionedClick = "";
			function App() {
				React.useLayoutEffect(() => {
					const container = document.getElementById("position-container");
					const high = document.getElementById("position-high");
					const fixed = document.getElementById("position-fixed");
					const highStyle = getComputedStyle(high);
					if (highStyle.position !== "absolute" || highStyle.zIndex !== "5" ||
						getComputedStyle(container).position !== "relative") {
						throw new Error("positioned computed styles diverged");
					}
					const before = fixed.getBoundingClientRect();
					window.scrollTo(0, 120);
					const after = fixed.getBoundingClientRect();
					if (before.x !== 5 || before.y !== 6 || after.x !== before.x || after.y !== before.y) {
						throw new Error("fixed geometry moved with root scrolling");
					}
					window.scrollTo(0, 0);
					globalThis.__positionedPoint = high.getBoundingClientRect().toJSON();
				}, []);
				const clicked = event => { globalThis.__positionedClick = event.target.id; };
				return h(React.Fragment, null,
					h("section", {id:"position-container", style:{display:"block", position:"relative", width:"200px", height:"160px"}},
						h("button", {id:"position-low", onClick:clicked, style:{display:"block", position:"absolute", left:"20px", top:"30px", width:"100px", height:"60px", zIndex:"1"}}, "low"),
						h("button", {id:"position-high", onClick:clicked, style:{display:"block", position:"absolute", left:"40px", top:"40px", width:"100px", height:"60px", zIndex:"5"}}, "high")),
					h("div", {style:{display:"block", height:"800px"}}),
					h("button", {id:"position-fixed", style:{display:"block", position:"fixed", left:"5px", top:"6px", width:"30px", height:"20px"}}, "fixed"));
			}
			globalThis.__positionedRoot = ReactDOM.createRoot(document.getElementById("root"));
			ReactDOM.flushSync(() => __positionedRoot.render(h(App)));
		})();
	`)
	drain("positioned mount and scroll")
	highID, found := page.Document().ElementByID("position-high")
	if !found {
		t.Fatal("position-high is missing")
	}
	if hit, ok := page.HitTest(50, 50); !ok || hit.Node != highID {
		t.Fatalf("positioned overlap hit = %#v, %t; want node %d", hit, ok, highID)
	}
	if _, err := page.QueueClick(50, 50, 0); err != nil {
		t.Fatal(err)
	}
	runOne("positioned overlap click")
	queue("assert", `
		if (__positionedClick !== "position-high" || __positionedPoint.x !== 40 || __positionedPoint.y !== 40) {
			throw new Error("positioned paint, geometry, and hit testing diverged: " + __positionedClick);
		}
		ReactDOM.flushSync(() => __positionedRoot.unmount());
		globalThis.__positionedRoot = undefined;
		globalThis.__positionedPoint = undefined;
	`)
	drain("positioned unmount")
	if realm, ok := engine.LatestRealm(); ok {
		if err := realm.CollectGarbage(page); err != nil {
			t.Fatal(err)
		}
	}
	if err := page.Close(); err != nil {
		t.Fatal(err)
	}
	if ledger := browserRuntime.Ledger().Stats(); ledger.LiveObjects != 0 || ledger.PersistentObjects != 0 {
		t.Fatalf("positioned layout teardown ownership = %#v", ledger)
	}
}
