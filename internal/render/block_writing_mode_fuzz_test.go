package render_test

import (
	"fmt"
	"math"
	"strings"
	"testing"

	htmlparser "github.com/JediWattson/gossamer/internal/html"
	"github.com/JediWattson/gossamer/internal/render"
)

func FuzzBlockWritingModeLayoutStaysFinite(fuzz *testing.F) {
	fuzz.Add(uint8(0), uint8(0), uint16(200), uint16(120))
	fuzz.Add(uint8(1), uint8(1), uint16(1), uint16(4096))
	fuzz.Add(uint8(2), uint8(0), uint16(65535), uint16(0))
	fuzz.Fuzz(func(t *testing.T, rawMode, rawDirection uint8, rawWidth, rawHeight uint16) {
		modes := []string{"horizontal-tb", "vertical-rl", "vertical-lr"}
		directions := []string{"ltr", "rtl"}
		mode := modes[int(rawMode)%len(modes)]
		nestedMode := modes[(int(rawMode)+1)%len(modes)]
		width := 1 + int(rawWidth)%512
		height := 1 + int(rawHeight)%512
		source := fmt.Sprintf(`<!doctype html><html><body style="margin:0"><section style="writing-mode:%s;direction:%s;width:%dpx;height:%dpx;padding:3px;border:2px solid #123456"><i style="display:block;width:30px;height:40px">A</i><div style="writing-mode:%s;direction:%s;width:80px;height:60px"><b style="display:block;width:20px;height:10px">B</b><b style="display:block;width:30px;height:20px">C</b></div></section></body></html>`, mode, directions[int(rawDirection)%2], width, height, nestedMode, directions[(int(rawDirection)+1)%2])
		document, err := htmlparser.Parse(strings.NewReader(source))
		if err != nil {
			t.Fatal(err)
		}
		frame, err := render.Render(document, render.Viewport{Width: 640, Height: 480})
		if err != nil {
			t.Fatal(err)
		}
		var walk func(*render.Box)
		walk = func(box *render.Box) {
			if box == nil {
				return
			}
			for _, value := range []float64{box.Bounds.X, box.Bounds.Y, box.Bounds.Width, box.Bounds.Height, box.ContentBounds.X, box.ContentBounds.Y, box.ContentBounds.Width, box.ContentBounds.Height} {
				if math.IsNaN(value) || math.IsInf(value, 0) || math.Abs(value) > 1e9 {
					t.Fatalf("non-finite block writing-mode geometry: %#v", box)
				}
			}
			for _, child := range box.Children {
				walk(child)
			}
		}
		walk(frame.Root)
	})
}
