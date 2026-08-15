// Package loader retrieves resources for the browser engine.
package loader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// UserAgent identifies Gossamer when it requests web resources.
	UserAgent = "Gossamer/0.1"

	defaultTimeout = 30 * time.Second
	acceptDocument = "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"
)

// ErrInvalidURL marks URL input that cannot be loaded over HTTP or HTTPS.
var ErrInvalidURL = errors.New("invalid URL")

// Response is a loaded resource. The caller must close Body.
type Response struct {
	URL        *url.URL
	StatusCode int
	Header     http.Header
	Body       io.ReadCloser
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

	return &Loader{httpClient: httpClient}
}

// Load performs a document GET request. HTTP error statuses are returned as
// responses so callers can consume the document supplied by the server.
func (loader *Loader) Load(ctx context.Context, rawURL string) (*Response, error) {
	parsedURL, err := ParseHTTPURL(rawURL)
	if err != nil {
		return nil, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsedURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create request for %q: %w", rawURL, err)
	}
	request.Header.Set("Accept", acceptDocument)
	request.Header.Set("User-Agent", UserAgent)

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
