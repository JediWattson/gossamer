package engineparity

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"sync"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/loader"
	"github.com/JediWattson/gossamer/internal/nativeengine"
)

const nativeModuleGraphPageURL = "https://modules.gossamer.test/"

func TestStrandLinksLiveModuleGraphWithCycles(t *testing.T) {
	engine := nativeengine.New(nativeengine.Config{})
	runLiveModuleGraphParity(t, engine, func(page *browser.Page) error {
		realm, ok := engine.LatestRealm()
		if !ok {
			return nativeengine.ErrRealmClosed
		}
		return realm.CollectGarbage(page)
	})
}

func TestStrandCachesModuleFailuresAndReleasesGraphs(t *testing.T) {
	engine := nativeengine.New(nativeengine.Config{})
	runModuleFailureCacheParity(t, engine, func(page *browser.Page) error {
		realm, ok := engine.LatestRealm()
		if !ok {
			return nativeengine.ErrRealmClosed
		}
		return realm.CollectGarbage(page)
	})
}

func runLiveModuleGraphParity(t *testing.T, engine browser.Engine, collect func(*browser.Page) error) {
	t.Helper()
	client := &moduleGraphMemoryLoader{sources: map[string]string{
		"https://modules.gossamer.test/root.js": `
import primary, {counter, bump, forwarded, depNamespace} from "./bridge.js";
import * as namespace from "./dependency.js";
import {readA} from "./cycle-a.js";
globalThis.__nativeModuleRuns = (globalThis.__nativeModuleRuns || 0) + 1;
globalThis.__nativeModuleSnapshot = [primary, counter, namespace.counter, forwarded, depNamespace.counter, readA()].join(":");
bump();
globalThis.__nativeModuleLive = [counter, namespace.counter, depNamespace.counter].join(":");
`,
		"https://modules.gossamer.test/dependency.js": `
export let counter = 1;
export function bump() { counter += 1; }
export default "dependency";
`,
		"https://modules.gossamer.test/bridge.js": `
export * from "./dependency.js";
export {default} from "./dependency.js";
export {counter as forwarded} from "./dependency.js";
export * as depNamespace from "./dependency.js";
`,
		"https://modules.gossamer.test/cycle-a.js": `
import {readB} from "./cycle-b.js";
export const a = "a";
export function readA() { return a + readB(); }
`,
		"https://modules.gossamer.test/cycle-b.js": `
import {a} from "./cycle-a.js";
export const b = "b";
export function readB() { return b + a; }
`,
	}}
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	page, err := browserRuntime.LoadPage(context.Background(), nativeModuleGraphPageURL, client)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := page.Navigation()
	if snapshot.State != browser.NavigationComplete || snapshot.ScriptsTotal != 2 || snapshot.ScriptsFailed != 0 {
		t.Fatalf("module graph navigation = %#v", snapshot)
	}
	wantLoads := []string{
		"https://modules.gossamer.test/bridge.js",
		"https://modules.gossamer.test/cycle-a.js",
		"https://modules.gossamer.test/cycle-b.js",
		"https://modules.gossamer.test/dependency.js",
		"https://modules.gossamer.test/root.js",
	}
	if got := client.loadedOnce(); fmt.Sprint(got) != fmt.Sprint(wantLoads) {
		t.Fatalf("module fetches = %#v, want %#v", got, wantLoads)
	}
	if _, err := page.QueueScript(browser.ScriptSource{URL: nativeModuleGraphPageURL + "assert.js", Source: `
if (__nativeModuleRuns !== 1) throw new Error("module root evaluated more than once");
if (__nativeModuleSnapshot !== "dependency:1:1:1:1:aba") {
  throw new Error("module link snapshot: " + __nativeModuleSnapshot);
}
if (__nativeModuleLive !== "2:2:2") throw new Error("live module bindings: " + __nativeModuleLive);
`}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := collect(page); err != nil {
		t.Fatal(err)
	}
	if err := page.Close(); err != nil {
		t.Fatal(err)
	}
	stats := browserRuntime.Ledger().Stats()
	if stats.LiveObjects != 0 || stats.PersistentObjects != 0 {
		t.Fatalf("module graph ownership survived Page close: %#v", stats)
	}
	if err := browserRuntime.Close(); err != nil {
		t.Fatal(err)
	}
}

func runModuleFailureCacheParity(t *testing.T, engine browser.Engine, collect func(*browser.Page) error) {
	t.Helper()
	client := &moduleFailureMemoryLoader{sources: map[string]string{
		"https://module-errors.gossamer.test/evaluation.js": `
import {value} from "./dependency.js";
globalThis.__moduleFailureRuns = (globalThis.__moduleFailureRuns || 0) + value;
throw new Error("cached module evaluation failure");
`,
		"https://module-errors.gossamer.test/dependency.js": `export const value = 1;`,
		"https://module-errors.gossamer.test/link.js": `
import {collision} from "./ambiguous.js";
globalThis.__moduleLinkShouldNotRun = collision;
`,
		"https://module-errors.gossamer.test/ambiguous.js": `
export * from "./left.js";
export * from "./right.js";
`,
		"https://module-errors.gossamer.test/left.js":  `export const collision = "left";`,
		"https://module-errors.gossamer.test/right.js": `export const collision = "right";`,
	}}
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	page, err := browserRuntime.LoadPage(context.Background(), moduleFailurePageURL, client)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := page.Navigation()
	if snapshot.State != browser.NavigationComplete || snapshot.ScriptsTotal != 4 || snapshot.ScriptsFailed != 4 {
		t.Fatalf("module failure navigation = %#v", snapshot)
	}
	if _, err := page.QueueScript(browser.ScriptSource{URL: moduleFailurePageURL + "assert.js", Source: `
if (__moduleFailureRuns !== 1) {
  throw new Error("errored module evaluated more than once: " + __moduleFailureRuns);
}
if (typeof __moduleLinkShouldNotRun !== "undefined") {
  throw new Error("ambiguous module executed after a failed link");
}
`}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := collect(page); err != nil {
		t.Fatal(err)
	}
	if err := page.Close(); err != nil {
		t.Fatal(err)
	}
	stats := browserRuntime.Ledger().Stats()
	if stats.LiveObjects != 0 || stats.PersistentObjects != 0 {
		t.Fatalf("errored module graph ownership survived Page close: %#v", stats)
	}
	if err := browserRuntime.Close(); err != nil {
		t.Fatal(err)
	}
}

type moduleGraphMemoryLoader struct {
	mutex   sync.Mutex
	sources map[string]string
	loads   map[string]int
}

const moduleFailurePageURL = "https://module-errors.gossamer.test/"

type moduleFailureMemoryLoader struct {
	mutex   sync.Mutex
	sources map[string]string
}

func (client *moduleFailureMemoryLoader) Load(_ context.Context, rawURL string) (*loader.Response, error) {
	if rawURL != moduleFailurePageURL {
		return nil, fmt.Errorf("unexpected document URL %q", rawURL)
	}
	location, _ := url.Parse(rawURL)
	body := `<!doctype html><html><body>
<script type="module" src="/evaluation.js"></script>
<script type="module" src="/evaluation.js"></script>
<script type="module" src="/link.js"></script>
<script type="module" src="/link.js"></script>
</body></html>`
	return &loader.Response{
		URL: location, StatusCode: http.StatusOK,
		Header: http.Header{"Content-Type": []string{"text/html"}},
		Body:   io.NopCloser(bytes.NewBufferString(body)),
	}, nil
}

func (client *moduleFailureMemoryLoader) LoadResource(_ context.Context, rawURL string, destination loader.Destination) (*loader.Response, error) {
	if destination != loader.ScriptDestination {
		return nil, fmt.Errorf("unexpected destination %d for %q", destination, rawURL)
	}
	client.mutex.Lock()
	source, found := client.sources[rawURL]
	client.mutex.Unlock()
	if !found {
		return nil, fmt.Errorf("unexpected module URL %q", rawURL)
	}
	location, _ := url.Parse(rawURL)
	return &loader.Response{
		URL: location, StatusCode: http.StatusOK,
		Header: http.Header{"Content-Type": []string{"application/javascript"}},
		Body:   io.NopCloser(bytes.NewBufferString(source)),
	}, nil
}

func (client *moduleGraphMemoryLoader) Load(_ context.Context, rawURL string) (*loader.Response, error) {
	if rawURL != nativeModuleGraphPageURL {
		return nil, fmt.Errorf("unexpected document URL %q", rawURL)
	}
	location, _ := url.Parse(rawURL)
	body := `<!doctype html><html><body>
<script type="module" src="/root.js"></script>
<script type="module" src="/root.js"></script>
</body></html>`
	return &loader.Response{
		URL: location, StatusCode: http.StatusOK,
		Header: http.Header{"Content-Type": []string{"text/html"}},
		Body:   io.NopCloser(bytes.NewBufferString(body)),
	}, nil
}

func (client *moduleGraphMemoryLoader) LoadResource(_ context.Context, rawURL string, destination loader.Destination) (*loader.Response, error) {
	if destination != loader.ScriptDestination {
		return nil, fmt.Errorf("unexpected destination %d for %q", destination, rawURL)
	}
	source, found := client.sources[rawURL]
	if !found {
		return nil, fmt.Errorf("unexpected module URL %q", rawURL)
	}
	client.mutex.Lock()
	if client.loads == nil {
		client.loads = make(map[string]int)
	}
	client.loads[rawURL]++
	client.mutex.Unlock()
	location, _ := url.Parse(rawURL)
	return &loader.Response{
		URL: location, StatusCode: http.StatusOK,
		Header: http.Header{"Content-Type": []string{"application/javascript"}},
		Body:   io.NopCloser(bytes.NewBufferString(source)),
	}, nil
}

func (client *moduleGraphMemoryLoader) loadedOnce() []string {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	result := make([]string, 0, len(client.loads))
	for url, count := range client.loads {
		if count == 1 {
			result = append(result, url)
		}
	}
	sort.Strings(result)
	return result
}
