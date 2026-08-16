package browser

import "testing"

func TestComputedOverflowAlignmentStaysLiveAndDrivesUnpublishedGeometry(t *testing.T) {
	t.Parallel()

	engine, page, gridID := computedStyleTestPage(t, `<!doctype html><html><body style="margin:0"><section id=target style="display:grid;width:100px;height:100px;grid-template-columns:150px;grid-template-rows:150px;justify-content:safe center;align-content:safe center"><div id=child></div></section></body></html>`)
	defer engine.Close()
	generation := page.DocumentGeneration()
	grid := NodeHandle{Document: generation, Node: gridID}
	child := NodeHandle{Document: generation, Node: mustPageElementID(t, page, "child")}

	assertResolvedProperty(t, page, grid, "justify-content", "safe center")
	assertResolvedProperty(t, page, grid, "align-content", "safe center")
	gridGeometry, err := page.ElementGeometry(grid)
	if err != nil {
		t.Fatal(err)
	}
	childGeometry, err := page.ElementGeometry(child)
	if err != nil {
		t.Fatal(err)
	}
	if childGeometry.Rect.X != gridGeometry.Rect.X || childGeometry.Rect.Y != gridGeometry.Rect.Y {
		t.Fatalf("safe content geometry grid=%#v child=%#v", gridGeometry.Rect, childGeometry.Rect)
	}

	if err := page.document.SetAttribute(gridID, "style", "display:grid;width:100px;height:100px;grid-template-columns:150px;grid-template-rows:150px;justify-content:unsafe center;align-content:unsafe center"); err != nil {
		t.Fatal(err)
	}
	assertResolvedProperty(t, page, grid, "justify-content", "unsafe center")
	assertResolvedProperty(t, page, grid, "align-content", "unsafe center")
	gridGeometry, err = page.ElementGeometry(grid)
	if err != nil {
		t.Fatal(err)
	}
	childGeometry, err = page.ElementGeometry(child)
	if err != nil {
		t.Fatal(err)
	}
	if childGeometry.Rect.X-gridGeometry.Rect.X != -25 || childGeometry.Rect.Y-gridGeometry.Rect.Y != -25 {
		t.Fatalf("unsafe content geometry grid=%#v child=%#v", gridGeometry.Rect, childGeometry.Rect)
	}

	if err := page.document.SetAttribute(gridID, "style", "display:grid;width:100px;height:100px;grid-template-columns:100px;grid-template-rows:100px"); err != nil {
		t.Fatal(err)
	}
	if err := page.document.SetAttribute(child.Node, "style", "width:150px;height:150px;justify-self:safe center;align-self:safe center"); err != nil {
		t.Fatal(err)
	}
	assertResolvedProperty(t, page, child, "justify-self", "safe center")
	assertResolvedProperty(t, page, child, "align-self", "safe center")
	gridGeometry, _ = page.ElementGeometry(grid)
	childGeometry, _ = page.ElementGeometry(child)
	if childGeometry.Rect.X != gridGeometry.Rect.X || childGeometry.Rect.Y != gridGeometry.Rect.Y {
		t.Fatalf("safe self geometry grid=%#v child=%#v", gridGeometry.Rect, childGeometry.Rect)
	}

	if err := page.document.SetAttribute(child.Node, "style", "width:150px;height:150px;justify-self:unsafe center;align-self:unsafe center"); err != nil {
		t.Fatal(err)
	}
	assertResolvedProperty(t, page, child, "justify-self", "unsafe center")
	assertResolvedProperty(t, page, child, "align-self", "unsafe center")
	gridGeometry, _ = page.ElementGeometry(grid)
	childGeometry, _ = page.ElementGeometry(child)
	if childGeometry.Rect.X-gridGeometry.Rect.X != -25 || childGeometry.Rect.Y-gridGeometry.Rect.Y != -25 {
		t.Fatalf("unsafe self geometry grid=%#v child=%#v", gridGeometry.Rect, childGeometry.Rect)
	}
	if page.Frame() != nil || !page.Dirty() {
		t.Fatal("overflow-alignment computed/geometry reads published a frame or cleared dirtiness")
	}
}
