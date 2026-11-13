package browser

import "testing"

func TestPageScrollOnlyRenderReusesDocumentSpacePaint(t *testing.T) {
	t.Parallel()

	engine, page, target := computedStyleTestPage(t, `
		<html><body style="margin:0"><div id="target" style="height:1800px">content</div></body></html>
	`)
	defer engine.Close()
	if err := page.Render(); err != nil {
		t.Fatal(err)
	}
	base := page.paint.frame
	initial := page.frame
	if base == nil || initial == nil {
		t.Fatal("initial render did not retain document-space paint")
	}
	changed, err := page.ScrollViewport(0, 120)
	if err != nil || !changed {
		t.Fatalf("ScrollViewport() = changed %t, err %v", changed, err)
	}
	page.mutex.Lock()
	err = page.renderLocked(true)
	page.mutex.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if page.paint.frame != base {
		t.Fatal("scroll-only render rebuilt unchanged document-space paint")
	}
	if page.frame == initial {
		t.Fatal("scroll-only render did not publish a projected frame")
	}
	if page.dirty {
		t.Fatal("scroll-only render left the page dirty")
	}

	if err := page.document.SetAttribute(target, "style", "height:1900px"); err != nil {
		t.Fatal(err)
	}
	page.dirty = true
	page.mutex.Lock()
	err = page.renderLocked(true)
	page.mutex.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if page.paint.frame == base {
		t.Fatal("DOM mutation incorrectly reused stale document-space paint")
	}
}
