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
	"github.com/JediWattson/gossamer/internal/loader"
	"github.com/JediWattson/gossamer/internal/nativeengine"
)

const (
	solidNavigationURL = "https://solid.gossamer.test/"
	solidModuleURL     = "https://solid.gossamer.test/assets/solid-parity.js"
)

func TestStrandBootsProductionSolidModuleThroughNavigation(t *testing.T) {
	engine := nativeengine.New(nativeengine.Config{})
	runProductionSolidNavigationParity(t, engine, func(page *browser.Page) error {
		realm, ok := engine.LatestRealm()
		if !ok {
			return fmt.Errorf("native engine did not retain the navigation Realm")
		}
		return realm.CollectGarbage(page)
	})
}

func runProductionSolidNavigationParity(t *testing.T, engine browser.Engine, collect func(*browser.Page) error) {
	t.Helper()
	module, err := os.ReadFile("testdata/vite-solid/dist/solid-parity-1.9.14.production.module.js")
	if err != nil {
		t.Fatal(err)
	}
	client := &solidMemoryLoader{module: module}
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	page, err := browserRuntime.LoadPage(context.Background(), solidNavigationURL, client)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := page.Navigation()
	if snapshot.State != browser.NavigationComplete || snapshot.ScriptsTotal != 3 ||
		snapshot.ScriptsPending != 0 || snapshot.ScriptsFailed != 0 {
		t.Fatalf("Solid module navigation = %#v", snapshot)
	}
	if got := client.moduleLoadCount(); got != 1 {
		t.Fatalf("module resource loads = %d, want one cached fetch", got)
	}

	runScript := func(label, source string) {
		t.Helper()
		if _, queueErr := page.QueueScript(browser.ScriptSource{URL: solidNavigationURL + label + ".js", Source: "(() => {\n" + source + "\n})();"}); queueErr != nil {
			t.Fatalf("queue %s: %v", label, queueErr)
		}
		if runErr := page.Realm.RunOne(context.Background()); runErr != nil {
			t.Fatalf("run %s: %v", label, runErr)
		}
	}
	runScript("assert-module-boot", `
if (__solidModuleRuns !== 1 || __solidReady !== true ||
    document.getElementById("solid-counter").textContent !== "Count 0") {
  throw new Error("production Solid module did not boot exactly once");
}
if (__solidBootOrder.join(",") !==
    "inline:loading,module:interactive,DOMContentLoaded:interactive,load:complete,pageshow:complete") {
  throw new Error("navigation lifecycle order: " + __solidBootOrder.join(","));
}
`)
	counter, found := page.Document().ElementByID("solid-counter")
	if !found {
		t.Fatal("Solid counter is missing after module navigation")
	}
	if _, err := page.QueueInputEvent(browser.InputEvent{
		Type:   browser.InputClick,
		Target: browser.NodeHandle{Document: page.DocumentGeneration(), Node: counter},
	}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	runScript("assert-module-interaction", `
if (document.getElementById("solid-counter").textContent !== "Count 1") {
  throw new Error("production Solid module did not react to Go input");
}
const releaseSolid = globalThis.__solidDispose;
globalThis.__solidDispose = undefined;
releaseSolid();
if (document.getElementById("solid-root").firstChild !== null) {
  throw new Error("production Solid module did not dispose");
}
`)
	if err := collect(page); err != nil {
		t.Fatalf("collect disposed module graph: %v", err)
	}
	if err := page.Close(); err != nil {
		t.Fatal(err)
	}
	stats := browserRuntime.Ledger().Stats()
	if stats.LiveObjects != 0 || stats.PersistentObjects != 0 {
		t.Fatalf("ownership survived module page close: %#v", stats)
	}
	if err := browserRuntime.Close(); err != nil {
		t.Fatal(err)
	}
}

type solidMemoryLoader struct {
	mutex       sync.Mutex
	module      []byte
	moduleLoads int
}

func (client *solidMemoryLoader) Load(_ context.Context, rawURL string) (*loader.Response, error) {
	if rawURL != solidNavigationURL {
		return nil, fmt.Errorf("unexpected document URL %q", rawURL)
	}
	location, _ := url.Parse(rawURL)
	document := `<!doctype html>
<html><head>
<script>
globalThis.__solidBootOrder = ["inline:" + document.readyState];
document.addEventListener("DOMContentLoaded", function () {
  globalThis.__solidBootOrder.push("DOMContentLoaded:" + document.readyState);
});
document.addEventListener("load", function () {
  globalThis.__solidBootOrder.push("load:" + document.readyState);
});
window.addEventListener("pageshow", function () {
  globalThis.__solidBootOrder.push("pageshow:" + document.readyState);
});
</script>
</head><body>
<main id="solid-root"></main>
<script type="module" src="/assets/solid-parity.js"></script>
<script type="module" src="/assets/solid-parity.js"></script>
</body></html>`
	return &loader.Response{
		URL: location, StatusCode: http.StatusOK,
		Header: http.Header{"Content-Type": []string{"text/html"}},
		Body:   io.NopCloser(bytes.NewBufferString(document)),
	}, nil
}

func (client *solidMemoryLoader) LoadResource(_ context.Context, rawURL string, destination loader.Destination) (*loader.Response, error) {
	if rawURL != solidModuleURL || destination != loader.ScriptDestination {
		return nil, fmt.Errorf("unexpected resource %q destination %d", rawURL, destination)
	}
	client.mutex.Lock()
	client.moduleLoads++
	client.mutex.Unlock()
	location, _ := url.Parse(rawURL)
	return &loader.Response{
		URL: location, StatusCode: http.StatusOK,
		Header: http.Header{"Content-Type": []string{"application/javascript"}},
		Body:   io.NopCloser(bytes.NewReader(client.module)),
	}, nil
}

func (client *solidMemoryLoader) moduleLoadCount() int {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	return client.moduleLoads
}
