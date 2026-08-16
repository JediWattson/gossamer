package render_test

import (
	"image/color"
	"math"
	"reflect"
	"strings"
	"testing"

	htmlparser "github.com/JediWattson/gossamer/internal/html"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestGridLayoutSizesFixedFractionalAndSpanningTracks(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<section id=grid style="display:grid;width:300px;grid-template-columns:50px 1fr 2fr;grid-template-rows:40px 60px;column-gap:10px;row-gap:5px">
			<div id=a style="grid-column:1 / 3;grid-row:1;height:20px;background:#ff0000"></div>
			<div id=b style="grid-column:3;grid-row:1 / 3;background:#00ff00"></div>
		</section>
	</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 400, Height: 300})
	if err != nil {
		t.Fatal(err)
	}
	geometry := func(id string) render.LayoutGeometry {
		t.Helper()
		node := findStaticPageElementByID(document, id)
		value, ok := frame.Layout.Geometry(node)
		if !ok {
			t.Fatalf("%s has no grid geometry", id)
		}
		return value
	}
	grid := geometry("grid")
	a := geometry("a")
	b := geometry("b")
	assertNear(t, "grid content width", grid.ContentBounds.Width, 300)
	assertNear(t, "spanning item width", a.Bounds.Width, 136.6666666667)
	assertNear(t, "third fractional track x", b.Bounds.X, 146.6666666667)
	assertNear(t, "third fractional track width", b.Bounds.Width, 153.3333333333)
	assertNear(t, "two-row span height", b.Bounds.Height, 105)
	columns := grid.GridColumnSizes()
	rows := grid.GridRowSizes()
	if len(columns) != 3 || len(rows) != 2 {
		t.Fatalf("retained grid tracks = columns:%v rows:%v", columns, rows)
	}
	assertNear(t, "retained fractional track", columns[1], 76.6666666667)
	columns[0] = 999
	again, _ := frame.Layout.Geometry(findStaticPageElementByID(document, "grid"))
	if got := again.GridColumnSizes()[0]; got != 50 {
		t.Fatalf("mutating returned grid tracks changed snapshot: %v", got)
	}
	if hit := render.HitTest(frame, b.Bounds.X+10, b.Bounds.Y+10); hit != findStaticPageElementByID(document, "b") {
		t.Fatalf("grid hit = %#v, want item b", hit)
	}
}

func TestGridNamedLinesPlaceItemsAndSurviveUsedGeometry(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<section id=grid style="display:grid;width:180px;grid-template-columns:[first content-start x] 40px [middle x] 60px [x content-end] 80px [last];grid-auto-rows:20px">
			<div id=area style="grid-column:content"></div>
			<div id=span style="grid-column:x / span 2 x"></div>
			<div id=occurrence style="grid-column:2 x / last"></div>
		</section>
	</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 240, Height: 200})
	if err != nil {
		t.Fatal(err)
	}
	geometry := func(id string) render.LayoutGeometry {
		t.Helper()
		value, ok := frame.Layout.Geometry(findStaticPageElementByID(document, id))
		if !ok {
			t.Fatalf("%s has no geometry", id)
		}
		return value
	}
	grid := geometry("grid")
	area := geometry("area")
	span := geometry("span")
	occurrence := geometry("occurrence")
	assertNear(t, "area-name start", area.Bounds.X, grid.ContentBounds.X)
	assertNear(t, "area-name width", area.Bounds.Width, 100)
	assertNear(t, "named span width", span.Bounds.Width, 100)
	assertNear(t, "second occurrence x", occurrence.Bounds.X, grid.ContentBounds.X+40)
	assertNear(t, "second occurrence to last", occurrence.Bounds.Width, 140)

	wantNames := [][]string{
		{"first", "content-start", "x"},
		{"middle", "x"},
		{"x", "content-end"},
		{"last"},
	}
	names := grid.GridColumnLineNames()
	if !reflect.DeepEqual(names, wantNames) {
		t.Fatalf("retained line names = %v, want %v", names, wantNames)
	}
	names[0][0] = "mutated"
	again := geometry("grid").GridColumnLineNames()
	if again[0][0] != "first" {
		t.Fatalf("mutating returned names changed layout snapshot: %v", again)
	}
}

func TestGridTemplateAreasCreateExplicitTracksAndPlaceNamedItems(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<section id=grid style='display:grid;width:200px;grid-template-areas:"head head" "nav main" "foot .";grid-auto-columns:80px 120px;grid-auto-rows:20px 40px 30px'>
			<header id=head style="grid-area:head"></header>
			<nav id=nav style="grid-area:nav"></nav>
			<main id=main style="grid-area:main"></main>
			<footer id=foot style="grid-area:foot"></footer>
		</section>
	</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 240, Height: 200})
	if err != nil {
		t.Fatal(err)
	}
	geometry := func(id string) render.LayoutGeometry {
		t.Helper()
		value, ok := frame.Layout.Geometry(findStaticPageElementByID(document, id))
		if !ok {
			t.Fatalf("%s has no geometry", id)
		}
		return value
	}
	grid := geometry("grid")
	head := geometry("head")
	navigation := geometry("nav")
	main := geometry("main")
	foot := geometry("foot")
	assertNear(t, "area-created grid width", grid.ContentBounds.Width, 200)
	assertNear(t, "head area width", head.Bounds.Width, 200)
	assertNear(t, "head area height", head.Bounds.Height, 20)
	assertNear(t, "nav column", navigation.Bounds.X, grid.ContentBounds.X)
	assertNear(t, "nav row", navigation.Bounds.Y, grid.ContentBounds.Y+20)
	assertNear(t, "nav width", navigation.Bounds.Width, 80)
	assertNear(t, "main column", main.Bounds.X, grid.ContentBounds.X+80)
	assertNear(t, "main width", main.Bounds.Width, 120)
	assertNear(t, "foot row", foot.Bounds.Y, grid.ContentBounds.Y+60)
	assertNear(t, "foot width", foot.Bounds.Width, 80)
	if got := grid.GridColumnLineNames(); len(got) != 3 || len(got[0]) != 0 || len(got[1]) != 0 || len(got[2]) != 0 {
		t.Fatalf("implicit area line names leaked into CSSOM geometry: %v", got)
	}
}

func TestGridAutoFillCountsDefiniteTracksWithGapsAndNames(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<section id=grid style="display:grid;width:330px;column-gap:10px;grid-template-columns:[outer] 20px [before] repeat(auto-fill,[cell] minmax(80px,1fr) [edge]) [after] 30px [last]"><div id=third style="grid-column:cell 3 / edge 3"></div></section>
	</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 400, Height: 200})
	if err != nil {
		t.Fatal(err)
	}
	grid, ok := frame.Layout.Geometry(findStaticPageElementByID(document, "grid"))
	if !ok {
		t.Fatal("grid has no geometry")
	}
	if got, want := grid.GridColumnSizes(), []float64{20, 80, 80, 80, 30}; !nearSlice(got, want) {
		t.Fatalf("auto-fill columns = %v, want %v", got, want)
	}
	wantNames := [][]string{
		{"outer"}, {"before", "cell"}, {"edge", "cell"},
		{"edge", "cell"}, {"edge", "after"}, {"last"},
	}
	if got := grid.GridColumnLineNames(); !reflect.DeepEqual(got, wantNames) {
		t.Fatalf("auto-fill line names = %v, want %v", got, wantNames)
	}
	third, ok := frame.Layout.Geometry(findStaticPageElementByID(document, "third"))
	if !ok {
		t.Fatal("named auto-repeat item has no geometry")
	}
	assertNear(t, "third named auto-repeat x", third.Bounds.X-grid.ContentBounds.X, 210)
	assertNear(t, "third named auto-repeat width", third.Bounds.Width, 80)
}

func TestGridAutoFitCollapsesEmptyTracksAndAdjacentGaps(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<section id=fit style="display:grid;width:430px;column-gap:10px;grid-template-columns:repeat(auto-fit,100px)"><div id=first></div><div id=second></div></section>
		<section id=fill style="display:grid;width:430px;column-gap:10px;grid-template-columns:repeat(auto-fill,100px)"><div></div><div></div></section>
	</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 500, Height: 200})
	if err != nil {
		t.Fatal(err)
	}
	geometry := func(id string) render.LayoutGeometry {
		t.Helper()
		value, ok := frame.Layout.Geometry(findStaticPageElementByID(document, id))
		if !ok {
			t.Fatalf("%s has no geometry", id)
		}
		return value
	}
	if got, want := geometry("fit").GridColumnSizes(), []float64{100, 100, 0, 0}; !nearSlice(got, want) {
		t.Fatalf("auto-fit columns = %v, want %v", got, want)
	}
	if got, want := geometry("fill").GridColumnSizes(), []float64{100, 100, 100, 100}; !nearSlice(got, want) {
		t.Fatalf("auto-fill columns = %v, want %v", got, want)
	}
	first, second := geometry("first"), geometry("second")
	assertNear(t, "first auto-fit width", first.Bounds.Width, 100)
	assertNear(t, "second auto-fit x", second.Bounds.X-first.Bounds.X, 110)
	assertNear(t, "second auto-fit width", second.Bounds.Width, 100)
}

func TestGridAutoFitRowsUseDefiniteHeight(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<section id=grid style="display:grid;width:100px;height:230px;row-gap:10px;grid-template-columns:100px;grid-template-rows:repeat(auto-fit,50px);grid-auto-flow:column"><div id=first></div><div id=second></div></section>
	</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 300, Height: 300})
	if err != nil {
		t.Fatal(err)
	}
	geometry := func(id string) render.LayoutGeometry {
		t.Helper()
		value, ok := frame.Layout.Geometry(findStaticPageElementByID(document, id))
		if !ok {
			t.Fatalf("%s has no geometry", id)
		}
		return value
	}
	if got, want := geometry("grid").GridRowSizes(), []float64{50, 50, 0, 0}; !nearSlice(got, want) {
		t.Fatalf("auto-fit rows = %v, want %v", got, want)
	}
	first, second := geometry("first"), geometry("second")
	assertNear(t, "first auto-fit row height", first.Bounds.Height, 50)
	assertNear(t, "second auto-fit row y", second.Bounds.Y-first.Bounds.Y, 60)
}

func TestGridAutoFillRowsUseDefiniteMinAndMaxHeightConstraints(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<section id=minimum style="display:grid;width:100px;min-height:230px;row-gap:10px;grid-template-rows:repeat(auto-fill,50px)"></section>
		<section id=maximum style="display:grid;width:100px;max-height:230px;row-gap:10px;grid-template-rows:repeat(auto-fill,50px)"></section>
	</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 300, Height: 600})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"minimum", "maximum"} {
		geometry, ok := frame.Layout.Geometry(findStaticPageElementByID(document, id))
		if !ok {
			t.Fatalf("%s has no geometry", id)
		}
		if got, want := geometry.GridRowSizes(), []float64{50, 50, 50, 50}; !nearSlice(got, want) {
			t.Fatalf("%s auto-fill rows = %v, want %v", id, got, want)
		}
		assertNear(t, id+" constrained grid height", geometry.ContentBounds.Height, 230)
	}
}

func TestGridAutoFitDistributedAlignmentSkipsCollapsedMiddleTracks(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<section id=grid style="display:grid;width:430px;column-gap:10px;justify-content:space-between;grid-template-columns:repeat(auto-fit,100px)"><div id=first style="grid-column:1"></div><div id=last style="grid-column:4"></div></section>
	</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 500, Height: 200})
	if err != nil {
		t.Fatal(err)
	}
	geometry := func(id string) render.LayoutGeometry {
		t.Helper()
		value, ok := frame.Layout.Geometry(findStaticPageElementByID(document, id))
		if !ok {
			t.Fatalf("%s has no geometry", id)
		}
		return value
	}
	if got, want := geometry("grid").GridColumnSizes(), []float64{100, 0, 0, 100}; !nearSlice(got, want) {
		t.Fatalf("middle-collapse columns = %v, want %v", got, want)
	}
	first, last := geometry("first"), geometry("last")
	assertNear(t, "space-between surviving auto-fit tracks", last.Bounds.X-first.Bounds.X, 330)
}

func TestGridAutoRepeatFlexibleMaximumUsesOnlySurvivingTracks(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<section id=fit style="display:grid;width:450px;column-gap:10px;grid-template-columns:repeat(auto-fit,minmax(100px,1fr))"><div id=first></div><div id=second></div></section>
		<section id=fill style="display:grid;width:450px;column-gap:10px;grid-template-columns:repeat(auto-fill,minmax(100px,1fr))"><div></div><div></div></section>
	</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 500, Height: 200})
	if err != nil {
		t.Fatal(err)
	}
	geometry := func(id string) render.LayoutGeometry {
		t.Helper()
		value, ok := frame.Layout.Geometry(findStaticPageElementByID(document, id))
		if !ok {
			t.Fatalf("%s has no geometry", id)
		}
		return value
	}
	if got, want := geometry("fit").GridColumnSizes(), []float64{220, 220, 0, 0}; !nearSlice(got, want) {
		t.Fatalf("flexible auto-fit columns = %v, want %v", got, want)
	}
	if got, want := geometry("fill").GridColumnSizes(), []float64{105, 105, 105, 105}; !nearSlice(got, want) {
		t.Fatalf("flexible auto-fill columns = %v, want %v", got, want)
	}
	first, second := geometry("first"), geometry("second")
	assertNear(t, "flexible auto-fit first width", first.Bounds.Width, 220)
	assertNear(t, "flexible auto-fit second x", second.Bounds.X-first.Bounds.X, 230)
}

func TestGridDenseAutoPlacementBackfillsEarlierHole(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<section id=grid style="display:grid;width:200px;grid-template-columns:repeat(3, 60px);grid-auto-rows:30px;gap:10px;grid-auto-flow:row dense">
			<div id=a style="grid-column-end:span 2"></div>
			<div id=b style="grid-column-end:span 2"></div>
			<div id=c></div>
		</section>
	</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 240, Height: 200})
	if err != nil {
		t.Fatal(err)
	}
	bounds := func(id string) render.Rect {
		node := findStaticPageElementByID(document, id)
		geometry, ok := frame.Layout.Geometry(node)
		if !ok {
			t.Fatalf("%s has no geometry", id)
		}
		return geometry.Bounds
	}
	a, b, c := bounds("a"), bounds("b"), bounds("c")
	assertNear(t, "first dense span width", a.Width, 130)
	assertNear(t, "second dense row", b.Y-a.Y, 40)
	assertNear(t, "dense backfill row", c.Y, a.Y)
	assertNear(t, "dense backfill column", c.X, a.X+a.Width+10)
}

func TestGridAxisLockedItemsPlaceBeforeFullyAutomaticItems(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<section style="display:grid;width:100px;grid-template-columns:50px 50px;grid-auto-rows:20px">
			<div id=automatic></div><div id=locked style="grid-row:1"></div>
		</section>
	</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 120, Height: 100})
	if err != nil {
		t.Fatal(err)
	}
	automatic, _ := frame.Layout.Geometry(findStaticPageElementByID(document, "automatic"))
	locked, _ := frame.Layout.Geometry(findStaticPageElementByID(document, "locked"))
	assertNear(t, "axis-locked item first column", locked.Bounds.X, 0)
	assertNear(t, "fully automatic item follows", automatic.Bounds.X, 50)
}

func TestGridPlacementCreatesBoundedImplicitTracksAndResolvesNegativeLines(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<section id=grid style="display:grid;width:180px;grid-template-columns:60px 60px;grid-template-rows:20px 20px;grid-auto-columns:30px;grid-auto-rows:15px;gap:5px">
			<div id=negative style="grid-column:-2 / -1;grid-row:1"></div>
			<div id=implicit style="grid-column:4;grid-row:4"></div>
		</section>
	</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 220, Height: 200})
	if err != nil {
		t.Fatal(err)
	}
	geometry := func(id string) render.Rect {
		node := findStaticPageElementByID(document, id)
		value, ok := frame.Layout.Geometry(node)
		if !ok {
			t.Fatalf("%s has no geometry", id)
		}
		return value.Bounds
	}
	negative := geometry("negative")
	implicit := geometry("implicit")
	assertNear(t, "negative line selects second explicit column", negative.X, 65)
	assertNear(t, "negative line width", negative.Width, 60)
	assertNear(t, "implicit fourth column x", implicit.X, 165)
	assertNear(t, "implicit column width", implicit.Width, 30)
	assertNear(t, "implicit fourth row y", implicit.Y, 70)
	assertNear(t, "implicit row height", implicit.Height, 15)
}

func TestInlineGridUsesAtomicShrinkToFitGeometry(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<div style="width:300px"><span id=grid style="display:inline-grid;grid-template-columns:40px 60px;column-gap:10px;background:#123456"><span id=a>A</span><span id=b>B</span></span><span>after</span></div>
	</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 320, Height: 120})
	if err != nil {
		t.Fatal(err)
	}
	gridNode := findStaticPageElementByID(document, "grid")
	grid, ok := frame.Layout.Geometry(gridNode)
	if !ok {
		t.Fatal("inline-grid has no atomic geometry")
	}
	a, _ := frame.Layout.Geometry(findStaticPageElementByID(document, "a"))
	b, _ := frame.Layout.Geometry(findStaticPageElementByID(document, "b"))
	assertNear(t, "inline-grid shrink-to-fit width", grid.ContentBounds.Width, 110)
	assertNear(t, "inline-grid first track", a.Bounds.Width, 40)
	assertNear(t, "inline-grid second track x", b.Bounds.X-a.Bounds.X, 50)
}

func TestGridIntrinsicTracksHonorSpecifiedItemsAndIndefiniteFractionRatios(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<div><span id=inline style="display:inline-grid;grid-template-columns:auto"><span id=wide style="width:80px;height:10px"></span></span></div>
		<section id=rows style="display:grid;width:40px;grid-template-columns:40px;grid-template-rows:1fr 2fr"><div id=one style="height:10px"></div><div id=two style="height:10px"></div></section>
		<section style="display:grid;width:100px;grid-template-columns:.5fr"><div id=half></div></section>
		<div><span id=subunit style="display:inline-grid;grid-template-columns:.5fr 1fr"><span style="width:10px"></span><span style="width:10px"></span></span></div>
	</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 160, Height: 120})
	if err != nil {
		t.Fatal(err)
	}
	inline, _ := frame.Layout.Geometry(findStaticPageElementByID(document, "inline"))
	one, _ := frame.Layout.Geometry(findStaticPageElementByID(document, "one"))
	two, _ := frame.Layout.Geometry(findStaticPageElementByID(document, "two"))
	assertNear(t, "specified item contributes to inline-grid", inline.ContentBounds.Width, 80)
	assertNear(t, "indefinite first fr row", one.Bounds.Height, 10)
	assertNear(t, "indefinite second fr row starts", two.Bounds.Y-one.Bounds.Y, 10)
	// The specified item remains 10px tall, but its 2fr track contributes 20px
	// to the grid's intrinsic height.
	rows, _ := frame.Layout.Geometry(findStaticPageElementByID(document, "rows"))
	assertNear(t, "indefinite fr ratio grid height", rows.ContentBounds.Height, 30)
	half, _ := frame.Layout.Geometry(findStaticPageElementByID(document, "half"))
	assertNear(t, "subunit fr leaves definite free space", half.Bounds.Width, 50)
	subunit, _ := frame.Layout.Geometry(findStaticPageElementByID(document, "subunit"))
	assertNear(t, "subunit fr honors indefinite base sizing", subunit.ContentBounds.Width, 20)
}

func TestGridFlexibleTracksFreezeIntrinsicBasesBeforeSharingFreeSpace(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0"><section id=grid style="display:grid;width:100px;grid-template-columns:1fr 1fr"><div id=wide style="width:80px"></div><div id=narrow></div></section></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 120, Height: 80})
	if err != nil {
		t.Fatal(err)
	}
	grid, _ := frame.Layout.Geometry(findStaticPageElementByID(document, "grid"))
	tracks := grid.GridColumnSizes()
	if len(tracks) != 2 {
		t.Fatalf("grid tracks = %v, want two tracks", tracks)
	}
	assertNear(t, "intrinsic flexible track", tracks[0], 80)
	assertNear(t, "remaining flexible track", tracks[1], 20)
}

func TestEmptyGridDoesNotInventImplicitTracks(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0"><section id=grid style="display:grid;width:100px"></section></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 120, Height: 80})
	if err != nil {
		t.Fatal(err)
	}
	grid, _ := frame.Layout.Geometry(findStaticPageElementByID(document, "grid"))
	if columns, rows := grid.GridColumnSizes(), grid.GridRowSizes(); len(columns) != 0 || len(rows) != 0 {
		t.Fatalf("empty grid tracks = columns:%v rows:%v, want none", columns, rows)
	}
}

func TestGridIntrinsicTrackKeywordsAndMinMaxRanges(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<section id=grid style="display:grid;width:400px;grid-template-columns:min-content max-content minmax(40px,80px);grid-template-rows:min-content max-content minmax(10px,40px)">
			<div id=min style="grid-column:1;grid-row:1">alpha beta</div>
			<div id=max style="grid-column:2;grid-row:2">alpha beta</div>
			<div id=range style="grid-column:3;grid-row:3;height:5px"></div>
		</section>
	</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 440, Height: 180})
	if err != nil {
		t.Fatal(err)
	}
	grid, _ := frame.Layout.Geometry(findStaticPageElementByID(document, "grid"))
	minimum, _ := frame.Layout.Geometry(findStaticPageElementByID(document, "min"))
	maximum, _ := frame.Layout.Geometry(findStaticPageElementByID(document, "max"))
	ranged, _ := frame.Layout.Geometry(findStaticPageElementByID(document, "range"))
	if minimum.Bounds.Width <= 0 || maximum.Bounds.Width <= minimum.Bounds.Width {
		t.Fatalf("intrinsic keyword widths min=%v max=%v", minimum.Bounds.Width, maximum.Bounds.Width)
	}
	assertNear(t, "fixed minmax maximum", ranged.Bounds.Width, 80)
	columns, rows := grid.GridColumnSizes(), grid.GridRowSizes()
	if len(columns) != 3 || len(rows) != 3 {
		t.Fatalf("intrinsic grid tracks = columns:%v rows:%v", columns, rows)
	}
	assertNear(t, "fixed minmax column", columns[2], 80)
	assertNear(t, "fixed minmax row", rows[2], 40)
	if rows[0] <= 0 || rows[1] <= 0 {
		t.Fatalf("intrinsic row tracks = %v, want positive content contributions", rows)
	}
}

func TestGridMinMaxFlexibleTrackFreezesItsIntrinsicMinimum(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<section id=grid style="display:grid;width:200px;grid-template-columns:minmax(min-content,1fr) 1fr">
			<div id=wide style="width:160px"></div><div id=narrow></div>
		</section>
		<section id=floored style="display:grid;width:300px;grid-template-columns:minmax(100px,40px)"><div></div></section>
	</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 340, Height: 120})
	if err != nil {
		t.Fatal(err)
	}
	grid, _ := frame.Layout.Geometry(findStaticPageElementByID(document, "grid"))
	tracks := grid.GridColumnSizes()
	if len(tracks) != 2 {
		t.Fatalf("flex minmax tracks = %v", tracks)
	}
	assertNear(t, "minmax intrinsic floor", tracks[0], 160)
	assertNear(t, "remaining flexible space", tracks[1], 40)
	floored, _ := frame.Layout.Geometry(findStaticPageElementByID(document, "floored"))
	assertNear(t, "maximum below minimum is floored", floored.GridColumnSizes()[0], 100)
}

func TestInlineGridMinMaxContentRangeUsesMaxContentWidth(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0"><div><span id=grid style="display:inline-grid;grid-template-columns:minmax(min-content,max-content)"><span id=item>alpha beta</span></span></div></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 240, Height: 80})
	if err != nil {
		t.Fatal(err)
	}
	grid, _ := frame.Layout.Geometry(findStaticPageElementByID(document, "grid"))
	item, _ := frame.Layout.Geometry(findStaticPageElementByID(document, "item"))
	if grid.ContentBounds.Width <= 40 || item.Bounds.Width != grid.ContentBounds.Width {
		t.Fatalf("inline intrinsic range grid=%#v item=%#v", grid.ContentBounds, item.Bounds)
	}
}

func TestGridPercentageMinMaxMaximumDependsOnDefiniteAxis(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<section id=indefinite style="display:grid;width:100px;grid-template-rows:minmax(10px,50%)"><div style="height:30px"></div></section>
		<section id=definite style="display:grid;width:100px;height:100px;grid-template-rows:minmax(10px,50%)"><div style="height:30px"></div></section>
	</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 140, Height: 240})
	if err != nil {
		t.Fatal(err)
	}
	indefinite, _ := frame.Layout.Geometry(findStaticPageElementByID(document, "indefinite"))
	definite, _ := frame.Layout.Geometry(findStaticPageElementByID(document, "definite"))
	assertNear(t, "indefinite percentage maximum behaves as auto", indefinite.GridRowSizes()[0], 30)
	assertNear(t, "definite percentage maximum", definite.GridRowSizes()[0], 50)
}

func TestGridFitContentClampsMaxContentWithoutCrossingMinimum(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<section id=grid style="display:grid;width:400px;grid-template-columns:fit-content(50px) fit-content(20px) fit-content(25%)">
			<div style="grid-column:1">aaaa aaaa</div>
			<div style="grid-column:2">aaaa aaaa</div>
			<div style="grid-column:3">aaaa aaaa aaaa aaaa</div>
		</section>
	</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 440, Height: 100})
	if err != nil {
		t.Fatal(err)
	}
	grid, _ := frame.Layout.Geometry(findStaticPageElementByID(document, "grid"))
	tracks := grid.GridColumnSizes()
	if len(tracks) != 3 {
		t.Fatalf("fit-content tracks = %v", tracks)
	}
	assertNear(t, "fixed fit-content limit", tracks[0], 50)
	assertNear(t, "fit-content floor at min-content", tracks[1], 36)
	assertNear(t, "percentage fit-content limit", tracks[2], 100)
}

func TestGridAutomaticTrackPatternsCycleForwardAndBackward(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<section id=grid style="display:grid;width:300px;grid-template-columns:50px;grid-auto-columns:10px 20px 30px;grid-template-rows:15px;grid-auto-rows:5px 10px">
			<div style="grid-column:-4;grid-row:-4"></div>
			<div style="grid-column:-3;grid-row:-3"></div>
			<div style="grid-column:1;grid-row:1"></div>
			<div style="grid-column:2;grid-row:2"></div>
			<div style="grid-column:3;grid-row:3"></div>
			<div style="grid-column:4;grid-row:4"></div>
			<div style="grid-column:5;grid-row:5"></div>
		</section>
	</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 340, Height: 180})
	if err != nil {
		t.Fatal(err)
	}
	grid, _ := frame.Layout.Geometry(findStaticPageElementByID(document, "grid"))
	wantColumns := []float64{20, 30, 50, 10, 20, 30, 10}
	wantRows := []float64{5, 10, 15, 5, 10, 5, 10}
	if got := grid.GridColumnSizes(); !nearSlice(got, wantColumns) {
		t.Fatalf("automatic column pattern = %v, want %v", got, wantColumns)
	}
	if got := grid.GridRowSizes(); !nearSlice(got, wantRows) {
		t.Fatalf("automatic row pattern = %v, want %v", got, wantRows)
	}
}

func nearSlice(got, want []float64) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if math.Abs(got[index]-want[index]) > 0.001 {
			return false
		}
	}
	return true
}

func TestGridAlignmentOverlapPaintAndHitOrderFollowOrderModifiedItems(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<section id=grid style="display:grid;width:100px;grid-template-columns:100px;grid-template-rows:60px;align-items:center">
			<div id=front style="grid-column:1;grid-row:1;height:20px;background:#0000ff;order:2"></div>
			<div id=back style="grid-column:1;grid-row:1;height:40px;background:#ff0000;order:1"></div>
		</section>
	</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 140, Height: 100})
	if err != nil {
		t.Fatal(err)
	}
	frontNode := findStaticPageElementByID(document, "front")
	backNode := findStaticPageElementByID(document, "back")
	front, _ := frame.Layout.Geometry(frontNode)
	back, _ := frame.Layout.Geometry(backNode)
	assertNear(t, "centered front y", front.Bounds.Y, 20)
	assertNear(t, "centered back y", back.Bounds.Y, 10)
	redIndex, blueIndex := -1, -1
	for index, command := range frame.DisplayList.Commands {
		if command.Kind != render.FillRectCommand {
			continue
		}
		switch command.Color {
		case color.NRGBA{R: 0xff, A: 0xff}:
			redIndex = index
		case color.NRGBA{B: 0xff, A: 0xff}:
			blueIndex = index
		}
	}
	if redIndex < 0 || blueIndex <= redIndex {
		t.Fatalf("grid overlap paint order red=%d blue=%d", redIndex, blueIndex)
	}
	if hit := render.HitTest(frame, 50, 30); hit != frontNode {
		t.Fatalf("grid overlap hit = %#v, want front %#v (back %#v)", hit, frontNode, backNode)
	}
}

func TestGridContentAndSelfAlignmentUseTrackAndAreaFreeSpace(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<section id=grid style="display:grid;width:300px;height:200px;grid-template-columns:50px 50px;grid-template-rows:40px 40px;gap:10px;justify-content:space-between;align-content:center;justify-items:center;align-items:end">
			<div id=centered style="grid-column:1;grid-row:1;width:20px;height:10px"></div>
			<div id=overridden style="grid-column:2;grid-row:2;width:20px;height:10px;justify-self:end;align-self:start"></div>
		</section>
	</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 340, Height: 240})
	if err != nil {
		t.Fatal(err)
	}
	centered, _ := frame.Layout.Geometry(findStaticPageElementByID(document, "centered"))
	overridden, _ := frame.Layout.Geometry(findStaticPageElementByID(document, "overridden"))
	assertNear(t, "centered grid item x", centered.Bounds.X, 15)
	assertNear(t, "end-aligned grid item y", centered.Bounds.Y, 85)
	assertNear(t, "self-end grid item x", overridden.Bounds.X, 280)
	assertNear(t, "self-start grid item y", overridden.Bounds.Y, 105)
}

func TestGridNormalStretchesAutoTracksButStartKeepsMaxContent(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<section id=normal style="display:grid;width:200px;grid-template-columns:auto"><div>aa</div></section>
		<section id=start style="display:grid;width:200px;grid-template-columns:auto;justify-content:start"><div>aa</div></section>
	</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 240, Height: 100})
	if err != nil {
		t.Fatal(err)
	}
	normal, _ := frame.Layout.Geometry(findStaticPageElementByID(document, "normal"))
	start, _ := frame.Layout.Geometry(findStaticPageElementByID(document, "start"))
	assertNear(t, "normal auto track stretch", normal.GridColumnSizes()[0], 200)
	if got := start.GridColumnSizes()[0]; got <= 0 || got >= 200 {
		t.Fatalf("start-aligned auto track = %v, want intrinsic width", got)
	}
}

func TestGridCreatesAnonymousTextItemsAndBlockifiesElementItems(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0"><section style="display:grid;width:100px;grid-template-columns:50px 50px;grid-auto-rows:20px">hello<span id=item>world</span></section></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 140, Height: 80})
	if err != nil {
		t.Fatal(err)
	}
	hello := findTextFragment(collectTextFragments(frame.Root), "hello")
	item, ok := frame.Layout.Geometry(findStaticPageElementByID(document, "item"))
	if hello == nil || !ok {
		t.Fatalf("anonymous text/item geometry = %#v, %t", hello, ok)
	}
	assertNear(t, "anonymous text starts first cell", hello.X, 0)
	assertNear(t, "inline item blockified into second cell", item.Bounds.X, 50)
	assertNear(t, "blockified item fills track", item.Bounds.Width, 50)
}
