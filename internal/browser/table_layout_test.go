package browser

import "testing"

func TestComputedTableRolesAndGeometryStayLiveWithoutPublishingFrame(t *testing.T) {
	t.Parallel()

	engine, page, tableID := computedStyleTestPage(t, `<!doctype html><html><body style="margin:0">
		<table id=target style="border-spacing:0"><colgroup><col id=first style="width:40px"><col id=second style="width:60px"></colgroup>
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

func TestComputedTableFormattingPropertiesDriveLiveGeometry(t *testing.T) {
	t.Parallel()

	engine, page, tableID := computedStyleTestPage(t, `<!doctype html><html><body style="margin:0">
		<table id=target style="width:200px;table-layout:fixed;border-collapse:separate;border-spacing:10px 4px;empty-cells:hide">
		<caption id=caption style="caption-side:bottom">Caption</caption><col style="width:60px"><col>
		<tr><td id=first></td><td id=second>B</td></tr></table>
	</body></html>`)
	defer engine.Close()
	generation := page.DocumentGeneration()
	table := NodeHandle{Document: generation, Node: tableID}
	first := NodeHandle{Document: generation, Node: mustPageElementID(t, page, "first")}
	second := NodeHandle{Document: generation, Node: mustPageElementID(t, page, "second")}
	caption := NodeHandle{Document: generation, Node: mustPageElementID(t, page, "caption")}

	for property, expected := range map[string]string{
		"border-collapse": "separate",
		"border-spacing":  "10px 4px",
		"empty-cells":     "hide",
		"table-layout":    "fixed",
	} {
		assertResolvedProperty(t, page, table, property, expected)
	}
	assertResolvedProperty(t, page, first, "empty-cells", "hide")
	assertResolvedProperty(t, page, caption, "caption-side", "bottom")
	firstGeometry, err := page.ElementGeometry(first)
	if err != nil {
		t.Fatal(err)
	}
	secondGeometry, err := page.ElementGeometry(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstGeometry.Rect.Width != 60 || secondGeometry.Rect.X-(firstGeometry.Rect.X+firstGeometry.Rect.Width) != 10 {
		t.Fatalf("separated fixed cells = first:%#v second:%#v", firstGeometry.Rect, secondGeometry.Rect)
	}
	firstLayout := page.layout.snapshot
	if err := page.document.SetAttribute(tableID, "style", "width:200px;table-layout:fixed;border-collapse:collapse;border-spacing:10px 4px;empty-cells:hide"); err != nil {
		t.Fatal(err)
	}
	assertResolvedProperty(t, page, table, "border-collapse", "collapse")
	secondGeometry, err = page.ElementGeometry(second)
	if err != nil {
		t.Fatal(err)
	}
	firstGeometry, err = page.ElementGeometry(first)
	if err != nil {
		t.Fatal(err)
	}
	if gap := secondGeometry.Rect.X - (firstGeometry.Rect.X + firstGeometry.Rect.Width); gap != 0 {
		t.Fatalf("collapsed cell gap = %v, want 0", gap)
	}
	if page.layout.snapshot == firstLayout {
		t.Fatal("border-model mutation reused stale layout")
	}
	if page.Frame() != nil || !page.Dirty() {
		t.Fatal("live table formatting read published a frame or cleared dirtiness")
	}
}
