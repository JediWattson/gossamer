package browser

import "testing"

func TestComputedWritingModeInheritanceAndMutationStayLive(t *testing.T) {
	t.Parallel()

	engine, page, parentID := computedStyleTestPage(t, `<!doctype html><html><body>
		<section id=target style="writing-mode:vertical-rl"><div id=child></div></section>
	</body></html>`)
	defer engine.Close()
	generation := page.DocumentGeneration()
	parent := NodeHandle{Document: generation, Node: parentID}
	child := NodeHandle{Document: generation, Node: mustPageElementID(t, page, "child")}

	assertResolvedProperty(t, page, parent, "writing-mode", "vertical-rl")
	assertResolvedProperty(t, page, child, "writing-mode", "vertical-rl")
	if page.Frame() != nil || !page.Dirty() {
		t.Fatal("computed writing-mode read published a frame or cleared dirtiness")
	}
	if err := page.document.SetAttribute(parentID, "style", "writing-mode:vertical-lr"); err != nil {
		t.Fatal(err)
	}
	assertResolvedProperty(t, page, parent, "writing-mode", "vertical-lr")
	assertResolvedProperty(t, page, child, "writing-mode", "vertical-lr")
	if page.Frame() != nil || !page.Dirty() {
		t.Fatal("live writing-mode read published a frame or cleared dirtiness")
	}
}

func TestVerticalTableGeometryTracksLiveWritingModeAndDirection(t *testing.T) {
	t.Parallel()

	engine, page, tableID := computedStyleTestPage(t, `<!doctype html><html><body style="margin:0">
		<table id=target style="writing-mode:vertical-rl;direction:ltr;width:70px;height:130px;border-spacing:10px 5px">
			<col style="height:40px"><col style="height:60px">
			<tr id=first style="width:25px"><td id=a>A</td><td id=b>B</td></tr>
			<tr id=second style="width:30px"><td>C</td><td>D</td></tr>
		</table>
	</body></html>`)
	defer engine.Close()
	generation := page.DocumentGeneration()
	table := NodeHandle{Document: generation, Node: tableID}
	first := NodeHandle{Document: generation, Node: mustPageElementID(t, page, "first")}
	second := NodeHandle{Document: generation, Node: mustPageElementID(t, page, "second")}
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
	initialTable := geometry(table)
	initialFirst, initialSecond := geometry(first), geometry(second)
	initialA, initialB := geometry(a), geometry(b)
	if initialTable.Rect.Width != 70 || initialTable.Rect.Height != 130 || initialFirst.Rect.X <= initialSecond.Rect.X || initialA.Rect.Y >= initialB.Rect.Y {
		t.Fatalf("initial vertical-rl geometry = table:%#v rows:%#v/%#v columns:%#v/%#v", initialTable.Rect, initialFirst.Rect, initialSecond.Rect, initialA.Rect, initialB.Rect)
	}
	firstLayout := page.layout.snapshot

	if err := page.document.SetAttribute(tableID, "style", "writing-mode:vertical-lr;direction:rtl;width:70px;height:130px;border-spacing:10px 5px"); err != nil {
		t.Fatal(err)
	}
	assertResolvedProperty(t, page, table, "writing-mode", "vertical-lr")
	assertResolvedProperty(t, page, table, "width", "70px")
	assertResolvedProperty(t, page, table, "height", "130px")
	liveFirst, liveSecond := geometry(first), geometry(second)
	liveA, liveB := geometry(a), geometry(b)
	if liveFirst.Rect.X >= liveSecond.Rect.X || liveA.Rect.Y <= liveB.Rect.Y || page.layout.snapshot == firstLayout {
		t.Fatalf("live vertical-lr/rtl geometry = rows:%#v/%#v columns:%#v/%#v snapshots:%p/%p", liveFirst.Rect, liveSecond.Rect, liveA.Rect, liveB.Rect, page.layout.snapshot, firstLayout)
	}
	if page.Frame() != nil || !page.Dirty() {
		t.Fatal("vertical table geometry read published a frame or cleared dirtiness")
	}
}
