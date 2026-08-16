package browser

import "testing"

func TestComputedSubgridTracksAndGeometryStayLiveWithoutPublishingFrame(t *testing.T) {
	t.Parallel()

	engine, page, parentID := computedStyleTestPage(t, `<!doctype html><html><body style="margin:0">
		<section id=target style="display:grid;width:440px;grid-template-columns:[p1] 100px [p2] 100px [p3] 100px [p4] 100px [p5];column-gap:10px;grid-template-rows:20px">
			<div id=subgrid style="display:grid;grid-column:1 / span 4;grid-template-columns:subgrid [a] repeat(auto-fill,[b]) [c];grid-template-rows:20px"><i id=first style="grid-column:p1 / p2"></i><i id=last style="grid-column:p4 / p5"></i></div>
		</section>
	</body></html>`)
	defer engine.Close()
	generation := page.DocumentGeneration()
	parent := NodeHandle{Document: generation, Node: parentID}
	subgrid := NodeHandle{Document: generation, Node: mustPageElementID(t, page, "subgrid")}
	first := NodeHandle{Document: generation, Node: mustPageElementID(t, page, "first")}
	last := NodeHandle{Document: generation, Node: mustPageElementID(t, page, "last")}

	assertResolvedProperty(t, page, subgrid, "grid-template-columns", "subgrid [a] [b] [b] [b] [c]")
	assertResolvedProperty(t, page, subgrid, "column-gap", "normal")
	firstGeometry, err := page.ElementGeometry(first)
	if err != nil {
		t.Fatal(err)
	}
	lastGeometry, err := page.ElementGeometry(last)
	if err != nil {
		t.Fatal(err)
	}
	if firstGeometry.Rect.X != 0 || firstGeometry.Rect.Width != 100 || lastGeometry.Rect.X != 330 || lastGeometry.Rect.Width != 100 {
		t.Fatalf("adopted subgrid geometry first=%#v last=%#v", firstGeometry.Rect, lastGeometry.Rect)
	}

	if err := page.document.SetAttribute(subgrid.Node, "style", "display:grid;grid-column:1 / span 4;grid-template-columns:subgrid [a] repeat(auto-fill,[b]) [c];grid-template-rows:20px;column-gap:0"); err != nil {
		t.Fatal(err)
	}
	assertResolvedProperty(t, page, subgrid, "column-gap", "0px")
	lastGeometry, err = page.ElementGeometry(last)
	if err != nil {
		t.Fatal(err)
	}
	if lastGeometry.Rect.X != 325 || lastGeometry.Rect.Width != 105 {
		t.Fatalf("explicit zero-gap subgrid geometry = %#v, want x=325 width=105", lastGeometry.Rect)
	}

	if err := page.document.SetAttribute(parent.Node, "style", "display:block;width:440px"); err != nil {
		t.Fatal(err)
	}
	assertResolvedProperty(t, page, subgrid, "grid-template-columns", "none")
	if page.Frame() != nil || !page.Dirty() {
		t.Fatal("subgrid computed/geometry reads published a frame or cleared dirtiness")
	}
}

func TestSubgridDescendantsDriveLiveParentIntrinsicTracks(t *testing.T) {
	t.Parallel()

	engine, page, parentID := computedStyleTestPage(t, `<!doctype html><html><body style="margin:0"><section id=target style="display:grid;width:500px;grid-template-columns:auto auto;justify-content:start;grid-template-rows:20px"><div id=subgrid style="display:grid;grid-column:1/span 2;grid-template-columns:subgrid;grid-template-rows:20px"><i style="grid-column:1;width:120px"></i><i id=second style="grid-column:2;width:40px"></i></div></section></body></html>`)
	defer engine.Close()
	generation := page.DocumentGeneration()
	parent := NodeHandle{Document: generation, Node: parentID}
	second := NodeHandle{Document: generation, Node: mustPageElementID(t, page, "second")}

	assertResolvedProperty(t, page, parent, "grid-template-columns", "120px 40px")
	geometry, err := page.ElementGeometry(second)
	if err != nil {
		t.Fatal(err)
	}
	if geometry.Rect.X != 120 || geometry.Rect.Width != 40 {
		t.Fatalf("intrinsic subgrid geometry = %#v", geometry.Rect)
	}
	if page.Frame() != nil || !page.Dirty() {
		t.Fatal("intrinsic subgrid reads published a frame or cleared dirtiness")
	}
}
