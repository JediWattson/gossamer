package efchatcompat

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/loader"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
)

func TestTurnstileFixtureBlocksMessagesUntilDerivedTokenVerification(t *testing.T) {
	gate := &turnstileGate{}
	dialer := newRecordingDialer(gate)
	connection, err := dialer.Dial(context.Background(), "wss://efchat.test/ws/global", nil, http.Header{})
	if err != nil {
		t.Fatal(err)
	}
	message := `{"event":"message","payload":{"content":"blocked"}}`
	if err := connection.Write(context.Background(), browser.WebSocketTextMessage, []byte(message)); err != nil {
		t.Fatal(err)
	}
	if dialer.hasMessage("blocked") || gate.verified() {
		t.Fatal("unverified message crossed the fixture gate")
	}
	if blocked := gate.report(loader.New(nil), "https://efchat.test").BlockedWrites; blocked != 1 {
		t.Fatalf("blocked writes = %d, want 1", blocked)
	}
	if !gate.validateRawToken(turnstileRawToken) {
		t.Fatal("fixture raw token was rejected")
	}
	verify := `{"event":"turnstile:verify","payload":{"token":"` + turnstileVerifyToken + `"}}`
	if err := connection.Write(context.Background(), browser.WebSocketTextMessage, []byte(verify)); err != nil {
		t.Fatal(err)
	}
	if !gate.verified() {
		t.Fatal("derived token did not unlock fixture writes")
	}
	accepted := `{"event":"message","payload":{"content":"accepted"}}`
	if err := connection.Write(context.Background(), browser.WebSocketTextMessage, []byte(accepted)); err != nil {
		t.Fatal(err)
	}
	if !dialer.hasMessage("accepted") {
		t.Fatal("verified message did not cross the fixture gate")
	}
	redacted := redactedWebSocketMessages(dialer.snapshot().Messages)
	for _, raw := range redacted {
		if strings.Contains(raw, turnstileVerifyToken) {
			t.Fatal("verification token escaped report redaction")
		}
	}
}

func TestAnonymousSessionSendsMessageEnvelope(t *testing.T) {
	dist := t.TempDir()
	assets := filepath.Join(dist, "assets")
	if err := os.MkdirAll(assets, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte(`<!doctype html>
<html><body><div id="root"></div><script type="module" src="/assets/index.js"></script></body></html>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assets, "index.js"), []byte(`
fetch("/api/user")
  .then(response => response.json())
  .then(session => {
    const root = document.getElementById("root");
    root.textContent = session.username;
    const input = document.createElement("textarea");
    input.id = "efchat-input";
    root.appendChild(input);
    fetch("/api/chat/history?place=global&limit=24&includeReactions=false")
      .then(response => response.json())
      .then(history => {
        history.messages.forEach(message => {
          const row = document.createElement("div");
          row.setAttribute("data-message-bubble-id", message.id);
          row.textContent = message.payload.content;
          root.appendChild(row);
        });
        const socket = new WebSocket("ws://" + location.host + "/ws/global");
        let verificationRequired = false;
        let verificationStarted = false;
        let verified = false;
        socket.addEventListener("message", event => {
          const envelope = JSON.parse(event.data);
          if (envelope.event === "turnstile:init") verificationRequired = true;
        });
        const startVerification = () => {
          if (verificationStarted) return;
          verificationStarted = true;
          const script = document.createElement("script");
          script.src = "https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit";
          script.addEventListener("load", () => {
            window.turnstile.ready(() => {
              const mount = document.createElement("div");
              root.appendChild(mount);
              window.turnstile.render(mount, {
                sitekey: "1x00000000000000000000AA",
                callback: token => {
                  fetch("/api/turnstile/validate", {
                    method: "POST",
                    headers: { "Content-Type": "application/json" },
                    body: JSON.stringify({ token })
                  })
                    .then(response => response.json())
                    .then(result => {
                      socket.send(JSON.stringify({ event: "turnstile:verify", payload: { token: result.token } }));
                      verified = true;
                      verificationRequired = false;
                    });
                }
              });
            });
          });
          document.head.append(script);
        };
        input.addEventListener("keydown", event => {
          if (event.key === "Enter") {
            if (verificationRequired && !verified) {
              startVerification();
              return;
            }
            socket.send(JSON.stringify({
              event: "message",
              payload: { content: input.value }
            }));
          }
        });
      });
  });
`), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	profiler := &recordingMemoryProfiler{}
	report, err := Run(ctx, Options{Dist: dist, Message: "hello from the anonymous gate", MemoryProfiler: profiler})
	if err != nil {
		t.Fatalf("run anonymous session: %v\nreport: %#v", err, report)
	}
	if !report.Passed {
		t.Fatalf("anonymous session did not pass: %#v", report)
	}
	if report.Session.Username != defaultUsername || report.Session.Role != "anon" || !report.Session.IsAnonymous || report.Session.LoggedIn {
		t.Fatalf("anonymous session = %#v", report.Session)
	}
	if !report.Session.CookieOnSocket {
		t.Fatal("anonymous session cookie did not reach WebSocket")
	}
	if !report.History.Rendered || report.History.MessageID != historyMessageID || report.History.Content != historyMessageContent {
		t.Fatalf("history message = %#v", report.History)
	}
	if !report.Turnstile.InitSent || !report.Turnstile.RawTokenValidated || !report.Turnstile.VerifyReceived || !report.Turnstile.CookieStored {
		t.Fatalf("Turnstile gate = %#v", report.Turnstile)
	}
	if report.Turnstile.BlockedWrites != 0 {
		t.Fatalf("valid Turnstile flow blocked %d writes", report.Turnstile.BlockedWrites)
	}
	payload, payloadOK := report.WebSocket.Payload.(map[string]any)
	if report.WebSocket.Event != "message" || !payloadOK || payload["content"] != "hello from the anonymous gate" {
		t.Fatalf("message envelope = %#v", report.WebSocket)
	}
	if report.Teardown.LiveObjects != 0 || report.Teardown.PersistentObjects != 0 {
		t.Fatalf("teardown ownership = %#v", report.Teardown)
	}
	wantLabels := []string{"page-created", "navigation-complete", "session-ready", "history-rendered", "message-attempt"}
	if !slices.Equal(profiler.labels, wantLabels) || profiler.heapProfiles != 1 {
		t.Fatalf("memory profile = labels %v, heap profiles %d", profiler.labels, profiler.heapProfiles)
	}
}

type recordingMemoryProfiler struct {
	labels       []string
	heapProfiles int
}

func (profiler *recordingMemoryProfiler) Record(label string, _ *browser.Page, force bool) error {
	if force {
		profiler.labels = append(profiler.labels, label)
	}
	return nil
}

func (*recordingMemoryProfiler) RecordExecution(*browser.Page, *browserruntime.TaskContext, browserruntime.ExecutionSample) error {
	return nil
}

func (profiler *recordingMemoryProfiler) WriteHeapProfile() error {
	profiler.heapProfiles++
	return nil
}
