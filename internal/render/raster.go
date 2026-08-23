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

type rasterLayer struct {
	canvas    *image.RGBA
	kind      rasterLayerKind
	opacity   float64
	transform AffineTransform
	clip      Rect
	radii     CornerRadii
}

func rasterize(displayList DisplayList, fonts *fontBook) (*image.RGBA, error) {
	if displayList.Viewport.Width <= 0 || displayList.Viewport.Height <= 0 {
		return nil, fmt.Errorf(
			"render: invalid viewport %dx%d",
			displayList.Viewport.Width,
			displayList.Viewport.Height,
		)
	}

	bounds := image.Rect(0, 0, displayList.Viewport.Width, displayList.Viewport.Height)
	layers := []rasterLayer{{canvas: image.NewRGBA(bounds), kind: rasterBaseLayer, opacity: 1}}
	verticalTextPixels := 0
	verticalTextUnits := 0
	for _, command := range displayList.Commands {
		canvas := layers[len(layers)-1].canvas
		switch command.Kind {
		case FillRectCommand:
			if !command.Radii.empty() {
				target := draw.Image(canvas)
				if commandNeedsScratch(command) {
					target = image.NewRGBA(canvas.Bounds())
				}
				paintRoundedRect(target, command.Rect, command.Radii, command.Color)
				if scratch, ok := target.(*image.RGBA); ok && scratch != canvas {
					compositeCommandScratch(canvas, scratch, command)
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
			draw.Draw(canvas, rectangle, image.NewUniform(command.Color), image.Point{}, draw.Over)

		case FillEllipseCommand:
			if command.Rect.Width <= 0 || command.Rect.Height <= 0 || command.Color.A == 0 {
				continue
			}
			target := draw.Image(canvas)
			if commandNeedsScratch(command) {
				target = image.NewRGBA(canvas.Bounds())
			}
			paintEllipse(target, command.Rect, command.Color)
			if scratch, ok := target.(*image.RGBA); ok && scratch != canvas {
				compositeCommandScratch(canvas, scratch, command)
			}

		case DrawTextCommand:
			target := canvas
			if commandNeedsScratch(command) {
				target = image.NewRGBA(canvas.Bounds())
			}
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
				)
			}
			if err != nil {
				return nil, err
			}
			if target != canvas {
				compositeCommandScratch(canvas, target, command)
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
			if commandNeedsScratch(command) {
				target := image.NewRGBA(canvas.Bounds())
				xdraw.ApproxBiLinear.Scale(target, destination, command.Image, command.Image.Bounds(), draw.Over, options)
				compositeCommandScratch(canvas, target, command)
			} else {
				xdraw.ApproxBiLinear.Scale(canvas, destination, command.Image, command.Image.Bounds(), draw.Over, options)
			}

		case DrawRoundedBorderCommand:
			target := draw.Image(canvas)
			if commandNeedsScratch(command) {
				target = image.NewRGBA(canvas.Bounds())
			}
			paintRoundedBorder(target, command)
			if scratch, ok := target.(*image.RGBA); ok && scratch != canvas {
				compositeCommandScratch(canvas, scratch, command)
			}

		case DrawBoxShadowCommand:
			target := draw.Image(canvas)
			if commandNeedsScratch(command) {
				target = image.NewRGBA(canvas.Bounds())
			}
			paintBoxShadow(target, command)
			if scratch, ok := target.(*image.RGBA); ok && scratch != canvas {
				compositeCommandScratch(canvas, scratch, command)
			}

		case DrawGradientCommand:
			target := draw.Image(canvas)
			if commandNeedsScratch(command) {
				target = image.NewRGBA(canvas.Bounds())
			}
			paintGradient(target, command)
			if scratch, ok := target.(*image.RGBA); ok && scratch != canvas {
				compositeCommandScratch(canvas, scratch, command)
			}

		case BeginOpacityCommand:
			layers = append(layers, rasterLayer{
				canvas:  image.NewRGBA(bounds),
				kind:    rasterOpacityLayer,
				opacity: clamp(command.Opacity, 0, 1),
			})

		case EndOpacityCommand:
			if len(layers) == 1 || layers[len(layers)-1].kind != rasterOpacityLayer {
				return nil, fmt.Errorf("render: unmatched opacity group end")
			}
			group := layers[len(layers)-1]
			layers = layers[:len(layers)-1]
			compositeRasterLayer(layers[len(layers)-1].canvas, group, bounds)

		case BeginTransformCommand:
			layers = append(layers, rasterLayer{
				canvas: image.NewRGBA(bounds), kind: rasterTransformLayer,
				transform: command.Transform,
			})

		case EndTransformCommand:
			if len(layers) == 1 || layers[len(layers)-1].kind != rasterTransformLayer {
				return nil, fmt.Errorf("render: unmatched transform group end")
			}
			group := layers[len(layers)-1]
			layers = layers[:len(layers)-1]
			compositeRasterLayer(layers[len(layers)-1].canvas, group, bounds)

		case BeginClipCommand:
			layers = append(layers, rasterLayer{
				canvas: image.NewRGBA(bounds), kind: rasterClipLayer,
				clip: command.Rect, radii: command.Radii,
			})

		case EndClipCommand:
			if len(layers) == 1 || layers[len(layers)-1].kind != rasterClipLayer {
				return nil, fmt.Errorf("render: unmatched clip group end")
			}
			group := layers[len(layers)-1]
			layers = layers[:len(layers)-1]
			compositeRasterLayer(layers[len(layers)-1].canvas, group, bounds)

		default:
			return nil, fmt.Errorf("render: unknown display command %d", command.Kind)
		}
	}
	if len(layers) != 1 {
		return nil, fmt.Errorf("render: unclosed opacity group")
	}

	return layers[0].canvas, nil
}

func compositeRasterLayer(destination *image.RGBA, layer rasterLayer, bounds image.Rectangle) {
	switch layer.kind {
	case rasterOpacityLayer:
		alpha := uint8(math.Round(layer.opacity * 255))
		draw.DrawMask(destination, bounds, layer.canvas, bounds.Min, image.NewUniform(color.Alpha{A: alpha}), bounds.Min, draw.Over)
	case rasterTransformLayer:
		matrix := layer.transform
		if !finiteTransform(matrix) {
			return
		}
		xdraw.ApproxBiLinear.Transform(destination, f64.Aff3{
			matrix.A, matrix.C, matrix.E,
			matrix.B, matrix.D, matrix.F,
		}, layer.canvas, bounds, draw.Over, nil)
	case rasterClipLayer:
		clipBounds := rectImageBounds(layer.clip).Intersect(bounds)
		if clipBounds.Empty() {
			return
		}
		mask := image.NewAlpha(clipBounds)
		if layer.radii.empty() {
			draw.Draw(mask, clipBounds, image.NewUniform(color.Alpha{A: 0xff}), image.Point{}, draw.Src)
		} else {
			paintRoundedRect(mask, layer.clip, layer.radii, color.NRGBA{A: 0xff})
		}
		draw.DrawMask(destination, clipBounds, layer.canvas, clipBounds.Min, mask, clipBounds.Min, draw.Over)
	}
}

func finiteTransform(transform AffineTransform) bool {
	return isFinite(transform.A) && isFinite(transform.B) && isFinite(transform.C) &&
		isFinite(transform.D) && isFinite(transform.E) && isFinite(transform.F)
}

func commandNeedsScratch(command Command) bool {
	return command.HasClip || command.RoundedClipCount > 0
}

func compositeCommandScratch(destination, source *image.RGBA, command Command) {
	bounds := destination.Bounds()
	maskBounds := bounds
	if command.HasClip {
		maskBounds = maskBounds.Intersect(commandClip(command, bounds))
	}
	for index := 0; index < int(command.RoundedClipCount); index++ {
		maskBounds = maskBounds.Intersect(rectImageBounds(command.RoundedClips[index].Rect))
	}
	if maskBounds.Empty() {
		return
	}
	mask := image.NewAlpha(maskBounds)
	draw.Draw(mask, maskBounds, image.NewUniform(color.Alpha{A: 0xff}), image.Point{}, draw.Src)
	for index := 0; index < int(command.RoundedClipCount); index++ {
		clip := command.RoundedClips[index]
		shape := image.NewAlpha(maskBounds)
		paintRoundedRect(shape, clip.Rect, clip.Radii, color.NRGBA{A: 0xff})
		for y := maskBounds.Min.Y; y < maskBounds.Max.Y; y++ {
			for x := maskBounds.Min.X; x < maskBounds.Max.X; x++ {
				left := uint16(mask.AlphaAt(x, y).A)
				right := uint16(shape.AlphaAt(x, y).A)
				mask.SetAlpha(x, y, color.Alpha{A: uint8((left*right + 127) / 255)})
			}
		}
	}
	draw.DrawMask(destination, maskBounds, source, maskBounds.Min, mask, maskBounds.Min, draw.Over)
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
	local := rectImageBounds(Rect{
		X:      command.Rect.X + command.ShadowOffsetX - command.ShadowSpread - float64(padding),
		Y:      command.Rect.Y + command.ShadowOffsetY - command.ShadowSpread - float64(padding),
		Width:  command.Rect.Width + 2*command.ShadowSpread + float64(2*padding),
		Height: command.Rect.Height + 2*command.ShadowSpread + float64(2*padding),
	}).Intersect(target.Bounds())
	if command.ShadowInset {
		local = rectImageBounds(command.Rect).Intersect(target.Bounds())
	}
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
		paintRoundedRect(holeMask, hole, holeRadii, color.NRGBA{A: 0xff})
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
		paintRoundedRect(mask, shape, shapeRadii, color.NRGBA{A: 0xff})
	}
	if blurRadius > 0 {
		for pass := 0; pass < 3; pass++ {
			mask = boxBlurAlpha(mask, blurRadius)
		}
	}
	for y := local.Min.Y; y < local.Max.Y; y++ {
		for x := local.Min.X; x < local.Max.X; x++ {
			alpha := mask.AlphaAt(x, y).A
			inside := pointInRoundedRect(command.Rect, command.Radii, float64(x)+0.5, float64(y)+0.5)
			if command.ShadowInset != inside {
				alpha = 0
			}
			if alpha == 0 {
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
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		sum := 0
		for x := bounds.Min.X - radius; x <= bounds.Min.X+radius; x++ {
			if x >= bounds.Min.X && x < bounds.Max.X {
				sum += int(source.AlphaAt(x, y).A)
			}
		}
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			count := min(bounds.Max.X-1, x+radius) - max(bounds.Min.X, x-radius) + 1
			temporary.SetAlpha(x, y, color.Alpha{A: uint8(sum / count)})
			remove, add := x-radius, x+radius+1
			if remove >= bounds.Min.X {
				sum -= int(source.AlphaAt(remove, y).A)
			}
			if add < bounds.Max.X {
				sum += int(source.AlphaAt(add, y).A)
			}
		}
	}
	for x := bounds.Min.X; x < bounds.Max.X; x++ {
		sum := 0
		for y := bounds.Min.Y - radius; y <= bounds.Min.Y+radius; y++ {
			if y >= bounds.Min.Y && y < bounds.Max.Y {
				sum += int(temporary.AlphaAt(x, y).A)
			}
		}
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			count := min(bounds.Max.Y-1, y+radius) - max(bounds.Min.Y, y-radius) + 1
			result.SetAlpha(x, y, color.Alpha{A: uint8(sum / count)})
			remove, add := y-radius, y+radius+1
			if remove >= bounds.Min.Y {
				sum -= int(temporary.AlphaAt(x, remove).A)
			}
			if add < bounds.Max.Y {
				sum += int(temporary.AlphaAt(x, add).A)
			}
		}
	}
	return result
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
	}
	src := color.RGBAModel.Convert(source).(color.RGBA)
	dst := color.RGBAModel.Convert(target.At(x, y)).(color.RGBA)
	inverse := uint32(255 - src.A)
	target.Set(x, y, color.RGBA{
		R: uint8(min(255, uint32(src.R)+uint32(dst.R)*inverse/255)),
		G: uint8(min(255, uint32(src.G)+uint32(dst.G)*inverse/255)),
		B: uint8(min(255, uint32(src.B)+uint32(dst.B)*inverse/255)),
		A: uint8(min(255, uint32(src.A)+uint32(dst.A)*inverse/255)),
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
		command.FontSize, command.FontWeight, command.FontStyle, command.FontFamily, command.Color,
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
		metrics, err := fonts.metrics(unit, command.FontSize, command.FontWeight, command.FontStyle, command.FontFamily)
		if err != nil {
			return err
		}
		cellTop := command.textBounds.Y + float64(index)*advance
		glyphHeight := metrics.ascent + metrics.descent
		baseline := cellTop + math.Max(0, (advance-glyphHeight)/2) + metrics.ascent
		x := command.textBounds.X + (command.textBounds.Width-metrics.width)/2
		if err := fonts.draw(
			destination, unit, x, baseline,
			command.FontSize, command.FontWeight, command.FontStyle, command.FontFamily, command.Color,
		); err != nil {
			return err
		}
	}
	return nil
}

func paintEllipse(target draw.Image, rectangle Rect, fill color.NRGBA) {
	bounds := target.Bounds()
	rasterizer := vector.NewRasterizer(bounds.Dx(), bounds.Dy())
	cx := float32(rectangle.X + rectangle.Width/2 - float64(bounds.Min.X))
	cy := float32(rectangle.Y + rectangle.Height/2 - float64(bounds.Min.Y))
	rx := float32(rectangle.Width / 2)
	ry := float32(rectangle.Height / 2)
	// Cubic approximation of a circle/ellipse, accurate to well below one
	// device pixel at the border sizes emitted by the layout engine.
	const kappa = float32(0.5522847498307936)
	rasterizer.MoveTo(cx+rx, cy)
	rasterizer.CubeTo(cx+rx, cy+kappa*ry, cx+kappa*rx, cy+ry, cx, cy+ry)
	rasterizer.CubeTo(cx-kappa*rx, cy+ry, cx-rx, cy+kappa*ry, cx-rx, cy)
	rasterizer.CubeTo(cx-rx, cy-kappa*ry, cx-kappa*rx, cy-ry, cx, cy-ry)
	rasterizer.CubeTo(cx+kappa*rx, cy-ry, cx+rx, cy-kappa*ry, cx+rx, cy)
	rasterizer.ClosePath()
	rasterizer.Draw(target, bounds, image.NewUniform(fill), image.Point{})
}

func paintRoundedRect(target draw.Image, rectangle Rect, radii CornerRadii, fill color.NRGBA) {
	if rectangle.Width <= 0 || rectangle.Height <= 0 || fill.A == 0 {
		return
	}
	bounds := target.Bounds()
	rasterizer := vector.NewRasterizer(bounds.Dx(), bounds.Dy())
	x := float32(rectangle.X - float64(bounds.Min.X))
	y := float32(rectangle.Y - float64(bounds.Min.Y))
	w := float32(rectangle.Width)
	h := float32(rectangle.Height)
	const kappa = float32(0.5522847498307936)
	topLeftX, topLeftY := float32(radii.TopLeft.X), float32(radii.TopLeft.Y)
	topRightX, topRightY := float32(radii.TopRight.X), float32(radii.TopRight.Y)
	bottomRightX, bottomRightY := float32(radii.BottomRight.X), float32(radii.BottomRight.Y)
	bottomLeftX, bottomLeftY := float32(radii.BottomLeft.X), float32(radii.BottomLeft.Y)

	rasterizer.MoveTo(x+topLeftX, y)
	rasterizer.LineTo(x+w-topRightX, y)
	rasterizer.CubeTo(x+w-topRightX+kappa*topRightX, y, x+w, y+topRightY-kappa*topRightY, x+w, y+topRightY)
	rasterizer.LineTo(x+w, y+h-bottomRightY)
	rasterizer.CubeTo(x+w, y+h-bottomRightY+kappa*bottomRightY, x+w-bottomRightX+kappa*bottomRightX, y+h, x+w-bottomRightX, y+h)
	rasterizer.LineTo(x+bottomLeftX, y+h)
	rasterizer.CubeTo(x+bottomLeftX-kappa*bottomLeftX, y+h, x, y+h-bottomLeftY+kappa*bottomLeftY, x, y+h-bottomLeftY)
	rasterizer.LineTo(x, y+topLeftY)
	rasterizer.CubeTo(x, y+topLeftY-kappa*topLeftY, x+topLeftX-kappa*topLeftX, y, x+topLeftX, y)
	rasterizer.ClosePath()
	rasterizer.Draw(target, bounds, image.NewUniform(fill), image.Point{})
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
