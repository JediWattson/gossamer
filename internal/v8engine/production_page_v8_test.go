//go:build v8 && cgo && darwin && arm64

package v8engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/loader"
)

func TestStockV8BootsHTTPViteShapedProductionReactPage(t *testing.T) {
	files := make(map[string][]byte)
	for route, path := range map[string]string{
		"/":                                  "testdata/vite-react/index.html",
		"/assets/react-19.2.7.production.js": "testdata/react-19.2.7.production.js",
		"/src/defer.js":                      "testdata/vite-react/src/defer.js",
		"/src/main.js":                       "testdata/vite-react/src/main.js",
		"/src/App.js":                        "testdata/vite-react/src/App.js",
	} {
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read production fixture %s: %v", path, err)
		}
		files[route] = contents
	}
	var requestMutex sync.Mutex
	requests := make(map[string]int)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestMutex.Lock()
		requests[request.URL.Path]++
		requestMutex.Unlock()
		contents, found := files[request.URL.Path]
		if !found {
			http.NotFound(writer, request)
			return
		}
		if request.URL.Path == "/" {
			writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		} else {
			writer.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		}
		_, _ = writer.Write(contents)
	}))
	defer server.Close()

	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatalf("NewWithEngine: %v", err)
	}
	defer browserRuntime.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	page, err := browserRuntime.LoadPage(ctx, server.URL+"/", loader.New(server.Client()))
	if err != nil {
		t.Fatalf("LoadPage production React fixture: %v", err)
	}
	if snapshot := page.Navigation(); snapshot.State != browser.NavigationComplete ||
		snapshot.ScriptsTotal != 5 || snapshot.ScriptsPending != 0 || snapshot.ScriptsFailed != 0 {
		t.Fatalf("production React navigation = %#v", snapshot)
	}
	if !v8FrameContainsText(page.Frame(), "Gossamer production React") ||
		!v8FrameContainsText(page.Frame(), "Count") {
		t.Fatal("production React module commit did not reach the final navigation frame")
	}
	requestMutex.Lock()
	mainRequests := requests["/src/main.js"]
	appRequests := requests["/src/App.js"]
	requestMutex.Unlock()
	if mainRequests != 1 || appRequests != 1 {
		t.Fatalf("module graph HTTP requests main=%d app=%d, want one cached request each", mainRequests, appRequests)
	}

	assertScript := func(label, source string) {
		t.Helper()
		if _, queueErr := page.QueueScript(browser.ScriptSource{
			URL: server.URL + "/assert-" + label + ".js", Source: source,
		}); queueErr != nil {
			t.Fatalf("queue %s: %v", label, queueErr)
		}
		for page.Realm.Tasks.Len() != 0 {
			if runErr := page.Realm.RunOne(ctx); runErr != nil {
				t.Fatalf("run %s: %v", label, runErr)
			}
		}
	}
	assertScript("boot", `
		if (document.readyState !== "complete")
			throw new Error("document did not reach complete: " + document.readyState);
		const expected = "inline:loading,defer:interactive,module:interactive,DOMContentLoaded:interactive,load:complete";
		if (__gossamerBootOrder.join(",") !== expected)
			throw new Error("lifecycle order diverged: " + __gossamerBootOrder.join(","));
		if (__gossamerModuleRuns !== 1)
			throw new Error("duplicate module tag re-evaluated its cached module: " + __gossamerModuleRuns);
		if (document.getElementById("production-app").dataset.count !== "0")
			throw new Error("production React initial state did not commit");
	`)

	buttonID, found := page.Document().ElementByID("production-increment")
	if !found {
		t.Fatal("production React button is not connected")
	}
	if _, err := page.QueueInputEvent(browser.InputEvent{
		Type: browser.InputClick,
		Target: browser.NodeHandle{
			Document: page.DocumentGeneration(),
			Node:     buttonID,
		},
	}); err != nil {
		t.Fatalf("queue production React click: %v", err)
	}
	for page.Realm.Tasks.Len() != 0 {
		if err := page.Realm.RunOne(ctx); err != nil {
			t.Fatalf("dispatch production React click: %v", err)
		}
	}
	appID, found := page.Document().ElementByID("production-app")
	if !found {
		t.Fatal("production React app disappeared after its update")
	}
	if count, present, err := page.Document().GetAttribute(appID, "data-count"); err != nil || !present || count != "1" {
		t.Fatalf("production React count = %q, present=%t, err=%v", count, present, err)
	}
	buttonText, err := page.Document().TextContent(buttonID)
	if err != nil || strings.TrimSpace(buttonText) != "Count 1" {
		t.Fatalf("production React button text = %q, err=%v", buttonText, err)
	}

	assertScript("unmount", `
		ReactDOM.flushSync(() => __gossamerReactRoot.unmount());
		globalThis.__gossamerReactRoot = undefined;
		globalThis.__gossamerBootOrder = undefined;
		if (document.getElementById("root").childNodes.length !== 0)
			throw new Error("production React unmount left native children connected");
	`)
	realm, live := engine.LatestRealm()
	if !live {
		t.Fatal("production React realm disappeared before teardown")
	}
	if err := realm.CollectGarbage(page); err != nil {
		t.Fatalf("CollectGarbage after production React unmount: %v", err)
	}
	if err := page.Close(); err != nil {
		t.Fatalf("close production React page: %v", err)
	}
	if _, live := engine.LatestRealm(); live {
		t.Fatal("production React realm remained registered after page close")
	}
	if ledger := browserRuntime.Ledger().Stats(); ledger.LiveObjects != 0 || ledger.PersistentObjects != 0 {
		t.Fatalf("production React teardown ownership = %#v", ledger)
	}
}
