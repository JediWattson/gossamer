package browser

import "testing"

func TestTablePercentageSizingUpdatesLiveResolvedGeometry(t *testing.T) {
	t.Parallel()

	engine, page, tableID := computedStyleTestPage(t, `<!doctype html><html><body style="margin:0"><table id=target style="width:400px;border-spacing:0"><tr><td id=percent style="width:25%;height:10px;padding:0"></td><td id=pixel style="width:100px;height:10px;padding:0"></td><td id=auto style="height:10px;padding:0"></td></tr></table></body></html>`)
	defer engine.Close()
	generation := page.DocumentGeneration()
	table := NodeHandle{Document: generation, Node: tableID}
	percentID := mustPageElementID(t, page, "percent")
	percent := NodeHandle{Document: generation, Node: percentID}
	pixel := NodeHandle{Document: generation, Node: mustPageElementID(t, page, "pixel")}
	auto := NodeHandle{Document: generation, Node: mustPageElementID(t, page, "auto")}

	assertResolvedProperty(t, page, table, "width", "400px")
	assertResolvedProperty(t, page, percent, "width", "100px")
	assertResolvedProperty(t, page, pixel, "width", "100px")
	assertResolvedProperty(t, page, auto, "width", "200px")
	initialLayout := page.layout.snapshot

	if err := page.document.SetAttribute(percentID, "style", "width:50%;height:10px;padding:0"); err != nil {
		t.Fatal(err)
	}
	assertResolvedProperty(t, page, percent, "width", "200px")
	assertResolvedProperty(t, page, pixel, "width", "100px")
	assertResolvedProperty(t, page, auto, "width", "100px")
	if page.layout.snapshot == initialLayout {
		t.Fatal("percentage mutation reused the stale table layout")
	}
	percentageLayout := page.layout.snapshot

	if err := page.document.SetAttribute(percentID, "style", "width:80%;max-width:25%;height:10px;padding:0"); err != nil {
		t.Fatal(err)
	}
	assertResolvedProperty(t, page, percent, "width", "100px")
	assertResolvedProperty(t, page, pixel, "width", "100px")
	assertResolvedProperty(t, page, auto, "width", "200px")
	if page.layout.snapshot == percentageLayout {
		t.Fatal("max-width percentage mutation reused the stale table layout")
	}
	if page.Frame() != nil || !page.Dirty() {
		t.Fatal("percentage sizing reads published a frame or cleared dirtiness")
	}
}

func TestInlineTablePercentageIntrinsicWidthTracksDynamicContainingBlock(t *testing.T) {
	t.Parallel()

	engine, page, tableID := computedStyleTestPage(t, `<!doctype html><html><body style="margin:0"><div id=outer style="width:500px"><div style="width:10%"><div id=target style="display:inline-table;border-spacing:0"><div style="display:table-row"><div id=percent style="display:table-cell;width:100%;padding:0"><span style="display:inline-block;width:100%;height:10px"></span></div><div id=fixed style="display:table-cell;padding:0"><span style="display:inline-block;width:10px;height:10px"></span></div></div></div></div></div></body></html>`)
	defer engine.Close()
	generation := page.DocumentGeneration()
	table := NodeHandle{Document: generation, Node: tableID}
	outerID := mustPageElementID(t, page, "outer")
	percent := NodeHandle{Document: generation, Node: mustPageElementID(t, page, "percent")}
	fixed := NodeHandle{Document: generation, Node: mustPageElementID(t, page, "fixed")}

	assertResolvedProperty(t, page, table, "width", "50px")
	assertResolvedProperty(t, page, percent, "width", "40px")
	assertResolvedProperty(t, page, fixed, "width", "10px")
	initialLayout := page.layout.snapshot

	if err := page.document.SetAttribute(outerID, "style", "width:1000px"); err != nil {
		t.Fatal(err)
	}
	assertResolvedProperty(t, page, table, "width", "100px")
	assertResolvedProperty(t, page, percent, "width", "90px")
	assertResolvedProperty(t, page, fixed, "width", "10px")
	if page.layout.snapshot == initialLayout {
		t.Fatal("containing-block mutation reused stale inline-table intrinsic sizing")
	}
	if page.Frame() != nil || !page.Dirty() {
		t.Fatal("inline-table percentage reads published a frame or cleared dirtiness")
	}
}
