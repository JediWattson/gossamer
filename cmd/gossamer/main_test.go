package main

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/loader"
)

func TestRunFetchesAndPrintsDocument(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(writer, "<h1>Page not found</h1>")
	}))
	defer server.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run(
		context.Background(),
		[]string{server.URL},
		&stdout,
		&stderr,
		loader.New(server.Client()),
	)

	if exitCode != 0 {
		t.Errorf("run() exit code = %d, want 0", exitCode)
	}
	if stdout.String() != "<h1>Page not found</h1>" {
		t.Errorf("stdout = %q, want document body", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunRequiresOneURL(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		nil,
		{"https://example.com", "https://example.org"},
		{"--dump-dom"},
		{"--screenshot"},
		{"--screenshot", "page.png"},
		{"--screenshot", "", "https://example.com"},
	} {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		exitCode := run(context.Background(), args, &stdout, &stderr, stubLoader{})

		if exitCode != 2 {
			t.Errorf("run(%v) exit code = %d, want 2", args, exitCode)
		}
		if stdout.Len() != 0 {
			t.Errorf("run(%v) stdout = %q, want empty", args, stdout.String())
		}
		if stderr.String() != usage+"\n" {
			t.Errorf("run(%v) stderr = %q, want usage", args, stderr.String())
		}
	}
}

func TestRunWritesScreenshot(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, "<!doctype html><style>body{background:#eee}</style><p>Hello, Gossamer!</p>")
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

	file, err := os.Open(outputPath)
	if err != nil {
		t.Fatalf("open screenshot: %v", err)
	}
	defer file.Close()

	screenshot, err := png.Decode(file)
	if err != nil {
		t.Fatalf("decode screenshot: %v", err)
	}
	wantBounds := image.Rect(0, 0, 800, 600)
	if screenshot.Bounds() != wantBounds {
		t.Errorf("screenshot bounds = %v, want %v", screenshot.Bounds(), wantBounds)
	}
}

func TestRunParsesBeforeTruncatingScreenshot(t *testing.T) {
	t.Parallel()

	outputPath := filepath.Join(t.TempDir(), "page.png")
	const existingContents = "existing screenshot"
	if err := os.WriteFile(outputPath, []byte(existingContents), 0o600); err != nil {
		t.Fatalf("write existing screenshot: %v", err)
	}

	response := &loader.Response{Body: errorReadCloser{err: errors.New("response read failed")}}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run(
		context.Background(),
		[]string{"--screenshot", outputPath, "https://example.com"},
		&stdout,
		&stderr,
		stubLoader{response: response},
	)

	if exitCode != 1 {
		t.Errorf("run() exit code = %d, want 1", exitCode)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "parse document") || !strings.Contains(stderr.String(), "response read failed") {
		t.Errorf("stderr = %q, want parse error", stderr.String())
	}
	contents, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read existing screenshot: %v", err)
	}
	if string(contents) != existingContents {
		t.Errorf("existing screenshot = %q, want unchanged %q", contents, existingContents)
	}
}

func TestRunReportsScreenshotCreateError(t *testing.T) {
	t.Parallel()

	response := &loader.Response{Body: io.NopCloser(strings.NewReader("<p>Hello</p>"))}
	outputPath := t.TempDir()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run(
		context.Background(),
		[]string{"--screenshot", outputPath, "https://example.com"},
		&stdout,
		&stderr,
		stubLoader{response: response},
	)

	if exitCode != 1 {
		t.Errorf("run() exit code = %d, want 1", exitCode)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "create screenshot") {
		t.Errorf("stderr = %q, want create error", stderr.String())
	}
}

func TestRunDumpsParsedDOM(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, "<!doctype html><title>Gossamer</title><p>Hello &amp; welcome")
	}))
	defer server.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run(
		context.Background(),
		[]string{"--dump-dom", server.URL},
		&stdout,
		&stderr,
		loader.New(server.Client()),
	)

	if exitCode != 0 {
		t.Errorf("run() exit code = %d, want 0", exitCode)
	}
	want := "" +
		"#document\n" +
		"  #doctype \"html\"\n" +
		"  <html>\n" +
		"    <head>\n" +
		"      <title>\n" +
		"        #text \"Gossamer\"\n" +
		"    <body>\n" +
		"      <p>\n" +
		"        #text \"Hello & welcome\"\n"
	if stdout.String() != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", stdout.String(), want)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunReportsFetchError(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run(
		context.Background(),
		[]string{"https://example.com"},
		&stdout,
		&stderr,
		stubLoader{err: errors.New("network unavailable")},
	)

	if exitCode != 1 {
		t.Errorf("run() exit code = %d, want 1", exitCode)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "network unavailable") {
		t.Errorf("stderr = %q, want fetch error", stderr.String())
	}
}

func TestRunTreatsInvalidURLAsUsageError(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run(
		context.Background(),
		[]string{"example.com"},
		&stdout,
		&stderr,
		loader.New(nil),
	)

	if exitCode != 2 {
		t.Errorf("run() exit code = %d, want 2", exitCode)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "scheme must be http or https") {
		t.Errorf("stderr = %q, want invalid URL diagnostic", stderr.String())
	}
}

func TestRunReportsResponseWriteError(t *testing.T) {
	t.Parallel()

	response := &loader.Response{Body: io.NopCloser(strings.NewReader("document"))}
	var stderr bytes.Buffer
	exitCode := run(
		context.Background(),
		[]string{"https://example.com"},
		errorWriter{},
		&stderr,
		stubLoader{response: response},
	)

	if exitCode != 1 {
		t.Errorf("run() exit code = %d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), "write failed") {
		t.Errorf("stderr = %q, want write error", stderr.String())
	}
}

type stubLoader struct {
	response *loader.Response
	err      error
}

func (stub stubLoader) Load(context.Context, string) (*loader.Response, error) {
	return stub.response, stub.err
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

type errorReadCloser struct {
	err error
}

func (reader errorReadCloser) Read([]byte) (int, error) {
	return 0, reader.err
}

func (errorReadCloser) Close() error {
	return nil
}
