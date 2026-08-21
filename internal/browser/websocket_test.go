package browser

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/dom"
)

func TestWebSocketEventsRunAsOrderedPageTasks(t *testing.T) {
	t.Parallel()

	connection := newMemoryWebSocket()
	script := &webSocketTestRealm{}
	engine := &webSocketTestEngine{realm: script}
	runtime, err := NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	location, _ := url.Parse("https://strand.test/app")
	page, err := runtime.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	page.SetWebSocketDialer(memoryWebSocketDialer{connection: connection})
	if _, err := page.QueueScript(ScriptSource{URL: "https://strand.test/open.js", Source: "open"}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	connection.reads <- memoryWebSocketRead{message: WebSocketTextMessage, data: []byte("hello")}
	connection.queued.Wait()
	for attempt := 0; attempt < 4; attempt++ {
		script.mutex.Lock()
		count := len(script.events)
		script.mutex.Unlock()
		if count >= 2 {
			break
		}
		runContext, cancel := context.WithTimeout(context.Background(), time.Second)
		err := page.Realm.RunOne(runContext)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
	}
	script.mutex.Lock()
	events := append([]WebSocketEvent(nil), script.events...)
	id := script.id
	script.mutex.Unlock()
	if len(events) != 2 || events[0].Type != WebSocketOpenEvent || events[1].Type != WebSocketMessageEvent || string(events[1].Data) != "hello" {
		t.Fatalf("events = %#v", events)
	}
	host := &taskHost{page: page, generation: page.DocumentGeneration()}
	if err := host.SendWebSocket(id, WebSocketTextMessage, []byte("out")); err != nil {
		t.Fatal(err)
	}
	if write := <-connection.writes; write.message != WebSocketTextMessage || string(write.data) != "out" {
		t.Fatalf("write = %#v", write)
	}
	if err := page.Close(); err != nil {
		t.Fatal(err)
	}
	connection.mutex.Lock()
	closed := connection.closed
	connection.mutex.Unlock()
	if !closed {
		t.Fatal("Page.Close did not close the websocket")
	}
}

type webSocketTestEngine struct{ realm *webSocketTestRealm }

func (engine *webSocketTestEngine) NewRealm() (JSRealm, error) { return engine.realm, nil }
func (*webSocketTestEngine) Close() error                      { return nil }

type webSocketTestRealm struct {
	mutex  sync.Mutex
	id     WebSocketID
	events []WebSocketEvent
}

func (realm *webSocketTestRealm) Evaluate(host Host, _ ScriptSource) error {
	id, _, err := host.(WebSocketHost).OpenWebSocket("/socket", []string{"chat"})
	realm.mutex.Lock()
	realm.id = id
	realm.mutex.Unlock()
	return err
}
func (*webSocketTestRealm) DispatchEvent(Host, InputEvent) (EventDispatchResult, error) {
	return EventDispatchResult{}, nil
}
func (*webSocketTestRealm) Invoke(Host, ValueHandle) error { return nil }
func (*webSocketTestRealm) DrainMicrotasks(Host) error     { return nil }
func (*webSocketTestRealm) Close() error                   { return nil }
func (realm *webSocketTestRealm) DispatchWebSocket(_ Host, id WebSocketID, event WebSocketEvent) error {
	realm.mutex.Lock()
	defer realm.mutex.Unlock()
	if id != realm.id {
		return errors.New("wrong websocket ID")
	}
	realm.events = append(realm.events, event)
	return nil
}

type memoryWebSocketDialer struct{ connection *memoryWebSocket }

func (dialer memoryWebSocketDialer) Dial(_ context.Context, rawURL string, protocols []string, header http.Header) (WebSocketConnection, error) {
	if rawURL != "wss://strand.test/socket" || len(protocols) != 1 || protocols[0] != "chat" || header.Get("Origin") != "https://strand.test" {
		return nil, errors.New("bad websocket handshake inputs")
	}
	return dialer.connection, nil
}

type memoryWebSocketRead struct {
	message WebSocketMessageType
	data    []byte
	err     error
}

type memoryWebSocket struct {
	reads  chan memoryWebSocketRead
	writes chan memoryWebSocketRead
	queued sync.WaitGroup
	mutex  sync.Mutex
	closed bool
}

func newMemoryWebSocket() *memoryWebSocket {
	connection := &memoryWebSocket{reads: make(chan memoryWebSocketRead, 4), writes: make(chan memoryWebSocketRead, 4)}
	connection.queued.Add(1)
	return connection
}

func (connection *memoryWebSocket) Read(ctx context.Context) (WebSocketMessageType, []byte, error) {
	select {
	case item := <-connection.reads:
		connection.queued.Done()
		return item.message, item.data, item.err
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	}
}
func (connection *memoryWebSocket) Write(_ context.Context, message WebSocketMessageType, data []byte) error {
	connection.writes <- memoryWebSocketRead{message: message, data: append([]byte(nil), data...)}
	return nil
}
func (connection *memoryWebSocket) Close(_ uint16, _ string) error {
	connection.mutex.Lock()
	connection.closed = true
	connection.mutex.Unlock()
	return nil
}
func (*memoryWebSocket) Protocol() string { return "chat" }
