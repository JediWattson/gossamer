package render_test

import (
	"image"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	htmlparser "github.com/JediWattson/gossamer/internal/html"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestLayoutResolvesPercentageHeightsFromDefiniteContainingBlocks(t *testing.T) {
	t.Parallel()

	document, layout := percentageHeightLayout(t, `
		<html><body style="margin:0">
			<div id="parent" style="height:200px">
				<div id="half" style="height:50%"></div>
				<div id="calculated" style="height:calc(50% - 10px)"></div>
				<div id="border-box" style="box-sizing:border-box;height:50%;padding:10px;border:5px solid"></div>
				<div id="minimum" style="height:25%;min-height:40%;max-height:60%"></div>
				<div id="maximum" style="height:80%;max-height:60%"></div>
				<div id="nested" style="height:50%"><div id="grandchild" style="height:50%"></div></div>
			</div>
			<div id="constrained-parent" style="height:100px;min-height:200px">
				<div id="constrained-child" style="height:50%"></div>
			</div>
			<div id="auto-parent">
				<div id="indefinite" style="height:50%"></div>
				<div id="indefinite-min" style="min-height:50%"></div>
			</div>
		</body></html>
	`, render.Viewport{Width: 400, Height: 400}, render.Resources{})

	assertPercentageHeightGeometry(t, document, layout, "parent", 200, false)
	assertPercentageHeightGeometry(t, document, layout, "half", 100, true)
	assertPercentageHeightGeometry(t, document, layout, "calculated", 90, true)
	assertPercentageHeightGeometry(t, document, layout, "border-box", 70, true)
	assertPercentageHeightGeometry(t, document, layout, "minimum", 80, true)
	assertPercentageHeightGeometry(t, document, layout, "maximum", 120, true)
	assertPercentageHeightGeometry(t, document, layout, "nested", 100, true)
	assertPercentageHeightGeometry(t, document, layout, "grandchild", 50, true)
	assertPercentageHeightGeometry(t, document, layout, "constrained-parent", 200, false)
	assertPercentageHeightGeometry(t, document, layout, "constrained-child", 100, true)
	assertPercentageHeightGeometry(t, document, layout, "indefinite", 0, false)
	assertPercentageHeightGeometry(t, document, layout, "indefinite-min", 0, false)
}

func TestLayoutPropagatesDefiniteHeightThroughFormattingContexts(t *testing.T) {
	t.Parallel()

	source := `
		<html><body style="margin:0">
			<div id="parent" style="position:relative;height:200px;padding:10px;border:5px solid">
				<span id="atomic" style="display:inline-block;height:50%;width:20px"></span>
				<div id="flex" style="display:flex;height:50%">
					<div id="flex-item" style="height:50%"><div id="flex-grandchild" style="height:50%"></div></div>
				</div>
				<img id="inline-image" style="height:50%">
				<img id="block-image" style="display:block;height:50%">
				<div id="absolute" style="position:absolute;height:50%;width:10px"></div>
			</div>
		</body></html>
	`
	root, err := htmlparser.Parse(strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	document, err := dom.IndexDocument(root)
	if err != nil {
		t.Fatal(err)
	}
	inlineImageID := mustElementID(t, document, "inline-image")
	blockImageID := mustElementID(t, document, "block-image")
	inlineImage, _ := document.Resolve(inlineImageID)
	blockImage, _ := document.Resolve(blockImageID)
	resources := render.Resources{Images: map[*dom.Node]image.Image{
		inlineImage: image.NewRGBA(image.Rect(0, 0, 20, 10)),
		blockImage:  image.NewRGBA(image.Rect(0, 0, 20, 10)),
	}}
	viewport := render.Viewport{Width: 400, Height: 400}
	styles := mustDocumentStyleSnapshot(t, document, viewport, resources)
	var layout *render.LayoutSnapshot
	if err := document.WithReadView(func(view dom.ReadView) error {
		var layoutErr error
		layout, layoutErr = render.ComputeLayoutSnapshotFromReadView(view, viewport, resources, styles)
		return layoutErr
	}); err != nil {
		t.Fatal(err)
	}

	assertPercentageHeightGeometry(t, document, layout, "atomic", 100, true)
	assertPercentageHeightGeometry(t, document, layout, "flex", 100, true)
	assertPercentageHeightGeometry(t, document, layout, "flex-item", 50, true)
	assertPercentageHeightGeometry(t, document, layout, "flex-grandchild", 25, true)
	assertPercentageHeightGeometry(t, document, layout, "inline-image", 100, true)
	assertPercentageHeightGeometry(t, document, layout, "block-image", 100, true)
	// Absolutely positioned percentage heights use the padding-box height of
	// their containing block: 200px content + 20px vertical padding.
	assertPercentageHeightGeometry(t, document, layout, "absolute", 110, true)
}

func TestPercentageHeightResolvesInsideGrownColumnFlexItem(t *testing.T) {
	t.Parallel()

	document, layout := percentageHeightLayout(t, `
		<html><body style="margin:0">
			<section id="container" style="display:flex;flex-direction:column;height:200px">
				<div id="item" style="flex:1;min-height:0">
					<div id="fill" style="display:flex;flex-direction:column;height:100%;min-height:0">
						<div id="main" style="flex:1;min-height:0"></div>
						<div id="footer" style="flex:none;height:40px"></div>
					</div>
				</div>
			</section>
		</body></html>
	`, render.Viewport{Width: 400, Height: 400}, render.Resources{})

	assertPercentageHeightGeometry(t, document, layout, "container", 200, false)
	assertPercentageHeightGeometry(t, document, layout, "item", 200, false)
	assertPercentageHeightGeometry(t, document, layout, "fill", 200, true)
	assertPercentageHeightGeometry(t, document, layout, "main", 160, false)
	assertPercentageHeightGeometry(t, document, layout, "footer", 40, false)
}

func TestRootPercentageHeightRequiresDefiniteRootElementHeight(t *testing.T) {
	t.Parallel()

	definiteDocument, definiteLayout := percentageHeightLayout(t, `
		<html style="height:200px"><body id="target" style="margin:0;height:50%"></body></html>
	`, render.Viewport{Width: 400, Height: 400}, render.Resources{})
	assertPercentageHeightGeometry(t, definiteDocument, definiteLayout, "target", 100, true)

	indefiniteDocument, indefiniteLayout := percentageHeightLayout(t, `
		<html><body id="target" style="margin:0;height:50%"></body></html>
	`, render.Viewport{Width: 400, Height: 400}, render.Resources{})
	assertPercentageHeightGeometry(t, indefiniteDocument, indefiniteLayout, "target", 0, false)
}

func percentageHeightLayout(t *testing.T, source string, viewport render.Viewport, resources render.Resources) (*dom.Document, *render.LayoutSnapshot) {
	t.Helper()
	root, err := htmlparser.Parse(strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	document, err := dom.IndexDocument(root)
	if err != nil {
		t.Fatal(err)
	}
	styles := mustDocumentStyleSnapshot(t, document, viewport, resources)
	var layout *render.LayoutSnapshot
	if err := document.WithReadView(func(view dom.ReadView) error {
		var layoutErr error
		layout, layoutErr = render.ComputeLayoutSnapshotFromReadView(view, viewport, resources, styles)
		return layoutErr
	}); err != nil {
		t.Fatal(err)
	}
	return document, layout
}

func assertPercentageHeightGeometry(t *testing.T, document *dom.Document, layout *render.LayoutSnapshot, id string, want float64, wantResolved bool) {
	t.Helper()
	geometry, ok := layout.GeometryID(mustElementID(t, document, id))
	if !ok {
		t.Fatalf("element %q has no geometry", id)
	}
	if geometry.ContentBounds.Height != want || geometry.PercentHeightResolved != wantResolved {
		t.Fatalf("%s content height/resolved = %v/%t, want %v/%t (geometry %#v)", id, geometry.ContentBounds.Height, geometry.PercentHeightResolved, want, wantResolved, geometry)
	}
}
