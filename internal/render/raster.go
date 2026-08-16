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
	"golang.org/x/image/vector"
)

type rasterLayer struct {
	canvas  *image.RGBA
	opacity float64
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
	layers := []rasterLayer{{canvas: image.NewRGBA(bounds), opacity: 1}}
	sidewaysTextPixels := 0
	for _, command := range displayList.Commands {
		canvas := layers[len(layers)-1].canvas
		switch command.Kind {
		case FillRectCommand:
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
			if command.HasClip {
				target = image.NewRGBA(canvas.Bounds())
			}
			paintEllipse(target, command.Rect, command.Color)
			if command.HasClip {
				clip := commandClip(command, canvas.Bounds())
				draw.Draw(canvas, clip, target, clip.Min, draw.Over)
			}

		case DrawTextCommand:
			target := canvas
			if command.HasClip {
				target = image.NewRGBA(canvas.Bounds())
			}
			var err error
			if command.textOrientation == textPaintSidewaysRight {
				var width, height int
				width, height, err = sidewaysTextDimensions(command)
				if err == nil {
					sidewaysTextPixels += width * height
					if sidewaysTextPixels > maxSidewaysTextTotalPixels {
						err = fmt.Errorf("render: sideways text exceeds %d total pixels", maxSidewaysTextTotalPixels)
					} else {
						err = drawSidewaysText(target, fonts, command, width, height)
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
			if command.HasClip {
				clip := commandClip(command, canvas.Bounds())
				draw.Draw(canvas, clip, target, clip.Min, draw.Over)
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
			if command.HasClip {
				target := image.NewRGBA(canvas.Bounds())
				xdraw.ApproxBiLinear.Scale(target, destination, command.Image, command.Image.Bounds(), draw.Over, options)
				clip := commandClip(command, canvas.Bounds())
				draw.Draw(canvas, clip, target, clip.Min, draw.Over)
			} else {
				xdraw.ApproxBiLinear.Scale(canvas, destination, command.Image, command.Image.Bounds(), draw.Over, options)
			}

		case BeginOpacityCommand:
			layers = append(layers, rasterLayer{
				canvas:  image.NewRGBA(bounds),
				opacity: clamp(command.Opacity, 0, 1),
			})

		case EndOpacityCommand:
			if len(layers) == 1 {
				return nil, fmt.Errorf("render: unmatched opacity group end")
			}
			group := layers[len(layers)-1]
			layers = layers[:len(layers)-1]
			destination := layers[len(layers)-1].canvas
			alpha := uint8(math.Round(group.opacity * 255))
			draw.DrawMask(
				destination,
				bounds,
				group.canvas,
				bounds.Min,
				image.NewUniform(color.Alpha{A: alpha}),
				bounds.Min,
				draw.Over,
			)

		default:
			return nil, fmt.Errorf("render: unknown display command %d", command.Kind)
		}
	}
	if len(layers) != 1 {
		return nil, fmt.Errorf("render: unclosed opacity group")
	}

	return layers[0].canvas, nil
}

const (
	maxSidewaysTextPixels      = 4 * 1024 * 1024
	maxSidewaysTextTotalPixels = 32 * 1024 * 1024
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
