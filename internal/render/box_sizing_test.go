package render_test

import (
	"image"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestBoxSizingControlsSpecifiedWidthAndHeightGeometry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		boxSizing   string
		wantContent render.Rect
		wantBounds  render.Rect
	}{
		{
			name:        "content box",
			boxSizing:   "content-box",
			wantContent: render.Rect{X: 15, Y: 15, Width: 100, Height: 50},
			wantBounds:  render.Rect{Width: 130, Height: 80},
		},
		{
			name:        "border box",
			boxSizing:   "border-box",
			wantContent: render.Rect{X: 15, Y: 15, Width: 70, Height: 20},
			wantBounds:  render.Rect{Width: 100, Height: 50},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document, body := boxModelDocument()
			target := dom.NewElement("div", dom.Attribute{Name: "style", Value: "width:100px;height:50px;padding:10px;border:5px solid;box-sizing:" + test.boxSizing})
			body.AppendChild(target)
			frame, err := render.Render(document, render.Viewport{Width: 300, Height: 200})
			if err != nil {
				t.Fatal(err)
			}
			box := findBox(frame.Root, target)
			if box == nil {
				t.Fatal("target box not found")
			}
			if box.ContentBounds != test.wantContent || box.Bounds != test.wantBounds {
				t.Fatalf("content/bounds = %#v/%#v, want %#v/%#v", box.ContentBounds, box.Bounds, test.wantContent, test.wantBounds)
			}
		})
	}
}

func TestBorderBoxMinMaxSizesConstrainTheBorderBox(t *testing.T) {
	t.Parallel()

	document, body := boxModelDocument()
	target := dom.NewElement("div", dom.Attribute{Name: "style", Value: `
		box-sizing:border-box;
		width:200px;min-width:120px;max-width:140px;
		height:200px;min-height:80px;max-height:100px;
		padding:10px;border:5px solid;
	`})
	body.AppendChild(target)
	frame, err := render.Render(document, render.Viewport{Width: 300, Height: 240})
	if err != nil {
		t.Fatal(err)
	}
	box := findBox(frame.Root, target)
	if box == nil {
		t.Fatal("target box not found")
	}
	if box.ContentBounds.Width != 110 || box.ContentBounds.Height != 70 {
		t.Fatalf("content bounds = %#v, want 110x70", box.ContentBounds)
	}
	if box.Bounds.Width != 140 || box.Bounds.Height != 100 {
		t.Fatalf("border bounds = %#v, want 140x100", box.Bounds)
	}

	document, body = boxModelDocument()
	target = dom.NewElement("div", dom.Attribute{Name: "style", Value: `
		box-sizing:border-box;width:20px;min-width:120px;max-width:100px;
		height:auto;min-height:80px;max-height:60px;padding:10px;border:5px solid;
	`})
	body.AppendChild(target)
	frame, err = render.Render(document, render.Viewport{Width: 300, Height: 240})
	if err != nil {
		t.Fatal(err)
	}
	box = findBox(frame.Root, target)
	if box.Bounds.Width != 120 || box.Bounds.Height != 80 {
		t.Fatalf("minimum did not win conflicting constraints: %#v", box.Bounds)
	}
}

func TestBorderBoxSizingAppliesToBlockReplacedContent(t *testing.T) {
	t.Parallel()

	document, body := boxModelDocument()
	target := dom.NewElement("img", dom.Attribute{Name: "style", Value: "display:block;width:100px;height:80px;padding:10px;border:5px solid;box-sizing:border-box"})
	body.AppendChild(target)
	frame, err := render.RenderWithResources(document, render.Viewport{Width: 300, Height: 200}, render.Resources{Images: map[*dom.Node]image.Image{
		target: image.NewRGBA(image.Rect(0, 0, 200, 100)),
	}})
	if err != nil {
		t.Fatal(err)
	}
	box := findBox(frame.Root, target)
	if box == nil {
		t.Fatal("image box not found")
	}
	if box.ContentBounds.Width != 70 || box.ContentBounds.Height != 50 || box.Bounds.Width != 100 || box.Bounds.Height != 80 {
		t.Fatalf("image content/bounds = %#v/%#v, want 70x50/100x80", box.ContentBounds, box.Bounds)
	}
}
