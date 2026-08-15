package browser

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/loader"
	"github.com/JediWattson/gossamer/internal/resource"
)

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
