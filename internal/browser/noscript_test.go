package browser_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/browser/fake"
	htmlparser "github.com/JediWattson/gossamer/internal/html"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestScriptedPageDoesNotPaintNoscriptFallback(t *testing.T) {
	document, err := htmlparser.Parse(strings.NewReader(`
		<html><body><noscript>enable JavaScript</noscript><main>scripted page</main></body></html>
	`))
	if err != nil {
		t.Fatal(err)
	}
	engine := fake.New()
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	location, _ := url.Parse("https://noscript.gossamer.test/")
	page, err := browserRuntime.NewPage(document, location)
	if err != nil {
		t.Fatal(err)
	}
	defer page.Close()
	if err := page.Render(); err != nil {
		t.Fatal(err)
	}

	var painted strings.Builder
	for _, command := range page.Frame().DisplayList.Commands {
		if command.Kind == render.DrawTextCommand {
			painted.WriteString(command.Text)
		}
	}
	if strings.Contains(painted.String(), "enable JavaScript") {
		t.Fatalf("painted text = %q, want noscript fallback hidden", painted.String())
	}
	if !strings.Contains(painted.String(), "scripted page") {
		t.Fatalf("painted text = %q, want scripted content", painted.String())
	}
}
