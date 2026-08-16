package render_test

import (
	"strings"
	"testing"

	htmlparser "github.com/JediWattson/gossamer/internal/html"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestVerticalBlockFlowUsesLogicalInlineAndBlockAxes(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		mode    string
		firstX  float64
		secondX float64
	}{
		{mode: "vertical-rl", firstX: 90, secondX: 50},
		{mode: "vertical-lr", firstX: 0, secondX: 30},
	} {
		test := test
		t.Run(test.mode, func(t *testing.T) {
			t.Parallel()
			document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0"><section id=flow style="writing-mode:` + test.mode + `;width:120px;height:200px"><i id=first style="display:block;width:30px;height:50px"></i><i id=second style="display:block;width:40px;height:70px"></i></section></body></html>`))
			if err != nil {
				t.Fatal(err)
			}
			frame, err := render.Render(document, render.Viewport{Width: 300, Height: 300})
			if err != nil {
				t.Fatal(err)
			}
			geometry := func(id string) render.LayoutGeometry {
				value, ok := frame.Layout.Geometry(findElementByID(document, id))
				if !ok {
					t.Fatalf("missing geometry for %q", id)
				}
				return value
			}
			flow, first, second := geometry("flow"), geometry("first"), geometry("second")
			assertNear(t, "vertical block width", flow.Bounds.Width, 120)
			assertNear(t, "vertical block height", flow.Bounds.Height, 200)
			assertNear(t, "first block x", first.Bounds.X-flow.Bounds.X, test.firstX)
			assertNear(t, "first block y", first.Bounds.Y-flow.Bounds.Y, 0)
			assertNear(t, "first block width", first.Bounds.Width, 30)
			assertNear(t, "first block height", first.Bounds.Height, 50)
			assertNear(t, "second block x", second.Bounds.X-flow.Bounds.X, test.secondX)
			assertNear(t, "second block y", second.Bounds.Y-flow.Bounds.Y, 0)
			assertNear(t, "second block width", second.Bounds.Width, 40)
			assertNear(t, "second block height", second.Bounds.Height, 70)
		})
	}
}

func TestOrthogonalBlockAutoInlineSizeShrinkFitsItsContents(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0"><section id=flow style="writing-mode:vertical-lr;width:100px"><i style="display:block;width:20px;height:40px"></i><i style="display:block;width:30px;height:70px"></i></section><section id=percent style="writing-mode:vertical-lr;width:100px;height:50%"><i style="display:block;width:20px;height:40px"></i><i style="display:block;width:30px;height:70px"></i></section></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 300, Height: 300})
	if err != nil {
		t.Fatal(err)
	}
	flow, ok := frame.Layout.Geometry(findElementByID(document, "flow"))
	if !ok {
		t.Fatal("missing orthogonal block geometry")
	}
	assertNear(t, "auto orthogonal inline size", flow.ContentBounds.Height, 70)
	assertNear(t, "specified orthogonal block size", flow.ContentBounds.Width, 100)
	percent, ok := frame.Layout.Geometry(findElementByID(document, "percent"))
	if !ok {
		t.Fatal("missing indefinite percentage orthogonal block geometry")
	}
	assertNear(t, "indefinite percentage orthogonal inline size", percent.ContentBounds.Height, 70)
}

func TestHorizontalBlockKeepsIndependentAxesInsideVerticalGrid(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0"><section style="display:grid;writing-mode:vertical-lr;width:200px;height:200px;grid-template-columns:200px;grid-template-rows:200px;justify-content:start;align-content:start;justify-items:start;align-items:start"><div id=flow style="writing-mode:horizontal-tb;width:160px;height:100px"><i id=first style="display:block;width:60px;height:30px"></i><i id=second style="display:block;width:100px;height:40px"></i></div></section></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 300, Height: 300})
	if err != nil {
		t.Fatal(err)
	}
	geometry := func(id string) render.LayoutGeometry {
		value, ok := frame.Layout.Geometry(findElementByID(document, id))
		if !ok {
			t.Fatalf("missing geometry for %q", id)
		}
		return value
	}
	flow, first, second := geometry("flow"), geometry("first"), geometry("second")
	assertNear(t, "horizontal block width", flow.Bounds.Width, 160)
	assertNear(t, "horizontal block height", flow.Bounds.Height, 100)
	assertNear(t, "first width", first.Bounds.Width, 60)
	assertNear(t, "first height", first.Bounds.Height, 30)
	assertNear(t, "second x", second.Bounds.X, first.Bounds.X)
	assertNear(t, "second y", second.Bounds.Y-first.Bounds.Y, 30)
	assertNear(t, "second width", second.Bounds.Width, 100)
	assertNear(t, "second height", second.Bounds.Height, 40)
}

func TestOppositeVerticalBlockReversesItsOwnBlockProgression(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0"><section style="display:grid;writing-mode:vertical-rl;width:200px;height:200px;grid-template-columns:200px;grid-template-rows:200px;justify-content:start;align-content:start;justify-items:start;align-items:start"><div id=flow style="writing-mode:vertical-lr;width:160px;height:100px"><i id=first style="display:block;width:60px;height:50px"></i><i id=second style="display:block;width:100px;height:50px"></i></div></section></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 300, Height: 300})
	if err != nil {
		t.Fatal(err)
	}
	geometry := func(id string) render.LayoutGeometry {
		value, ok := frame.Layout.Geometry(findElementByID(document, id))
		if !ok {
			t.Fatalf("missing geometry for %q", id)
		}
		return value
	}
	flow, first, second := geometry("flow"), geometry("first"), geometry("second")
	assertNear(t, "opposite block width", flow.Bounds.Width, 160)
	assertNear(t, "opposite block height", flow.Bounds.Height, 100)
	if first.Bounds.X >= second.Bounds.X {
		t.Fatalf("vertical-lr block did not reverse its vertical-rl parent's block progression: first=%v second=%v", first.Bounds, second.Bounds)
	}
}

func TestVerticalBlockPhysicalEdgesAndParentAxisMarginsTransformTogether(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0"><section id=flow style="writing-mode:vertical-lr;width:100px;height:120px;margin-left:auto;margin-right:auto;padding:3px 5px 7px 11px;border-style:solid;border-width:2px 4px 6px 8px"><i id=item style="display:block;width:20px;height:30px"></i></section></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 300, Height: 300})
	if err != nil {
		t.Fatal(err)
	}
	flow, ok := frame.Layout.Geometry(findElementByID(document, "flow"))
	if !ok {
		t.Fatal("missing vertical block geometry")
	}
	itemNode := findElementByID(document, "item")
	item, ok := frame.Layout.Geometry(itemNode)
	if !ok {
		t.Fatal("missing vertical block child geometry")
	}
	assertNear(t, "parent-axis auto margin", flow.Bounds.X, 86)
	assertNear(t, "physical border-box width", flow.Bounds.Width, 128)
	assertNear(t, "physical border-box height", flow.Bounds.Height, 138)
	assertNear(t, "physical left edge inset", item.Bounds.X-flow.Bounds.X, 19)
	assertNear(t, "physical top edge inset", item.Bounds.Y-flow.Bounds.Y, 5)
	assertNear(t, "physical item width", item.Bounds.Width, 20)
	assertNear(t, "physical item height", item.Bounds.Height, 30)
	if hit := render.HitTest(frame, item.Bounds.X+10, item.Bounds.Y+15); hit != itemNode {
		t.Fatalf("vertical block hit = %v, want item", hit)
	}
}

func TestOrthogonalBlockPercentagesUseMatchingPhysicalContainingDimensions(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0"><main style="width:200px;height:300px"><section id=flow style="writing-mode:vertical-rl;width:50%;height:25%"></section></main></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 400, Height: 400})
	if err != nil {
		t.Fatal(err)
	}
	flow, ok := frame.Layout.Geometry(findElementByID(document, "flow"))
	if !ok {
		t.Fatal("missing orthogonal percentage block geometry")
	}
	assertNear(t, "physical width percentage", flow.ContentBounds.Width, 100)
	assertNear(t, "physical height percentage", flow.ContentBounds.Height, 75)
}
