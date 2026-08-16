package render_test

import (
	"image/color"
	"math"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestSeparatedTableSpacingContributesOuterAndIntercellGaps(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><head><style>
		body { margin:0 }
		table { border-spacing:10px 6px; background:#aa0000 }
		tr { background:#0000aa }
		td { height:20px; padding:0 }
	</style></head><body><table id=table><col style="width:40px"><col style="width:60px">
		<tr id=first><td id=a>A</td><td id=b>B</td></tr>
		<tr id=second><td>C</td><td>D</td></tr>
	</table></body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 300, Height: 200})
	if err != nil {
		t.Fatal(err)
	}
	table := findBox(frame.Root, tableElementByID(t, document, "table"))
	first := findBox(frame.Root, tableElementByID(t, document, "first"))
	second := findBox(frame.Root, tableElementByID(t, document, "second"))
	a := findBox(frame.Root, tableElementByID(t, document, "a"))
	b := findBox(frame.Root, tableElementByID(t, document, "b"))
	if table == nil || first == nil || second == nil || a == nil || b == nil {
		t.Fatalf("spacing boxes = table:%#v first:%#v second:%#v a:%#v b:%#v", table, first, second, a, b)
	}
	assertNear(t, "spacing table width", table.ContentBounds.Width, 130)
	assertNear(t, "leading horizontal spacing", a.Bounds.X-table.ContentBounds.X, 10)
	assertNear(t, "intercell horizontal spacing", b.Bounds.X-(a.Bounds.X+a.Bounds.Width), 10)
	assertNear(t, "trailing horizontal spacing", table.ContentBounds.X+table.ContentBounds.Width-(b.Bounds.X+b.Bounds.Width), 10)
	assertNear(t, "leading vertical spacing", first.Bounds.Y-table.ContentBounds.Y, 6)
	assertNear(t, "interrow vertical spacing", second.Bounds.Y-(first.Bounds.Y+first.Bounds.Height), 6)
	assertNear(t, "trailing vertical spacing", table.ContentBounds.Y+table.ContentBounds.Height-(second.Bounds.Y+second.Bounds.Height), 6)

	gapX := a.Bounds.X + a.Bounds.Width + 5
	gapY := first.Bounds.Y + first.Bounds.Height/2
	for _, command := range frame.DisplayList.Commands {
		if command.Kind != render.FillRectCommand || command.Color != (color.NRGBA{B: 0xaa, A: 0xff}) {
			continue
		}
		if pointInRect(gapX, gapY, command.Rect) {
			t.Fatalf("row background %#v paints through horizontal border spacing at %.1f,%.1f", command.Rect, gapX, gapY)
		}
	}
}

func TestFixedTableLayoutUsesColumnsThenFirstRowThenRemainder(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0">
		<table id=table style="width:300px;table-layout:fixed;border-spacing:10px 0">
			<col style="width:60px"><col><col>
			<tr><td id=first>A</td><td id=second style="width:90px">B</td><td id=third>C</td></tr>
			<tr><td>short</td><td style="width:400px">later row must not size the track</td><td>D</td></tr>
		</table>
	</body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 400, Height: 200})
	if err != nil {
		t.Fatal(err)
	}
	table := findBox(frame.Root, tableElementByID(t, document, "table"))
	first := findBox(frame.Root, tableElementByID(t, document, "first"))
	second := findBox(frame.Root, tableElementByID(t, document, "second"))
	third := findBox(frame.Root, tableElementByID(t, document, "third"))
	if table == nil || first == nil || second == nil || third == nil {
		t.Fatalf("fixed boxes = table:%#v first:%#v second:%#v third:%#v", table, first, second, third)
	}
	assertNear(t, "fixed table width", table.ContentBounds.Width, 300)
	assertNear(t, "fixed explicit column", first.Bounds.Width, 60)
	assertNear(t, "fixed first-row cell", second.Bounds.Width, 90)
	assertNear(t, "fixed remaining column", third.Bounds.Width, 110)
	assertNear(t, "fixed first gap", second.Bounds.X-(first.Bounds.X+first.Bounds.Width), 10)
	assertNear(t, "fixed second gap", third.Bounds.X-(second.Bounds.X+second.Bounds.Width), 10)
}

func TestCaptionSideBottomMovesCaptionOutsideTableGridPaint(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0">
		<table id=table style="border-spacing:0;background:#123456">
			<caption id=caption style="caption-side:bottom;height:20px;background:#abcdef">Bottom</caption>
			<tr><td id=cell style="height:30px;padding:0">Cell</td></tr>
		</table>
	</body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 300, Height: 200})
	if err != nil {
		t.Fatal(err)
	}
	tableNode := tableElementByID(t, document, "table")
	table := findBox(frame.Root, tableNode)
	caption := findBox(frame.Root, tableElementByID(t, document, "caption"))
	cell := findBox(frame.Root, tableElementByID(t, document, "cell"))
	if table == nil || caption == nil || cell == nil {
		t.Fatalf("caption boxes = table:%#v caption:%#v cell:%#v", table, caption, cell)
	}
	if caption.Bounds.Y < cell.Bounds.Y+cell.Bounds.Height {
		t.Fatalf("bottom caption y = %.1f, want at or below grid bottom %.1f", caption.Bounds.Y, cell.Bounds.Y+cell.Bounds.Height)
	}
	tableBackground := commandForNodeColor(frame.DisplayList.Commands, tableNode, color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff})
	if tableBackground == nil {
		t.Fatal("table background command missing")
	}
	if tableBackground.Rect.Y+tableBackground.Rect.Height > caption.Bounds.Y {
		t.Fatalf("table grid background %#v extends into bottom caption %#v", tableBackground.Rect, caption.Bounds)
	}
}

func TestEmptyCellsHideOnlySuppressesSeparatedEmptyCellDecorations(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><head><style>
		body { margin:0 }
		table { border-spacing:0; empty-cells:hide }
		td { width:30px; height:20px; padding:0; background:#112233; border:2px solid #445566 }
	</style></head><body><table>
		<tr><td id=empty></td><td id=collapsed>   </td><td id=element><span></span></td><td id=preserved style="white-space:pre"> </td></tr>
	</table></body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 300, Height: 100})
	if err != nil {
		t.Fatal(err)
	}
	background := color.NRGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xff}
	for _, id := range []string{"empty", "collapsed"} {
		node := tableElementByID(t, document, id)
		if commandForNodeColor(frame.DisplayList.Commands, node, background) != nil {
			t.Errorf("empty separated cell #%s painted its background", id)
		}
	}
	for _, id := range []string{"element", "preserved"} {
		node := tableElementByID(t, document, id)
		if commandForNodeColor(frame.DisplayList.Commands, node, background) == nil {
			t.Errorf("non-empty separated cell #%s did not paint its background", id)
		}
	}

	collapsed := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0"><table style="border-collapse:collapse;empty-cells:hide"><tr><td id=cell style="width:20px;height:20px;background:#112233"></td></tr></table></body></html>`)
	collapsedFrame, err := render.Render(collapsed, render.Viewport{Width: 100, Height: 100})
	if err != nil {
		t.Fatal(err)
	}
	if commandForNodeColor(collapsedFrame.DisplayList.Commands, tableElementByID(t, collapsed, "cell"), background) == nil {
		t.Error("empty-cells:hide affected collapsed-border mode")
	}
}

func TestTableCellVerticalAlignmentMovesContentInsideStretchedRows(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0">
		<table style="border-spacing:0;font:10px/10px monospace"><tr style="height:100px">
			<td style="padding:0;vertical-align:top">top</td>
			<td style="padding:0;vertical-align:middle">middle</td>
			<td style="padding:0;vertical-align:bottom">bottom</td>
			<td style="padding:0;vertical-align:baseline">base-a</td>
			<td style="padding:20px 0 0;vertical-align:baseline">base-b</td>
		</tr></table>
	</body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 400, Height: 160})
	if err != nil {
		t.Fatal(err)
	}
	fragments := collectTextFragments(frame.Root)
	top := findTextFragment(fragments, "top")
	middle := findTextFragment(fragments, "middle")
	bottom := findTextFragment(fragments, "bottom")
	baseA := findTextFragment(fragments, "base-a")
	baseB := findTextFragment(fragments, "base-b")
	if top == nil || middle == nil || bottom == nil || baseA == nil || baseB == nil {
		t.Fatalf("aligned fragments = top:%#v middle:%#v bottom:%#v base-a:%#v base-b:%#v", top, middle, bottom, baseA, baseB)
	}
	if !(top.BaselineY < middle.BaselineY && middle.BaselineY < bottom.BaselineY) {
		t.Fatalf("top/middle/bottom baselines = %.1f/%.1f/%.1f", top.BaselineY, middle.BaselineY, bottom.BaselineY)
	}
	assertNear(t, "table-cell shared baseline", baseA.BaselineY, baseB.BaselineY)
}

func TestCollapsedBordersHarmonizeConflictsAndUseHalfWidthInsets(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><head><style>
		body { margin:0 }
		table { border-collapse:collapse; border-spacing:20px; padding:10px; border:4px solid #aaaa00 }
		col:first-child { border-right:6px solid #000000 }
		tr:first-child { border-bottom:8px solid #0000aa }
		td { width:40px; height:20px; padding:0; border:2px solid #aa0000 }
		#right { border-left:10px solid #00aa00 }
	</style></head><body><table id=table><col><col>
		<tr><td id=left>A</td><td id=right>B</td></tr>
		<tr><td id=lower-left>C</td><td id=lower-right>D</td></tr>
	</table></body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 300, Height: 200})
	if err != nil {
		t.Fatal(err)
	}
	table := findBox(frame.Root, tableElementByID(t, document, "table"))
	left := findBox(frame.Root, tableElementByID(t, document, "left"))
	rightNode := tableElementByID(t, document, "right")
	right := findBox(frame.Root, rightNode)
	lowerLeft := findBox(frame.Root, tableElementByID(t, document, "lower-left"))
	if table == nil || left == nil || right == nil || lowerLeft == nil {
		t.Fatalf("collapsed boxes = table:%#v left:%#v right:%#v lower-left:%#v", table, left, right, lowerLeft)
	}
	assertNear(t, "collapsed table left half border", table.ContentBounds.X-table.Bounds.X, 2)
	assertNear(t, "collapsed table top half border", table.ContentBounds.Y-table.Bounds.Y, 2)
	assertNear(t, "collapsed spacing ignored", right.Bounds.X-(left.Bounds.X+left.Bounds.Width), 0)
	assertNear(t, "collapsed cell winning right half", left.Border.Right, 5)
	assertNear(t, "collapsed cell winning left half", right.Border.Left, 5)

	green := commandForNodeColor(frame.DisplayList.Commands, rightNode, color.NRGBA{G: 0xaa, A: 0xff})
	if green == nil {
		t.Fatal("winning 10px cell border was not painted")
	}
	assertNear(t, "winning vertical collapsed border width", green.Rect.Width, 10)
	if mathAbs((green.Rect.X+green.Rect.Width/2)-right.Bounds.X) > 0.001 {
		t.Fatalf("winning vertical border center = %.3f, want cell boundary %.3f", green.Rect.X+green.Rect.Width/2, right.Bounds.X)
	}
	rowBorder := firstCommandWithColor(frame.DisplayList.Commands, color.NRGBA{B: 0xaa, A: 0xff})
	if rowBorder == nil {
		t.Fatal("winning 8px row border was not painted")
	}
	assertNear(t, "winning horizontal collapsed border height", rowBorder.Rect.Height, 8)
	assertNear(t, "winning horizontal border center", rowBorder.Rect.Y+rowBorder.Rect.Height/2, lowerLeft.Bounds.Y)
}

func TestEmptyCollapsedTableKeepsHalfWidthTableBorder(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0">
		<table id=table style="border-collapse:collapse;width:40px;height:20px;border:8px solid #123456"></table>
	</body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 100, Height: 100})
	if err != nil {
		t.Fatal(err)
	}
	tableNode := tableElementByID(t, document, "table")
	table := findBox(frame.Root, tableNode)
	if table == nil {
		t.Fatal("empty collapsed table box missing")
	}
	var tableRoot *render.Box
	for _, child := range table.Children {
		if child.Node == tableNode {
			tableRoot = child
			break
		}
	}
	if tableRoot == nil {
		t.Fatal("empty collapsed table-root box missing")
	}
	assertNear(t, "empty collapsed top half border", tableRoot.Border.Top, 4)
	assertNear(t, "empty collapsed right half border", tableRoot.Border.Right, 4)
	assertNear(t, "empty collapsed bottom half border", tableRoot.Border.Bottom, 4)
	assertNear(t, "empty collapsed left half border", tableRoot.Border.Left, 4)
	if commandForNodeColor(frame.DisplayList.Commands, tableNode, color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff}) == nil {
		t.Fatal("empty collapsed table border was not painted")
	}
}

func TestCollapsedTableBorderStylePrecedenceAndPatternPaint(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0">
		<table style="border-collapse:collapse;border-spacing:0"><tr>
		<td id=double style="width:30px;height:20px;border-right:6px double #aa0000"></td>
		<td id=solid style="width:30px;height:20px;border-left:6px solid #0000aa"></td>
		</tr></table>
	</body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 100, Height: 60})
	if err != nil {
		t.Fatal(err)
	}
	doubleNode := tableElementByID(t, document, "double")
	red := color.NRGBA{R: 0xaa, A: 0xff}
	blue := color.NRGBA{B: 0xaa, A: 0xff}
	redCommands := borderCommandsForNode(frame.DisplayList.Commands, doubleNode)
	if len(redCommands) != 2 || redCommands[0].Color != red || redCommands[1].Color != red ||
		redCommands[0].Rect.Width != 2 || redCommands[1].Rect.Width != 2 {
		t.Fatalf("collapsed double border commands = %#v, want two red 2px lines", redCommands)
	}
	if firstCommandWithColor(frame.DisplayList.Commands, blue) != nil {
		t.Fatal("lower-priority solid collapsed border was painted")
	}
}

func TestCollapsedBorderJunctionUsesWinningPatternWithoutUnderpainting(t *testing.T) {
	t.Parallel()

	green := color.NRGBA{G: 0xaa, A: 0xff}
	red := color.NRGBA{R: 0xaa, A: 0xff}
	white := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	tests := []struct {
		name            string
		verticalBorder  string
		wantCenter      color.NRGBA
		stripeOffset    int
		wantStripeColor color.NRGBA
		wantEllipse     bool
	}{
		{name: "dotted winner owns junction", verticalBorder: "10px dotted #00aa00", wantCenter: green, wantEllipse: true},
		{name: "double gap does not reveal lower border", verticalBorder: "12px double #00aa00", wantCenter: white, stripeOffset: -4, wantStripeColor: green},
		{name: "transparent winner suppresses lower border", verticalBorder: "10px solid transparent", wantCenter: white},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0"><table style="margin-left:17px;border-collapse:collapse"><tr>
				<td id=top-left style="width:40px;height:30px;padding:0;border-right:`+test.verticalBorder+`;border-bottom:4px solid #aa0000"></td>
				<td id=top-right style="width:40px;height:30px;padding:0;border-bottom:4px solid #aa0000"></td>
			</tr><tr><td id=bottom-left style="width:40px;height:30px;padding:0"></td><td style="width:40px;height:30px;padding:0"></td></tr></table></body></html>`)
			frame, err := render.Render(document, render.Viewport{Width: 120, Height: 100})
			if err != nil {
				t.Fatal(err)
			}
			topRight := findBox(frame.Root, tableElementByID(t, document, "top-right"))
			bottomLeft := findBox(frame.Root, tableElementByID(t, document, "bottom-left"))
			if topRight == nil || bottomLeft == nil {
				t.Fatalf("junction cells = top-right:%#v bottom-left:%#v", topRight, bottomLeft)
			}
			crossX, crossY := topRight.Bounds.X, bottomLeft.Bounds.Y
			for _, command := range frame.DisplayList.Commands {
				if (command.Kind == render.FillRectCommand || command.Kind == render.FillEllipseCommand) && command.Color == red && pointInRect(crossX, crossY, command.Rect) {
					t.Fatalf("lower red border was painted below winning junction: %#v", command)
				}
			}
			painted, err := render.Rasterize(frame)
			if err != nil {
				t.Fatal(err)
			}
			centerX, centerY := int(math.Floor(crossX)), int(math.Floor(crossY))
			assertBorderPixel(t, painted, centerX, centerY, test.wantCenter)
			if test.stripeOffset != 0 {
				assertBorderPixel(t, painted, centerX+test.stripeOffset, centerY, test.wantStripeColor)
			}
			if test.wantEllipse {
				found := false
				for _, command := range frame.DisplayList.Commands {
					if command.Kind == render.FillEllipseCommand && command.Color == green &&
						mathAbs(command.Rect.X+command.Rect.Width/2-crossX) <= 0.001 &&
						mathAbs(command.Rect.Y+command.Rect.Height/2-crossY) <= 0.001 &&
						command.Rect.Width == 10 && command.Rect.Height == 10 {
						found = true
						break
					}
				}
				if !found {
					t.Fatal("dotted junction ellipse was not retained at the crossing")
				}
			}
		})
	}
}

func TestCollapsedHiddenBorderSuppressesOtherwiseVisibleConflict(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0"><table style="border-collapse:collapse"><tr>
		<td id=left style="width:30px;height:20px;padding:0;border-right:10px hidden #ff0000">A</td>
		<td id=right style="width:30px;height:20px;padding:0;border-left:20px solid #00ff00">B</td>
	</tr></table></body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 120, Height: 80})
	if err != nil {
		t.Fatal(err)
	}
	left := findBox(frame.Root, tableElementByID(t, document, "left"))
	right := findBox(frame.Root, tableElementByID(t, document, "right"))
	if left == nil || right == nil {
		t.Fatalf("hidden-border cells = left:%#v right:%#v", left, right)
	}
	boundary := right.Bounds.X
	for _, command := range frame.DisplayList.Commands {
		if command.Kind != render.FillRectCommand || command.Rect.Height <= 0 {
			continue
		}
		if command.Rect.X < boundary && command.Rect.X+command.Rect.Width > boundary &&
			(command.Color == (color.NRGBA{G: 0xff, A: 0xff}) || command.Color == (color.NRGBA{R: 0xff, A: 0xff})) {
			t.Fatalf("hidden collapsed conflict painted %#v", command)
		}
	}
}

func TestCollapsedEqualWidthConflictPrefersCellThenTopCell(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><head><style>
		body { margin:0 }
		table { border-collapse:collapse }
		tr:first-child { border-bottom:5px solid #0000ff }
		#top { width:40px; height:20px; padding:0; border-bottom:5px solid #ff0000 }
		#bottom { width:40px; height:20px; padding:0; border-top:5px solid #00ff00 }
	</style></head><body><table><tr><td id=top>A</td></tr><tr><td id=bottom>B</td></tr></table></body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 100, Height: 100})
	if err != nil {
		t.Fatal(err)
	}
	red := commandForNodeColor(frame.DisplayList.Commands, tableElementByID(t, document, "top"), color.NRGBA{R: 0xff, A: 0xff})
	if red == nil || red.Rect.Height != 5 {
		t.Fatalf("top cell winning border = %#v, want 5px red", red)
	}
	if firstCommandWithColor(frame.DisplayList.Commands, color.NRGBA{G: 0xff, A: 0xff}) != nil ||
		firstCommandWithColor(frame.DisplayList.Commands, color.NRGBA{B: 0xff, A: 0xff}) != nil {
		t.Fatal("lower-priority row or lower cell border survived equal-width conflict")
	}
}

func TestTableDirectionMirrorsColumnsAndKeepsColumnStartConflictWinner(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name         string
		direction    string
		firstBorder  string
		secondBorder string
		firstIsRight bool
	}{
		{
			name: "ltr", direction: "ltr",
			firstBorder: "border-right:6px solid #ff0000", secondBorder: "border-left:6px solid #00ff00",
		},
		{
			name: "rtl", direction: "rtl", firstIsRight: true,
			firstBorder: "border-left:6px solid #ff0000", secondBorder: "border-right:6px solid #00ff00",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0">
				<table id=table style="direction:`+test.direction+`;border-collapse:collapse;table-layout:fixed;width:120px">
					<tr><td id=first style="height:20px;padding:0;`+test.firstBorder+`">first</td>
					<td id=second style="height:20px;padding:0;`+test.secondBorder+`">second</td></tr>
				</table></body></html>`)
			frame, err := render.Render(document, render.Viewport{Width: 200, Height: 80})
			if err != nil {
				t.Fatal(err)
			}
			firstNode := tableElementByID(t, document, "first")
			secondNode := tableElementByID(t, document, "second")
			first := findBox(frame.Root, firstNode)
			second := findBox(frame.Root, secondNode)
			if first == nil || second == nil {
				t.Fatalf("directional cells = first:%#v second:%#v", first, second)
			}
			if got := first.Bounds.X > second.Bounds.X; got != test.firstIsRight {
				t.Fatalf("first cell right of second = %t, want %t; first=%#v second=%#v", got, test.firstIsRight, first.Bounds, second.Bounds)
			}
			winner := commandForNodeColor(frame.DisplayList.Commands, firstNode, color.NRGBA{R: 0xff, A: 0xff})
			if winner == nil || winner.Rect.Width != 6 {
				t.Fatalf("column-start collapsed winner = %#v, want first cell 6px red", winner)
			}
			if firstCommandWithColor(frame.DisplayList.Commands, color.NRGBA{G: 0xff, A: 0xff}) != nil {
				t.Fatal("column-end cell survived exact collapsed conflict")
			}
			boundary := math.Max(first.Bounds.X, second.Bounds.X)
			assertNear(t, "directional collapsed boundary", winner.Rect.X+winner.Rect.Width/2, boundary)
		})
	}
}

func TestRTLTableMirrorsColumnBoxesAndSpanningCells(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0">
		<table style="direction:rtl;table-layout:fixed;width:180px;border-spacing:5px 0">
			<colgroup id=group><col id=col0 style="width:40px"><col id=col1 style="width:50px"></colgroup>
			<col id=col2 style="width:60px">
			<tr><td id=span colspan=2>span</td><td id=last>last</td></tr>
		</table>
	</body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 240, Height: 80})
	if err != nil {
		t.Fatal(err)
	}
	box := func(id string) *render.Box {
		return findBox(frame.Root, tableElementByID(t, document, id))
	}
	group, col0, col1, col2 := box("group"), box("col0"), box("col1"), box("col2")
	span, last := box("span"), box("last")
	if group == nil || col0 == nil || col1 == nil || col2 == nil || span == nil || last == nil {
		t.Fatalf("rtl table boxes = group:%#v col0:%#v col1:%#v col2:%#v span:%#v last:%#v", group, col0, col1, col2, span, last)
	}
	if !(col0.Bounds.X > col1.Bounds.X && col1.Bounds.X > col2.Bounds.X) {
		t.Fatalf("rtl column order = col0:%#v col1:%#v col2:%#v", col0.Bounds, col1.Bounds, col2.Bounds)
	}
	assertNear(t, "rtl group start", group.Bounds.X, col1.Bounds.X)
	assertNear(t, "rtl group width", group.Bounds.Width, col0.Bounds.X+col0.Bounds.Width-col1.Bounds.X)
	assertNear(t, "rtl spanning start", span.Bounds.X, col1.Bounds.X)
	assertNear(t, "rtl spanning width", span.Bounds.Width, group.Bounds.Width)
	if span.Bounds.X <= last.Bounds.X {
		t.Fatalf("rtl spanning cell %#v is not right of final logical cell %#v", span.Bounds, last.Bounds)
	}
}

func TestRTLCollapsedBordersKeepPhysicalLeftAndRightInsets(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0">
		<table id=table style="direction:rtl;border-collapse:collapse;border-left:8px solid #ff0000;border-right:4px solid #0000ff">
			<tr><td id=cell style="width:40px;height:20px;padding:0;border-left:2px solid #00ff00;border-right:6px solid #ffff00"></td></tr>
		</table>
	</body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 100, Height: 60})
	if err != nil {
		t.Fatal(err)
	}
	tableNode := tableElementByID(t, document, "table")
	wrapper := findBox(frame.Root, tableNode)
	cell := findBox(frame.Root, tableElementByID(t, document, "cell"))
	if wrapper == nil || cell == nil {
		t.Fatalf("rtl collapsed boxes = wrapper:%#v cell:%#v", wrapper, cell)
	}
	var root *render.Box
	for _, child := range wrapper.Children {
		if child.Node == tableNode {
			root = child
			break
		}
	}
	if root == nil {
		t.Fatal("rtl table-root box missing")
	}
	assertNear(t, "rtl root physical left half", root.Border.Left, 4)
	assertNear(t, "rtl root physical right half", root.Border.Right, 3)
	assertNear(t, "rtl cell physical left half", cell.Border.Left, 4)
	assertNear(t, "rtl cell physical right half", cell.Border.Right, 3)
	if commandForNodeColor(frame.DisplayList.Commands, tableNode, color.NRGBA{R: 0xff, A: 0xff}) == nil {
		t.Fatal("physical left table border did not win")
	}
	if commandForNodeColor(frame.DisplayList.Commands, tableElementByID(t, document, "cell"), color.NRGBA{R: 0xff, G: 0xff, A: 0xff}) == nil {
		t.Fatal("physical right cell border did not win")
	}
}

func pointInRect(x, y float64, rectangle render.Rect) bool {
	return x >= rectangle.X && y >= rectangle.Y && x < rectangle.X+rectangle.Width && y < rectangle.Y+rectangle.Height
}

func commandForNodeColor(commands []render.Command, node *dom.Node, value color.NRGBA) *render.Command {
	for index := range commands {
		command := &commands[index]
		if command.Kind == render.FillRectCommand && command.Node == node && command.Color == value {
			return command
		}
	}
	return nil
}

func firstCommandWithColor(commands []render.Command, value color.NRGBA) *render.Command {
	for index := range commands {
		if commands[index].Kind == render.FillRectCommand && commands[index].Color == value {
			return &commands[index]
		}
	}
	return nil
}

func mathAbs(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}
