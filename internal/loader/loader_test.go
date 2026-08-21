package loader

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoadDocument(t *testing.T) {
	t.Parallel()

	var gotMethod string
	var gotPath string
	var gotAccept string
	var gotUserAgent string

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		gotMethod = request.Method
		gotPath = request.URL.RequestURI()
		gotAccept = request.Header.Get("Accept")
		gotUserAgent = request.Header.Get("User-Agent")
		writer.Header().Set("Content-Type", "text/html")
		writer.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(writer, "<h1>Hello, Gossamer</h1>")
	}))
	defer server.Close()

	response, err := New(server.Client()).Load(context.Background(), server.URL+"/page?q=go")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}

	if response.StatusCode != http.StatusCreated {
		t.Errorf("status = %d, want %d", response.StatusCode, http.StatusCreated)
	}
	if response.Header.Get("Content-Type") != "text/html" {
		t.Errorf("Content-Type = %q, want text/html", response.Header.Get("Content-Type"))
	}
	if string(body) != "<h1>Hello, Gossamer</h1>" {
		t.Errorf("body = %q, want document body", body)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotPath != "/page?q=go" {
		t.Errorf("path = %q, want /page?q=go", gotPath)
	}
	if gotAccept != acceptDocument {
		t.Errorf("Accept = %q, want %q", gotAccept, acceptDocument)
	}
	if gotUserAgent != UserAgent {
		t.Errorf("User-Agent = %q, want %q", gotUserAgent, UserAgent)
	}
}

func TestDoSharesCookiesAcrossRequests(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/login":
			http.SetCookie(writer, &http.Cookie{Name: "session", Value: "strand", Path: "/"})
			writer.WriteHeader(http.StatusNoContent)
		case "/me":
			cookie, err := request.Cookie("session")
			if err != nil {
				http.Error(writer, "missing session", http.StatusUnauthorized)
				return
			}
			_, _ = io.WriteString(writer, cookie.Value)
		}
	}))
	defer server.Close()

	client := server.Client()
	loader := New(client)
	login, err := loader.Do(context.Background(), Request{URL: server.URL + "/login", Method: http.MethodPost})
	if err != nil {
		t.Fatalf("login Do() error = %v", err)
	}
	login.Body.Close()
	me, err := loader.Do(context.Background(), Request{URL: server.URL + "/me"})
	if err != nil {
		t.Fatalf("me Do() error = %v", err)
	}
	defer me.Body.Close()
	body, err := io.ReadAll(me.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(body) != "strand" {
		t.Fatalf("session body = %q, want strand", body)
	}
	if client.Jar != nil {
		t.Fatal("New mutated the caller's HTTP client")
	}
}

func TestDoPreservesMethodHeadersAndBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		writer.Header().Set("X-Echo", request.Header.Get("X-Test"))
		writer.WriteHeader(http.StatusAccepted)
		_, _ = writer.Write([]byte(request.Method + ":" + string(body)))
	}))
	defer server.Close()

	response, err := New(server.Client()).Do(context.Background(), Request{
		URL: server.URL, Method: "post", Header: http.Header{"X-Test": {"yes"}}, Body: []byte("payload"),
	})
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusAccepted || response.Header.Get("X-Echo") != "yes" || string(body) != "POST:payload" {
		t.Fatalf("response = status %d, header %q, body %q", response.StatusCode, response.Header.Get("X-Echo"), body)
	}
}

func TestLoadReturnsHTTPErrorDocument(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(writer, "not found")
	}))
	defer server.Close()

	response, err := New(server.Client()).Load(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}

	if response.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", response.StatusCode, http.StatusNotFound)
	}
	if string(body) != "not found" {
		t.Errorf("body = %q, want error document", body)
	}
}

func TestLoadResourceNegotiatesForDestination(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		destination Destination
		wantAccept  string
	}{
		{name: "document", destination: DocumentDestination, wantAccept: acceptDocument},
		{name: "style", destination: StyleDestination, wantAccept: acceptStyle},
		{name: "image", destination: ImageDestination, wantAccept: acceptImage},
		{name: "font", destination: FontDestination, wantAccept: acceptFont},
		{name: "script", destination: ScriptDestination, wantAccept: acceptScript},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			accept := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				accept <- request.Header.Get("Accept")
				writer.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()

			response, err := New(server.Client()).LoadResource(context.Background(), server.URL, test.destination)
			if err != nil {
				t.Fatalf("LoadResource() error = %v", err)
			}
			response.Body.Close()
			if got := <-accept; got != test.wantAccept {
				t.Errorf("Accept = %q, want %q", got, test.wantAccept)
			}
		})
	}
}

func TestLoadFollowsRedirect(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/start", func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, "/final", http.StatusFound)
	})
	mux.HandleFunc("/final", func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, "redirected")
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	response, err := New(server.Client()).Load(context.Background(), server.URL+"/start")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	defer response.Body.Close()

	if response.URL.String() != server.URL+"/final" {
		t.Errorf("final URL = %q, want %q", response.URL, server.URL+"/final")
	}
}

func TestLoadHonorsCancelledContext(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	response, err := New(server.Client()).Load(ctx, server.URL)
	if response != nil {
		response.Body.Close()
		t.Fatalf("Load() response = %v, want nil", response)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Load() error = %v, want context.Canceled", err)
	}
}

func TestParseHTTPURL(t *testing.T) {
	t.Parallel()

	validURLs := map[string]string{
		"http":             "http://example.com/page",
		"https":            "https://example.com",
		"case insensitive": "HTTP://example.com",
	}
	for name, rawURL := range validURLs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			parsedURL, err := ParseHTTPURL(rawURL)
			if err != nil {
				t.Fatalf("ParseHTTPURL(%q) error = %v", rawURL, err)
			}
			if parsedURL.Host == "" {
				t.Errorf("ParseHTTPURL(%q) host is empty", rawURL)
			}
		})
	}
}

func TestParseHTTPURLRejectsInvalidURLs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		url     string
		message string
	}{
		{name: "empty", url: "", message: "scheme must be http or https"},
		{name: "missing scheme", url: "example.com", message: "scheme must be http or https"},
		{name: "unsupported scheme", url: "file:///tmp/page.html", message: "scheme must be http or https"},
		{name: "missing host", url: "https:///page", message: "host is required"},
		{name: "missing hostname", url: "http://:8080", message: "host is required"},
		{name: "malformed port", url: "http://example.com:bad", message: "invalid port"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			parsedURL, err := ParseHTTPURL(test.url)
			if parsedURL != nil {
				t.Fatalf("ParseHTTPURL() URL = %v, want nil", parsedURL)
			}
			if !errors.Is(err, ErrInvalidURL) {
				t.Fatalf("ParseHTTPURL() error = %v, want ErrInvalidURL", err)
			}
			if !strings.Contains(err.Error(), test.message) {
				t.Errorf("ParseHTTPURL() error = %q, want it to contain %q", err, test.message)
			}
		})
	}
}
