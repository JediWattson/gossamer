package render

import (
	"fmt"
	"image"
	"image/color"
	"io"
	"math"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

type faceKey struct {
	size   int
	weight FontWeight
}

type fontBook struct {
	regular *opentype.Font
	bold    *opentype.Font
	faces   map[faceKey]font.Face
}

type textMetrics struct {
	width   float64
	height  float64
	ascent  float64
	descent float64
}

func newFontBook() (*fontBook, error) {
	regular, err := opentype.Parse(goregular.TTF)
	if err != nil {
		return nil, fmt.Errorf("parse Go Regular font: %w", err)
	}
	bold, err := opentype.Parse(gobold.TTF)
	if err != nil {
		return nil, fmt.Errorf("parse Go Bold font: %w", err)
	}

	return &fontBook{
		regular: regular,
		bold:    bold,
		faces:   make(map[faceKey]font.Face),
	}, nil
}

func (book *fontBook) Close() error {
	var firstError error
	for _, face := range book.faces {
		closer, ok := face.(io.Closer)
		if !ok {
			continue
		}
		if err := closer.Close(); err != nil && firstError == nil {
			firstError = err
		}
	}
	book.faces = nil
	return firstError
}

func (book *fontBook) metrics(text string, size float64, weight FontWeight) (textMetrics, error) {
	face, err := book.face(size, weight)
	if err != nil {
		return textMetrics{}, err
	}
	metrics := face.Metrics()
	return textMetrics{
		width:   float64(font.MeasureString(face, text)) / 64,
		height:  float64(metrics.Height) / 64,
		ascent:  float64(metrics.Ascent) / 64,
		descent: float64(metrics.Descent) / 64,
	}, nil
}

func (book *fontBook) draw(
	destination *image.RGBA,
	text string,
	x float64,
	baselineY float64,
	size float64,
	weight FontWeight,
	textColor color.NRGBA,
) error {
	face, err := book.face(size, weight)
	if err != nil {
		return err
	}

	drawer := font.Drawer{
		Dst:  destination,
		Src:  image.NewUniform(textColor),
		Face: face,
		Dot: fixed.Point26_6{
			X: fixed.Int26_6(math.Round(x * 64)),
			Y: fixed.Int26_6(math.Round(baselineY * 64)),
		},
	}
	drawer.DrawString(text)
	return nil
}

func (book *fontBook) face(size float64, weight FontWeight) (font.Face, error) {
	if size <= 0 {
		size = 16
	}
	key := faceKey{size: int(math.Round(size * 64)), weight: weight}
	if face := book.faces[key]; face != nil {
		return face, nil
	}

	parsedFont := book.regular
	if weight == FontWeightBold {
		parsedFont = book.bold
	}
	face, err := opentype.NewFace(parsedFont, &opentype.FaceOptions{
		Size:    size,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, fmt.Errorf("create %.2fpx font face: %w", size, err)
	}
	book.faces[key] = face
	return face, nil
}
