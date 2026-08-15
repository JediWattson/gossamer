package resource_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/JediWattson/gossamer/internal/loader"
	"github.com/JediWattson/gossamer/internal/resource"
)

func TestPipelineFetchesAndCachesDefensiveCopies(t *testing.T) {
	t.Parallel()

	fetcher := &stubFetcher{response: func(rawURL string, _ loader.Destination) *loader.Response {
		return &loader.Response{
			URL:        mustURL(t, "https://cdn.example/final.css"),
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"text/css"}},
			Body:       io.NopCloser(strings.NewReader("body { color: navy }")),
		}
	}}
	pipeline := resource.NewPipeline(fetcher, resource.PipelineOptions{})
	reference := resource.Reference{Kind: resource.Stylesheet, URL: mustURL(t, "https://example.com/site.css#theme")}

	first, err := pipeline.Fetch(context.Background(), reference)
	if err != nil {
		t.Fatalf("first Fetch() error = %v", err)
	}
	if got, want := first.URL.String(), "https://cdn.example/final.css"; got != want {
		t.Errorf("final URL = %q, want %q", got, want)
	}
	if got, want := first.ContentType(), "text/css"; got != want {
		t.Errorf("ContentType() = %q, want %q", got, want)
	}
	if got, want := string(first.Bytes()), "body { color: navy }"; got != want {
		t.Errorf("Bytes() = %q, want %q", got, want)
	}

	bytes := first.Bytes()
	bytes[0] = 'X'
	first.Header.Set("Content-Type", "corrupted")
	first.URL.Host = "corrupted.example"
	second, err := pipeline.Fetch(context.Background(), reference)
	if err != nil {
		t.Fatalf("cached Fetch() error = %v", err)
	}
	if got, want := string(second.Bytes()), "body { color: navy }"; got != want {
		t.Errorf("cached Bytes() = %q, want %q", got, want)
	}
	if second.ContentType() != "text/css" || second.URL.Host != "cdn.example" {
		t.Errorf("cached metadata was mutated: URL=%q Content-Type=%q", second.URL, second.ContentType())
	}
	if got := fetcher.callCount(); got != 1 {
		t.Errorf("fetch calls = %d, want 1", got)
	}
	if got := pipeline.CacheSize(); got != 1 {
		t.Errorf("CacheSize() = %d, want 1", got)
	}

	pipeline.Clear()
	if got := pipeline.CacheSize(); got != 0 {
		t.Errorf("CacheSize() after Clear = %d, want 0", got)
	}
}

func TestPipelineCacheSeparatesDestinations(t *testing.T) {
	t.Parallel()

	fetcher := &stubFetcher{response: func(rawURL string, _ loader.Destination) *loader.Response {
		return &loader.Response{URL: mustURL(t, rawURL), StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("resource"))}
	}}
	pipeline := resource.NewPipeline(fetcher, resource.PipelineOptions{})
	resourceURL := mustURL(t, "https://example.com/shared")
	for _, kind := range []resource.Kind{resource.Stylesheet, resource.Image, resource.Stylesheet, resource.Image} {
		if _, err := pipeline.Fetch(context.Background(), resource.Reference{Kind: kind, URL: resourceURL}); err != nil {
			t.Fatalf("Fetch(%s) error = %v", kind, err)
		}
	}
	if got := fetcher.callCount(); got != 2 {
		t.Errorf("fetch calls = %d, want one per destination (2)", got)
	}
	if got := pipeline.CacheSize(); got != 2 {
		t.Errorf("CacheSize() = %d, want 2", got)
	}
}

func TestPipelineEnforcesBoundAndClosesBody(t *testing.T) {
	t.Parallel()

	body := &trackingReadCloser{Reader: strings.NewReader("12345")}
	fetcher := &stubFetcher{response: func(rawURL string, _ loader.Destination) *loader.Response {
		return &loader.Response{URL: mustURL(t, rawURL), StatusCode: http.StatusOK, Body: body}
	}}
	pipeline := resource.NewPipeline(fetcher, resource.PipelineOptions{MaxResourceBytes: 4})
	_, err := pipeline.Fetch(context.Background(), resource.Reference{
		Kind: resource.Image,
		URL:  mustURL(t, "https://example.com/large.png"),
	})
	if !errors.Is(err, resource.ErrResourceTooLarge) {
		t.Fatalf("Fetch() error = %v, want ErrResourceTooLarge", err)
	}
	if !body.closed {
		t.Error("response body was not closed")
	}
	if got := pipeline.CacheSize(); got != 0 {
		t.Errorf("CacheSize() = %d, want oversized response uncached", got)
	}
}

func TestPipelineDoesNotCacheTransportErrors(t *testing.T) {
	t.Parallel()

	fetcher := &stubFetcher{err: errors.New("network unavailable")}
	pipeline := resource.NewPipeline(fetcher, resource.PipelineOptions{})
	reference := resource.Reference{Kind: resource.Image, URL: mustURL(t, "https://example.com/image.png")}
	for range 2 {
		if _, err := pipeline.Fetch(context.Background(), reference); err == nil || !strings.Contains(err.Error(), "network unavailable") {
			t.Errorf("Fetch() error = %v, want transport error", err)
		}
	}
	if got := fetcher.callCount(); got != 2 {
		t.Errorf("fetch calls = %d, want 2", got)
	}
}

func TestPipelineEvictsLeastRecentlyUsedAssetsWithinBudget(t *testing.T) {
	t.Parallel()

	fetcher := &stubFetcher{response: func(rawURL string, _ loader.Destination) *loader.Response {
		return &loader.Response{
			URL:        mustURL(t, rawURL),
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("123")),
		}
	}}
	pipeline := resource.NewPipeline(fetcher, resource.PipelineOptions{
		MaxResourceBytes: 10,
		MaxCacheBytes:    5,
	})
	first := resource.Reference{Kind: resource.Image, URL: mustURL(t, "https://example.com/first.png")}
	second := resource.Reference{Kind: resource.Image, URL: mustURL(t, "https://example.com/second.png")}
	if _, err := pipeline.Fetch(context.Background(), first); err != nil {
		t.Fatalf("Fetch(first) error = %v", err)
	}
	if _, err := pipeline.Fetch(context.Background(), second); err != nil {
		t.Fatalf("Fetch(second) error = %v", err)
	}
	if got := pipeline.CacheSize(); got != 1 {
		t.Errorf("CacheSize() = %d, want 1 after eviction", got)
	}
	if got := pipeline.CacheBytes(); got != 3 {
		t.Errorf("CacheBytes() = %d, want 3", got)
	}
	if _, err := pipeline.Fetch(context.Background(), first); err != nil {
		t.Fatalf("second Fetch(first) error = %v", err)
	}
	if got := fetcher.callCount(); got != 3 {
		t.Errorf("fetch calls = %d, want evicted first asset to be fetched again (3)", got)
	}
}

func TestPipelineFetchAllPreservesDOMConsumersAndUsesCache(t *testing.T) {
	t.Parallel()

	fetcher := &stubFetcher{response: func(rawURL string, _ loader.Destination) *loader.Response {
		return &loader.Response{
			URL:        mustURL(t, rawURL),
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("shared image")),
		}
	}}
	pipeline := resource.NewPipeline(fetcher, resource.PipelineOptions{})
	sharedURL := mustURL(t, "https://example.com/shared.png")
	first := resource.Reference{Kind: resource.Image, URL: sharedURL}
	second := resource.Reference{Kind: resource.Image, URL: sharedURL}

	results := pipeline.FetchAll(context.Background(), []resource.Reference{first, second})
	if got := len(results); got != 2 {
		t.Fatalf("len(FetchAll()) = %d, want 2", got)
	}
	for index, result := range results {
		if result.Err != nil {
			t.Errorf("results[%d].Err = %v", index, result.Err)
		}
		if got, want := string(result.Asset.Bytes()), "shared image"; got != want {
			t.Errorf("results[%d] body = %q, want %q", index, got, want)
		}
	}
	if got := fetcher.callCount(); got != 1 {
		t.Errorf("fetch calls = %d, want duplicate consumers to share one fetch", got)
	}
}

type stubFetcher struct {
	mutex    sync.Mutex
	calls    int
	response func(string, loader.Destination) *loader.Response
	err      error
}

func (fetcher *stubFetcher) LoadResource(_ context.Context, rawURL string, destination loader.Destination) (*loader.Response, error) {
	fetcher.mutex.Lock()
	fetcher.calls++
	fetcher.mutex.Unlock()
	if fetcher.err != nil {
		return nil, fetcher.err
	}
	return fetcher.response(rawURL, destination), nil
}

func (fetcher *stubFetcher) callCount() int {
	fetcher.mutex.Lock()
	defer fetcher.mutex.Unlock()
	return fetcher.calls
}

type trackingReadCloser struct {
	*strings.Reader
	closed bool
}

func (body *trackingReadCloser) Close() error {
	body.closed = true
	return nil
}

var _ resource.Fetcher = (*stubFetcher)(nil)
