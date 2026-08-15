package render_test

import (
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestRenderShowsBodyNoscriptWithoutPaintingHeadNoscript(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	document.AppendChild(html)
	head := dom.NewElement("head")
	html.AppendChild(head)
	headFallback := dom.NewElement("noscript")
	headFallback.AppendChild(dom.NewText("hidden head fallback"))
	head.AppendChild(headFallback)
	body := dom.NewElement("body")
	html.AppendChild(body)
	bodyFallback := dom.NewElement("noscript")
	bodyFallback.AppendChild(dom.NewText("visible body fallback"))
	body.AppendChild(bodyFallback)

	frame, err := render.Render(document, render.Viewport{Width: 320, Height: 200})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	var paintedText strings.Builder
	for _, command := range frame.DisplayList.Commands {
		if command.Kind == render.DrawTextCommand {
			paintedText.WriteString(command.Text)
		}
	}
	if !strings.Contains(paintedText.String(), "visible body fallback") {
		t.Errorf("painted text = %q, want body noscript fallback", paintedText.String())
	}
	if strings.Contains(paintedText.String(), "hidden head fallback") {
		t.Errorf("painted text = %q, want head noscript fallback hidden", paintedText.String())
	}
}
