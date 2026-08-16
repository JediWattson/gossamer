package render_test

import (
	"errors"
	"image"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	htmlparser "github.com/JediWattson/gossamer/internal/html"
	"github.com/JediWattson/gossamer/internal/render"
	"github.com/JediWattson/gossamer/internal/style"
)

func TestLayoutSnapshotIndexesStableContentGeometryAndPaintsWithoutRelayout(t *testing.T) {
	t.Parallel()

	document, target := indexedLayoutDocument(t, `
		<html><body style="margin:0">
			<div id="target" style="width:50%;height:40px;padding:10px;border:2px solid">text</div>
		</body></html>
	`)
	viewport := render.Viewport{Width: 400, Height: 240}
	var styles = mustDocumentStyleSnapshot(t, document, viewport, render.Resources{})
	var layout *render.LayoutSnapshot
	err := document.WithReadView(func(view dom.ReadView) error {
		var layoutErr error
		layout, layoutErr = render.ComputeLayoutSnapshotFromReadView(view, viewport, render.Resources{}, styles)
		return layoutErr
	})
	if err != nil {
		t.Fatal(err)
	}
	if layout.ComputedStyles() != styles {
		t.Fatal("layout does not retain the exact computed-style snapshot")
	}
	if layout.Viewport() != viewport || layout.DocumentIdentity() != document.Identity() || layout.Version() != document.Version() {
		t.Fatalf("layout identity = viewport %#v, document %#v, version %d", layout.Viewport(), layout.DocumentIdentity(), layout.Version())
	}
	geometry, ok := layout.GeometryID(target)
	if !ok {
		t.Fatal("target has no stable layout geometry")
	}
	if geometry.ContentBounds.Width != 200 || geometry.ContentBounds.Height != 40 {
		t.Fatalf("content bounds = %#v, want 200x40", geometry.ContentBounds)
	}
	if geometry.Bounds.Width != 224 || geometry.Bounds.Height != 64 {
		t.Fatalf("border bounds = %#v, want 224x64", geometry.Bounds)
	}

	var frame *render.Frame
	err = document.WithReadView(func(view dom.ReadView) error {
		var renderErr error
		frame, renderErr = render.RenderReadViewWithLayoutSnapshot(view, layout)
		return renderErr
	})
	if err != nil {
		t.Fatal(err)
	}
	if frame.Layout != layout || frame.ComputedStyles != styles || frame.Viewport != viewport || len(frame.DisplayList.Commands) == 0 {
		t.Fatal("painting the cached layout did not produce a coherent frame")
	}
}

func TestLayoutSnapshotOnlyIndexesPrincipalBoxesAndInlineReplacedContent(t *testing.T) {
	t.Parallel()

	document, _ := indexedLayoutDocument(t, `
		<html><body style="margin:0">
			<span id="inline" style="width:50px;height:20px">inline</span>
			<span id="inline-block" style="display:inline-block;width:60px;height:30px">inline block</span>
			<div id="none" style="display:none;width:70px;height:40px"></div>
			<img id="image">
		</body></html>
	`)
	inline := mustElementID(t, document, "inline")
	inlineBlock := mustElementID(t, document, "inline-block")
	none := mustElementID(t, document, "none")
	imageID := mustElementID(t, document, "image")
	imageNode, ok := document.Resolve(imageID)
	if !ok {
		t.Fatal("image node is missing")
	}
	viewport := render.Viewport{Width: 400, Height: 240}
	resources := render.Resources{Images: map[*dom.Node]image.Image{
		imageNode: image.NewRGBA(image.Rect(0, 0, 13, 7)),
	}}
	styles := mustDocumentStyleSnapshot(t, document, viewport, resources)
	var layout *render.LayoutSnapshot
	err := document.WithReadView(func(view dom.ReadView) error {
		var layoutErr error
		layout, layoutErr = render.ComputeLayoutSnapshotFromReadView(view, viewport, resources, styles)
		return layoutErr
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, id := range map[string]dom.NodeID{"inline": inline, "display:none": none} {
		if _, found := layout.GeometryID(id); found {
			t.Errorf("%s unexpectedly has principal geometry", name)
		}
	}
	inlineBlockGeometry, found := layout.GeometryID(inlineBlock)
	if !found || inlineBlockGeometry.ContentBounds.Width != 60 || inlineBlockGeometry.ContentBounds.Height != 30 {
		t.Fatalf("inline-block geometry = %#v, %t; want 60x30 principal content box", inlineBlockGeometry, found)
	}
	geometry, found := layout.GeometryID(imageID)
	if !found || geometry.ContentBounds.Width != 13 || geometry.ContentBounds.Height != 7 {
		t.Fatalf("inline image geometry = %#v, %t; want 13x7", geometry, found)
	}
}

func TestLayoutSnapshotRejectsStaleDocumentVersion(t *testing.T) {
	t.Parallel()

	document, target := indexedLayoutDocument(t, `<html><body><div id="target"></div></body></html>`)
	viewport := render.Viewport{Width: 320, Height: 200}
	styles := mustDocumentStyleSnapshot(t, document, viewport, render.Resources{})
	var layout *render.LayoutSnapshot
	if err := document.WithReadView(func(view dom.ReadView) error {
		var layoutErr error
		layout, layoutErr = render.ComputeLayoutSnapshotFromReadView(view, viewport, render.Resources{}, styles)
		return layoutErr
	}); err != nil {
		t.Fatal(err)
	}
	if err := document.SetAttribute(target, "style", "width:10px"); err != nil {
		t.Fatal(err)
	}
	err := document.WithReadView(func(view dom.ReadView) error {
		_, renderErr := render.RenderReadViewWithLayoutSnapshot(view, layout)
		return renderErr
	})
	if err == nil || !strings.Contains(err.Error(), "layout snapshot version") {
		t.Fatalf("stale layout error = %v, want version mismatch", err)
	}
}

func TestLayoutSnapshotRejectsExpiredViewAndDifferentDocument(t *testing.T) {
	t.Parallel()

	first, _ := indexedLayoutDocument(t, `<html><body><div id="target"></div></body></html>`)
	second, _ := indexedLayoutDocument(t, `<html><body><div id="target"></div></body></html>`)
	viewport := render.Viewport{Width: 320, Height: 200}
	styles := mustDocumentStyleSnapshot(t, first, viewport, render.Resources{})
	var layout *render.LayoutSnapshot
	var expired dom.ReadView
	if err := first.WithReadView(func(view dom.ReadView) error {
		expired = view
		var layoutErr error
		layout, layoutErr = render.ComputeLayoutSnapshotFromReadView(view, viewport, render.Resources{}, styles)
		return layoutErr
	}); err != nil {
		t.Fatal(err)
	}
	if computed, err := render.ComputeLayoutSnapshotFromReadView(expired, viewport, render.Resources{}, styles); !errors.Is(err, dom.ErrExpiredReadView) || computed != nil {
		t.Fatalf("ComputeLayoutSnapshotFromReadView(expired) = %v, %v; want nil, ErrExpiredReadView", computed, err)
	}
	if frame, err := render.RenderReadViewWithLayoutSnapshot(expired, layout); !errors.Is(err, dom.ErrExpiredReadView) || frame != nil {
		t.Fatalf("RenderReadViewWithLayoutSnapshot(expired) = %v, %v; want nil, ErrExpiredReadView", frame, err)
	}
	err := second.WithReadView(func(view dom.ReadView) error {
		_, renderErr := render.RenderReadViewWithLayoutSnapshot(view, layout)
		return renderErr
	})
	if err == nil || !strings.Contains(err.Error(), "different document") {
		t.Fatalf("cross-document layout error = %v, want document mismatch", err)
	}
}

func indexedLayoutDocument(t *testing.T, source string) (*dom.Document, dom.NodeID) {
	t.Helper()
	root, err := htmlparser.Parse(strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	document, err := dom.IndexDocument(root)
	if err != nil {
		t.Fatal(err)
	}
	target, _ := document.ElementByID("target")
	return document, target
}

func mustElementID(t *testing.T, document *dom.Document, id string) dom.NodeID {
	t.Helper()
	node, ok := document.ElementByID(id)
	if !ok {
		t.Fatalf("element %q is missing", id)
	}
	return node
}

func mustDocumentStyleSnapshot(t *testing.T, document *dom.Document, viewport render.Viewport, resources render.Resources) *style.Snapshot {
	t.Helper()
	snapshot, err := render.ComputeDocumentStyleSnapshot(document, viewport, resources)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}
