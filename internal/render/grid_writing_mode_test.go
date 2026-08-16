package render_test

import (
	"fmt"
	"strings"
	"testing"

	htmlparser "github.com/JediWattson/gossamer/internal/html"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestVerticalGridUsesLogicalColumnsRowsAndDirection(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		mode          string
		direction     string
		firstRowAfter bool
		firstColAfter bool
	}{
		{mode: "vertical-rl", direction: "ltr", firstRowAfter: true},
		{mode: "vertical-rl", direction: "rtl", firstRowAfter: true, firstColAfter: true},
		{mode: "vertical-lr", direction: "ltr"},
		{mode: "vertical-lr", direction: "rtl", firstColAfter: true},
	} {
		test := test
		t.Run(test.mode+"_"+test.direction, func(t *testing.T) {
			t.Parallel()
			document, err := htmlparser.Parse(strings.NewReader(fmt.Sprintf(`<!doctype html><html><body style="margin:0">
				<section id=grid style="display:grid;writing-mode:%s;direction:%s;width:120px;height:200px;grid-template-columns:50px 70px;grid-template-rows:40px 60px;column-gap:10px;row-gap:20px;justify-content:start;align-content:start">
					<i id=a style="grid-column:1;grid-row:1;background:#123456"></i><i id=b style="grid-column:2;grid-row:2;background:#abcdef"></i>
				</section></body></html>`, test.mode, test.direction)))
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
			grid, a, b := geometry("grid"), geometry("a"), geometry("b")
			assertNear(t, "vertical grid physical width", grid.Bounds.Width, 120)
			assertNear(t, "vertical grid physical height", grid.Bounds.Height, 200)
			if got, want := grid.GridColumnSizes(), []float64{50, 70}; !nearSlice(got, want) {
				t.Fatalf("vertical grid columns = %v, want %v", got, want)
			}
			if got, want := grid.GridRowSizes(), []float64{40, 60}; !nearSlice(got, want) {
				t.Fatalf("vertical grid rows = %v, want %v", got, want)
			}
			assertNear(t, "first logical row physical width", a.Bounds.Width, 40)
			assertNear(t, "first logical column physical height", a.Bounds.Height, 50)
			assertNear(t, "second logical row physical width", b.Bounds.Width, 60)
			assertNear(t, "second logical column physical height", b.Bounds.Height, 70)
			if got := a.Bounds.X > b.Bounds.X; got != test.firstRowAfter {
				t.Fatalf("first row x %.1f vs second %.1f: after=%t, want %t", a.Bounds.X, b.Bounds.X, got, test.firstRowAfter)
			}
			if got := a.Bounds.Y > b.Bounds.Y; got != test.firstColAfter {
				t.Fatalf("first column y %.1f vs second %.1f: after=%t, want %t", a.Bounds.Y, b.Bounds.Y, got, test.firstColAfter)
			}
			if hit := render.HitTest(frame, a.Bounds.X+a.Bounds.Width/2, a.Bounds.Y+a.Bounds.Height/2); hit != findElementByID(document, "a") {
				t.Fatalf("vertical grid hit = %v, want a", hit)
			}
		})
	}
}

func TestOrthogonalSubgridAdoptsOppositeParentAxes(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<section id=parent style="display:grid;width:210px;grid-template-columns:[c1] 80px [c2] 120px [c3];grid-template-rows:[r1] 50px [r2] 70px [r3];column-gap:10px;row-gap:10px;justify-content:start;align-content:start">
			<div id=sub style="display:grid;writing-mode:vertical-rl;direction:ltr;grid-column:1/span 2;grid-row:1/span 2;grid-template-columns:subgrid [local-inline];grid-template-rows:subgrid [local-block]">
				<i id=a style="grid-column:r1/r2;grid-row:c3/c2"></i><i id=b style="grid-column:r2/r3;grid-row:c2/c1"></i>
			</div>
		</section></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 300, Height: 250})
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
	parent, sub, a, b := geometry("parent"), geometry("sub"), geometry("a"), geometry("b")
	if got, want := parent.GridColumnSizes(), []float64{80, 120}; !nearSlice(got, want) {
		t.Fatalf("parent columns = %v, want %v", got, want)
	}
	if got, want := parent.GridRowSizes(), []float64{50, 70}; !nearSlice(got, want) {
		t.Fatalf("parent rows = %v, want %v", got, want)
	}
	if !sub.GridColumnSubgrid() || !sub.GridRowSubgrid() {
		t.Fatal("orthogonal child did not retain both subgridded axes")
	}
	if got, want := sub.GridColumnSizes(), []float64{50, 70}; !nearSlice(got, want) {
		t.Fatalf("child columns did not adopt parent rows: %v, want %v", got, want)
	}
	if got, want := sub.GridRowSizes(), []float64{120, 80}; !nearSlice(got, want) {
		t.Fatalf("child rows did not adopt reversed parent columns: %v, want %v", got, want)
	}
	assertNear(t, "orthogonal subgrid width", sub.Bounds.Width, 210)
	assertNear(t, "orthogonal subgrid height", sub.Bounds.Height, 130)
	assertNear(t, "first child right track x", a.Bounds.X-sub.Bounds.X, 90)
	assertNear(t, "first child top track y", a.Bounds.Y-sub.Bounds.Y, 0)
	assertNear(t, "first child width", a.Bounds.Width, 120)
	assertNear(t, "first child height", a.Bounds.Height, 50)
	assertNear(t, "second child left track x", b.Bounds.X-sub.Bounds.X, 0)
	assertNear(t, "second child lower track y", b.Bounds.Y-sub.Bounds.Y, 60)
	assertNear(t, "second child width", b.Bounds.Width, 80)
	assertNear(t, "second child height", b.Bounds.Height, 70)
}

func TestGridDirectionMirrorsPlacementAndSubgridLineOrder(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<section id=plain style="display:grid;direction:rtl;width:220px;grid-template-columns:100px 100px;column-gap:20px;grid-template-rows:20px;justify-content:start"><i id=p1 style="grid-column:1"></i><i id=p2 style="grid-column:2"></i></section>
		<section id=parent style="display:grid;direction:rtl;width:170px;grid-template-columns:[p1] 100px [p2] 50px [p3];column-gap:20px;grid-template-rows:20px;justify-content:start">
			<div id=sub style="display:grid;direction:ltr;grid-column:1/span 2;grid-template-columns:subgrid;grid-template-rows:20px"><i id=left style="grid-column:p3/p2"></i><i id=right style="grid-column:p2/p1"></i></div>
		</section>
	</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 300, Height: 120})
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
	plain := geometry("plain")
	assertNear(t, "rtl first logical column", geometry("p1").Bounds.X-plain.Bounds.X, 120)
	assertNear(t, "rtl second logical column", geometry("p2").Bounds.X-plain.Bounds.X, 0)
	if got, want := geometry("sub").GridColumnSizes(), []float64{50, 100}; !nearSlice(got, want) {
		t.Fatalf("opposite-direction subgrid tracks = %v, want %v", got, want)
	}
	assertNear(t, "reversed named left track width", geometry("left").Bounds.Width, 50)
	assertNear(t, "reversed named right track width", geometry("right").Bounds.Width, 100)
	if geometry("left").Bounds.X >= geometry("right").Bounds.X {
		t.Fatalf("subgrid line order was not expressed in the child's ltr writing direction: left=%v right=%v", geometry("left").Bounds, geometry("right").Bounds)
	}
}

func TestVerticalGridPhysicalEdgesAndAutoInlineSizeTransformTogether(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<section id=auto style="display:grid;writing-mode:vertical-lr;width:100px;grid-template-columns:30px 50px;grid-template-rows:40px;column-gap:10px;justify-content:start;align-content:start;padding:3px 5px 7px 11px;border-style:solid;border-width:2px 4px 6px 8px"><i id=auto-child style="grid-column:2;grid-row:1;background:#123456"></i></section>
	</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 300, Height: 250})
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
	auto, child := geometry("auto"), geometry("auto-child")
	assertNear(t, "auto vertical grid physical width", auto.Bounds.Width, 128)
	assertNear(t, "auto vertical grid physical height", auto.Bounds.Height, 108)
	assertNear(t, "physical left padding and border", child.Bounds.X-auto.Bounds.X, 19)
	assertNear(t, "second logical column physical y", child.Bounds.Y-auto.Bounds.Y, 45)
	assertNear(t, "logical row physical width", child.Bounds.Width, 40)
	assertNear(t, "logical column physical height", child.Bounds.Height, 50)
}

func TestHorizontalGridDescendantKeepsIndependentAxesInsideVerticalGrid(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<section id=outer style="display:grid;writing-mode:vertical-lr;width:200px;height:200px;grid-template-columns:200px;grid-template-rows:200px;justify-content:start;align-content:start;justify-items:start;align-items:start">
			<div id=inner style="display:grid;writing-mode:horizontal-tb;width:160px;height:100px;grid-template-columns:60px 100px;grid-template-rows:50px 20px;justify-content:start;align-content:start"><i id=a style="grid-column:1;background:#123456">A</i><i id=b style="grid-column:2;background:#abcdef">B</i><img id=image style="grid-column:2;grid-row:2;width:20px;height:10px"></div>
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
	inner, a, b, image := geometry("inner"), geometry("a"), geometry("b"), geometry("image")
	assertNear(t, "independent horizontal grid width", inner.Bounds.Width, 160)
	assertNear(t, "independent horizontal grid height", inner.Bounds.Height, 100)
	assertNear(t, "horizontal first column width", a.Bounds.Width, 60)
	assertNear(t, "horizontal second column width", b.Bounds.Width, 100)
	assertNear(t, "horizontal row height", a.Bounds.Height, 50)
	assertNear(t, "horizontal child y", b.Bounds.Y, a.Bounds.Y)
	assertNear(t, "horizontal second column x", b.Bounds.X-a.Bounds.X, 60)
	assertNear(t, "horizontal replaced width", image.Bounds.Width, 20)
	assertNear(t, "horizontal replaced height", image.Bounds.Height, 10)
	if hit := render.HitTest(frame, b.Bounds.X+b.Bounds.Width/2, b.Bounds.Y+b.Bounds.Height/2); hit != findElementByID(document, "b") {
		t.Fatalf("independent horizontal descendant hit = %v, want b", hit)
	}
}

func TestOppositeVerticalGridDescendantReversesItsOwnBlockAxis(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<section style="display:grid;writing-mode:vertical-rl;width:200px;height:200px;grid-template-columns:200px;grid-template-rows:200px;justify-content:start;align-content:start;justify-items:start;align-items:start">
			<div id=inner style="display:grid;writing-mode:vertical-lr;width:160px;height:100px;grid-template-columns:50px 50px;grid-template-rows:60px 100px;justify-content:start;align-content:start"><i id=a style="grid-column:1;grid-row:1"></i><i id=b style="grid-column:1;grid-row:2"></i></div>
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
	inner, a, b := geometry("inner"), geometry("a"), geometry("b")
	assertNear(t, "opposite vertical child width", inner.Bounds.Width, 160)
	assertNear(t, "opposite vertical child height", inner.Bounds.Height, 100)
	assertNear(t, "vertical-lr first row width", a.Bounds.Width, 60)
	assertNear(t, "vertical-lr second row width", b.Bounds.Width, 100)
	if a.Bounds.X >= b.Bounds.X {
		t.Fatalf("vertical-lr child did not reverse its vertical-rl parent's block progression: a=%v b=%v", a.Bounds, b.Bounds)
	}
}

func TestNestedOrthogonalSubgridReturnsToOuterPhysicalAxes(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<section id=outer style="display:grid;width:210px;grid-template-columns:80px 120px;grid-template-rows:50px 70px;column-gap:10px;row-gap:10px;justify-content:start;align-content:start">
			<div id=middle style="display:grid;writing-mode:vertical-rl;grid-column:1/span 2;grid-row:1/span 2;grid-template-columns:subgrid;grid-template-rows:subgrid">
				<div id=inner style="display:grid;writing-mode:horizontal-tb;grid-column:1/span 2;grid-row:1/span 2;grid-template-columns:subgrid;grid-template-rows:subgrid"><i id=a style="grid-column:1;grid-row:1"></i><i id=b style="grid-column:2;grid-row:2"></i></div>
			</div>
		</section>
	</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 300, Height: 250})
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
	inner, a, b := geometry("inner"), geometry("a"), geometry("b")
	if !inner.GridColumnSubgrid() || !inner.GridRowSubgrid() {
		t.Fatal("nested orthogonal Grid did not retain both subgridded axes")
	}
	if got, want := inner.GridColumnSizes(), []float64{80, 120}; !nearSlice(got, want) {
		t.Fatalf("nested horizontal columns = %v, want %v", got, want)
	}
	if got, want := inner.GridRowSizes(), []float64{50, 70}; !nearSlice(got, want) {
		t.Fatalf("nested horizontal rows = %v, want %v", got, want)
	}
	assertNear(t, "nested first column x", a.Bounds.X-inner.Bounds.X, 0)
	assertNear(t, "nested first row y", a.Bounds.Y-inner.Bounds.Y, 0)
	assertNear(t, "nested first width", a.Bounds.Width, 80)
	assertNear(t, "nested first height", a.Bounds.Height, 50)
	assertNear(t, "nested second column x", b.Bounds.X-inner.Bounds.X, 90)
	assertNear(t, "nested second row y", b.Bounds.Y-inner.Bounds.Y, 60)
	assertNear(t, "nested second width", b.Bounds.Width, 120)
	assertNear(t, "nested second height", b.Bounds.Height, 70)
}

func TestOrthogonalSubgridContributesIntrinsicSizesAcrossAxes(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<section id=columns style="display:grid;width:500px;grid-template-columns:auto auto;grid-template-rows:100px;justify-content:start">
			<div id=column-sub style="display:grid;writing-mode:vertical-rl;grid-column:1/span 2;grid-row:1;grid-template-columns:100px;grid-template-rows:subgrid;align-self:start;height:20px">
				<i id=c-right style="grid-row:1;width:40px"></i><i id=c-left style="grid-row:2;width:120px"></i>
			</div>
		</section>
		<section id=rows style="display:grid;width:100px;grid-template-columns:100px;grid-template-rows:auto auto;align-content:start">
			<div id=row-sub style="display:grid;writing-mode:vertical-lr;grid-column:1;grid-row:1/span 2;grid-template-columns:subgrid;grid-template-rows:100px">
				<i id=r-top style="grid-column:1;height:30px"></i><i id=r-bottom style="grid-column:2;height:50px"></i>
			</div>
		</section>
	</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 600, Height: 300})
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
	if got, want := geometry("columns").GridColumnSizes(), []float64{120, 40}; !nearSlice(got, want) {
		t.Fatalf("orthogonal block-axis contributions = %v, want %v", got, want)
	}
	if got, want := geometry("rows").GridRowSizes(), []float64{30, 50}; !nearSlice(got, want) {
		t.Fatalf("orthogonal inline-axis contributions = %v, want %v", got, want)
	}
	assertNear(t, "single-axis subgrid does not force orthogonal height", geometry("column-sub").Bounds.Height, 20)
	assertNear(t, "right child width contribution", geometry("c-right").Bounds.Width, 40)
	assertNear(t, "left child width contribution", geometry("c-left").Bounds.Width, 120)
	assertNear(t, "top child height contribution", geometry("r-top").Bounds.Height, 30)
	assertNear(t, "bottom child height contribution", geometry("r-bottom").Bounds.Height, 50)
}
