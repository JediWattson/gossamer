package nativeengine_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
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
	if text != "pending" {
		t.Fatalf("fetch settled during the navigation task: result = %q", text)
	}
	for attempt := 0; attempt < 4 && text != "42:strand:1, 2:2"; attempt++ {
		deadline := time.Now().Add(time.Second)
		for page.Realm.Tasks.Len() == 0 && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if page.Realm.Tasks.Len() == 0 {
			client.mutex.Lock()
			requests := append([]loader.Request(nil), client.requests...)
			client.mutex.Unlock()
			t.Fatalf("fetch settlement task was not queued after attempt %d: result = %q, requests = %#v", attempt, text, requests)
		}
		runContext, cancel := context.WithTimeout(context.Background(), time.Second)
		err = page.Realm.RunOne(runContext)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		text, err = page.Document().TextContent(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	if text != "42:strand:1, 2:2" {
		client.mutex.Lock()
		requests := append([]loader.Request(nil), client.requests...)
		client.mutex.Unlock()
		t.Fatalf("result = %q, navigation = %#v, requests = %#v; want 42:strand:1, 2:2", text, snapshot, requests)
	}
	client.mutex.Lock()
	defer client.mutex.Unlock()
	if len(client.requests) != 2 || client.requests[0].URL != "https://strand.test/api" || client.requests[0].Method != http.MethodPost ||
		client.requests[0].Header.Get("X-Test") != "yes" || string(client.requests[0].Body) != "payload" {
		t.Fatalf("requests = %#v", client.requests)
	}
}

func TestNativeDynamicClassicScriptsFetchEvaluateAndDispatchElementEvents(t *testing.T) {
	t.Parallel()

	var dynamicRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/page":
			writer.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(writer, `<!doctype html><html><head></head><body><div id="result">pending</div><script>
const loaded = document.createElement("script");
loaded.async = false;
loaded.addEventListener("load", function () {
  document.getElementById("result").textContent = "loaded:" + String(window.dynamicLoads) + ":" + String(loaded instanceof HTMLScriptElement) + ":" + String(loaded.async);
});
loaded.addEventListener("error", function () { document.getElementById("result").textContent = "unexpected-error"; });
loaded.src = "/dynamic.js";
document.head.append(loaded);

const failed = document.createElement("script");
failed.addEventListener("error", function () { document.getElementById("result").textContent += ":error"; });
failed.src = "/not-javascript";
document.head.append(failed);
</script></body></html>`)
		case "/dynamic.js":
			dynamicRequests.Add(1)
			writer.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			_, _ = io.WriteString(writer, `window.dynamicLoads = (window.dynamicLoads || 0) + 1;`)
		case "/not-javascript":
			writer.Header().Set("Content-Type", "text/plain")
			_, _ = io.WriteString(writer, "not JavaScript")
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	engine := nativeengine.New(nativeengine.Config{})
	runtime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	page, err := runtime.LoadPage(context.Background(), server.URL+"/page", loader.New(server.Client()))
	if err != nil {
		t.Fatal(err)
	}

	result, ok := page.Document().ElementByID("result")
	if !ok {
		t.Fatal("missing result element")
	}
	deadline := time.Now().Add(3 * time.Second)
	text := ""
	for time.Now().Before(deadline) {
		text, err = page.Document().TextContent(result)
		if err != nil {
			t.Fatal(err)
		}
		if text == "loaded:1:true:false:error" {
			break
		}
		if page.Realm.Tasks.Len() == 0 {
			time.Sleep(time.Millisecond)
			continue
		}
		runContext, cancel := context.WithTimeout(context.Background(), time.Second)
		err = page.Realm.RunOne(runContext)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
	}
	if text != "loaded:1:true:false:error" {
		t.Fatalf("dynamic script result = %q", text)
	}

	if _, err := page.QueueScript(browser.ScriptSource{URL: server.URL + "/unrelated.js", Source: `document.body.setAttribute("data-after", "true");`}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if got := dynamicRequests.Load(); got != 1 {
		t.Fatalf("dynamic script fetch count = %d, want 1", got)
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

	var reported uint64
	engine := nativeengine.New(nativeengine.Config{ConsoleSink: func(message nativeengine.ConsoleMessage) {
		if message.Method == "error" && len(message.Arguments) == 1 && strings.Contains(message.Arguments[0], "reported websocket exception") {
			atomic.AddUint64(&reported, 1)
		}
	}})
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
	if got := atomic.LoadUint64(&reported); got != 1 {
		t.Fatalf("reported WebSocket handler exceptions = %d, want 1", got)
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
socket.onmessage = function () { throw new Error("reported websocket exception"); };
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
const receiver = { calls: 0 };
const markSettled = (function () { return () => { this.calls += 1; }; }).call(receiver);
headers.append("X-List", "1");
headers.append("X-List", "2");
fetch("/api", {method: "POST", headers: headers, body: "payload"}).then(function (response) {
  if (!response.ok || response.status !== 201 || response.headers.get("content-type") !== "application/json") {
    throw new Error("bad response metadata");
  }
  return response.clone().json();
}).then(function (data) {
	markSettled();
  return fetch("/session").then(function (response) { return response.text(); }).then(function (session) {
	markSettled();
    document.getElementById("result").textContent = data.answer + ":" + session + ":" + headers.get("x-list") + ":" + receiver.calls;
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
