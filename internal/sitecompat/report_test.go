package sitecompat_test

import (
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
