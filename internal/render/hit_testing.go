package render

import "github.com/JediWattson/gossamer/internal/dom"

// HitTest returns the topmost DOM node painted at a CSS-pixel coordinate.
// Pointer identity remains internal to the renderer; Page immediately converts
// the result into a document-generation-safe NodeHandle.
func HitTest(frame *Frame, x, y float64) *dom.Node {
	if frame == nil || frame.Root == nil || x < 0 || y < 0 ||
		x >= float64(frame.Viewport.Width) || y >= float64(frame.Viewport.Height) {
		return nil
	}
	return hitTestBox(frame.Root, x, y)
}

func hitTestBox(box *Box, x, y float64) *dom.Node {
	if box == nil {
		return nil
	}
	for index := len(box.Children) - 1; index >= 0; index-- {
		if node := hitTestBox(box.Children[index], x, y); node != nil {
			return node
		}
	}
	for index := len(box.Fragments) - 1; index >= 0; index-- {
		fragment := box.Fragments[index]
		switch fragment.Kind {
		case ImageFragmentKind:
			if containsPoint(fragment.Image.Bounds, x, y) {
				return fragment.Image.Node
			}
		case TextFragmentKind:
			bounds := Rect{
				X:      fragment.Text.X,
				Y:      fragment.Text.BaselineY - fragment.Text.Height,
				Width:  fragment.Text.Width,
				Height: fragment.Text.Height,
			}
			if containsPoint(bounds, x, y) {
				return fragment.Text.Node
			}
		}
	}
	if containsPoint(box.Bounds, x, y) {
		return box.Node
	}
	return nil
}

func containsPoint(rectangle Rect, x, y float64) bool {
	return rectangle.Width > 0 && rectangle.Height > 0 &&
		x >= rectangle.X && y >= rectangle.Y &&
		x < rectangle.X+rectangle.Width && y < rectangle.Y+rectangle.Height
}
