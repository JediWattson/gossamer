package render_test

import (
	"strings"
	"testing"

	htmlparser "github.com/JediWattson/gossamer/internal/html"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestFlexLayoutDistributesMainAxisAndSharesVisualOrderWithHitTesting(t *testing.T) {
	t.Parallel()
	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<section id="row" style="display:flex;width:300px;height:100px;column-gap:10px;align-items:center">
			<button id="a" style="display:block;width:50px;height:20px;order:2">a</button>
			<button id="b" style="display:block;flex:1 1 40px;height:40px;order:1">b</button>
			<button id="c" style="display:block;flex:2 1 40px;height:30px;order:3">c</button>
		</section>
		<section id="column" style="display:flex;flex-direction:column;width:120px;height:200px;row-gap:10px;justify-content:space-between;align-items:flex-end">
			<div id="d" style="display:block;width:30px;height:40px"></div>
			<div id="e" style="display:block;width:50px;height:60px"></div>
		</section>
	</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 400, Height: 400})
	if err != nil {
		t.Fatal(err)
	}
	geometry := func(id string) render.Rect {
		t.Helper()
		node := findStaticPageElementByID(document, id)
		value, ok := frame.Layout.Geometry(node)
		if !ok {
			t.Fatalf("%s has no layout geometry", id)
		}
		return value.Bounds
	}
	for id, want := range map[string]render.Rect{
		"b": {X: 0, Y: 30, Width: 90, Height: 40},
		"a": {X: 100, Y: 40, Width: 50, Height: 20},
		"c": {X: 160, Y: 35, Width: 140, Height: 30},
		"d": {X: 90, Y: 100, Width: 30, Height: 40},
		"e": {X: 70, Y: 240, Width: 50, Height: 60},
	} {
		if got := geometry(id); got != want {
			t.Errorf("%s bounds = %#v, want %#v", id, got, want)
		}
	}

	c := findStaticPageElementByID(document, "c")
	hit := render.HitTest(frame, 200, 50)
	for hit != nil && hit.Parent != nil && hit != c {
		hit = hit.Parent
	}
	if hit != c {
		t.Fatalf("flex hit = %#v, want item c %#v", hit, c)
	}
}

func TestInlineFlexUsesAtomicInlineOuterDisplay(t *testing.T) {
	t.Parallel()
	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<div style="width:200px"><span id="flex" style="display:inline-flex;column-gap:10px;background:#123456"><span id="a" style="display:block;width:30px;height:20px"></span><span id="b" style="display:block;width:40px;height:20px"></span></span><span id="after">after</span></div>
	</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 240, Height: 100})
	if err != nil {
		t.Fatal(err)
	}
	flex := findStaticPageElementByID(document, "flex")
	a := findStaticPageElementByID(document, "a")
	b := findStaticPageElementByID(document, "b")
	flexGeometry, ok := frame.Layout.Geometry(flex)
	if !ok {
		t.Fatal("inline-flex has no atomic principal geometry")
	}
	assertNear(t, "inline-flex shrink-to-fit width", flexGeometry.ContentBounds.Width, 80)
	aGeometry, _ := frame.Layout.Geometry(a)
	bGeometry, _ := frame.Layout.Geometry(b)
	assertNear(t, "inline-flex first item x", aGeometry.Bounds.X, flexGeometry.ContentBounds.X)
	assertNear(t, "inline-flex second item x", bGeometry.Bounds.X, aGeometry.Bounds.X+aGeometry.Bounds.Width+10)
	after := findTextFragment(collectTextFragments(frame.Root), "after")
	if after == nil || after.X < flexGeometry.Bounds.X+flexGeometry.Bounds.Width {
		t.Fatalf("following inline text = %#v, want after atomic inline-flex box %#v", after, flexGeometry.Bounds)
	}
}
