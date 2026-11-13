package render

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"math"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/math/f64"
	"golang.org/x/image/vector"
)

type rasterLayerKind uint8

const (
	rasterBaseLayer rasterLayerKind = iota
	rasterOpacityLayer
	rasterTransformLayer
	rasterClipLayer
)

const maxRasterPixels = 64 * 1024 * 1024

func scaledViewport(viewport Viewport, scale float64) (Viewport, error) {
	if !isFinite(scale) || scale <= 0 || scale > 8 {
		return Viewport{}, fmt.Errorf("render: invalid device scale %g", scale)
	}
	width := int(math.Round(float64(viewport.Width) * scale))
	height := int(math.Round(float64(viewport.Height) * scale))
	if width <= 0 || height <= 0 || int64(width)*int64(height) > maxRasterPixels {
		return Viewport{}, fmt.Errorf("render: scaled viewport %dx%d exceeds %d pixels", width, height, maxRasterPixels)
	}
	return Viewport{Width: width, Height: height}, nil
}

// scaleRasterCommand projects one immutable CSS-pixel command into device
// pixels. Callers deliberately keep the projected command on the stack: a
// Command retains fixed-size gradient and clip arrays and is large enough that
// materializing a second display list causes meaningful per-frame churn.
func scaleRasterCommand(command *Command, scale float64) {
	if scale == 1 {
		return
	}
	command.Rect = scaleRect(command.Rect, scale)
	command.Radii = scaleCornerRadii(command.Radii, scale)
	command.BorderWidths = scaleEdges(command.BorderWidths, scale)
	command.ShadowOffsetX *= scale
	command.ShadowOffsetY *= scale
	command.ShadowBlur *= scale
	command.ShadowSpread *= scale
	command.Transform.E *= scale
	command.Transform.F *= scale
	command.X *= scale
	command.BaselineY *= scale
	command.FontSize *= scale
	command.Clip = scaleRect(command.Clip, scale)
	for clipIndex := 0; clipIndex < int(command.RoundedClipCount); clipIndex++ {
		command.RoundedClips[clipIndex].Rect = scaleRect(command.RoundedClips[clipIndex].Rect, scale)
		command.RoundedClips[clipIndex].Radii = scaleCornerRadii(command.RoundedClips[clipIndex].Radii, scale)
	}
	command.textBounds = scaleRect(command.textBounds, scale)
	command.textWidth *= scale
	command.textHeight *= scale
	command.textBaseline *= scale
}

func scaleRect(rectangle Rect, scale float64) Rect {
	return Rect{X: rectangle.X * scale, Y: rectangle.Y * scale, Width: rectangle.Width * scale, Height: rectangle.Height * scale}
}

func scaleEdges(edges Edges, scale float64) Edges {
	return Edges{Top: edges.Top * scale, Right: edges.Right * scale, Bottom: edges.Bottom * scale, Left: edges.Left * scale}
}

func scaleCornerRadii(radii CornerRadii, scale float64) CornerRadii {
	scaleRadius := func(radius CornerRadius) CornerRadius {
		return CornerRadius{X: radius.X * scale, Y: radius.Y * scale}
	}
	return CornerRadii{
		TopLeft: scaleRadius(radii.TopLeft), TopRight: scaleRadius(radii.TopRight),
		BottomRight: scaleRadius(radii.BottomRight), BottomLeft: scaleRadius(radii.BottomLeft),
	}
}

type rasterLayer struct {
	canvas      *image.RGBA
	shapeBounds image.Rectangle
	kind        rasterLayerKind
	direct      bool
	opacity     float64
	transform   AffineTransform
	clip        Rect
	radii       CornerRadii
	command     int
	commandEnd  int
	cacheable   bool
	cacheOutput image.Rectangle
}

func rasterize(displayList DisplayList, fonts *fontBook) (*image.RGBA, error) {
	if displayList.Viewport.Width <= 0 || displayList.Viewport.Height <= 0 {
		return nil, fmt.Errorf(
			"render: invalid viewport %dx%d",
			displayList.Viewport.Width,
			displayList.Viewport.Height,
		)
	}
	viewport, err := scaledViewport(displayList.Viewport, 1)
	if err != nil {
		return nil, err
	}
	return rasterizeAtScale(displayList, fonts, 1, viewport)
}

func rasterizeAtScale(displayList DisplayList, fonts *fontBook, scale float64, viewport Viewport) (*image.RGBA, error) {
	bounds := image.Rect(0, 0, viewport.Width, viewport.Height)
	return rasterizeAtScaleInto(displayList, fonts, scale, viewport, nil, bounds)
}

// rasterizeAtScaleWithUnderlayCapture paints one complete frame and snapshots
// the base framebuffer immediately before captureAt. The caller only supplies
// group-safe boundaries, so the snapshot contains the exact prefix result even
// when a direct rectangular clip still views the same backing pixels.
func rasterizeAtScaleWithUnderlayCapture(displayList DisplayList, fonts *fontBook, scale float64, viewport Viewport, captureAt int, paintCache *rasterOverlayPaintCache) (*image.RGBA, *image.RGBA, error) {
	if captureAt <= 0 || captureAt > len(displayList.Commands) {
		return nil, nil, fmt.Errorf("render: invalid underlay capture boundary %d", captureAt)
	}
	bounds := image.Rect(0, 0, viewport.Width, viewport.Height)
	var underlay *image.RGBA
	canvas, err := rasterizeAtScaleIntoRangeWithGroupsCapture(
		displayList, fonts, scale, viewport, nil, bounds, nil,
		0, len(displayList.Commands), captureAt, &underlay, paintCache, captureAt,
	)
	if err != nil {
		return nil, nil, err
	}
	if underlay == nil {
		return nil, nil, fmt.Errorf("render: underlay capture boundary %d was not reached", captureAt)
	}
	return canvas, underlay, nil
}

// rasterizeAtScaleInto replays the complete ordered display list into one
// clipped region of an existing framebuffer. Replaying every command keeps
// z-order and transparency exact while bounding actual paint work to damage.
func rasterizeAtScaleInto(displayList DisplayList, fonts *fontBook, scale float64, viewport Viewport, target *image.RGBA, paintBounds image.Rectangle) (*image.RGBA, error) {
	return rasterizeAtScaleIntoWithGroups(displayList, fonts, scale, viewport, target, paintBounds, nil)
}

func rasterizeAtScaleIntoWithGroups(displayList DisplayList, fonts *fontBook, scale float64, viewport Viewport, target *image.RGBA, paintBounds image.Rectangle, groupBounds map[int]image.Rectangle) (*image.RGBA, error) {
	return rasterizeAtScaleIntoRangeWithGroups(displayList, fonts, scale, viewport, target, paintBounds, groupBounds, 0, len(displayList.Commands))
}

// rasterizeAtScaleIntoRangeWithGroups preserves every structural command but
// paints only pixel commands in [paintStart, paintEnd). This lets retained
// scrolling keep a document underlay separate from a trailing stationary
// overlay without changing group, clip, or command ordering semantics.
func rasterizeAtScaleIntoRangeWithGroups(displayList DisplayList, fonts *fontBook, scale float64, viewport Viewport, target *image.RGBA, paintBounds image.Rectangle, groupBounds map[int]image.Rectangle, paintStart, paintEnd int) (*image.RGBA, error) {
	return rasterizeAtScaleIntoRangeWithGroupsCapture(
		displayList, fonts, scale, viewport, target, paintBounds, groupBounds,
		paintStart, paintEnd, -1, nil, nil, -1,
	)
}

func rasterizeAtScaleIntoRangeWithGroupsOverlayCache(displayList DisplayList, fonts *fontBook, scale float64, viewport Viewport, target *image.RGBA, paintBounds image.Rectangle, groupBounds map[int]image.Rectangle, paintStart, paintEnd int, paintCache *rasterOverlayPaintCache) (*image.RGBA, error) {
	return rasterizeAtScaleIntoRangeWithGroupsCapture(
		displayList, fonts, scale, viewport, target, paintBounds, groupBounds,
		paintStart, paintEnd, -1, nil, paintCache, paintStart,
	)
}

func rasterizeAtScaleIntoRangeWithGroupsCapture(displayList DisplayList, fonts *fontBook, scale float64, viewport Viewport, target *image.RGBA, paintBounds image.Rectangle, groupBounds map[int]image.Rectangle, paintStart, paintEnd, captureAt int, capture **image.RGBA, paintCache *rasterOverlayPaintCache, cacheStart int) (*image.RGBA, error) {
	bounds := image.Rect(0, 0, viewport.Width, viewport.Height)
	if target == nil {
		target = image.NewRGBA(bounds)
	} else if target.Bounds() != bounds {
		return nil, fmt.Errorf("render: framebuffer bounds %v do not match viewport %v", target.Bounds(), bounds)
	}
	paintBounds = paintBounds.Intersect(bounds)
	if paintBounds.Empty() {
		return target, nil
	}
	canvas := target.SubImage(paintBounds).(*image.RGBA)
	if groupBounds == nil {
		groupBounds = rasterGroupBoundsAtScaleRange(displayList.Commands, bounds, scale, paintStart, paintEnd)
	}
	var layerStorage [8]rasterLayer
	layerStorage[0] = rasterLayer{canvas: canvas, shapeBounds: bounds, kind: rasterBaseLayer, opacity: 1}
	layers := layerStorage[:1]
	var clipMasks map[commandClipMaskKey]*image.Alpha
	verticalTextPixels := 0
	verticalTextUnits := 0
	for commandIndex := 0; commandIndex < len(displayList.Commands); commandIndex++ {
		if capture != nil && *capture == nil && commandIndex == captureAt {
			*capture = cloneRGBA(target)
		}
		command := displayList.Commands[commandIndex]
		scaleRasterCommand(&command, scale)
		cacheCommand := paintCache != nil && commandIndex >= cacheStart
		if commandPaintsPixels(command.Kind) && (commandIndex < paintStart || commandIndex >= paintEnd) {
			continue
		}
		currentLayer := layers[len(layers)-1]
		canvas := currentLayer.canvas
		shapeBounds := currentLayer.shapeBounds
		if commandPaintsPixels(command.Kind) && !hasTransformLayer(layers) {
			commandBounds, valid := rasterCommandBounds(command, bounds)
			if !valid || rectImageBounds(commandBounds).Intersect(canvas.Bounds()).Empty() {
				continue
			}
		}
		switch command.Kind {
		case FillRectCommand:
			if !command.Radii.empty() || commandNeedsScratch(command) {
				target, scratch := commandPaintTarget(canvas, command)
				if command.Radii.empty() {
					rectangle := rectImageBounds(command.Rect).Intersect(target.Bounds())
					drawRGBAUniformOver(target, rectangle, command.Color)
				} else if scratch || !cacheCommand || !paintCache.coverage.paint(target, command.Rect, command.Radii, command.Color, commandShapeModeBounds(command, shapeBounds, false)) {
					paintRoundedRectWithMode(target, command.Rect, command.Radii, command.Color, commandShapeModeBounds(command, shapeBounds, scratch))
				}
				if scratch {
					compositeCommandScratch(canvas, target, command, &clipMasks, commandShapeModeBounds(command, shapeBounds, true))
				}
				continue
			}
			rectangle := image.Rect(
				int(math.Floor(command.Rect.X)),
				int(math.Floor(command.Rect.Y)),
				int(math.Ceil(command.Rect.X+command.Rect.Width)),
				int(math.Ceil(command.Rect.Y+command.Rect.Height)),
			).Intersect(canvas.Bounds()).Intersect(commandClip(command, canvas.Bounds()))
			if rectangle.Empty() {
				continue
			}
			drawRGBAUniformOver(canvas, rectangle, command.Color)

		case FillEllipseCommand:
			if command.Rect.Width <= 0 || command.Rect.Height <= 0 || command.Color.A == 0 {
				continue
			}
			target, scratch := commandPaintTarget(canvas, command)
			paintEllipseWithMode(target, command.Rect, command.Color, commandShapeModeBounds(command, shapeBounds, scratch))
			if scratch {
				compositeCommandScratch(canvas, target, command, &clipMasks, commandShapeModeBounds(command, shapeBounds, true))
			}

		case DrawTextCommand:
			target, scratch := commandPaintTarget(canvas, command)
			var err error
			if command.textOrientation == textPaintSidewaysRight {
				var width, height int
				width, height, err = sidewaysTextDimensions(command)
				if err == nil {
					verticalTextPixels += width * height
					if verticalTextPixels > maxVerticalTextTotalPixels {
						err = fmt.Errorf("render: vertical text exceeds %d total pixels", maxVerticalTextTotalPixels)
					} else {
						err = drawSidewaysText(target, fonts, command, width, height)
					}
				}
			} else if command.textOrientation == textPaintUpright {
				var units int
				var pixels int
				units, pixels, err = uprightTextCost(command)
				if err == nil {
					verticalTextUnits += units
					verticalTextPixels += pixels
					if verticalTextUnits > maxVerticalTextTotalUnits {
						err = fmt.Errorf("render: upright text exceeds %d total units", maxVerticalTextTotalUnits)
					} else if verticalTextPixels > maxVerticalTextTotalPixels {
						err = fmt.Errorf("render: vertical text exceeds %d total pixels", maxVerticalTextTotalPixels)
					} else {
						err = drawUprightText(target, fonts, command)
					}
				}
			} else {
				err = fonts.draw(
					target,
					command.Text,
					command.X,
					command.BaselineY,
					command.FontSize,
					command.FontWeight,
					command.FontStyle,
					command.FontFamily,
					command.Color,
					command.WebFont,
				)
			}
			if err != nil {
				return nil, err
			}
			if scratch {
				compositeCommandScratch(canvas, target, command, &clipMasks, commandShapeModeBounds(command, shapeBounds, true))
			}

		case DrawImageCommand:
			if command.Image == nil || command.Rect.Width <= 0 || command.Rect.Height <= 0 {
				continue
			}
			destination := image.Rect(
				int(math.Floor(command.Rect.X)),
				int(math.Floor(command.Rect.Y)),
				int(math.Ceil(command.Rect.X+command.Rect.Width)),
				int(math.Ceil(command.Rect.Y+command.Rect.Height)),
			)
			// Scale against the full destination and let the draw implementation
			// clip to the canvas. Intersecting first would distort offscreen images.
			opacity := clamp(command.Opacity, 0, 1)
			if opacity == 0 {
				continue
			}
			var options *xdraw.Options
			if opacity < 1 {
				options = &xdraw.Options{SrcMask: image.NewUniform(color.Alpha{A: uint8(math.Round(opacity * 255))})}
			}
			target, scratch := commandPaintTarget(canvas, command)
			if scratch {
				scaleImage(target, destination, command.Image, options)
				compositeCommandScratch(canvas, target, command, &clipMasks, commandShapeModeBounds(command, shapeBounds, true))
			} else {
				scaleImage(target, destination, command.Image, options)
			}

		case DrawRoundedBorderCommand:
			target, scratch := commandPaintTarget(canvas, command)
			paintRoundedBorder(target, command)
			if scratch {
				compositeCommandScratch(canvas, target, command, &clipMasks, commandShapeModeBounds(command, shapeBounds, true))
			}

		case DrawBoxShadowCommand:
			target, scratch := commandPaintTarget(canvas, command)
			paintBoxShadow(target, command)
			if scratch {
				compositeCommandScratch(canvas, target, command, &clipMasks, commandShapeModeBounds(command, shapeBounds, true))
			}

		case DrawGradientCommand:
			target, scratch := commandPaintTarget(canvas, command)
			paintGradient(target, command)
			if scratch {
				compositeCommandScratch(canvas, target, command, &clipMasks, commandShapeModeBounds(command, shapeBounds, true))
			}

		case BeginOpacityCommand:
			fullLayerBounds := groupBounds[commandIndex].Intersect(bounds)
			layerBounds := fullLayerBounds.Intersect(canvas.Bounds())
			commandEnd, groupClosed := matchingOpacityEnd(displayList.Commands, commandIndex)
			if groupClosed && layerBounds.Empty() && !hasTransformLayer(layers) &&
				(capture == nil || commandIndex > captureAt || commandEnd < captureAt) {
				commandIndex = commandEnd
				continue
			}
			opacity := clamp(command.Opacity, 0, 1)
			alpha := uint8(math.Round(opacity * 255))
			cacheLookup := cacheCommand && groupClosed && layersAllowIndependentOpacityCache(layers)
			if cacheLookup {
				if cached, ok := paintCache.layers.getOpacity(commandIndex, commandEnd, scale, fullLayerBounds, alpha); ok {
					compositeRasterLayer(canvas, rasterLayer{canvas: cached, kind: rasterOpacityLayer, opacity: opacity})
					commandIndex = commandEnd
					continue
				}
			}
			cacheable := cacheLookup && layerBounds == fullLayerBounds
			layers = append(layers, rasterLayer{
				canvas: image.NewRGBA(layerBounds), shapeBounds: fullLayerBounds,
				kind: rasterOpacityLayer, opacity: opacity,
				command: commandIndex, commandEnd: commandEnd, cacheable: cacheable,
			})

		case EndOpacityCommand:
			if len(layers) == 1 || layers[len(layers)-1].kind != rasterOpacityLayer {
				return nil, fmt.Errorf("render: unmatched opacity group end")
			}
			group := layers[len(layers)-1]
			layers = layers[:len(layers)-1]
			if group.cacheable && group.commandEnd == commandIndex {
				paintCache.layers.putOpacity(
					group.command, group.commandEnd, scale, group.canvas.Bounds(),
					uint8(math.Round(group.opacity*255)), group.canvas,
				)
			}
			compositeRasterLayer(layers[len(layers)-1].canvas, group)

		case BeginTransformCommand:
			fullLayerBounds := groupBounds[commandIndex].Intersect(bounds)
			layerBounds := fullLayerBounds
			commandEnd, groupClosed := matchingTransformEnd(displayList.Commands, commandIndex)
			outputBounds := expandDamageRect(
				transformedGroupBounds(fullLayerBounds, command.Transform), 2,
			).Intersect(shapeBounds).Intersect(bounds)
			if groupClosed && !hasTransformLayer(layers) &&
				(fullLayerBounds.Empty() || outputBounds.Intersect(canvas.Bounds()).Empty()) &&
				(capture == nil || commandIndex > captureAt || commandEnd < captureAt) {
				commandIndex = commandEnd
				continue
			}
			cacheLookup := cacheCommand && groupClosed && transformSampleCacheEligible(command.Transform, outputBounds)
			if cacheLookup {
				if cached, ok := paintCache.layers.getTransform(
					commandIndex, commandEnd, scale, fullLayerBounds, outputBounds, command.Transform,
				); ok {
					drawRGBA64Over(canvas, cached)
					commandIndex = commandEnd
					continue
				}
			}
			layers = append(layers, rasterLayer{
				canvas: image.NewRGBA(layerBounds), shapeBounds: fullLayerBounds, kind: rasterTransformLayer,
				transform: command.Transform, command: commandIndex, commandEnd: commandEnd,
				cacheable: cacheLookup, cacheOutput: outputBounds,
			})

		case EndTransformCommand:
			if len(layers) == 1 || layers[len(layers)-1].kind != rasterTransformLayer {
				return nil, fmt.Errorf("render: unmatched transform group end")
			}
			group := layers[len(layers)-1]
			layers = layers[:len(layers)-1]
			if group.cacheable && group.commandEnd == commandIndex {
				samples := image.NewRGBA64(group.cacheOutput)
				matrix := f64.Aff3{
					group.transform.A, group.transform.C, group.transform.E,
					group.transform.B, group.transform.D, group.transform.F,
				}
				xdraw.ApproxBiLinear.Transform(samples, matrix, group.canvas, group.canvas.Bounds(), draw.Src, nil)
				paintCache.layers.putTransform(
					group.command, group.commandEnd, scale, group.canvas.Bounds(),
					group.cacheOutput, group.transform, samples,
				)
				drawRGBA64Over(layers[len(layers)-1].canvas, samples)
				continue
			}
			compositeRasterLayer(layers[len(layers)-1].canvas, group)

		case BeginClipCommand:
			fullClipBounds := rectImageBounds(command.Rect).Intersect(shapeBounds)
			clipBounds := fullClipBounds.Intersect(canvas.Bounds())
			if clipBounds.Empty() && !hasTransformLayer(layers) {
				if commandEnd, groupClosed := matchingClipEnd(displayList.Commands, commandIndex); groupClosed &&
					(capture == nil || commandIndex > captureAt || commandEnd < captureAt) {
					commandIndex = commandEnd
					continue
				}
			}
			if command.Radii.empty() {
				layers = append(layers, rasterLayer{
					canvas:      canvas.SubImage(clipBounds).(*image.RGBA),
					shapeBounds: fullClipBounds, kind: rasterClipLayer,
					direct: true,
					clip:   command.Rect,
				})
				continue
			}
			layers = append(layers, rasterLayer{
				canvas: image.NewRGBA(clipBounds), shapeBounds: fullClipBounds, kind: rasterClipLayer,
				clip: command.Rect, radii: command.Radii,
			})

		case EndClipCommand:
			if len(layers) == 1 || layers[len(layers)-1].kind != rasterClipLayer {
				return nil, fmt.Errorf("render: unmatched clip group end")
			}
			group := layers[len(layers)-1]
			layers = layers[:len(layers)-1]
			if !group.direct {
				compositeRasterLayer(layers[len(layers)-1].canvas, group)
			}

		default:
			return nil, fmt.Errorf("render: unknown display command %d", command.Kind)
		}
	}
	if len(layers) != 1 {
		return nil, fmt.Errorf("render: unclosed opacity group")
	}
	if capture != nil && *capture == nil && captureAt == len(displayList.Commands) {
		*capture = cloneRGBA(target)
	}

	return target, nil
}

func cloneRGBA(source *image.RGBA) *image.RGBA {
	clone := image.NewRGBA(source.Bounds())
	draw.Draw(clone, clone.Bounds(), source, source.Bounds().Min, draw.Src)
	return clone
}

func compositeRasterLayer(destination *image.RGBA, layer rasterLayer) {
	switch layer.kind {
	case rasterOpacityLayer:
		bounds := layer.canvas.Bounds().Intersect(destination.Bounds())
		alpha := uint8(math.Round(layer.opacity * 255))
		drawRGBAOpacityOver(destination, bounds, layer.canvas, bounds.Min, alpha)
	case rasterTransformLayer:
		matrix := layer.transform
		if !finiteTransform(matrix) {
			return
		}
		if matrix.A == 1 && matrix.B == 0 && matrix.C == 0 && matrix.D == 1 &&
			matrix.E == math.Trunc(matrix.E) && matrix.F == math.Trunc(matrix.F) {
			sourceBounds := layer.canvas.Bounds()
			offset := image.Point{X: int(matrix.E), Y: int(matrix.F)}
			draw.Draw(destination, sourceBounds.Add(offset), layer.canvas, sourceBounds.Min, draw.Over)
			return
		}
		xdraw.ApproxBiLinear.Transform(destination, f64.Aff3{
			matrix.A, matrix.C, matrix.E,
			matrix.B, matrix.D, matrix.F,
		}, layer.canvas, layer.canvas.Bounds(), draw.Over, nil)
	case rasterClipLayer:
		clipBounds := rectImageBounds(layer.clip).Intersect(layer.canvas.Bounds()).Intersect(destination.Bounds())
		if clipBounds.Empty() {
			return
		}
		mask := image.NewAlpha(clipBounds)
		if layer.radii.empty() {
			draw.Draw(mask, clipBounds, image.NewUniform(color.Alpha{A: 0xff}), image.Point{}, draw.Src)
		} else {
			paintRoundedRectWithMode(mask, layer.clip, layer.radii, color.NRGBA{A: 0xff}, layer.shapeBounds)
		}
		draw.DrawMask(destination, clipBounds, layer.canvas, clipBounds.Min, mask, clipBounds.Min, draw.Over)
	}
}

// drawRGBA64Over composites cached 16-bit premultiplied transform samples with
// the same arithmetic used by x/image/draw's RGBA ApproxBiLinear Over path.
// Keeping the low eight sample bits avoids the rounding drift that would result
// from flattening a transformed layer into an ordinary RGBA image first.
func drawRGBA64Over(destination *image.RGBA, source *image.RGBA64) {
	if destination == nil || source == nil {
		return
	}
	bounds := destination.Bounds().Intersect(source.Bounds())
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		destinationOffset := destination.PixOffset(bounds.Min.X, y)
		sourceOffset := source.PixOffset(bounds.Min.X, y)
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			sr := uint32(source.Pix[sourceOffset])<<8 | uint32(source.Pix[sourceOffset+1])
			sg := uint32(source.Pix[sourceOffset+2])<<8 | uint32(source.Pix[sourceOffset+3])
			sb := uint32(source.Pix[sourceOffset+4])<<8 | uint32(source.Pix[sourceOffset+5])
			sa := uint32(source.Pix[sourceOffset+6])<<8 | uint32(source.Pix[sourceOffset+7])
			if sa == 0 {
				destinationOffset += 4
				sourceOffset += 8
				continue
			}
			if sa == 0xffff {
				destination.Pix[destinationOffset] = uint8(sr >> 8)
				destination.Pix[destinationOffset+1] = uint8(sg >> 8)
				destination.Pix[destinationOffset+2] = uint8(sb >> 8)
				destination.Pix[destinationOffset+3] = 0xff
				destinationOffset += 4
				sourceOffset += 8
				continue
			}
			inverseAlpha := (0xffff - sa) * 0x101
			destination.Pix[destinationOffset] = uint8((uint32(destination.Pix[destinationOffset])*inverseAlpha/0xffff + sr) >> 8)
			destination.Pix[destinationOffset+1] = uint8((uint32(destination.Pix[destinationOffset+1])*inverseAlpha/0xffff + sg) >> 8)
			destination.Pix[destinationOffset+2] = uint8((uint32(destination.Pix[destinationOffset+2])*inverseAlpha/0xffff + sb) >> 8)
			destination.Pix[destinationOffset+3] = uint8((uint32(destination.Pix[destinationOffset+3])*inverseAlpha/0xffff + sa) >> 8)
			destinationOffset += 4
			sourceOffset += 8
		}
	}
}

func transformSampleCacheEligible(transform AffineTransform, output image.Rectangle) bool {
	if output.Empty() || int64(output.Dx())*int64(output.Dy())*8 > maxEffectLayerCacheBytes {
		return false
	}
	// x/image/draw uses a separate Copy path for integer translations. Keep
	// those already-cheap transforms on that path (and avoid depending on its
	// different affected-rectangle calculation for non-zero source origins).
	integerTranslation := transform.A == 1 && transform.B == 0 && transform.C == 0 && transform.D == 1 &&
		transform.E == math.Trunc(transform.E) && transform.F == math.Trunc(transform.F)
	return !integerTranslation && finiteTransform(transform)
}

// drawRGBAOpacityOver is the concrete RGBA/constant-mask form of
// image/draw's generic DrawMask Over loop. image.NewUniform does not select
// the standard library's Alpha-mask fast path, so opacity groups otherwise pay
// for per-pixel interface dispatch and RGBA64 conversion. Keeping the same
// 16-bit integer formula preserves byte-for-byte output.
func drawRGBAOpacityOver(destination *image.RGBA, rectangle image.Rectangle, source *image.RGBA, sourcePoint image.Point, maskAlpha uint8) {
	rectangle = rectangle.Intersect(destination.Bounds())
	if rectangle.Empty() || maskAlpha == 0 {
		return
	}
	ma := uint32(maskAlpha)
	ma |= ma << 8
	for y := rectangle.Min.Y; y < rectangle.Max.Y; y++ {
		destinationOffset := destination.PixOffset(rectangle.Min.X, y)
		sourceY := sourcePoint.Y + y - rectangle.Min.Y
		sourceOffset := source.PixOffset(sourcePoint.X, sourceY)
		for x := 0; x < rectangle.Dx(); x++ {
			destinationPixel := destination.Pix[destinationOffset : destinationOffset+4 : destinationOffset+4]
			sourcePixel := source.Pix[sourceOffset : sourceOffset+4 : sourceOffset+4]
			sr := uint32(sourcePixel[0])
			sg := uint32(sourcePixel[1])
			sb := uint32(sourcePixel[2])
			sa := uint32(sourcePixel[3])
			sr |= sr << 8
			sg |= sg << 8
			sb |= sb << 8
			sa |= sa << 8
			a := uint32(0xffff) - sa*ma/0xffff
			destinationPixel[0] = uint8((uint32(destinationPixel[0])*0x101*a + sr*ma) / 0xffff >> 8)
			destinationPixel[1] = uint8((uint32(destinationPixel[1])*0x101*a + sg*ma) / 0xffff >> 8)
			destinationPixel[2] = uint8((uint32(destinationPixel[2])*0x101*a + sb*ma) / 0xffff >> 8)
			destinationPixel[3] = uint8((uint32(destinationPixel[3])*0x101*a + sa*ma) / 0xffff >> 8)
			destinationOffset += 4
			sourceOffset += 4
		}
	}
}

type rasterGroupBound struct {
	kind   CommandKind
	index  int
	begin  Command
	bounds Rect
	valid  bool
}

// rasterGroupBounds finds the conservative painted extent of every retained
// opacity and transform group. Raster layers use non-zero image origins, so a
// small grouped card no longer allocates and composites a full-viewport RGBA
// surface. Bounds are computed once with a stack and remain linear in the
// display-list size even for deeply nested effects.
func rasterGroupBounds(commands []Command, viewport image.Rectangle) map[int]image.Rectangle {
	return rasterGroupBoundsAtScale(commands, viewport, 1)
}

func rasterGroupBoundsAtScale(commands []Command, viewport image.Rectangle, scale float64) map[int]image.Rectangle {
	return rasterGroupBoundsAtScaleRange(commands, viewport, scale, 0, len(commands))
}

func rasterGroupBoundsAtScaleRange(commands []Command, viewport image.Rectangle, scale float64, paintStart, paintEnd int) map[int]image.Rectangle {
	groups, _ := rasterGroupBoundsAndTransformOutputsAtScaleRange(commands, viewport, scale, paintStart, paintEnd)
	return groups
}

// rasterGroupBoundsAndTransformOutputsAtScaleRange returns source-layer bounds
// for raster allocation plus final device-space bounds for top-level transform
// groups. Nested transform outputs remain expressed in their parent layer and
// are deliberately rejected by the overlay decomposition planner.
func rasterGroupBoundsAndTransformOutputsAtScaleRange(commands []Command, viewport image.Rectangle, scale float64, paintStart, paintEnd int) (map[int]image.Rectangle, map[int]image.Rectangle) {
	result := make(map[int]image.Rectangle)
	transformOutputs := make(map[int]image.Rectangle)
	stack := make([]rasterGroupBound, 0, 8)
	add := func(bounds Rect, valid bool) {
		if !valid || len(stack) == 0 {
			return
		}
		current := &stack[len(stack)-1]
		if current.valid {
			current.bounds = unionRasterRects(current.bounds, bounds)
		} else {
			current.bounds, current.valid = bounds, true
		}
	}
	for index := range commands {
		command := commands[index]
		scaleRasterCommand(&command, scale)
		switch command.Kind {
		case BeginOpacityCommand, BeginTransformCommand, BeginClipCommand:
			stack = append(stack, rasterGroupBound{kind: command.Kind, index: index, begin: command})
		case EndOpacityCommand, EndTransformCommand, EndClipCommand:
			if len(stack) == 0 {
				continue
			}
			group := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			layerBounds := group.bounds
			if group.valid {
				switch group.kind {
				case BeginClipCommand:
					group.bounds = intersectRasterRects(group.bounds, group.begin.Rect)
				case BeginTransformCommand:
					group.bounds = transformRasterRect(group.bounds, group.begin.Transform)
					// ApproxBiLinear samples a small neighborhood at transformed
					// edges. Two device pixels conservatively cover its footprint.
					transformOutputs[group.index] = expandDamageRect(
						rectImageBounds(group.bounds), 2,
					).Intersect(viewport)
				}
			}
			if group.kind == BeginOpacityCommand || group.kind == BeginTransformCommand {
				bounds := image.Rectangle{}
				if group.valid {
					bounds = rectImageBounds(layerBounds).Intersect(viewport)
				}
				result[group.index] = bounds
			}
			add(group.bounds, group.valid)
		default:
			if commandPaintsPixels(command.Kind) && (index < paintStart || index >= paintEnd) {
				continue
			}
			bounds, valid := rasterCommandBounds(command, viewport)
			add(bounds, valid)
		}
	}
	return result, transformOutputs
}

func rasterCommandBounds(command Command, viewport image.Rectangle) (Rect, bool) {
	var bounds Rect
	switch command.Kind {
	case FillRectCommand, FillEllipseCommand, DrawImageCommand, DrawRoundedBorderCommand, DrawGradientCommand:
		bounds = command.Rect
	case DrawBoxShadowCommand:
		extent := math.Ceil(math.Max(0, command.ShadowBlur)/2)*3 + 2 + math.Abs(command.ShadowSpread)
		bounds = Rect{
			X:      command.Rect.X + command.ShadowOffsetX - extent,
			Y:      command.Rect.Y + command.ShadowOffsetY - extent,
			Width:  command.Rect.Width + extent*2,
			Height: command.Rect.Height + extent*2,
		}
	case DrawTextCommand:
		bounds = command.textBounds
		if bounds.Width <= 0 || bounds.Height <= 0 {
			bounds = Rect{X: float64(viewport.Min.X), Y: float64(viewport.Min.Y), Width: float64(viewport.Dx()), Height: float64(viewport.Dy())}
		}
	default:
		return Rect{}, false
	}
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return Rect{}, false
	}
	if command.HasClip {
		bounds = intersectRasterRects(bounds, command.Clip)
	}
	for index := 0; index < int(command.RoundedClipCount); index++ {
		bounds = intersectRasterRects(bounds, command.RoundedClips[index].Rect)
	}
	return bounds, bounds.Width > 0 && bounds.Height > 0
}

func unionRasterRects(left, right Rect) Rect {
	x := math.Min(left.X, right.X)
	y := math.Min(left.Y, right.Y)
	endX := math.Max(left.X+left.Width, right.X+right.Width)
	endY := math.Max(left.Y+left.Height, right.Y+right.Height)
	return Rect{X: x, Y: y, Width: endX - x, Height: endY - y}
}

func intersectRasterRects(left, right Rect) Rect {
	x := math.Max(left.X, right.X)
	y := math.Max(left.Y, right.Y)
	endX := math.Min(left.X+left.Width, right.X+right.Width)
	endY := math.Min(left.Y+left.Height, right.Y+right.Height)
	if endX <= x || endY <= y {
		return Rect{X: x, Y: y}
	}
	return Rect{X: x, Y: y, Width: endX - x, Height: endY - y}
}

func transformRasterRect(bounds Rect, transform AffineTransform) Rect {
	points := [4][2]float64{
		{bounds.X, bounds.Y},
		{bounds.X + bounds.Width, bounds.Y},
		{bounds.X, bounds.Y + bounds.Height},
		{bounds.X + bounds.Width, bounds.Y + bounds.Height},
	}
	minimumX, minimumY := math.Inf(1), math.Inf(1)
	maximumX, maximumY := math.Inf(-1), math.Inf(-1)
	for _, point := range points {
		x := transform.A*point[0] + transform.C*point[1] + transform.E
		y := transform.B*point[0] + transform.D*point[1] + transform.F
		minimumX, minimumY = math.Min(minimumX, x), math.Min(minimumY, y)
		maximumX, maximumY = math.Max(maximumX, x), math.Max(maximumY, y)
	}
	return Rect{X: minimumX, Y: minimumY, Width: maximumX - minimumX, Height: maximumY - minimumY}
}

func finiteTransform(transform AffineTransform) bool {
	return isFinite(transform.A) && isFinite(transform.B) && isFinite(transform.C) &&
		isFinite(transform.D) && isFinite(transform.E) && isFinite(transform.F)
}

func commandNeedsScratch(command Command) bool {
	return command.RoundedClipCount > 0
}

func commandPaintTarget(canvas *image.RGBA, command Command) (*image.RGBA, bool) {
	if commandNeedsScratch(command) {
		return image.NewRGBA(commandScratchBounds(command, canvas.Bounds())), true
	}
	if command.HasClip {
		return canvas.SubImage(commandClip(command, canvas.Bounds())).(*image.RGBA), false
	}
	return canvas, false
}

func hasTransformLayer(layers []rasterLayer) bool {
	for index := len(layers) - 1; index > 0; index-- {
		if layers[index].kind == rasterTransformLayer {
			return true
		}
	}
	return false
}

func layersAllowIndependentOpacityCache(layers []rasterLayer) bool {
	for index := 1; index < len(layers); index++ {
		if layers[index].kind != rasterClipLayer || !layers[index].direct {
			return false
		}
	}
	return true
}

func commandBoundsVisible(bounds Rect, command Command, canvas image.Rectangle) bool {
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return true
	}
	visible := rectImageBounds(bounds).Intersect(canvas)
	if command.HasClip {
		visible = visible.Intersect(commandClip(command, canvas))
	}
	for index := 0; index < int(command.RoundedClipCount); index++ {
		visible = visible.Intersect(rectImageBounds(command.RoundedClips[index].Rect))
	}
	return !visible.Empty()
}

// commandScratchBounds bounds temporary paint storage to the intersection of
// the command's retained clips. A clipped command used to allocate another
// full-viewport RGBA image, which made a page containing many independently
// clipped nodes consume viewport-by-command memory during rasterization.
func commandScratchBounds(command Command, bounds image.Rectangle) image.Rectangle {
	if command.HasClip {
		bounds = bounds.Intersect(commandClip(command, bounds))
	}
	for index := 0; index < int(command.RoundedClipCount); index++ {
		bounds = bounds.Intersect(rectImageBounds(command.RoundedClips[index].Rect))
	}
	return bounds
}

func scaleImage(target draw.Image, destination image.Rectangle, source image.Image, options *xdraw.Options) {
	sourceBounds := source.Bounds()
	if destination.Dx() == sourceBounds.Dx() && destination.Dy() == sourceBounds.Dy() {
		xdraw.Copy(target, destination.Min, source, sourceBounds, draw.Over, options)
		return
	}
	// Cubic resampling preserves considerably more detail when a large decoded
	// image is reduced into a CSS box. Approximate bilinear remains a good,
	// bounded choice for enlargement where cubic ringing is more visible.
	scaler := xdraw.Scaler(xdraw.ApproxBiLinear)
	if destination.Dx() < sourceBounds.Dx() || destination.Dy() < sourceBounds.Dy() {
		scaler = xdraw.CatmullRom
	}
	scaler.Scale(target, destination, source, sourceBounds, draw.Over, options)
}

type commandClipMaskKey struct {
	bounds  image.Rectangle
	hasClip bool
	clip    Rect
	count   uint8
	rounded [maxCommandRoundedClips]RoundedClip
}

func compositeCommandScratch(destination, source *image.RGBA, command Command, cache *map[commandClipMaskKey]*image.Alpha, shapeModeBounds image.Rectangle) {
	bounds := destination.Bounds()
	maskBounds := commandScratchBounds(command, bounds)
	if maskBounds.Empty() {
		return
	}
	key := commandClipMaskKey{
		bounds: maskBounds, hasClip: command.HasClip, clip: command.Clip,
		count: command.RoundedClipCount, rounded: command.RoundedClips,
	}
	var mask *image.Alpha
	if cache != nil && *cache != nil {
		mask = (*cache)[key]
	}
	if mask == nil {
		mask = image.NewAlpha(maskBounds)
		draw.Draw(mask, maskBounds, image.NewUniform(color.Alpha{A: 0xff}), image.Point{}, draw.Src)
		for index := 0; index < int(command.RoundedClipCount); index++ {
			clip := command.RoundedClips[index]
			shape := image.NewAlpha(maskBounds)
			paintRoundedRectWithMode(shape, clip.Rect, clip.Radii, color.NRGBA{A: 0xff}, shapeModeBounds)
			for y := maskBounds.Min.Y; y < maskBounds.Max.Y; y++ {
				for x := maskBounds.Min.X; x < maskBounds.Max.X; x++ {
					left := uint16(mask.AlphaAt(x, y).A)
					right := uint16(shape.AlphaAt(x, y).A)
					mask.SetAlpha(x, y, color.Alpha{A: uint8((left*right + 127) / 255)})
				}
			}
		}
		if cache != nil {
			if *cache == nil {
				*cache = make(map[commandClipMaskKey]*image.Alpha)
			}
			(*cache)[key] = mask
		}
	}
	draw.DrawMask(destination, maskBounds, source, maskBounds.Min, mask, maskBounds.Min, draw.Over)
}

func drawRGBAUniformOver(destination *image.RGBA, rectangle image.Rectangle, fill color.NRGBA) {
	rectangle = rectangle.Intersect(destination.Bounds())
	if rectangle.Empty() || fill.A == 0 {
		return
	}
	rowBytes := rectangle.Dx() * 4
	if fill.A == 0xff {
		firstOffset := destination.PixOffset(rectangle.Min.X, rectangle.Min.Y)
		firstRow := destination.Pix[firstOffset : firstOffset+rowBytes]
		firstRow[0] = fill.R
		firstRow[1] = fill.G
		firstRow[2] = fill.B
		firstRow[3] = 0xff
		for filled := 4; filled < rowBytes; filled *= 2 {
			copy(firstRow[filled:], firstRow[:filled])
		}
		for y := rectangle.Min.Y + 1; y < rectangle.Max.Y; y++ {
			offset := destination.PixOffset(rectangle.Min.X, y)
			copy(destination.Pix[offset:offset+rowBytes], firstRow)
		}
		return
	}
	sr, sg, sb, sa := fill.RGBA()
	inverse := (0xffff - sa) * 0x101
	for y := rectangle.Min.Y; y < rectangle.Max.Y; y++ {
		offset := destination.PixOffset(rectangle.Min.X, y)
		row := destination.Pix[offset : offset+rowBytes]
		for index := 0; index < rowBytes; index += 4 {
			row[index] = uint8((uint32(row[index])*inverse/0xffff + sr) >> 8)
			row[index+1] = uint8((uint32(row[index+1])*inverse/0xffff + sg) >> 8)
			row[index+2] = uint8((uint32(row[index+2])*inverse/0xffff + sb) >> 8)
			row[index+3] = uint8((uint32(row[index+3])*inverse/0xffff + sa) >> 8)
		}
	}
}

func commandShapeModeBounds(command Command, layerBounds image.Rectangle, scratch bool) image.Rectangle {
	if scratch {
		return commandScratchBounds(command, layerBounds)
	}
	return layerBounds
}

func rectImageBounds(rectangle Rect) image.Rectangle {
	return image.Rect(
		int(math.Floor(rectangle.X)),
		int(math.Floor(rectangle.Y)),
		int(math.Ceil(rectangle.X+rectangle.Width)),
		int(math.Ceil(rectangle.Y+rectangle.Height)),
	)
}

func paintRoundedBorder(target draw.Image, command Command) {
	outer := command.Rect
	if outer.Width <= 0 || outer.Height <= 0 {
		return
	}
	inner := Rect{
		X:      outer.X + command.BorderWidths.Left,
		Y:      outer.Y + command.BorderWidths.Top,
		Width:  math.Max(0, outer.Width-command.BorderWidths.Left-command.BorderWidths.Right),
		Height: math.Max(0, outer.Height-command.BorderWidths.Top-command.BorderWidths.Bottom),
	}
	innerRadii := insetCornerRadii(command.Radii, command.BorderWidths)
	bounds := rectImageBounds(outer).Intersect(target.Bounds())
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			coverage := roundedRectCoverage(outer, command.Radii, x, y) - roundedRectCoverage(inner, innerRadii, x, y)
			if coverage <= 0 {
				continue
			}
			edge := nearestBorderEdge(outer, float64(x)+0.5, float64(y)+0.5)
			if command.BorderStyles[edge] != borderStyleSolid || command.BorderColors[edge].A == 0 {
				continue
			}
			blendNRGBA(target, x, y, command.BorderColors[edge], clamp(coverage, 0, 1))
		}
	}
}

func nearestBorderEdge(rectangle Rect, x, y float64) int {
	distances := [4]float64{
		math.Abs(y - rectangle.Y),
		math.Abs(rectangle.X + rectangle.Width - x),
		math.Abs(rectangle.Y + rectangle.Height - y),
		math.Abs(x - rectangle.X),
	}
	result := 0
	for index := 1; index < len(distances); index++ {
		if distances[index] < distances[result] {
			result = index
		}
	}
	return result
}

func roundedRectCoverage(rectangle Rect, radii CornerRadii, pixelX, pixelY int) float64 {
	if rectangle.Width <= 0 || rectangle.Height <= 0 {
		return 0
	}
	covered := 0
	for _, offset := range [4][2]float64{{0.25, 0.25}, {0.75, 0.25}, {0.25, 0.75}, {0.75, 0.75}} {
		if pointInRoundedRect(rectangle, radii, float64(pixelX)+offset[0], float64(pixelY)+offset[1]) {
			covered++
		}
	}
	return float64(covered) / 4
}

func pointInRoundedRect(rectangle Rect, radii CornerRadii, x, y float64) bool {
	if x < rectangle.X || y < rectangle.Y || x >= rectangle.X+rectangle.Width || y >= rectangle.Y+rectangle.Height {
		return false
	}
	check := func(centerX, centerY float64, radius CornerRadius) bool {
		if radius.X <= 0 || radius.Y <= 0 {
			return true
		}
		dx, dy := (x-centerX)/radius.X, (y-centerY)/radius.Y
		return dx*dx+dy*dy <= 1
	}
	if x < rectangle.X+radii.TopLeft.X && y < rectangle.Y+radii.TopLeft.Y {
		return check(rectangle.X+radii.TopLeft.X, rectangle.Y+radii.TopLeft.Y, radii.TopLeft)
	}
	if x > rectangle.X+rectangle.Width-radii.TopRight.X && y < rectangle.Y+radii.TopRight.Y {
		return check(rectangle.X+rectangle.Width-radii.TopRight.X, rectangle.Y+radii.TopRight.Y, radii.TopRight)
	}
	if x > rectangle.X+rectangle.Width-radii.BottomRight.X && y > rectangle.Y+rectangle.Height-radii.BottomRight.Y {
		return check(rectangle.X+rectangle.Width-radii.BottomRight.X, rectangle.Y+rectangle.Height-radii.BottomRight.Y, radii.BottomRight)
	}
	if x < rectangle.X+radii.BottomLeft.X && y > rectangle.Y+rectangle.Height-radii.BottomLeft.Y {
		return check(rectangle.X+radii.BottomLeft.X, rectangle.Y+rectangle.Height-radii.BottomLeft.Y, radii.BottomLeft)
	}
	return true
}

func paintBoxShadow(target draw.Image, command Command) {
	blurRadius := min(128, max(0, int(math.Ceil(command.ShadowBlur/2))))
	padding := blurRadius*3 + 2
	unclipped := rectImageBounds(Rect{
		X:      command.Rect.X + command.ShadowOffsetX - command.ShadowSpread - float64(padding),
		Y:      command.Rect.Y + command.ShadowOffsetY - command.ShadowSpread - float64(padding),
		Width:  command.Rect.Width + 2*command.ShadowSpread + float64(2*padding),
		Height: command.Rect.Height + 2*command.ShadowSpread + float64(2*padding),
	})
	if command.ShadowInset {
		unclipped = rectImageBounds(command.Rect)
	}
	writeBounds := unclipped.Intersect(target.Bounds())
	if writeBounds.Empty() {
		return
	}
	local := unclipped.Intersect(expandDamageRect(writeBounds, padding))
	if local.Empty() || int64(local.Dx())*int64(local.Dy()) > 32_000_000 {
		return
	}
	mask := image.NewAlpha(local)
	if command.ShadowInset {
		draw.Draw(mask, local, image.NewUniform(color.Alpha{A: 0xff}), image.Point{}, draw.Src)
		hole := Rect{
			X:      command.Rect.X + command.ShadowSpread + command.ShadowOffsetX,
			Y:      command.Rect.Y + command.ShadowSpread + command.ShadowOffsetY,
			Width:  math.Max(0, command.Rect.Width-2*command.ShadowSpread),
			Height: math.Max(0, command.Rect.Height-2*command.ShadowSpread),
		}
		holeRadii := insetCornerRadii(command.Radii, Edges{
			Top: command.ShadowSpread, Right: command.ShadowSpread,
			Bottom: command.ShadowSpread, Left: command.ShadowSpread,
		})
		holeMask := image.NewAlpha(local)
		paintRoundedRectWithMode(holeMask, hole, holeRadii, color.NRGBA{A: 0xff}, unclipped)
		for y := local.Min.Y; y < local.Max.Y; y++ {
			for x := local.Min.X; x < local.Max.X; x++ {
				mask.SetAlpha(x, y, color.Alpha{A: 0xff - holeMask.AlphaAt(x, y).A})
			}
		}
	} else {
		shape := Rect{
			X:      command.Rect.X + command.ShadowOffsetX - command.ShadowSpread,
			Y:      command.Rect.Y + command.ShadowOffsetY - command.ShadowSpread,
			Width:  math.Max(0, command.Rect.Width+2*command.ShadowSpread),
			Height: math.Max(0, command.Rect.Height+2*command.ShadowSpread),
		}
		spread := math.Max(0, command.ShadowSpread)
		shapeRadii := CornerRadii{
			TopLeft:     CornerRadius{X: command.Radii.TopLeft.X + spread, Y: command.Radii.TopLeft.Y + spread},
			TopRight:    CornerRadius{X: command.Radii.TopRight.X + spread, Y: command.Radii.TopRight.Y + spread},
			BottomRight: CornerRadius{X: command.Radii.BottomRight.X + spread, Y: command.Radii.BottomRight.Y + spread},
			BottomLeft:  CornerRadius{X: command.Radii.BottomLeft.X + spread, Y: command.Radii.BottomLeft.Y + spread},
		}
		paintRoundedRectWithMode(mask, shape, shapeRadii, color.NRGBA{A: 0xff}, unclipped)
	}
	if blurRadius > 0 {
		temporary := image.NewAlpha(local)
		scratch := image.NewAlpha(local)
		for pass := 0; pass < 3; pass++ {
			boxBlurAlphaInto(scratch, temporary, mask, blurRadius)
			mask, scratch = scratch, mask
		}
	}
	for y := writeBounds.Min.Y; y < writeBounds.Max.Y; y++ {
		maskRow := mask.Pix[(y-local.Min.Y)*mask.Stride:]
		for x := writeBounds.Min.X; x < writeBounds.Max.X; x++ {
			alpha := maskRow[x-local.Min.X]
			if alpha == 0 {
				continue
			}
			inside := pointInRoundedRect(command.Rect, command.Radii, float64(x)+0.5, float64(y)+0.5)
			if command.ShadowInset != inside {
				continue
			}
			blendNRGBA(target, x, y, command.Color, float64(alpha)/255)
		}
	}
}

func boxBlurAlpha(source *image.Alpha, radius int) *image.Alpha {
	if source == nil || radius <= 0 {
		return source
	}
	bounds := source.Bounds()
	temporary := image.NewAlpha(bounds)
	result := image.NewAlpha(bounds)
	boxBlurAlphaInto(result, temporary, source, radius)
	return result
}

func boxBlurAlphaInto(result, temporary, source *image.Alpha, radius int) {
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 {
		return
	}
	for y := 0; y < height; y++ {
		srcRow := source.Pix[y*source.Stride : y*source.Stride+width]
		tmpRow := temporary.Pix[y*temporary.Stride : y*temporary.Stride+width]
		sum := 0
		limit := min(width-1, radius)
		for x := 0; x <= limit; x++ {
			sum += int(srcRow[x])
		}
		for x := 0; x < width; x++ {
			count := min(width-1, x+radius) - max(0, x-radius) + 1
			tmpRow[x] = uint8(sum / count)
			if remove := x - radius; remove >= 0 {
				sum -= int(srcRow[remove])
			}
			if add := x + radius + 1; add < width {
				sum += int(srcRow[add])
			}
		}
	}
	tmpStride := temporary.Stride
	dstStride := result.Stride
	limitY := min(height-1, radius)
	for x := 0; x < width; x++ {
		sum := 0
		for y := 0; y <= limitY; y++ {
			sum += int(temporary.Pix[y*tmpStride+x])
		}
		for y := 0; y < height; y++ {
			count := min(height-1, y+radius) - max(0, y-radius) + 1
			result.Pix[y*dstStride+x] = uint8(sum / count)
			if remove := y - radius; remove >= 0 {
				sum -= int(temporary.Pix[remove*tmpStride+x])
			}
			if add := y + radius + 1; add < height {
				sum += int(temporary.Pix[add*tmpStride+x])
			}
		}
	}
}

func paintGradient(target draw.Image, command Command) {
	if command.GradientStopCount < 2 || command.Rect.Width <= 0 || command.Rect.Height <= 0 {
		return
	}
	bounds := rectImageBounds(command.Rect).Intersect(target.Bounds())
	angle := command.GradientAngle * math.Pi / 180
	dx, dy := math.Sin(angle), -math.Cos(angle)
	halfProjection := math.Abs(dx)*command.Rect.Width/2 + math.Abs(dy)*command.Rect.Height/2
	centerX, centerY := command.Rect.X+command.Rect.Width/2, command.Rect.Y+command.Rect.Height/2
	radius := math.Hypot(command.Rect.Width/2, command.Rect.Height/2)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			position := 0.0
			if command.GradientKind == RadialGradient {
				if radius > 0 {
					position = math.Hypot(float64(x)+0.5-centerX, float64(y)+0.5-centerY) / radius
				}
			} else if halfProjection > 0 {
				position = (dx*(float64(x)+0.5-centerX) + dy*(float64(y)+0.5-centerY) + halfProjection) / (2 * halfProjection)
			}
			blendNRGBA(target, x, y, gradientColorAt(command, position), 1)
		}
	}
}

func gradientColorAt(command Command, position float64) color.NRGBA {
	count := int(command.GradientStopCount)
	first, last := command.GradientStops[0].Offset, command.GradientStops[count-1].Offset
	if command.GradientRepeating && last > first {
		position = first + math.Mod(position-first, last-first)
		if position < first {
			position += last - first
		}
	}
	if position <= first {
		return command.GradientStops[0].Color
	}
	for index := 1; index < count; index++ {
		right := command.GradientStops[index]
		if position > right.Offset {
			continue
		}
		left := command.GradientStops[index-1]
		amount := 0.0
		if right.Offset > left.Offset {
			amount = (position - left.Offset) / (right.Offset - left.Offset)
		}
		return mixNRGBA(left.Color, right.Color, clamp(amount, 0, 1))
	}
	return command.GradientStops[count-1].Color
}

func mixNRGBA(left, right color.NRGBA, amount float64) color.NRGBA {
	mix := func(a, b uint8) uint8 { return uint8(math.Round(float64(a)*(1-amount) + float64(b)*amount)) }
	return color.NRGBA{R: mix(left.R, right.R), G: mix(left.G, right.G), B: mix(left.B, right.B), A: mix(left.A, right.A)}
}

func blendNRGBA(target draw.Image, x, y int, source color.NRGBA, coverage float64) {
	if coverage <= 0 || source.A == 0 {
		return
	}
	if coverage < 1 {
		source.A = uint8(math.Round(float64(source.A) * coverage))
		if source.A == 0 {
			return
		}
	}
	r, g, b, a := source.RGBA()
	srcR, srcG, srcB, srcA := uint32(r>>8), uint32(g>>8), uint32(b>>8), uint32(a>>8)
	inverse := uint32(255) - srcA
	if rgba, ok := target.(*image.RGBA); ok {
		if !(image.Point{X: x, Y: y}.In(rgba.Rect)) {
			return
		}
		offset := rgba.PixOffset(x, y)
		pix := rgba.Pix[offset : offset+4 : offset+4]
		pix[0] = uint8(min(255, srcR+uint32(pix[0])*inverse/255))
		pix[1] = uint8(min(255, srcG+uint32(pix[1])*inverse/255))
		pix[2] = uint8(min(255, srcB+uint32(pix[2])*inverse/255))
		pix[3] = uint8(min(255, srcA+uint32(pix[3])*inverse/255))
		return
	}
	dst := color.RGBAModel.Convert(target.At(x, y)).(color.RGBA)
	target.Set(x, y, color.RGBA{
		R: uint8(min(255, srcR+uint32(dst.R)*inverse/255)),
		G: uint8(min(255, srcG+uint32(dst.G)*inverse/255)),
		B: uint8(min(255, srcB+uint32(dst.B)*inverse/255)),
		A: uint8(min(255, srcA+uint32(dst.A)*inverse/255)),
	})
}

const (
	maxSidewaysTextPixels      = 4 * 1024 * 1024
	maxVerticalTextTotalPixels = 32 * 1024 * 1024
	maxUprightTextUnits        = 4096
	maxVerticalTextTotalUnits  = 65536
)

func sidewaysTextDimensions(command Command) (int, int, error) {
	widthValue := math.Ceil(math.Max(1, command.textWidth))
	heightValue := math.Ceil(math.Max(1, command.textHeight))
	if !isFinite(widthValue) || !isFinite(heightValue) ||
		widthValue > maxSidewaysTextPixels || heightValue > maxSidewaysTextPixels {
		return 0, 0, fmt.Errorf("render: sideways text exceeds %d pixels", maxSidewaysTextPixels)
	}
	width, height := int(widthValue), int(heightValue)
	if width*height > maxSidewaysTextPixels {
		return 0, 0, fmt.Errorf("render: sideways text exceeds %d pixels", maxSidewaysTextPixels)
	}
	return width, height, nil
}

func drawSidewaysText(destination *image.RGBA, fonts *fontBook, command Command, width, height int) error {
	source := image.NewRGBA(image.Rect(0, 0, width, height))
	if err := fonts.draw(
		source, command.Text, 0, command.textBaseline,
		command.FontSize, command.FontWeight, command.FontStyle, command.FontFamily, command.Color, command.WebFont,
	); err != nil {
		return err
	}
	rotated := image.NewRGBA(image.Rect(0, 0, height, width))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			rotated.SetRGBA(height-1-y, x, source.RGBAAt(x, y))
		}
	}
	destinationPoint := image.Point{
		X: int(math.Floor(command.textBounds.X)),
		Y: int(math.Floor(command.textBounds.Y)),
	}
	destinationRect := rotated.Bounds().Add(destinationPoint).Intersect(destination.Bounds())
	if destinationRect.Empty() {
		return nil
	}
	draw.Draw(destination, destinationRect, rotated, destinationRect.Min.Sub(destinationPoint), draw.Over)
	return nil
}

func uprightTextCost(command Command) (int, int, error) {
	units := len(splitVerticalTextUnits(command.Text))
	if units == 0 {
		return 0, 0, nil
	}
	if units > maxUprightTextUnits {
		return 0, 0, fmt.Errorf("render: upright text exceeds %d units", maxUprightTextUnits)
	}
	width := math.Ceil(math.Max(1, command.textBounds.Width))
	height := math.Ceil(math.Max(1, command.textBounds.Height))
	if !isFinite(width) || !isFinite(height) || width > maxSidewaysTextPixels || height > maxSidewaysTextPixels || width*height > maxSidewaysTextPixels {
		return 0, 0, fmt.Errorf("render: upright text exceeds %d pixels", maxSidewaysTextPixels)
	}
	return units, int(width * height), nil
}

func drawUprightText(destination *image.RGBA, fonts *fontBook, command Command) error {
	units := splitVerticalTextUnits(command.Text)
	if len(units) == 0 {
		return nil
	}
	advance := command.textBounds.Height / float64(len(units))
	if !isFinite(advance) || advance <= 0 {
		return fmt.Errorf("render: invalid upright text advance %g", advance)
	}
	for index, unit := range units {
		metrics, err := fonts.metrics(unit, command.FontSize, command.FontWeight, command.FontStyle, command.FontFamily, command.WebFont)
		if err != nil {
			return err
		}
		cellTop := command.textBounds.Y + float64(index)*advance
		glyphHeight := metrics.ascent + metrics.descent
		baseline := cellTop + math.Max(0, (advance-glyphHeight)/2) + metrics.ascent
		x := command.textBounds.X + (command.textBounds.Width-metrics.width)/2
		if err := fonts.draw(
			destination, unit, x, baseline,
			command.FontSize, command.FontWeight, command.FontStyle, command.FontFamily, command.Color, command.WebFont,
		); err != nil {
			return err
		}
	}
	return nil
}

func paintEllipse(target draw.Image, rectangle Rect, fill color.NRGBA) {
	paintEllipseWithMode(target, rectangle, fill, target.Bounds())
}

func paintEllipseWithMode(target draw.Image, rectangle Rect, fill color.NRGBA, shapeModeBounds image.Rectangle) {
	bounds := vectorPaintBounds(target.Bounds(), rectangle)
	stableBounds := vectorPaintBounds(shapeModeBounds, rectangle)
	if stableTarget, ok := stableVectorPaintTarget(target, bounds, stableBounds); ok {
		paintEllipseWithinMode(stableTarget, rectangle, fill, stableBounds, shapeModeBounds)
		copyStableVectorPaint(target, stableTarget, bounds)
		return
	}
	paintEllipseWithinMode(target, rectangle, fill, bounds, shapeModeBounds)
}

func paintEllipseWithin(target draw.Image, rectangle Rect, fill color.NRGBA, bounds image.Rectangle) {
	paintEllipseWithinMode(target, rectangle, fill, bounds, target.Bounds())
}

func paintEllipseWithinMode(target draw.Image, rectangle Rect, fill color.NRGBA, bounds, shapeModeBounds image.Rectangle) {
	if rectangle.Width <= 0 || rectangle.Height <= 0 || fill.A == 0 || bounds.Empty() {
		return
	}
	rasterizer := newShapeRasterizer(bounds, shapeModeBounds)
	targetBounds := target.Bounds()
	cx := float32(rectangle.X + rectangle.Width/2 - float64(targetBounds.Min.X))
	cy := float32(rectangle.Y + rectangle.Height/2 - float64(targetBounds.Min.Y))
	rx := float32(rectangle.Width / 2)
	ry := float32(rectangle.Height / 2)
	offsetX := float32(bounds.Min.X - targetBounds.Min.X)
	offsetY := float32(bounds.Min.Y - targetBounds.Min.Y)
	// Cubic approximation of a circle/ellipse, accurate to well below one
	// device pixel at the border sizes emitted by the layout engine.
	const kappa = float32(0.5522847498307936)
	rasterizer.MoveTo(cx+rx-offsetX, cy-offsetY)
	rasterizer.CubeTo(cx+rx-offsetX, cy+kappa*ry-offsetY, cx+kappa*rx-offsetX, cy+ry-offsetY, cx-offsetX, cy+ry-offsetY)
	rasterizer.CubeTo(cx-kappa*rx-offsetX, cy+ry-offsetY, cx-rx-offsetX, cy+kappa*ry-offsetY, cx-rx-offsetX, cy-offsetY)
	rasterizer.CubeTo(cx-rx-offsetX, cy-kappa*ry-offsetY, cx-kappa*rx-offsetX, cy-ry-offsetY, cx-offsetX, cy-ry-offsetY)
	rasterizer.CubeTo(cx+kappa*rx-offsetX, cy-ry-offsetY, cx+rx-offsetX, cy-kappa*ry-offsetY, cx+rx-offsetX, cy-offsetY)
	rasterizer.ClosePath()
	rasterizer.Draw(target, bounds, image.NewUniform(fill), image.Point{})
}

func paintRoundedRect(target draw.Image, rectangle Rect, radii CornerRadii, fill color.NRGBA) {
	paintRoundedRectWithMode(target, rectangle, radii, fill, target.Bounds())
}

func paintRoundedRectWithMode(target draw.Image, rectangle Rect, radii CornerRadii, fill color.NRGBA, shapeModeBounds image.Rectangle) {
	if rectangle.Width <= 0 || rectangle.Height <= 0 || fill.A == 0 {
		return
	}
	bounds := vectorPaintBounds(target.Bounds(), rectangle)
	stableBounds := vectorPaintBounds(shapeModeBounds, rectangle)
	// Alpha targets here are already tightly sized clip and shadow masks. Keep
	// their full bounds so vector retains its historical Alpha accumulation and
	// therefore its exact coverage rounding.
	if _, ok := target.(*image.Alpha); ok {
		bounds = target.Bounds()
		stableBounds = shapeModeBounds
	}
	if stableTarget, ok := stableVectorPaintTarget(target, bounds, stableBounds); ok {
		paintRoundedRectWithinMode(stableTarget, rectangle, radii, fill, stableBounds, shapeModeBounds)
		copyStableVectorPaint(target, stableTarget, bounds)
		return
	}
	paintRoundedRectWithinMode(target, rectangle, radii, fill, bounds, shapeModeBounds)
}

// stableVectorPaintTarget preserves the full-layer coordinate origin when a
// retained damage replay exposes only a lower or right-hand crop of a vector
// shape. x/image/vector subdivides cubic paths in float32 before converting to
// coverage; translating the path to a cropped origin can move one edge channel
// by a byte even when the geometry is otherwise identical. Seeding a stable
// local target also preserves the concrete RGBA/Alpha Over rounding used by a
// full replay.
func stableVectorPaintTarget(target draw.Image, paintBounds, stableBounds image.Rectangle) (draw.Image, bool) {
	if paintBounds.Empty() || stableBounds.Empty() || paintBounds.Min == stableBounds.Min || !paintBounds.In(stableBounds) {
		return nil, false
	}
	var stable draw.Image
	switch target.(type) {
	case *image.RGBA:
		stable = image.NewRGBA(stableBounds)
	case *image.Alpha:
		stable = image.NewAlpha(stableBounds)
	default:
		return nil, false
	}
	draw.Draw(stable, paintBounds, target, paintBounds.Min, draw.Src)
	return stable, true
}

func copyStableVectorPaint(target draw.Image, stable draw.Image, paintBounds image.Rectangle) {
	draw.Draw(target, paintBounds, stable, paintBounds.Min, draw.Src)
}

func paintRoundedRectWithin(target draw.Image, rectangle Rect, radii CornerRadii, fill color.NRGBA, bounds image.Rectangle) {
	paintRoundedRectWithinMode(target, rectangle, radii, fill, bounds, target.Bounds())
}

func paintRoundedRectWithinMode(target draw.Image, rectangle Rect, radii CornerRadii, fill color.NRGBA, bounds, shapeModeBounds image.Rectangle) {
	if bounds.Empty() {
		return
	}
	rasterizer := roundedRectRasterizer(rectangle, radii, bounds, shapeModeBounds, target.Bounds())
	rasterizer.Draw(target, bounds, image.NewUniform(fill), image.Point{})
}

// roundedRectRasterizer builds the exact path operands used by the historical
// direct painter. pathTargetBounds is intentionally separate from paintBounds:
// float32 conversion before subtracting a crop origin affects edge coverage.
func roundedRectRasterizer(rectangle Rect, radii CornerRadii, paintBounds, shapeModeBounds, pathTargetBounds image.Rectangle) *vector.Rasterizer {
	rasterizer := newShapeRasterizer(paintBounds, shapeModeBounds)
	targetBounds := pathTargetBounds
	x := float32(rectangle.X - float64(targetBounds.Min.X))
	y := float32(rectangle.Y - float64(targetBounds.Min.Y))
	w := float32(rectangle.Width)
	h := float32(rectangle.Height)
	offsetX := float32(paintBounds.Min.X - targetBounds.Min.X)
	offsetY := float32(paintBounds.Min.Y - targetBounds.Min.Y)
	const kappa = float32(0.5522847498307936)
	topLeftX, topLeftY := float32(radii.TopLeft.X), float32(radii.TopLeft.Y)
	topRightX, topRightY := float32(radii.TopRight.X), float32(radii.TopRight.Y)
	bottomRightX, bottomRightY := float32(radii.BottomRight.X), float32(radii.BottomRight.Y)
	bottomLeftX, bottomLeftY := float32(radii.BottomLeft.X), float32(radii.BottomLeft.Y)

	rasterizer.MoveTo(x+topLeftX-offsetX, y-offsetY)
	rasterizer.LineTo(x+w-topRightX-offsetX, y-offsetY)
	rasterizer.CubeTo(x+w-topRightX+kappa*topRightX-offsetX, y-offsetY, x+w-offsetX, y+topRightY-kappa*topRightY-offsetY, x+w-offsetX, y+topRightY-offsetY)
	rasterizer.LineTo(x+w-offsetX, y+h-bottomRightY-offsetY)
	rasterizer.CubeTo(x+w-offsetX, y+h-bottomRightY+kappa*bottomRightY-offsetY, x+w-bottomRightX+kappa*bottomRightX-offsetX, y+h-offsetY, x+w-bottomRightX-offsetX, y+h-offsetY)
	rasterizer.LineTo(x+bottomLeftX-offsetX, y+h-offsetY)
	rasterizer.CubeTo(x+bottomLeftX-kappa*bottomLeftX-offsetX, y+h-offsetY, x-offsetX, y+h-bottomLeftY+kappa*bottomLeftY-offsetY, x-offsetX, y+h-bottomLeftY-offsetY)
	rasterizer.LineTo(x-offsetX, y+topLeftY-offsetY)
	rasterizer.CubeTo(x-offsetX, y+topLeftY-kappa*topLeftY-offsetY, x+topLeftX-kappa*topLeftX-offsetX, y-offsetY, x+topLeftX-offsetX, y-offsetY)
	rasterizer.ClosePath()
	return rasterizer
}

// vectorPaintBounds limits the vector rasterizer's coverage buffers to the
// pixels a shape can touch. The x/image/vector rasterizer allocates storage in
// proportion to its declared bounds, so using the target canvas here would
// turn every small rounded box or ellipse into a viewport-sized allocation.
func vectorPaintBounds(target image.Rectangle, shape Rect) image.Rectangle {
	return rectImageBounds(shape).Intersect(target)
}

const vectorFloatingPointThreshold = 512

// newShapeRasterizer preserves x/image/vector's anti-aliasing mode while
// tightening its coverage storage. vector selects floating-point coverage when
// either target dimension exceeds 512. A small shape on such a target needs a
// single padded dimension to retain identical edge pixels, not the target's
// full width and height.
func newShapeRasterizer(bounds, originalTarget image.Rectangle) *vector.Rasterizer {
	width, height := bounds.Dx(), bounds.Dy()
	if (originalTarget.Dx() > vectorFloatingPointThreshold || originalTarget.Dy() > vectorFloatingPointThreshold) &&
		width <= vectorFloatingPointThreshold && height <= vectorFloatingPointThreshold {
		if width >= height {
			width = vectorFloatingPointThreshold + 1
		} else {
			height = vectorFloatingPointThreshold + 1
		}
	}
	return vector.NewRasterizer(width, height)
}

func commandClip(command Command, bounds image.Rectangle) image.Rectangle {
	if !command.HasClip {
		return bounds
	}
	return image.Rect(
		int(math.Floor(command.Clip.X)),
		int(math.Floor(command.Clip.Y)),
		int(math.Ceil(command.Clip.X+command.Clip.Width)),
		int(math.Ceil(command.Clip.Y+command.Clip.Height)),
	).Intersect(bounds)
}

func encodePNG(writer io.Writer, canvas image.Image) error {
	if err := png.Encode(writer, canvas); err != nil {
		return fmt.Errorf("encode PNG: %w", err)
	}
	return nil
}
