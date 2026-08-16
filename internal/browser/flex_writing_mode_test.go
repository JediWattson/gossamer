package browser

import "testing"

func TestVerticalFlexGeometryStaysLiveAcrossWritingModeAndDirectionChanges(t *testing.T) {
	t.Parallel()

	engine, page, flexID := computedStyleTestPage(t, `<!doctype html><html><body style="margin:0">
		<section id=target style="display:flex;writing-mode:vertical-rl;direction:ltr;width:120px;height:200px;align-items:flex-start"><i id=first style="flex:none;width:30px;height:70px"></i><i id=second style="flex:none;width:40px;height:50px"></i></section>
	</body></html>`)
	defer engine.Close()
	generation := page.DocumentGeneration()
	flex := NodeHandle{Document: generation, Node: flexID}
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
	assertResolvedProperty(t, page, flex, "writing-mode", "vertical-rl")
	assertResolvedProperty(t, page, flex, "width", "120px")
	assertResolvedProperty(t, page, flex, "height", "200px")
	initialFirst, initialSecond := geometry(first), geometry(second)
	if initialFirst.Rect.X <= initialSecond.Rect.X || initialFirst.Rect.Y >= initialSecond.Rect.Y {
		t.Fatalf("initial vertical Flex geometry = first:%#v second:%#v", initialFirst.Rect, initialSecond.Rect)
	}
	firstLayout := page.layout.snapshot

	if err := page.document.SetAttribute(flexID, "style", "display:flex;writing-mode:vertical-lr;direction:rtl;width:120px;height:200px;align-items:flex-start"); err != nil {
		t.Fatal(err)
	}
	assertResolvedProperty(t, page, flex, "writing-mode", "vertical-lr")
	liveFirst, liveSecond := geometry(first), geometry(second)
	if liveFirst.Rect.X != liveSecond.Rect.X || liveFirst.Rect.Y <= liveSecond.Rect.Y || page.layout.snapshot == firstLayout {
		t.Fatalf("live vertical Flex geometry = first:%#v second:%#v snapshots:%p/%p", liveFirst.Rect, liveSecond.Rect, page.layout.snapshot, firstLayout)
	}
	if page.Frame() != nil || !page.Dirty() {
		t.Fatal("vertical Flex geometry read published a frame or cleared dirtiness")
	}
}
