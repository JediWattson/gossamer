package browser

import "testing"

func TestVerticalGridGeometryAndResolvedTracksStayLive(t *testing.T) {
	t.Parallel()

	engine, page, gridID := computedStyleTestPage(t, `<!doctype html><html><body style="margin:0">
		<section id=target style="display:grid;writing-mode:vertical-rl;direction:ltr;width:120px;height:200px;grid-template-columns:50px 70px;grid-template-rows:40px 60px;column-gap:10px;row-gap:20px;justify-content:start;align-content:start"><i id=a style="grid-column:1;grid-row:1"></i><i id=b style="grid-column:2;grid-row:2"></i></section>
	</body></html>`)
	defer engine.Close()
	generation := page.DocumentGeneration()
	grid := NodeHandle{Document: generation, Node: gridID}
	a := NodeHandle{Document: generation, Node: mustPageElementID(t, page, "a")}
	b := NodeHandle{Document: generation, Node: mustPageElementID(t, page, "b")}

	geometry := func(handle NodeHandle) DOMElementGeometry {
		t.Helper()
		value, err := page.ElementGeometry(handle)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	assertResolvedProperty(t, page, grid, "grid-template-columns", "50px 70px")
	assertResolvedProperty(t, page, grid, "grid-template-rows", "40px 60px")
	assertResolvedProperty(t, page, grid, "width", "120px")
	assertResolvedProperty(t, page, grid, "height", "200px")
	initialA, initialB := geometry(a), geometry(b)
	if initialA.Rect.X <= initialB.Rect.X || initialA.Rect.Y >= initialB.Rect.Y {
		t.Fatalf("initial vertical grid geometry = a:%#v b:%#v", initialA.Rect, initialB.Rect)
	}
	firstLayout := page.layout.snapshot

	if err := page.document.SetAttribute(gridID, "style", "display:grid;writing-mode:vertical-lr;direction:rtl;width:120px;height:200px;grid-template-columns:50px 70px;grid-template-rows:40px 60px;column-gap:10px;row-gap:20px;justify-content:start;align-content:start"); err != nil {
		t.Fatal(err)
	}
	assertResolvedProperty(t, page, grid, "writing-mode", "vertical-lr")
	assertResolvedProperty(t, page, grid, "grid-template-columns", "50px 70px")
	assertResolvedProperty(t, page, grid, "width", "120px")
	assertResolvedProperty(t, page, grid, "height", "200px")
	liveA, liveB := geometry(a), geometry(b)
	if liveA.Rect.X >= liveB.Rect.X || liveA.Rect.Y <= liveB.Rect.Y || page.layout.snapshot == firstLayout {
		t.Fatalf("live vertical grid geometry = a:%#v b:%#v snapshots:%p/%p", liveA.Rect, liveB.Rect, page.layout.snapshot, firstLayout)
	}
	if page.Frame() != nil || !page.Dirty() {
		t.Fatal("vertical grid geometry read published a frame or cleared dirtiness")
	}
}
