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
		<section id=target style="display:grid;width:150px;grid-template-columns:[first] 40px [middle] 40px [last];grid-auto-columns:20px 30px;column-gap:5px"><div style="grid-column:-5"></div><div style="grid-column:4"></div></section>
		<section id=empty style="display:grid;width:100px"></section>
	</body></html>`)
	defer engine.Close()
	generation := page.DocumentGeneration()
	grid := NodeHandle{Document: generation, Node: gridID}
	empty := NodeHandle{Document: generation, Node: mustPageElementID(t, page, "empty")}

	assertResolvedProperty(t, page, grid, "grid-template-columns", "20px 30px [first] 40px [middle] 40px [last] 20px 30px")
	assertResolvedProperty(t, page, grid, "grid-auto-columns", "20px 30px")
	assertResolvedProperty(t, page, empty, "grid-template-columns", "none")
	assertResolvedProperty(t, page, empty, "grid-template-rows", "none")
	if page.Frame() != nil || !page.Dirty() {
		t.Fatal("grid resolved-style reads published a frame or cleared dirtiness")
	}
}

func TestComputedGridTemplateRetainsNamedLinesAndNamedPlacement(t *testing.T) {
	t.Parallel()

	engine, page, gridID := computedStyleTestPage(t, `<!doctype html><html><body style="margin:0">
		<section id=target style="display:grid;width:100px;grid-template-columns:[first content-start] 40px [middle] 60px [last content-end];grid-auto-rows:20px"><div id=child style="grid-column:content"></div></section>
	</body></html>`)
	defer engine.Close()
	generation := page.DocumentGeneration()
	grid := NodeHandle{Document: generation, Node: gridID}
	child := NodeHandle{Document: generation, Node: mustPageElementID(t, page, "child")}

	assertResolvedProperty(t, page, grid, "grid-template-columns", "[first content-start] 40px [middle] 60px [last content-end]")
	assertResolvedProperty(t, page, child, "grid-column-start", "content")
	assertResolvedProperty(t, page, child, "grid-column-end", "content")
	geometry, err := page.ElementGeometry(child)
	if err != nil {
		t.Fatal(err)
	}
	if geometry.Rect.X != 0 || geometry.Rect.Width != 100 {
		t.Fatalf("named-area geometry = %#v, want x=0 width=100", geometry.Rect)
	}

	if err := page.document.SetAttribute(gridID, "style", "display:grid;width:100px;grid-template-columns:repeat(2,[slot] 50px [edge]);grid-auto-rows:20px"); err != nil {
		t.Fatal(err)
	}
	if err := page.document.SetAttribute(child.Node, "style", "grid-column:slot / edge"); err != nil {
		t.Fatal(err)
	}
	assertResolvedProperty(t, page, grid, "grid-template-columns", "[slot] 50px [edge slot] 50px [edge]")
	if page.Frame() != nil || !page.Dirty() {
		t.Fatal("named grid computed-style read published a frame or cleared dirtiness")
	}
}

func TestComputedGridTemplateAreasStayLiveAndDriveGeometry(t *testing.T) {
	t.Parallel()

	engine, page, gridID := computedStyleTestPage(t, `<!doctype html><html><body style="margin:0">
		<section id=target style='display:grid;width:200px;grid-template-areas:"head head" "nav main";grid-auto-columns:80px 120px;grid-auto-rows:20px 40px'>
			<header id=head style="grid-area:head"></header><main id=main style="grid-area:main"></main>
		</section>
	</body></html>`)
	defer engine.Close()
	generation := page.DocumentGeneration()
	grid := NodeHandle{Document: generation, Node: gridID}
	head := NodeHandle{Document: generation, Node: mustPageElementID(t, page, "head")}
	main := NodeHandle{Document: generation, Node: mustPageElementID(t, page, "main")}

	assertResolvedProperty(t, page, grid, "grid-template-areas", `"head head" "nav main"`)
	assertResolvedProperty(t, page, grid, "grid-template-columns", "80px 120px")
	for _, property := range []string{"grid-row-start", "grid-column-start", "grid-row-end", "grid-column-end"} {
		assertResolvedProperty(t, page, main, property, "main")
	}
	initial, err := page.ElementGeometry(main)
	if err != nil {
		t.Fatal(err)
	}
	if initial.Rect.X != 80 || initial.Rect.Y != 20 || initial.Rect.Width != 120 || initial.Rect.Height != 40 {
		t.Fatalf("initial named area geometry = %#v", initial.Rect)
	}

	if err := page.document.SetAttribute(gridID, "style", `display:grid;width:200px;grid-template-areas:"head main" "head main";grid-auto-columns:80px 120px;grid-auto-rows:30px 30px`); err != nil {
		t.Fatal(err)
	}
	assertResolvedProperty(t, page, grid, "grid-template-areas", `"head main" "head main"`)
	mutatedHead, err := page.ElementGeometry(head)
	if err != nil {
		t.Fatal(err)
	}
	mutatedMain, err := page.ElementGeometry(main)
	if err != nil {
		t.Fatal(err)
	}
	if mutatedHead.Rect.Width != 80 || mutatedHead.Rect.Height != 60 || mutatedMain.Rect.X != 80 || mutatedMain.Rect.Height != 60 {
		t.Fatalf("mutated area geometry head=%#v main=%#v", mutatedHead.Rect, mutatedMain.Rect)
	}
	if page.Frame() != nil || !page.Dirty() {
		t.Fatal("grid-template-areas reads published a frame or cleared dirtiness")
	}
}

func TestComputedGridAutoRepeatStaysLiveAndCollapsesAutoFitTracks(t *testing.T) {
	t.Parallel()

	engine, page, gridID := computedStyleTestPage(t, `<!doctype html><html><body style="margin:0">
		<section id=target style="display:grid;width:430px;column-gap:10px;grid-template-columns:repeat(auto-fit,100px)"><div id=first></div><div id=second></div></section>
	</body></html>`)
	defer engine.Close()
	generation := page.DocumentGeneration()
	grid := NodeHandle{Document: generation, Node: gridID}
	second := NodeHandle{Document: generation, Node: mustPageElementID(t, page, "second")}

	assertResolvedProperty(t, page, grid, "grid-template-columns", "100px 100px 0px 0px")
	geometry, err := page.ElementGeometry(second)
	if err != nil {
		t.Fatal(err)
	}
	if geometry.Rect.X != 110 || geometry.Rect.Width != 100 {
		t.Fatalf("auto-fit second geometry = %#v", geometry.Rect)
	}

	if err := page.document.SetAttribute(gridID, "style", "display:grid;width:430px;column-gap:10px;grid-template-columns:repeat(auto-fit,[slot] 100px [edge])"); err != nil {
		t.Fatal(err)
	}
	assertResolvedProperty(t, page, grid, "grid-template-columns", "[slot] 100px [edge slot] 100px [edge slot] 0px [edge slot] 0px [edge]")
	if err := page.document.SetAttribute(gridID, "style", "display:grid;width:430px;column-gap:10px;grid-template-columns:repeat(auto-fill,100px)"); err != nil {
		t.Fatal(err)
	}
	assertResolvedProperty(t, page, grid, "grid-template-columns", "100px 100px 100px 100px")
	if err := page.document.SetAttribute(gridID, "style", "display:grid;width:320px;column-gap:10px;grid-template-columns:repeat(auto-fit,100px)"); err != nil {
		t.Fatal(err)
	}
	assertResolvedProperty(t, page, grid, "grid-template-columns", "100px 100px 0px")
	if page.Frame() != nil || !page.Dirty() {
		t.Fatal("auto-repeat computed/layout reads published a frame or cleared dirtiness")
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

func TestComputedGridAlignmentPropertiesDriveLiveGeometry(t *testing.T) {
	t.Parallel()

	engine, page, gridID := computedStyleTestPage(t, `<!doctype html><html><body style="margin:0"><section id=target style="display:grid;width:300px;height:200px;grid-template-columns:50px 50px;grid-template-rows:40px 40px;gap:10px;justify-content:space-between;align-content:center;justify-items:center;align-items:end"><div id=child style="width:20px;height:10px"></div></section></body></html>`)
	defer engine.Close()
	generation := page.DocumentGeneration()
	grid := NodeHandle{Document: generation, Node: gridID}
	child := NodeHandle{Document: generation, Node: mustPageElementID(t, page, "child")}

	for property, expected := range map[string]string{
		"align-content":   "center",
		"align-items":     "end",
		"justify-content": "space-between",
		"justify-items":   "center",
	} {
		assertResolvedProperty(t, page, grid, property, expected)
	}
	initial, err := page.ElementGeometry(child)
	if err != nil {
		t.Fatal(err)
	}
	if initial.Rect.X != 15 || initial.Rect.Y != 85 {
		t.Fatalf("initial aligned child = %#v, want x=15 y=85", initial.Rect)
	}
	if err := page.document.SetAttribute(gridID, "style", "display:grid;width:300px;height:200px;grid-template-columns:50px 50px;grid-template-rows:40px 40px;gap:10px;justify-content:end;align-content:end;justify-items:end;align-items:start"); err != nil {
		t.Fatal(err)
	}
	assertResolvedProperty(t, page, grid, "justify-content", "end")
	assertResolvedProperty(t, page, grid, "align-content", "end")
	mutated, err := page.ElementGeometry(child)
	if err != nil {
		t.Fatal(err)
	}
	if mutated.Rect.X != 220 || mutated.Rect.Y != 110 {
		t.Fatalf("mutated aligned child = %#v, want x=220 y=110", mutated.Rect)
	}
	if page.Frame() != nil || !page.Dirty() {
		t.Fatal("alignment computed/geometry reads published a frame or cleared dirtiness")
	}
}
