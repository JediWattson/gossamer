package browser

import "testing"

func TestTableTrackMergingAndMissingCellsUpdateLiveGeometry(t *testing.T) {
	t.Parallel()

	engine, page, tableID := computedStyleTestPage(t, `<!doctype html><html><body style="margin:0"><table id=target style="border:10px solid #808080;border-spacing:20px"><col id=columns span=10 style="width:0"><tr><td id=first style="width:50px;height:50px;padding:0"></td><td id=second style="width:50px;height:50px;padding:0"></td></tr><tr><td style="width:50px;height:50px;padding:0"></td><td style="width:50px;height:50px;padding:0"></td></tr></table></body></html>`)
	defer engine.Close()
	generation := page.DocumentGeneration()
	table := NodeHandle{Document: generation, Node: tableID}
	columnID := mustPageElementID(t, page, "columns")
	column := NodeHandle{Document: generation, Node: columnID}
	first := NodeHandle{Document: generation, Node: mustPageElementID(t, page, "first")}
	second := NodeHandle{Document: generation, Node: mustPageElementID(t, page, "second")}

	assertResolvedProperty(t, page, table, "width", "160px")
	tableGeometry, err := page.ElementGeometry(table)
	if err != nil {
		t.Fatal(err)
	}
	if tableGeometry.Rect.Width != 180 {
		t.Fatalf("initial merged table border box = %#v, want 180px", tableGeometry.Rect)
	}
	firstGeometry, err := page.ElementGeometry(first)
	if err != nil {
		t.Fatal(err)
	}
	secondGeometry, err := page.ElementGeometry(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstGeometry.Rect.Width != 50 || secondGeometry.Rect.X-firstGeometry.Rect.X != 70 {
		t.Fatalf("initial merged geometry = first:%#v second:%#v", firstGeometry.Rect, secondGeometry.Rect)
	}
	mergedLayout := page.layout.snapshot

	if err := page.document.SetAttribute(columnID, "style", "width:30px"); err != nil {
		t.Fatal(err)
	}
	assertResolvedProperty(t, page, column, "width", "520px")
	assertResolvedProperty(t, page, table, "width", "560px")
	tableGeometry, err = page.ElementGeometry(table)
	if err != nil {
		t.Fatal(err)
	}
	if tableGeometry.Rect.Width != 580 {
		t.Fatalf("constrained table border box = %#v, want 580px", tableGeometry.Rect)
	}
	if page.layout.snapshot == mergedLayout {
		t.Fatal("column constraint mutation reused the merged layout")
	}
	constrainedLayout := page.layout.snapshot

	if err := page.document.SetAttribute(columnID, "style", "width:0"); err != nil {
		t.Fatal(err)
	}
	assertResolvedProperty(t, page, table, "width", "160px")
	if page.layout.snapshot == constrainedLayout {
		t.Fatal("zero-width mutation reused the constrained layout")
	}
	if page.Frame() != nil || !page.Dirty() {
		t.Fatal("track-merging geometry reads published a frame or cleared dirtiness")
	}
}
