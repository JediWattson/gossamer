package engineparity

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/loader"
	"github.com/JediWattson/gossamer/internal/nativeengine"
)

type productionReactDocumentLoader struct{}

func (productionReactDocumentLoader) Load(_ context.Context, rawURL string) (*loader.Response, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	return &loader.Response{
		URL:        parsed,
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
		Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><body><main id="root"></main></body></html>`)),
	}, nil
}

func TestStrandCreatesProductionReactRoot(t *testing.T) {
	runProductionReactRootParity(t, nativeengine.New(nativeengine.Config{}))
}

func runProductionReactRootParity(t *testing.T, engine browser.Engine) {
	t.Helper()
	source, err := os.ReadFile("../v8engine/testdata/react-19.2.7.production.js")
	if err != nil {
		t.Fatal(err)
	}
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://gossamer.test/react/")
	page, err := browserRuntime.LoadPage(context.Background(), location.String(), productionReactDocumentLoader{})
	if err != nil {
		t.Fatal(err)
	}
	runQueuedCheckpoint := func(label string) {
		t.Helper()
		queued := page.Realm.Tasks.Len()
		if queued == 0 {
			t.Fatalf("run %s: no queued tasks", label)
		}
		for index := 0; index < queued; index++ {
			if err := page.Realm.RunOne(context.Background()); err != nil {
				t.Fatalf("run %s: %v", label, err)
			}
		}
	}
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.ResolveReference(&url.URL{Path: "preflight.js"}).String(), Source: `
if (typeof document !== "object" || typeof document.getElementById !== "function") {
  throw new Error("document facade was not initialized");
}
`}); err != nil {
		t.Fatal(err)
	}
	runQueuedCheckpoint("document preflight")
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.ResolveReference(&url.URL{Path: "react.js"}).String(), Source: string(source)}); err != nil {
		t.Fatal(err)
	}
	runQueuedCheckpoint("production React bundle")
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.ResolveReference(&url.URL{Path: "assert.js"}).String(), Source: `
if (typeof React !== "object" || typeof React.createElement !== "function" ||
    typeof ReactDOM !== "object" || typeof ReactDOM.createRoot !== "function" ||
    typeof ReactDOM.flushSync !== "function") {
  throw new Error("production React globals were not initialized");
}

globalThis.__strandReactRoot = ReactDOM.createRoot(document.getElementById("root"));
if (typeof __strandReactRoot !== "object" || typeof __strandReactRoot.render !== "function") {
  throw new Error("production React root was not created");
}
`}); err != nil {
		t.Fatal(err)
	}
	runQueuedCheckpoint("production React assertion")
	if err := page.Close(); err != nil {
		t.Fatal(err)
	}
	if stats := browserRuntime.Ledger().Stats(); stats.LiveObjects != 0 || stats.PersistentObjects != 0 {
		t.Fatalf("production React root teardown ownership = %#v", stats)
	}
	if err := browserRuntime.Close(); err != nil {
		t.Fatal(err)
	}
}
