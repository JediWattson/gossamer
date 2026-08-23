package render

import (
	"math"

	"github.com/JediWattson/gossamer/internal/dom"
)

// HitTest returns the topmost DOM node painted at a CSS-pixel coordinate.
// Pointer identity remains internal to the renderer; Page immediately converts
// the result into a document-generation-safe NodeHandle.
func HitTest(frame *Frame, x, y float64) *dom.Node {
	if frame == nil || frame.Root == nil || x < 0 || y < 0 ||
		x >= float64(frame.Viewport.Width) || y >= float64(frame.Viewport.Height) {
		return nil
	}
	return hitTestBox(frame.Root, x, y, frame.Viewport)
}

// HitTestDocument resolves a point already translated into document
// coordinates. Page performs the viewport clip before applying root scroll.
func HitTestDocument(frame *Frame, x, y float64) *dom.Node {
	if frame == nil || frame.Root == nil {
		return nil
	}
	return hitTestBox(frame.Root, x, y, frame.Viewport)
}

// HitTestVisual resolves a point against boxes and fragments after applying
// the same Page-owned scroll transforms and nested scrollport clips used by
// paint. Layout remains in immutable document coordinates.
func HitTestVisual(frame *Frame, x, y float64, transforms map[*dom.Node]VisualTransform) *dom.Node {
	if frame == nil || frame.Root == nil || x < 0 || y < 0 ||
		x >= float64(frame.Viewport.Width) || y >= float64(frame.Viewport.Height) {
		return nil
	}
	return hitTestVisualBox(frame.Root, x, y, transforms, frame.Viewport)
}

func hitTestVisualBox(box *Box, x, y float64, transforms map[*dom.Node]VisualTransform, viewport Viewport) *dom.Node {
	if box == nil {
		return nil
	}
	boxTransform := transforms[box.Node]
	if boxTransform.HasClip && !containsPoint(boxTransform.Clip, x, y) {
		return nil
	}
	visualBounds := translatedRect(box.Bounds, boxTransform)
	if box.hasStyle && hasUsedTransform(box.style) {
		var ok bool
		x, y, ok = inverseTransformPoint(usedTransform(box.style, visualBounds, viewport), x, y)
		if !ok {
			return nil
		}
	}
	if box.hasClipBounds && !containsPoint(translatedRect(box.clipBounds, boxTransform), x, y) {
		return nil
	}
	if box.hasStyle && clipsOverflow(box.style) {
		clip := translatedRect(boxClientBounds(box), boxTransform)
		radii := insetCornerRadii(usedCornerRadii(box.style, visualBounds, viewport), box.Border)
		if !pointInRoundedRect(clip, radii, x, y) {
			if !box.hitTransparent && containsPoint(visualBounds, x, y) {
				return box.Node
			}
			return nil
		}
	}
	negative, nonNegative := positionedPaintChildren(box)
	for index := len(nonNegative) - 1; index >= 0; index-- {
		if node := hitTestVisualBox(nonNegative[index], x, y, transforms, viewport); node != nil {
			return node
		}
	}
	if len(box.flow) != 0 {
		for index := len(box.flow) - 1; index >= 0; index-- {
			item := box.flow[index]
			if item.box != nil {
				if node := hitTestVisualBox(item.box, x, y, transforms, viewport); node != nil {
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
			if node := hitTestVisualBox(box.Children[index], x, y, transforms, viewport); node != nil {
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
		if node := hitTestVisualBox(negative[index], x, y, transforms, viewport); node != nil {
			return node
		}
	}
	if !box.hitTransparent && containsPoint(translatedRect(box.Bounds, boxTransform), x, y) {
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

func hitTestBox(box *Box, x, y float64, viewport Viewport) *dom.Node {
	if box == nil {
		return nil
	}
	if box.hasStyle && hasUsedTransform(box.style) {
		var ok bool
		x, y, ok = inverseTransformPoint(usedTransform(box.style, box.Bounds, viewport), x, y)
		if !ok {
			return nil
		}
	}
	if box.hasClipBounds && !containsPoint(box.clipBounds, x, y) {
		return nil
	}
	if box.hasStyle && clipsOverflow(box.style) {
		clip := boxClientBounds(box)
		radii := insetCornerRadii(usedCornerRadii(box.style, box.Bounds, viewport), box.Border)
		if !pointInRoundedRect(clip, radii, x, y) {
			if !box.hitTransparent && containsPoint(box.Bounds, x, y) {
				return box.Node
			}
			return nil
		}
	}
	negative, nonNegative := positionedPaintChildren(box)
	for index := len(nonNegative) - 1; index >= 0; index-- {
		if node := hitTestBox(nonNegative[index], x, y, viewport); node != nil {
			return node
		}
	}
	if len(box.flow) != 0 {
		for index := len(box.flow) - 1; index >= 0; index-- {
			item := box.flow[index]
			if item.box != nil {
				if node := hitTestBox(item.box, x, y, viewport); node != nil {
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
			if node := hitTestBox(box.Children[index], x, y, viewport); node != nil {
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
		if node := hitTestBox(negative[index], x, y, viewport); node != nil {
			return node
		}
	}
	if !box.hitTransparent && containsPoint(box.Bounds, x, y) {
		return box.Node
	}
	return nil
}

func inverseTransformPoint(transform AffineTransform, x, y float64) (float64, float64, bool) {
	determinant := transform.A*transform.D - transform.B*transform.C
	if !isFinite(determinant) || math.Abs(determinant) < 1e-12 {
		return 0, 0, false
	}
	x -= transform.E
	y -= transform.F
	return (transform.D*x - transform.C*y) / determinant,
		(-transform.B*x + transform.A*y) / determinant, true
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
