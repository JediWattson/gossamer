package render

import (
	"strings"
	"testing"

	htmlparser "github.com/JediWattson/gossamer/internal/html"
)

func TestBlockWritingModeBoundariesComposeTextPaintOrientation(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0"><section style="writing-mode:vertical-rl;width:200px;height:200px"><i style="display:block;width:30px;height:40px">V</i><div style="writing-mode:horizontal-tb;width:100px;height:50px"><i style="display:block;width:50px;height:30px">H</i></div></section></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := Render(document, Viewport{Width: 300, Height: 300})
	if err != nil {
		t.Fatal(err)
	}
	orientations := make(map[string]textPaintOrientation)
	for _, command := range frame.DisplayList.Commands {
		if command.Kind == DrawTextCommand && (command.Text == "V" || command.Text == "H") {
			orientations[command.Text] = command.textOrientation
		}
	}
	if got := orientations["V"]; got != textPaintSidewaysRight {
		t.Fatalf("vertical block text orientation = %d, want sideways", got)
	}
	if got := orientations["H"]; got != textPaintHorizontal {
		t.Fatalf("orthogonal horizontal block text orientation = %d, want horizontal", got)
	}
}
