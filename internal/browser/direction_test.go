package browser

import "testing"

func TestComputedDirectionAndRTLTableGeometryStayLive(t *testing.T) {
	t.Parallel()

	engine, page, tableID := computedStyleTestPage(t, `<!doctype html><html><body style="margin:0">
		<table id=target dir=rtl style="table-layout:fixed;width:120px;border-spacing:0"><tr>
			<td id=first>first</td><td id=second>second</td>
		</tr></table>
	</body></html>`)
	defer engine.Close()
	generation := page.DocumentGeneration()
	table := NodeHandle{Document: generation, Node: tableID}
	first := NodeHandle{Document: generation, Node: mustPageElementID(t, page, "first")}
	second := NodeHandle{Document: generation, Node: mustPageElementID(t, page, "second")}

	assertResolvedProperty(t, page, table, "direction", "rtl")
	firstRTL, err := page.ElementGeometry(first)
	if err != nil {
		t.Fatal(err)
	}
	secondRTL, err := page.ElementGeometry(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstRTL.Rect.X <= secondRTL.Rect.X {
		t.Fatalf("rtl cell positions = first:%#v second:%#v, want first on right", firstRTL.Rect, secondRTL.Rect)
	}
	firstLayout := page.layout.snapshot

	if err := page.document.SetAttribute(tableID, "dir", "ltr"); err != nil {
		t.Fatal(err)
	}
	assertResolvedProperty(t, page, table, "direction", "ltr")
	firstLTR, err := page.ElementGeometry(first)
	if err != nil {
		t.Fatal(err)
	}
	secondLTR, err := page.ElementGeometry(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstLTR.Rect.X >= secondLTR.Rect.X || page.layout.snapshot == firstLayout {
		t.Fatalf("ltr cell positions/layout = first:%#v second:%#v current:%p previous:%p", firstLTR.Rect, secondLTR.Rect, page.layout.snapshot, firstLayout)
	}
	if page.Frame() != nil || !page.Dirty() {
		t.Fatal("direction computed/geometry reads published a frame or cleared dirtiness")
	}
}
