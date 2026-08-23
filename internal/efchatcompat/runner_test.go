package efchatcompat

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
)

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
        input.addEventListener("keydown", event => {
          if (event.key === "Enter") {
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
