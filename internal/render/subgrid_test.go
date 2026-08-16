package render_test

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	htmlparser "github.com/JediWattson/gossamer/internal/html"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestSubgridAdoptsParentTracksGuttersAndLineNames(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<section id=normal style="display:grid;width:320px;grid-template-columns:[p1] 100px [p2] 200px [p3];column-gap:20px;grid-template-rows:20px">
			<div id=normal-sub style="display:grid;grid-column:1 / span 2;grid-template-columns:subgrid [l1] [l2] [l3];grid-template-rows:20px"><i id=n1 style="grid-column:l1 / p2"></i><i id=n2 style="grid-column:p2 / l3"></i></div>
		</section>
		<section id=zero style="display:grid;width:320px;grid-template-columns:100px 200px;column-gap:20px;grid-template-rows:20px">
			<div id=zero-sub style="display:grid;grid-column:1 / span 2;grid-template-columns:subgrid;grid-template-rows:20px;column-gap:0"><i id=z1 style="grid-column:1"></i><i id=z2 style="grid-column:2"></i></div>
		</section>
	</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 500, Height: 200})
	if err != nil {
		t.Fatal(err)
	}
	geometry := func(id string) render.LayoutGeometry {
		node := findElementByID(document, id)
		if node == nil {
			t.Fatalf("missing element %q", id)
		}
		value, ok := frame.Layout.Geometry(node)
		if !ok {
			t.Fatalf("missing geometry for %q", id)
		}
		return value
	}

	normal := geometry("normal-sub")
	if !normal.GridColumnSubgrid() || !nearSlice(normal.GridColumnSizes(), []float64{100, 200}) {
		computed, _ := frame.ComputedStyles.Lookup(findElementByID(document, "normal-sub"))
		t.Fatalf("normal subgrid tracks = %v, subgrid=%t computed=%t", normal.GridColumnSizes(), normal.GridColumnSubgrid(), computed.GridTemplateColumns().IsSubgrid())
	}
	for _, test := range []struct {
		id          string
		x, width    float64
		description string
	}{
		{id: "n1", x: 0, width: 100, description: "combined local/parent first line"},
		{id: "n2", x: 120, width: 200, description: "adopted parent middle line"},
		{id: "z1", x: 0, width: 110, description: "zero-gap first track expansion"},
		{id: "z2", x: 110, width: 210, description: "zero-gap second track expansion"},
	} {
		got := geometry(test.id).Bounds
		assertNear(t, test.description+" x", got.X, test.x)
		assertNear(t, test.description+" width", got.Width, test.width)
	}
	zero := geometry("zero-sub")
	if !zero.GridColumnSubgrid() || !nearSlice(zero.GridColumnSizes(), []float64{110, 210}) {
		t.Fatalf("explicit zero-gap subgrid tracks = %v, subgrid=%t", zero.GridColumnSizes(), zero.GridColumnSubgrid())
	}
}

func TestSubgridAdoptsRowsAndFallsBackWithoutParentGrid(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<section style="display:grid;width:100px;grid-template-columns:100px;grid-template-rows:30px 50px;row-gap:10px">
			<div id=rows-sub style="display:grid;grid-column:1;grid-row:1 / span 2;grid-template-columns:100px;grid-template-rows:subgrid"><i id=r1 style="grid-row:1"></i><i id=r2 style="grid-row:2"></i></div>
		</section>
		<section id=orphan style="display:grid;width:200px;grid-template-columns:subgrid"><i></i><i></i></section>
	</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 300, Height: 220})
	if err != nil {
		t.Fatal(err)
	}
	geometry := func(id string) render.LayoutGeometry {
		node := findElementByID(document, id)
		value, ok := frame.Layout.Geometry(node)
		if !ok {
			t.Fatalf("missing geometry for %q", id)
		}
		return value
	}
	rows := geometry("rows-sub")
	if !rows.GridRowSubgrid() || !nearSlice(rows.GridRowSizes(), []float64{30, 50}) {
		computed, _ := frame.ComputedStyles.Lookup(findElementByID(document, "rows-sub"))
		t.Fatalf("row subgrid tracks = %v, subgrid=%t computed=%t", rows.GridRowSizes(), rows.GridRowSubgrid(), computed.GridTemplateRows().IsSubgrid())
	}
	if got, want := rows.GridRowLineNames(), [][]string{nil, nil, nil}; !reflect.DeepEqual(got, want) {
		t.Fatalf("row subgrid local line sets = %#v, want %#v", got, want)
	}
	assertNear(t, "first subgrid row height", geometry("r1").Bounds.Height, 30)
	assertNear(t, "second subgrid row offset", geometry("r2").Bounds.Y-rows.Bounds.Y, 40)
	assertNear(t, "second subgrid row height", geometry("r2").Bounds.Height, 50)
	if orphan := geometry("orphan"); orphan.GridColumnSubgrid() {
		t.Fatal("orphan subgrid axis did not fall back to an independent none track list")
	}
}

func TestSubgridDescendantsContributeToParentIntrinsicTracks(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<section id=columns style="display:grid;width:500px;grid-template-columns:auto auto;justify-content:start;grid-template-rows:20px"><div id=column-sub style="display:grid;grid-column:1/span 2;grid-template-columns:subgrid;grid-template-rows:20px"><i id=c1 style="grid-column:1;width:120px"></i><i id=c2 style="grid-column:2;width:40px"></i></div></section>
		<section id=rows style="display:grid;width:100px;grid-template-columns:100px;grid-template-rows:auto auto;align-content:start"><div id=row-sub style="display:grid;grid-row:1/span 2;grid-template-columns:100px;grid-template-rows:subgrid"><i id=ir1 style="grid-row:1;height:30px"></i><i id=ir2 style="grid-row:2;height:50px"></i></div></section>
	</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 600, Height: 220})
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
		t.Fatalf("subgrid inline contributions = %v, want %v", got, want)
	}
	assertNear(t, "intrinsic second column x", geometry("c2").Bounds.X, 120)
	if got, want := geometry("rows").GridRowSizes(), []float64{30, 50}; !nearSlice(got, want) {
		t.Fatalf("subgrid block contributions = %v, want %v", got, want)
	}
	assertNear(t, "intrinsic second row y", geometry("ir2").Bounds.Y-geometry("row-sub").Bounds.Y, 30)
}

func TestSubgridClampsPlacementAndOverlapsWhenBothAxesAreExhausted(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0"><section style="display:grid;width:200px;grid-template-columns:100px 100px;grid-template-rows:20px"><div id=sub style="display:grid;grid-column:1/span 2;grid-row:1;grid-template-columns:subgrid;grid-template-rows:subgrid"><i id=a></i><i id=b></i><i id=c></i><i id=d style="grid-column:2/span 3"></i></div></section></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 300, Height: 100})
	if err != nil {
		t.Fatal(err)
	}
	geometry := func(id string) render.Rect {
		value, ok := frame.Layout.Geometry(findElementByID(document, id))
		if !ok {
			t.Fatalf("missing geometry for %q", id)
		}
		return value.Bounds
	}
	for _, test := range []struct {
		id       string
		x, width float64
	}{
		{id: "a", x: 0, width: 100},
		{id: "b", x: 100, width: 100},
		{id: "c", x: 0, width: 100},
		{id: "d", x: 100, width: 100},
	} {
		got := geometry(test.id)
		assertNear(t, test.id+" clamped x", got.X, test.x)
		assertNear(t, test.id+" clamped width", got.Width, test.width)
	}
}

func TestSubgridEdgeDecorationsContributeWithoutDoubleShiftingDescendants(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0"><section id=parent style="display:grid;width:500px;grid-template-columns:auto auto;justify-content:start;column-gap:20px;grid-template-rows:20px"><div id=sub style="display:grid;grid-column:1/span 2;grid-template-columns:subgrid;grid-template-rows:20px;padding-left:10px;padding-right:20px;border-left:2px solid;border-right:4px solid"><i id=edge-a style="grid-column:1;width:100px"></i><i id=edge-b style="grid-column:2;width:40px"></i></div></section></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 600, Height: 100})
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
	if got, want := geometry("parent").GridColumnSizes(), []float64{112, 64}; !nearSlice(got, want) {
		t.Fatalf("decorated subgrid parent tracks = %v, want %v", got, want)
	}
	for _, test := range []struct {
		id       string
		x, width float64
	}{
		{id: "sub", x: 0, width: 196},
		{id: "edge-a", x: 12, width: 100},
		{id: "edge-b", x: 132, width: 40},
	} {
		got := geometry(test.id).Bounds
		assertNear(t, test.id+" decorated x", got.X, test.x)
		assertNear(t, test.id+" decorated width", got.Width, test.width)
	}
}

func TestNestedSubgridsPropagateTracksAndIntrinsicContributions(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0"><section id=nested-parent style="display:grid;width:500px;grid-template-columns:auto auto;justify-content:start;grid-template-rows:20px"><div id=middle style="display:grid;grid-column:1/span 2;grid-template-columns:subgrid;grid-template-rows:20px"><div id=inner style="display:grid;grid-column:1/span 2;grid-template-columns:subgrid;grid-template-rows:20px"><i id=nested-a style="grid-column:1;width:120px"></i><i id=nested-b style="grid-column:2;width:40px"></i></div></div></section></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 600, Height: 100})
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
	if got, want := geometry("nested-parent").GridColumnSizes(), []float64{120, 40}; !nearSlice(got, want) {
		t.Fatalf("nested subgrid parent tracks = %v, want %v", got, want)
	}
	if !geometry("middle").GridColumnSubgrid() || !geometry("inner").GridColumnSubgrid() {
		t.Fatal("nested subgrid axes were not retained")
	}
	assertNear(t, "nested second item x", geometry("nested-b").Bounds.X, 120)
	assertNear(t, "nested second item width", geometry("nested-b").Bounds.Width, 40)
}

func TestRowSubgridDescendantSharesParentBaselineGroup(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0"><section id=baseline-parent style="display:grid;width:200px;grid-template-columns:100px 100px;grid-template-rows:auto;align-items:baseline"><div style="font-size:32px;line-height:32px">parent-baseline</div><div style="display:grid;grid-column:2;grid-row:1;grid-template-columns:100px;grid-template-rows:subgrid;align-items:baseline"><div style="font-size:16px;line-height:16px;padding-top:20px">subgrid-baseline</div></div></section></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 300, Height: 120})
	if err != nil {
		t.Fatal(err)
	}
	fragments := collectTextFragments(frame.Root)
	parent := findTextFragment(fragments, "parent-baseline")
	subgrid := findTextFragment(fragments, "subgrid-baseline")
	if parent == nil || subgrid == nil {
		t.Fatalf("missing baseline fragments: %#v/%#v", parent, subgrid)
	}
	assertNear(t, "subgrid descendant parent baseline", parent.BaselineY, subgrid.BaselineY)
}

func FuzzSubgridLayoutStaysFinite(f *testing.F) {
	f.Add(byte(1), byte(2), byte(2), byte(3), byte(0))
	f.Add(byte(3), byte(4), byte(1), byte(8), byte(0xff))
	f.Add(byte(4), byte(3), byte(4), byte(5), byte(0x55))
	f.Fuzz(func(t *testing.T, rawDepth, rawColumns, rawRows, rawItems, rawModes byte) {
		depth := int(rawDepth%4) + 1
		columns := int(rawColumns%4) + 1
		rows := int(rawRows%4) + 1
		items := int(rawItems%8) + 1
		columnTrack := fmt.Sprintf("%dpx", 20+int(rawModes%21))
		rowTrack := fmt.Sprintf("%dpx", 10+int(rawModes%17))
		columnTemplate := strings.TrimSpace(strings.Repeat(columnTrack+" ", columns))
		rowTemplate := strings.TrimSpace(strings.Repeat(rowTrack+" ", rows))
		gap := int(rawModes % 7)

		var source strings.Builder
		outerDirection := "ltr"
		if rawModes&0x40 != 0 {
			outerDirection = "rtl"
		}
		fmt.Fprintf(&source, `<!doctype html><html><body style="margin:0"><section id="outer" style="display:grid;direction:%s;grid-template-columns:%s;grid-template-rows:%s;gap:%dpx">`, outerDirection, columnTemplate, rowTemplate, gap)
		for level := range depth {
			subgridGap := "normal"
			if rawModes&(1<<uint(level%8)) != 0 {
				subgridGap = fmt.Sprintf("%dpx", (int(rawModes)+level)%5)
			}
			writingMode := "horizontal-tb"
			if (int(rawModes)+level)%3 == 1 {
				writingMode = "vertical-rl"
			} else if (int(rawModes)+level)%3 == 2 {
				writingMode = "vertical-lr"
			}
			direction := "ltr"
			if rawModes&(1<<uint((level+3)%8)) != 0 {
				direction = "rtl"
			}
			fmt.Fprintf(&source, `<div style="display:grid;writing-mode:%s;direction:%s;grid-column:1/span %d;grid-row:1/span %d;grid-template-columns:subgrid;grid-template-rows:subgrid;gap:%s;padding:%dpx">`, writingMode, direction, columns, rows, subgridGap, level%3)
		}
		for index := range items {
			column := index%columns + 1
			row := index%rows + 1
			fmt.Fprintf(&source, `<i style="grid-column:%d/span %d;grid-row:%d;width:%dpx;height:%dpx">x</i>`, column, int(rawModes%3)+1, row, 1+index%19, 1+index%13)
		}
		for range depth {
			source.WriteString(`</div>`)
		}
		source.WriteString(`</section></body></html>`)

		document, err := htmlparser.Parse(strings.NewReader(source.String()))
		if err != nil {
			t.Fatal(err)
		}
		frame, err := render.Render(document, render.Viewport{Width: 400, Height: 300})
		if err != nil {
			t.Fatal(err)
		}
		outer := findElementByID(document, "outer")
		geometry, ok := frame.Layout.Geometry(outer)
		if !ok || geometry.Bounds.Width < 0 || geometry.Bounds.Height < 0 {
			t.Fatalf("subgrid geometry = %#v, %t", geometry, ok)
		}
		values := []float64{geometry.Bounds.X, geometry.Bounds.Y, geometry.Bounds.Width, geometry.Bounds.Height}
		for _, command := range frame.DisplayList.Commands {
			values = append(values, command.Rect.X, command.Rect.Y, command.Rect.Width, command.Rect.Height, command.X, command.BaselineY)
		}
		for _, value := range values {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				t.Fatalf("non-finite subgrid result %v", value)
			}
		}
	})
}

func findElementByID(root *dom.Node, id string) *dom.Node {
	if root == nil {
		return nil
	}
	if root.Type == dom.ElementNode {
		for _, attribute := range root.Attributes {
			if attribute.Name == "id" && attribute.Value == id {
				return root
			}
		}
	}
	for _, child := range root.Children {
		if found := findElementByID(child, id); found != nil {
			return found
		}
	}
	return nil
}
