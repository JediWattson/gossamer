package efchatcompat

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
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
`), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	report, err := Run(ctx, Options{Dist: dist, Message: "hello from the anonymous gate"})
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
	payload, payloadOK := report.WebSocket.Payload.(map[string]any)
	if report.WebSocket.Event != "message" || !payloadOK || payload["content"] != "hello from the anonymous gate" {
		t.Fatalf("message envelope = %#v", report.WebSocket)
	}
	if report.Teardown.LiveObjects != 0 || report.Teardown.PersistentObjects != 0 {
		t.Fatalf("teardown ownership = %#v", report.Teardown)
	}
}
