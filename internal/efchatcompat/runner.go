// Package efchatcompat drives the real efchat production bundle through a
// deterministic anonymous session and captures its WebSocket message envelope.
package efchatcompat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/loader"
	"github.com/JediWattson/gossamer/internal/nativeengine"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

const (
	defaultPlace          = "global"
	defaultUsername       = "anon-strand"
	defaultUserUUID       = "00000000-0000-4000-8000-000000000001"
	historyMessageID      = "strand-history-message-1"
	historyMessageContent = "history rendered by Strand"
	turnstileRawToken     = "XXXX.DUMMY.TOKEN.XXXX"
	turnstileVerifyToken  = "strand-derived-websocket-verification"
)

type Options struct {
	Dist                    string
	Message                 string
	Place                   string
	MemoryProfiler          MemoryProfiler
	ExecutionSampleInterval uint64
}

// MemoryProfiler lets the production compatibility gate record the same
// engine-neutral checkpoints as the interactive shell. The gate owns neither
// the profiler nor its output files; callers close it after Run returns.
type MemoryProfiler interface {
	Record(string, *browser.Page, bool) error
	RecordExecution(*browser.Page, *browserruntime.TaskContext, browserruntime.ExecutionSample) error
	WriteHeapProfile() error
}

type SessionReport struct {
	Username       string   `json:"username"`
	Role           string   `json:"role"`
	LoggedIn       bool     `json:"loggedIn"`
	IsAnonymous    bool     `json:"isAnonymous"`
	UserUUID       string   `json:"userUuid"`
	Requests       int      `json:"requests"`
	CookieOnSocket bool     `json:"cookieOnSocket"`
	Paths          []string `json:"paths,omitempty"`
}

type WebSocketReport struct {
	URL      string   `json:"url,omitempty"`
	Message  string   `json:"message,omitempty"`
	Messages []string `json:"messages,omitempty"`
	Event    string   `json:"event,omitempty"`
	Payload  any      `json:"payload,omitempty"`
}

type HistoryReport struct {
	MessageID string `json:"messageId"`
	Content   string `json:"content"`
	Rendered  bool   `json:"rendered"`
}

type TurnstileReport struct {
	InitSent          bool `json:"initSent"`
	RawTokenValidated bool `json:"rawTokenValidated"`
	VerifyReceived    bool `json:"verifyReceived"`
	CookieStored      bool `json:"cookieStored"`
	BlockedWrites     int  `json:"blockedWrites"`
}

type Report struct {
	Passed     bool                          `json:"passed"`
	URL        string                        `json:"url,omitempty"`
	Navigation browser.NavigationSnapshot    `json:"navigation"`
	Session    SessionReport                 `json:"session"`
	History    HistoryReport                 `json:"history"`
	Turnstile  TurnstileReport               `json:"turnstile"`
	Console    []nativeengine.ConsoleMessage `json:"console,omitempty"`
	WebSocket  WebSocketReport               `json:"webSocket"`
	DOM        []string                      `json:"dom,omitempty"`
	Ownership  ownership.Stats               `json:"ownership"`
	Teardown   ownership.Stats               `json:"teardown"`
	Failure    string                        `json:"failure,omitempty"`
}

type wireEnvelope struct {
	Event   string         `json:"event"`
	Payload map[string]any `json:"payload"`
}

func redactedWebSocketMessages(messages []string) []string {
	redacted := make([]string, 0, len(messages))
	for _, raw := range messages {
		var envelope wireEnvelope
		if json.Unmarshal([]byte(raw), &envelope) == nil && envelope.Event == "turnstile:verify" {
			if envelope.Payload == nil {
				envelope.Payload = make(map[string]any)
			}
			envelope.Payload["token"] = "[redacted]"
			encoded, err := json.Marshal(envelope)
			if err == nil {
				redacted = append(redacted, string(encoded))
				continue
			}
		}
		redacted = append(redacted, raw)
	}
	return redacted
}

func Run(ctx context.Context, options Options) (report Report, resultErr error) {
	if ctx == nil {
		return report, fmt.Errorf("efchatcompat: nil context")
	}
	root, err := filepath.Abs(strings.TrimSpace(options.Dist))
	if err != nil {
		return report, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return report, fmt.Errorf("efchatcompat: inspect dist: %w", err)
	}
	if !info.IsDir() {
		return report, fmt.Errorf("efchatcompat: dist %q is not a directory", root)
	}
	message := options.Message
	if strings.TrimSpace(message) == "" {
		return report, fmt.Errorf("efchatcompat: message is empty")
	}
	place := strings.Trim(strings.TrimSpace(options.Place), "/")
	if place == "" {
		place = defaultPlace
	}

	gate := &turnstileGate{}
	session := &anonymousSessionServer{root: root, gate: gate}
	server := httptest.NewServer(session)
	defer server.Close()
	client := newFixtureLoader(loader.New(nil))
	report.URL = server.URL + "/" + url.PathEscape(place)

	dialer := newRecordingDialer(gate)
	console := &consoleRecorder{}
	var page *browser.Page
	interpreter := browserruntime.InterpreterConfig{}
	if options.MemoryProfiler != nil && options.ExecutionSampleInterval != 0 {
		interpreter.ExecutionSampleInterval = options.ExecutionSampleInterval
		interpreter.ExecutionSample = func(context *browserruntime.TaskContext, sample browserruntime.ExecutionSample) error {
			if page == nil {
				return nil
			}
			return options.MemoryProfiler.RecordExecution(page, context, sample)
		}
	}
	engine := nativeengine.New(nativeengine.Config{Interpreter: interpreter, ConsoleSink: console.record})
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		return report, errors.Join(err, engine.Close())
	}
	page, err = browserRuntime.NewPage(dom.NewDocument(), nil)
	if err == nil {
		err = recordMemory(options.MemoryProfiler, "page-created", page)
	}
	if err == nil {
		page.SetWebSocketDialer(dialer)
		page.SetNavigationLoader(client)
		var navigation browser.NavigationID
		navigation, err = page.Navigate(ctx, report.URL, client)
		if err == nil {
			err = page.WaitNavigation(ctx, navigation)
		}
	}
	if err == nil {
		err = recordMemory(options.MemoryProfiler, "navigation-complete", page)
	}
	if err == nil {
		snapshot := page.Navigation()
		if snapshot.ScriptsFailed != 0 {
			err = fmt.Errorf("efchatcompat: initial script failure: %s", snapshot.ScriptFailures[0].Message)
		}
	}
	if err == nil {
		err = waitFor(ctx, page, func() bool {
			_, inputReady := page.Document().ElementByID("efchat-input")
			root, rootReady := page.Document().ElementByID("root")
			text, _ := page.Document().TextContent(root)
			return inputReady && rootReady && strings.Contains(text, defaultUsername) && dialer.snapshot().URL != ""
		})
	}
	if err == nil {
		err = recordMemory(options.MemoryProfiler, "session-ready", page)
	}
	if err == nil {
		err = waitFor(ctx, page, func() bool {
			root, rootReady := page.Document().ElementByID("root")
			text, _ := page.Document().TextContent(root)
			return rootReady && strings.Contains(text, historyMessageContent)
		})
		if err != nil {
			err = fmt.Errorf("efchatcompat: wait for history message: %w", err)
		}
	}
	if err == nil {
		err = recordMemory(options.MemoryProfiler, "history-rendered", page)
	}
	if err == nil {
		source := fmt.Sprintf(`
const historyRow = document.querySelector('[data-message-bubble-id=%s]');
if (!historyRow) throw new Error('efchat history gate: missing message row');
historyRow.id = 'strand-history-gate-row';
`, quotedJavaScriptString(historyMessageID))
		_, err = page.QueueScript(browser.ScriptSource{URL: server.URL + "/strand-history-row.js", Source: source})
	}
	if err == nil {
		err = waitFor(ctx, page, func() bool {
			_, found := page.Document().ElementByID("strand-history-gate-row")
			return found
		})
		if err != nil {
			err = fmt.Errorf("efchatcompat: verify history message row: %w", err)
		}
	}
	report.History = HistoryReport{
		MessageID: historyMessageID,
		Content:   historyMessageContent,
	}
	if page != nil {
		_, report.History.Rendered = page.Document().ElementByID("strand-history-gate-row")
	}
	if err == nil {
		source := fmt.Sprintf(`
const input = document.getElementById("efchat-input");
if (!input) throw new Error("efchat anonymous gate: missing #efchat-input");
input.focus();
input.value = %s;
input.dispatchEvent(new Event("input", { bubbles: true }));
const enter = new KeyboardEvent("keydown", { bubbles: true, cancelable: true, key: "Enter", code: "Enter" });
input.dispatchEvent(enter);
`, quotedJavaScriptString(message))
		_, err = page.QueueScript(browser.ScriptSource{URL: server.URL + "/strand-anon-send.js", Source: source})
	}
	if err == nil {
		err = waitFor(ctx, page, dialer.isVerified)
		if err != nil {
			err = fmt.Errorf("efchatcompat: wait for Turnstile verification: %w", err)
		}
	}
	if err == nil {
		_, err = page.QueueScript(browser.ScriptSource{URL: server.URL + "/strand-anon-send-after-turnstile.js", Source: `
const verifiedInput = document.getElementById("efchat-input");
if (!verifiedInput) throw new Error("efchat anonymous gate: missing #efchat-input after Turnstile");
const verifiedEnter = new KeyboardEvent("keydown", { bubbles: true, cancelable: true, key: "Enter", code: "Enter" });
verifiedInput.dispatchEvent(verifiedEnter);
`})
	}
	if err == nil {
		err = waitFor(ctx, page, func() bool { return dialer.hasMessage(message) })
	}
	if page != nil {
		err = errors.Join(err, recordMemory(options.MemoryProfiler, "message-attempt", page))
		if options.MemoryProfiler != nil {
			err = errors.Join(err, options.MemoryProfiler.WriteHeapProfile())
		}
	}

	if page != nil {
		report.Navigation = page.Navigation()
		report.DOM, _ = page.InspectorDOMLines(120)
	}
	report.Session = session.report(dialer.snapshot().Cookie)
	report.Turnstile = gate.report(client, server.URL)
	report.Console = console.snapshot()
	socket := dialer.snapshot()
	report.WebSocket.URL = socket.URL
	report.WebSocket.Messages = redactedWebSocketMessages(socket.Messages)
	for _, raw := range socket.Accepted {
		var envelope wireEnvelope
		if json.Unmarshal([]byte(raw), &envelope) == nil && envelope.Event == "message" {
			if content, _ := envelope.Payload["content"].(string); content == message {
				report.WebSocket.Message = raw
				report.WebSocket.Event = envelope.Event
				report.WebSocket.Payload = envelope.Payload
				break
			}
		}
	}
	report.Ownership = browserRuntime.Ledger().Stats()
	if page != nil {
		err = errors.Join(err, page.Close())
	}
	err = errors.Join(err, browserRuntime.Close())
	report.Teardown = browserRuntime.Ledger().Stats()
	if report.Teardown.LiveObjects != 0 || report.Teardown.PersistentObjects != 0 {
		err = errors.Join(err, fmt.Errorf("efchatcompat: teardown ownership = %#v", report.Teardown))
	}
	if err == nil && (report.Navigation.State != browser.NavigationComplete || report.Navigation.ScriptsFailed != 0) {
		err = fmt.Errorf("efchatcompat: navigation did not complete cleanly")
	}
	if err == nil && (!report.Session.IsAnonymous || !report.Session.CookieOnSocket) {
		err = fmt.Errorf("efchatcompat: anonymous cookie session did not reach WebSocket")
	}
	if err == nil && report.WebSocket.Event != "message" {
		err = fmt.Errorf("efchatcompat: efchat message envelope was not observed")
	}
	if err == nil && (!report.Turnstile.InitSent || !report.Turnstile.RawTokenValidated ||
		!report.Turnstile.VerifyReceived || !report.Turnstile.CookieStored) {
		err = fmt.Errorf("efchatcompat: Turnstile gate did not complete: %#v", report.Turnstile)
	}
	if err == nil && !report.History.Rendered {
		err = fmt.Errorf("efchatcompat: history message row was not rendered")
	}
	report.Passed = err == nil
	if err != nil {
		report.Failure = err.Error()
	}
	return report, err
}

func recordMemory(profiler MemoryProfiler, label string, page *browser.Page) error {
	if profiler == nil || page == nil {
		return nil
	}
	return profiler.Record(label, page, true)
}

type consoleRecorder struct {
	mutex    sync.Mutex
	messages []nativeengine.ConsoleMessage
}

func (recorder *consoleRecorder) record(message nativeengine.ConsoleMessage) {
	recorder.mutex.Lock()
	recorder.messages = append(recorder.messages, message)
	recorder.mutex.Unlock()
}

func (recorder *consoleRecorder) snapshot() []nativeengine.ConsoleMessage {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	result := make([]nativeengine.ConsoleMessage, len(recorder.messages))
	for index, message := range recorder.messages {
		result[index] = nativeengine.ConsoleMessage{
			Method:    message.Method,
			Arguments: append([]string(nil), message.Arguments...),
		}
	}
	return result
}

func waitFor(ctx context.Context, page *browser.Page, ready func() bool) error {
	for !ready() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if page.Realm.Tasks.Len() != 0 {
			if err := page.Realm.RunOne(ctx); err != nil {
				return err
			}
			continue
		}
		timer := time.NewTimer(2 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return nil
}

func quotedJavaScriptString(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

const turnstileFixtureJavaScript = `(function () {
  const widgets = new Map();
  let nextWidget = 0;
  window.turnstile = {
    ready: function (callback) { queueMicrotask(callback); },
    render: function (container, options) {
      const id = "strand-turnstile-widget-" + String(++nextWidget);
      const marker = document.createElement("div");
      marker.id = id;
      marker.textContent = "Security check";
      container.append(marker);
      widgets.set(id, marker);
      queueMicrotask(function () { options.callback("XXXX.DUMMY.TOKEN.XXXX"); });
      return id;
    },
    remove: function (id) {
      const marker = widgets.get(id);
      if (marker) marker.remove();
      widgets.delete(id);
    },
    reset: function () {}
  };
})();`

type fixtureLoader struct {
	base *loader.Loader
}

func newFixtureLoader(base *loader.Loader) *fixtureLoader {
	return &fixtureLoader{base: base}
}

func (client *fixtureLoader) Load(ctx context.Context, rawURL string) (*loader.Response, error) {
	return client.base.Load(ctx, rawURL)
}

func (client *fixtureLoader) LoadResource(ctx context.Context, rawURL string, destination loader.Destination) (*loader.Response, error) {
	location, err := url.Parse(rawURL)
	if err == nil && destination == loader.ScriptDestination && strings.EqualFold(location.Hostname(), "challenges.cloudflare.com") &&
		location.Path == "/turnstile/v0/api.js" {
		return &loader.Response{
			URL: location, StatusCode: http.StatusOK,
			Header: http.Header{"Content-Type": []string{"text/javascript; charset=utf-8"}},
			Body:   io.NopCloser(strings.NewReader(turnstileFixtureJavaScript)),
		}, nil
	}
	return client.base.LoadResource(ctx, rawURL, destination)
}

func (client *fixtureLoader) Do(ctx context.Context, request loader.Request) (*loader.Response, error) {
	return client.base.Do(ctx, request)
}

func (client *fixtureLoader) Cookies(location *url.URL) []*http.Cookie {
	return client.base.Cookies(location)
}

func (client *fixtureLoader) SetCookies(location *url.URL, cookies []*http.Cookie) {
	client.base.SetCookies(location, cookies)
}

type turnstileGate struct {
	mutex             sync.Mutex
	initSent          bool
	rawTokenValidated bool
	verifyReceived    bool
	blockedWrites     int
}

func (gate *turnstileGate) markInitSent() {
	gate.mutex.Lock()
	gate.initSent = true
	gate.mutex.Unlock()
}

func (gate *turnstileGate) validateRawToken(token string) bool {
	gate.mutex.Lock()
	defer gate.mutex.Unlock()
	if strings.TrimSpace(token) != turnstileRawToken {
		return false
	}
	gate.rawTokenValidated = true
	return true
}

func (gate *turnstileGate) verifyDerivedToken(token string) bool {
	gate.mutex.Lock()
	defer gate.mutex.Unlock()
	if !gate.rawTokenValidated || strings.TrimSpace(token) != turnstileVerifyToken {
		return false
	}
	gate.verifyReceived = true
	return true
}

func (gate *turnstileGate) markBlockedWrite() {
	gate.mutex.Lock()
	gate.blockedWrites++
	gate.mutex.Unlock()
}

func (gate *turnstileGate) verified() bool {
	gate.mutex.Lock()
	defer gate.mutex.Unlock()
	return gate.verifyReceived
}

func (gate *turnstileGate) report(store loader.CookieStore, rawURL string) TurnstileReport {
	gate.mutex.Lock()
	report := TurnstileReport{
		InitSent: gate.initSent, RawTokenValidated: gate.rawTokenValidated,
		VerifyReceived: gate.verifyReceived, BlockedWrites: gate.blockedWrites,
	}
	gate.mutex.Unlock()
	location, err := url.Parse(rawURL)
	if err == nil {
		for _, cookie := range store.Cookies(location) {
			if cookie != nil && cookie.Name == "efchat_code" && cookie.Value == turnstileVerifyToken {
				report.CookieStored = true
				break
			}
		}
	}
	return report
}

type anonymousSessionServer struct {
	root     string
	gate     *turnstileGate
	mutex    sync.Mutex
	requests int
	paths    []string
}

func (server *anonymousSessionServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	server.mutex.Lock()
	server.paths = append(server.paths, request.Method+" "+request.URL.RequestURI())
	server.mutex.Unlock()
	switch request.URL.Path {
	case "/api/user":
		server.mutex.Lock()
		server.requests++
		server.mutex.Unlock()
		http.SetCookie(writer, &http.Cookie{Name: "access_token", Value: "strand-anonymous-session", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
		http.SetCookie(writer, &http.Cookie{Name: "csrf_token", Value: "strand-csrf", Path: "/", SameSite: http.SameSiteLaxMode})
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"username":"`+defaultUsername+`","role":"anon","loggedIn":false,"isAnonymous":true,"usernameEditable":false,"userUuid":"`+defaultUserUUID+`","canReceiveSuperchats":false,"appearance":{"color":"#7c3aed","badges":[]},"promotions":[]}`)
	case "/api/dev/admin-session":
		writer.WriteHeader(http.StatusNotFound)
	case "/api/chat/history":
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"messages":[{"id":"`+historyMessageID+`","event":"message","createdAt":"2026-08-21T12:00:00Z","updateNumber":0,"historyCursor":"strand-history-cursor-1","historyPosition":{"score":1,"id":"`+historyMessageID+`"},"payload":{"username":"history-user","content":"`+historyMessageContent+`","userUuid":"00000000-0000-4000-8000-000000000002","isAnonymous":false,"appearance":{"color":"#2563eb","badges":[]}}}],"reactions":[],"hasMore":false,"placeId":"global","pageInfo":{"startCursor":"strand-history-cursor-1","endCursor":"strand-history-cursor-1","hasOlder":false,"hasNewer":false}}`)
	case "/api/chat/reactions":
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"reactions":[]}`)
	case "/api/turnstile/validate":
		if request.Method != http.MethodPost {
			writer.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var payload struct {
			Token string `json:"token"`
		}
		if json.NewDecoder(request.Body).Decode(&payload) != nil || server.gate == nil || !server.gate.validateRawToken(payload.Token) {
			http.Error(writer, "Turnstile verification failed", http.StatusForbidden)
			return
		}
		http.SetCookie(writer, &http.Cookie{
			Name: "efchat_code", Value: turnstileVerifyToken, Path: "/", HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
		})
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]string{"token": turnstileVerifyToken})
	case "/api/attachments/place/global/photo/meta":
		writer.WriteHeader(http.StatusNotFound)
	default:
		if request.Method == http.MethodGet && filepath.Ext(request.URL.Path) == "" {
			http.ServeFile(writer, request, filepath.Join(server.root, "index.html"))
			return
		}
		http.FileServer(http.Dir(server.root)).ServeHTTP(writer, request)
	}
}

func (server *anonymousSessionServer) report(cookie string) SessionReport {
	server.mutex.Lock()
	requests := server.requests
	paths := append([]string(nil), server.paths...)
	server.mutex.Unlock()
	return SessionReport{
		Username: defaultUsername, Role: "anon", LoggedIn: false, IsAnonymous: true,
		UserUUID: defaultUserUUID, Requests: requests,
		CookieOnSocket: strings.Contains(cookie, "access_token=strand-anonymous-session"),
		Paths:          paths,
	}
}

type socketSnapshot struct {
	URL      string
	Cookie   string
	Messages []string
	Accepted []string
}

type recordingDialer struct {
	mutex    sync.Mutex
	url      string
	cookie   string
	messages []string
	accepted []string
	reads    chan recordingRead
	gate     *turnstileGate
}

type recordingRead struct {
	message browser.WebSocketMessageType
	data    []byte
	err     error
}

func newRecordingDialer(gate *turnstileGate) *recordingDialer {
	return &recordingDialer{reads: make(chan recordingRead, 8), gate: gate}
}

func (dialer *recordingDialer) Dial(_ context.Context, rawURL string, _ []string, header http.Header) (browser.WebSocketConnection, error) {
	location, err := url.Parse(rawURL)
	if err != nil || (location.Scheme != "ws" && location.Scheme != "wss") {
		return nil, fmt.Errorf("efchatcompat: invalid WebSocket URL %q", rawURL)
	}
	dialer.mutex.Lock()
	dialer.url = rawURL
	dialer.cookie = header.Get("Cookie")
	dialer.mutex.Unlock()
	if dialer.gate != nil {
		dialer.gate.markInitSent()
		dialer.reads <- recordingRead{
			message: browser.WebSocketTextMessage,
			data:    []byte(`{"event":"turnstile:init"}`),
		}
	}
	return &recordingConnection{dialer: dialer}, nil
}

func (dialer *recordingDialer) snapshot() socketSnapshot {
	dialer.mutex.Lock()
	defer dialer.mutex.Unlock()
	return socketSnapshot{
		URL: dialer.url, Cookie: dialer.cookie,
		Messages: append([]string(nil), dialer.messages...),
		Accepted: append([]string(nil), dialer.accepted...),
	}
}

func (dialer *recordingDialer) hasMessage(content string) bool {
	for _, raw := range dialer.snapshot().Accepted {
		var envelope wireEnvelope
		if json.Unmarshal([]byte(raw), &envelope) == nil && envelope.Event == "message" && envelope.Payload["content"] == content {
			return true
		}
	}
	return false
}

func (dialer *recordingDialer) isVerified() bool {
	return dialer.gate != nil && dialer.gate.verified()
}

type recordingConnection struct{ dialer *recordingDialer }

func (connection *recordingConnection) Read(ctx context.Context) (browser.WebSocketMessageType, []byte, error) {
	select {
	case item := <-connection.dialer.reads:
		return item.message, item.data, item.err
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	}
}

func (connection *recordingConnection) Write(_ context.Context, message browser.WebSocketMessageType, data []byte) error {
	if message != browser.WebSocketTextMessage {
		return fmt.Errorf("efchatcompat: expected text WebSocket message")
	}
	raw := string(data)
	connection.dialer.mutex.Lock()
	connection.dialer.messages = append(connection.dialer.messages, raw)
	connection.dialer.mutex.Unlock()

	gate := connection.dialer.gate
	if gate != nil && !gate.verified() {
		var envelope wireEnvelope
		if json.Unmarshal(data, &envelope) == nil && envelope.Event == "turnstile:verify" {
			token, _ := envelope.Payload["token"].(string)
			_ = gate.verifyDerivedToken(token)
		} else {
			gate.markBlockedWrite()
		}
		return nil
	}
	connection.dialer.mutex.Lock()
	connection.dialer.accepted = append(connection.dialer.accepted, raw)
	connection.dialer.mutex.Unlock()
	return nil
}

func (*recordingConnection) Close(_ uint16, _ string) error { return nil }
func (*recordingConnection) Protocol() string               { return "" }
