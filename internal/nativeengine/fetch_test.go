package nativeengine_test

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/loader"
	"github.com/JediWattson/gossamer/internal/nativeengine"
)

func TestNativeFetchUsesDocumentRequesterAndWebResponseObjects(t *testing.T) {
	t.Parallel()

	engine := nativeengine.New(nativeengine.Config{})
	runtime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	client := &nativeFetchLoader{}
	page, err := runtime.LoadPage(context.Background(), "https://strand.test/app/index.html", client)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := page.Navigation()
	if snapshot.ScriptsFailed != 0 {
		t.Fatalf("script failures = %#v", snapshot.ScriptFailures)
	}
	body, ok := page.Document().ElementByID("result")
	if !ok {
		t.Fatal("missing result element")
	}
	text, err := page.Document().TextContent(body)
	if err != nil {
		t.Fatal(err)
	}
	if text != "42:strand:1, 2" {
		client.mutex.Lock()
		requests := append([]loader.Request(nil), client.requests...)
		client.mutex.Unlock()
		t.Fatalf("result = %q, navigation = %#v, requests = %#v; want 42:strand:1, 2", text, snapshot, requests)
	}
	client.mutex.Lock()
	defer client.mutex.Unlock()
	if len(client.requests) != 2 || client.requests[0].URL != "https://strand.test/api" || client.requests[0].Method != http.MethodPost ||
		client.requests[0].Header.Get("X-Test") != "yes" || string(client.requests[0].Body) != "payload" {
		t.Fatalf("requests = %#v", client.requests)
	}
}

func TestNativeStorageAndCookiesSurviveDocumentNavigation(t *testing.T) {
	t.Parallel()

	engine := nativeengine.New(nativeengine.Config{})
	runtime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	client := &nativeStorageLoader{Loader: loader.New(nil)}
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

func TestNativeWebSocketQueuesOpenMessageAndCloseEvents(t *testing.T) {
	t.Parallel()

	engine := nativeengine.New(nativeengine.Config{})
	runtime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	page, err := runtime.NewPage(dom.NewDocument(), nil)
	if err != nil {
		t.Fatal(err)
	}
	connection := newNativeScriptWebSocket()
	page.SetWebSocketDialer(nativeScriptWebSocketDialer{connection: connection})
	client := nativeSocketDocumentLoader{}
	navigation, err := page.Navigate(context.Background(), "https://strand.test/socket-page", client)
	if err != nil {
		t.Fatal(err)
	}
	if err := page.WaitNavigation(context.Background(), navigation); err != nil {
		t.Fatal(err)
	}
	var write nativeScriptWebSocketItem
	for attempt := 0; attempt < 6; attempt++ {
		select {
		case write = <-connection.writes:
			attempt = 6
			continue
		default:
		}
		runContext, cancel := context.WithTimeout(context.Background(), time.Second)
		err := page.Realm.RunOne(runContext)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
	}
	if write.message != browser.WebSocketTextMessage || string(write.data) != "ping" {
		t.Fatalf("websocket write = %#v", write)
	}
	connection.reads <- nativeScriptWebSocketItem{message: browser.WebSocketTextMessage, data: []byte("hello")}
	result, _ := page.Document().ElementByID("result")
	var text string
	for attempt := 0; attempt < 8; attempt++ {
		text, _ = page.Document().TextContent(result)
		if text == "hello:1000:done" {
			break
		}
		runContext, cancel := context.WithTimeout(context.Background(), time.Second)
		err := page.Realm.RunOne(runContext)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
	}
	if text != "hello:1000:done" {
		t.Fatalf("websocket result = %q", text)
	}
}

type nativeSocketDocumentLoader struct{}

func (nativeSocketDocumentLoader) Load(_ context.Context, rawURL string) (*loader.Response, error) {
	location, _ := url.Parse(rawURL)
	return &loader.Response{
		URL: location, StatusCode: http.StatusOK, Header: make(http.Header),
		Body: io.NopCloser(strings.NewReader(`<!doctype html><html><body><div id="result">pending</div><script>
const socket = new WebSocket("/socket", "chat");
socket.onopen = function () { document.getElementById("result").textContent = "open"; socket.send("ping"); };
socket.addEventListener("message", function (event) { document.getElementById("result").textContent = event.data; socket.close(1000, "done"); });
socket.onclose = function (event) { document.getElementById("result").textContent += ":" + event.code + ":" + event.reason; };
</script></body></html>`)),
	}, nil
}

type nativeScriptWebSocketDialer struct{ connection *nativeScriptWebSocket }

func (dialer nativeScriptWebSocketDialer) Dial(_ context.Context, _ string, _ []string, _ http.Header) (browser.WebSocketConnection, error) {
	return dialer.connection, nil
}

type nativeScriptWebSocketItem struct {
	message browser.WebSocketMessageType
	data    []byte
	err     error
}

type nativeScriptWebSocket struct {
	reads  chan nativeScriptWebSocketItem
	writes chan nativeScriptWebSocketItem
}

func newNativeScriptWebSocket() *nativeScriptWebSocket {
	return &nativeScriptWebSocket{reads: make(chan nativeScriptWebSocketItem, 4), writes: make(chan nativeScriptWebSocketItem, 4)}
}
func (connection *nativeScriptWebSocket) Read(ctx context.Context) (browser.WebSocketMessageType, []byte, error) {
	select {
	case item := <-connection.reads:
		return item.message, item.data, item.err
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	}
}
func (connection *nativeScriptWebSocket) Write(_ context.Context, message browser.WebSocketMessageType, data []byte) error {
	connection.writes <- nativeScriptWebSocketItem{message: message, data: append([]byte(nil), data...)}
	return nil
}
func (*nativeScriptWebSocket) Close(_ uint16, _ string) error { return nil }
func (*nativeScriptWebSocket) Protocol() string               { return "chat" }

type nativeStorageLoader struct {
	*loader.Loader
	mutex sync.Mutex
	loads int
}

func (client *nativeStorageLoader) Load(_ context.Context, rawURL string) (*loader.Response, error) {
	client.mutex.Lock()
	client.loads++
	load := client.loads
	client.mutex.Unlock()
	location, _ := url.Parse(rawURL)
	script := `
localStorage.setItem("local", "local");
sessionStorage.setItem("session", "session");
document.cookie = "identity=strand; Path=/";
document.getElementById("result").textContent = "initialized";
`
	if load > 1 {
		script = `
const firstKey = localStorage.key(0);
document.getElementById("result").textContent = localStorage.getItem("local") + ":" + sessionStorage.getItem("session") + ":" +
  String(document.cookie.indexOf("identity=strand") >= 0) + ":" + String(localStorage.length === 1 && firstKey === "local");
`
	}
	return &loader.Response{
		URL: location, StatusCode: http.StatusOK, Header: make(http.Header),
		Body: io.NopCloser(strings.NewReader(`<!doctype html><html><body><div id="result"></div><script>` + script + `</script></body></html>`)),
	}, nil
}

type nativeFetchLoader struct {
	mutex    sync.Mutex
	requests []loader.Request
	session  bool
}

func (client *nativeFetchLoader) Load(_ context.Context, rawURL string) (*loader.Response, error) {
	location, _ := url.Parse(rawURL)
	return &loader.Response{
		URL: location, StatusCode: http.StatusOK, Header: make(http.Header),
		Body: io.NopCloser(strings.NewReader(`<!doctype html><html><body><div id="result">pending</div><script>
const headers = new Headers({"X-Test": "yes"});
headers.append("X-List", "1");
headers.append("X-List", "2");
fetch("/api", {method: "POST", headers: headers, body: "payload"}).then(function (response) {
  if (!response.ok || response.status !== 201 || response.headers.get("content-type") !== "application/json") {
    throw new Error("bad response metadata");
  }
  return response.clone().json();
}).then(function (data) {
  return fetch("/session").then(function (response) { return response.text(); }).then(function (session) {
    document.getElementById("result").textContent = data.answer + ":" + session + ":" + headers.get("x-list");
  });
}, function (error) {
  document.getElementById("result").textContent = "error:" + error.message;
});
</script></body></html>`)),
	}, nil
}

func (client *nativeFetchLoader) Do(_ context.Context, request loader.Request) (*loader.Response, error) {
	client.mutex.Lock()
	client.requests = append(client.requests, request)
	location, _ := url.Parse(request.URL)
	var status int
	var contentType, body string
	switch location.Path {
	case "/api":
		client.session = true
		status, contentType, body = http.StatusCreated, "application/json", `{"answer":42}`
	case "/session":
		if client.session {
			status, body = http.StatusOK, "strand"
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
