package render_test

import (
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	htmlparser "github.com/JediWattson/gossamer/internal/html"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestHitTestReturnsPaintedTextAndRejectsOutsideViewport(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`
		<html><body style="margin:0"><button style="display:block;width:120px;height:40px">click me</button></body></html>
	`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 200, Height: 100})
	if err != nil {
		t.Fatal(err)
	}
	node := render.HitTest(frame, 4, 10)
	if node == nil {
		t.Fatal("HitTest() returned nil inside button")
	}
	for node.Type != dom.ElementNode && node.Parent != nil {
		node = node.Parent
	}
	if node.Type != dom.ElementNode || node.Data != "button" {
		t.Fatalf("HitTest() normalized node = %#v, want button", node)
	}
	if node := render.HitTest(frame, -1, 10); node != nil {
		t.Fatalf("HitTest() outside viewport = %#v, want nil", node)
	}
}

func TestHitTestPointerEventsNonePassthroughAndAutoOverride(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`
		<html><body style="margin:0;position:relative">
			<textarea id="composer" style="position:absolute;left:0;top:0;width:200px;height:60px">msg</textarea>
			<div id="overlay" style="position:absolute;left:0;top:0;width:200px;height:60px;pointer-events:none">
				overlay text
				<button id="pill" style="position:absolute;left:120px;top:10px;width:60px;height:20px;pointer-events:auto">send</button>
			</div>
		</body></html>
	`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 240, Height: 100})
	if err != nil {
		t.Fatal(err)
	}

	wantComposer := findStaticPageElementByID(document, "composer")
	wantPill := findStaticPageElementByID(document, "pill")

	normalizeElement := func(node *dom.Node) *dom.Node {
		for node != nil && node.Type != dom.ElementNode {
			node = node.Parent
		}
		return node
	}

	composer := normalizeElement(render.HitTest(frame, 10, 8))
	if composer != wantComposer {
		t.Fatalf("HitTest(10, 8) through pointer-events:none overlay = %#v, want #composer", composer)
	}
	visualComposer := normalizeElement(render.HitTestVisual(frame, 10, 8, nil))
	if visualComposer != wantComposer {
		t.Fatalf("HitTestVisual(10, 8) through pointer-events:none overlay = %#v, want #composer", visualComposer)
	}

	pill := normalizeElement(render.HitTest(frame, 130, 18))
	if pill != wantPill {
		t.Fatalf("HitTest(130, 18) on pointer-events:auto child = %#v, want #pill", pill)
	}
	visualPill := normalizeElement(render.HitTestVisual(frame, 130, 18, nil))
	if visualPill != wantPill {
		t.Fatalf("HitTestVisual(130, 18) on pointer-events:auto child = %#v, want #pill", visualPill)
	}
}
