package browser_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/browser/fake"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/loader"
	"github.com/JediWattson/gossamer/internal/render"
	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

func TestFakeEngineClickQueuesCallbackMutationAndRender(t *testing.T) {
	t.Parallel()

	fakeEngine := fake.New()
	engine, err := browser.NewWithEngine(fakeEngine)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	location, _ := url.Parse("https://example.test/app")
	page, err := engine.LoadPage(context.Background(), location.String(), stubDocumentLoader{response: &loader.Response{
		URL:        location,
		StatusCode: 200,
		Body: io.NopCloser(strings.NewReader(`
			<html><body style="margin:0">
				<button style="display:block;width:120px;height:40px">change</button>
				<p>before</p>
			</body></html>
		`)),
	}})
	if err != nil {
		t.Fatal(err)
	}
	document := page.Document()
	button := findElementNode(document.Root(), "button")
	text := findTextNode(document.Root(), "before")
	buttonID, buttonOK := document.ID(button)
	textID, textOK := document.ID(text)
	if !buttonOK || !textOK {
		t.Fatal("interactive nodes do not have stable IDs")
	}
	generation := page.DocumentGeneration()
	buttonHandle := browser.NodeHandle{Document: generation, Node: buttonID}
	textHandle := browser.NodeHandle{Document: generation, Node: textID}

	script, ok := fakeEngine.LatestRealm()
	if !ok {
		t.Fatal("fake engine did not create a document realm")
	}
	callback, err := script.RegisterCallback(func(host browser.Host) error {
		return host.SetText(textHandle, "after")
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := script.Bind(browser.InputClick, buttonHandle, callback); err != nil {
		t.Fatal(err)
	}
	buttonBox := findBoxForNode(page.Frame().Root, button)
	if buttonBox == nil {
		t.Fatal("rendered frame has no button box")
	}
	x := buttonBox.Bounds.X + 2
	y := buttonBox.Bounds.Y + 2
	if hit, ok := page.HitTest(x, y); !ok || hit != buttonHandle {
		t.Fatalf("Page.HitTest() = %#v, %t, want button handle %#v", hit, ok, buttonHandle)
	}

	baseline := engine.Ledger().Stats()
	if _, err := page.QueueClick(x, y, 0); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := page.Realm.Tasks.Len(); got != 1 {
		t.Fatalf("tasks after click dispatch = %d, want queued callback", got)
	}
	if !frameContainsText(page.Frame(), "before") || frameContainsText(page.Frame(), "after") {
		t.Fatal("frame changed before fake callback execution")
	}

	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := page.Realm.Tasks.Len(); got != 1 {
		t.Fatalf("tasks after callback = %d, want queued render", got)
	}
	if !page.Dirty() || !frameContainsText(page.Frame(), "before") {
		t.Fatal("callback did not dirty DOM while preserving the old frame")
	}

	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if page.Dirty() || !frameContainsText(page.Frame(), "after") || frameContainsText(page.Frame(), "before") {
		t.Fatal("render task did not publish the fake-engine mutation")
	}
	final := engine.Ledger().Stats()
	if final.ObjectsCreated-baseline.ObjectsCreated < 3 ||
		final.ObjectsDestroyed-baseline.ObjectsDestroyed < 3 ||
		final.LiveObjects != baseline.LiveObjects {
		t.Fatalf("click ownership delta: before=%#v after=%#v", baseline, final)
	}

	var external, internal, transfers int
	for _, event := range engine.Ledger().Events() {
		if event.Kind == ownership.ObjectPublished && event.From.Kind == ownership.OwnerBrowser && event.To.Kind == ownership.OwnerQueue {
			external++
		}
		if event.Kind == ownership.ObjectPublished && event.From.Kind == ownership.OwnerTask && event.To.Kind == ownership.OwnerQueue {
			internal++
		}
		if event.Kind == ownership.ObjectTransferred && event.From.Kind == ownership.OwnerQueue && event.To.Kind == ownership.OwnerTask {
			transfers++
		}
	}
	if external == 0 || internal < 2 || transfers < 3 {
		t.Fatalf("click trace external=%d internal=%d transfers=%d", external, internal, transfers)
	}
}

func TestFakeEngineEvaluatesSourceInsideGenerationBoundPageTask(t *testing.T) {
	t.Parallel()

	fakeEngine := fake.New()
	engine, err := browser.NewWithEngine(fakeEngine)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	location, _ := url.Parse("https://example.test/evaluate")
	page, err := engine.LoadPage(context.Background(), location.String(), stubDocumentLoader{response: &loader.Response{
		URL: location, StatusCode: 200,
		Body: io.NopCloser(strings.NewReader(`<html><body><p>before evaluation</p></body></html>`)),
	}})
	if err != nil {
		t.Fatal(err)
	}
	text := findTextNode(page.Document().Root(), "before evaluation")
	textID, ok := page.Document().ID(text)
	if !ok {
		t.Fatal("text node has no stable ID")
	}
	handle := browser.NodeHandle{Document: page.DocumentGeneration(), Node: textID}
	script, ok := fakeEngine.LatestRealm()
	if !ok {
		t.Fatal("fake document realm was not created")
	}
	if err := script.SetEvaluator(func(host browser.Host, source browser.ScriptSource) error {
		if source.URL != "https://example.test/app.js" || source.Source != "fake program" {
			t.Fatalf("evaluated source = %#v", source)
		}
		return host.SetText(handle, "after evaluation")
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := page.QueueScript(browser.ScriptSource{
		URL: "https://example.test/app.js", Source: "fake program",
	}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !page.Dirty() || !frameContainsText(page.Frame(), "before evaluation") {
		t.Fatal("evaluation did not queue rendering after host mutation")
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if page.Dirty() || !frameContainsText(page.Frame(), "after evaluation") {
		t.Fatal("evaluated host mutation was not rendered")
	}
}

func TestNavigationFetchesAndEvaluatesClassicScriptsInDOMOrder(t *testing.T) {
	t.Parallel()

	var evaluationMutex sync.Mutex
	var evaluated []browser.ScriptSource
	fakeEngine := fake.NewWithInitializer(func(realm *fake.Realm) error {
		return realm.SetEvaluator(func(_ browser.Host, source browser.ScriptSource) error {
			evaluationMutex.Lock()
			evaluated = append(evaluated, source)
			evaluationMutex.Unlock()
			return nil
		})
	})
	var scriptAccept string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/page":
			writer.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(writer, `<html><body>
				<script>inline one</script>
				<script src="/external.js"></script>
				<script type="module">module skipped</script>
				<script>inline two</script>
				<p>scripted page</p>
			</body></html>`)
		case "/external.js":
			evaluationMutex.Lock()
			scriptAccept = request.Header.Get("Accept")
			evaluationMutex.Unlock()
			writer.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			_, _ = io.WriteString(writer, "external script")
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	engine, err := browser.NewWithEngine(fakeEngine)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	page, err := engine.LoadPage(context.Background(), server.URL+"/page", loader.New(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	snapshot := page.Navigation()
	if snapshot.State != browser.NavigationComplete || snapshot.ScriptsTotal != 3 ||
		snapshot.ScriptsPending != 0 || snapshot.ScriptsFailed != 0 {
		t.Fatalf("script navigation = %#v", snapshot)
	}
	evaluationMutex.Lock()
	got := append([]browser.ScriptSource(nil), evaluated...)
	gotAccept := scriptAccept
	evaluationMutex.Unlock()
	if len(got) != 3 {
		t.Fatalf("evaluated scripts = %#v, want three classic scripts", got)
	}
	if strings.TrimSpace(got[0].Source) != "inline one" || got[1].Source != "external script" ||
		strings.TrimSpace(got[2].Source) != "inline two" {
		t.Fatalf("evaluation order = %#v", got)
	}
	if got[1].URL != server.URL+"/external.js" {
		t.Fatalf("external script URL = %q", got[1].URL)
	}
	if !strings.HasPrefix(gotAccept, "text/javascript") {
		t.Fatalf("script Accept = %q", gotAccept)
	}
	if !frameContainsText(page.Frame(), "scripted page") {
		t.Fatal("page did not render after script evaluation")
	}
}

func findElementNode(root *dom.Node, name string) *dom.Node {
	if root == nil {
		return nil
	}
	if root.Type == dom.ElementNode && root.Data == name {
		return root
	}
	for _, child := range root.Children {
		if found := findElementNode(child, name); found != nil {
			return found
		}
	}
	return nil
}

func findBoxForNode(box *render.Box, node *dom.Node) *render.Box {
	if box == nil {
		return nil
	}
	if box.Node == node {
		return box
	}
	for _, child := range box.Children {
		if found := findBoxForNode(child, node); found != nil {
			return found
		}
	}
	return nil
}
