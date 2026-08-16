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

func TestPageGeometryObservesCollapsedMarginMutationWithoutPublishingFrame(t *testing.T) {
	t.Parallel()

	engine, page, targetID := computedStyleTestPage(t, `<!doctype html><html><body style="margin:0;padding-top:1px">
		<div style="height:10px;margin-bottom:20px"></div>
		<div id="target" style="height:10px;margin-top:30px"></div>
	</body></html>`)
	defer engine.Close()
	handle := NodeHandle{Document: page.DocumentGeneration(), Node: targetID}

	initial, err := page.ElementGeometry(handle)
	if err != nil {
		t.Fatal(err)
	}
	if initial.Rect.Y != 41 {
		t.Fatalf("initial collapsed rect = %#v, want y=41", initial.Rect)
	}
	firstLayout := page.layout.snapshot
	if err := page.document.SetAttribute(targetID, "style", "height:10px;margin-top:-30px"); err != nil {
		t.Fatal(err)
	}
	after, err := page.ElementGeometry(handle)
	if err != nil {
		t.Fatal(err)
	}
	if after.Rect.Y != 1 {
		t.Fatalf("mixed-sign collapsed rect = %#v, want y=1", after.Rect)
	}
	if page.layout.snapshot == firstLayout {
		t.Fatal("margin mutation reused stale layout")
	}
	if page.Frame() != nil || !page.Dirty() {
		t.Fatal("geometry read published a frame or cleared dirtiness")
	}
}

func TestElementOverflowScrollTranslatesClipsAndHitTestsWithoutRelayout(t *testing.T) {
	engine, page, targetID := computedStyleTestPage(t, `<!doctype html><html><body style="margin:0"><div id="target" style="display:block;height:40px;overflow:auto"><div style="display:block;height:60px"></div><button id="button" style="display:block;height:30px">button</button></div><div style="height:80px"></div></body></html>`)
	defer engine.Close()
	handle := NodeHandle{Document: page.DocumentGeneration(), Node: targetID}
	buttonID, ok := page.document.ElementByID("button")
	if !ok {
		t.Fatal("button id missing")
	}
	buttonHandle := NodeHandle{Document: page.DocumentGeneration(), Node: buttonID}
	containerBefore, err := page.ElementGeometry(handle)
	if err != nil {
		t.Fatal(err)
	}
	buttonBefore, err := page.ElementGeometry(buttonHandle)
	if err != nil {
		t.Fatal(err)
	}
	if containerBefore.ClientHeight != 40 || containerBefore.ScrollHeight < 90 || buttonBefore.Rect.Y < 60 {
		t.Fatalf("initial container/button geometry = %#v / %#v", containerBefore, buttonBefore)
	}
	layout := page.layout.snapshot
	changed, err := page.ScrollElement(handle, 0, 60)
	if err != nil || !changed {
		t.Fatalf("ScrollElement = %t, %v; want true, nil", changed, err)
	}
	containerAfter, _ := page.ElementGeometry(handle)
	buttonAfter, _ := page.ElementGeometry(buttonHandle)
	if containerAfter.ScrollTop != 50 || buttonAfter.Rect.Y != buttonBefore.Rect.Y-50 {
		t.Fatalf("scrolled container/button geometry = %#v / %#v", containerAfter, buttonAfter)
	}
	if page.layout.snapshot != layout {
		t.Fatal("element scroll rebuilt immutable layout")
	}
	if err := page.Render(); err != nil {
		t.Fatal(err)
	}
	if hit, ok := page.HitTest(10, 20); !ok || hit != buttonHandle {
		t.Fatalf("nested-scrolled HitTest = %#v, %t; want %#v", hit, ok, buttonHandle)
	}
	if hit, ok := page.HitTest(10, 45); ok && hit == buttonHandle {
		t.Fatal("hit testing escaped the overflow clip")
	}
}
