package sitecompat_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/browser/fake"
	"github.com/JediWattson/gossamer/internal/loader"
	"github.com/JediWattson/gossamer/internal/render"
	"github.com/JediWattson/gossamer/internal/sitecompat"
)

func TestRunReportsScriptFailureDOMFrameAndCleanTeardown(t *testing.T) {
	engine := fake.NewWithInitializer(func(realm *fake.Realm) error {
		return realm.SetModuleEvaluator(func(browser.Host, browser.ModuleGraph) error {
			return errors.New("ReferenceError: fetch is not defined")
		})
	})
	report, err := sitecompat.Run(context.Background(), engine, "https://compat.test/", compatLoader{}, sitecompat.Options{
		EngineName: "fake", DOMLimit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Passed || report.Navigation.ScriptsFailed != 1 || len(report.Navigation.ScriptFailures) != 1 {
		t.Fatalf("compatibility report = %#v", report)
	}
	failure := report.Navigation.ScriptFailures[0]
	if failure.URL != "https://compat.test/app.js" || failure.Phase != "evaluate" || !strings.Contains(failure.Message, "fetch is not defined") {
		t.Fatalf("script failure = %#v", failure)
	}
	if report.Frame == nil || report.Frame.Commands == 0 || len(report.DOM) == 0 {
		t.Fatalf("report omitted page evidence: %#v", report)
	}
	if report.Teardown.LiveObjects != 0 || report.Teardown.PersistentObjects != 0 {
		t.Fatalf("teardown = %#v", report.Teardown)
	}
}

func TestRunEnforcesDeterministicPaintAtRequestedViewport(t *testing.T) {
	var screenshot bytes.Buffer
	options := sitecompat.Options{
		EngineName: "fake", DOMLimit: 20, Screenshot: &screenshot,
		Viewport: render.Viewport{Width: 320, Height: 180},
	}
	first, err := sitecompat.Run(context.Background(), fake.New(), "https://compat.test/", compatLoader{}, options)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Passed || first.Frame == nil || first.Frame.Width != 320 || first.Frame.Height != 180 ||
		len(first.Frame.PaintSHA256) != 64 || first.Frame.PaintedPixels == 0 || first.Frame.PaintedBounds == nil || screenshot.Len() == 0 {
		t.Fatalf("fidelity report = %#v, screenshot bytes = %d", first, screenshot.Len())
	}
	options.Screenshot = nil
	options.ExpectedPaintSHA256 = first.Frame.PaintSHA256
	second, err := sitecompat.Run(context.Background(), fake.New(), "https://compat.test/", compatLoader{}, options)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Passed || second.Frame == nil || second.Frame.PaintSHA256 != first.Frame.PaintSHA256 {
		t.Fatalf("repeat report = %#v", second)
	}
	options.ExpectedPaintSHA256 = strings.Repeat("0", 64)
	mismatch, err := sitecompat.Run(context.Background(), fake.New(), "https://compat.test/", compatLoader{}, options)
	if err != nil {
		t.Fatal(err)
	}
	if mismatch.Passed || !strings.Contains(mismatch.FidelityError, first.Frame.PaintSHA256) {
		t.Fatalf("mismatch report = %#v", mismatch)
	}
}

type compatLoader struct{}

func (compatLoader) Load(_ context.Context, rawURL string) (*loader.Response, error) {
	location, _ := url.Parse(rawURL)
	return &loader.Response{
		URL: location, StatusCode: http.StatusOK,
		Header: http.Header{"Content-Type": []string{"text/html"}},
		Body:   io.NopCloser(strings.NewReader(`<!doctype html><html><body><main id="app">boot</main><script type="module" src="/app.js"></script></body></html>`)),
	}, nil
}

func (compatLoader) LoadResource(_ context.Context, rawURL string, _ loader.Destination) (*loader.Response, error) {
	location, _ := url.Parse(rawURL)
	return &loader.Response{
		URL: location, StatusCode: http.StatusOK,
		Header: http.Header{"Content-Type": []string{"text/javascript"}},
		Body:   io.NopCloser(strings.NewReader(`globalThis.__boot = true;`)),
	}, nil
}
