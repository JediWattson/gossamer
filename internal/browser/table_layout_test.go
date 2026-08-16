package browser

import "testing"

func TestComputedTableRolesAndGeometryStayLiveWithoutPublishingFrame(t *testing.T) {
	t.Parallel()

	engine, page, tableID := computedStyleTestPage(t, `<!doctype html><html><body style="margin:0">
		<table id=target><colgroup><col id=first style="width:40px"><col id=second style="width:60px"></colgroup>
		<tbody><tr><td id=cell>A</td><td>B</td></tr></tbody></table>
	</body></html>`)
	defer engine.Close()
	generation := page.DocumentGeneration()
	table := NodeHandle{Document: generation, Node: tableID}
	cellID := mustPageElementID(t, page, "cell")
	cell := NodeHandle{Document: generation, Node: cellID}
	secondID := mustPageElementID(t, page, "second")

	assertResolvedProperty(t, page, table, "display", "table")
	assertResolvedProperty(t, page, cell, "display", "table-cell")
	assertResolvedProperty(t, page, table, "width", "100px")
	assertResolvedProperty(t, page, cell, "width", "40px")
	firstLayout := page.layout.snapshot
	if firstLayout == nil {
		t.Fatal("table geometry did not retain an unpublished layout")
	}
	geometry, err := page.ElementGeometry(cell)
	if err != nil {
		t.Fatal(err)
	}
	if geometry.Rect.Width != 40 || geometry.Rect.Height <= 0 {
		t.Fatalf("table cell geometry = %#v, want 40px nonempty cell", geometry.Rect)
	}
	if err := page.document.SetAttribute(secondID, "style", "width:80px"); err != nil {
		t.Fatal(err)
	}
	assertResolvedProperty(t, page, table, "width", "120px")
	if page.layout.snapshot == firstLayout {
		t.Fatal("column width mutation reused stale table layout")
	}
	if page.Frame() != nil || !page.Dirty() {
		t.Fatal("table computed/geometry reads published a frame or cleared dirtiness")
	}
}
