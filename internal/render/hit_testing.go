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

// HitTestDocument resolves a point already translated into document
// coordinates. Page performs the viewport clip before applying root scroll.
func HitTestDocument(frame *Frame, x, y float64) *dom.Node {
	if frame == nil || frame.Root == nil {
		return nil
	}
	return hitTestBox(frame.Root, x, y)
}

// HitTestVisual resolves a point against boxes and fragments after applying
// the same Page-owned scroll transforms and nested scrollport clips used by
// paint. Layout remains in immutable document coordinates.
func HitTestVisual(frame *Frame, x, y float64, transforms map[*dom.Node]VisualTransform) *dom.Node {
	if frame == nil || frame.Root == nil || x < 0 || y < 0 ||
		x >= float64(frame.Viewport.Width) || y >= float64(frame.Viewport.Height) {
		return nil
	}
	return hitTestVisualBox(frame.Root, x, y, transforms)
}

func hitTestVisualBox(box *Box, x, y float64, transforms map[*dom.Node]VisualTransform) *dom.Node {
	if box == nil {
		return nil
	}
	boxTransform := transforms[box.Node]
	if boxTransform.HasClip && !containsPoint(boxTransform.Clip, x, y) {
		return nil
	}
	negative, nonNegative := positionedPaintChildren(box)
	for index := len(nonNegative) - 1; index >= 0; index-- {
		if node := hitTestVisualBox(nonNegative[index], x, y, transforms); node != nil {
			return node
		}
	}
	if len(box.flow) != 0 {
		for index := len(box.flow) - 1; index >= 0; index-- {
			item := box.flow[index]
			if item.box != nil {
				if node := hitTestVisualBox(item.box, x, y, transforms); node != nil {
					return node
				}
				continue
			}
			if node := hitTestVisualFragment(item.fragment, x, y, transforms); node != nil {
				return node
			}
		}
	} else {
		for index := len(box.Children) - 1; index >= 0; index-- {
			if box.Children[index].positioned {
				continue
			}
			if node := hitTestVisualBox(box.Children[index], x, y, transforms); node != nil {
				return node
			}
		}
		for index := len(box.Fragments) - 1; index >= 0; index-- {
			if node := hitTestVisualFragment(box.Fragments[index], x, y, transforms); node != nil {
				return node
			}
		}
	}
	for index := len(negative) - 1; index >= 0; index-- {
		if node := hitTestVisualBox(negative[index], x, y, transforms); node != nil {
			return node
		}
	}
	if containsPoint(translatedRect(box.Bounds, boxTransform), x, y) {
		return box.Node
	}
	return nil
}

func hitTestVisualFragment(fragment InlineFragment, x, y float64, transforms map[*dom.Node]VisualTransform) *dom.Node {
	switch fragment.Kind {
	case ImageFragmentKind:
		transform := transforms[fragment.Image.Node]
		if (!transform.HasClip || containsPoint(transform.Clip, x, y)) && containsPoint(translatedRect(fragment.Image.Bounds, transform), x, y) {
			return fragment.Image.Node
		}
	case TextFragmentKind:
		transform := transforms[fragment.Text.Node]
		bounds := textFragmentBounds(fragment.Text)
		if (!transform.HasClip || containsPoint(transform.Clip, x, y)) && containsPoint(translatedRect(bounds, transform), x, y) {
			return fragment.Text.Node
		}
	}
	return nil
}

func translatedRect(rectangle Rect, transform VisualTransform) Rect {
	rectangle.X -= transform.OffsetX
	rectangle.Y -= transform.OffsetY
	return rectangle
}

func hitTestBox(box *Box, x, y float64) *dom.Node {
	if box == nil {
		return nil
	}
	negative, nonNegative := positionedPaintChildren(box)
	for index := len(nonNegative) - 1; index >= 0; index-- {
		if node := hitTestBox(nonNegative[index], x, y); node != nil {
			return node
		}
	}
	if len(box.flow) != 0 {
		for index := len(box.flow) - 1; index >= 0; index-- {
			item := box.flow[index]
			if item.box != nil {
				if node := hitTestBox(item.box, x, y); node != nil {
					return node
				}
				continue
			}
			if node := hitTestFragment(item.fragment, x, y); node != nil {
				return node
			}
		}
	} else {
		for index := len(box.Children) - 1; index >= 0; index-- {
			if box.Children[index].positioned {
				continue
			}
			if node := hitTestBox(box.Children[index], x, y); node != nil {
				return node
			}
		}
		for index := len(box.Fragments) - 1; index >= 0; index-- {
			if node := hitTestFragment(box.Fragments[index], x, y); node != nil {
				return node
			}
		}
	}
	for index := len(negative) - 1; index >= 0; index-- {
		if node := hitTestBox(negative[index], x, y); node != nil {
			return node
		}
	}
	if containsPoint(box.Bounds, x, y) {
		return box.Node
	}
	return nil
}

func hitTestFragment(fragment InlineFragment, x, y float64) *dom.Node {
	switch fragment.Kind {
	case ImageFragmentKind:
		if containsPoint(fragment.Image.Bounds, x, y) {
			return fragment.Image.Node
		}
	case TextFragmentKind:
		bounds := textFragmentBounds(fragment.Text)
		if containsPoint(bounds, x, y) {
			return fragment.Text.Node
		}
	}
	return nil
}

func containsPoint(rectangle Rect, x, y float64) bool {
	return rectangle.Width > 0 && rectangle.Height > 0 &&
		x >= rectangle.X && y >= rectangle.Y &&
		x < rectangle.X+rectangle.Width && y < rectangle.Y+rectangle.Height
}
