package render_test

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestTableHeightDistributionUsesBaseAndReferenceRowSizes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		rows   string
		first  float64
		second float64
	}{
		{
			name:   "percentage before auto",
			rows:   `<tr id=first style="height:25%"><td style="padding:0"></td></tr><tr id=second><td style="padding:0"></td></tr>`,
			first:  25,
			second: 75,
		},
		{
			name:   "oversubscribed percentages interpolate",
			rows:   `<tr id=first style="height:80%"><td style="padding:0"></td></tr><tr id=second style="height:80%"><td style="padding:0"></td></tr>`,
			first:  50,
			second: 50,
		},
		{
			name:   "pixel before auto",
			rows:   `<tr id=first style="height:40px"><td style="padding:0"></td></tr><tr id=second><td style="padding:0"></td></tr>`,
			first:  40,
			second: 60,
		},
		{
			name:   "no auto rows share excess",
			rows:   `<tr id=first style="height:20px"><td style="padding:0"></td></tr><tr id=second style="height:20px"><td style="padding:0"></td></tr>`,
			first:  50,
			second: 50,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0"><table id=table style="height:100px;width:100px;border-spacing:0">`+test.rows+`</table></body></html>`)
			frame, err := render.Render(document, render.Viewport{Width: 200, Height: 160})
			if err != nil {
				t.Fatal(err)
			}
			assertTableBoxHeight(t, frame, document, "table", 100)
			assertTableBoxHeight(t, frame, document, "first", test.first)
			assertTableBoxHeight(t, frame, document, "second", test.second)
		})
	}
}

func TestTableRowMinimumUsesNaturalAndSpecifiedCellHeights(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0"><table style="border-spacing:0">
		<tr id=natural><td style="height:20px;padding:0"><div style="height:40px"></div></td></tr>
		<tr id=specified><td style="height:60px;padding:0"><div style="height:20px"></div></td></tr>
		<tr id=span-a><td rowspan=2 style="height:100px;padding:0"></td><td style="padding:0"></td></tr>
		<tr id=span-b><td style="padding:0"></td></tr>
	</table></body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 200, Height: 260})
	if err != nil {
		t.Fatal(err)
	}
	assertTableBoxHeight(t, frame, document, "natural", 40)
	assertTableBoxHeight(t, frame, document, "specified", 60)
	assertTableBoxHeight(t, frame, document, "span-a", 50)
	assertTableBoxHeight(t, frame, document, "span-b", 50)
}

func TestTableCellPercentageContributesToSecondPassRowReference(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0"><table style="height:100px;border-spacing:0"><tr id=first style="height:25%"><td style="height:100%;padding:0"><div style="height:10px"></div></td></tr><tr id=second><td style="padding:0"><div style="height:10px"></div></td></tr></table></body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 200, Height: 160})
	if err != nil {
		t.Fatal(err)
	}
	assertTableBoxHeight(t, frame, document, "first", 90)
	assertTableBoxHeight(t, frame, document, "second", 10)
}

func TestTableRowGroupHeightIsIgnoredButGroupReceivesDistributedRows(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0">
		<table style="height:100px;border-spacing:0"><thead id=distributed><tr id=distributed-row><td style="padding:0"></td></tr></thead></table>
		<table style="border-spacing:0"><tbody id=ignored style="height:200px"><tr id=ignored-row><td style="padding:0"><div style="height:10px"></div></td></tr></tbody></table>
	</body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 200, Height: 180})
	if err != nil {
		t.Fatal(err)
	}
	assertTableBoxHeight(t, frame, document, "distributed", 100)
	assertTableBoxHeight(t, frame, document, "distributed-row", 100)
	assertTableBoxHeight(t, frame, document, "ignored", 10)
	assertTableBoxHeight(t, frame, document, "ignored-row", 10)
}

func TestTablePercentageCellChildrenUseFinalCellHeightSecondPass(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		tableStyle string
		cellStyle  string
		sibling    string
		childStyle string
		wantTable  float64
		wantCell   float64
		wantChild  float64
	}{
		{
			name:       "definite table height",
			tableStyle: "height:100px",
			childStyle: "height:100%",
			wantTable:  100, wantCell: 100, wantChild: 100,
		},
		{
			name:       "definite cell height",
			cellStyle:  "height:50px",
			childStyle: "height:100%",
			wantTable:  50, wantCell: 50, wantChild: 50,
		},
		{
			name:       "percentage table behaving as auto still opts in",
			tableStyle: "height:100%",
			sibling:    `<td style="height:40px;padding:0"></td>`,
			childStyle: "height:100%",
			wantTable:  40, wantCell: 40, wantChild: 40,
		},
		{
			name:       "percentage cell with auto table stays intrinsic",
			cellStyle:  "height:100%",
			sibling:    `<td style="height:50px;padding:0"></td>`,
			childStyle: "height:100%;min-height:10px",
			wantTable:  50, wantCell: 50, wantChild: 10,
		},
		{
			name:       "second pass may overflow the cell",
			tableStyle: "height:100px",
			childStyle: "height:200%",
			wantTable:  100, wantCell: 100, wantChild: 200,
		},
		{
			name:       "cell padding is excluded from percentage base",
			tableStyle: "height:120px",
			cellStyle:  "padding:10px 0",
			childStyle: "height:100%",
			wantTable:  120, wantCell: 120, wantChild: 100,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			cellStyle := "padding:0;" + test.cellStyle
			document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0"><table id=table style="width:100px;border-spacing:0;`+test.tableStyle+`"><tr><td id=cell style="`+cellStyle+`"><div id=child style="`+test.childStyle+`"></div></td>`+test.sibling+`</tr></table></body></html>`)
			frame, err := render.Render(document, render.Viewport{Width: 200, Height: 200})
			if err != nil {
				t.Fatal(err)
			}
			assertTableBoxHeight(t, frame, document, "table", test.wantTable)
			assertTableBoxHeight(t, frame, document, "cell", test.wantCell)
			assertTableBoxHeight(t, frame, document, "child", test.wantChild)
		})
	}
}

func TestTablePercentageChildSecondPassUsesRowHeightAfterSpacing(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0"><table id=table style="width:100px;height:120px;border-spacing:0 10px"><tr><td id=cell style="padding:0"><div id=child style="height:100%"></div></td></tr></table></body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 200, Height: 180})
	if err != nil {
		t.Fatal(err)
	}
	assertTableBoxHeight(t, frame, document, "table", 120)
	assertTableBoxHeight(t, frame, document, "cell", 100)
	assertTableBoxHeight(t, frame, document, "child", 100)
}

func TestTableFirstPassTreatsScrollablePercentageChildAsZeroBeforeMinHeight(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0">
		<table style="border-spacing:0"><tr><td id=scroll-cell style="height:100%;padding:0"><div id=scroll style="height:100%;min-height:100px;overflow:auto"><div id=scroll-overflow style="height:200px"></div></div></td></tr></table>
		<table style="border-spacing:0"><tr><td id=hidden-cell style="height:100%;padding:0"><div id=hidden style="height:100%;min-height:100px;overflow:hidden"><div style="height:200px"></div></div></td></tr></table>
		<table style="border-spacing:0"><tr><td id=sibling-cell style="height:100%;padding:0"><div id=sibling-child style="height:100%;min-height:100px;overflow:auto"><div style="height:200px"></div></div></td><td style="height:150px;padding:0"></td></tr></table>
		<table style="border-spacing:0"><tr><td id=atomic-cell style="height:100%;padding:0"><span id=atomic style="display:inline-block;height:100%;min-height:100px;overflow:auto;vertical-align:top"><span style="display:block;height:200px"></span></span></td></tr></table>
	</body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 300, Height: 500})
	if err != nil {
		t.Fatal(err)
	}
	assertTableBoxHeight(t, frame, document, "scroll-cell", 100)
	assertTableBoxHeight(t, frame, document, "scroll", 100)
	assertTableBoxHeight(t, frame, document, "scroll-overflow", 200)
	assertTableBoxHeight(t, frame, document, "hidden-cell", 200)
	assertTableBoxHeight(t, frame, document, "hidden", 200)
	assertTableBoxHeight(t, frame, document, "sibling-cell", 150)
	assertTableBoxHeight(t, frame, document, "sibling-child", 100)
	assertTableBoxHeight(t, frame, document, "atomic-cell", 100)
	assertTableBoxHeight(t, frame, document, "atomic", 100)
}

func assertTableBoxHeight(t *testing.T, frame *render.Frame, document *dom.Node, id string, want float64) {
	t.Helper()
	box := findBox(frame.Root, tableElementByID(t, document, id))
	if box == nil {
		t.Fatalf("table box %q missing", id)
	}
	assertNear(t, id+" height", box.Bounds.Height, want)
}
