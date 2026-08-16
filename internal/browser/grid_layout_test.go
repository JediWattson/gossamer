package browser

import "testing"

func TestComputedGridPropertiesAndGeometryStayLiveWithoutPublishingFrame(t *testing.T) {
	t.Parallel()

	engine, page, gridID := computedStyleTestPage(t, `<!doctype html><html><body style="margin:0">
		<section id=target style="display:grid;width:300px;grid-template-columns:50px 1fr 2fr;grid-template-rows:40px;grid-auto-rows:20px;grid-auto-flow:row dense;gap:10px">
			<div id=first style="grid-column:1 / span 2"></div><div id=second></div>
		</section>
	</body></html>`)
	defer engine.Close()
	generation := page.DocumentGeneration()
	grid := NodeHandle{Document: generation, Node: gridID}
	first := NodeHandle{Document: generation, Node: mustPageElementID(t, page, "first")}
	second := NodeHandle{Document: generation, Node: mustPageElementID(t, page, "second")}

	for property, expected := range map[string]string{
		"display":               "grid",
		"grid-template-columns": "50px 76.66666666666667px 153.33333333333334px",
		"grid-template-rows":    "40px",
		"grid-auto-rows":        "20px",
		"grid-auto-flow":        "row dense",
		"grid-column-start":     "1",
		"grid-column-end":       "span 2",
	} {
		handle := grid
		if property == "grid-column-start" || property == "grid-column-end" {
			handle = first
		}
		assertResolvedProperty(t, page, handle, property, expected)
	}
	firstGeometry, err := page.ElementGeometry(first)
	if err != nil {
		t.Fatal(err)
	}
	secondGeometry, err := page.ElementGeometry(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstGeometry.Rect.Width < 130 || secondGeometry.Rect.X <= firstGeometry.Rect.X+firstGeometry.Rect.Width {
		t.Fatalf("initial grid geometry first=%#v second=%#v", firstGeometry.Rect, secondGeometry.Rect)
	}
	firstLayout := page.layout.snapshot
	if err := page.document.SetAttribute(gridID, "style", "display:grid;width:300px;grid-template-columns:100px 1fr 1fr;grid-auto-rows:30px;column-gap:20px"); err != nil {
		t.Fatal(err)
	}
	assertResolvedProperty(t, page, grid, "grid-template-columns", "100px 80px 80px")
	secondGeometry, err = page.ElementGeometry(second)
	if err != nil {
		t.Fatal(err)
	}
	if secondGeometry.Rect.X != 220 {
		t.Fatalf("mutated grid second geometry = %#v, want x=220", secondGeometry.Rect)
	}
	if page.layout.snapshot == firstLayout {
		t.Fatal("grid style mutation reused stale layout snapshot")
	}
	if page.Frame() != nil || !page.Dirty() {
		t.Fatal("grid computed/geometry reads published a frame or cleared dirtiness")
	}
}

func TestComputedGridTemplateIncludesImplicitTracksAndEmptyGridKeepsNone(t *testing.T) {
	t.Parallel()

	engine, page, gridID := computedStyleTestPage(t, `<!doctype html><html><body style="margin:0">
		<section id=target style="display:grid;width:150px;grid-template-columns:40px 40px;grid-auto-columns:30px;column-gap:5px"><div style="grid-column:4"></div></section>
		<section id=empty style="display:grid;width:100px"></section>
	</body></html>`)
	defer engine.Close()
	generation := page.DocumentGeneration()
	grid := NodeHandle{Document: generation, Node: gridID}
	empty := NodeHandle{Document: generation, Node: mustPageElementID(t, page, "empty")}

	assertResolvedProperty(t, page, grid, "grid-template-columns", "40px 40px 30px 30px")
	assertResolvedProperty(t, page, empty, "grid-template-columns", "none")
	assertResolvedProperty(t, page, empty, "grid-template-rows", "none")
	if page.Frame() != nil || !page.Dirty() {
		t.Fatal("grid resolved-style reads published a frame or cleared dirtiness")
	}
}

func TestComputedGridMinMaxTracksResolveAndStayLive(t *testing.T) {
	t.Parallel()

	engine, page, gridID := computedStyleTestPage(t, `<!doctype html><html><body style="margin:0"><section id=target style="display:grid;width:200px;grid-template-columns:minmax(80px,1fr) 1fr"><div>aaaa aaaa</div><div></div></section></body></html>`)
	defer engine.Close()
	handle := NodeHandle{Document: page.DocumentGeneration(), Node: gridID}

	assertResolvedProperty(t, page, handle, "grid-template-columns", "100px 100px")
	if err := page.document.SetAttribute(gridID, "style", "display:grid;width:200px;grid-template-columns:minmax(160px,1fr) 1fr;grid-auto-columns:minmax(min-content,max-content)"); err != nil {
		t.Fatal(err)
	}
	assertResolvedProperty(t, page, handle, "grid-template-columns", "160px 40px")
	assertResolvedProperty(t, page, handle, "grid-auto-columns", "minmax(min-content, max-content)")
	if err := page.document.SetAttribute(gridID, "style", "display:grid;width:200px;grid-template-columns:fit-content(50px) 1fr;grid-auto-columns:fit-content(25%)"); err != nil {
		t.Fatal(err)
	}
	assertResolvedProperty(t, page, handle, "grid-template-columns", "50px 150px")
	assertResolvedProperty(t, page, handle, "grid-auto-columns", "fit-content(25%)")
	if page.Frame() != nil || !page.Dirty() {
		t.Fatal("minmax computed-style reads published a frame or cleared dirtiness")
	}
}
