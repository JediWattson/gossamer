package render_test

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/render"
)

func TestAutomaticTableMergesAnonymousColspanTracks(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0"><table id=table style="border:10px solid #808080;border-spacing:20px"><tr><td id=wide colspan=10 style="width:50px;height:50px;padding:0"></td><td id=last style="width:50px;height:50px;padding:0"></td></tr><tr><td colspan=10 style="width:50px;height:50px;padding:0"></td><td style="width:50px;height:50px;padding:0"></td></tr></table></body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 800, Height: 600})
	if err != nil {
		t.Fatal(err)
	}
	table := findBox(frame.Root, tableElementByID(t, document, "table"))
	wide := findBox(frame.Root, tableElementByID(t, document, "wide"))
	last := findBox(frame.Root, tableElementByID(t, document, "last"))
	if table == nil || wide == nil || last == nil {
		t.Fatalf("track-merging boxes = table:%#v wide:%#v last:%#v", table, wide, last)
	}
	assertNear(t, "WPT automatic merged table width", table.Bounds.Width, 180)
	assertNear(t, "merged colspan cell width", wide.Bounds.Width, 50)
	assertNear(t, "remaining cell x", last.Bounds.X-wide.Bounds.X, 70)
}

func TestExplicitRowsAndColumnsReceiveAnonymousMissingCells(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0"><table id=table style="display:table;border-spacing:25px"><col style="width:50px"><col style="width:50px"><tr id=first-row style="height:50px"></tr><tr id=second-row style="height:50px"></tr></table></body></html>`)
	firstRow := tableElementByID(t, document, "first-row")
	secondRow := tableElementByID(t, document, "second-row")
	if len(firstRow.Children) != 0 || len(secondRow.Children) != 0 {
		t.Fatal("missing-cell fixture unexpectedly has DOM cells")
	}
	frame, err := render.Render(document, render.Viewport{Width: 400, Height: 300})
	if err != nil {
		t.Fatal(err)
	}
	table := findBox(frame.Root, tableElementByID(t, document, "table"))
	if table == nil {
		t.Fatal("table box missing")
	}
	assertNear(t, "WPT explicit missing-cell width", table.Bounds.Width, 175)
	assertNear(t, "WPT explicit missing-cell height", table.Bounds.Height, 175)
	if count := anonymousLeafTableCellCount(table); count != 4 {
		t.Fatalf("anonymous missing cell boxes = %d, want 4", count)
	}
	if len(firstRow.Children) != 0 || len(secondRow.Children) != 0 {
		t.Fatal("missing-cell fixup mutated the DOM")
	}
}

func anonymousLeafTableCellCount(box *render.Box) int {
	if box == nil {
		return 0
	}
	count := 0
	if box.Node == nil && len(box.Children) == 0 && box.Bounds.Width > 0 && box.Bounds.Height > 0 {
		count = 1
	}
	for _, child := range box.Children {
		count += anonymousLeafTableCellCount(child)
	}
	return count
}
