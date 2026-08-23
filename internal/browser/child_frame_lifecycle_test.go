package browser

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/dom"
	htmlparser "github.com/JediWattson/gossamer/internal/html"
	"github.com/JediWattson/gossamer/internal/loader"
	"github.com/JediWattson/gossamer/internal/render"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
)

func TestConnectedIframeAutomaticallyOwnsAndReleasesChildRealm(t *testing.T) {
	t.Parallel()

	browserRuntime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	root := dom.NewDocument()
	iframe := dom.NewElement("iframe", dom.Attribute{Name: "src", Value: "/child"})
	root.AppendChild(iframe)
	location, _ := url.Parse("https://parent.test/root")
	page, err := browserRuntime.NewPage(root, location)
	if err != nil {
		t.Fatal(err)
	}
	page.SetNavigationLoader(frameDocumentLoader{})
	iframeID, _ := page.Document().ID(iframe)
	handle := NodeHandle{Document: page.DocumentGeneration(), Node: iframeID}

	if err := page.Render(); err != nil {
		t.Fatal(err)
	}
	child := waitForChildFrame(t, page, handle)
	waitForNavigation(t, child)
	if got := child.URL(); got == nil || got.String() != "https://parent.test/child" {
		t.Fatalf("child URL = %v", got)
	}
	if child.Realm == page.Realm {
		t.Fatal("automatic iframe reused parent Realm")
	}

	if err := page.Document().RemoveChild(page.Document().RootID(), iframeID); err != nil {
		t.Fatal(err)
	}
	if err := page.Render(); err != nil {
		t.Fatal(err)
	}
	if _, ok := page.ChildFrame(handle); ok {
		t.Fatal("detached iframe retained its child Page")
	}
	if _, err := child.Realm.EnqueueTask(func(*browserruntime.TaskContext) error { return nil }); err != browserruntime.ErrRealmClosed {
		t.Fatalf("detached child Realm error = %v, want ErrRealmClosed", err)
	}
}

func TestAutomaticIframeSourceChangeReplacesChildRealm(t *testing.T) {
	t.Parallel()

	browserRuntime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	root := dom.NewDocument()
	iframe := dom.NewElement("iframe", dom.Attribute{Name: "src", Value: "/one"})
	root.AppendChild(iframe)
	location, _ := url.Parse("https://parent.test/root")
	page, err := browserRuntime.NewPage(root, location)
	if err != nil {
		t.Fatal(err)
	}
	page.SetNavigationLoader(frameDocumentLoader{})
	iframeID, _ := page.Document().ID(iframe)
	handle := NodeHandle{Document: page.DocumentGeneration(), Node: iframeID}
	if err := page.Render(); err != nil {
		t.Fatal(err)
	}
	first := waitForChildFrame(t, page, handle)
	waitForNavigation(t, first)

	if err := page.Document().SetAttribute(iframeID, "src", "/two"); err != nil {
		t.Fatal(err)
	}
	if err := page.Render(); err != nil {
		t.Fatal(err)
	}
	second := waitForReplacementChildFrame(t, page, handle, first)
	waitForNavigation(t, second)
	if got := second.URL(); got == nil || got.String() != "https://parent.test/two" {
		t.Fatalf("replacement child URL = %v", got)
	}
	if _, err := first.Realm.EnqueueTask(func(*browserruntime.TaskContext) error { return nil }); err != browserruntime.ErrRealmClosed {
		t.Fatalf("replaced child Realm error = %v, want ErrRealmClosed", err)
	}
}

func TestClosedShadowIframeParticipatesInAutomaticFrameLifecycle(t *testing.T) {
	t.Parallel()

	browserRuntime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	root := dom.NewDocument()
	host := dom.NewElement("div")
	root.AppendChild(host)
	location, _ := url.Parse("https://parent.test/")
	page, err := browserRuntime.NewPage(root, location)
	if err != nil {
		t.Fatal(err)
	}
	page.SetNavigationLoader(frameDocumentLoader{})
	hostID, _ := page.Document().ID(host)
	shadowID, err := page.Document().AttachShadow(hostID, dom.ShadowRootClosed, false)
	if err != nil {
		t.Fatal(err)
	}
	iframeID, err := page.Document().AppendChild(shadowID, dom.NewElement("iframe", dom.Attribute{Name: "src", Value: "https://child.test/widget"}))
	if err != nil {
		t.Fatal(err)
	}
	handle := NodeHandle{Document: page.DocumentGeneration(), Node: iframeID}

	if err := page.Render(); err != nil {
		t.Fatal(err)
	}
	child := waitForChildFrame(t, page, handle)
	waitForNavigation(t, child)
	if _, ok := page.ContentDocument(handle); ok {
		t.Fatal("closed-shadow cross-origin iframe exposed contentDocument")
	}

	if err := page.Document().RemoveChild(shadowID, iframeID); err != nil {
		t.Fatal(err)
	}
	if err := page.Render(); err != nil {
		t.Fatal(err)
	}
	if _, ok := page.ChildFrame(handle); ok {
		t.Fatal("closed-shadow iframe retained child Page after removal")
	}
}

func TestChildFrameDisplayListComposesIntoIframeContentBox(t *testing.T) {
	t.Parallel()

	browserRuntime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	parentRoot, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body><iframe id="child" style="width:180px;height:80px"></iframe></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://parent.test/")
	parent, err := browserRuntime.NewPage(parentRoot, location)
	if err != nil {
		t.Fatal(err)
	}
	iframeID, _ := parent.Document().ElementByID("child")
	iframe := NodeHandle{Document: parent.DocumentGeneration(), Node: iframeID}
	childRoot, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body>nested frame paint</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	child, err := parent.AttachChildFrame(iframe, childRoot, location)
	if err != nil {
		t.Fatal(err)
	}
	if err := child.SetViewport(render.Viewport{Width: 180, Height: 80}); err != nil {
		t.Fatal(err)
	}
	if err := child.Render(); err != nil {
		t.Fatal(err)
	}
	if !frameHasText(child.Frame(), "nested frame paint") {
		t.Fatalf("child display list omitted its document; text=%q", frameTextCommands(child.Frame()))
	}
	if err := parent.Render(); err != nil {
		t.Fatal(err)
	}
	if !frameHasText(parent.Frame(), "nested frame paint") {
		geometry, found := parent.Frame().Layout.GeometryID(iframeID)
		t.Fatalf("parent display list omitted the child frame; text=%q geometry=%#v found=%v", frameTextCommands(parent.Frame()), geometry, found)
	}
}

type frameDocumentLoader struct{}

func (frameDocumentLoader) Load(_ context.Context, rawURL string) (*loader.Response, error) {
	location, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	return &loader.Response{
		URL:        location,
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><body>child frame</body></html>`)),
	}, nil
}

func waitForChildFrame(t *testing.T, page *Page, handle NodeHandle) *Page {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if child, ok := page.ChildFrame(handle); ok {
			return child
		}
		runtime.Gosched()
	}
	t.Fatal("timed out waiting for child frame")
	return nil
}

func waitForReplacementChildFrame(t *testing.T, page *Page, handle NodeHandle, previous *Page) *Page {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if child, ok := page.ChildFrame(handle); ok && child != previous {
			return child
		}
		runtime.Gosched()
	}
	t.Fatal("timed out waiting for replacement child frame")
	return nil
}

func waitForNavigation(t *testing.T, page *Page) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snapshot := page.Navigation()
		if snapshot.State.terminal() {
			if snapshot.Err != nil {
				t.Fatal(snapshot.Err)
			}
			return
		}
		runtime.Gosched()
	}
	t.Fatal("timed out waiting for child navigation")
}

func frameHasText(frame *render.Frame, text string) bool {
	if frame == nil {
		return false
	}
	for _, command := range frame.DisplayList.Commands {
		if command.Kind == render.DrawTextCommand && strings.Contains(command.Text, text) {
			return true
		}
	}
	return false
}

func frameTextCommands(frame *render.Frame) []string {
	if frame == nil {
		return nil
	}
	var result []string
	for _, command := range frame.DisplayList.Commands {
		if command.Kind == render.DrawTextCommand {
			result = append(result, command.Text)
		}
	}
	return result
}
