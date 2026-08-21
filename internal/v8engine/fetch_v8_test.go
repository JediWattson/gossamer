//go:build v8 && cgo && darwin && arm64

package v8engine

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/loader"
)

func TestStockV8FetchUsesBrowserSessionHost(t *testing.T) {
	engine := newTestEngine(t)
	runtime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	client := &v8FetchLoader{}
	page, err := runtime.LoadPage(context.Background(), "https://strand.test/app/index.html", client)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot := page.Navigation(); snapshot.ScriptsFailed != 0 {
		t.Fatalf("script failures = %#v", snapshot.ScriptFailures)
	}
	result, ok := page.Document().ElementByID("result")
	if !ok {
		t.Fatal("missing result element")
	}
	text, err := page.Document().TextContent(result)
	if err != nil {
		t.Fatal(err)
	}
	if text != "42:strand:1, 2" {
		t.Fatalf("result = %q, want 42:strand:1, 2", text)
	}
	client.mutex.Lock()
	defer client.mutex.Unlock()
	if len(client.requests) != 2 || client.requests[0].URL != "https://strand.test/api" ||
		client.requests[0].Method != http.MethodPost || client.requests[0].Header.Get("X-Test") != "yes" ||
		string(client.requests[0].Body) != "payload" {
		t.Fatalf("requests = %#v", client.requests)
	}
}

func TestStockV8StorageAndCookiesSurviveDocumentNavigation(t *testing.T) {
	engine := newTestEngine(t)
	runtime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	client := &v8StorageLoader{Loader: loader.New(nil)}
	page, err := runtime.LoadPage(context.Background(), "https://strand.test/storage", client)
	if err != nil {
		t.Fatal(err)
	}
	navigation, err := page.Reload(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := page.WaitNavigation(context.Background(), navigation); err != nil {
		t.Fatal(err)
	}
	result, ok := page.Document().ElementByID("result")
	if !ok {
		t.Fatal("missing result element")
	}
	text, err := page.Document().TextContent(result)
	if err != nil {
		t.Fatal(err)
	}
	if text != "local:session:true:true" {
		t.Fatalf("storage result = %q", text)
	}
}

type v8StorageLoader struct {
	*loader.Loader
	mutex sync.Mutex
	loads int
}

func (client *v8StorageLoader) Load(_ context.Context, rawURL string) (*loader.Response, error) {
	client.mutex.Lock()
	client.loads++
	load := client.loads
	client.mutex.Unlock()
	location, _ := url.Parse(rawURL)
	script := `localStorage.setItem("local", "local"); sessionStorage.setItem("session", "session"); document.cookie = "identity=strand; Path=/";`
	if load > 1 {
		script = `const firstKey = localStorage.key(0); document.getElementById("result").textContent = localStorage.getItem("local") + ":" + sessionStorage.getItem("session") + ":" + String(document.cookie.indexOf("identity=strand") >= 0) + ":" + String(localStorage.length === 1 && firstKey === "local");`
	}
	return &loader.Response{
		URL: location, StatusCode: http.StatusOK, Header: make(http.Header),
		Body: io.NopCloser(strings.NewReader(`<!doctype html><html><body><div id="result"></div><script>` + script + `</script></body></html>`)),
	}, nil
}

type v8FetchLoader struct {
	mutex    sync.Mutex
	requests []loader.Request
	session  bool
}

func (client *v8FetchLoader) Load(_ context.Context, rawURL string) (*loader.Response, error) {
	location, _ := url.Parse(rawURL)
	return &loader.Response{
		URL: location, StatusCode: http.StatusOK, Header: make(http.Header),
		Body: io.NopCloser(strings.NewReader(`<!doctype html><html><body><div id="result">pending</div><script>
const headers = new Headers({"X-Test": "yes"});
headers.append("X-List", "1");
headers.append("X-List", "2");
fetch("/api", {method: "POST", headers, body: "payload"}).then(response => {
  if (!response.ok || response.status !== 201 || response.headers.get("content-type") !== "application/json") throw new Error("bad metadata");
  return response.clone().json();
}).then(data => fetch("/session").then(response => response.text()).then(session => {
  document.getElementById("result").textContent = data.answer + ":" + session + ":" + headers.get("x-list");
}));
</script></body></html>`)),
	}, nil
}

func (client *v8FetchLoader) Do(_ context.Context, request loader.Request) (*loader.Response, error) {
	client.mutex.Lock()
	client.requests = append(client.requests, request)
	location, _ := url.Parse(request.URL)
	status, contentType, body := http.StatusOK, "", ""
	switch location.Path {
	case "/api":
		client.session = true
		status, contentType, body = http.StatusCreated, "application/json", `{"answer":42}`
	case "/session":
		if client.session {
			body = "strand"
		} else {
			status, body = http.StatusUnauthorized, "missing"
		}
	}
	client.mutex.Unlock()
	header := make(http.Header)
	if contentType != "" {
		header.Set("Content-Type", contentType)
	}
	return &loader.Response{URL: location, StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body))}, nil
}
