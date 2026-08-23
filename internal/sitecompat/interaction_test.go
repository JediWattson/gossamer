package sitecompat

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/loader"
	"github.com/JediWattson/gossamer/internal/nativeengine"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
)

func TestInteractionScenarioDrivesSessionChallengeAndAcceptedSocketMessage(t *testing.T) {
	dist := t.TempDir()
	assets := filepath.Join(dist, "assets")
	if err := os.MkdirAll(assets, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte(`<!doctype html><html><body><div id="app"></div><script type="module" src="/assets/app.js"></script></body></html>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assets, "app.js"), []byte(`
fetch("/session")
  .then(response => response.json())
  .then(session => {
    const app = document.getElementById("app");
    app.textContent = session.name;
    const input = document.createElement("textarea");
    input.id = "message-input";
    app.appendChild(input);
    fetch("/history")
      .then(response => response.json())
      .then(history => {
        const row = document.createElement("div");
        row.id = "history-row";
        row.textContent = history.content;
        app.appendChild(row);
        const socket = new WebSocket("ws://" + location.host + "/socket");
        let challengeRequired = false;
        let challengeStarted = false;
        let unlocked = false;
        socket.addEventListener("message", event => {
          const envelope = JSON.parse(event.data);
          if (envelope.event === "challenge") {
            challengeRequired = true;
            const marker = document.createElement("div");
            marker.id = "challenge-required";
            app.appendChild(marker);
          }
        });
        input.addEventListener("keydown", event => {
          if (event.key !== "Enter") return;
          if (challengeRequired && !unlocked) {
            if (challengeStarted) return;
            challengeStarted = true;
            const script = document.createElement("script");
            script.src = "https://challenge.test/widget.js";
            script.addEventListener("load", () => {
              globalThis.testChallenge.run(token => {
                fetch("/verify", {
                  method: "POST",
                  headers: { "Content-Type": "application/json" },
                  body: JSON.stringify({ token })
                })
                  .then(response => response.json())
                  .then(result => {
                    socket.send(JSON.stringify({ event: "unlock", payload: { token: result.token } }));
                    unlocked = true;
                    challengeRequired = false;
                  });
              });
            });
            document.head.append(script);
            return;
          }
          socket.send(JSON.stringify({ event: "message", payload: { content: input.value } }));
        });
      });
  });
`), 0o644); err != nil {
		t.Fatal(err)
	}

	definition := InteractionDefinition{
		Path: "/room", Viewport: &InteractionViewport{Width: 640, Height: 480},
		Routes: []InteractionRoute{
			{Path: "/session", Headers: map[string]string{"Content-Type": "application/json", "Set-Cookie": "session_id=local; Path=/; HttpOnly"}, Body: `{"name":"anonymous-user"}`},
			{Path: "/history", Headers: map[string]string{"Content-Type": "application/json"}, Body: `{"content":"fixture history"}`},
			{Method: "POST", Path: "/verify", Headers: map[string]string{"Content-Type": "application/json", "Set-Cookie": "challenge_key=derived; Path=/; HttpOnly"}, Body: `{"token":"derived-token"}`},
		},
		Resources: []InteractionResource{{
			URL: "https://challenge.test/widget.js", Headers: map[string]string{"Content-Type": "text/javascript"},
			Body: `globalThis.testChallenge = { run: callback => queueMicrotask(() => callback("raw-token")) };`,
		}},
		Socket: InteractionSocket{
			OpenMessages: []string{`{"event":"challenge"}`},
			Unlock:       &InteractionMatch{Fields: map[string]any{"event": "unlock", "payload.token": "derived-token"}},
		},
		Steps: []InteractionStep{
			{Name: "session ready", Checkpoint: "session-ready", Before: &InteractionCondition{ElementID: "message-input", TextRootID: "app", ContainsText: "anonymous-user", SocketConnected: true}},
			{Name: "history ready", Checkpoint: "history-ready", Before: &InteractionCondition{ElementID: "history-row", TextRootID: "app", ContainsText: "fixture history"}},
			{Name: "unlock socket", Before: &InteractionCondition{ElementID: "challenge-required"}, Script: `
const input = document.getElementById("message-input");
input.value = {{message}};
input.dispatchEvent(new Event("input", { bubbles: true }));
input.dispatchEvent(new KeyboardEvent("keydown", { bubbles: true, cancelable: true, key: "Enter", code: "Enter" }));
`, After: &InteractionCondition{SocketUnlocked: true}},
			{Name: "send message", Checkpoint: "message-sent", Script: `
document.getElementById("message-input").dispatchEvent(new KeyboardEvent("keydown", { bubbles: true, cancelable: true, key: "Enter", code: "Enter" }));
	`, After: &InteractionCondition{AcceptedMessage: &InteractionMatch{Fields: map[string]any{"event": "message", "payload.content": "hello from scenario"}}}},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	profiler := &interactionProfiler{}
	report, err := RunInteraction(ctx, nativeengine.New(nativeengine.Config{}), InteractionOptions{
		Dist: dist, Definition: definition, Variables: map[string]string{"message": "hello from scenario"}, MemoryProfiler: profiler,
	})
	if err != nil {
		t.Fatalf("run interaction: %v\nreport: %#v", err, report)
	}
	if !report.Passed || len(report.Steps) != len(definition.Steps) {
		t.Fatalf("interaction report = %#v", report)
	}
	for _, step := range report.Steps {
		if !step.Passed {
			t.Fatalf("failed interaction step = %#v", step)
		}
	}
	if !report.WebSocket.CookieAttached || !report.WebSocket.Unlocked || report.WebSocket.Accepted != 1 || report.WebSocket.Blocked != 0 {
		t.Fatalf("interaction socket = %#v", report.WebSocket)
	}
	if report.Teardown.LiveObjects != 0 || report.Teardown.PersistentObjects != 0 {
		t.Fatalf("interaction teardown = %#v", report.Teardown)
	}
	wantLabels := []string{"page-created", "navigation-complete", "session-ready", "history-ready", "message-sent", "scenario-complete"}
	if !slices.Equal(profiler.labels, wantLabels) || profiler.heapProfiles != 1 {
		t.Fatalf("interaction profile = labels %v heap profiles %d", profiler.labels, profiler.heapProfiles)
	}
}

func TestInteractionSocketGateBlocksUntilDeclarativeMatch(t *testing.T) {
	dialer := newInteractionDialer(InteractionSocket{Unlock: &InteractionMatch{Fields: map[string]any{"event": "unlock", "payload.key": "ok"}}})
	connection, err := dialer.Dial(context.Background(), "wss://scenario.test/socket", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := connection.Write(context.Background(), browser.WebSocketTextMessage, []byte(`{"event":"message"}`)); err != nil {
		t.Fatal(err)
	}
	if err := connection.Write(context.Background(), browser.WebSocketTextMessage, []byte(`{"event":"unlock","payload":{"key":"ok"}}`)); err != nil {
		t.Fatal(err)
	}
	if err := connection.Write(context.Background(), browser.WebSocketTextMessage, []byte(`{"event":"message","payload":{"content":"accepted"}}`)); err != nil {
		t.Fatal(err)
	}
	snapshot := dialer.snapshot()
	if !snapshot.unlocked || snapshot.blocked != 1 || len(snapshot.accepted) != 1 {
		t.Fatalf("gated socket snapshot = %#v", snapshot)
	}
}

func TestInteractionLoaderBlocksUnconfiguredExternalTraffic(t *testing.T) {
	client := &interactionLoader{base: loader.New(nil), origin: "https://local.test"}
	if _, err := client.LoadResource(context.Background(), "https://external.test/app.js", loader.ScriptDestination); err == nil {
		t.Fatal("unconfigured external resource was allowed")
	}
	if _, err := client.Do(context.Background(), loader.Request{URL: "https://external.test/api", Method: "POST"}); err == nil {
		t.Fatal("unconfigured external request was allowed")
	}
}

type interactionProfiler struct {
	labels       []string
	heapProfiles int
}

func (profiler *interactionProfiler) Record(label string, _ *browser.Page, force bool) error {
	if force {
		profiler.labels = append(profiler.labels, label)
	}
	return nil
}

func (*interactionProfiler) RecordExecution(*browser.Page, *browserruntime.TaskContext, browserruntime.ExecutionSample) error {
	return nil
}

func (profiler *interactionProfiler) WriteHeapProfile() error {
	profiler.heapProfiles++
	return nil
}
