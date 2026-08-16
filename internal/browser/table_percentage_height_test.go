package browser

import "testing"

func TestTablePercentageHeightDistributionUpdatesLiveResolvedGeometry(t *testing.T) {
	t.Parallel()

	engine, page, tableID := computedStyleTestPage(t, `<!doctype html><html><body style="margin:0"><table id=target style="width:100px;height:120px;border-spacing:0"><tr id=percent-row style="height:25%"><td id=cell style="padding:0"><div id=child style="height:100%"><div style="height:10px"></div></div></td></tr><tr id=auto-row><td style="padding:0"><div style="height:10px"></div></td></tr></table></body></html>`)
	defer engine.Close()
	generation := page.DocumentGeneration()
	table := NodeHandle{Document: generation, Node: tableID}
	percentRow := NodeHandle{Document: generation, Node: mustPageElementID(t, page, "percent-row")}
	autoRow := NodeHandle{Document: generation, Node: mustPageElementID(t, page, "auto-row")}
	cellID := mustPageElementID(t, page, "cell")
	cell := NodeHandle{Document: generation, Node: cellID}
	child := NodeHandle{Document: generation, Node: mustPageElementID(t, page, "child")}

	assertResolvedProperty(t, page, table, "height", "120px")
	assertResolvedProperty(t, page, percentRow, "height", "30px")
	assertResolvedProperty(t, page, autoRow, "height", "90px")
	assertResolvedProperty(t, page, cell, "height", "30px")
	assertResolvedProperty(t, page, child, "height", "30px")
	initialLayout := page.layout.snapshot

	if err := page.document.SetAttribute(tableID, "style", "width:100px;height:200px;border-spacing:0"); err != nil {
		t.Fatal(err)
	}
	assertResolvedProperty(t, page, percentRow, "height", "50px")
	assertResolvedProperty(t, page, autoRow, "height", "150px")
	assertResolvedProperty(t, page, child, "height", "50px")
	if page.layout.snapshot == initialLayout {
		t.Fatal("table height mutation reused stale row geometry")
	}
	definiteLayout := page.layout.snapshot

	if err := page.document.SetAttribute(cellID, "style", "height:100%;padding:0"); err != nil {
		t.Fatal(err)
	}
	if err := page.document.SetAttribute(tableID, "style", "width:100px;height:auto;border-spacing:0"); err != nil {
		t.Fatal(err)
	}
	assertResolvedProperty(t, page, percentRow, "height", "10px")
	assertResolvedProperty(t, page, autoRow, "height", "10px")
	assertResolvedProperty(t, page, child, "height", "100%")
	if page.layout.snapshot == definiteLayout {
		t.Fatal("auto table height mutation reused stale second-pass geometry")
	}
	autoLayout := page.layout.snapshot

	if err := page.document.SetAttribute(tableID, "style", "width:100px;height:100%;border-spacing:0"); err != nil {
		t.Fatal(err)
	}
	assertResolvedProperty(t, page, child, "height", "10px")
	if page.layout.snapshot == autoLayout {
		t.Fatal("computed percentage table height did not trigger a fresh cell-content second pass")
	}
	if page.Frame() != nil || !page.Dirty() {
		t.Fatal("table percentage-height reads published a frame or cleared dirtiness")
	}
}
