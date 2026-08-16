package browser

import (
	"image"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
	"github.com/JediWattson/gossamer/internal/resource"
)

func TestPageComputedStylePropertyResolvesWidthAndSupportedHeightFromCachedLayout(t *testing.T) {
	t.Parallel()

	engine, page, target := computedStyleTestPage(t, `
		<html><body style="margin:0">
			<div id="target" style="width:50%;height:40px;padding:10px;border:2px solid">target</div>
			<div id="auto" style="width:auto;height:auto"></div>
			<div id="viewport" style="width:25vw;height:25vh"></div>
			<div id="percent-height" style="height:50%"></div>
			<div id="calculated-percent-height" style="height:calc(50% - 10px)"></div>
			<div id="definite-height-parent" style="height:200px">
				<div id="resolved-percent-height" style="height:50%"></div>
				<div id="resolved-calculated-percent-height" style="height:calc(50% - 10px)"></div>
				<div id="resolved-border-box-percent-height" style="box-sizing:border-box;height:50%;padding:10px;border:5px solid"></div>
			</div>
			<div id="border-box" style="box-sizing:border-box;width:100px;height:80px;padding:10px;border:5px solid"></div>
			<span id="inline" style="width:50px;height:20px">inline</span>
			<span id="inline-block" style="display:inline-block;width:60px;height:30px">inline block</span>
			<span id="inline-block-auto" style="display:inline-block"><span style="display:block;width:40px;height:10px"></span></span>
			<div id="none" style="display:none;width:70px;height:40px"></div>
			<img id="image">
		</body></html>
	`)
	defer engine.Close()

	generation := page.DocumentGeneration()
	assertResolvedProperty(t, page, NodeHandle{Document: generation, Node: target}, "width", "400px")
	firstLayout := page.layout.snapshot
	if firstLayout == nil {
		t.Fatal("resolved width did not cache layout")
	}
	assertResolvedProperty(t, page, NodeHandle{Document: generation, Node: target}, "height", "40px")
	if page.layout.snapshot != firstLayout {
		t.Fatal("repeated resolved reads recomputed unchanged layout")
	}
	if page.Frame() != nil || !page.Dirty() {
		t.Fatal("resolved reads published a frame or cleared dirtiness")
	}

	auto := NodeHandle{Document: generation, Node: mustPageElementID(t, page, "auto")}
	assertResolvedProperty(t, page, auto, "width", "800px")
	assertResolvedProperty(t, page, auto, "height", "0px")
	viewport := NodeHandle{Document: generation, Node: mustPageElementID(t, page, "viewport")}
	assertResolvedProperty(t, page, viewport, "width", "200px")
	assertResolvedProperty(t, page, viewport, "height", "150px")
	assertResolvedProperty(t, page, NodeHandle{Document: generation, Node: mustPageElementID(t, page, "percent-height")}, "height", "50%")
	assertResolvedProperty(t, page, NodeHandle{Document: generation, Node: mustPageElementID(t, page, "calculated-percent-height")}, "height", "calc(50% - 10px)")
	assertResolvedProperty(t, page, NodeHandle{Document: generation, Node: mustPageElementID(t, page, "resolved-percent-height")}, "height", "100px")
	assertResolvedProperty(t, page, NodeHandle{Document: generation, Node: mustPageElementID(t, page, "resolved-calculated-percent-height")}, "height", "90px")
	assertResolvedProperty(t, page, NodeHandle{Document: generation, Node: mustPageElementID(t, page, "resolved-border-box-percent-height")}, "height", "100px")
	borderBox := NodeHandle{Document: generation, Node: mustPageElementID(t, page, "border-box")}
	assertResolvedProperty(t, page, borderBox, "width", "100px")
	assertResolvedProperty(t, page, borderBox, "height", "80px")
	assertResolvedProperty(t, page, NodeHandle{Document: generation, Node: mustPageElementID(t, page, "inline")}, "width", "50px")
	assertResolvedProperty(t, page, NodeHandle{Document: generation, Node: mustPageElementID(t, page, "inline-block")}, "width", "60px")
	inlineBlockAuto := NodeHandle{Document: generation, Node: mustPageElementID(t, page, "inline-block-auto")}
	assertResolvedProperty(t, page, inlineBlockAuto, "width", "40px")
	assertResolvedProperty(t, page, inlineBlockAuto, "height", "10px")
	assertResolvedProperty(t, page, NodeHandle{Document: generation, Node: mustPageElementID(t, page, "none")}, "width", "70px")

	if err := page.Render(); err != nil {
		t.Fatal(err)
	}
	if page.layout.snapshot != firstLayout {
		t.Fatal("render replaced rather than reused the current layout snapshot")
	}
	if page.Frame() == nil || page.Frame().Layout != firstLayout || page.Frame().ComputedStyles != firstLayout.ComputedStyles() || page.Dirty() {
		t.Fatal("render did not publish the cached layout coherently")
	}

	oldFrame := page.Frame()
	imageID := mustPageElementID(t, page, "image")
	imageNode, ok := page.document.Resolve(imageID)
	if !ok {
		t.Fatal("image node is missing")
	}
	if err := page.SetResources(render.Resources{Images: map[*dom.Node]image.Image{
		imageNode: image.NewRGBA(image.Rect(0, 0, 13, 7)),
	}}); err != nil {
		t.Fatal(err)
	}
	assertResolvedProperty(t, page, NodeHandle{Document: generation, Node: imageID}, "width", "13px")
	assertResolvedProperty(t, page, NodeHandle{Document: generation, Node: imageID}, "height", "7px")
	if page.layout.snapshot == firstLayout {
		t.Fatal("image resource change reused stale layout")
	}
	if page.Frame() != oldFrame || !page.Dirty() {
		t.Fatal("resource-backed resolved read published or replaced the old frame")
	}
}

func TestPageComputedStylePropertyTracksPercentageHeightDefinitenessWithinSameTask(t *testing.T) {
	t.Parallel()

	engine, page, _ := computedStyleTestPage(t, `
		<html><body style="margin:0">
			<div id="parent"><div id="target" style="height:50%"></div></div>
		</body></html>
	`)
	defer engine.Close()
	generation := page.DocumentGeneration()
	parentID := mustPageElementID(t, page, "parent")
	target := NodeHandle{Document: generation, Node: mustPageElementID(t, page, "target")}

	assertResolvedProperty(t, page, target, "height", "50%")
	indefiniteLayout := page.layout.snapshot
	if indefiniteLayout == nil {
		t.Fatal("indefinite percentage-height read did not consult layout")
	}
	if err := page.document.SetAttribute(parentID, "style", "height:200px"); err != nil {
		t.Fatal(err)
	}
	assertResolvedProperty(t, page, target, "height", "100px")
	definiteLayout := page.layout.snapshot
	if definiteLayout == indefiniteLayout {
		t.Fatal("definite parent mutation reused stale layout")
	}
	if err := page.document.SetAttribute(parentID, "style", "height:300px"); err != nil {
		t.Fatal(err)
	}
	assertResolvedProperty(t, page, target, "height", "150px")
	if page.layout.snapshot == definiteLayout {
		t.Fatal("second definite parent mutation reused stale layout")
	}
	if err := page.document.SetAttribute(parentID, "style", "height:auto"); err != nil {
		t.Fatal(err)
	}
	assertResolvedProperty(t, page, target, "height", "50%")
	if page.Frame() != nil || !page.Dirty() {
		t.Fatal("same-task percentage-height reads published a frame or cleared dirtiness")
	}
}

func TestPageComputedStylePropertyInvalidatesLayoutForDOMAndViewportChanges(t *testing.T) {
	t.Parallel()

	engine, page, target := computedStyleTestPage(t, `
		<html><head><style>#target { width:25vw; height:25vh; }</style></head>
		<body style="margin:0"><div id="target"></div></body></html>
	`)
	defer engine.Close()

	handle := NodeHandle{Document: page.DocumentGeneration(), Node: target}
	assertResolvedProperty(t, page, handle, "width", "200px")
	firstLayout := page.layout.snapshot
	if err := page.document.SetAttribute(target, "style", "width:50%;height:20px"); err != nil {
		t.Fatal(err)
	}
	assertResolvedProperty(t, page, handle, "width", "400px")
	assertResolvedProperty(t, page, handle, "height", "20px")
	if page.layout.snapshot == firstLayout {
		t.Fatal("DOM version change reused stale layout")
	}
	mutatedLayout := page.layout.snapshot

	if err := page.SetViewport(render.Viewport{Width: 400, Height: 200}); err != nil {
		t.Fatal(err)
	}
	assertResolvedProperty(t, page, handle, "width", "200px")
	if page.layout.snapshot == mutatedLayout {
		t.Fatal("viewport change reused stale layout")
	}
	if page.Frame() != nil || !page.Dirty() {
		t.Fatal("same-task invalidation plus resolved reads changed frame publication state")
	}
}

func TestPageComputedStylePropertyKeepsNonGeometryReadsStyleOnly(t *testing.T) {
	t.Parallel()

	engine, page, target := computedStyleTestPage(t, `<html><body><div id="target" style="color:#123456;width:10px"></div></body></html>`)
	defer engine.Close()
	handle := NodeHandle{Document: page.DocumentGeneration(), Node: target}

	assertResolvedProperty(t, page, handle, "color", "rgb(18, 52, 86)")
	if page.layout.snapshot != nil {
		t.Fatal("layout-independent computed property unexpectedly flushed layout")
	}
	assertResolvedProperty(t, page, handle, "width", "10px")
	if page.layout.snapshot == nil {
		t.Fatal("geometry-dependent computed property did not flush layout")
	}
}

func TestNavigationImageArrivalInvalidatesLayoutWithoutInvalidatingStyle(t *testing.T) {
	t.Parallel()

	engine, page, _ := computedStyleTestPage(t, `<html><body><img id="image"><div id="target"></div></body></html>`)
	defer engine.Close()
	imageID := mustPageElementID(t, page, "image")
	page.navigation = navigationRecord{
		id:                 91,
		state:              NavigationLoadingResources,
		documentGeneration: page.DocumentGeneration(),
		resourcesPending:   2,
	}
	page.dirty = false
	styleRevision := page.styleRevision
	layoutRevision := page.layoutRevision

	err := page.applyNavigationResource(nil, 91, page.DocumentGeneration(), navigationResourceResult{
		target: NodeHandle{Document: page.DocumentGeneration(), Node: imageID},
		kind:   resource.Image,
		image:  image.NewRGBA(image.Rect(0, 0, 11, 5)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.styleRevision != styleRevision {
		t.Fatalf("image arrival changed style revision from %d to %d", styleRevision, page.styleRevision)
	}
	if page.layoutRevision != layoutRevision+1 || !page.Dirty() {
		t.Fatalf("image arrival layout revision/dirty = %d/%t, want %d/true", page.layoutRevision, page.Dirty(), layoutRevision+1)
	}
	assertResolvedProperty(t, page, NodeHandle{Document: page.DocumentGeneration(), Node: imageID}, "width", "11px")
}

func assertResolvedProperty(t *testing.T, page *Page, handle NodeHandle, property, want string) {
	t.Helper()
	got, found, err := page.ComputedStyleProperty(handle, property)
	if err != nil {
		t.Fatalf("ComputedStyleProperty(%q): %v", property, err)
	}
	if !found || got != want {
		t.Fatalf("ComputedStyleProperty(%q) = %q, %t; want %q, true", property, got, found, want)
	}
}

func mustPageElementID(t *testing.T, page *Page, id string) dom.NodeID {
	t.Helper()
	node, ok := page.document.ElementByID(id)
	if !ok {
		t.Fatalf("element %q is missing", id)
	}
	return node
}
