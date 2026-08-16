package browser_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	htmlparser "github.com/JediWattson/gossamer/internal/html"
	"github.com/JediWattson/gossamer/internal/loader"
	"github.com/JediWattson/gossamer/internal/render"
	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

func TestNavigationSequencesDocumentResourcesAndRenderThroughRealmTasks(t *testing.T) {
	t.Parallel()

	engine, err := browser.New()
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	initialURL, _ := url.Parse("https://example.test/initial")
	page, err := engine.NewPage(dom.NewDocument(), initialURL)
	if err != nil {
		t.Fatal(err)
	}
	client := newStagedNavigationLoader(t)
	navigation, err := page.Navigate(context.Background(), "https://example.test/page", client)
	if err != nil {
		t.Fatal(err)
	}

	waitFor(t, "document completion task", func() bool { return page.Realm.Tasks.Len() == 1 })
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	snapshot := page.Navigation()
	if snapshot.ID != navigation || snapshot.State != browser.NavigationLoadingResources ||
		snapshot.ResourcesTotal != 2 || snapshot.ResourcesPending != 2 {
		t.Fatalf("after document completion = %#v", snapshot)
	}
	if page.Frame() != nil {
		t.Fatal("document rendered before its resource completion sequence")
	}

	waitSignal(t, client.stylesheetStarted, "stylesheet request")
	select {
	case <-client.imageStarted:
		t.Fatal("image request started before the earlier stylesheet completed")
	default:
	}
	close(client.stylesheetRelease)
	waitSignal(t, client.imageStarted, "image request")
	waitFor(t, "stylesheet completion task", func() bool { return page.Realm.Tasks.Len() >= 1 })
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	snapshot = page.Navigation()
	if snapshot.State != browser.NavigationLoadingResources || snapshot.ResourcesPending != 1 {
		t.Fatalf("after stylesheet completion = %#v", snapshot)
	}
	if page.Frame() != nil {
		t.Fatal("page rendered before the final resource completed")
	}

	close(client.imageRelease)
	waitFor(t, "image completion task", func() bool { return page.Realm.Tasks.Len() >= 1 })
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	snapshot = page.Navigation()
	if snapshot.State != browser.NavigationLoadingScripts || snapshot.ResourcesPending != 0 {
		t.Fatalf("after image completion = %#v", snapshot)
	}
	if page.Frame() != nil {
		t.Fatal("page rendered before the document lifecycle completed")
	}
	if err := page.WaitNavigation(context.Background(), navigation); err != nil {
		t.Fatal(err)
	}
	snapshot = page.Navigation()
	if snapshot.State != browser.NavigationComplete || snapshot.ResourcesFailed != 0 {
		t.Fatalf("completed navigation = %#v", snapshot)
	}
	if !frameContainsText(page.Frame(), "sequenced") || !frameContainsCommand(page.Frame(), render.DrawImageCommand) {
		t.Fatal("final frame does not contain the document text and decoded image")
	}

	var browserPublications, queueTransfers int
	for _, event := range engine.Ledger().Events() {
		if event.Kind == ownership.ObjectPublished && event.From.Kind == ownership.OwnerBrowser && event.To.Kind == ownership.OwnerQueue {
			browserPublications++
		}
		if event.Kind == ownership.ObjectTransferred && event.From.Kind == ownership.OwnerQueue && event.To.Kind == ownership.OwnerTask {
			queueTransfers++
		}
	}
	if browserPublications < 3 || queueTransfers < 4 {
		t.Fatalf("ownership trace publications=%d transfers=%d", browserPublications, queueTransfers)
	}
}

func TestCancelNavigationStopsCurrentDocumentLoad(t *testing.T) {
	t.Parallel()

	engine, err := browser.New()
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	initialURL, _ := url.Parse("https://example.test/initial")
	page, err := engine.NewPage(dom.NewDocument(), initialURL)
	if err != nil {
		t.Fatal(err)
	}
	client := newSupersedingDocumentLoader()
	navigation, err := page.Navigate(context.Background(), "https://example.test/a", client)
	if err != nil {
		t.Fatal(err)
	}
	waitSignal(t, client.firstStarted, "blocked document request")
	if err := page.CancelNavigation(navigation); err != nil {
		t.Fatal(err)
	}
	snapshot := page.Navigation()
	if snapshot.ID != navigation || snapshot.State != browser.NavigationCanceled || !errors.Is(snapshot.Err, context.Canceled) {
		t.Fatalf("canceled navigation = %#v", snapshot)
	}
	close(client.firstRelease)
}

func TestNewNavigationRejectsLateDocumentCompletionAndOldNodeHandles(t *testing.T) {
	t.Parallel()

	engine, err := browser.New()
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	root, err := parseDocument(`<html><body><p>initial</p></body></html>`)
	if err != nil {
		t.Fatal(err)
	}
	initialURL, _ := url.Parse("https://example.test/initial")
	page, err := engine.NewPage(root, initialURL)
	if err != nil {
		t.Fatal(err)
	}
	textID, ok := page.Document().ID(findTextNode(root, "initial"))
	if !ok {
		t.Fatal("initial text node has no ID")
	}
	oldHandle := browser.NodeHandle{Document: page.DocumentGeneration(), Node: textID}

	client := newSupersedingDocumentLoader()
	first, err := page.Navigate(context.Background(), "https://example.test/a", client)
	if err != nil {
		t.Fatal(err)
	}
	waitSignal(t, client.firstStarted, "first navigation")
	second, err := page.Navigate(context.Background(), "https://example.test/b", client)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "second document completion", func() bool { return page.Realm.Tasks.Len() >= 1 })
	close(client.firstRelease)
	waitFor(t, "late first document completion", func() bool { return page.Realm.Tasks.Len() >= 2 })

	if err := page.WaitNavigation(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	if err := page.WaitNavigation(context.Background(), first); !errors.Is(err, browser.ErrNavigationSuperseded) {
		t.Fatalf("first WaitNavigation() error = %v, want ErrNavigationSuperseded", err)
	}
	if got := page.URL().String(); got != "https://example.test/b-final" {
		t.Fatalf("Page.URL() = %q, want second navigation final URL", got)
	}
	if !frameContainsText(page.Frame(), "document B") || frameContainsText(page.Frame(), "document A") {
		t.Fatal("late first completion replaced the second navigation")
	}
	if _, ok := page.Resolve(oldHandle); ok {
		t.Fatal("old document handle resolved after navigation replacement")
	}
	if _, err := page.QueueTextMutationHandle(oldHandle, "wrong document"); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); !errors.Is(err, browser.ErrStaleNodeHandle) {
		t.Fatalf("stale mutation task error = %v, want ErrStaleNodeHandle", err)
	}
	if !frameContainsText(page.Frame(), "document B") {
		t.Fatal("stale mutation changed the current document")
	}
}

type stagedNavigationLoader struct {
	imageBytes        []byte
	stylesheetStarted chan struct{}
	stylesheetRelease chan struct{}
	imageStarted      chan struct{}
	imageRelease      chan struct{}
}

func newStagedNavigationLoader(t *testing.T) *stagedNavigationLoader {
	t.Helper()
	pixel := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	pixel.SetNRGBA(0, 0, color.NRGBA{R: 0x11, G: 0x88, B: 0xee, A: 0xff})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, pixel); err != nil {
		t.Fatal(err)
	}
	return &stagedNavigationLoader{
		imageBytes:        encoded.Bytes(),
		stylesheetStarted: make(chan struct{}),
		stylesheetRelease: make(chan struct{}),
		imageStarted:      make(chan struct{}),
		imageRelease:      make(chan struct{}),
	}
}

func (client *stagedNavigationLoader) Load(context.Context, string) (*loader.Response, error) {
	location, _ := url.Parse("https://example.test/page-final")
	return &loader.Response{
		URL:        location,
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(`
			<html><head><link rel="stylesheet" href="/page.css"></head>
			<body><p>sequenced</p><img src="/pixel.png"></body></html>
		`)),
	}, nil
}

func (client *stagedNavigationLoader) LoadResource(ctx context.Context, rawURL string, _ loader.Destination) (*loader.Response, error) {
	location, _ := url.Parse(rawURL)
	header := make(http.Header)
	switch location.Path {
	case "/page.css":
		close(client.stylesheetStarted)
		select {
		case <-client.stylesheetRelease:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		header.Set("Content-Type", "text/css")
		return &loader.Response{
			URL: location, StatusCode: http.StatusOK, Header: header,
			Body: io.NopCloser(strings.NewReader(`body { margin: 0; background: #224466; }`)),
		}, nil

	case "/pixel.png":
		close(client.imageStarted)
		select {
		case <-client.imageRelease:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		header.Set("Content-Type", "image/png")
		return &loader.Response{
			URL: location, StatusCode: http.StatusOK, Header: header,
			Body: io.NopCloser(bytes.NewReader(client.imageBytes)),
		}, nil
	default:
		return nil, errors.New("unexpected resource URL")
	}
}

type supersedingDocumentLoader struct {
	firstStarted chan struct{}
	firstRelease chan struct{}
}

func newSupersedingDocumentLoader() *supersedingDocumentLoader {
	return &supersedingDocumentLoader{firstStarted: make(chan struct{}), firstRelease: make(chan struct{})}
}

func (client *supersedingDocumentLoader) Load(_ context.Context, rawURL string) (*loader.Response, error) {
	requested, _ := url.Parse(rawURL)
	if requested.Path == "/a" {
		close(client.firstStarted)
		<-client.firstRelease // Deliberately model a transport that finishes after cancellation.
		location, _ := url.Parse("https://example.test/a-final")
		return documentResponse(location, "document A"), nil
	}
	location, _ := url.Parse("https://example.test/b-final")
	return documentResponse(location, "document B"), nil
}

func documentResponse(location *url.URL, text string) *loader.Response {
	return &loader.Response{
		URL:        location,
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`<html><body><p>` + text + `</p></body></html>`)),
	}
}

func parseDocument(source string) (*dom.Node, error) {
	return htmlparser.Parse(strings.NewReader(source))
}

func frameContainsCommand(frame *render.Frame, kind render.CommandKind) bool {
	if frame == nil {
		return false
	}
	for _, command := range frame.DisplayList.Commands {
		if command.Kind == kind {
			return true
		}
	}
	return false
}

func waitFor(t *testing.T, label string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", label)
		}
		time.Sleep(time.Millisecond)
	}
}

func waitSignal(t *testing.T, signal <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}
