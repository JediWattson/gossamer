package browser

import "testing"

func TestComputedGridBaselineAlignmentStaysLiveWithGeometry(t *testing.T) {
	t.Parallel()

	engine, page, gridID := computedStyleTestPage(t, `<!doctype html><html><body style="margin:0"><section id=target style="display:grid;width:200px;grid-template-columns:100px 100px;grid-template-rows:auto;align-items:baseline"><div id=first style="font-size:32px;line-height:32px;padding-bottom:20px">first</div><div id=second style="font-size:16px;line-height:16px;padding-top:30px">second</div></section></body></html>`)
	defer engine.Close()
	generation := page.DocumentGeneration()
	grid := NodeHandle{Document: generation, Node: gridID}
	first := NodeHandle{Document: generation, Node: mustPageElementID(t, page, "first")}
	second := NodeHandle{Document: generation, Node: mustPageElementID(t, page, "second")}

	assertResolvedProperty(t, page, grid, "align-items", "baseline")
	assertResolvedProperty(t, page, grid, "grid-template-rows", "73px")
	gridGeometry, err := page.ElementGeometry(grid)
	if err != nil {
		t.Fatal(err)
	}
	firstGeometry, err := page.ElementGeometry(first)
	if err != nil {
		t.Fatal(err)
	}
	secondGeometry, err := page.ElementGeometry(second)
	if err != nil {
		t.Fatal(err)
	}
	if gridGeometry.Rect.Height != 73 || firstGeometry.Rect.Y-gridGeometry.Rect.Y != 15 || secondGeometry.Rect.Y != gridGeometry.Rect.Y {
		t.Fatalf("initial baseline geometry grid=%#v first=%#v second=%#v", gridGeometry.Rect, firstGeometry.Rect, secondGeometry.Rect)
	}

	if err := page.document.SetAttribute(gridID, "style", "display:grid;width:200px;height:100px;grid-template-columns:100px 100px;grid-template-rows:100px;align-items:last baseline"); err != nil {
		t.Fatal(err)
	}
	assertResolvedProperty(t, page, grid, "align-items", "last baseline")
	gridGeometry, _ = page.ElementGeometry(grid)
	firstGeometry, _ = page.ElementGeometry(first)
	secondGeometry, _ = page.ElementGeometry(second)
	if firstGeometry.Rect.Y-gridGeometry.Rect.Y != 42 || secondGeometry.Rect.Y-gridGeometry.Rect.Y != 27 {
		t.Fatalf("last baseline geometry grid=%#v first=%#v second=%#v", gridGeometry.Rect, firstGeometry.Rect, secondGeometry.Rect)
	}

	if err := page.document.SetAttribute(gridID, "style", "display:grid;width:200px;height:100px;grid-template-columns:100px 100px;grid-template-rows:20px;align-content:last baseline;align-items:start"); err != nil {
		t.Fatal(err)
	}
	assertResolvedProperty(t, page, grid, "align-content", "last baseline")
	firstGeometry, _ = page.ElementGeometry(first)
	gridGeometry, _ = page.ElementGeometry(grid)
	if firstGeometry.Rect.Y-gridGeometry.Rect.Y != 80 {
		t.Fatalf("last baseline content fallback grid=%#v first=%#v", gridGeometry.Rect, firstGeometry.Rect)
	}
	if page.Frame() != nil || !page.Dirty() {
		t.Fatal("baseline computed/geometry reads published a frame or cleared dirtiness")
	}
}
