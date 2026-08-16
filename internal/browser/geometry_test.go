package browser

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestPageGeometryAndRootScrollReuseLayoutAndTranslateHitTesting(t *testing.T) {
	engine, page, targetID := computedStyleTestPage(t, `<!doctype html><html><body style="margin:0">
		<div style="height:180px">before</div>
		<button id="target" style="display:block;height:40px">target</button>
		<div style="height:180px">after</div>
	</body></html>`)
	defer engine.Close()
	if err := page.SetViewport(render.Viewport{Width: 200, Height: 100}); err != nil {
		t.Fatal(err)
	}

	handle := NodeHandle{Document: page.DocumentGeneration(), Node: targetID}
	initial, err := page.ElementGeometry(handle)
	if err != nil {
		t.Fatal(err)
	}
	if initial.Rect.Y != 180 || initial.Rect.Width != 200 || initial.Rect.Height != 40 {
		t.Fatalf("initial target rect = %#v", initial.Rect)
	}
	rootID, found, err := page.document.RelatedNode(page.document.RootID(), dom.DocumentElement)
	if err != nil || !found {
		t.Fatalf("document element = %d, %t, %v", rootID, found, err)
	}
	root, err := page.ElementGeometry(NodeHandle{Document: page.DocumentGeneration(), Node: rootID})
	if err != nil {
		t.Fatal(err)
	}
	if root.ClientWidth != 200 || root.ClientHeight != 100 || root.ScrollHeight < 400 {
		t.Fatalf("root geometry = %#v", root)
	}
	layout := page.layout.snapshot
	if layout == nil || page.Frame() != nil || !page.Dirty() {
		t.Fatal("geometry read did not retain an unpublished layout snapshot")
	}

	changed, err := page.ScrollViewport(0, 180)
	if err != nil || !changed {
		t.Fatalf("ScrollViewport = %t, %v", changed, err)
	}
	after, err := page.ElementGeometry(handle)
	if err != nil {
		t.Fatal(err)
	}
	if after.Rect.Y != 0 || page.layout.snapshot != layout {
		t.Fatalf("scrolled rect/layout = %#v/%p, want y=0 and %p", after.Rect, page.layout.snapshot, layout)
	}
	if err := page.Render(); err != nil {
		t.Fatal(err)
	}
	if page.layout.snapshot != layout || page.Frame() == nil || page.Frame().Layout != layout {
		t.Fatal("scroll render did not reuse retained layout")
	}
	if hit, ok := page.HitTest(10, 10); !ok || hit != handle {
		t.Fatalf("scrolled HitTest = %#v, %t; want %#v", hit, ok, handle)
	}
}

func TestNonRootElementsRemainNonScrollableUntilOverflowFormattingLands(t *testing.T) {
	engine, page, targetID := computedStyleTestPage(t, `<!doctype html><html><body><div id="target" style="height:20px"><div style="height:80px"></div></div></body></html>`)
	defer engine.Close()
	handle := NodeHandle{Document: page.DocumentGeneration(), Node: targetID}
	changed, err := page.ScrollElement(handle, 0, 20)
	if err != nil || changed {
		t.Fatalf("ScrollElement = %t, %v; want false, nil", changed, err)
	}
	geometry, err := page.ElementGeometry(handle)
	if err != nil {
		t.Fatal(err)
	}
	if geometry.ScrollLeft != 0 || geometry.ScrollTop != 0 {
		t.Fatalf("non-root scroll offset = %g,%g", geometry.ScrollLeft, geometry.ScrollTop)
	}
}
