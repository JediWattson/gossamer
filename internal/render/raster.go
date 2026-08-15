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
	for _, command := range displayList.Commands {
		canvas := layers[len(layers)-1].canvas
		switch command.Kind {
		case FillRectCommand:
			rectangle := image.Rect(
				int(math.Floor(command.Rect.X)),
				int(math.Floor(command.Rect.Y)),
				int(math.Ceil(command.Rect.X+command.Rect.Width)),
				int(math.Ceil(command.Rect.Y+command.Rect.Height)),
			).Intersect(canvas.Bounds())
			if rectangle.Empty() {
				continue
			}
			draw.Draw(canvas, rectangle, image.NewUniform(command.Color), image.Point{}, draw.Over)

		case DrawTextCommand:
			if err := fonts.draw(
				canvas,
				command.Text,
				command.X,
				command.BaselineY,
				command.FontSize,
				command.FontWeight,
				command.Color,
			); err != nil {
				return nil, err
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
			xdraw.ApproxBiLinear.Scale(canvas, destination, command.Image, command.Image.Bounds(), draw.Over, options)

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

func encodePNG(writer io.Writer, canvas image.Image) error {
	if err := png.Encode(writer, canvas); err != nil {
		return fmt.Errorf("encode PNG: %w", err)
	}
	return nil
}
