package browser

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/loader"
	"github.com/JediWattson/gossamer/internal/resource"
)

func TestStylesheetGraphTracksOrderGenerationsAndStaleResults(t *testing.T) {
	t.Parallel()

	documentNode := dom.NewDocument()
	html := dom.NewElement("html")
	head := dom.NewElement("head")
	embedded := dom.NewElement("style")
	embeddedText := dom.NewText("#target { color: red }")
	embedded.AppendChild(embeddedText)
	link := dom.NewElement("link",
		dom.Attribute{Name: "rel", Value: "stylesheet"},
		dom.Attribute{Name: "href", Value: "theme.css#fragment"},
	)
	head.AppendChild(embedded)
	head.AppendChild(link)
	html.AppendChild(head)
	documentNode.AppendChild(html)
	document, err := dom.IndexDocument(documentNode)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://example.test/path/page.html")
	embeddedID, _ := document.ID(embedded)
	linkID, _ := document.ID(link)
	textID, _ := document.ID(embeddedText)

	graph := newStylesheetGraph()
	requests, changed, err := graph.sync(document, location)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || len(requests) != 1 {
		t.Fatalf("initial sync = %d requests, changed %t", len(requests), changed)
	}
	if got, want := graph.order, []dom.NodeID{embeddedID, linkID}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("order = %v, want %v", got, want)
	}
	if got := requests[0].url.String(); got != "https://example.test/path/theme.css" {
		t.Fatalf("resolved URL = %q", got)
	}
	firstGeneration := requests[0].stylesheetGeneration
	if firstGeneration == 0 {
		t.Fatal("external sheet did not receive a generation")
	}
	resolved := graph.resolvedStylesheets(document)
	if _, ok := resolved[embedded]; !ok {
		t.Fatal("embedded stylesheet was not parsed into the graph")
	}
	if _, ok := resolved[link]; ok {
		t.Fatal("unloaded external stylesheet was exposed")
	}

	loaded, _ := css.Parse("#target { color: blue }")
	if !graph.apply(navigationResourceResult{
		target: NodeHandle{Document: 1, Node: linkID}, kind: resource.Stylesheet,
		stylesheet: loaded, stylesheetGeneration: firstGeneration,
	}) {
		t.Fatal("current external result was rejected")
	}
	if _, ok := graph.resolvedStylesheets(document)[link]; !ok {
		t.Fatal("loaded external stylesheet was not exposed")
	}

	if err := document.SetText(textID, "#target { color: green }"); err != nil {
		t.Fatal(err)
	}
	if err := document.SetAttribute(linkID, "href", "next.css"); err != nil {
		t.Fatal(err)
	}
	requests, changed, err = graph.sync(document, location)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || len(requests) != 1 || requests[0].stylesheetGeneration == firstGeneration {
		t.Fatalf("replacement sync = %#v, changed %t", requests, changed)
	}
	if graph.apply(navigationResourceResult{
		target: NodeHandle{Document: 1, Node: linkID}, kind: resource.Stylesheet,
		stylesheet: loaded, stylesheetGeneration: firstGeneration,
	}) {
		t.Fatal("stale external result overwrote a newer generation")
	}
	if got := graph.entries[embeddedID].stylesheet.Rules[0].Declarations[0].Value; got != "green" {
		t.Fatalf("updated embedded value = %q, want green", got)
	}

	if err := document.SetAttribute(linkID, "disabled", ""); err != nil {
		t.Fatal(err)
	}
	requests, changed, err = graph.sync(document, location)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || len(requests) != 0 {
		t.Fatalf("disabled sync = %d requests, changed %t", len(requests), changed)
	}
	if _, ok := graph.entries[linkID]; ok {
		t.Fatal("disabled stylesheet owner remained in the active graph")
	}
}

func TestStylesheetGraphPreservesManualResourceUntilOwnerChanges(t *testing.T) {
	t.Parallel()

	documentNode := dom.NewDocument()
	html := dom.NewElement("html")
	link := dom.NewElement("link", dom.Attribute{Name: "rel", Value: "stylesheet"}, dom.Attribute{Name: "href", Value: "theme.css"})
	html.AppendChild(link)
	documentNode.AppendChild(html)
	document, err := dom.IndexDocument(documentNode)
	if err != nil {
		t.Fatal(err)
	}
	linkID, _ := document.ID(link)
	manual, _ := css.Parse("html { color: red }")
	graph := newStylesheetGraph()
	graph.setManual(linkID, manual)
	location, _ := url.Parse("https://example.test/")

	requests, _, err := graph.sync(document, location)
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 0 || !graph.entries[linkID].ready {
		t.Fatalf("manual resource sync = %d requests, ready %t", len(requests), graph.entries[linkID].ready)
	}
	if err := document.SetAttribute(linkID, "href", "replacement.css"); err != nil {
		t.Fatal(err)
	}
	requests, _, err = graph.sync(document, location)
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 1 || graph.entries[linkID].ready {
		t.Fatalf("changed manual owner = %d requests, ready %t", len(requests), graph.entries[linkID].ready)
	}
}

func TestStylesheetGraphRegeneratesEmbeddedImportsWhenBaseChanges(t *testing.T) {
	t.Parallel()

	documentNode := dom.NewDocument()
	html := dom.NewElement("html")
	head := dom.NewElement("head")
	base := dom.NewElement("base", dom.Attribute{Name: "href", Value: "/one/"})
	embedded := dom.NewElement("style")
	embedded.AppendChild(dom.NewText(`@import "theme.css"; body { color: black }`))
	head.AppendChild(base)
	head.AppendChild(embedded)
	html.AppendChild(head)
	documentNode.AppendChild(html)
	document, err := dom.IndexDocument(documentNode)
	if err != nil {
		t.Fatal(err)
	}
	baseID, _ := document.ID(base)
	location, _ := url.Parse("https://example.test/page")
	graph := newStylesheetGraph()

	requests, _, err := graph.sync(document, location)
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 1 || requests[0].stylesheetBase.String() != "https://example.test/one/" {
		t.Fatalf("initial embedded request = %#v", requests)
	}
	firstGeneration := requests[0].stylesheetGeneration
	if err := document.SetAttribute(baseID, "href", "/two/"); err != nil {
		t.Fatal(err)
	}
	requests, changed, err := graph.sync(document, location)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || len(requests) != 1 {
		t.Fatalf("base replacement = %d requests, changed %t", len(requests), changed)
	}
	if requests[0].stylesheetGeneration == firstGeneration {
		t.Fatal("base replacement reused the embedded stylesheet generation")
	}
	if got := requests[0].stylesheetBase.String(); got != "https://example.test/two/" {
		t.Fatalf("replacement base = %q", got)
	}
}

func TestDynamicLinkStylesheetLoadsAndInvalidatesCurrentPage(t *testing.T) {
	t.Parallel()

	engine, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	location, _ := url.Parse("https://graph.test/initial")
	page, err := engine.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	loaderStub := &stylesheetGraphLoader{fetched: make(chan string, 1)}
	navigation, err := page.Navigate(context.Background(), "https://graph.test/page", loaderStub)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := page.WaitNavigation(ctx, navigation); err != nil {
		t.Fatal(err)
	}

	head := computedStyleTestElement(page.document.Root(), "head")
	if head == nil {
		t.Fatal("navigated document has no head")
	}
	headID, _ := page.document.ID(head)
	linkID, err := page.document.CreateElement("link")
	if err != nil {
		t.Fatal(err)
	}
	if err := page.document.SetAttribute(linkID, "rel", "stylesheet"); err != nil {
		t.Fatal(err)
	}
	if err := page.document.SetAttribute(linkID, "href", "dynamic.css"); err != nil {
		t.Fatal(err)
	}
	if err := page.document.AppendNode(headID, linkID); err != nil {
		t.Fatal(err)
	}
	if err := page.syncAndLoadStylesheets(); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-loaderStub.fetched:
		if got != "https://graph.test/dynamic.css" {
			t.Fatalf("dynamic fetch URL = %q", got)
		}
	case <-ctx.Done():
		t.Fatal("dynamic stylesheet fetch did not start")
	}
	if err := page.Realm.RunOne(ctx); err != nil {
		t.Fatal(err)
	}

	targetID, ok := page.document.ElementByID("target")
	if !ok {
		t.Fatal("navigated target was not indexed")
	}
	computed, err := page.ComputedStyle(NodeHandle{Document: page.DocumentGeneration(), Node: targetID})
	if err != nil {
		t.Fatal(err)
	}
	if got := computed.Color(); got.R != 0x12 || got.G != 0x34 || got.B != 0x56 || got.A != 0xff {
		t.Fatalf("dynamic stylesheet color = %#v", got)
	}
}

func TestDynamicEmbeddedImportLoadsThroughStylesheetGraph(t *testing.T) {
	t.Parallel()

	engine, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	location, _ := url.Parse("https://graph.test/initial")
	page, err := engine.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	loaderStub := &stylesheetGraphLoader{fetched: make(chan string, 1)}
	navigation, err := page.Navigate(context.Background(), "https://graph.test/page", loaderStub)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := page.WaitNavigation(ctx, navigation); err != nil {
		t.Fatal(err)
	}

	head := computedStyleTestElement(page.document.Root(), "head")
	if head == nil {
		t.Fatal("navigated document has no head")
	}
	headID, _ := page.document.ID(head)
	styleID, err := page.document.CreateElement("style")
	if err != nil {
		t.Fatal(err)
	}
	textID, err := page.document.CreateTextNode(`@import "dynamic.css";`)
	if err != nil {
		t.Fatal(err)
	}
	if err := page.document.AppendNode(styleID, textID); err != nil {
		t.Fatal(err)
	}
	if err := page.document.AppendNode(headID, styleID); err != nil {
		t.Fatal(err)
	}
	if err := page.syncAndLoadStylesheets(); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-loaderStub.fetched:
		if got != "https://graph.test/dynamic.css" {
			t.Fatalf("embedded import URL = %q", got)
		}
	case <-ctx.Done():
		t.Fatal("embedded import fetch did not start")
	}
	if err := page.Realm.RunOne(ctx); err != nil {
		t.Fatal(err)
	}

	targetID, ok := page.document.ElementByID("target")
	if !ok {
		t.Fatal("navigated target was not indexed")
	}
	computed, err := page.ComputedStyle(NodeHandle{Document: page.DocumentGeneration(), Node: targetID})
	if err != nil {
		t.Fatal(err)
	}
	if got := computed.Color(); got.R != 0x12 || got.G != 0x34 || got.B != 0x56 || got.A != 0xff {
		t.Fatalf("embedded imported color = %#v", got)
	}
}

type stylesheetGraphLoader struct {
	fetched chan string
}

func (loaderStub *stylesheetGraphLoader) Load(_ context.Context, rawURL string) (*loader.Response, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	return &loader.Response{
		URL: parsed, StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(`<!doctype html><html><head></head><body><div id="target">target</div></body></html>`)),
	}, nil
}

func (loaderStub *stylesheetGraphLoader) LoadResource(_ context.Context, rawURL string, _ loader.Destination) (*loader.Response, error) {
	loaderStub.fetched <- rawURL
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	header := make(http.Header)
	header.Set("Content-Type", "text/css")
	return &loader.Response{
		URL: parsed, StatusCode: http.StatusOK, Header: header,
		Body: io.NopCloser(bytes.NewBufferString(`#target { color: #123456 }`)),
	}, nil
}
