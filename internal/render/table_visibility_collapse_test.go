package render_test

import (
	"image/color"
	"testing"

	"github.com/JediWattson/gossamer/internal/render"
)

func TestCollapsedTableColumnsRemoveUsedTracksButKeepSizingConstraints(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0">
		<table id=table style="width:340px;table-layout:fixed;border-spacing:10px 5px">
			<col id=first-col style="width:100px">
			<colgroup id=collapsed-group style="visibility:collapse"><col id=collapsed-col style="visibility:visible;width:80px"></colgroup>
			<col id=last-col style="width:120px">
			<tr id=row><td id=first style="height:20px;padding:0"></td><td id=collapsed style="height:20px;padding:0"></td><td id=last style="height:20px;padding:0"></td></tr>
		</table>
	</body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 400, Height: 120})
	if err != nil {
		t.Fatal(err)
	}
	box := func(id string) *render.Box {
		return findBox(frame.Root, tableElementByID(t, document, id))
	}
	table, first, collapsed, last := box("table"), box("first"), box("collapsed"), box("last")
	group, collapsedColumn, lastColumn := box("collapsed-group"), box("collapsed-col"), box("last-col")
	if table == nil || first == nil || collapsed == nil || last == nil || group == nil || collapsedColumn == nil || lastColumn == nil {
		t.Fatalf("collapsed column boxes are missing: table=%#v first=%#v collapsed=%#v last=%#v group=%#v col=%#v lastCol=%#v", table, first, collapsed, last, group, collapsedColumn, lastColumn)
	}
	// All three tracks first receive their fixed 100/80/120px sizing. The
	// middle track and its two obsolete spacing edges are then suppressed, so
	// the used table is 100 + 120 + three visible-edge spacings.
	assertNear(t, "collapsed-column table width", table.ContentBounds.Width, 250)
	assertNear(t, "leading spacing", first.Bounds.X-table.ContentBounds.X, 10)
	assertNear(t, "first column width", first.Bounds.Width, 100)
	assertNear(t, "collapsed cell width", collapsed.Bounds.Width, 0)
	assertNear(t, "collapsed group width", group.Bounds.Width, 0)
	assertNear(t, "collapsed column width", collapsedColumn.Bounds.Width, 0)
	assertNear(t, "visible neighbor gap", last.Bounds.X-(first.Bounds.X+first.Bounds.Width), 10)
	assertNear(t, "last cell width", last.Bounds.Width, 120)
	assertNear(t, "last column x", lastColumn.Bounds.X, last.Bounds.X)
	assertNear(t, "table height unchanged by column collapse", table.ContentBounds.Height, 30)
}

func TestCollapsedTableRowsAndGroupsRemoveUsedHeightAndSpacing(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0">
		<table id=table style="width:100px;table-layout:fixed;border-spacing:0 5px">
			<tbody><tr id=first-row><td style="height:20px;padding:0"></td></tr></tbody>
			<tbody id=collapsed-group style="visibility:collapse">
				<tr id=collapsed-row-a style="visibility:visible"><td id=collapsed-a style="height:30px;padding:0"></td></tr>
				<tr id=collapsed-row-b><td style="height:40px;padding:0"></td></tr>
			</tbody>
			<tbody><tr id=last-row><td id=last style="height:50px;padding:0"></td></tr></tbody>
		</table>
	</body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 200, Height: 160})
	if err != nil {
		t.Fatal(err)
	}
	box := func(id string) *render.Box {
		return findBox(frame.Root, tableElementByID(t, document, id))
	}
	table, firstRow, collapsedA, collapsedB, group, lastRow, last :=
		box("table"), box("first-row"), box("collapsed-a"), box("collapsed-row-b"), box("collapsed-group"), box("last-row"), box("last")
	if table == nil || firstRow == nil || collapsedA == nil || collapsedB == nil || group == nil || lastRow == nil || last == nil {
		t.Fatalf("collapsed row boxes are missing: table=%#v first=%#v a=%#v b=%#v group=%#v lastRow=%#v last=%#v", table, firstRow, collapsedA, collapsedB, group, lastRow, last)
	}
	assertNear(t, "collapsed-row table height", table.ContentBounds.Height, 85)
	assertNear(t, "first row y", firstRow.Bounds.Y-table.ContentBounds.Y, 5)
	assertNear(t, "first row height", firstRow.Bounds.Height, 20)
	assertNear(t, "visible override cannot escape collapsed group", collapsedA.Bounds.Height, 0)
	assertNear(t, "inherited collapsed row height", collapsedB.Bounds.Height, 0)
	assertNear(t, "collapsed group height", group.Bounds.Height, 0)
	assertNear(t, "remaining interrow spacing", lastRow.Bounds.Y-(firstRow.Bounds.Y+firstRow.Bounds.Height), 5)
	assertNear(t, "last row height", lastRow.Bounds.Height, 50)
	assertNear(t, "last cell height", last.Bounds.Height, 50)
}

func TestCollapsedRowsSuppressTheirShareOfSpecifiedTableHeight(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0">
		<table id=table style="width:100px;height:100px;table-layout:fixed;border-spacing:0">
			<tr id=collapsed style="visibility:collapse"><td style="height:20px;padding:0"></td></tr>
			<tr id=visible><td style="height:20px;padding:0"></td></tr>
		</table>
	</body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 160, Height: 140})
	if err != nil {
		t.Fatal(err)
	}
	table := findBox(frame.Root, tableElementByID(t, document, "table"))
	collapsed := findBox(frame.Root, tableElementByID(t, document, "collapsed"))
	visible := findBox(frame.Root, tableElementByID(t, document, "visible"))
	if table == nil || collapsed == nil || visible == nil {
		t.Fatalf("specified-height boxes are missing: table=%#v collapsed=%#v visible=%#v", table, collapsed, visible)
	}
	// The 100px minimum table height is distributed across both 20px rows
	// before collapse. Removing one resulting 50px track leaves the other 50px
	// track; the specified height is not reapplied after suppression.
	assertNear(t, "specified-height collapsed row", collapsed.Bounds.Height, 0)
	assertNear(t, "specified-height visible row", visible.Bounds.Height, 50)
	assertNear(t, "specified-height collapsed table", table.ContentBounds.Height, 50)
}

func TestSpanningCellAcrossCollapsedTracksKeepsLayoutAndClipsPaintAndHits(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0">
		<table id=table style="width:200px;table-layout:fixed;border-spacing:0">
			<col style="width:100px;visibility:collapse"><col style="width:100px">
			<tr style="height:40px"><td id=span colspan=2 style="visibility:visible;padding:0;background:#010203"><div id=overflow style="width:200px;height:40px;background:#aabbcc"></div></td></tr>
		</table>
	</body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 260, Height: 100})
	if err != nil {
		t.Fatal(err)
	}
	table := findBox(frame.Root, tableElementByID(t, document, "table"))
	span := findBox(frame.Root, tableElementByID(t, document, "span"))
	overflowNode := tableElementByID(t, document, "overflow")
	overflow := findBox(frame.Root, overflowNode)
	if table == nil || span == nil || overflow == nil {
		t.Fatalf("spanning boxes are missing: table=%#v span=%#v overflow=%#v", table, span, overflow)
	}
	assertNear(t, "collapsed spanning table width", table.ContentBounds.Width, 100)
	assertNear(t, "spanning used border box", span.Bounds.Width, 100)
	assertNear(t, "spanning child retained logical layout", overflow.Bounds.Width, 200)
	command := commandForNodeColor(frame.DisplayList.Commands, overflowNode, color.NRGBA{R: 0xaa, G: 0xbb, B: 0xcc, A: 0xff})
	if command == nil || !command.HasClip {
		t.Fatalf("overflow command lacks collapsed-track clip: %#v", command)
	}
	assertNear(t, "paint clip x", command.Clip.X, span.Bounds.X)
	assertNear(t, "paint clip width", command.Clip.Width, span.Bounds.Width)
	if hit := render.HitTest(frame, span.Bounds.X+50, span.Bounds.Y+10); hit != overflowNode {
		t.Fatalf("visible clipped content hit = %v, want overflow child", hit)
	}
	if hit := render.HitTest(frame, span.Bounds.X+150, span.Bounds.Y+10); hit == overflowNode {
		t.Fatal("content outside a collapsed-track cell clip remained hit-testable")
	}
}

func TestVisibleRowspanCellClipsToRemainingRows(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0">
		<table id=table style="width:100px;table-layout:fixed;border-spacing:0">
			<tr style="visibility:collapse"><td id=span rowspan=2 style="visibility:visible;padding:0"><div id=content style="height:60px;background:#123456"></div></td></tr>
			<tr><td style="height:30px;padding:0"></td></tr>
		</table>
	</body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 160, Height: 100})
	if err != nil {
		t.Fatal(err)
	}
	span := findBox(frame.Root, tableElementByID(t, document, "span"))
	contentNode := tableElementByID(t, document, "content")
	content := findBox(frame.Root, contentNode)
	if span == nil || content == nil {
		t.Fatalf("rowspan boxes are missing: span=%#v content=%#v", span, content)
	}
	if span.Bounds.Height <= 0 || span.Bounds.Height >= content.Bounds.Height {
		t.Fatalf("rowspan used/logical heights = span %.2f content %.2f", span.Bounds.Height, content.Bounds.Height)
	}
	command := commandForNodeColor(frame.DisplayList.Commands, contentNode, color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff})
	if command == nil || !command.HasClip {
		t.Fatalf("rowspan command lacks collapsed-row clip: %#v", command)
	}
	assertNear(t, "rowspan clip height", command.Clip.Height, span.Bounds.Height)
}

func TestCollapsedTracksCompressCollapsedBorderGeometry(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0">
		<table id=table style="width:200px;table-layout:fixed;border-collapse:collapse;border:4px solid #111111">
			<col style="width:100px;visibility:collapse"><col style="width:100px">
			<tr><td id=hidden style="height:30px;border:6px solid #223344"></td><td id=visible style="height:30px;border:8px solid #556677"></td></tr>
		</table>
	</body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 260, Height: 100})
	if err != nil {
		t.Fatal(err)
	}
	table := findBox(frame.Root, tableElementByID(t, document, "table"))
	hidden := findBox(frame.Root, tableElementByID(t, document, "hidden"))
	visible := findBox(frame.Root, tableElementByID(t, document, "visible"))
	if table == nil || hidden == nil || visible == nil {
		t.Fatalf("collapsed-border boxes are missing: table=%#v hidden=%#v visible=%#v", table, hidden, visible)
	}
	assertNear(t, "collapsed-border hidden width", hidden.Bounds.Width, 0)
	assertNear(t, "collapsed-border visible x", visible.Bounds.X, table.ContentBounds.X)
	if table.ContentBounds.Width <= 0 || table.ContentBounds.Width >= 200 {
		t.Fatalf("collapsed-border table content width = %.2f, want one visible track", table.ContentBounds.Width)
	}
	tableNode := tableElementByID(t, document, "table")
	hiddenNode := tableElementByID(t, document, "hidden")
	visibleNode := tableElementByID(t, document, "visible")
	for _, command := range frame.DisplayList.Commands {
		if command.Kind != render.FillRectCommand || command.Rect.Width < 0 || command.Rect.Height < 0 ||
			command.Node != tableNode && command.Node != hiddenNode && command.Node != visibleNode {
			continue
		}
		// Collapsed borders are centered on the compressed grid edge, so at
		// most half of the widest 8px winner may extend beyond the table box.
		if command.Rect.X < table.Bounds.X-4.01 || command.Rect.X+command.Rect.Width > table.Bounds.X+table.Bounds.Width+4.01 {
			t.Fatalf("collapsed border escaped compressed table: command=%#v table=%#v", command, table.Bounds)
		}
	}
}
