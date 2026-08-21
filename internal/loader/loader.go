// Package loader retrieves resources for the browser engine.
package loader

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"
)

const (
	// UserAgent identifies Gossamer when it requests web resources.
	UserAgent = "Gossamer/0.1"

	defaultTimeout = 30 * time.Second
	acceptDocument = "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"
	acceptStyle    = "text/css,*/*;q=0.1"
	acceptImage    = "image/webp,image/png,image/jpeg,image/gif,*/*;q=0.1"
	acceptFont     = "font/woff2,font/woff,*/*;q=0.1"
	acceptScript   = "text/javascript,application/javascript,*/*;q=0.1"
)

// ErrInvalidURL marks URL input that cannot be loaded over HTTP or HTTPS.
var ErrInvalidURL = errors.New("invalid URL")

// Destination identifies how a fetched resource will be consumed. It controls
// request negotiation without coupling the HTTP loader to DOM elements.
type Destination uint8

const (
	DocumentDestination Destination = iota
	StyleDestination
	ImageDestination
	FontDestination
	ScriptDestination
)

// Response is a loaded resource. The caller must close Body.
type Response struct {
	URL        *url.URL
	StatusCode int
	Header     http.Header
	Body       io.ReadCloser
}

// Request is an engine-neutral HTTP request. Body is copied into the outgoing
// request, so callers may reuse or release the source bytes after Do returns.
type Request struct {
	URL    string
	Method string
	Header http.Header
	Body   []byte
}

// Requester is the document-lifetime HTTP boundary used by browser APIs such
// as fetch. Loader implements it while navigation tests may provide a small
// in-memory implementation.
type Requester interface {
	Do(context.Context, Request) (*Response, error)
}

// CookieStore exposes the Loader's session jar without exposing its HTTP
// client. Browser document.cookie bindings use this alongside navigation and
// fetch, which keeps all three surfaces in one session.
type CookieStore interface {
	Cookies(*url.URL) []*http.Cookie
	SetCookies(*url.URL, []*http.Cookie)
}

// Loader retrieves web resources over HTTP.
type Loader struct {
	httpClient *http.Client
}

// New creates a resource loader. A nil HTTP client uses a bounded default
// suitable for command-line navigation.
func New(httpClient *http.Client) *Loader {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	client := *httpClient
	if client.Jar == nil {
		client.Jar, _ = cookiejar.New(nil)
	}

	return &Loader{httpClient: &client}
}

// Load performs a document GET request. HTTP error statuses are returned as
// responses so callers can consume the document supplied by the server.
func (loader *Loader) Load(ctx context.Context, rawURL string) (*Response, error) {
	return loader.LoadResource(ctx, rawURL, DocumentDestination)
}

// LoadResource performs a GET for a document subresource. HTTP error statuses
// are returned as responses so the caller can decide whether they are usable.
func (loader *Loader) LoadResource(ctx context.Context, rawURL string, destination Destination) (*Response, error) {
	header := make(http.Header)
	header.Set("Accept", destination.accept())
	return loader.Do(ctx, Request{URL: rawURL, Method: http.MethodGet, Header: header})
}

// Do performs a general HTTP request through the same client and cookie jar as
// document navigation. HTTP error statuses remain ordinary responses.
func (loader *Loader) Do(ctx context.Context, input Request) (*Response, error) {
	if loader == nil || loader.httpClient == nil {
		return nil, fmt.Errorf("loader: nil HTTP client")
	}
	rawURL := input.URL
	parsedURL, err := ParseHTTPURL(rawURL)
	if err != nil {
		return nil, err
	}
	method := strings.ToUpper(strings.TrimSpace(input.Method))
	if method == "" {
		method = http.MethodGet
	}

	request, err := http.NewRequestWithContext(ctx, method, parsedURL.String(), bytes.NewReader(input.Body))
	if err != nil {
		return nil, fmt.Errorf("create request for %q: %w", rawURL, err)
	}
	request.Header = input.Header.Clone()
	if request.Header == nil {
		request.Header = make(http.Header)
	}
	if request.Header.Get("Accept") == "" {
		request.Header.Set("Accept", "*/*")
	}
	if request.Header.Get("User-Agent") == "" {
		request.Header.Set("User-Agent", UserAgent)
	}

	httpResponse, err := loader.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("load %q: %w", rawURL, err)
	}

	return &Response{
		URL:        httpResponse.Request.URL,
		StatusCode: httpResponse.StatusCode,
		Header:     httpResponse.Header,
		Body:       httpResponse.Body,
	}, nil
}

func (loader *Loader) Cookies(location *url.URL) []*http.Cookie {
	if loader == nil || loader.httpClient == nil || loader.httpClient.Jar == nil || location == nil {
		return nil
	}
	return loader.httpClient.Jar.Cookies(location)
}

func (loader *Loader) SetCookies(location *url.URL, cookies []*http.Cookie) {
	if loader == nil || loader.httpClient == nil || loader.httpClient.Jar == nil || location == nil {
		return
	}
	loader.httpClient.Jar.SetCookies(location, cookies)
}

func (destination Destination) accept() string {
	switch destination {
	case StyleDestination:
		return acceptStyle
	case ImageDestination:
		return acceptImage
	case FontDestination:
		return acceptFont
	case ScriptDestination:
		return acceptScript
	default:
		return acceptDocument
	}
}

// ParseHTTPURL validates a URL that can be loaded by the HTTP loader.
func ParseHTTPURL(rawURL string) (*url.URL, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("%w %q: %v", ErrInvalidURL, rawURL, err)
	}

	scheme := strings.ToLower(parsedURL.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("%w %q: scheme must be http or https", ErrInvalidURL, rawURL)
	}
	if parsedURL.Hostname() == "" {
		return nil, fmt.Errorf("%w %q: host is required", ErrInvalidURL, rawURL)
	}
	parsedURL.Scheme = scheme

	return parsedURL, nil
}
