package resource_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	htmlparser "github.com/JediWattson/gossamer/internal/html"
	"github.com/JediWattson/gossamer/internal/resource"
)

func TestDiscoverResolvesDOMResourcesInTreeOrder(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html>
		<base href="/assets/">
		<link rel="alternate STYLESHEET" href="css/site.css#theme">
		<link rel="icon" href="//cdn.example/icon.png">
		<body>
			<img src="../hero.png">
			<input type="IMAGE" src="/button.png">
			<video poster="poster.jpg"></video>
			<img src="data:image/png;base64,AAAA">
			<script src="ignored.js"></script>
		</body>`))
	if err != nil {
		t.Fatalf("html.Parse() error = %v", err)
	}
	documentURL := mustURL(t, "https://example.com/app/page.html?view=full")

	graph, err := resource.Discover(document, documentURL)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if got, want := graph.BaseURL.String(), "https://example.com/assets/"; got != want {
		t.Errorf("BaseURL = %q, want %q", got, want)
	}
	if got, want := graph.DocumentURL.String(), documentURL.String(); got != want {
		t.Errorf("DocumentURL = %q, want %q", got, want)
	}

	want := []struct {
		kind      resource.Kind
		url       string
		element   string
		attribute string
	}{
		{kind: resource.Stylesheet, url: "https://example.com/assets/css/site.css", element: "link", attribute: "href"},
		{kind: resource.Image, url: "https://cdn.example/icon.png", element: "link", attribute: "href"},
		{kind: resource.Image, url: "https://example.com/hero.png", element: "img", attribute: "src"},
		{kind: resource.Image, url: "https://example.com/button.png", element: "input", attribute: "src"},
		{kind: resource.Image, url: "https://example.com/assets/poster.jpg", element: "video", attribute: "poster"},
	}
	if got := len(graph.References); got != len(want) {
		t.Fatalf("len(References) = %d, want %d: %#v", got, len(want), graph.References)
	}
	for index, expectation := range want {
		got := graph.References[index]
		if got.Kind != expectation.kind || got.URL.String() != expectation.url || got.Node.Data != expectation.element || got.Attribute != expectation.attribute {
			t.Errorf("References[%d] = {%s %q <%s> %s}, want {%s %q <%s> %s}",
				index, got.Kind, got.URL, got.Node.Data, got.Attribute,
				expectation.kind, expectation.url, expectation.element, expectation.attribute)
		}
	}
}

func TestDiscoverUsesOnlyFirstBaseHref(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	document.AppendChild(html)
	head := dom.NewElement("head")
	html.AppendChild(head)
	head.AppendChild(dom.NewElement("base", dom.Attribute{Name: "href", Value: "http://[::1"}))
	head.AppendChild(dom.NewElement("base", dom.Attribute{Name: "href", Value: "https://ignored.example/"}))
	head.AppendChild(dom.NewElement("link",
		dom.Attribute{Name: "rel", Value: "stylesheet"},
		dom.Attribute{Name: "href", Value: "site.css"},
	))

	graph, err := resource.Discover(document, mustURL(t, "https://example.com/docs/page.html"))
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if got, want := graph.BaseURL.String(), "https://example.com/docs/page.html"; got != want {
		t.Errorf("BaseURL = %q, want fallback %q", got, want)
	}
	if got, want := graph.References[0].URL.String(), "https://example.com/docs/site.css"; got != want {
		t.Errorf("stylesheet URL = %q, want %q", got, want)
	}
}

func TestDiscoverPreservesDuplicateConsumers(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	document.AppendChild(html)
	body := dom.NewElement("body")
	html.AppendChild(body)
	first := dom.NewElement("img", dom.Attribute{Name: "src", Value: "/shared.png"})
	second := dom.NewElement("img", dom.Attribute{Name: "src", Value: "/shared.png"})
	body.AppendChild(first)
	body.AppendChild(second)

	graph, err := resource.Discover(document, mustURL(t, "https://example.com/"))
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if got := len(graph.References); got != 2 {
		t.Fatalf("len(References) = %d, want 2", got)
	}
	if graph.References[0].Node != first || graph.References[1].Node != second {
		t.Error("duplicate URL consumers did not retain their initiating DOM nodes")
	}
}

func TestDiscoverRejectsInvalidInputs(t *testing.T) {
	t.Parallel()

	absolute := mustURL(t, "https://example.com/")
	relative := mustURL(t, "/relative")
	for name, test := range map[string]struct {
		document    *dom.Node
		documentURL *url.URL
	}{
		"nil document": {documentURL: absolute},
		"element root": {document: dom.NewElement("html"), documentURL: absolute},
		"nil URL":      {document: dom.NewDocument()},
		"relative URL": {document: dom.NewDocument(), documentURL: relative},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := resource.Discover(test.document, test.documentURL); err == nil {
				t.Error("Discover() error = nil, want validation error")
			}
		})
	}
}

func mustURL(t *testing.T, source string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(source)
	if err != nil {
		t.Fatalf("url.Parse(%q): %v", source, err)
	}
	return parsed
}
