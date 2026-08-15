package main

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/loader"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestRunScreenshotIgnoresUnusableSubresources(t *testing.T) {
	t.Parallel()

	var requestMutex sync.Mutex
	requests := make(map[string]int)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestMutex.Lock()
		requests[request.URL.Path]++
		requestMutex.Unlock()

		switch request.URL.Path {
		case "/":
			writer.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(writer, `<!doctype html>
<html>
<head>
  <link rel="stylesheet" href="/missing.css">
  <link rel="stylesheet" href="/wrong-mime.css">
</head>
<body><img src="/corrupt.png" alt="corrupt image"></body>
</html>`)
		case "/missing.css":
			writer.Header().Set("Content-Type", "text/css")
			writer.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(writer, `body { background: #ff0000; }`)
		case "/wrong-mime.css":
			writer.Header().Set("Content-Type", "text/plain")
			_, _ = io.WriteString(writer, `body { background: #00ff00; }`)
		case "/corrupt.png":
			writer.Header().Set("Content-Type", "image/png")
			_, _ = io.WriteString(writer, "not a PNG")
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	outputPath := filepath.Join(t.TempDir(), "page.png")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run(
		context.Background(),
		[]string{"--screenshot", outputPath, server.URL},
		&stdout,
		&stderr,
		loader.New(server.Client()),
	)

	if exitCode != 0 {
		t.Fatalf("run() exit code = %d, want 0; stderr = %q", exitCode, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}

	requestMutex.Lock()
	requestSnapshot := make(map[string]int, len(requests))
	for path, count := range requests {
		requestSnapshot[path] = count
	}
	requestMutex.Unlock()
	for _, path := range []string{"/missing.css", "/wrong-mime.css", "/corrupt.png"} {
		if got := requestSnapshot[path]; got != 1 {
			t.Errorf("requests to %s = %d, want 1", path, got)
		}
	}

	file, err := os.Open(outputPath)
	if err != nil {
		t.Fatalf("open screenshot: %v", err)
	}
	defer file.Close()
	screenshot, err := png.Decode(file)
	if err != nil {
		t.Fatalf("decode screenshot: %v", err)
	}

	want := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	for y := screenshot.Bounds().Min.Y; y < screenshot.Bounds().Max.Y; y++ {
		for x := screenshot.Bounds().Min.X; x < screenshot.Bounds().Max.X; x++ {
			got := color.NRGBAModel.Convert(screenshot.At(x, y)).(color.NRGBA)
			if got != want {
				t.Fatalf(
					"pixel (%d,%d) = %#v, want white; unusable stylesheet or image may have applied",
					x,
					y,
					got,
				)
			}
		}
	}
}

func TestRunScreenshotCancellationPreservesExistingFile(t *testing.T) {
	t.Parallel()

	resourceStarted := make(chan struct{})
	var signalOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/":
			writer.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(writer, `<!doctype html><link rel="stylesheet" href="/slow.css"><p>Page</p>`)
		case "/slow.css":
			signalOnce.Do(func() { close(resourceStarted) })
			<-request.Context().Done()
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	directory := t.TempDir()
	outputPath := filepath.Join(directory, "page.png")
	const existingContents = "existing screenshot"
	if err := os.WriteFile(outputPath, []byte(existingContents), 0o600); err != nil {
		t.Fatalf("write existing screenshot: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	completed := make(chan int, 1)
	go func() {
		completed <- run(
			ctx,
			[]string{"--screenshot", outputPath, server.URL},
			&stdout,
			&stderr,
			loader.New(server.Client()),
		)
	}()

	select {
	case <-resourceStarted:
		cancel()
	case <-time.After(5 * time.Second):
		t.Fatal("subresource request did not start")
	}

	var exitCode int
	select {
	case exitCode = <-completed:
	case <-time.After(5 * time.Second):
		t.Fatal("run() did not return after cancellation")
	}
	if exitCode != 1 {
		t.Errorf("run() exit code = %d, want 1", exitCode)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "load page resources") || !strings.Contains(stderr.String(), "context canceled") {
		t.Errorf("stderr = %q, want canceled resource-load error", stderr.String())
	}

	contents, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read existing screenshot: %v", err)
	}
	if string(contents) != existingContents {
		t.Errorf("existing screenshot = %q, want unchanged %q", contents, existingContents)
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read screenshot directory: %v", err)
	}
	temporaryPrefix := "." + filepath.Base(outputPath) + "-"
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), temporaryPrefix) {
			t.Errorf("temporary screenshot artifact remains: %s", entry.Name())
		}
	}
}

func TestWriteScreenshotPreCanceledPreservesExistingFileWithoutTemporaryArtifact(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	outputPath := filepath.Join(directory, "page.png")
	const existingContents = "existing screenshot"
	if err := os.WriteFile(outputPath, []byte(existingContents), 0o600); err != nil {
		t.Fatalf("write existing screenshot: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var stderr bytes.Buffer
	if exitCode := writeScreenshot(ctx, &stderr, outputPath, commandTestDocument(), render.Resources{}); exitCode != 1 {
		t.Errorf("writeScreenshot() exit code = %d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), "context canceled") {
		t.Errorf("stderr = %q, want context canceled error", stderr.String())
	}

	contents, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read existing screenshot: %v", err)
	}
	if string(contents) != existingContents {
		t.Errorf("existing screenshot = %q, want unchanged %q", contents, existingContents)
	}
	assertNoScreenshotTemporaryArtifacts(t, directory, outputPath)
}

func TestWriteScreenshotReplacementPreservesExistingPermissions(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	outputPath := filepath.Join(directory, "page.png")
	if err := os.WriteFile(outputPath, []byte("existing screenshot"), 0o600); err != nil {
		t.Fatalf("write existing screenshot: %v", err)
	}
	wantMode := os.FileMode(0o640)
	if err := os.Chmod(outputPath, wantMode); err != nil {
		t.Fatalf("chmod existing screenshot: %v", err)
	}

	var stderr bytes.Buffer
	if exitCode := writeScreenshot(context.Background(), &stderr, outputPath, commandTestDocument(), render.Resources{}); exitCode != 0 {
		t.Fatalf("writeScreenshot() exit code = %d, want 0; stderr = %q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}

	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatalf("stat replacement screenshot: %v", err)
	}
	if got := info.Mode().Perm(); got != wantMode {
		t.Errorf("replacement permissions = %#o, want %#o", got, wantMode)
	}
	file, err := os.Open(outputPath)
	if err != nil {
		t.Fatalf("open replacement screenshot: %v", err)
	}
	defer file.Close()
	if _, err := png.Decode(file); err != nil {
		t.Fatalf("decode replacement screenshot: %v", err)
	}
	assertNoScreenshotTemporaryArtifacts(t, directory, outputPath)
}

func TestLoadRenderResourcesImageBudgetMapsDuplicateWithoutFetchingLaterUniqueImage(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	document.AppendChild(html)
	body := dom.NewElement("body")
	html.AppendChild(body)
	first := dom.NewElement("img", dom.Attribute{Name: "src", Value: "/shared.png"})
	duplicate := dom.NewElement("img", dom.Attribute{Name: "src", Value: "/shared.png"})
	laterUnique := dom.NewElement("img", dom.Attribute{Name: "src", Value: "/later.png"})
	body.AppendChild(first)
	body.AppendChild(duplicate)
	body.AppendChild(laterUnique)

	pixel := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	pixel.SetNRGBA(0, 0, color.NRGBA{R: 0x42, G: 0x81, B: 0xc3, A: 0xff})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, pixel); err != nil {
		t.Fatalf("encode image fixture: %v", err)
	}
	fetcher := &countingImageFetcher{
		body:  encoded.Bytes(),
		calls: make(map[string]int),
	}
	documentURL, err := url.Parse("https://example.test/pages/index.html")
	if err != nil {
		t.Fatalf("parse document URL: %v", err)
	}

	resources, err := loadRenderResourcesWithImageBudget(
		context.Background(),
		document,
		documentURL,
		fetcher,
		1,
	)
	if err != nil {
		t.Fatalf("loadRenderResourcesWithImageBudget() error = %v", err)
	}
	if got := fetcher.calls["https://example.test/shared.png"]; got != 1 {
		t.Errorf("shared image fetches = %d, want 1", got)
	}
	if got := fetcher.calls["https://example.test/later.png"]; got != 0 {
		t.Errorf("later unique image fetches = %d, want 0 after budget exhaustion", got)
	}
	if resources.Images[first] == nil {
		t.Error("first image consumer was not mapped")
	}
	if resources.Images[duplicate] == nil {
		t.Error("duplicate image consumer was not mapped")
	}
	if resources.Images[first] != resources.Images[duplicate] {
		t.Error("duplicate image consumers did not share the decoded image")
	}
	if _, ok := resources.Images[laterUnique]; ok {
		t.Error("later unique image was mapped after the pixel budget was exhausted")
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

func commandTestDocument() *dom.Node {
	document := dom.NewDocument()
	html := dom.NewElement("html")
	document.AppendChild(html)
	body := dom.NewElement("body")
	html.AppendChild(body)
	body.AppendChild(dom.NewText("Gossamer"))
	return document
}

func assertNoScreenshotTemporaryArtifacts(t *testing.T, directory, outputPath string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read screenshot directory: %v", err)
	}
	temporaryPrefix := "." + filepath.Base(outputPath) + "-"
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), temporaryPrefix) {
			t.Errorf("temporary screenshot artifact remains: %s", entry.Name())
		}
	}
}
