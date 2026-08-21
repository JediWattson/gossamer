package engineparity

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sync"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/js/compiler"
	"github.com/JediWattson/gossamer/internal/loader"
	"github.com/JediWattson/gossamer/internal/nativeengine"
)

const (
	solidTaskboardURL        = "https://taskboard.gossamer.test/"
	solidTaskboardEntryURL   = "https://taskboard.gossamer.test/assets/solid-taskboard.js"
	solidTaskboardRuntimeURL = "https://taskboard.gossamer.test/assets/solid-taskboard-runtime-1.9.14.production.module.js"
	solidTaskboardDataURL    = "https://taskboard.gossamer.test/assets/solid-taskboard-board-data-1.9.14.production.module.js"
	solidTaskboardDetailsURL = "https://taskboard.gossamer.test/assets/solid-taskboard-task-details-1.9.14.production.module.js"
)

func TestStrandBootsSplitSolidTaskboardThroughNavigation(t *testing.T) {
	engine := nativeengine.New(nativeengine.Config{})
	for _, name := range []string{
		"solid-taskboard-1.9.14.production.module.js",
		"solid-taskboard-runtime-1.9.14.production.module.js",
		"solid-taskboard-board-data-1.9.14.production.module.js",
		"solid-taskboard-task-details-1.9.14.production.module.js",
	} {
		source, err := os.ReadFile("testdata/vite-solid/dist/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := compiler.CompileModuleWithOptions(string(source), compiler.Options{AllowUnresolvedGlobals: true}); err != nil {
			t.Fatalf("compile generated taskboard chunk %q: %v", name, err)
		}
	}
	runSplitSolidTaskboardParity(t, engine, func(page *browser.Page) error {
		realm, ok := engine.LatestRealm()
		if !ok {
			return fmt.Errorf("native engine did not retain the navigation Realm")
		}
		return realm.CollectGarbage(page)
	})
	if profile := engine.Profile(); profile.ModuleCompilations != 4 {
		t.Fatalf("native taskboard compilations = %d, want four chunks once", profile.ModuleCompilations)
	}
}

func runSplitSolidTaskboardParity(t *testing.T, engine browser.Engine, collect func(*browser.Page) error) {
	t.Helper()
	files := map[string]string{
		solidTaskboardEntryURL:   "solid-taskboard-1.9.14.production.module.js",
		solidTaskboardRuntimeURL: "solid-taskboard-runtime-1.9.14.production.module.js",
		solidTaskboardDataURL:    "solid-taskboard-board-data-1.9.14.production.module.js",
		solidTaskboardDetailsURL: "solid-taskboard-task-details-1.9.14.production.module.js",
	}
	modules := make(map[string][]byte, len(files))
	for moduleURL, name := range files {
		source, err := os.ReadFile("testdata/vite-solid/dist/" + name)
		if err != nil {
			t.Fatal(err)
		}
		modules[moduleURL] = source
	}
	client := &solidTaskboardLoader{modules: modules}
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	page, err := browserRuntime.LoadPage(context.Background(), solidTaskboardURL, client)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot := page.Navigation(); snapshot.State != browser.NavigationComplete ||
		snapshot.ScriptsTotal != 2 || snapshot.ScriptsPending != 0 || snapshot.ScriptsFailed != 0 {
		t.Fatalf("taskboard module navigation = %#v", snapshot)
	}
	for moduleURL := range files {
		if got := client.moduleLoadCount(moduleURL); got != 1 {
			t.Fatalf("taskboard resource loads for %q = %d, want one cached fetch", moduleURL, got)
		}
	}
	runScript := func(label, source string) {
		t.Helper()
		if _, queueErr := page.QueueScript(browser.ScriptSource{URL: solidTaskboardURL + label + ".js", Source: "(() => {\n" + source + "\n})();"}); queueErr != nil {
			t.Fatalf("queue %s: %v", label, queueErr)
		}
		if runErr := page.Realm.RunOne(context.Background()); runErr != nil {
			t.Fatalf("run %s: %v", label, runErr)
		}
	}
	runScript("assert-taskboard-boot", `
if (__solidTaskboardRuns !== 1 ||
    document.getElementById("task-summary").textContent !== "all:3" ||
    document.getElementById("task-list").children.length !== 3 ||
    document.getElementById("task-loading") !== null) {
  throw new Error("split Solid taskboard did not settle after navigation");
}

`)
	open, found := page.Document().ElementByID("filter-open")
	if !found {
		t.Fatal("taskboard open filter is missing")
	}
	if _, err := page.QueueInputEvent(browser.InputEvent{
		Type:   browser.InputClick,
		Target: browser.NodeHandle{Document: page.DocumentGeneration(), Node: open},
	}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	runScript("assert-taskboard-filter", `
const list = document.getElementById("task-list");
if (document.getElementById("task-summary").textContent !== "open:1" ||
    list.children.length !== 1 ||
    list.firstChild.getAttribute("data-task") !== "solid-app") {
  throw new Error("split Solid taskboard did not react to its resource filter");
}
`)
	detailsButton, found := page.Document().ElementByID("task-solid-app")
	if !found {
		t.Fatal("taskboard lazy-details trigger is missing")
	}
	if _, err := page.QueueInputEvent(browser.InputEvent{
		Type:   browser.InputClick,
		Target: browser.NodeHandle{Document: page.DocumentGeneration(), Node: detailsButton},
	}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	runScript("assert-taskboard-details", `
const details = document.getElementById("task-details");
if (details === null || details.getAttribute("data-task") !== "solid-app" ||
    details.textContent !== "Boot a real Solid appIn progress" ||
    document.getElementById("details-loading") !== null) {
  throw new Error("split Solid taskboard did not resolve its lazy details chunk");
}
const releaseTaskboard = globalThis.__solidTaskboardDispose;
globalThis.__solidTaskboardDispose = undefined;
releaseTaskboard();
if (document.getElementById("solid-taskboard-root").firstChild !== null ||
    __solidTaskboardCleanups !== 1) {
  throw new Error("split Solid taskboard did not dispose");
}
`)
	if err := collect(page); err != nil {
		t.Fatalf("collect disposed taskboard graph: %v", err)
	}
	if err := page.Close(); err != nil {
		t.Fatal(err)
	}
	stats := browserRuntime.Ledger().Stats()
	if stats.LiveObjects != 0 || stats.PersistentObjects != 0 {
		t.Fatalf("ownership survived taskboard page close: %#v", stats)
	}
	if err := browserRuntime.Close(); err != nil {
		t.Fatal(err)
	}
}

type solidTaskboardLoader struct {
	mutex       sync.Mutex
	modules     map[string][]byte
	moduleLoads map[string]int
}

func (client *solidTaskboardLoader) Load(_ context.Context, rawURL string) (*loader.Response, error) {
	if rawURL != solidTaskboardURL {
		return nil, fmt.Errorf("unexpected taskboard document URL %q", rawURL)
	}
	location, _ := url.Parse(rawURL)
	document := `<!doctype html><html><head></head><body>
<main id="solid-taskboard-root"></main>
<script type="module" src="/assets/solid-taskboard.js"></script>
<script type="module" src="/assets/solid-taskboard.js"></script>
</body></html>`
	return &loader.Response{
		URL: location, StatusCode: http.StatusOK,
		Header: http.Header{"Content-Type": []string{"text/html"}},
		Body:   io.NopCloser(bytes.NewBufferString(document)),
	}, nil
}

func (client *solidTaskboardLoader) LoadResource(_ context.Context, rawURL string, destination loader.Destination) (*loader.Response, error) {
	module, found := client.modules[rawURL]
	if !found || destination != loader.ScriptDestination {
		return nil, fmt.Errorf("unexpected taskboard resource %q destination %d", rawURL, destination)
	}
	client.mutex.Lock()
	if client.moduleLoads == nil {
		client.moduleLoads = make(map[string]int)
	}
	client.moduleLoads[rawURL]++
	client.mutex.Unlock()
	location, _ := url.Parse(rawURL)
	return &loader.Response{
		URL: location, StatusCode: http.StatusOK,
		Header: http.Header{"Content-Type": []string{"application/javascript"}},
		Body:   io.NopCloser(bytes.NewReader(module)),
	}, nil
}

func (client *solidTaskboardLoader) moduleLoadCount(moduleURL string) int {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	return client.moduleLoads[moduleURL]
}
