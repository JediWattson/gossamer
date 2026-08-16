package render_test

import (
	"image/color"
	"strings"
	"testing"

	htmlparser "github.com/JediWattson/gossamer/internal/html"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestPositionedLayoutRemovesAbsoluteBoxesFromFlowAndSharesStackingOrderWithHitTesting(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<div id="container" style="display:block;position:relative;width:200px;height:160px;background:#eeeeee">
			<div id="flow" style="display:block;height:20px"></div>
			<button id="low" style="display:block;position:absolute;left:20px;top:30px;width:100px;height:60px;z-index:1;background:#ff0000">low</button>
			<button id="high" style="display:block;position:absolute;left:40px;top:40px;width:100px;height:60px;z-index:5;background:#0000ff">high</button>
			<div id="relative" style="display:block;position:relative;left:10px;top:5px;width:50px;height:20px">
				<button id="nested" style="display:block;position:absolute;right:5px;bottom:10px;width:20px;height:10px">nested</button>
			</div>
		</div>
		<div id="after" style="display:block;height:20px"></div>
		<button id="fixed" style="display:block;position:fixed;left:5px;top:6px;width:30px;height:20px">fixed</button>
	</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 300, Height: 200})
	if err != nil {
		t.Fatal(err)
	}
	geometry := func(id string) render.LayoutGeometry {
		t.Helper()
		node := findStaticPageElementByID(document, id)
		if node == nil {
			t.Fatalf("element %q is missing", id)
		}
		value, ok := frame.Layout.Geometry(node)
		if !ok {
			t.Fatalf("element %q has no layout geometry", id)
		}
		return value
	}

	if got := geometry("after").Bounds.Y; got != 160 {
		t.Fatalf("after block y = %v, want 160; absolute boxes must not affect flow", got)
	}
	if got := geometry("relative").Bounds; got.X != 10 || got.Y != 25 {
		t.Fatalf("relative bounds = %#v, want x=10 y=25", got)
	}
	if got := geometry("nested").Bounds; got.X != 35 || got.Y != 25 {
		t.Fatalf("nested absolute bounds = %#v, want x=35 y=25 from relative containing block", got)
	}
	if got := geometry("fixed").Bounds; got.X != 5 || got.Y != 6 {
		t.Fatalf("fixed bounds = %#v, want viewport x=5 y=6", got)
	}

	low := findStaticPageElementByID(document, "low")
	high := findStaticPageElementByID(document, "high")
	red := color.NRGBA{R: 0xff, A: 0xff}
	blue := color.NRGBA{B: 0xff, A: 0xff}
	lowPaint := commandIndex(frame.DisplayList.Commands, func(command render.Command) bool {
		return command.Node == low && command.Kind == render.FillRectCommand && command.Color == red
	})
	highPaint := commandIndex(frame.DisplayList.Commands, func(command render.Command) bool {
		return command.Node == high && command.Kind == render.FillRectCommand && command.Color == blue
	})
	if lowPaint < 0 || highPaint < 0 || lowPaint >= highPaint {
		t.Fatalf("positioned paint order low=%d high=%d, want low before high", lowPaint, highPaint)
	}
	hit := render.HitTest(frame, 50, 50)
	for hit != nil && hit.Parent != nil && hit != high {
		hit = hit.Parent
	}
	if hit != high {
		t.Fatalf("overlap hit = %#v, want higher z-index node %#v", hit, high)
	}
}
