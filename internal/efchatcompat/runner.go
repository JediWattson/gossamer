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
	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

const (
	defaultPlace          = "global"
	defaultUsername       = "anon-strand"
	defaultUserUUID       = "00000000-0000-4000-8000-000000000001"
	historyMessageID      = "strand-history-message-1"
	historyMessageContent = "history rendered by Strand"
)

type Options struct {
	Dist    string
	Message string
	Place   string
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

type Report struct {
	Passed     bool                          `json:"passed"`
	URL        string                        `json:"url,omitempty"`
	Navigation browser.NavigationSnapshot    `json:"navigation"`
	Session    SessionReport                 `json:"session"`
	History    HistoryReport                 `json:"history"`
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

	session := &anonymousSessionServer{root: root}
	server := httptest.NewServer(session)
	defer server.Close()
	report.URL = server.URL + "/" + url.PathEscape(place)

	dialer := newRecordingDialer()
	console := &consoleRecorder{}
	engine := nativeengine.New(nativeengine.Config{ConsoleSink: console.record})
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		return report, errors.Join(err, engine.Close())
	}
	page, err := browserRuntime.NewPage(dom.NewDocument(), nil)
	if err == nil {
		page.SetWebSocketDialer(dialer)
		client := loader.New(nil)
		page.SetNavigationLoader(client)
		var navigation browser.NavigationID
		navigation, err = page.Navigate(ctx, report.URL, client)
		if err == nil {
			err = page.WaitNavigation(ctx, navigation)
		}
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
		err = waitFor(ctx, page, func() bool { return dialer.hasMessage(message) })
	}

	if page != nil {
		report.Navigation = page.Navigation()
		report.DOM, _ = page.InspectorDOMLines(120)
	}
	report.Session = session.report(dialer.snapshot().Cookie)
	report.Console = console.snapshot()
	socket := dialer.snapshot()
	report.WebSocket.URL = socket.URL
	report.WebSocket.Messages = append([]string(nil), socket.Messages...)
	for _, raw := range socket.Messages {
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
	if err == nil && !report.History.Rendered {
		err = fmt.Errorf("efchatcompat: history message row was not rendered")
	}
	report.Passed = err == nil
	if err != nil {
		report.Failure = err.Error()
	}
	return report, err
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

type anonymousSessionServer struct {
	root     string
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
}

type recordingDialer struct {
	mutex    sync.Mutex
	url      string
	cookie   string
	messages []string
	reads    chan recordingRead
}

type recordingRead struct {
	message browser.WebSocketMessageType
	data    []byte
	err     error
}

func newRecordingDialer() *recordingDialer {
	return &recordingDialer{reads: make(chan recordingRead, 8)}
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
	return &recordingConnection{dialer: dialer}, nil
}

func (dialer *recordingDialer) snapshot() socketSnapshot {
	dialer.mutex.Lock()
	defer dialer.mutex.Unlock()
	return socketSnapshot{URL: dialer.url, Cookie: dialer.cookie, Messages: append([]string(nil), dialer.messages...)}
}

func (dialer *recordingDialer) hasMessage(content string) bool {
	for _, raw := range dialer.snapshot().Messages {
		var envelope wireEnvelope
		if json.Unmarshal([]byte(raw), &envelope) == nil && envelope.Event == "message" && envelope.Payload["content"] == content {
			return true
		}
	}
	return false
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
	connection.dialer.mutex.Lock()
	connection.dialer.messages = append(connection.dialer.messages, string(data))
	connection.dialer.mutex.Unlock()
	return nil
}

func (*recordingConnection) Close(_ uint16, _ string) error { return nil }
func (*recordingConnection) Protocol() string               { return "" }
