package render_test

import (
	"math"
	"strings"
	"testing"

	htmlparser "github.com/JediWattson/gossamer/internal/html"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestAspectRatioBlockGridAndReplacedSizing(t *testing.T) {
	t.Parallel()

	doc, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<div id="block" style="width:160px;aspect-ratio:16 / 9"></div>
		<div id="borderbox" style="box-sizing:border-box;width:120px;padding:10px;border:5px solid #000;aspect-ratio:1"></div>
		<div id="fromheight" style="height:50px;aspect-ratio:3 / 2"></div>
		<div style="display:flex;flex-direction:column;width:163px">
			<div id="grid" style="display:grid;aspect-ratio:1;width:100%;grid-template-columns:repeat(4, minmax(0, 1fr));grid-template-rows:repeat(4, minmax(0, 1fr));gap:1px">
				<button id="cell0" type="button" style="border:0;padding:0"></button>
			</div>
		</div>
		<canvas id="canvasoverride" width="40" height="20" style="display:block;width:100px;aspect-ratio:1"></canvas>
		<canvas id="canvasauto" width="40" height="20" style="display:block;width:100px;aspect-ratio:auto 1 / 1"></canvas>
	</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(doc, render.Viewport{Width: 400, Height: 600})
	if err != nil {
		t.Fatal(err)
	}

	boxes := map[string]*render.Box{}
	var walk func(*render.Box)
	walk = func(box *render.Box) {
		if box == nil {
			return
		}
		if box.Node != nil {
			for _, attr := range box.Node.Attributes {
				if attr.Name == "id" {
					boxes[attr.Value] = box
				}
			}
		}
		for _, child := range box.Children {
			walk(child)
		}
	}
	walk(frame.Root)

	assertNear := func(name string, got, want float64) {
		t.Helper()
		if math.Abs(got-want) > 0.5 {
			t.Fatalf("%s = %.2f, want %.2f", name, got, want)
		}
	}

	assertNear("block width", boxes["block"].Bounds.Width, 160)
	assertNear("block height", boxes["block"].Bounds.Height, 90)
	assertNear("borderbox width", boxes["borderbox"].Bounds.Width, 120)
	assertNear("borderbox height", boxes["borderbox"].Bounds.Height, 120)
	assertNear("fromheight height", boxes["fromheight"].Bounds.Height, 50)
	assertNear("fromheight width", boxes["fromheight"].Bounds.Width, 75)
	assertNear("grid width", boxes["grid"].Bounds.Width, 163)
	assertNear("grid height", boxes["grid"].Bounds.Height, 163)
	assertNear("cell0 width", boxes["cell0"].Bounds.Width, 40)
	assertNear("cell0 height", boxes["cell0"].Bounds.Height, 40)
	assertNear("canvasoverride height", boxes["canvasoverride"].Bounds.Height, 100)
	assertNear("canvasauto height", boxes["canvasauto"].Bounds.Height, 50)
}
