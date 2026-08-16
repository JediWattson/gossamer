package browser

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/render"
)

func TestComputedVerticalAlignAndAtomicGeometryAreLiveWithoutPublishingFrame(t *testing.T) {
	t.Parallel()

	engine, page, targetID := computedStyleTestPage(t, `<!doctype html><html><body style="margin:0">
		<div style="font:20px/40px monospace"><span>strut</span><span id="target" style="display:inline-block;width:10px;height:10px;vertical-align:top"></span></div>
	</body></html>`)
	defer engine.Close()
	handle := NodeHandle{Document: page.DocumentGeneration(), Node: targetID}
	host := &taskHost{page: page, generation: page.DocumentGeneration()}

	value, found, err := host.ComputedStyleProperty(handle, "", "vertical-align")
	if err != nil || !found || value != "top" {
		t.Fatalf("initial vertical-align = %q, %t, %v; want top, true, nil", value, found, err)
	}
	top, err := page.ElementGeometry(handle)
	if err != nil {
		t.Fatal(err)
	}
	if err := host.SetStyleProperty(handle, "vertical-align", "bottom", ""); err != nil {
		t.Fatal(err)
	}
	value, found, err = host.ComputedStyleProperty(handle, "", "vertical-align")
	if err != nil || !found || value != "bottom" {
		t.Fatalf("live vertical-align = %q, %t, %v; want bottom, true, nil", value, found, err)
	}
	bottom, err := page.ElementGeometry(handle)
	if err != nil {
		t.Fatal(err)
	}
	if bottom.Rect.Y <= top.Rect.Y {
		t.Fatalf("top/bottom atomic rects = %#v / %#v, want bottom alignment lower", top.Rect, bottom.Rect)
	}
	if page.Frame() != nil || !page.Dirty() {
		t.Fatal("vertical-align style/geometry read published a frame or cleared dirtiness")
	}
	if page.layout.snapshot == nil {
		t.Fatal("vertical-align geometry did not retain an unpublished layout snapshot")
	}
	if err := page.SetViewport(render.Viewport{Width: 640, Height: 480}); err != nil {
		t.Fatal(err)
	}
	if page.Frame() != nil || !page.Dirty() {
		t.Fatal("viewport invalidation unexpectedly published a frame or cleared dirtiness")
	}
}
