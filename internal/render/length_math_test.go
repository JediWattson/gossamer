package render_test

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
	"github.com/JediWattson/gossamer/internal/style"
)

func TestLayoutResolvesTypedLengthMathAgainstBoxAndViewport(t *testing.T) {
	t.Parallel()

	document, targetID := indexedLayoutDocument(t, `
		<html><body style="margin:0">
			<div id="target" style="
				--gutter: 20px;
				width: calc(50% - var(--gutter));
				height: min(50vh, 100px);
				padding: max(5px, 2vw);
				border: clamp(1px, 1vw, 6px) solid;
			"></div>
		</body></html>
	`)
	viewport := render.Viewport{Width: 400, Height: 240}
	styles := mustDocumentStyleSnapshot(t, document, viewport, render.Resources{})

	computed, ok := styles.LookupID(targetID)
	if !ok {
		t.Fatal("target has no computed style")
	}
	for property, want := range map[string]string{
		"width":             "calc(50% - 20px)",
		"height":            "min(50vh, 100px)",
		"padding-left":      "max(5px, 2vw)",
		"border-left-width": "clamp(1px, 1vw, 6px)",
	} {
		got, found := style.ComputedPropertyValue(computed, property)
		if !found || got != want {
			t.Errorf("computed %s = %q, %t, want %q, true", property, got, found, want)
		}
	}

	var layout *render.LayoutSnapshot
	if err := document.WithReadView(func(view dom.ReadView) error {
		var layoutErr error
		layout, layoutErr = render.ComputeLayoutSnapshotFromReadView(view, viewport, render.Resources{}, styles)
		return layoutErr
	}); err != nil {
		t.Fatal(err)
	}
	geometry, ok := layout.GeometryID(targetID)
	if !ok {
		t.Fatal("target has no layout geometry")
	}
	// 50% of 400 - 20 = 180 content px; padding resolves to 8px and
	// border width to 4px on each side. Height is min(120px, 100px).
	if geometry.ContentBounds.Width != 180 || geometry.ContentBounds.Height != 100 {
		t.Fatalf("content bounds = %#v, want 180x100", geometry.ContentBounds)
	}
	paddingLeft := geometry.ContentBounds.X - geometry.ClientBounds.X
	borderLeft := geometry.ClientBounds.X - geometry.Bounds.X
	if paddingLeft != 8 || borderLeft != 4 {
		t.Fatalf("padding/border = %v/%v, want 8px/4px", paddingLeft, borderLeft)
	}
	if geometry.Bounds.Width != 204 || geometry.Bounds.Height != 124 {
		t.Fatalf("border bounds = %#v, want 204x124", geometry.Bounds)
	}
}

func TestNegativeCalculatedPaddingClampsAtUsedValueTime(t *testing.T) {
	t.Parallel()

	document, targetID := indexedLayoutDocument(t, `
		<html><body style="margin:0">
			<div id="target" style="width:100px;padding-left:calc(10% - 80px)"></div>
		</body></html>
	`)
	viewport := render.Viewport{Width: 400, Height: 200}
	styles := mustDocumentStyleSnapshot(t, document, viewport, render.Resources{})
	var layout *render.LayoutSnapshot
	if err := document.WithReadView(func(view dom.ReadView) error {
		var layoutErr error
		layout, layoutErr = render.ComputeLayoutSnapshotFromReadView(view, viewport, render.Resources{}, styles)
		return layoutErr
	}); err != nil {
		t.Fatal(err)
	}
	geometry, ok := layout.GeometryID(targetID)
	if !ok {
		t.Fatal("target has no layout geometry")
	}
	if geometry.ContentBounds.X-geometry.ClientBounds.X != 0 || geometry.Bounds.Width != 100 {
		t.Fatalf("negative calculated padding produced geometry %#v", geometry)
	}
}
