// Package sitecompat runs a bounded, engine-neutral production-site boot and
// returns diagnostics that remain useful even when page scripts fail.
package sitecompat

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"strings"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

type Options struct {
	EngineName          string
	DOMLimit            int
	Screenshot          io.Writer
	Viewport            render.Viewport
	ExpectedPaintSHA256 string
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
	Width           int          `json:"width"`
	Height          int          `json:"height"`
	Commands        int          `json:"commands"`
	PaintSHA256     string       `json:"paintSha256"`
	PaintedPixels   int          `json:"paintedPixels"`
	OpaquePixels    int          `json:"opaquePixels"`
	Colors          int          `json:"colors"`
	ColorsTruncated bool         `json:"colorsTruncated,omitempty"`
	PaintedBounds   *PixelBounds `json:"paintedBounds,omitempty"`
}

type PixelBounds struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
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
	FidelityError   string           `json:"fidelityError,omitempty"`
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
	page, loadErr := browserRuntime.NewPage(dom.NewDocument(), nil)
	if loadErr == nil {
		viewport := options.Viewport
		if viewport == (render.Viewport{}) {
			viewport = render.DefaultViewport
		}
		if viewport.Width <= 0 || viewport.Height <= 0 || viewport.Width > maxFidelityPixels/viewport.Height {
			loadErr = fmt.Errorf("sitecompat: viewport %dx%d exceeds the %d-pixel fidelity budget", viewport.Width, viewport.Height, maxFidelityPixels)
		} else {
			loadErr = page.SetViewport(viewport)
		}
	}
	if loadErr == nil {
		page.SetNavigationLoader(client)
		var navigation browser.NavigationID
		navigation, loadErr = page.Navigate(ctx, rawURL, client)
		if loadErr == nil {
			loadErr = page.WaitNavigation(ctx, navigation)
		}
	}
	if loadErr != nil {
		if page != nil {
			loadErr = errors.Join(loadErr, page.Close())
		}
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
		canvas, rasterErr := render.Rasterize(frame)
		if rasterErr == nil {
			measurePaint(canvas, report.Frame)
			if options.Screenshot != nil {
				rasterErr = png.Encode(options.Screenshot, canvas)
			}
		}
		if rasterErr != nil {
			err = errors.Join(err, rasterErr)
		}
		if expected := strings.ToLower(strings.TrimSpace(options.ExpectedPaintSHA256)); expected != "" {
			if _, decodeErr := hex.DecodeString(expected); decodeErr != nil || len(expected) != sha256.Size*2 {
				err = errors.Join(err, fmt.Errorf("sitecompat: invalid expected paint SHA-256 %q", options.ExpectedPaintSHA256))
			} else if report.Frame.PaintSHA256 != expected {
				report.FidelityError = fmt.Sprintf("paint SHA-256 = %s, want %s", report.Frame.PaintSHA256, expected)
			}
		}
	}
	report.Ownership = browserRuntime.Ledger().Stats()
	report.Passed = snapshot.State == browser.NavigationComplete && snapshot.ResourcesFailed == 0 && snapshot.ScriptsFailed == 0 && snapshot.Err == nil && report.Frame != nil && report.Frame.PaintSHA256 != "" && report.FidelityError == "" && err == nil

	closeErr := page.Close()
	closeErr = errors.Join(closeErr, browserRuntime.Close())
	report.Teardown = browserRuntime.Ledger().Stats()
	if report.Teardown.LiveObjects != 0 || report.Teardown.PersistentObjects != 0 {
		closeErr = errors.Join(closeErr, fmt.Errorf("sitecompat: teardown ownership = %#v", report.Teardown))
		report.Passed = false
	}
	return report, errors.Join(err, closeErr)
}

func measurePaint(canvas image.Image, report *FrameReport) {
	if canvas == nil || report == nil {
		return
	}
	bounds := canvas.Bounds()
	hash := sha256.New()
	var dimensions [8]byte
	binary.BigEndian.PutUint32(dimensions[:4], uint32(bounds.Dx()))
	binary.BigEndian.PutUint32(dimensions[4:], uint32(bounds.Dy()))
	_, _ = hash.Write(dimensions[:])
	colors := make(map[color.NRGBA]struct{})
	minimumX, minimumY := bounds.Max.X, bounds.Max.Y
	maximumX, maximumY := bounds.Min.X-1, bounds.Min.Y-1
	row := make([]byte, bounds.Dx()*4)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			value := color.NRGBAModel.Convert(canvas.At(x, y)).(color.NRGBA)
			offset := (x - bounds.Min.X) * 4
			row[offset], row[offset+1], row[offset+2], row[offset+3] = value.R, value.G, value.B, value.A
			if _, found := colors[value]; !found {
				if len(colors) < maxReportedColors {
					colors[value] = struct{}{}
				} else {
					report.ColorsTruncated = true
				}
			}
			if value.A == 0 {
				continue
			}
			report.PaintedPixels++
			if value.A == 255 {
				report.OpaquePixels++
			}
			if x < minimumX {
				minimumX = x
			}
			if y < minimumY {
				minimumY = y
			}
			if x > maximumX {
				maximumX = x
			}
			if y > maximumY {
				maximumY = y
			}
		}
		_, _ = hash.Write(row)
	}
	report.PaintSHA256 = hex.EncodeToString(hash.Sum(nil))
	report.Colors = len(colors)
	if maximumX >= minimumX && maximumY >= minimumY {
		report.PaintedBounds = &PixelBounds{X: minimumX, Y: minimumY, Width: maximumX - minimumX + 1, Height: maximumY - minimumY + 1}
	}
}

const (
	maxFidelityPixels = 16 << 20
	maxReportedColors = 1 << 16
)

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
