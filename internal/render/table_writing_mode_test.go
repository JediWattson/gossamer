package render_test

import (
	"fmt"
	"image/color"
	"testing"

	"github.com/JediWattson/gossamer/internal/render"
)

func TestVerticalTableRowsColumnsAndDirectionUseLogicalAxes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		mode          string
		direction     string
		firstRowAfter bool
		firstColAfter bool
	}{
		{mode: "vertical-rl", direction: "ltr", firstRowAfter: true},
		{mode: "vertical-rl", direction: "rtl", firstRowAfter: true, firstColAfter: true},
		{mode: "vertical-lr", direction: "ltr"},
		{mode: "vertical-lr", direction: "rtl", firstColAfter: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.mode+"_"+test.direction, func(t *testing.T) {
			t.Parallel()
			document := mustParseTableDocument(t, fmt.Sprintf(`<!doctype html><html><body style="margin:0">
				<table id=table style="writing-mode:%s;direction:%s;width:70px;height:130px;border-spacing:10px 5px">
					<col id=first-col style="height:40px"><col id=second-col style="height:60px">
					<tr id=first-row style="width:25px"><td id=a>A</td><td id=b>B</td></tr>
					<tr id=second-row style="width:30px"><td id=c>C</td><td id=d>D</td></tr>
				</table>
			</body></html>`, test.mode, test.direction))
			frame, err := render.Render(document, render.Viewport{Width: 300, Height: 300})
			if err != nil {
				t.Fatal(err)
			}
			table := findBox(frame.Root, tableElementByID(t, document, "table"))
			firstRow := findBox(frame.Root, tableElementByID(t, document, "first-row"))
			secondRow := findBox(frame.Root, tableElementByID(t, document, "second-row"))
			firstColumn := findBox(frame.Root, tableElementByID(t, document, "first-col"))
			secondColumn := findBox(frame.Root, tableElementByID(t, document, "second-col"))
			a := findBox(frame.Root, tableElementByID(t, document, "a"))
			b := findBox(frame.Root, tableElementByID(t, document, "b"))
			if table == nil || firstRow == nil || secondRow == nil || firstColumn == nil || secondColumn == nil || a == nil || b == nil {
				t.Fatalf("vertical table boxes missing: table=%v rows=%v/%v columns=%v/%v cells=%v/%v", table, firstRow, secondRow, firstColumn, secondColumn, a, b)
			}
			assertNear(t, "vertical table physical width", table.ContentBounds.Width, 70)
			assertNear(t, "vertical table physical height", table.ContentBounds.Height, 130)
			assertNear(t, "vertical row inline extent", firstRow.Bounds.Height, 130)
			// Column structural boxes cover the row tracks plus leading/inter-row
			// spacing; the table retains the final block-end spacing itself.
			assertNear(t, "vertical column block extent", firstColumn.Bounds.Width, 65)
			if got := firstRow.Bounds.X > secondRow.Bounds.X; got != test.firstRowAfter {
				t.Fatalf("first row x %.1f vs second %.1f: after=%t, want %t", firstRow.Bounds.X, secondRow.Bounds.X, got, test.firstRowAfter)
			}
			if got := firstColumn.Bounds.Y > secondColumn.Bounds.Y; got != test.firstColAfter {
				t.Fatalf("first column y %.1f vs second %.1f: after=%t, want %t", firstColumn.Bounds.Y, secondColumn.Bounds.Y, got, test.firstColAfter)
			}
			if got := a.Bounds.Y > b.Bounds.Y; got != test.firstColAfter {
				t.Fatalf("first-row cells y %.1f/%.1f: first after=%t, want %t", a.Bounds.Y, b.Bounds.Y, got, test.firstColAfter)
			}
			if hit := render.HitTest(frame, a.Bounds.X+a.Bounds.Width/2, a.Bounds.Y+a.Bounds.Height/2); hit != tableElementByID(t, document, "a") {
				t.Fatalf("vertical cell hit = <%v>, want a", tableNodeName(hit))
			}
		})
	}
}

func TestHorizontalGridKeepsIndependentAxesInsideVerticalTable(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0"><table style="writing-mode:vertical-lr;width:120px;height:180px;border-spacing:0"><tr style="width:120px"><td style="height:180px;padding:0"><div id=inner style="display:grid;writing-mode:horizontal-tb;width:100px;height:60px;grid-template-columns:40px 60px;grid-template-rows:30px;justify-content:start;align-content:start"><i id=a>A</i><i id=b>B</i></div></td></tr></table></body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 300, Height: 300})
	if err != nil {
		t.Fatal(err)
	}
	inner := findBox(frame.Root, tableElementByID(t, document, "inner"))
	a := findBox(frame.Root, tableElementByID(t, document, "a"))
	b := findBox(frame.Root, tableElementByID(t, document, "b"))
	if inner == nil || a == nil || b == nil {
		t.Fatalf("horizontal Grid boxes inside vertical table missing: %v/%v/%v", inner, a, b)
	}
	assertNear(t, "horizontal Grid physical width", inner.Bounds.Width, 100)
	assertNear(t, "horizontal Grid physical height", inner.Bounds.Height, 60)
	assertNear(t, "horizontal Grid first track", a.Bounds.Width, 40)
	assertNear(t, "horizontal Grid second track", b.Bounds.Width, 60)
	assertNear(t, "horizontal Grid shared row", b.Bounds.Y, a.Bounds.Y)
	assertNear(t, "horizontal Grid second column", b.Bounds.X-a.Bounds.X, 40)
}

func TestHorizontalFlexKeepsIndependentAxesInsideVerticalTable(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0"><table style="writing-mode:vertical-lr;width:120px;height:180px;border-spacing:0"><tr style="width:120px"><td style="height:180px;padding:0"><div id=inner style="display:flex;writing-mode:horizontal-tb;width:100px;height:60px;align-items:flex-start"><i id=a style="flex:none;width:40px;height:30px">A</i><i id=b style="flex:none;width:60px;height:20px">B</i></div></td></tr></table></body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 300, Height: 300})
	if err != nil {
		t.Fatal(err)
	}
	inner := findBox(frame.Root, tableElementByID(t, document, "inner"))
	a := findBox(frame.Root, tableElementByID(t, document, "a"))
	b := findBox(frame.Root, tableElementByID(t, document, "b"))
	if inner == nil || a == nil || b == nil {
		t.Fatalf("horizontal Flex boxes inside vertical table missing: %v/%v/%v", inner, a, b)
	}
	assertNear(t, "horizontal Flex physical width", inner.Bounds.Width, 100)
	assertNear(t, "horizontal Flex physical height", inner.Bounds.Height, 60)
	assertNear(t, "horizontal Flex first item", a.Bounds.Width, 40)
	assertNear(t, "horizontal Flex second item", b.Bounds.Width, 60)
	assertNear(t, "horizontal Flex shared row", b.Bounds.Y, a.Bounds.Y)
	assertNear(t, "horizontal Flex second offset", b.Bounds.X-a.Bounds.X, 40)
}

func TestHorizontalBlockKeepsIndependentAxesInsideVerticalTable(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0"><table style="writing-mode:vertical-lr;width:120px;height:180px;border-spacing:0"><tr style="width:120px"><td style="height:180px;padding:0"><div id=inner style="writing-mode:horizontal-tb;width:100px;height:60px"><i id=a style="display:block;width:40px;height:30px">A</i><i id=b style="display:block;width:60px;height:20px">B</i></div></td></tr></table></body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 300, Height: 300})
	if err != nil {
		t.Fatal(err)
	}
	inner := findBox(frame.Root, tableElementByID(t, document, "inner"))
	a := findBox(frame.Root, tableElementByID(t, document, "a"))
	b := findBox(frame.Root, tableElementByID(t, document, "b"))
	if inner == nil || a == nil || b == nil {
		t.Fatalf("horizontal block boxes inside vertical table missing: %v/%v/%v", inner, a, b)
	}
	assertNear(t, "horizontal block physical width", inner.Bounds.Width, 100)
	assertNear(t, "horizontal block physical height", inner.Bounds.Height, 60)
	assertNear(t, "horizontal block first width", a.Bounds.Width, 40)
	assertNear(t, "horizontal block first height", a.Bounds.Height, 30)
	assertNear(t, "horizontal block shared x", b.Bounds.X, a.Bounds.X)
	assertNear(t, "horizontal block second y", b.Bounds.Y-a.Bounds.Y, 30)
}

func TestHorizontalTableKeepsIndependentAxesInsideVerticalGrid(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0"><section style="display:grid;writing-mode:vertical-lr;width:200px;height:200px;grid-template-columns:200px;grid-template-rows:200px;justify-content:start;align-content:start;justify-items:start;align-items:start"><table id=inner style="writing-mode:horizontal-tb;width:100px;height:60px;table-layout:fixed;border-spacing:0"><tr><td id=a style="width:40px;padding:0">A</td><td id=b style="width:60px;padding:0">B</td></tr></table></section></body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 300, Height: 300})
	if err != nil {
		t.Fatal(err)
	}
	inner := findBox(frame.Root, tableElementByID(t, document, "inner"))
	a := findBox(frame.Root, tableElementByID(t, document, "a"))
	b := findBox(frame.Root, tableElementByID(t, document, "b"))
	if inner == nil || a == nil || b == nil {
		t.Fatalf("horizontal table boxes inside vertical Grid missing: %v/%v/%v", inner, a, b)
	}
	assertNear(t, "horizontal table physical width", inner.Bounds.Width, 100)
	assertNear(t, "horizontal table physical height", inner.Bounds.Height, 60)
	assertNear(t, "horizontal table first column", a.Bounds.Width, 40)
	assertNear(t, "horizontal table second column", b.Bounds.Width, 60)
	assertNear(t, "horizontal table shared row", b.Bounds.Y, a.Bounds.Y)
	assertNear(t, "horizontal table second offset", b.Bounds.X-a.Bounds.X, 40)
}

func TestHorizontalTableKeepsIndependentAxesInsideVerticalTable(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0"><table style="writing-mode:vertical-rl;width:140px;height:200px;border-spacing:0"><tr><td style="padding:0"><table id=inner style="writing-mode:horizontal-tb;width:100px;height:60px;table-layout:fixed;border-spacing:0"><tr><td id=a style="width:40px;padding:0">A</td><td id=b style="width:60px;padding:0">B</td></tr></table></td></tr></table></body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 320, Height: 320})
	if err != nil {
		t.Fatal(err)
	}
	inner := findBox(frame.Root, tableElementByID(t, document, "inner"))
	a := findBox(frame.Root, tableElementByID(t, document, "a"))
	b := findBox(frame.Root, tableElementByID(t, document, "b"))
	if inner == nil || a == nil || b == nil {
		t.Fatalf("horizontal table boxes inside vertical table missing: %v/%v/%v", inner, a, b)
	}
	assertNear(t, "nested horizontal table width", inner.Bounds.Width, 100)
	assertNear(t, "nested horizontal table height", inner.Bounds.Height, 60)
	assertNear(t, "nested horizontal table second x", b.Bounds.X-a.Bounds.X, 40)
	assertNear(t, "nested horizontal table shared y", b.Bounds.Y, a.Bounds.Y)
}

func TestOppositeVerticalTableKeepsItsOwnBlockProgression(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0"><section style="display:grid;writing-mode:vertical-rl;width:200px;height:200px;grid-template-columns:200px;grid-template-rows:200px;justify-content:start;align-content:start;justify-items:start;align-items:start"><table id=inner style="writing-mode:vertical-lr;width:160px;height:100px;border-spacing:0"><tr><td id=a style="width:60px;height:100px;padding:0">A</td></tr><tr><td id=b style="width:100px;height:100px;padding:0">B</td></tr></table></section></body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 320, Height: 320})
	if err != nil {
		t.Fatal(err)
	}
	inner := findBox(frame.Root, tableElementByID(t, document, "inner"))
	a := findBox(frame.Root, tableElementByID(t, document, "a"))
	b := findBox(frame.Root, tableElementByID(t, document, "b"))
	if inner == nil || a == nil || b == nil {
		t.Fatalf("opposite vertical table boxes missing: %v/%v/%v", inner, a, b)
	}
	assertNear(t, "opposite vertical table width", inner.Bounds.Width, 160)
	assertNear(t, "opposite vertical table height", inner.Bounds.Height, 100)
	if a.Bounds.X >= b.Bounds.X {
		t.Fatalf("vertical-lr table did not preserve its own block progression: first=%v second=%v", a.Bounds, b.Bounds)
	}
}

func TestVerticalTableCaptionsTextAndCollapsedBordersTransformTogether(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><head><style>
		body { margin:0 }
		table { writing-mode:vertical-rl; width:60px; height:100px; border-collapse:collapse; font:12px/16px monospace }
		caption { width:20px; background:#010203 }
		td { width:30px; height:50px; padding:0; border:6px solid #123456 }
	</style></head><body><table id=table>
		<caption id=caption>Cap</caption><tr><td id=cell>Latin</td></tr>
	</table></body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 240, Height: 240})
	if err != nil {
		t.Fatal(err)
	}
	caption := findBox(frame.Root, tableElementByID(t, document, "caption"))
	cell := findBox(frame.Root, tableElementByID(t, document, "cell"))
	if caption == nil || cell == nil {
		t.Fatalf("vertical caption/cell = %#v/%#v", caption, cell)
	}
	if caption.Bounds.X < cell.Bounds.X+cell.Bounds.Width {
		t.Fatalf("vertical-rl block-start caption %#v is not to the right of cell %#v", caption.Bounds, cell.Bounds)
	}
	text := commandForText(frame.DisplayList.Commands, "Latin")
	if text == nil {
		t.Fatal("vertical cell text command missing")
	}
	if text.Rect.Height <= text.Rect.Width {
		t.Fatalf("vertical text bounds = %#v, want inline advance on physical y axis", text.Rect)
	}
	if text.Rect.X < cell.Bounds.X || text.Rect.X+text.Rect.Width > cell.Bounds.X+cell.Bounds.Width ||
		text.Rect.Y < cell.Bounds.Y || text.Rect.Y+text.Rect.Height > cell.Bounds.Y+cell.Bounds.Height {
		t.Fatalf("vertical text bounds %#v escape cell %#v", text.Rect, cell.Bounds)
	}
	border := firstCommandWithColor(frame.DisplayList.Commands, colorFromHex(0x12, 0x34, 0x56))
	if border == nil {
		t.Fatal("transformed collapsed border command missing")
	}
	if border.Rect.Width != 6 && border.Rect.Height != 6 {
		t.Fatalf("transformed collapsed border thickness = %#v, want 6px", border.Rect)
	}
}

func TestVerticalTablePreservesPhysicalBoxEdgesAfterLogicalLayout(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0">
		<table id=table style="writing-mode:vertical-lr;width:80px;height:100px;border-spacing:0;
			padding:1px 2px 3px 4px;border-style:solid;border-width:11px 12px 13px 14px">
			<tr><td id=cell style="width:40px;height:50px;padding:5px 6px 7px 8px;
				border-style:solid;border-width:15px 16px 17px 18px">X</td></tr>
		</table>
	</body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 260, Height: 260})
	if err != nil {
		t.Fatal(err)
	}
	tableNode := tableElementByID(t, document, "table")
	wrapper := findBox(frame.Root, tableNode)
	cell := findBox(frame.Root, tableElementByID(t, document, "cell"))
	if wrapper == nil || cell == nil {
		t.Fatalf("vertical physical-edge boxes = %#v/%#v", wrapper, cell)
	}
	var root *render.Box
	for _, child := range wrapper.Children {
		if child.Node == tableNode {
			root = child
			break
		}
	}
	if root == nil {
		t.Fatal("vertical table root box missing below wrapper")
	}
	assertEdges := func(label string, got render.Edges, top, right, bottom, left float64) {
		t.Helper()
		assertNear(t, label+" top", got.Top, top)
		assertNear(t, label+" right", got.Right, right)
		assertNear(t, label+" bottom", got.Bottom, bottom)
		assertNear(t, label+" left", got.Left, left)
	}
	assertEdges("table padding", root.Padding, 1, 2, 3, 4)
	assertEdges("table border", root.Border, 11, 12, 13, 14)
	assertEdges("cell padding", cell.Padding, 5, 6, 7, 8)
	assertEdges("cell border", cell.Border, 15, 16, 17, 18)
	assertNear(t, "table content physical x", root.ContentBounds.X-root.Bounds.X, 18)
	assertNear(t, "table content physical y", root.ContentBounds.Y-root.Bounds.Y, 12)
}

func TestVerticalTableLaysOutParallelBlockDescendantsInLogicalFlow(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0">
		<table style="writing-mode:vertical-rl;width:60px;height:100px;border-spacing:0"><tr><td style="padding:0">
			<div id=first style="width:20px;height:30px"></div>
			<div id=second style="width:25px;height:40px"></div>
			<img id=image style="display:block;width:12px;height:18px">
		</td></tr></table>
	</body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 220, Height: 220})
	if err != nil {
		t.Fatal(err)
	}
	first := findBox(frame.Root, tableElementByID(t, document, "first"))
	second := findBox(frame.Root, tableElementByID(t, document, "second"))
	imageBox := findBox(frame.Root, tableElementByID(t, document, "image"))
	if first == nil || second == nil || imageBox == nil {
		t.Fatalf("parallel vertical descendants = %#v/%#v/%#v", first, second, imageBox)
	}
	if first.Bounds.X <= second.Bounds.X {
		t.Fatalf("vertical-rl block order = first:%#v second:%#v, want first on right", first.Bounds, second.Bounds)
	}
	assertNear(t, "first physical width", first.ContentBounds.Width, 20)
	assertNear(t, "first physical height", first.ContentBounds.Height, 30)
	assertNear(t, "second physical width", second.ContentBounds.Width, 25)
	assertNear(t, "second physical height", second.ContentBounds.Height, 40)
	// Replaced content participates in logical flow, but its physical dimensions
	// and bitmap orientation are not rotated by writing-mode.
	assertNear(t, "image physical width", imageBox.ContentBounds.Width, 12)
	assertNear(t, "image physical height", imageBox.ContentBounds.Height, 18)
}

func TestVerticalTablePercentageInlineSizeNeedsDefiniteContainingHeight(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0">
		<table id=indefinite style="writing-mode:vertical-lr;height:50%;border-spacing:0"><tr><td style="height:40px">A</td></tr></table>
		<table id=auto style="writing-mode:vertical-lr;height:auto;border-spacing:0"><tr><td style="height:40px">A</td></tr></table>
		<div style="height:200px"><table id=definite style="writing-mode:vertical-lr;height:50%;border-spacing:0"><tr><td>A</td></tr></table></div>
	</body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 300, Height: 600})
	if err != nil {
		t.Fatal(err)
	}
	indefinite := findBox(frame.Root, tableElementByID(t, document, "indefinite"))
	auto := findBox(frame.Root, tableElementByID(t, document, "auto"))
	definite := findBox(frame.Root, tableElementByID(t, document, "definite"))
	if indefinite == nil || auto == nil || definite == nil {
		t.Fatalf("vertical percentage tables = %#v/%#v/%#v", indefinite, auto, definite)
	}
	assertNear(t, "indefinite percentage behaves as auto", indefinite.ContentBounds.Height, auto.ContentBounds.Height)
	assertNear(t, "definite percentage inline size", definite.ContentBounds.Height, 100)
}

func commandForText(commands []render.Command, text string) *render.Command {
	for index := range commands {
		if commands[index].Kind == render.DrawTextCommand && commands[index].Text == text {
			return &commands[index]
		}
	}
	return nil
}

func colorFromHex(red, green, blue uint8) color.NRGBA {
	return color.NRGBA{R: red, G: green, B: blue, A: 0xff}
}
