package render_test

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestAutomaticTablePercentageColumnsUseCSSSizingGuesses(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0"><table id=table style="width:400px;border-spacing:0"><tr><td id=percent style="width:25%;height:10px;padding:0"></td><td id=pixel style="width:100px;height:10px;padding:0"></td><td id=auto style="height:10px;padding:0"></td></tr></table></body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 800, Height: 600})
	if err != nil {
		t.Fatal(err)
	}
	assertTableBoxWidth(t, frame, document, "table", 400)
	assertTableBoxWidth(t, frame, document, "percent", 100)
	assertTableBoxWidth(t, frame, document, "pixel", 100)
	assertTableBoxWidth(t, frame, document, "auto", 200)
}

func TestAutomaticTablePercentageContributionsClampAndHonorMaxWidth(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0"><table style="width:400px;border-spacing:0"><tr><td id=first style="width:80%;max-width:25%;height:10px;padding:0"></td><td id=second style="width:80%;height:10px;padding:0"></td><td id=third style="height:10px;padding:0"></td></tr></table></body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 800, Height: 600})
	if err != nil {
		t.Fatal(err)
	}
	assertTableBoxWidth(t, frame, document, "first", 100)
	assertTableBoxWidth(t, frame, document, "second", 300)
	assertTableBoxWidth(t, frame, document, "third", 0)
}

func TestAutomaticTableDistributesSpanningPercentageContribution(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0"><table style="width:400px;border-spacing:0"><tr><td id=span colspan=2 style="width:75%;height:10px;padding:0"></td><td style="height:10px;padding:0"></td></tr><tr><td id=first style="height:10px;padding:0"></td><td id=second style="height:10px;padding:0"></td><td id=last style="height:10px;padding:0"></td></tr></table></body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 800, Height: 600})
	if err != nil {
		t.Fatal(err)
	}
	assertTableBoxWidth(t, frame, document, "span", 300)
	assertTableBoxWidth(t, frame, document, "first", 150)
	assertTableBoxWidth(t, frame, document, "second", 150)
	assertTableBoxWidth(t, frame, document, "last", 100)
}

func TestAutomaticTableCellMinContentOverridesPercentage(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0"><table style="width:100px;border-spacing:0"><tr><td id=percent style="width:80%;height:10px;padding:0"><span style="display:inline-block;width:90px"></span></td><td id=auto style="height:10px;padding:0"></td></tr></table></body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 800, Height: 600})
	if err != nil {
		t.Fatal(err)
	}
	assertTableBoxWidth(t, frame, document, "percent", 90)
	assertTableBoxWidth(t, frame, document, "auto", 10)
}

func TestAutomaticTableCalcMixedConstraintRemainsAuto(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0"><table style="width:200px;border-spacing:0"><col style="width:calc(20% + 80px)"><tr><td id=first style="height:10px;padding:0"></td><td id=second style="height:10px;padding:0"></td></tr></table></body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 800, Height: 600})
	if err != nil {
		t.Fatal(err)
	}
	assertTableBoxWidth(t, frame, document, "first", 100)
	assertTableBoxWidth(t, frame, document, "second", 100)
}

func TestAutomaticTableColumnAndGroupMeasuresFollowHTMLMapping(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0">
		<table id=one style="border-spacing:0"><colgroup style="width:1px"><col style="width:10px"></colgroup><tr><td style="width:1px;padding:0"></td></tr></table>
		<table id=two style="border-spacing:0"><colgroup style="width:10px"><col style="width:1px"></colgroup><tr><td style="width:1px;padding:0"></td></tr></table>
		<table id=three style="border-spacing:0"><colgroup style="width:1px"><col style="width:1px"></colgroup><tr><td style="width:10px;padding:0"></td></tr></table>
		<table id=four style="border-spacing:0"><colgroup style="width:10px"><col></colgroup><tr><td style="width:1px;padding:0"></td></tr></table>
	</body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 800, Height: 600})
	if err != nil {
		t.Fatal(err)
	}
	assertTableBoxWidth(t, frame, document, "one", 10)
	assertTableBoxWidth(t, frame, document, "two", 1)
	assertTableBoxWidth(t, frame, document, "three", 10)
	assertTableBoxWidth(t, frame, document, "four", 10)
}

func TestTableSpecifiedWidthCannotUndercutCaptionOrGridMinimum(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0"><table id=table style="width:50px;border-spacing:0"><caption style="padding:0"><span style="display:inline-block;width:100px"></span></caption><tr><td id=cell style="width:75px;height:10px;padding:0"></td></tr></table></body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 800, Height: 600})
	if err != nil {
		t.Fatal(err)
	}
	assertTableBoxWidth(t, frame, document, "table", 100)
	assertTableBoxWidth(t, frame, document, "cell", 100)
}

func TestFixedTableNormalizesOversubscribedPercentColumns(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0"><table id=table style="width:400px;table-layout:fixed;border-spacing:0"><col style="width:80%"><col style="width:80%"><tr><td id=first style="height:10px;padding:0"></td><td id=second style="height:10px;padding:0"></td></tr></table></body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 800, Height: 600})
	if err != nil {
		t.Fatal(err)
	}
	assertTableBoxWidth(t, frame, document, "table", 400)
	assertTableBoxWidth(t, frame, document, "first", 200)
	assertTableBoxWidth(t, frame, document, "second", 200)
}

func TestFixedTableTreatsMixedCalcColumnConstraintAsAuto(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0"><table style="width:200px;table-layout:fixed;border-collapse:collapse"><col style="width:calc(20% + 80px)"><tr><td id=first style="height:10px;padding:0"></td><td id=second style="height:10px;padding:0"></td></tr></table></body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 800, Height: 600})
	if err != nil {
		t.Fatal(err)
	}
	assertTableBoxWidth(t, frame, document, "first", 100)
	assertTableBoxWidth(t, frame, document, "second", 100)
}

func TestFixedTableDistributesExcessToPixelColumnsFirst(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0"><table style="width:300px;table-layout:fixed;border-collapse:collapse"><tr><td id=first style="width:20px;height:10px;padding:0"></td><td id=second style="width:10px;height:10px;padding:0"></td><td id=percent style="width:10%;height:10px;padding:0"></td></tr></table></body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 800, Height: 600})
	if err != nil {
		t.Fatal(err)
	}
	assertTableBoxWidth(t, frame, document, "first", 180)
	assertTableBoxWidth(t, frame, document, "second", 90)
	assertTableBoxWidth(t, frame, document, "percent", 30)
}

func TestFixedTablePercentageCellIgnoresPaddingDuringTrackSizing(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0"><table style="width:200px;table-layout:fixed;border-collapse:collapse"><tr><td id=percent style="width:50%;height:10px;padding-left:50px;padding-right:50px"></td><td id=auto style="height:10px;padding:0"></td></tr></table></body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 800, Height: 600})
	if err != nil {
		t.Fatal(err)
	}
	assertTableBoxWidth(t, frame, document, "percent", 100)
	assertTableBoxWidth(t, frame, document, "auto", 100)
}

func TestFractionalTablePercentageIsNotRoundedAway(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0"><table style="width:400px;border-spacing:0"><tr><td id=fraction style="width:.5%;height:10px;padding:0"></td><td id=remainder style="height:10px;padding:0"></td></tr></table></body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 800, Height: 600})
	if err != nil {
		t.Fatal(err)
	}
	assertTableBoxWidth(t, frame, document, "fraction", 2)
	assertTableBoxWidth(t, frame, document, "remainder", 398)
}

func TestInlineAutomaticTablePercentageConstraintTracksContainingWidth(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0"><div style="width:1000px"><div style="width:10%"><div id=table style="display:inline-table;border-spacing:0"><div style="display:table-row"><div id=percent style="display:table-cell;width:100%;padding:0"><span style="display:inline-block;width:100%;height:10px"></span></div><div id=fixed style="display:table-cell;padding:0"><span style="display:inline-block;width:10px;height:10px"></span></div></div></div></div></div></body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 1200, Height: 600})
	if err != nil {
		t.Fatal(err)
	}
	assertTableBoxWidth(t, frame, document, "table", 100)
	assertTableBoxWidth(t, frame, document, "percent", 90)
	assertTableBoxWidth(t, frame, document, "fixed", 10)
}

func assertTableBoxWidth(t *testing.T, frame *render.Frame, document *dom.Node, id string, want float64) {
	t.Helper()
	box := findBox(frame.Root, tableElementByID(t, document, id))
	if box == nil {
		t.Fatalf("table box %q missing", id)
	}
	assertNear(t, id+" width", box.Bounds.Width, want)
}
