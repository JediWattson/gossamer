//go:build v8 && cgo && darwin && arm64

package v8engine

import (
	"context"
	"os"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
)

func TestStockV8ReactFlexLayoutGeometryAndDelegatedHitTesting(t *testing.T) {
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
	page, err := browserRuntime.LoadPage(context.Background(), "https://gossamer.test/flex", staticDocumentLoader{
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
		if _, err := page.QueueScript(browser.ScriptSource{URL: "https://gossamer.test/flex/" + label + ".js", Source: source}); err != nil {
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
			globalThis.__flexClick = "";
			function App() {
				React.useLayoutEffect(() => {
					const row = document.getElementById("flex-row");
					const a = document.getElementById("flex-a").getBoundingClientRect();
					const b = document.getElementById("flex-b").getBoundingClientRect();
					const c = document.getElementById("flex-c").getBoundingClientRect();
					const rowStyle = getComputedStyle(row);
					if (rowStyle.display !== "flex" || rowStyle.columnGap !== "10px" ||
						rowStyle.alignItems !== "center" || b.x !== 0 || b.y !== 30 || b.width !== 90 ||
						a.x !== 100 || a.y !== 40 || c.x !== 160 || c.y !== 35 || c.width !== 140) {
						throw new Error("React flex layout effect geometry diverged");
					}
					globalThis.__flexPoint = c.toJSON();
				}, []);
				const clicked = event => { globalThis.__flexClick = event.target.id; };
				return h("section", {id:"flex-row", style:{display:"flex", width:"300px", height:"100px", columnGap:"10px", alignItems:"center"}},
					h("button", {id:"flex-a", onClick:clicked, style:{display:"block", width:"50px", height:"20px", order:"2"}}, "a"),
					h("button", {id:"flex-b", onClick:clicked, style:{display:"block", flex:"1 1 40px", height:"40px", order:"1"}}, "b"),
					h("button", {id:"flex-c", onClick:clicked, style:{display:"block", flex:"2 1 40px", height:"30px", order:"3"}}, "c"));
			}
			globalThis.__flexRoot = ReactDOM.createRoot(document.getElementById("root"));
			ReactDOM.flushSync(() => __flexRoot.render(h(App)));
		})();
	`)
	drain("flex mount")
	cID, found := page.Document().ElementByID("flex-c")
	if !found {
		t.Fatal("flex-c is missing after React mount")
	}
	if hit, ok := page.HitTest(200, 50); !ok || hit.Node != cID {
		t.Fatalf("flex hit = %#v, %t; want node %d", hit, ok, cID)
	}
	if _, err := page.QueueClick(200, 50, 0); err != nil {
		t.Fatal(err)
	}
	runOne("flex delegated click")
	queue("assert", `
		if (__flexClick !== "flex-c" || __flexPoint.x !== 160 || __flexPoint.width !== 140) {
			throw new Error("flex geometry and delegated hit target diverged: " + __flexClick);
		}
		ReactDOM.flushSync(() => __flexRoot.unmount());
		globalThis.__flexRoot = undefined;
		globalThis.__flexPoint = undefined;
	`)
	drain("flex unmount")
	if realm, ok := engine.LatestRealm(); ok {
		if err := realm.CollectGarbage(page); err != nil {
			t.Fatal(err)
		}
	}
	if err := page.Close(); err != nil {
		t.Fatal(err)
	}
	if ledger := browserRuntime.Ledger().Stats(); ledger.LiveObjects != 0 || ledger.PersistentObjects != 0 {
		t.Fatalf("flex layout teardown ownership = %#v", ledger)
	}
}
