package browser_test

import (
	"context"
	"io"
	"net/url"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	htmlparser "github.com/JediWattson/gossamer/internal/html"
	"github.com/JediWattson/gossamer/internal/loader"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestPageSchedulesStableIDMutationThenRender(t *testing.T) {
	t.Parallel()

	engine, err := browser.New()
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	root, err := htmlparser.Parse(strings.NewReader(`
		<html>
			<body style="margin: 0">
				<p>before</p>
			</body>
		</html>
	`))
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://example.com/")
	page, err := engine.NewPage(root, location)
	if err != nil {
		t.Fatal(err)
	}
	if err := page.Render(); err != nil {
		t.Fatal(err)
	}
	if !frameContainsText(page.Frame(), "before") {
		t.Fatal("initial frame does not contain before")
	}
	baselineStats := engine.Ledger().Stats()

	textNode := findTextNode(root, "before")
	textID, ok := page.Document().ID(textNode)
	if !ok || textID == dom.InvalidNodeID {
		t.Fatal("text node has no stable ID")
	}
	if _, err := page.QueueTextMutation(textID, "after"); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !page.Dirty() {
		t.Fatal("page is clean after mutation task, want dirty until render task")
	}
	if !frameContainsText(page.Frame(), "before") || frameContainsText(page.Frame(), "after") {
		t.Fatal("frame changed before queued render task executed")
	}
	queuedStats := engine.Ledger().Stats()
	if queuedStats.LiveObjects != baselineStats.LiveObjects+1 ||
		queuedStats.PublishOperations <= baselineStats.PublishOperations {
		t.Fatalf("ownership after mutation task = %#v", queuedStats)
	}

	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if page.Dirty() {
		t.Fatal("page remains dirty after render task")
	}
	if !frameContainsText(page.Frame(), "after") || frameContainsText(page.Frame(), "before") {
		t.Fatal("rendered frame did not observe stable-ID mutation")
	}
	finalStats := engine.Ledger().Stats()
	if finalStats.LiveObjects != baselineStats.LiveObjects ||
		finalStats.ObjectsDestroyed != baselineStats.ObjectsDestroyed+1 ||
		finalStats.TransferOperations <= baselineStats.TransferOperations {
		t.Fatalf("ownership after render task = %#v", finalStats)
	}
}

func TestBrowserLoadPageIndexesFinalDocumentURL(t *testing.T) {
	t.Parallel()

	engine, err := browser.New()
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	finalURL, _ := url.Parse("https://example.com/final")
	client := stubDocumentLoader{response: &loader.Response{
		URL:        finalURL,
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(`<html><body><h1>loaded</h1></body></html>`)),
	}}
	page, err := engine.LoadPage(context.Background(), "https://example.com/start", client)
	if err != nil {
		t.Fatal(err)
	}
	if page.URL().String() != finalURL.String() {
		t.Fatalf("Page.URL() = %q, want %q", page.URL(), finalURL)
	}
	if page.Document().RootID() == dom.InvalidNodeID || page.Document().Store().Len() == 0 {
		t.Fatal("loaded page has no indexed DOM")
	}
	if err := page.Render(); err != nil {
		t.Fatal(err)
	}
	if !frameContainsText(page.Frame(), "loaded") {
		t.Fatal("loaded page frame does not contain heading")
	}
}

type stubDocumentLoader struct {
	response *loader.Response
}

func (stub stubDocumentLoader) Load(context.Context, string) (*loader.Response, error) {
	return stub.response, nil
}

func findTextNode(root *dom.Node, text string) *dom.Node {
	if root == nil {
		return nil
	}
	if root.Type == dom.TextNode && strings.Contains(root.Data, text) {
		return root
	}
	for _, child := range root.Children {
		if found := findTextNode(child, text); found != nil {
			return found
		}
	}
	return nil
}

func frameContainsText(frame *render.Frame, text string) bool {
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
