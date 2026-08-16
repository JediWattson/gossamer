package browser

import "testing"

func TestTableWrapperSeparatesRootAndCaptionCSSOMGeometry(t *testing.T) {
	t.Parallel()

	engine, page, tableID := computedStyleTestPage(t, `<!doctype html><html><body style="margin:0">
		<table id=target style="border-spacing:0;width:20px;height:30px"><caption style="width:10px;height:20px;padding:0"></caption></table>
	</body></html>`)
	defer engine.Close()
	handle := NodeHandle{Document: page.DocumentGeneration(), Node: tableID}

	geometry, err := page.ElementGeometry(handle)
	if err != nil {
		t.Fatal(err)
	}
	if geometry.Rect.Width != 20 || geometry.Rect.Height != 50 ||
		geometry.OffsetWidth != 20 || geometry.OffsetHeight != 50 ||
		geometry.ClientWidth != 20 || geometry.ClientHeight != 50 ||
		geometry.ScrollWidth != 20 || geometry.ScrollHeight != 50 {
		t.Fatalf("caption-inclusive table geometry = %#v, want 20x50", geometry)
	}
	if len(geometry.ClientRects) != 2 {
		t.Fatalf("table client rect count = %d, want table-root plus caption", len(geometry.ClientRects))
	}
	root, caption := geometry.ClientRects[0], geometry.ClientRects[1]
	if root.X != 0 || root.Y != 20 || root.Width != 20 || root.Height != 30 {
		t.Fatalf("table-root client rect = %#v, want (0,20 20x30)", root)
	}
	if caption.X != 0 || caption.Y != 0 || caption.Width != 10 || caption.Height != 20 {
		t.Fatalf("caption client rect = %#v, want (0,0 10x20)", caption)
	}
	assertResolvedProperty(t, page, handle, "width", "20px")
	assertResolvedProperty(t, page, handle, "height", "30px")
	if page.Frame() != nil || !page.Dirty() {
		t.Fatal("table geometry read published a frame or cleared page dirtiness")
	}
}

func TestTableWrapperIncludesCaptionMarginsAndKeepsMarginSpaceTransparent(t *testing.T) {
	t.Parallel()

	engine, page, tableID := computedStyleTestPage(t, `<!doctype html><html><body style="margin:0">
		<table id=target style="border-spacing:0;width:20px;height:30px"><caption style="height:20px;margin:1px 2px 3px 4px;padding:0"></caption></table>
	</body></html>`)
	defer engine.Close()
	handle := NodeHandle{Document: page.DocumentGeneration(), Node: tableID}

	geometry, err := page.ElementGeometry(handle)
	if err != nil {
		t.Fatal(err)
	}
	if geometry.OffsetWidth != 20 || geometry.OffsetHeight != 54 || geometry.Rect.Width != 20 || geometry.Rect.Height != 53 {
		t.Fatalf("caption-margin table geometry = %#v, want offset 20x54 and rect union 20x53", geometry)
	}
	if len(geometry.ClientRects) != 2 {
		t.Fatalf("table client rect count = %d, want 2", len(geometry.ClientRects))
	}
	if hit, ok := page.HitTest(1, 0.5); ok && hit == handle {
		t.Fatalf("anonymous wrapper margin-space hit returned table: %#v", hit)
	}
}
