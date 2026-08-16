package browser

import "testing"

func TestTableTrackCollapseUpdatesLiveGeometryWithoutPublishingFrame(t *testing.T) {
	t.Parallel()

	engine, page, tableID := computedStyleTestPage(t, `<!doctype html><html><body style="margin:0">
		<table id=target style="width:200px;table-layout:fixed;border-spacing:0">
			<col id=first-col style="width:100px"><col style="width:100px">
			<tr id=first-row><td id=first style="height:20px;padding:0"></td><td id=second style="height:20px;padding:0"></td></tr>
			<tr id=second-row><td style="height:30px;padding:0"></td><td style="height:30px;padding:0"></td></tr>
		</table>
	</body></html>`)
	defer engine.Close()
	generation := page.DocumentGeneration()
	table := NodeHandle{Document: generation, Node: tableID}
	columnID := mustPageElementID(t, page, "first-col")
	column := NodeHandle{Document: generation, Node: columnID}
	first := NodeHandle{Document: generation, Node: mustPageElementID(t, page, "first")}
	second := NodeHandle{Document: generation, Node: mustPageElementID(t, page, "second")}
	rowID := mustPageElementID(t, page, "first-row")
	row := NodeHandle{Document: generation, Node: rowID}

	assertResolvedProperty(t, page, table, "width", "200px")
	firstGeometry, err := page.ElementGeometry(first)
	if err != nil {
		t.Fatal(err)
	}
	secondGeometry, err := page.ElementGeometry(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstGeometry.Rect.Width != 100 || secondGeometry.Rect.X-firstGeometry.Rect.X != 100 {
		t.Fatalf("initial table geometry = first:%#v second:%#v", firstGeometry.Rect, secondGeometry.Rect)
	}
	initialLayout := page.layout.snapshot
	if err := page.document.SetAttribute(columnID, "style", "width:100px;visibility:collapse"); err != nil {
		t.Fatal(err)
	}
	assertResolvedProperty(t, page, column, "visibility", "collapse")
	assertResolvedProperty(t, page, table, "width", "100px")
	firstGeometry, err = page.ElementGeometry(first)
	if err != nil {
		t.Fatal(err)
	}
	secondGeometry, err = page.ElementGeometry(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstGeometry.Rect.Width != 0 || secondGeometry.Rect.X != firstGeometry.Rect.X || secondGeometry.Rect.Width != 100 {
		t.Fatalf("collapsed column geometry = first:%#v second:%#v", firstGeometry.Rect, secondGeometry.Rect)
	}
	columnLayout := page.layout.snapshot
	if columnLayout == initialLayout {
		t.Fatal("column visibility mutation reused stale layout")
	}
	tableBefore, err := page.ElementGeometry(table)
	if err != nil {
		t.Fatal(err)
	}
	if err := page.document.SetAttribute(rowID, "style", "visibility:collapse"); err != nil {
		t.Fatal(err)
	}
	assertResolvedProperty(t, page, row, "visibility", "collapse")
	tableAfter, err := page.ElementGeometry(table)
	if err != nil {
		t.Fatal(err)
	}
	if tableAfter.Rect.Height >= tableBefore.Rect.Height || page.layout.snapshot == columnLayout {
		t.Fatalf("collapsed row layout = before:%#v after:%#v reused=%t", tableBefore.Rect, tableAfter.Rect, page.layout.snapshot == columnLayout)
	}
	if page.Frame() != nil || !page.Dirty() {
		t.Fatal("track-collapse computed/geometry reads published a frame or cleared dirtiness")
	}
}
