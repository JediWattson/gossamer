// Package sitecompat runs a bounded, engine-neutral production-site boot and
// returns diagnostics that remain useful even when page scripts fail.
package sitecompat

import (
	"context"
	"errors"
	"fmt"
	"image/png"
	"io"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/render"
	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

type Options struct {
	EngineName string
	DOMLimit   int
	Screenshot io.Writer
}

type NavigationReport struct {
	RequestedURL    string                  `json:"requestedUrl,omitempty"`
	URL             string                  `json:"url,omitempty"`
	State           browser.NavigationState `json:"state"`
	Document        uint64                  `json:"documentGeneration"`
	ResourcesTotal  int                     `json:"resourcesTotal"`
	ResourcesFailed int                     `json:"resourcesFailed"`
	ScriptsTotal    int                     `json:"scriptsTotal"`
	ScriptsFailed   int                     `json:"scriptsFailed"`
	ScriptFailures  []browser.ScriptFailure `json:"scriptFailures,omitempty"`
	Error           string                  `json:"error,omitempty"`
}

type FrameReport struct {
	Width    int `json:"width"`
	Height   int `json:"height"`
	Commands int `json:"commands"`
}

type Report struct {
	Engine          string           `json:"engine"`
	Passed          bool             `json:"passed"`
	Navigation      NavigationReport `json:"navigation"`
	DOM             []string         `json:"dom,omitempty"`
	Frame           *FrameReport     `json:"frame,omitempty"`
	Ownership       ownership.Stats  `json:"ownership"`
	Teardown        ownership.Stats  `json:"teardown"`
	DiagnosticError string           `json:"diagnosticError,omitempty"`
}

func Run(ctx context.Context, engine browser.Engine, rawURL string, client browser.DocumentLoader, options Options) (Report, error) {
	report := Report{Engine: options.EngineName}
	if ctx == nil {
		return report, fmt.Errorf("sitecompat: nil context")
	}
	if engine == nil {
		return report, fmt.Errorf("sitecompat: nil engine")
	}
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		return report, errors.Join(err, engine.Close())
	}
	page, loadErr := browserRuntime.LoadPage(ctx, rawURL, client)
	if loadErr != nil {
		closeErr := browserRuntime.Close()
		return report, errors.Join(loadErr, closeErr)
	}

	snapshot := page.Navigation()
	report.Navigation = navigationReport(snapshot)
	domLimit := options.DOMLimit
	if domLimit == 0 {
		domLimit = 200
	}
	if domLimit > 0 {
		report.DOM, err = page.InspectorDOMLines(domLimit)
		if err != nil {
			report.DiagnosticError = err.Error()
		}
	}
	if frame := page.Frame(); frame != nil {
		report.Frame = &FrameReport{
			Width: frame.Viewport.Width, Height: frame.Viewport.Height, Commands: len(frame.DisplayList.Commands),
		}
		if options.Screenshot != nil {
			canvas, rasterErr := render.Rasterize(frame)
			if rasterErr == nil {
				rasterErr = png.Encode(options.Screenshot, canvas)
			}
			if rasterErr != nil {
				err = errors.Join(err, rasterErr)
			}
		}
	}
	report.Ownership = browserRuntime.Ledger().Stats()
	report.Passed = snapshot.State == browser.NavigationComplete && snapshot.ResourcesFailed == 0 && snapshot.ScriptsFailed == 0 && snapshot.Err == nil && err == nil

	closeErr := page.Close()
	closeErr = errors.Join(closeErr, browserRuntime.Close())
	report.Teardown = browserRuntime.Ledger().Stats()
	if report.Teardown.LiveObjects != 0 || report.Teardown.PersistentObjects != 0 {
		closeErr = errors.Join(closeErr, fmt.Errorf("sitecompat: teardown ownership = %#v", report.Teardown))
		report.Passed = false
	}
	return report, errors.Join(err, closeErr)
}

func navigationReport(snapshot browser.NavigationSnapshot) NavigationReport {
	report := NavigationReport{
		State: snapshot.State, Document: uint64(snapshot.DocumentGeneration),
		ResourcesTotal: snapshot.ResourcesTotal, ResourcesFailed: snapshot.ResourcesFailed,
		ScriptsTotal: snapshot.ScriptsTotal, ScriptsFailed: snapshot.ScriptsFailed,
		ScriptFailures: append([]browser.ScriptFailure(nil), snapshot.ScriptFailures...),
	}
	if snapshot.RequestedURL != nil {
		report.RequestedURL = snapshot.RequestedURL.String()
	}
	if snapshot.URL != nil {
		report.URL = snapshot.URL.String()
	}
	if snapshot.Err != nil {
		report.Error = snapshot.Err.Error()
	}
	return report
}
