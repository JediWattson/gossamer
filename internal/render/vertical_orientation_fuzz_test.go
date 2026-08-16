package render

import (
	"html"
	"math"
	"strings"
	"testing"

	htmlparser "github.com/JediWattson/gossamer/internal/html"
	computed "github.com/JediWattson/gossamer/internal/style"
)

func FuzzVerticalTextOrientationStaysFiniteAndLossless(fuzz *testing.F) {
	fuzz.Add("A漢B", uint8(0))
	fuzz.Add("A\u0301👩🏽‍💻🇺🇸。", uint8(1))
	fuzz.Add("「vertical」", uint8(2))
	fuzz.Add("orthogonal漢", uint8(3))
	fuzz.Fuzz(func(t *testing.T, source string, rawOrientation uint8) {
		if len(source) > 512 {
			source = source[:512]
		}
		orientations := []struct {
			css   string
			value computed.TextOrientation
		}{{"mixed", computed.TextOrientationMixed}, {"upright", computed.TextOrientationUpright}, {"sideways", computed.TextOrientationSideways}}
		orientation := orientations[int(rawOrientation)%len(orientations)]
		display := "block"
		if int(rawOrientation)/len(orientations)%2 != 0 {
			display = "inline-block"
		}
		units := splitVerticalTextUnits(source)
		if got := strings.Join(units, ""); got != source {
			t.Fatalf("vertical units lost input: got %q, want %q", got, source)
		}
		runs := verticalTextRuns(source, orientation.value)
		var reconstructed strings.Builder
		for _, run := range runs {
			reconstructed.WriteString(run.text)
			if run.units <= 0 || run.orientation != textPaintUpright && run.orientation != textPaintSidewaysRight {
				t.Fatalf("invalid vertical run %#v", run)
			}
		}
		if reconstructed.String() != source {
			t.Fatalf("vertical runs lost input: got %q, want %q", reconstructed.String(), source)
		}

		document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0"><section style="display:` + display + `;writing-mode:vertical-rl;text-orientation:` + orientation.css + `;max-width:180px;max-height:260px;font-size:16px">` + html.EscapeString(source) + `</section></body></html>`))
		if err != nil {
			t.Fatal(err)
		}
		frame, err := Render(document, Viewport{Width: 320, Height: 320})
		if err != nil {
			t.Fatal(err)
		}
		for _, command := range frame.DisplayList.Commands {
			if command.Kind != DrawTextCommand {
				continue
			}
			bounds := command.textBounds
			if command.textOrientation == textPaintHorizontal {
				bounds = command.Rect
			}
			for _, value := range []float64{bounds.X, bounds.Y, bounds.Width, bounds.Height} {
				if math.IsNaN(value) || math.IsInf(value, 0) || math.Abs(value) > 1e9 {
					t.Fatalf("non-finite vertical text command: %#v", command)
				}
			}
		}
	})
}
