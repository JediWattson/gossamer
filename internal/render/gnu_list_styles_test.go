package render_test

import (
	"reflect"
	"strings"
	"testing"

	htmlparser "github.com/JediWattson/gossamer/internal/html"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestRenderGNUListStyleCascade(t *testing.T) {
	t.Parallel()

	// These are the list rules used by www.gnu.org/layout.min.css. Keeping the
	// selectors together catches both the shorthand values and their real
	// cascade: the more-specific no-bullet rule precedes the element rules.
	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html>
<html>
<head>
<style>
.inline-list li { display: inline; }
.no-bullet li { list-style: none; }
ul li { list-style: square outside; }
ul ul li, ol ul li { list-style: circle; }
ol li, #content ul li ol li { list-style: decimal outside; }
</style>
</head>
<body>
<main id="content">
	<ul>
		<li>Top-level square
			<ul><li>Nested circle</li></ul>
			<ol><li>Nested decimal</li></ol>
		</li>
	</ul>
	<ul class="no-bullet"><li>Suppressed by list style</li></ul>
	<ul class="inline-list"><li>Suppressed by inline display</li></ul>
</main>
</body>
</html>`))
	if err != nil {
		t.Fatalf("html.Parse() error = %v", err)
	}

	frame, err := render.Render(document, render.Viewport{Width: 480, Height: 360})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	var markers []string
	paintedText := make(map[string]bool)
	for _, command := range frame.DisplayList.Commands {
		if command.Kind != render.DrawTextCommand {
			continue
		}
		switch command.Text {
		case "▪", "◦", "1.":
			markers = append(markers, command.Text)
		default:
			for _, text := range []string{
				"Top-level square",
				"Nested circle",
				"Nested decimal",
				"Suppressed by list style",
				"Suppressed by inline display",
			} {
				if strings.Contains(command.Text, text) {
					paintedText[text] = true
				}
			}
		}
	}

	if want := []string{"▪", "◦", "1."}; !reflect.DeepEqual(markers, want) {
		t.Errorf("painted list markers = %q, want exactly %q", markers, want)
	}
	for _, text := range []string{
		"Top-level square",
		"Nested circle",
		"Nested decimal",
		"Suppressed by list style",
		"Suppressed by inline display",
	} {
		if !paintedText[text] {
			t.Errorf("text %q was not painted", text)
		}
	}
}
