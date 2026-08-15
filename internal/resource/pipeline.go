package resource

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"sync"

	"github.com/JediWattson/gossamer/internal/loader"
)

const (
	DefaultMaxResourceBytes int64 = 32 << 20
	DefaultMaxCacheBytes    int64 = 64 << 20
)

// ErrResourceTooLarge marks a response that exceeded the configured bounded
// read limit.
var ErrResourceTooLarge = errors.New("resource exceeds size limit")

// Fetcher is the HTTP boundary used by Pipeline. loader.Loader implements it.
type Fetcher interface {
	LoadResource(context.Context, string, loader.Destination) (*loader.Response, error)
}

// PipelineOptions configure resource retrieval. Zero or negative maxima select
// their corresponding defaults.
type PipelineOptions struct {
	MaxResourceBytes int64
	MaxCacheBytes    int64
}

// Asset is an immutable cached HTTP resource. Bytes returns a defensive copy.
type Asset struct {
	URL        *url.URL
	StatusCode int
	Header     http.Header

	data []byte
}

// Result associates one DOM reference with its fetched resource or error.
// Multiple references may share the same cached asset bytes.
type Result struct {
	Reference Reference
	Asset     *Asset
	Err       error
}

// Bytes returns an owned copy of the response body.
func (asset *Asset) Bytes() []byte {
	if asset == nil {
		return nil
	}
	return append([]byte(nil), asset.data...)
}

// Size reports the response-body size in bytes.
func (asset *Asset) Size() int {
	if asset == nil {
		return 0
	}
	return len(asset.data)
}

// ContentType returns the server's Content-Type header.
func (asset *Asset) ContentType() string {
	if asset == nil {
		return ""
	}
	return asset.Header.Get("Content-Type")
}

// Pipeline retrieves bounded resources and caches successful transport
// results by destination and request URL for the lifetime of a navigation.
type Pipeline struct {
	fetcher       Fetcher
	maxBytes      int64
	maxCacheBytes int64

	mutex      sync.Mutex
	cache      map[string]*list.Element
	recent     *list.List
	cacheBytes int64
}

type cacheEntry struct {
	key   string
	asset *Asset
	size  int64
}

// NewPipeline creates a navigation-scoped resource pipeline. A nil fetcher
// uses the default HTTP loader.
func NewPipeline(fetcher Fetcher, options PipelineOptions) *Pipeline {
	if fetcher == nil {
		fetcher = loader.New(nil)
	}
	maxBytes := options.MaxResourceBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxResourceBytes
	}
	maxCacheBytes := options.MaxCacheBytes
	if maxCacheBytes <= 0 {
		maxCacheBytes = DefaultMaxCacheBytes
	}
	return &Pipeline{
		fetcher:       fetcher,
		maxBytes:      maxBytes,
		maxCacheBytes: maxCacheBytes,
		cache:         make(map[string]*list.Element),
		recent:        list.New(),
	}
}

// Fetch loads reference or returns its cached bytes. Response bodies are
// always closed before Fetch returns.
func (pipeline *Pipeline) Fetch(ctx context.Context, reference Reference) (*Asset, error) {
	if pipeline == nil {
		return nil, fmt.Errorf("resource: nil pipeline")
	}
	if reference.URL == nil {
		return nil, fmt.Errorf("resource: nil reference URL")
	}
	if reference.Kind != Stylesheet && reference.Kind != Image {
		return nil, fmt.Errorf("resource: unsupported kind %d", reference.Kind)
	}

	requestURL := cloneURL(reference.URL)
	requestURL.Fragment = ""
	key := cacheKey(reference.Kind, requestURL)
	if cached := pipeline.cached(key); cached != nil {
		return cached, nil
	}

	response, err := pipeline.fetcher.LoadResource(ctx, requestURL.String(), reference.Kind.destination())
	if err != nil {
		return nil, fmt.Errorf("fetch %s %q: %w", reference.Kind, requestURL, err)
	}
	if response == nil || response.Body == nil {
		return nil, fmt.Errorf("fetch %s %q: empty response", reference.Kind, requestURL)
	}

	readLimit := pipeline.maxBytes
	if readLimit < math.MaxInt64 {
		readLimit++
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, readLimit))
	closeErr := response.Body.Close()
	if readErr != nil {
		return nil, fmt.Errorf("read %s %q: %w", reference.Kind, requestURL, readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close %s %q: %w", reference.Kind, requestURL, closeErr)
	}
	if int64(len(body)) > pipeline.maxBytes {
		return nil, fmt.Errorf("%w: %s %q is larger than %d bytes", ErrResourceTooLarge, reference.Kind, requestURL, pipeline.maxBytes)
	}

	finalURL := requestURL
	if response.URL != nil {
		finalURL = cloneURL(response.URL)
	}
	asset := &Asset{
		URL:        finalURL,
		StatusCode: response.StatusCode,
		Header:     response.Header.Clone(),
		data:       append([]byte(nil), body...),
	}
	pipeline.store(key, asset)
	return cloneAsset(asset), nil
}

// FetchAll retrieves references synchronously in DOM order. The per-reference
// result shape is stable if a later scheduler performs independent fetches in
// parallel, and a failed subresource does not hide successful siblings.
func (pipeline *Pipeline) FetchAll(ctx context.Context, references []Reference) []Result {
	results := make([]Result, 0, len(references))
	for _, reference := range references {
		asset, err := pipeline.Fetch(ctx, reference)
		results = append(results, Result{Reference: reference, Asset: asset, Err: err})
	}
	return results
}

// Clear drops all navigation-scoped cached resources.
func (pipeline *Pipeline) Clear() {
	if pipeline == nil {
		return
	}
	pipeline.mutex.Lock()
	pipeline.cache = make(map[string]*list.Element)
	pipeline.recent.Init()
	pipeline.cacheBytes = 0
	pipeline.mutex.Unlock()
}

// CacheSize reports the number of destination/URL entries currently cached.
func (pipeline *Pipeline) CacheSize() int {
	if pipeline == nil {
		return 0
	}
	pipeline.mutex.Lock()
	size := len(pipeline.cache)
	pipeline.mutex.Unlock()
	return size
}

// CacheBytes reports the total response-body bytes retained by the cache.
func (pipeline *Pipeline) CacheBytes() int64 {
	if pipeline == nil {
		return 0
	}
	pipeline.mutex.Lock()
	size := pipeline.cacheBytes
	pipeline.mutex.Unlock()
	return size
}

func (pipeline *Pipeline) cached(key string) *Asset {
	pipeline.mutex.Lock()
	element := pipeline.cache[key]
	if element == nil {
		pipeline.mutex.Unlock()
		return nil
	}
	pipeline.recent.MoveToFront(element)
	asset := cloneAsset(element.Value.(*cacheEntry).asset)
	pipeline.mutex.Unlock()
	return asset
}

func (pipeline *Pipeline) store(key string, asset *Asset) {
	size := int64(asset.Size())
	if size > pipeline.maxCacheBytes {
		return
	}
	owned := cloneAsset(asset)
	pipeline.mutex.Lock()
	if existing := pipeline.cache[key]; existing != nil {
		entry := existing.Value.(*cacheEntry)
		pipeline.cacheBytes -= entry.size
		entry.asset = owned
		entry.size = size
		pipeline.cacheBytes += size
		pipeline.recent.MoveToFront(existing)
	} else {
		entry := &cacheEntry{key: key, asset: owned, size: size}
		pipeline.cache[key] = pipeline.recent.PushFront(entry)
		pipeline.cacheBytes += size
	}
	for pipeline.cacheBytes > pipeline.maxCacheBytes {
		oldest := pipeline.recent.Back()
		if oldest == nil {
			break
		}
		entry := oldest.Value.(*cacheEntry)
		delete(pipeline.cache, entry.key)
		pipeline.cacheBytes -= entry.size
		pipeline.recent.Remove(oldest)
	}
	pipeline.mutex.Unlock()
}

func cacheKey(kind Kind, resourceURL *url.URL) string {
	return strconv.Itoa(int(kind)) + "\x00" + resourceURL.String()
}

func cloneAsset(source *Asset) *Asset {
	if source == nil {
		return nil
	}
	return &Asset{
		URL:        cloneURL(source.URL),
		StatusCode: source.StatusCode,
		Header:     source.Header.Clone(),
		data:       append([]byte(nil), source.data...),
	}
}
