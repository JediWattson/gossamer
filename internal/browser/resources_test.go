package browser

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/loader"
	"github.com/JediWattson/gossamer/internal/render"
	"github.com/JediWattson/gossamer/internal/resource"
)

func TestImageResourceGraphDecodesDataURLAndRejectsStaleResults(t *testing.T) {
	t.Parallel()

	pixel := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	pixel.SetNRGBA(0, 0, color.NRGBA{R: 0xef, G: 0x4f, B: 0x91, A: 0xff})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, pixel); err != nil {
		t.Fatal(err)
	}
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(encoded.Bytes())

	root := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body")
	imageNode := dom.NewElement("img", dom.Attribute{Name: "src", Value: dataURL})
	root.AppendChild(html)
	html.AppendChild(body)
	body.AppendChild(imageNode)
	location, _ := url.Parse("https://images.test/app/")
	browser, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer browser.Close()
	page, err := browser.NewPage(root, location)
	if err != nil {
		t.Fatal(err)
	}
	imageID, _ := page.document.ID(imageNode)

	page.mutex.Lock()
	requests, changed, err := page.syncImagesLocked()
	decoded := page.resources.images[imageID]
	page.mutex.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 0 || !changed || decoded == nil {
		t.Fatalf("data image sync = requests:%d changed:%t decoded:%v", len(requests), changed, decoded != nil)
	}
	if got := color.NRGBAModel.Convert(decoded.At(0, 0)).(color.NRGBA); got != (color.NRGBA{R: 0xef, G: 0x4f, B: 0x91, A: 0xff}) {
		t.Fatalf("decoded data image pixel = %#v", got)
	}

	page.mutex.Lock()
	if err := page.document.SetAttribute(imageID, "src", "first.png"); err != nil {
		page.mutex.Unlock()
		t.Fatal(err)
	}
	first, _, err := page.syncImagesLocked()
	page.mutex.Unlock()
	if err != nil || len(first) != 1 {
		t.Fatalf("first external sync = %#v, %v", first, err)
	}

	page.mutex.Lock()
	if err := page.document.SetAttribute(imageID, "src", "second.png"); err != nil {
		page.mutex.Unlock()
		t.Fatal(err)
	}
	second, _, err := page.syncImagesLocked()
	if err != nil || len(second) != 1 {
		page.mutex.Unlock()
		t.Fatalf("second external sync = %#v, %v", second, err)
	}
	oldApplied := page.resources.apply(navigationResourceResult{
		target: NodeHandle{Document: page.documentGeneration, Node: imageID}, kind: resource.Image,
		image: pixel, imageGeneration: first[0].imageGeneration, imageSource: first[0].imageSource,
	})
	newApplied := page.resources.apply(navigationResourceResult{
		target: NodeHandle{Document: page.documentGeneration, Node: imageID}, kind: resource.Image,
		image: pixel, imageGeneration: second[0].imageGeneration, imageSource: second[0].imageSource,
	})
	page.mutex.Unlock()
	if oldApplied || !newApplied {
		t.Fatalf("stale applied = %t, current applied = %t", oldApplied, newApplied)
	}

	page.mutex.Lock()
	if err := page.document.RemoveAttribute(imageID, "src"); err != nil {
		page.mutex.Unlock()
		t.Fatal(err)
	}
	_, changed, err = page.syncImagesLocked()
	_, imageRemains := page.resources.images[imageID]
	_, sourceRemains := page.resources.imageSources[imageID]
	page.mutex.Unlock()
	if err != nil || !changed || imageRemains || sourceRemains {
		t.Fatalf("removed image sync = changed:%t image:%t source:%t err:%v", changed, imageRemains, sourceRemains, err)
	}
}

func TestInitialDataImageIsDecodedBeforeNavigationRender(t *testing.T) {
	t.Parallel()

	pixel := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, pixel); err != nil {
		t.Fatal(err)
	}
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(encoded.Bytes())
	root := dom.NewDocument()
	html := dom.NewElement("html")
	imageNode := dom.NewElement("img", dom.Attribute{Name: "src", Value: dataURL})
	root.AppendChild(html)
	html.AppendChild(imageNode)
	document, err := dom.IndexDocument(root)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://images.test/")
	resources := newPageResources()
	if err := resources.decodeInitialInlineImages(document, location); err != nil {
		t.Fatal(err)
	}
	imageID, _ := document.ID(imageNode)
	if resources.images[imageID] == nil {
		t.Fatal("initial data image was not decoded")
	}
}

func TestDynamicImageFetchQueuesRepaint(t *testing.T) {
	t.Parallel()

	pixel := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	pixel.SetNRGBA(0, 0, color.NRGBA{R: 0x42, G: 0x81, B: 0xc3, A: 0xff})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, pixel); err != nil {
		t.Fatal(err)
	}
	fetcher := &signalingImageFetcher{body: encoded.Bytes(), fetched: make(chan string, 1)}

	root := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body")
	imageNode := dom.NewElement("img",
		dom.Attribute{Name: "src", Value: "logo.png"},
		dom.Attribute{Name: "width", Value: "1"},
		dom.Attribute{Name: "height", Value: "1"},
	)
	root.AppendChild(html)
	html.AppendChild(body)
	body.AppendChild(imageNode)
	location, _ := url.Parse("https://images.test/app/")
	browser, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer browser.Close()
	page, err := browser.NewPage(root, location)
	if err != nil {
		t.Fatal(err)
	}
	page.mutex.Lock()
	page.resourceFetcher = fetcher
	page.mutex.Unlock()
	if err := page.syncAndLoadImages(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	select {
	case fetched := <-fetcher.fetched:
		if fetched != "https://images.test/app/logo.png" {
			t.Fatalf("fetched image = %q", fetched)
		}
	case <-ctx.Done():
		t.Fatal("dynamic image fetch did not start")
	}
	if err := page.Realm.RunOne(ctx); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(ctx); err != nil {
		t.Fatal(err)
	}
	frame := page.Frame()
	if frame == nil {
		t.Fatal("dynamic image result did not publish a frame")
	}
	found := false
	for _, command := range frame.DisplayList.Commands {
		if command.Kind == render.DrawImageCommand {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("dynamic image result did not paint")
	}
}

func TestNavigationResourceSequenceSharesDecodedImageAndHonorsTotalPixelBudget(t *testing.T) {
	t.Parallel()

	pixel := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	pixel.SetNRGBA(0, 0, color.NRGBA{R: 0x42, G: 0x81, B: 0xc3, A: 0xff})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, pixel); err != nil {
		t.Fatal(err)
	}
	fetcher := &countingImageFetcher{body: encoded.Bytes(), calls: make(map[string]int)}
	pipeline := resource.NewPipeline(fetcher, resource.PipelineOptions{})
	sharedURL, _ := url.Parse("https://example.test/shared.png")
	laterURL, _ := url.Parse("https://example.test/later.png")
	requests := []navigationResourceRequest{
		{kind: resource.Image, url: sharedURL, node: 1},
		{kind: resource.Image, url: sharedURL, node: 2},
		{kind: resource.Image, url: laterURL, node: 3},
	}
	var results []navigationResourceResult
	err := loadNavigationResourceSequence(
		context.Background(),
		pipeline,
		1,
		requests,
		1,
		func(result navigationResourceResult) error {
			results = append(results, result)
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := fetcher.calls[sharedURL.String()]; got != 1 {
		t.Errorf("shared image fetches = %d, want 1", got)
	}
	if got := fetcher.calls[laterURL.String()]; got != 0 {
		t.Errorf("later image fetches = %d, want 0 after budget exhaustion", got)
	}
	if len(results) != 3 {
		t.Fatalf("results = %d, want 3", len(results))
	}
	if results[0].image == nil || results[1].image == nil || results[0].image != results[1].image {
		t.Fatal("duplicate consumers did not share one decoded image")
	}
	if results[2].err == nil || results[2].image != nil {
		t.Fatalf("later result = %#v, want pixel-budget failure", results[2])
	}
}

func TestIsRenderedReferenceFiltersInactiveAndUnsupportedConsumers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		node      *dom.Node
		kind      resource.Kind
		attribute string
		want      bool
	}{
		{name: "stylesheet", node: dom.NewElement("link", dom.Attribute{Name: "rel", Value: "stylesheet"}), kind: resource.Stylesheet, attribute: "href", want: true},
		{name: "screen stylesheet", node: dom.NewElement("link", dom.Attribute{Name: "rel", Value: "stylesheet"}, dom.Attribute{Name: "media", Value: "screen"}), kind: resource.Stylesheet, attribute: "href", want: true},
		{name: "alternate stylesheet", node: dom.NewElement("link", dom.Attribute{Name: "rel", Value: "alternate stylesheet"}), kind: resource.Stylesheet, attribute: "href"},
		{name: "disabled stylesheet", node: dom.NewElement("link", dom.Attribute{Name: "rel", Value: "stylesheet"}, dom.Attribute{Name: "disabled"}), kind: resource.Stylesheet, attribute: "href"},
		{name: "print stylesheet fetched for render filtering", node: dom.NewElement("link", dom.Attribute{Name: "rel", Value: "stylesheet"}, dom.Attribute{Name: "media", Value: "print"}), kind: resource.Stylesheet, attribute: "href", want: true},
		{name: "image", node: dom.NewElement("img"), kind: resource.Image, attribute: "src", want: true},
		{name: "icon", node: dom.NewElement("link"), kind: resource.Image, attribute: "href"},
		{name: "video poster", node: dom.NewElement("video"), kind: resource.Image, attribute: "poster"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			reference := resource.Reference{Kind: test.kind, Node: test.node, Attribute: test.attribute}
			if got := isRenderedReference(reference); got != test.want {
				t.Errorf("isRenderedReference() = %t, want %t", got, test.want)
			}
		})
	}
}

type countingImageFetcher struct {
	body  []byte
	calls map[string]int
}

type signalingImageFetcher struct {
	body    []byte
	fetched chan string
}

func (fetcher *signalingImageFetcher) LoadResource(
	_ context.Context,
	rawURL string,
	_ loader.Destination,
) (*loader.Response, error) {
	fetcher.fetched <- rawURL
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	header := make(http.Header)
	header.Set("Content-Type", "image/png")
	return &loader.Response{
		URL: parsedURL, StatusCode: http.StatusOK, Header: header,
		Body: io.NopCloser(bytes.NewReader(fetcher.body)),
	}, nil
}

func (fetcher *countingImageFetcher) LoadResource(
	_ context.Context,
	rawURL string,
	_ loader.Destination,
) (*loader.Response, error) {
	fetcher.calls[rawURL]++
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	header := make(http.Header)
	header.Set("Content-Type", "image/png")
	return &loader.Response{
		URL:        parsedURL,
		StatusCode: http.StatusOK,
		Header:     header,
		Body:       io.NopCloser(bytes.NewReader(fetcher.body)),
	}, nil
}
