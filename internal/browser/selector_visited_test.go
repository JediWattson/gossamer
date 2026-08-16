package browser

import (
	"context"
	"net/url"
	"slices"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
)

func TestVisitedPseudoUsesSameOriginSessionHistoryAcrossStyleAndSelectors(t *testing.T) {
	t.Parallel()

	engine, page, targetID := computedStyleTestPage(t, `
		<html><head>
			<base id="base-owner" href="/base/">
			<style>
				a:link { color:red; width:10px }
				a:visited { color:purple; width:30px }
			</style>
		</head><body>
			<a id="target" href="../#different-fragment">current</a>
			<a id="prior" href="../visited#new-fragment">prior</a>
			<a id="base-relative" href="old">base relative</a>
			<a id="future" href="../future">future</a>
			<a id="cross-origin" href="https://other.test/secret">cross origin</a>
		</body></html>
	`)
	defer engine.Close()

	generation := page.DocumentGeneration()
	host := &taskHost{page: page, generation: generation}
	handle := func(id string) NodeHandle {
		t.Helper()
		node, ok := page.document.ElementByID(id)
		if !ok {
			t.Fatalf("%s has no stable ID", id)
		}
		return NodeHandle{Document: generation, Node: node}
	}
	target := NodeHandle{Document: generation, Node: targetID}
	prior := handle("prior")
	baseRelative := handle("base-relative")
	future := handle("future")
	crossOrigin := handle("cross-origin")
	baseOwner := handle("base-owner")

	// The current document URL is already in this Page's session history, and
	// fragment differences do not create a distinct visited destination.
	assertFormSelectorMatch(t, host, target, ":visited", true)
	assertSelectorStateProperty(t, page, target, "color", "rgb(128, 0, 128)")
	assertFormSelectorMatch(t, host, prior, ":link", true)
	firstSnapshot := page.computedStyle.snapshot

	page.mutex.Lock()
	page.pushHistoryLocked(visitedTestURL(t, "https://computed-style.test:443/visited#old-fragment"), 1)
	page.pushHistoryLocked(visitedTestURL(t, "https://computed-style.test/base/old"), 2)
	// Cross-origin history is retained for navigation introspection but is not
	// exposed to the document through :visited.
	page.pushHistoryLocked(visitedTestURL(t, "https://other.test/secret"), 3)
	page.mutex.Unlock()

	for _, visited := range []NodeHandle{target, prior, baseRelative} {
		assertFormSelectorMatch(t, host, visited, ":visited", true)
		assertFormSelectorMatch(t, host, visited, ":link", false)
		assertSelectorStateProperty(t, page, visited, "width", "30px")
	}
	for _, unvisited := range []NodeHandle{future, crossOrigin} {
		assertFormSelectorMatch(t, host, unvisited, ":visited", false)
		assertFormSelectorMatch(t, host, unvisited, ":link", true)
		assertSelectorStateProperty(t, page, unvisited, "width", "10px")
	}
	if page.computedStyle.snapshot == firstSnapshot {
		t.Fatal("new same-origin history did not replace the style snapshot")
	}

	documentID, ok := page.document.ID(page.document.Root())
	if !ok {
		t.Fatal("document has no stable ID")
	}
	matches, err := host.QuerySelector(NodeHandle{Document: generation, Node: documentID}, ":visited", true)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]NodeHandle, len(matches))
	copy(got, matches)
	want := []NodeHandle{target, prior, baseRelative}
	if !slices.Equal(got, want) {
		t.Fatalf("querySelectorAll(:visited) = %#v, want %#v", got, want)
	}

	if err := host.SetAttribute(future, "href", "../visited"); err != nil {
		t.Fatal(err)
	}
	assertFormSelectorMatch(t, host, future, ":visited", true)
	assertSelectorStateProperty(t, page, future, "color", "rgb(128, 0, 128)")

	// The first base element participates in hyperlink resolution, and changing
	// it invalidates the stable style snapshot through the DOM version.
	if err := host.SetAttribute(baseOwner, "href", "/other/"); err != nil {
		t.Fatal(err)
	}
	assertFormSelectorMatch(t, host, baseRelative, ":visited", false)
	assertSelectorStateProperty(t, page, baseRelative, "color", "rgb(255, 0, 0)")

	if page.Frame() != nil {
		t.Fatal("visited-history style flush unexpectedly published a frame")
	}
	if !page.Dirty() {
		t.Fatal("visited-history mutation did not leave the page dirty")
	}
}

func TestVisitedPseudoTracksCompletedSameOriginNavigation(t *testing.T) {
	t.Parallel()

	engine, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	start := visitedTestURL(t, "https://history.test/start")
	page, err := engine.NewPage(dom.NewDocument(), start)
	if err != nil {
		t.Fatal(err)
	}
	next := visitedTestURL(t, "https://history.test/next")
	navigation, err := page.Navigate(context.Background(), next.String(), computedStyleDocumentLoader{
		url: next,
		source: `<!doctype html><html><head><style>
			a:link { color:red }
			a:visited { color:purple }
		</style></head><body><a id="prior" href="/start#section">prior</a></body></html>`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := page.WaitNavigation(context.Background(), navigation); err != nil {
		t.Fatal(err)
	}
	priorID, ok := page.document.ElementByID("prior")
	if !ok {
		t.Fatal("navigated link has no stable ID")
	}
	handle := NodeHandle{Document: page.DocumentGeneration(), Node: priorID}
	host := &taskHost{page: page, generation: handle.Document}
	assertFormSelectorMatch(t, host, handle, ":visited", true)
	assertSelectorStateProperty(t, page, handle, "color", "rgb(128, 0, 128)")
	history, current := page.History()
	if len(history) != 2 || current != 1 || history[0].URL.String() != start.String() || history[1].URL.String() != next.String() {
		t.Fatalf("navigation history = %#v at %d", history, current)
	}
}

func visitedTestURL(t *testing.T, source string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func FuzzVisitedURLResolutionDoesNotPanic(f *testing.F) {
	for _, seed := range [][3]string{
		{"https://example.test/path/page", "../visited#fragment", "https://example.test/visited"},
		{"https://example.test:443/", "//EXAMPLE.test/current", "https://example.test/current#old"},
		{"https://[::1]/base/", "next", "https://[::1]/base/next"},
		{"https://user@example.test/", "%", "https://example.test/"},
	} {
		f.Add(seed[0], seed[1], seed[2])
	}
	f.Fuzz(func(t *testing.T, baseSource, href, historySource string) {
		if len(baseSource)+len(href)+len(historySource) > 16*1024 {
			t.Skip()
		}
		base, err := url.Parse(baseSource)
		if err != nil {
			return
		}
		history, err := url.Parse(historySource)
		if err != nil {
			return
		}
		node := dom.NewElement("a", dom.Attribute{Name: "href", Value: href})
		destination, _ := resolvedHyperlinkURL(node, base)
		_, _ = visitedURLKey(base)
		_, _ = visitedURLKey(history)
		_, _ = visitedURLKey(destination)
		_ = sameOriginURL(base, history)
		_ = sameOriginURL(base, destination)
	})
}
