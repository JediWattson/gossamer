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
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/js/compiler"
	"github.com/JediWattson/gossamer/internal/loader"
	"github.com/JediWattson/gossamer/internal/nativeengine"
)

const (
	reactTaskboardURL          = "https://react-taskboard.gossamer.test/"
	reactTaskboardEntryURL     = "https://react-taskboard.gossamer.test/assets/react-taskboard.js"
	reactTaskboardRuntimeURL   = "https://react-taskboard.gossamer.test/assets/react-taskboard-runtime-19.2.7.production.module.js"
	reactTaskboardRolldownURL  = "https://react-taskboard.gossamer.test/assets/react-taskboard-rolldown-runtime-19.2.7.production.module.js"
	reactTaskboardDataURL      = "https://react-taskboard.gossamer.test/assets/react-taskboard-board-data-19.2.7.production.module.js"
	reactTaskboardDetailsURL   = "https://react-taskboard.gossamer.test/assets/react-taskboard-task-details-19.2.7.production.module.js"
	reactTaskboardEntryFile    = "react-taskboard-19.2.7.production.module.js"
	reactTaskboardRuntimeFile  = "react-taskboard-runtime-19.2.7.production.module.js"
	reactTaskboardRolldownFile = "react-taskboard-rolldown-runtime-19.2.7.production.module.js"
	reactTaskboardDataFile     = "react-taskboard-board-data-19.2.7.production.module.js"
	reactTaskboardDetailsFile  = "react-taskboard-task-details-19.2.7.production.module.js"
)

func TestStrandBootsSplitReactTaskboardThroughNavigation(t *testing.T) {
	engine := nativeengine.New(nativeengine.Config{})
	for _, name := range reactTaskboardFiles() {
		source, err := os.ReadFile("testdata/vite-react/dist/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := compiler.CompileModuleWithOptions(string(source), compiler.Options{AllowUnresolvedGlobals: true}); err != nil {
			t.Fatalf("compile generated React taskboard chunk %q: %v", name, err)
		}
	}
	runSplitReactTaskboardParity(t, engine, func(page *browser.Page) error {
		realm, ok := engine.LatestRealm()
		if !ok {
			return fmt.Errorf("native engine did not retain the navigation Realm")
		}
		return realm.CollectGarbage(page)
	})
	if profile := engine.Profile(); profile.ModuleCompilations != 5 {
		t.Fatalf("native React taskboard compilations = %d, want five chunks once", profile.ModuleCompilations)
	}
}

func reactTaskboardFiles() []string {
	return []string{
		reactTaskboardEntryFile,
		reactTaskboardRuntimeFile,
		reactTaskboardRolldownFile,
		reactTaskboardDataFile,
		reactTaskboardDetailsFile,
	}
}

func runSplitReactTaskboardParity(t *testing.T, engine browser.Engine, collect func(*browser.Page) error) {
	t.Helper()
	files := map[string]string{
		reactTaskboardEntryURL:    reactTaskboardEntryFile,
		reactTaskboardRuntimeURL:  reactTaskboardRuntimeFile,
		reactTaskboardRolldownURL: reactTaskboardRolldownFile,
		reactTaskboardDataURL:     reactTaskboardDataFile,
		reactTaskboardDetailsURL:  reactTaskboardDetailsFile,
	}
	modules := make(map[string][]byte, len(files))
	for moduleURL, name := range files {
		source, err := os.ReadFile("testdata/vite-react/dist/" + name)
		if err != nil {
			t.Fatal(err)
		}
		modules[moduleURL] = source
	}
	client := &reactTaskboardLoader{modules: modules}
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	page, err := browserRuntime.LoadPage(context.Background(), reactTaskboardURL, client)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot := page.Navigation(); snapshot.State != browser.NavigationComplete ||
		snapshot.ScriptsTotal != 2 || snapshot.ScriptsPending != 0 || snapshot.ScriptsFailed != 0 {
		t.Fatalf("React taskboard module navigation = %#v", snapshot)
	}
	for moduleURL := range files {
		if got := client.moduleLoadCount(moduleURL); got != 1 {
			t.Fatalf("React taskboard resource loads for %q = %d, want one cached fetch", moduleURL, got)
		}
	}
	runQueued := func(label string) {
		t.Helper()
		if page.Realm.Tasks.Len() == 0 {
			t.Fatalf("run %s: no queued tasks", label)
		}
		deadline := time.Now().Add(2 * time.Second)
		var idleSince time.Time
		for {
			if time.Now().After(deadline) {
				t.Fatalf("run %s: Realm did not reach an idle checkpoint", label)
			}
			if page.Realm.Tasks.Len() == 0 {
				if idleSince.IsZero() {
					idleSince = time.Now()
				}
				if time.Since(idleSince) >= 10*time.Millisecond {
					return
				}
				time.Sleep(time.Millisecond)
				continue
			}
			idleSince = time.Time{}
			if runErr := page.Realm.RunOne(context.Background()); runErr != nil {
				t.Fatalf("run %s: %v", label, runErr)
			}
		}
	}
	waitForElement := func(label, id string) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			for page.Realm.Tasks.Len() != 0 {
				if runErr := page.Realm.RunOne(context.Background()); runErr != nil {
					t.Fatalf("run %s: %v", label, runErr)
				}
			}
			if _, found := page.Document().ElementByID(id); found {
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatalf("wait %s: element %q did not appear", label, id)
	}
	runScript := func(label, source string) {
		t.Helper()
		if _, queueErr := page.QueueScript(browser.ScriptSource{
			URL: reactTaskboardURL + label + ".js", Source: "(() => {\n" + source + "\n})();",
		}); queueErr != nil {
			t.Fatalf("queue %s: %v", label, queueErr)
		}
		runQueued(label)
	}
	runScript("assert-react-taskboard-boot", `
if (__reactTaskboardRuns !== 1 ||
    document.getElementById("react-task-summary").textContent !== "all:3" ||
    document.getElementById("react-task-list").children.length !== 3 ||
    document.getElementById("react-task-loading") !== null) {
  throw new Error("split React taskboard did not settle after navigation");
}
`)
	open, found := page.Document().ElementByID("react-filter-open")
	if !found {
		t.Fatal("React taskboard open filter is missing")
	}
	if _, err := page.QueueInputEvent(browser.InputEvent{
		Type:   browser.InputClick,
		Target: browser.NodeHandle{Document: page.DocumentGeneration(), Node: open},
	}); err != nil {
		t.Fatal(err)
	}
	runQueued("React taskboard open filter")
	runScript("assert-react-taskboard-filter", `
const list = document.getElementById("react-task-list");
if (document.getElementById("react-task-summary").textContent !== "open:1" ||
    list.children.length !== 1 ||
    list.firstChild.getAttribute("data-task") !== "react-app") {
  throw new Error("split React taskboard did not react to its data filter: " +
    document.getElementById("react-task-summary").textContent + ":" + list.children.length + ":" +
    list.firstChild.getAttribute("data-task"));
}
`)
	detailsButton, found := page.Document().ElementByID("react-task-react-app")
	if !found {
		t.Fatal("React taskboard lazy-details trigger is missing")
	}
	if _, err := page.QueueInputEvent(browser.InputEvent{
		Type:   browser.InputClick,
		Target: browser.NodeHandle{Document: page.DocumentGeneration(), Node: detailsButton},
	}); err != nil {
		t.Fatal(err)
	}
	runQueued("React taskboard lazy details")
	waitForElement("React taskboard lazy details", "react-task-details")
	runScript("assert-react-taskboard-details", `
const details = document.getElementById("react-task-details");
if (details === null || details.getAttribute("data-task") !== "react-app" ||
    details.textContent !== "Boot a real React appIn progress" ||
    document.getElementById("react-details-loading") !== null) {
  throw new Error("split React taskboard did not resolve its lazy details chunk: " +
    (details === null ? "missing" : details.textContent) + ":loading=" +
    (document.getElementById("react-details-loading") !== null));
}
const releaseTaskboard = globalThis.__reactTaskboardDispose;
globalThis.__reactTaskboardDispose = undefined;
releaseTaskboard();
if (document.getElementById("react-taskboard-root").firstChild !== null ||
    __reactTaskboardEffectCleanups !== 2) {
  throw new Error("split React taskboard did not dispose cleanly");
}
`)
	if err := collect(page); err != nil {
		t.Fatalf("collect disposed React taskboard graph: %v", err)
	}
	if err := page.Close(); err != nil {
		t.Fatal(err)
	}
	stats := browserRuntime.Ledger().Stats()
	if stats.LiveObjects != 0 || stats.PersistentObjects != 0 {
		t.Fatalf("ownership survived React taskboard page close: %#v", stats)
	}
	if err := browserRuntime.Close(); err != nil {
		t.Fatal(err)
	}
}

type reactTaskboardLoader struct {
	mutex       sync.Mutex
	modules     map[string][]byte
	moduleLoads map[string]int
}

func (client *reactTaskboardLoader) Load(_ context.Context, rawURL string) (*loader.Response, error) {
	if rawURL != reactTaskboardURL {
		return nil, fmt.Errorf("unexpected React taskboard document URL %q", rawURL)
	}
	location, _ := url.Parse(rawURL)
	document := `<!doctype html><html><head></head><body>
<main id="react-taskboard-root"></main>
<script type="module" src="/assets/react-taskboard.js"></script>
<script type="module" src="/assets/react-taskboard.js"></script>
</body></html>`
	return &loader.Response{
		URL: location, StatusCode: http.StatusOK,
		Header: http.Header{"Content-Type": []string{"text/html"}},
		Body:   io.NopCloser(bytes.NewBufferString(document)),
	}, nil
}

func (client *reactTaskboardLoader) LoadResource(_ context.Context, rawURL string, destination loader.Destination) (*loader.Response, error) {
	module, found := client.modules[rawURL]
	if !found || destination != loader.ScriptDestination {
		return nil, fmt.Errorf("unexpected React taskboard resource %q destination %d", rawURL, destination)
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

func (client *reactTaskboardLoader) moduleLoadCount(moduleURL string) int {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	return client.moduleLoads[moduleURL]
}
