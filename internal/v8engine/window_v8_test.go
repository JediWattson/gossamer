//go:build v8 && cgo && darwin && arm64

package v8engine

import (
	"context"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/window"
)

func TestStockV8GraphiteShellRoutesNativeInputAndTeardown(t *testing.T) {
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	page, err := browserRuntime.LoadPage(context.Background(), "https://gossamer.test/window", staticDocumentLoader{
		document: `<!doctype html><html><body style="margin:0">
			<input id="target" style="display:block;width:100px;height:30px">
			<div style="display:block;height:800px">content</div>
		</body></html>`,
	})
	if err != nil {
		t.Fatal(err)
	}
	queue := func(label, source string) {
		t.Helper()
		if _, queueErr := page.QueueScript(browser.ScriptSource{
			URL: "https://gossamer.test/window/" + label + ".js", Source: source,
		}); queueErr != nil {
			t.Fatal(queueErr)
		}
		for attempt := 0; attempt < 64 && page.Realm.Tasks.Len() != 0; attempt++ {
			if runErr := page.Realm.RunOne(context.Background()); runErr != nil {
				t.Fatalf("%s: %v", label, runErr)
			}
		}
		if page.Realm.Tasks.Len() != 0 {
			t.Fatalf("%s: task queue did not drain", label)
		}
	}
	queue("listen", `
		globalThis.__windowEvents = [];
		const target = document.getElementById("target");
		const record = event => __windowEvents.push({
			type: event.type,
			target: event.target.id || event.target.nodeName,
			key: event.key || "",
			code: event.code || "",
			data: event.data || "",
			inputType: event.inputType || "",
			value: target.value,
		});
		for (const type of ["pointerdown", "pointerup", "click", "focus", "keydown", "beforeinput", "input", "keyup", "blur"])
			target.addEventListener(type, record);
		document.documentElement.addEventListener("resize", record);
		document.documentElement.addEventListener("scroll", record);
	`)

	backend := window.NewMemoryBackend(
		window.Event{Kind: window.EventResize, Width: 368, Height: 324},
		window.Event{Kind: window.EventPointerDown, X: 5, Y: 89, Button: 0, Buttons: 1},
		window.Event{Kind: window.EventPointerUp, X: 5, Y: 89, Button: 0},
		window.Event{Kind: window.EventPointerDown, X: 5, Y: 89, Button: 0, Buttons: 1},
		window.Event{Kind: window.EventPointerUp, X: 150, Y: 134, Button: 0},
		window.Event{Kind: window.EventFocus},
		window.Event{Kind: window.EventKeyDown, Key: "a", Code: "KeyA", Text: "a"},
		window.Event{Kind: window.EventKeyUp, Key: "a", Code: "KeyA"},
		window.Event{Kind: window.EventScroll, DeltaY: 100},
		window.Event{Kind: window.EventBlur},
		window.Event{Kind: window.EventClose},
	)
	if err := window.RunBrowser(context.Background(), page, backend, window.ShellConfig{
		Title: "Gossamer V8 Graphite test",
	}); err != nil {
		t.Fatal(err)
	}
	queue("assert", `
		const types = __windowEvents.map(event => event.type).join(",");
		if (types !== "resize,pointerdown,pointerup,click,pointerdown,focus,keydown,beforeinput,input,keyup,scroll,blur")
			throw new Error("native event order diverged: " + types);
		const key = __windowEvents.find(event => event.type === "keydown");
		const before = __windowEvents.find(event => event.type === "beforeinput");
		const input = __windowEvents.find(event => event.type === "input");
		if (key.key !== "a" || key.code !== "KeyA" || before.data !== "a" ||
			before.inputType !== "insertText" || input.value !== "a" ||
			innerWidth !== 320 || innerHeight !== 240 || scrollY !== 100 ||
			document.activeElement === document.getElementById("target"))
			throw new Error("native payload, edit, viewport, scroll, or blur state diverged");
		globalThis.__windowEvents = undefined;
	`)
	if len(backend.Frames()) < 3 {
		t.Fatalf("Graphite backend presented %d frames, want at least 3", len(backend.Frames()))
	}
	lastFrame := backend.Frames()[len(backend.Frames())-1].Bounds()
	if lastFrame.Dx() != 368 || lastFrame.Dy() != 324 {
		t.Fatalf("Graphite frame = %v, want 368x324", lastFrame)
	}
	if realm, ok := engine.LatestRealm(); ok {
		if err := realm.CollectGarbage(page); err != nil {
			t.Fatal(err)
		}
	}
	if err := page.Close(); err != nil {
		t.Fatal(err)
	}
	if ledger := browserRuntime.Ledger().Stats(); ledger.LiveObjects != 0 || ledger.PersistentObjects != 0 {
		t.Fatalf("interactive window teardown ownership = %#v", ledger)
	}
}

func TestStockV8HistoryTraversalReplacesRealmsAndInvalidatesWrappers(t *testing.T) {
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	client := staticDocumentLoader{document: `<!doctype html><html><body><p id="current">history document</p></body></html>`}
	page, err := browserRuntime.LoadPage(context.Background(), "https://gossamer.test/a", client)
	if err != nil {
		t.Fatal(err)
	}
	navigate := func(rawURL string) {
		t.Helper()
		navigation, navigateErr := page.Navigate(context.Background(), rawURL, client)
		if navigateErr != nil {
			t.Fatal(navigateErr)
		}
		if waitErr := page.WaitNavigation(context.Background(), navigation); waitErr != nil {
			t.Fatal(waitErr)
		}
	}
	navigate("https://gossamer.test/b")
	navigate("https://gossamer.test/c")
	cGeneration := page.DocumentGeneration()
	cID, found := page.Document().ElementByID("current")
	if !found {
		t.Fatal("V8 history document has no stable current element")
	}
	cHandle := browser.NodeHandle{Document: cGeneration, Node: cID}

	back, err := page.Back(context.Background(), client)
	if err != nil {
		t.Fatal(err)
	}
	if err := page.WaitNavigation(context.Background(), back); err != nil {
		t.Fatal(err)
	}
	if got := page.URL().String(); got != "https://gossamer.test/b" {
		t.Fatalf("V8 back URL = %q", got)
	}
	if page.DocumentGeneration() == cGeneration {
		t.Fatal("V8 back traversal retained the C document generation")
	}
	if _, ok := page.Resolve(cHandle); ok {
		t.Fatal("V8 back traversal resolved a C wrapper handle")
	}
	forward, err := page.Forward(context.Background(), client)
	if err != nil {
		t.Fatal(err)
	}
	if err := page.WaitNavigation(context.Background(), forward); err != nil {
		t.Fatal(err)
	}
	reload, err := page.Reload(context.Background(), client)
	if err != nil {
		t.Fatal(err)
	}
	if err := page.WaitNavigation(context.Background(), reload); err != nil {
		t.Fatal(err)
	}
	if realm, ok := engine.LatestRealm(); ok {
		if err := realm.CollectGarbage(page); err != nil {
			t.Fatal(err)
		}
	}
	if err := page.Close(); err != nil {
		t.Fatal(err)
	}
	profile := engine.Profile()
	if profile.RealmsCreated < 7 || profile.RealmsCreated != profile.RealmsClosed {
		t.Fatalf("V8 history Realm lifecycle = %#v", profile)
	}
	if _, live := engine.LatestRealm(); live {
		t.Fatal("V8 history traversal retained a live Realm after Page close")
	}
	if ledger := browserRuntime.Ledger().Stats(); ledger.LiveObjects != 0 || ledger.PersistentObjects != 0 {
		t.Fatalf("V8 history teardown ownership = %#v", ledger)
	}
}

func TestStockV8GraphiteTabsOwnPageRealmLifecycle(t *testing.T) {
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	client := staticDocumentLoader{document: `<!doctype html><html><body><p>tab document</p></body></html>`}
	initialPage, err := browserRuntime.LoadPage(context.Background(), "https://gossamer.test/first-tab", client)
	if err != nil {
		t.Fatal(err)
	}
	initialRealm, ok := engine.LatestRealm()
	if !ok {
		t.Fatal("initial tab did not create a live V8 Realm")
	}
	baseline := engine.Profile()

	var openedPage *browser.Page
	opener := func(ctx context.Context) (*browser.Page, error) {
		openedPage, err = browserRuntime.NewBlankPage(ctx)
		return openedPage, err
	}
	backend := window.NewMemoryBackend(
		window.Event{Kind: window.EventKeyDown, Key: "t", Code: "KeyT", Modifiers: window.Modifiers{Meta: true}},
		window.Event{Kind: window.EventKeyDown, Key: "s", Code: "KeyS", Text: "second.gossamer.test/path"},
		window.Event{Kind: window.EventKeyDown, Key: "Enter", Code: "Enter"},
		window.Event{Kind: window.EventKeyDown, Key: "Tab", Code: "Tab", Modifiers: window.Modifiers{Ctrl: true}},
		window.Event{Kind: window.EventKeyDown, Key: "2", Code: "Digit2", Modifiers: window.Modifiers{Meta: true}},
		window.Event{Kind: window.EventKeyDown, Key: "w", Code: "KeyW", Modifiers: window.Modifiers{Meta: true}},
		window.Event{Kind: window.EventClose},
	)
	if err := window.RunBrowser(context.Background(), initialPage, backend, window.ShellConfig{
		Title: "Gossamer V8 tabs test", Loader: client, OpenTab: opener,
	}); err != nil {
		t.Fatal(err)
	}
	if openedPage == nil {
		t.Fatal("Graphite did not create a Page for Command-T")
	}
	if _, err := openedPage.Navigate(context.Background(), "https://closed.gossamer.test/", client); err != browser.ErrPageClosed {
		t.Fatalf("closed V8 tab navigation = %v, want ErrPageClosed", err)
	}
	afterTabClose := engine.Profile()
	if afterTabClose.RealmsCreated != baseline.RealmsCreated+2 || afterTabClose.RealmsClosed != baseline.RealmsClosed+2 {
		t.Fatalf("V8 tab open, navigation, and close Realm lifecycle = before %#v after %#v", baseline, afterTabClose)
	}
	if err := initialRealm.CollectGarbage(initialPage); err != nil {
		t.Fatal(err)
	}
	if err := initialPage.Close(); err != nil {
		t.Fatal(err)
	}
	profile := engine.Profile()
	if profile.RealmsCreated != profile.RealmsClosed {
		t.Fatalf("V8 tab final Realm lifecycle = %#v", profile)
	}
	if _, live := engine.LatestRealm(); live {
		t.Fatal("V8 tab lifecycle retained a live Realm")
	}
	if ledger := browserRuntime.Ledger().Stats(); ledger.LiveObjects != 0 || ledger.PersistentObjects != 0 {
		t.Fatalf("V8 tab teardown ownership = %#v", ledger)
	}
}
