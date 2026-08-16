package render_test

import (
	"image/color"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/html"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestTableLayoutBuildsColumnsRowsSpansAndPaintLayers(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><head><style>
		body { margin:0 }
		table { border:2px solid #010203; background:#f0f0f0 }
		caption { height:20px; background:#111111 }
		colgroup { background:#222222 }
		col:first-child { width:60px; background:#333333 }
		col:last-child { width:80px }
		tbody { background:#444444 }
		tr { background:#555555 }
		td { padding:4px; border:1px solid #666666 }
		#beta { background:#777777 }
	</style></head><body>
		<table id=table><caption id=caption>Cap</caption><colgroup id=columns><col id=first-col><col id=second-col></colgroup>
		<tbody id=group><tr id=first-row><td id=alpha>Alpha</td><td id=beta rowspan=2 style="height:60px">Beta</td></tr>
		<tr id=second-row><td id=wide>Wide</td></tr><tr id=third-row><td id=span colspan=2>Across</td></tr></tbody></table>
		<div id=after style="height:10px"></div>
	</body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 500, Height: 400})
	if err != nil {
		t.Fatal(err)
	}
	table := tableElementByID(t, document, "table")
	caption := tableElementByID(t, document, "caption")
	columns := tableElementByID(t, document, "columns")
	firstColumn := tableElementByID(t, document, "first-col")
	secondColumn := tableElementByID(t, document, "second-col")
	group := tableElementByID(t, document, "group")
	firstRow := tableElementByID(t, document, "first-row")
	secondRow := tableElementByID(t, document, "second-row")
	thirdRow := tableElementByID(t, document, "third-row")
	alpha := tableElementByID(t, document, "alpha")
	beta := tableElementByID(t, document, "beta")
	wide := tableElementByID(t, document, "wide")
	span := tableElementByID(t, document, "span")
	after := tableElementByID(t, document, "after")

	boxes := map[string]*render.Box{
		"table": findBox(frame.Root, table), "caption": findBox(frame.Root, caption),
		"columns": findBox(frame.Root, columns), "first column": findBox(frame.Root, firstColumn), "second column": findBox(frame.Root, secondColumn),
		"group": findBox(frame.Root, group), "first row": findBox(frame.Root, firstRow), "second row": findBox(frame.Root, secondRow), "third row": findBox(frame.Root, thirdRow),
		"alpha": findBox(frame.Root, alpha), "beta": findBox(frame.Root, beta), "wide": findBox(frame.Root, wide), "span": findBox(frame.Root, span),
		"after": findBox(frame.Root, after),
	}
	for name, box := range boxes {
		if box == nil {
			t.Fatalf("%s box is missing", name)
		}
	}
	tableBox := boxes["table"]
	assertNear(t, "table content width", tableBox.ContentBounds.Width, 140)
	assertNear(t, "table border width", tableBox.Bounds.Width, 144)
	assertNear(t, "caption width", boxes["caption"].Bounds.Width, 140)
	assertNear(t, "first column width", boxes["first column"].Bounds.Width, 60)
	assertNear(t, "second column width", boxes["second column"].Bounds.Width, 80)
	assertNear(t, "column group width", boxes["columns"].Bounds.Width, 140)
	assertNear(t, "row group width", boxes["group"].Bounds.Width, 140)
	for _, name := range []string{"first row", "second row", "third row"} {
		assertNear(t, name+" width", boxes[name].Bounds.Width, 140)
	}
	assertNear(t, "alpha column width", boxes["alpha"].Bounds.Width, 60)
	assertNear(t, "wide column width", boxes["wide"].Bounds.Width, 60)
	assertNear(t, "spanning width", boxes["span"].Bounds.Width, 140)
	assertNear(t, "second column x", boxes["beta"].Bounds.X, boxes["alpha"].Bounds.X+60)
	assertNear(t, "rowspan height", boxes["beta"].Bounds.Height, boxes["first row"].Bounds.Height+boxes["second row"].Bounds.Height)
	assertNear(t, "second row y", boxes["second row"].Bounds.Y, boxes["first row"].Bounds.Y+boxes["first row"].Bounds.Height)
	assertNear(t, "third row y", boxes["third row"].Bounds.Y, boxes["second row"].Bounds.Y+boxes["second row"].Bounds.Height)
	if boxes["after"].Bounds.Y < tableBox.Bounds.Y+tableBox.Bounds.Height {
		t.Fatalf("following block y = %.2f, want at or below table bottom %.2f", boxes["after"].Bounds.Y, tableBox.Bounds.Y+tableBox.Bounds.Height)
	}

	commands := frame.DisplayList.Commands
	wantPaintOrder := []color.NRGBA{
		{R: 0xf0, G: 0xf0, B: 0xf0, A: 0xff},
		{R: 0x11, G: 0x11, B: 0x11, A: 0xff},
		{R: 0x22, G: 0x22, B: 0x22, A: 0xff},
		{R: 0x33, G: 0x33, B: 0x33, A: 0xff},
		{R: 0x44, G: 0x44, B: 0x44, A: 0xff},
		{R: 0x55, G: 0x55, B: 0x55, A: 0xff},
		{R: 0x77, G: 0x77, B: 0x77, A: 0xff},
	}
	previous := -1
	for _, want := range wantPaintOrder {
		index := commandIndex(commands, func(command render.Command) bool {
			return command.Kind == render.FillRectCommand && command.Color == want
		})
		if index < 0 || index <= previous {
			t.Fatalf("paint color %#v index = %d after %d", want, index, previous)
		}
		previous = index
	}
	pointX := boxes["alpha"].Bounds.X + 2
	pointY := boxes["alpha"].Bounds.Y + boxes["alpha"].Bounds.Height - 2
	if hit := render.HitTest(frame, pointX, pointY); hit != alpha {
		t.Fatalf("cell padding hit = <%v>, want alpha cell", tableNodeName(hit))
	}
}

func TestTableFixupGeneratesAnonymousWrappersWithoutChangingDOM(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0">
		<div id=host><span id=first style="display:table-cell;width:40px">A</span><span id=second style="display:table-cell;width:60px">B</span></div>
		<div id=table style="display:table"><span id=ordinary>A</span><span>B</span></div>
	</body></html>`)
	host := tableElementByID(t, document, "host")
	first := tableElementByID(t, document, "first")
	second := tableElementByID(t, document, "second")
	table := tableElementByID(t, document, "table")
	if len(host.Children) != 2 || host.Children[0] != first || host.Children[1] != second || len(table.Children) != 2 {
		t.Fatal("pre-render DOM fixture is malformed")
	}
	frame, err := render.Render(document, render.Viewport{Width: 300, Height: 200})
	if err != nil {
		t.Fatal(err)
	}
	firstBox := findBox(frame.Root, first)
	secondBox := findBox(frame.Root, second)
	tableBox := findBox(frame.Root, table)
	if firstBox == nil || secondBox == nil || tableBox == nil {
		t.Fatalf("fixed boxes = first:%#v second:%#v table:%#v", firstBox, secondBox, tableBox)
	}
	assertNear(t, "misparented first width", firstBox.Bounds.Width, 40)
	assertNear(t, "misparented second width", secondBox.Bounds.Width, 60)
	assertNear(t, "misparented second x", secondBox.Bounds.X, firstBox.Bounds.X+firstBox.Bounds.Width)
	if anonymousTableBoxCount(frame.Root) < 4 {
		t.Fatalf("anonymous box count = %d, want table/row/cell fixup wrappers", anonymousTableBoxCount(frame.Root))
	}
	if len(host.Children) != 2 || host.Children[0] != first || host.Children[1] != second || len(table.Children) != 2 {
		t.Fatal("table box fixup mutated the DOM")
	}
}

func TestInlineTableParticipatesAsOneAtomicInlineBox(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0"><div id=container style="width:220px;font:16px/24px monospace">before <table id=table style="display:inline-table"><tr><td style="width:30px">A</td><td style="width:40px">B</td></tr></table> after</div></body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 300, Height: 200})
	if err != nil {
		t.Fatal(err)
	}
	container := tableElementByID(t, document, "container")
	table := tableElementByID(t, document, "table")
	containerBox := findBox(frame.Root, container)
	tableBox := findBox(frame.Root, table)
	if containerBox == nil || tableBox == nil {
		t.Fatalf("inline table boxes = container:%#v table:%#v", containerBox, tableBox)
	}
	assertNear(t, "inline table width", tableBox.Bounds.Width, 70)
	before := findTextFragment(collectTextFragments(frame.Root), "before")
	after := findTextFragment(collectTextFragments(frame.Root), "after")
	if before == nil || after == nil {
		t.Fatalf("inline table surrounding fragments = before:%#v after:%#v", before, after)
	}
	if !(before.X+before.Width <= tableBox.Bounds.X && tableBox.Bounds.X+tableBox.Bounds.Width <= after.X) {
		t.Fatalf("inline order = before %#v table %#v after %#v", before, tableBox.Bounds, after)
	}
	if tableBox.Bounds.Y < containerBox.ContentBounds.Y || tableBox.Bounds.Y+tableBox.Bounds.Height > containerBox.ContentBounds.Y+containerBox.ContentBounds.Height {
		t.Fatalf("inline table %#v escapes containing line %#v", tableBox.Bounds, containerBox.ContentBounds)
	}
}

func TestTableMinimumWidthExpandsSpecifiedWidthBeforeAutoMarginCentering(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0;width:300px"><table id=table style="width:50px;margin-left:auto;margin-right:auto"><colgroup><col style="width:60px"><col style="width:80px"></colgroup><tr><td>A</td><td>B</td></tr></table></body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 300, Height: 200})
	if err != nil {
		t.Fatal(err)
	}
	table := tableElementByID(t, document, "table")
	box := findBox(frame.Root, table)
	if box == nil {
		t.Fatal("table box missing")
	}
	assertNear(t, "minimum-expanded table width", box.Bounds.Width, 140)
	assertNear(t, "minimum-expanded centered x", box.Bounds.X, 80)
}

func TestDefiniteTableHeightDistributesExtraSpaceAcrossRows(t *testing.T) {
	t.Parallel()

	document := mustParseTableDocument(t, `<!doctype html><html><body style="margin:0"><table id=table style="height:100px"><tr id=first><td>A</td></tr><tr id=second><td>B</td></tr></table></body></html>`)
	frame, err := render.Render(document, render.Viewport{Width: 300, Height: 200})
	if err != nil {
		t.Fatal(err)
	}
	table := findBox(frame.Root, tableElementByID(t, document, "table"))
	first := findBox(frame.Root, tableElementByID(t, document, "first"))
	second := findBox(frame.Root, tableElementByID(t, document, "second"))
	if table == nil || first == nil || second == nil {
		t.Fatalf("definite-height table boxes = table:%#v first:%#v second:%#v", table, first, second)
	}
	assertNear(t, "definite table content height", table.ContentBounds.Height, 100)
	assertNear(t, "distributed row heights", first.Bounds.Height, second.Bounds.Height)
	assertNear(t, "distributed row total", first.Bounds.Height+second.Bounds.Height, 100)
}

func mustParseTableDocument(t *testing.T, source string) *dom.Node {
	t.Helper()
	document, err := html.Parse(strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	return document
}

func tableElementByID(t *testing.T, root *dom.Node, id string) *dom.Node {
	t.Helper()
	var visit func(*dom.Node) *dom.Node
	visit = func(node *dom.Node) *dom.Node {
		if node == nil {
			return nil
		}
		if node.Type == dom.ElementNode {
			for _, attribute := range node.Attributes {
				if attribute.Name == "id" && attribute.Value == id {
					return node
				}
			}
		}
		for _, child := range node.Children {
			if found := visit(child); found != nil {
				return found
			}
		}
		return nil
	}
	if node := visit(root); node != nil {
		return node
	}
	t.Fatalf("element #%s not found", id)
	return nil
}

func anonymousTableBoxCount(box *render.Box) int {
	if box == nil {
		return 0
	}
	count := 0
	if box.Node == nil && box.Bounds.Width > 0 {
		count++
	}
	for _, child := range box.Children {
		count += anonymousTableBoxCount(child)
	}
	return count
}

func tableNodeName(node *dom.Node) string {
	if node == nil {
		return "nil"
	}
	return node.Data
}
