package browser

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/loader"
	"github.com/JediWattson/gossamer/internal/render"
	"github.com/JediWattson/gossamer/internal/resource"
	"golang.org/x/image/font/gofont/gomonobold"
)

func TestStylesheetFontsResolveAgainstOwningImportAndRender(t *testing.T) {
	fetcher := &fontMapFetcher{responses: map[string]fontMapResponse{
		"https://fonts.test/css/root.css": {
			contentType: "text/css", body: []byte(`@import "theme/face.css"; p { font-family: "Fixture Web", serif; font-weight: bold }`),
		},
		"https://fonts.test/css/theme/face.css": {
			contentType: "text/css", body: []byte(`@font-face { font-family: "Fixture Web"; src: url(missing.woff2) format("woff2"), url(../../assets/fixture.ttf) format("truetype"); font-weight: 700; }`),
		},
		"https://fonts.test/css/theme/missing.woff2": {
			contentType: "font/woff2", status: http.StatusNotFound,
		},
		"https://fonts.test/assets/fixture.ttf": {
			contentType: "font/ttf", body: gomonobold.TTF,
		},
	}}
	pipeline := resource.NewPipeline(fetcher, resource.PipelineOptions{})
	rootURL, _ := url.Parse("https://fonts.test/css/root.css")
	asset, err := pipeline.Fetch(context.Background(), resource.Reference{Kind: resource.Stylesheet, URL: rootURL})
	if err != nil {
		t.Fatal(err)
	}
	stylesheet, err := loadStylesheetWithImports(context.Background(), pipeline, asset)
	if err != nil {
		t.Fatal(err)
	}
	if len(stylesheet.FontFaces) != 1 || len(stylesheet.FontFaces[0].Sources) != 2 {
		t.Fatalf("flattened font faces = %#v", stylesheet.FontFaces)
	}
	if got := stylesheet.FontFaces[0].Sources[1].URL; got != "https://fonts.test/assets/fixture.ttf" {
		t.Fatalf("resolved font URL = %q", got)
	}
	loaded, err := loadStylesheetFonts(context.Background(), pipeline, stylesheet, newFontLoadBudget())
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].face.Source != "https://fonts.test/assets/fixture.ttf" {
		t.Fatalf("loaded fonts = %#v", loaded)
	}
	if fetcher.destinations["https://fonts.test/assets/fixture.ttf"] != loader.FontDestination {
		t.Fatalf("font destination = %v, want FontDestination", fetcher.destinations["https://fonts.test/assets/fixture.ttf"])
	}

	document := dom.NewDocument()
	html := dom.NewElement("html")
	head := dom.NewElement("head")
	link := dom.NewElement("link")
	body := dom.NewElement("body")
	paragraph := dom.NewElement("p")
	text := dom.NewText("font pipeline")
	document.AppendChild(html)
	html.AppendChild(head)
	head.AppendChild(link)
	html.AppendChild(body)
	body.AppendChild(paragraph)
	paragraph.AppendChild(text)
	frame, err := render.RenderWithResources(document, render.Viewport{Width: 320, Height: 100}, render.Resources{
		Stylesheets: map[*dom.Node]css.Stylesheet{link: stylesheet},
		Fonts:       []render.WebFontFace{loaded[0].face},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range frame.DisplayList.Commands {
		if command.Kind == render.DrawTextCommand && command.Node == text {
			if command.WebFont != 1 {
				t.Fatalf("text web font = %d, want 1", command.WebFont)
			}
			return
		}
	}
	t.Fatal("no rendered text command")
}

func TestStylesheetGraphRejectsStaleFontsAndFiltersConditionalFaces(t *testing.T) {
	root := dom.NewDocument()
	html := dom.NewElement("html")
	link := dom.NewElement("link", dom.Attribute{Name: "rel", Value: "stylesheet"}, dom.Attribute{Name: "href", Value: "first.css"})
	root.AppendChild(html)
	html.AppendChild(link)
	document, err := dom.IndexDocument(root)
	if err != nil {
		t.Fatal(err)
	}
	linkID, _ := document.ID(link)
	location, _ := url.Parse("https://fonts.test/page")
	graph := newStylesheetGraph()
	requests, _, err := graph.sync(document, location)
	if err != nil || len(requests) != 1 {
		t.Fatalf("initial sync = %#v, %v", requests, err)
	}
	staleGeneration := requests[0].stylesheetGeneration
	if err := document.SetAttribute(linkID, "href", "second.css"); err != nil {
		t.Fatal(err)
	}
	requests, _, err = graph.sync(document, location)
	if err != nil || len(requests) != 1 {
		t.Fatalf("replacement sync = %#v, %v", requests, err)
	}
	currentGeneration := requests[0].stylesheetGeneration
	fixture := render.WebFontFace{Family: "Fixture", Data: gomonobold.TTF}
	if graph.apply(navigationResourceResult{
		target: NodeHandle{Node: linkID}, kind: resource.Stylesheet, stylesheetGeneration: staleGeneration,
		fonts: []loadedFontFace{{rule: css.FontFaceRule{Family: "Fixture"}, face: fixture}},
	}) {
		t.Fatal("stale stylesheet font result was accepted")
	}
	if !graph.apply(navigationResourceResult{
		target: NodeHandle{Node: linkID}, kind: resource.Stylesheet, stylesheetGeneration: currentGeneration,
		fonts: []loadedFontFace{
			{rule: css.FontFaceRule{Family: "Fixture", Media: []string{"print"}}, face: fixture},
			{rule: css.FontFaceRule{Family: "Fixture", Media: []string{"screen and (min-width: 600px)"}}, face: fixture},
		},
	}) {
		t.Fatal("current stylesheet font result was rejected")
	}
	if got := len(graph.resolvedFonts(render.Viewport{Width: 500, Height: 300})); got != 0 {
		t.Fatalf("narrow resolved fonts = %d, want 0", got)
	}
	if got := len(graph.resolvedFonts(render.Viewport{Width: 800, Height: 600})); got != 1 {
		t.Fatalf("wide resolved fonts = %d, want 1", got)
	}
}

func TestDynamicFontLoadQueuesDeterministicReflow(t *testing.T) {
	engine, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	location, _ := url.Parse("https://font-reflow.test/initial")
	page, err := engine.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	fontRequested := make(chan struct{}, 1)
	loaderStub := dynamicFontLoader{fontRequested: fontRequested}
	navigation, err := page.Navigate(context.Background(), "https://font-reflow.test/page", loaderStub)
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
	textID, err := page.document.CreateTextNode(`
		@font-face { font-family: "Dynamic Fixture"; src: url("fonts/fixture.ttf") format("truetype"); font-weight: 700; }
		#target { font-family: "Dynamic Fixture", serif; font-weight: 700; }
	`)
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
	case <-fontRequested:
	case <-ctx.Done():
		t.Fatal("dynamic font request did not start")
	}
	deadline := time.Now().Add(2 * time.Second)
	for page.Realm.Tasks.Len() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for dynamic font completion task")
		}
		time.Sleep(time.Millisecond)
	}
	if err := page.Realm.RunOne(ctx); err != nil {
		t.Fatal(err)
	}
	if page.Realm.Tasks.Len() != 1 {
		t.Fatalf("queued reflow tasks = %d, want 1", page.Realm.Tasks.Len())
	}
	if err := page.Realm.RunOne(ctx); err != nil {
		t.Fatal(err)
	}
	frame := page.Frame()
	for _, command := range frame.DisplayList.Commands {
		if command.Kind == render.DrawTextCommand && command.Text == "dynamic font" {
			if command.WebFont == 0 {
				t.Fatal("reflowed target still uses fallback font")
			}
			return
		}
	}
	t.Fatal("reflowed frame has no target text")
}

func TestPageCloseReleasesLoadedFontResources(t *testing.T) {
	engine, err := New()
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://font-release.test/")
	page, err := engine.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	page.resources.fonts = []render.WebFontFace{{Family: "Fixture", Source: "fixture.ttf", Data: append([]byte(nil), gomonobold.TTF...)}}
	page.resources.stylesheets.entries[dom.NodeID(1)] = stylesheetGraphEntry{
		fonts: []loadedFontFace{{face: render.WebFontFace{Family: "Fixture", Source: "fixture.ttf", Data: append([]byte(nil), gomonobold.TTF...)}}},
	}

	if err := page.Close(); err != nil {
		t.Fatal(err)
	}
	if page.resources.fonts != nil || page.resources.stylesheets.entries != nil {
		t.Fatal("closed page retained loaded font resources")
	}
	if err := engine.Close(); err != nil {
		t.Fatal(err)
	}
}

type fontMapResponse struct {
	contentType string
	status      int
	body        []byte
}

type dynamicFontLoader struct {
	fontRequested chan<- struct{}
}

func (loaderStub dynamicFontLoader) Load(_ context.Context, rawURL string) (*loader.Response, error) {
	location, _ := url.Parse(rawURL)
	return &loader.Response{
		URL: location, StatusCode: http.StatusOK,
		Body: io.NopCloser(bytes.NewBufferString(`<!doctype html><html><head></head><body><p id="target">dynamic font</p></body></html>`)),
	}, nil
}

func (loaderStub dynamicFontLoader) LoadResource(_ context.Context, rawURL string, destination loader.Destination) (*loader.Response, error) {
	if destination != loader.FontDestination || rawURL != "https://font-reflow.test/fonts/fixture.ttf" {
		return nil, fmt.Errorf("unexpected dynamic resource %s destination %d", rawURL, destination)
	}
	loaderStub.fontRequested <- struct{}{}
	location, _ := url.Parse(rawURL)
	header := make(http.Header)
	header.Set("Content-Type", "font/ttf")
	return &loader.Response{URL: location, StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(bytes.NewReader(gomonobold.TTF))}, nil
}

type fontMapFetcher struct {
	responses    map[string]fontMapResponse
	destinations map[string]loader.Destination
}

func (fetcher *fontMapFetcher) LoadResource(_ context.Context, rawURL string, destination loader.Destination) (*loader.Response, error) {
	if fetcher.destinations == nil {
		fetcher.destinations = make(map[string]loader.Destination)
	}
	fetcher.destinations[rawURL] = destination
	response, ok := fetcher.responses[rawURL]
	if !ok {
		return nil, fmt.Errorf("unexpected resource %s", rawURL)
	}
	location, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	status := response.status
	if status == 0 {
		status = http.StatusOK
	}
	header := make(http.Header)
	header.Set("Content-Type", response.contentType)
	return &loader.Response{URL: location, StatusCode: status, Header: header, Body: io.NopCloser(bytes.NewReader(response.body))}, nil
}
