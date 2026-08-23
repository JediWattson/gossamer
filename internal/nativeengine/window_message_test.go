package nativeengine_test

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	htmlparser "github.com/JediWattson/gossamer/internal/html"
	"github.com/JediWattson/gossamer/internal/nativeengine"
)

func TestNativeIframeWindowProxyPostsBoundedOriginCheckedMessages(t *testing.T) {
	t.Parallel()

	engine := nativeengine.New(nativeengine.Config{})
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	parentRoot, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body><iframe id="child"></iframe></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	parentLocation, _ := url.Parse("https://parent.test/app")
	parent, err := browserRuntime.NewPage(parentRoot, parentLocation)
	if err != nil {
		t.Fatal(err)
	}
	iframe, found := parent.Document().ElementByID("child")
	if !found {
		t.Fatal("missing iframe")
	}
	iframeHandle := browser.NodeHandle{Document: parent.DocumentGeneration(), Node: iframe}
	childRoot, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	childLocation, _ := url.Parse("https://child.test/widget")
	child, err := parent.AttachChildFrame(iframeHandle, childRoot, childLocation)
	if err != nil {
		t.Fatal(err)
	}

	run := func(page *browser.Page, label, source string) {
		t.Helper()
		if _, err := page.QueueScript(browser.ScriptSource{URL: label, Source: source}); err != nil {
			t.Fatal(err)
		}
		if err := page.Realm.RunOne(context.Background()); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
	}
	run(parent, "https://parent.test/listener.js", `
		let reply = "";
		window.addEventListener("message", function (event) {
			if (event.origin !== "https://child.test") throw new Error("wrong reply origin");
			reply = event.data.ack;
		});
	`)
	run(child, "https://child.test/listener.js", `
		let received = "";
		window.addEventListener("message", function (event) {
			if (event.origin !== "https://parent.test") throw new Error("wrong source origin");
			if (event.source !== window.parent) throw new Error("source proxy lost identity");
			received = event.data.kind + ":" + event.data.count;
			event.source.postMessage({ ack: received }, "https://parent.test");
		});
	`)
	run(parent, "https://parent.test/send.js", `
		const target = document.getElementById("child").contentWindow;
		if (target === null) throw new Error("missing contentWindow");
		target.postMessage({ kind: "ready", count: 3 }, "https://child.test");
	`)
	if err := child.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := parent.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	run(child, "https://child.test/verify.js", `
		if (received !== "ready:3") throw new Error("message data was not cloned");
	`)
	run(parent, "https://parent.test/verify.js", `
		if (reply !== "ready:3") throw new Error("reply did not cross WindowProxy");
	`)

	run(parent, "https://parent.test/drop.js", `
		document.getElementById("child").contentWindow.postMessage({ kind: "blocked" }, "https://other.test");
	`)
	if child.Realm.Tasks.Len() != 0 {
		t.Fatalf("targetOrigin mismatch queued %d child tasks", child.Realm.Tasks.Len())
	}
}

func TestNativeDisconnectedIframeContentWindowIsNull(t *testing.T) {
	t.Parallel()

	engine := nativeengine.New(nativeengine.Config{})
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	location, _ := url.Parse("https://parent.test/")
	page, err := browserRuntime.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `
		const frame = document.createElement("iframe");
		if (frame.contentWindow !== null) throw new Error("disconnected iframe owns a child context");
	`}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
}
