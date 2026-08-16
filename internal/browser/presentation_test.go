package browser_test

import (
	"context"
	"io"
	"net/url"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/loader"
)

func TestPageMetadataTracksLiveTitleAndResolvedFavicon(t *testing.T) {
	t.Parallel()

	browserRuntime, err := browser.New()
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	document, err := parseDocument(`<!doctype html><html><head>
		<base href="/assets/">
		<title>  Gossamer   Memory
		Architecture </title>
		<link rel="icon" href="first.png">
		<link rel="shortcut icon" href="final.png">
	</head><body></body></html>`)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://metadata.gossamer.test/app/index.html")
	page, err := browserRuntime.NewPage(document, location)
	if err != nil {
		t.Fatal(err)
	}

	metadata := page.Metadata()
	if metadata.Title != "Gossamer Memory Architecture" {
		t.Fatalf("metadata title = %q", metadata.Title)
	}
	if metadata.FaviconURL == nil || metadata.FaviconURL.String() != "https://metadata.gossamer.test/assets/final.png" {
		t.Fatalf("favicon URL = %v", metadata.FaviconURL)
	}

	title := findElement(document, "title")
	titleID, _ := page.Document().ID(title)
	if err := page.Document().SetTextContent(titleID, "Live tab title"); err != nil {
		t.Fatal(err)
	}
	if got := page.Metadata().Title; got != "Live tab title" {
		t.Fatalf("live metadata title = %q", got)
	}
	if err := page.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestOptionalFaviconDoesNotRequireSubresourceFetcher(t *testing.T) {
	t.Parallel()

	browserRuntime, err := browser.New()
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	page, err := browserRuntime.LoadPage(context.Background(), "https://metadata.gossamer.test/", stubDocumentLoader{response: &loader.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(`<html><head><link rel="icon" href="favicon.png"></head><body>loaded</body></html>`)),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if metadata := page.Metadata(); metadata.FaviconURL == nil || metadata.FaviconURL.String() != "https://metadata.gossamer.test/favicon.png" {
		t.Fatalf("favicon metadata = %#v", metadata)
	}
}
