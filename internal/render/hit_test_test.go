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
