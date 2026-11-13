package render_test

import (
	"image"
	"image/color"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestRenderHTMLCanvasDefaultAndScaledPixels(t *testing.T) {
	t.Parallel()

	root := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body", dom.Attribute{Name: "style", Value: "margin:0"})
	defaultCanvas := dom.NewElement("canvas")
	sizedCanvas := dom.NewElement("canvas",
		dom.Attribute{Name: "width", Value: "40"},
		dom.Attribute{Name: "height", Value: "20"},
		dom.Attribute{Name: "style", Value: "display:block;width:80px;height:auto"},
	)
	body.AppendChild(defaultCanvas)
	body.AppendChild(sizedCanvas)
	html.AppendChild(body)
	root.AppendChild(html)

	doc, err := dom.IndexDocument(root)
	if err != nil {
		t.Fatalf("IndexDocument() error = %v", err)
	}
	sizedID, ok := doc.ID(sizedCanvas)
	if !ok {
		t.Fatal("missing sizedCanvas ID")
	}
	if _, _, err := doc.MutateCanvas(sizedID, func(width, height int, bitmap *image.RGBA) bool {
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				bitmap.SetRGBA(x, y, color.RGBA{R: 0x10, G: 0x80, B: 0xe0, A: 0xff})
			}
		}
		return true
	}); err != nil {
		t.Fatalf("MutateCanvas() error = %v", err)
	}

	frame, err := render.Render(root, render.Viewport{Width: 400, Height: 300})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	var defaultFrag *render.ImageFragment
	var sizedBox *render.Box
	var walk func(*render.Box)
	walk = func(box *render.Box) {
		if box == nil {
			return
		}
		for i := range box.Fragments {
			frag := &box.Fragments[i]
			if frag.Kind == render.ImageFragmentKind && frag.Image.Node == defaultCanvas {
				defaultFrag = &frag.Image
			}
		}
		if box.Node == sizedCanvas {
			sizedBox = box
		}
		for _, child := range box.Children {
			walk(child)
		}
	}
	walk(frame.Root)

	if defaultFrag == nil || defaultFrag.Bounds.Width != 300 || defaultFrag.Bounds.Height != 150 {
		t.Fatalf("defaultCanvas inline fragment = %+v, want 300x150", defaultFrag)
	}
	if sizedBox == nil {
		t.Fatal("expected Box for block canvas")
	}
	if sizedBox.Bounds.Width != 80 || sizedBox.Bounds.Height != 40 {
		t.Fatalf("sizedCanvas bounds = %vx%v, want 80x40 (aspect preserved)", sizedBox.Bounds.Width, sizedBox.Bounds.Height)
	}

	raster, err := render.RasterizeAtScale(frame, 1)
	if err != nil {
		t.Fatalf("RasterizeAtScale() error = %v", err)
	}
	got := raster.RGBAAt(40, int(sizedBox.Bounds.Y)+20)
	if got.R != 0x10 || got.G != 0x80 || got.B != 0xe0 || got.A != 0xff {
		t.Fatalf("rasterized canvas pixel = %+v, want #1080e0", got)
	}
}
