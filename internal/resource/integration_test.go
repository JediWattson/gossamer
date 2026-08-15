package resource_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	htmlparser "github.com/JediWattson/gossamer/internal/html"
	"github.com/JediWattson/gossamer/internal/loader"
	"github.com/JediWattson/gossamer/internal/resource"
)

func TestDiscoverAndFetchDOMResources(t *testing.T) {
	t.Parallel()

	type observedRequest struct {
		path   string
		accept string
	}
	requests := make(chan observedRequest, 2)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests <- observedRequest{path: request.URL.Path, accept: request.Header.Get("Accept")}
		switch request.URL.Path {
		case "/assets/site.css":
			writer.Header().Set("Content-Type", "text/css")
			_, _ = io.WriteString(writer, "body { color: #123 }\n")
		case "/assets/logo.png":
			writer.Header().Set("Content-Type", "image/png")
			_, _ = writer.Write([]byte("not decoded yet"))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	document, err := htmlparser.Parse(strings.NewReader(`
		<base href="/assets/">
		<link rel="stylesheet" href="site.css">
		<img src="logo.png">
	`))
	if err != nil {
		t.Fatalf("html.Parse() error = %v", err)
	}
	graph, err := resource.Discover(document, mustURL(t, server.URL+"/page"))
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	pipeline := resource.NewPipeline(loader.New(server.Client()), resource.PipelineOptions{})
	results := pipeline.FetchAll(context.Background(), graph.References)
	if got := len(results); got != 2 {
		t.Fatalf("len(FetchAll()) = %d, want 2", got)
	}
	if results[0].Err != nil || string(results[0].Asset.Bytes()) != "body { color: #123 }\n" {
		t.Errorf("stylesheet result = asset %#v, error %v", results[0].Asset, results[0].Err)
	}
	if results[1].Err != nil || string(results[1].Asset.Bytes()) != "not decoded yet" {
		t.Errorf("image result = asset %#v, error %v", results[1].Asset, results[1].Err)
	}

	observed := map[string]string{}
	for range 2 {
		request := <-requests
		observed[request.path] = request.accept
	}
	if got := observed["/assets/site.css"]; !strings.HasPrefix(got, "text/css") {
		t.Errorf("stylesheet Accept = %q, want text/css preference", got)
	}
	if got := observed["/assets/logo.png"]; !strings.HasPrefix(got, "image/") {
		t.Errorf("image Accept = %q, want image preference", got)
	}
}
