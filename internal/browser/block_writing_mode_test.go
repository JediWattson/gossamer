package browser

import "testing"

func TestVerticalBlockGeometryStaysLiveAcrossWritingModeChanges(t *testing.T) {
	t.Parallel()

	engine, page, flowID := computedStyleTestPage(t, `<!doctype html><html><body style="margin:0"><section id=target style="writing-mode:vertical-rl;width:120px;height:200px"><i id=first style="display:block;width:30px;height:50px"></i><i id=second style="display:block;width:40px;height:70px"></i></section></body></html>`)
	defer engine.Close()
	generation := page.DocumentGeneration()
	flow := NodeHandle{Document: generation, Node: flowID}
	first := NodeHandle{Document: generation, Node: mustPageElementID(t, page, "first")}
	second := NodeHandle{Document: generation, Node: mustPageElementID(t, page, "second")}

	geometry := func(handle NodeHandle) DOMElementGeometry {
		t.Helper()
		value, err := page.ElementGeometry(handle)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	assertResolvedProperty(t, page, flow, "writing-mode", "vertical-rl")
	assertResolvedProperty(t, page, flow, "width", "120px")
	assertResolvedProperty(t, page, flow, "height", "200px")
	initialFirst, initialSecond := geometry(first), geometry(second)
	if initialFirst.Rect.X <= initialSecond.Rect.X || initialFirst.Rect.Y != initialSecond.Rect.Y {
		t.Fatalf("initial vertical block geometry = first:%#v second:%#v", initialFirst.Rect, initialSecond.Rect)
	}
	firstLayout := page.layout.snapshot

	if err := page.document.SetAttribute(flowID, "style", "writing-mode:vertical-lr;width:120px;height:200px"); err != nil {
		t.Fatal(err)
	}
	assertResolvedProperty(t, page, flow, "writing-mode", "vertical-lr")
	liveFirst, liveSecond := geometry(first), geometry(second)
	if liveFirst.Rect.X >= liveSecond.Rect.X || liveFirst.Rect.Y != liveSecond.Rect.Y || page.layout.snapshot == firstLayout {
		t.Fatalf("live vertical block geometry = first:%#v second:%#v snapshots:%p/%p", liveFirst.Rect, liveSecond.Rect, page.layout.snapshot, firstLayout)
	}
	if page.Frame() != nil || !page.Dirty() {
		t.Fatal("vertical block geometry read published a frame or cleared dirtiness")
	}
}
