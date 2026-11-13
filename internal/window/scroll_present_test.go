package window_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/browser/fake"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/loader"
	"github.com/JediWattson/gossamer/internal/window"
)

func TestRunPresentsScrolledFrameBeforeDispatchingScrollListener(t *testing.T) {
	t.Parallel()

	engine := fake.New()
	runtime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	page, err := runtime.LoadPage(context.Background(), "https://scroll-present.gossamer.test/", windowDocumentLoader{response: &loader.Response{
		URL: mustWindowURL(t, "https://scroll-present.gossamer.test/"), StatusCode: 200,
		Body: io.NopCloser(strings.NewReader(`<!doctype html><html><body style="margin:0">
			<div style="height:200px"></div>
			<div id="target" style="height:40px;background:#ff0000"></div>
			<div style="height:1360px"></div>
		</body></html>`)),
	}})
	if err != nil {
		t.Fatal(err)
	}
	rootID, found, err := page.Document().RelatedNode(page.Document().RootID(), dom.DocumentElement)
	if err != nil || !found {
		t.Fatalf("document element: found=%t err=%v", found, err)
	}
	root := browser.NodeHandle{Document: page.DocumentGeneration(), Node: rootID}
	targetID, found := page.Document().ElementByID("target")
	if !found {
		t.Fatal("target element has no stable identity")
	}
	target := browser.NodeHandle{Document: page.DocumentGeneration(), Node: targetID}
	backend := window.NewMemoryBackend(
		window.Event{Kind: window.EventScroll, DeltaY: 120},
		window.Event{Kind: window.EventClose},
	)
	realm, found := engine.LatestRealm()
	if !found {
		t.Fatal("fake engine did not retain the page Realm")
	}
	presentationsAtDispatch := 0
	callback, err := realm.RegisterCallback(func(host browser.Host) error {
		presentationsAtDispatch = len(backend.Frames())
		return host.SetAttribute(target, "style", "height:40px;background:#0000ff")
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := realm.Bind(browser.InputScroll, root, callback); err != nil {
		t.Fatal(err)
	}

	if err := window.Run(context.Background(), page, backend, "Scroll presentation"); err != nil {
		t.Fatal(err)
	}
	if presentationsAtDispatch != 2 {
		t.Fatalf("presentations when scroll listener dispatched = %d, want exactly initial and compositor-scroll frames", presentationsAtDispatch)
	}
	frames := backend.Frames()
	if got := len(frames); got != 3 {
		t.Fatalf("presented frames = %d, want initial, pre-listener scroll, and listener-mutation frames", got)
	}
	if bytes.Equal(frames[0].Pix, frames[1].Pix) {
		t.Fatal("pre-listener presentation did not contain visibly scrolled pixels")
	}
	if got := frames[1].RGBAAt(5, 85); got.R < 240 || got.G > 15 || got.B > 15 {
		t.Fatalf("pre-listener target pixel = %#v, want listener's red source style", got)
	}
	if got := frames[2].RGBAAt(5, 85); got.R > 15 || got.G > 15 || got.B < 240 {
		t.Fatalf("post-listener target pixel = %#v, want listener's blue mutation", got)
	}
}

func TestRunKeepsNonScrollTaskFramesCoalesced(t *testing.T) {
	t.Parallel()

	engine := fake.New()
	runtime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	page, err := runtime.LoadPage(context.Background(), "https://frame-coalescing.gossamer.test/", windowDocumentLoader{response: &loader.Response{
		URL: mustWindowURL(t, "https://frame-coalescing.gossamer.test/"), StatusCode: 200,
		Body: io.NopCloser(strings.NewReader(`<!doctype html><html><body><p id="target">before</p></body></html>`)),
	}})
	if err != nil {
		t.Fatal(err)
	}
	targetID, found := page.Document().ElementByID("target")
	if !found {
		t.Fatal("target element has no stable identity")
	}
	target, found := page.Document().Resolve(targetID)
	if !found || len(target.Children) != 1 {
		t.Fatal("target text node is unavailable")
	}
	textID, found := page.Document().ID(target.Children[0])
	if !found {
		t.Fatal("target text has no stable identity")
	}
	if _, err := page.QueueRender(); err != nil {
		t.Fatal(err)
	}
	if _, err := page.QueueTextMutation(textID, "after"); err != nil {
		t.Fatal(err)
	}
	backend := window.NewMemoryBackend(window.Event{Kind: window.EventClose})
	if err := window.Run(context.Background(), page, backend, "Frame coalescing"); err != nil {
		t.Fatal(err)
	}
	if got := len(backend.Frames()); got != 2 {
		t.Fatalf("presented frames = %d, want initial plus one coalesced mutation frame", got)
	}
}
