package nativeengine_test

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	htmlparser "github.com/JediWattson/gossamer/internal/html"
	"github.com/JediWattson/gossamer/internal/nativeengine"
)

func TestStrandInitializesProductionReactBundle(t *testing.T) {
	source, err := os.ReadFile("../v8engine/testdata/react-19.2.7.production.js")
	if err != nil {
		t.Fatal(err)
	}
	root, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body><main id="root"></main></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	engine := nativeengine.New(nativeengine.Config{})
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	location, _ := url.Parse("https://gossamer.test/react/")
	page, err := browserRuntime.NewPage(root, location)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.ResolveReference(&url.URL{Path: "react.js"}).String(), Source: string(source)}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.ResolveReference(&url.URL{Path: "assert.js"}).String(), Source: `
if (typeof React !== "object" || typeof React.createElement !== "function" ||
    typeof ReactDOM !== "object" || typeof ReactDOM.createRoot !== "function" ||
    typeof ReactDOM.flushSync !== "function") {
  throw new Error("production React globals were not initialized");
}
`}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := page.Close(); err != nil {
		t.Fatal(err)
	}
}
