package browser

import "testing"

func TestComputedBorderLineStylesStayLiveThroughGeometry(t *testing.T) {
	t.Parallel()

	engine, page, targetID := computedStyleTestPage(t, `<!doctype html><html><body style="margin:0">
		<div id=target style="width:40px;height:20px;border:6px dotted #6480a0"></div>
	</body></html>`)
	defer engine.Close()
	handle := NodeHandle{Document: page.DocumentGeneration(), Node: targetID}

	assertResolvedProperty(t, page, handle, "border-top-style", "dotted")
	geometry, err := page.ElementGeometry(handle)
	if err != nil {
		t.Fatal(err)
	}
	if geometry.Rect.Width != 52 || geometry.Rect.Height != 32 {
		t.Fatalf("dotted border geometry = %#v, want 52x32", geometry.Rect)
	}
	firstLayout := page.layout.snapshot
	if err := page.document.SetAttribute(targetID, "style", "width:40px;height:20px;border:6px double #6480a0"); err != nil {
		t.Fatal(err)
	}
	assertResolvedProperty(t, page, handle, "border-top-style", "double")
	geometry, err = page.ElementGeometry(handle)
	if err != nil {
		t.Fatal(err)
	}
	if geometry.Rect.Width != 52 || geometry.Rect.Height != 32 || page.layout.snapshot == firstLayout {
		t.Fatalf("live double border geometry/layout = %#v/%p, previous %p", geometry.Rect, page.layout.snapshot, firstLayout)
	}
	if page.Frame() != nil || !page.Dirty() {
		t.Fatal("border-style computed/geometry reads published a frame or cleared dirtiness")
	}
}
