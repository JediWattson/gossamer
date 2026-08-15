package browser

import (
	"context"
	"errors"
	"image/color"
	"io"
	"net/url"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
	htmlparser "github.com/JediWattson/gossamer/internal/html"
	"github.com/JediWattson/gossamer/internal/loader"
	"github.com/JediWattson/gossamer/internal/render"
	computed "github.com/JediWattson/gossamer/internal/style"
)

func TestPageComputedStyleFlushesWithoutPublishingFrameAndRenderReusesSnapshot(t *testing.T) {
	t.Parallel()

	engine, page, target := computedStyleTestPage(t, `
		<html><head><style>
			#target { display: block; color: #112233; width: 25vw; }
		</style></head><body><div id="target"></div></body></html>
	`)
	defer engine.Close()

	handle := NodeHandle{Document: page.DocumentGeneration(), Node: target}
	style, err := page.ComputedStyle(handle)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := style.Display(), computed.DisplayBlock; got != want {
		t.Fatalf("display = %v, want %v", got, want)
	}
	if got, want := style.Color(), (color.NRGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xff}); got != want {
		t.Fatalf("color = %#v, want %#v", got, want)
	}
	if got, want := style.Width().Unit(), computed.LengthVW; got != want || style.Width().Value() != 25 {
		t.Fatalf("width = %v %v, want 25vw", style.Width().Value(), got)
	}
	if page.Frame() != nil {
		t.Fatal("computed-style flush unexpectedly published a frame")
	}
	if !page.Dirty() {
		t.Fatal("computed-style flush cleared render dirtiness")
	}
	firstSnapshot := page.computedStyle.snapshot
	if firstSnapshot == nil {
		t.Fatal("computed-style flush did not cache a snapshot")
	}

	if err := page.document.SetAttribute(target, "style", "color: #aabbcc"); err != nil {
		t.Fatal(err)
	}
	style, err = page.ComputedStyle(handle)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := style.Color(), (color.NRGBA{R: 0xaa, G: 0xbb, B: 0xcc, A: 0xff}); got != want {
		t.Fatalf("mutated color = %#v, want %#v", got, want)
	}
	secondSnapshot := page.computedStyle.snapshot
	if secondSnapshot == nil || secondSnapshot == firstSnapshot {
		t.Fatal("DOM version change did not replace the computed-style snapshot")
	}
	if page.Frame() != nil || !page.Dirty() {
		t.Fatal("second style-only flush changed frame publication state")
	}

	if err := page.Render(); err != nil {
		t.Fatal(err)
	}
	if page.Frame() == nil || page.Dirty() {
		t.Fatal("render did not publish and clean the pending frame")
	}
	if page.computedStyle.snapshot != secondSnapshot {
		t.Fatal("render recomputed instead of reusing the current style snapshot")
	}
	if page.Frame().ComputedStyles != secondSnapshot {
		t.Fatal("published frame does not retain the reused style snapshot")
	}
}

func TestPageComputedStyleInvalidatesForViewportAndExternalStylesheets(t *testing.T) {
	t.Parallel()

	engine, page, target := computedStyleTestPage(t, `
		<html><head>
			<link rel="stylesheet" href="theme.css">
			<style>
				#target { color: #111111; }
				@media (min-width: 700px) { #target { color: #222222; } }
			</style>
		</head><body><div id="target"></div></body></html>
	`)
	defer engine.Close()

	handle := NodeHandle{Document: page.DocumentGeneration(), Node: target}
	initial, err := page.ComputedStyle(handle)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := initial.Color(), (color.NRGBA{R: 0x22, G: 0x22, B: 0x22, A: 0xff}); got != want {
		t.Fatalf("wide viewport color = %#v, want %#v", got, want)
	}
	wideSnapshot := page.computedStyle.snapshot

	if err := page.SetViewport(render.Viewport{Width: 600, Height: 600}); err != nil {
		t.Fatal(err)
	}
	narrow, err := page.ComputedStyle(handle)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := narrow.Color(), (color.NRGBA{R: 0x11, G: 0x11, B: 0x11, A: 0xff}); got != want {
		t.Fatalf("narrow viewport color = %#v, want %#v", got, want)
	}
	if page.computedStyle.snapshot == wideSnapshot {
		t.Fatal("viewport change reused a stale style snapshot")
	}
	narrowSnapshot := page.computedStyle.snapshot

	link := computedStyleTestElement(page.document.Root(), "link")
	stylesheet, err := css.Parse(`#target { color: #334455 !important; }`)
	if err != nil {
		t.Fatal(err)
	}
	if err := page.SetResources(render.Resources{
		Stylesheets: map[*dom.Node]css.Stylesheet{link: stylesheet},
	}); err != nil {
		t.Fatal(err)
	}
	withExternal, err := page.ComputedStyle(handle)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := withExternal.Color(), (color.NRGBA{R: 0x33, G: 0x44, B: 0x55, A: 0xff}); got != want {
		t.Fatalf("external cascade color = %#v, want %#v", got, want)
	}
	if page.computedStyle.snapshot == narrowSnapshot {
		t.Fatal("resource change reused a stale style snapshot")
	}
}

func TestPageComputedStyleValidatesHandleAndClearsOnClose(t *testing.T) {
	t.Parallel()

	engine, page, target := computedStyleTestPage(t, `<html><body><div id="target">text</div></body></html>`)
	defer engine.Close()

	if _, err := page.ComputedStyle(NodeHandle{Document: page.DocumentGeneration() + 1, Node: target}); !errors.Is(err, ErrStaleNodeHandle) {
		t.Fatalf("stale handle error = %v, want %v", err, ErrStaleNodeHandle)
	}
	text := computedStyleTestText(page.document.Root())
	textID, ok := page.document.ID(text)
	if !ok {
		t.Fatal("text node has no stable ID")
	}
	if _, err := page.ComputedStyle(NodeHandle{Document: page.DocumentGeneration(), Node: textID}); !errors.Is(err, dom.ErrWrongNodeKind) {
		t.Fatalf("text handle error = %v, want %v", err, dom.ErrWrongNodeKind)
	}
	if _, err := page.ComputedStyle(NodeHandle{Document: page.DocumentGeneration(), Node: target}); err != nil {
		t.Fatal(err)
	}
	if page.computedStyle.snapshot == nil {
		t.Fatal("computed style did not populate snapshot before close")
	}
	if err := page.Close(); err != nil {
		t.Fatal(err)
	}
	if page.computedStyle.snapshot != nil {
		t.Fatal("Page.Close retained the computed-style snapshot")
	}
}

func TestPageComputedStyleDoesNotCrossNavigationGeneration(t *testing.T) {
	t.Parallel()

	engine, page, target := computedStyleTestPage(t, `<html><body><div id="target" style="color:#112233"></div></body></html>`)
	defer engine.Close()

	oldHandle := NodeHandle{Document: page.DocumentGeneration(), Node: target}
	if _, err := page.ComputedStyle(oldHandle); err != nil {
		t.Fatal(err)
	}
	oldSnapshot := page.computedStyle.snapshot
	finalURL, _ := url.Parse("https://computed-style.test/next")
	navigation, err := page.Navigate(context.Background(), finalURL.String(), computedStyleDocumentLoader{
		url:    finalURL,
		source: `<html><body><div id="target" style="color:#aabbcc"></div></body></html>`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := page.WaitNavigation(context.Background(), navigation); err != nil {
		t.Fatal(err)
	}
	if _, err := page.ComputedStyle(oldHandle); !errors.Is(err, ErrStaleNodeHandle) {
		t.Fatalf("old-generation style error = %v, want %v", err, ErrStaleNodeHandle)
	}
	newTarget, ok := page.document.ElementByID("target")
	if !ok {
		t.Fatal("navigated target has no stable ID")
	}
	style, err := page.ComputedStyle(NodeHandle{Document: page.DocumentGeneration(), Node: newTarget})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := style.Color(), (color.NRGBA{R: 0xaa, G: 0xbb, B: 0xcc, A: 0xff}); got != want {
		t.Fatalf("navigated color = %#v, want %#v", got, want)
	}
	if page.computedStyle.snapshot == oldSnapshot || page.computedStyle.document != page.DocumentGeneration() {
		t.Fatal("navigation reused computed styles from the previous document generation")
	}
}

type computedStyleDocumentLoader struct {
	url    *url.URL
	source string
}

func (loaderStub computedStyleDocumentLoader) Load(context.Context, string) (*loader.Response, error) {
	return &loader.Response{
		URL:        loaderStub.url,
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(loaderStub.source)),
	}, nil
}

func computedStyleTestPage(t *testing.T, source string) (*Browser, *Page, dom.NodeID) {
	t.Helper()
	root, err := htmlparser.Parse(strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	engine, err := New()
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://computed-style.test/")
	page, err := engine.NewPage(root, location)
	if err != nil {
		engine.Close()
		t.Fatal(err)
	}
	target, ok := page.document.ElementByID("target")
	if !ok {
		engine.Close()
		t.Fatal("test target has no stable ID")
	}
	return engine, page, target
}

func computedStyleTestElement(root *dom.Node, name string) *dom.Node {
	if root == nil {
		return nil
	}
	if root.Type == dom.ElementNode && root.Data == name {
		return root
	}
	for _, child := range root.Children {
		if found := computedStyleTestElement(child, name); found != nil {
			return found
		}
	}
	return nil
}

func computedStyleTestText(root *dom.Node) *dom.Node {
	if root == nil {
		return nil
	}
	if root.Type == dom.TextNode && root.Data == "text" {
		return root
	}
	for _, child := range root.Children {
		if found := computedStyleTestText(child); found != nil {
			return found
		}
	}
	return nil
}
