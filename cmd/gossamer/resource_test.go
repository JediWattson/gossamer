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
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/JediWattson/gossamer/internal/loader"
)

func TestRunScreenshotLoadsRedirectRelativeStylesheetAndImage(t *testing.T) {
	t.Parallel()

	const (
		stylesheetPath = "/pages/assets/site.css"
		imagePath      = "/pages/assets/logo.png"
	)
	backgroundColor := color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff}
	imageColor := color.NRGBA{R: 0xf0, G: 0x2d, B: 0xab, A: 0xff}

	logo := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	for y := logo.Bounds().Min.Y; y < logo.Bounds().Max.Y; y++ {
		for x := logo.Bounds().Min.X; x < logo.Bounds().Max.X; x++ {
			logo.SetNRGBA(x, y, imageColor)
		}
	}
	var encodedLogo bytes.Buffer
	if err := png.Encode(&encodedLogo, logo); err != nil {
		t.Fatalf("encode logo fixture: %v", err)
	}

	var requestMutex sync.Mutex
	requests := make(map[string][]string)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestMutex.Lock()
		requests[request.URL.Path] = append(requests[request.URL.Path], request.Header.Get("Accept"))
		requestMutex.Unlock()

		switch request.URL.Path {
		case "/start":
			http.Redirect(writer, request, "/pages/index.html", http.StatusFound)
		case "/pages/index.html":
			writer.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(writer, `<!doctype html>
<html>
<head><link rel="stylesheet" href="assets/site.css"></head>
<body><img src="assets/logo.png" alt="logo"><img src="assets/logo.png" alt="logo again"></body>
</html>`)
		case stylesheetPath:
			writer.Header().Set("Content-Type", "text/css; charset=utf-8")
			_, _ = io.WriteString(writer, `body { margin: 0; background-color: #123456; }`)
		case imagePath:
			writer.Header().Set("Content-Type", "image/png")
			_, _ = writer.Write(encodedLogo.Bytes())
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
		[]string{"--screenshot", outputPath, server.URL + "/start"},
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
	requestSnapshot := make(map[string][]string, len(requests))
	for path, accepts := range requests {
		requestSnapshot[path] = append([]string(nil), accepts...)
	}
	requestMutex.Unlock()

	if got := requestSnapshot[stylesheetPath]; len(got) != 1 || !strings.HasPrefix(got[0], "text/css") {
		t.Errorf("stylesheet requests = %#v, want one %q request with text/css Accept", got, stylesheetPath)
	}
	if got := requestSnapshot[imagePath]; len(got) != 1 || !strings.HasPrefix(got[0], "image/") {
		t.Errorf("image requests = %#v, want one %q request with image Accept", got, imagePath)
	}
	if got := requestSnapshot["/assets/site.css"]; len(got) != 0 {
		t.Errorf("pre-redirect stylesheet path was requested: %#v", got)
	}
	if got := requestSnapshot["/assets/logo.png"]; len(got) != 0 {
		t.Errorf("pre-redirect image path was requested: %#v", got)
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

	bottomRight := color.NRGBAModel.Convert(screenshot.At(screenshot.Bounds().Max.X-1, screenshot.Bounds().Max.Y-1)).(color.NRGBA)
	if bottomRight != backgroundColor {
		t.Errorf("screenshot background = %#v, want %#v", bottomRight, backgroundColor)
	}
	if !imageColumnContainsNRGBA(screenshot, screenshot.Bounds().Min.X, imageColor) {
		t.Errorf("screenshot first column does not contain decoded image color %#v; external body margin:0 may not have applied", imageColor)
	}
	if !imageColumnContainsNRGBA(screenshot, screenshot.Bounds().Min.X+4, imageColor) {
		t.Errorf("screenshot column 4 does not contain decoded image color %#v; duplicate image consumer may not have painted", imageColor)
	}
}

func imageColumnContainsNRGBA(source image.Image, x int, target color.NRGBA) bool {
	for y := source.Bounds().Min.Y; y < source.Bounds().Max.Y; y++ {
		if color.NRGBAModel.Convert(source.At(x, y)).(color.NRGBA) == target {
			return true
		}
	}
	return false
}
