package render_test

import (
	"strings"
	"testing"

	htmlparser "github.com/JediWattson/gossamer/internal/html"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestVerticalFlexUsesItsLogicalMainAndCrossAxes(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		mode    string
		firstX  float64
		secondX float64
		secondY float64
	}{
		{mode: "vertical-rl", firstX: 90, secondX: 80, secondY: 70},
		{mode: "vertical-lr", firstX: 0, secondX: 0, secondY: 70},
	} {
		test := test
		t.Run(test.mode, func(t *testing.T) {
			t.Parallel()
			document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
				<section id=flex style="display:flex;writing-mode:` + test.mode + `;width:120px;height:200px;flex-direction:row;align-items:flex-start">
					<i id=first style="flex:none;width:30px;height:70px"></i><i id=second style="flex:none;width:40px;height:50px"></i>
				</section>
			</body></html>`))
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
			flex, first, second := geometry("flex"), geometry("first"), geometry("second")
			assertNear(t, "vertical flex width", flex.Bounds.Width, 120)
			assertNear(t, "vertical flex height", flex.Bounds.Height, 200)
			assertNear(t, "first physical x", first.Bounds.X-flex.Bounds.X, test.firstX)
			assertNear(t, "first physical y", first.Bounds.Y-flex.Bounds.Y, 0)
			assertNear(t, "first physical width", first.Bounds.Width, 30)
			assertNear(t, "first physical height", first.Bounds.Height, 70)
			assertNear(t, "second physical x", second.Bounds.X-flex.Bounds.X, test.secondX)
			assertNear(t, "second physical y", second.Bounds.Y-flex.Bounds.Y, test.secondY)
			assertNear(t, "second physical width", second.Bounds.Width, 40)
			assertNear(t, "second physical height", second.Bounds.Height, 50)
		})
	}
}

func TestFlexRowDirectionFollowsTheLogicalInlineAxis(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name          string
		mode          string
		direction     string
		flexDirection string
		firstX        float64
		firstY        float64
	}{
		{name: "horizontal rtl", mode: "horizontal-tb", direction: "rtl", flexDirection: "row", firstX: 150},
		{name: "vertical rtl", mode: "vertical-lr", direction: "rtl", flexDirection: "row", firstY: 150},
		{name: "horizontal rtl row-reverse", mode: "horizontal-tb", direction: "rtl", flexDirection: "row-reverse"},
		{name: "vertical rtl row-reverse", mode: "vertical-lr", direction: "rtl", flexDirection: "row-reverse"},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0"><section id=flex style="display:flex;writing-mode:` + test.mode + `;direction:` + test.direction + `;flex-direction:` + test.flexDirection + `;width:200px;height:200px;align-items:flex-start"><i id=first style="flex:none;width:50px;height:50px"></i><i style="flex:none;width:50px;height:50px"></i></section></body></html>`))
			if err != nil {
				t.Fatal(err)
			}
			frame, err := render.Render(document, render.Viewport{Width: 300, Height: 300})
			if err != nil {
				t.Fatal(err)
			}
			flex, ok := frame.Layout.Geometry(findElementByID(document, "flex"))
			if !ok {
				t.Fatal("missing flex geometry")
			}
			first, ok := frame.Layout.Geometry(findElementByID(document, "first"))
			if !ok {
				t.Fatal("missing first item geometry")
			}
			assertNear(t, "row inline x", first.Bounds.X-flex.Bounds.X, test.firstX)
			assertNear(t, "row inline y", first.Bounds.Y-flex.Bounds.Y, test.firstY)
		})
	}
}

func TestHorizontalFlexKeepsIndependentAxesInsideVerticalGrid(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<section style="display:grid;writing-mode:vertical-lr;width:200px;height:200px;grid-template-columns:200px;grid-template-rows:200px;justify-content:start;align-content:start;justify-items:start;align-items:start">
			<div id=flex style="display:flex;writing-mode:horizontal-tb;width:160px;height:100px;align-items:flex-start">
				<i id=first style="flex:none;width:60px;height:30px">A</i><i id=second style="flex:none;width:100px;height:40px">B</i>
			</div>
		</section>
	</body></html>`))
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
	flex, first, second := geometry("flex"), geometry("first"), geometry("second")
	assertNear(t, "independent flex width", flex.Bounds.Width, 160)
	assertNear(t, "independent flex height", flex.Bounds.Height, 100)
	assertNear(t, "first child width", first.Bounds.Width, 60)
	assertNear(t, "first child height", first.Bounds.Height, 30)
	assertNear(t, "second child x", second.Bounds.X-first.Bounds.X, 60)
	assertNear(t, "second child y", second.Bounds.Y, first.Bounds.Y)
	assertNear(t, "second child width", second.Bounds.Width, 100)
	assertNear(t, "second child height", second.Bounds.Height, 40)
	if hit := render.HitTest(frame, second.Bounds.X+second.Bounds.Width/2, second.Bounds.Y+second.Bounds.Height/2); hit != findElementByID(document, "second") {
		t.Fatalf("horizontal flex descendant hit = %v, want second", hit)
	}
}

func TestOppositeVerticalFlexReversesItsOwnBlockAxis(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<section style="display:grid;writing-mode:vertical-rl;width:200px;height:200px;grid-template-columns:200px;grid-template-rows:200px;justify-content:start;align-content:start;justify-items:start;align-items:start">
			<div id=flex style="display:flex;writing-mode:vertical-lr;width:160px;height:100px;flex-direction:column;align-items:flex-start">
				<i id=first style="flex:none;width:60px;height:50px"></i><i id=second style="flex:none;width:100px;height:50px"></i>
			</div>
		</section>
	</body></html>`))
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
	flex, first, second := geometry("flex"), geometry("first"), geometry("second")
	assertNear(t, "opposite vertical flex width", flex.Bounds.Width, 160)
	assertNear(t, "opposite vertical flex height", flex.Bounds.Height, 100)
	assertNear(t, "first item width", first.Bounds.Width, 60)
	assertNear(t, "second item width", second.Bounds.Width, 100)
	if first.Bounds.X >= second.Bounds.X {
		t.Fatalf("vertical-lr Flex did not reverse its vertical-rl parent's block progression: first=%v second=%v", first.Bounds, second.Bounds)
	}
}

func TestVerticalFlexPhysicalEdgesAndHitGeometryTransformTogether(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<section id=flex style="display:flex;writing-mode:vertical-lr;width:100px;height:120px;align-items:flex-start;padding:3px 5px 7px 11px;border-style:solid;border-width:2px 4px 6px 8px">
			<i id=item style="flex:none;width:20px;height:30px"></i>
		</section>
	</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 300, Height: 300})
	if err != nil {
		t.Fatal(err)
	}
	flex, ok := frame.Layout.Geometry(findElementByID(document, "flex"))
	if !ok {
		t.Fatal("missing vertical Flex geometry")
	}
	itemNode := findElementByID(document, "item")
	item, ok := frame.Layout.Geometry(itemNode)
	if !ok {
		t.Fatal("missing vertical Flex item geometry")
	}
	assertNear(t, "physical border-box width", flex.Bounds.Width, 128)
	assertNear(t, "physical border-box height", flex.Bounds.Height, 138)
	assertNear(t, "physical left edge inset", item.Bounds.X-flex.Bounds.X, 19)
	assertNear(t, "physical top edge inset", item.Bounds.Y-flex.Bounds.Y, 5)
	assertNear(t, "physical item width", item.Bounds.Width, 20)
	assertNear(t, "physical item height", item.Bounds.Height, 30)
	if hit := render.HitTest(frame, item.Bounds.X+10, item.Bounds.Y+15); hit != itemNode {
		t.Fatalf("vertical Flex hit = %v, want item", hit)
	}
}
