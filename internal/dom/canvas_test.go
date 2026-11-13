package dom

import (
	"image"
	"image/color"
	"testing"
)

func TestCanvasDimensionsAndMutationLifecycle(t *testing.T) {
	t.Parallel()

	root := NewDocument()
	html := NewElement("html")
	body := NewElement("body")
	canvas := NewElement("canvas")
	body.AppendChild(canvas)
	html.AppendChild(body)
	root.AppendChild(html)

	doc, err := IndexDocument(root)
	if err != nil {
		t.Fatalf("IndexDocument() error = %v", err)
	}
	canvasID, ok := doc.ID(canvas)
	if !ok {
		t.Fatal("missing canvas ID")
	}

	w, h, rev, err := doc.CanvasSize(canvasID)
	if err != nil || w != DefaultCanvasWidth || h != DefaultCanvasHeight || rev != 0 {
		t.Fatalf("initial CanvasSize = (%d,%d,rev=%d,err=%v)", w, h, rev, err)
	}

	rev, changed, err := doc.MutateCanvas(canvasID, func(width, height int, bitmap *image.RGBA) bool {
		if width != 300 || height != 150 || bitmap == nil {
			t.Fatalf("MutateCanvas got (%d,%d,%v)", width, height, bitmap != nil)
		}
		bitmap.SetRGBA(5, 6, color.RGBA{R: 255, G: 0, B: 0, A: 255})
		return true
	})
	if err != nil || !changed || rev != 1 {
		t.Fatalf("MutateCanvas = (rev=%d, changed=%v, err=%v)", rev, changed, err)
	}

	snap1 := canvas.CanvasImage()
	if snap1 == nil || snap1.At(5, 6) != (color.RGBA{R: 255, G: 0, B: 0, A: 255}) {
		t.Fatalf("unexpected CanvasImage pixel: %v", snap1)
	}

	// Subsequent mutation must invalidate snapshot without mutating snap1's data.
	_, changed, err = doc.MutateCanvas(canvasID, func(width, height int, bitmap *image.RGBA) bool {
		bitmap.SetRGBA(5, 6, color.RGBA{R: 0, G: 255, B: 0, A: 255})
		return true
	})
	if err != nil || !changed {
		t.Fatalf("second MutateCanvas failed: changed=%v err=%v", changed, err)
	}
	if snap1.At(5, 6) != (color.RGBA{R: 255, G: 0, B: 0, A: 255}) {
		t.Fatalf("previous immutable snapshot was mutated: %v", snap1.At(5, 6))
	}
	snap2 := canvas.CanvasImage()
	if snap2 == nil || snap2.At(5, 6) != (color.RGBA{R: 0, G: 255, B: 0, A: 255}) {
		t.Fatalf("updated CanvasImage pixel mismatch: %v", snap2)
	}

	// Setting width resets and clears backing bitmap even to same parsed dimension.
	w, h, rev, err = doc.SetCanvasDimension(canvasID, "width", "120")
	if err != nil || w != 120 || h != 150 || rev <= 2 {
		t.Fatalf("SetCanvasDimension = (%d,%d,rev=%d,err=%v)", w, h, rev, err)
	}
	if canvas.CanvasImage() != nil {
		// Re-allocated zeroed bitmap only happens lazily on draw; snapshot should be nil.
		t.Fatalf("expected nil CanvasImage after resize reset")
	}

	// Cloning a canvas copies attributes but keeps bitmap independent.
	cloneID, err := doc.CloneNode(canvasID, false)
	if err != nil {
		t.Fatalf("CloneNode() error = %v", err)
	}
	cw, ch, crev, err := doc.CanvasSize(cloneID)
	if err != nil || cw != 120 || ch != 150 || crev != 0 {
		t.Fatalf("cloned CanvasSize = (%d,%d,rev=%d,err=%v)", cw, ch, crev, err)
	}
}
