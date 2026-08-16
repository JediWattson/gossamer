package render

import (
	"fmt"
	"image"
	"image/color"
	"io"
	"math"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gobolditalic"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/gofont/gomonobolditalic"
	"golang.org/x/image/font/gofont/gomonoitalic"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

type faceKey struct {
	size   int
	weight FontWeight
	style  FontStyle
	family FontFamily
}

type fontBook struct {
	regular        *opentype.Font
	bold           *opentype.Font
	italic         *opentype.Font
	boldItalic     *opentype.Font
	monoRegular    *opentype.Font
	monoBold       *opentype.Font
	monoItalic     *opentype.Font
	monoBoldItalic *opentype.Font
	faces          map[faceKey]font.Face
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
	italic, err := opentype.Parse(goitalic.TTF)
	if err != nil {
		return nil, fmt.Errorf("parse Go Italic font: %w", err)
	}
	boldItalic, err := opentype.Parse(gobolditalic.TTF)
	if err != nil {
		return nil, fmt.Errorf("parse Go Bold Italic font: %w", err)
	}
	monoRegular, err := opentype.Parse(gomono.TTF)
	if err != nil {
		return nil, fmt.Errorf("parse Go Mono font: %w", err)
	}
	monoBold, err := opentype.Parse(gomonobold.TTF)
	if err != nil {
		return nil, fmt.Errorf("parse Go Mono Bold font: %w", err)
	}
	monoItalic, err := opentype.Parse(gomonoitalic.TTF)
	if err != nil {
		return nil, fmt.Errorf("parse Go Mono Italic font: %w", err)
	}
	monoBoldItalic, err := opentype.Parse(gomonobolditalic.TTF)
	if err != nil {
		return nil, fmt.Errorf("parse Go Mono Bold Italic font: %w", err)
	}

	return &fontBook{
		regular:        regular,
		bold:           bold,
		italic:         italic,
		boldItalic:     boldItalic,
		monoRegular:    monoRegular,
		monoBold:       monoBold,
		monoItalic:     monoItalic,
		monoBoldItalic: monoBoldItalic,
		faces:          make(map[faceKey]font.Face),
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

func (book *fontBook) metrics(text string, size float64, weight FontWeight, style FontStyle, family FontFamily) (textMetrics, error) {
	face, err := book.face(size, weight, style, family)
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

func (book *fontBook) xHeight(size float64, weight FontWeight, style FontStyle, family FontFamily) (float64, error) {
	face, err := book.face(size, weight, style, family)
	if err != nil {
		return 0, err
	}
	bounds, _ := font.BoundString(face, "x")
	height := float64(bounds.Max.Y-bounds.Min.Y) / 64
	if height <= 0 || math.IsNaN(height) || math.IsInf(height, 0) {
		return size / 2, nil
	}
	return height, nil
}

func (book *fontBook) draw(
	destination *image.RGBA,
	text string,
	x float64,
	baselineY float64,
	size float64,
	weight FontWeight,
	style FontStyle,
	family FontFamily,
	textColor color.NRGBA,
) error {
	face, err := book.face(size, weight, style, family)
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

func (book *fontBook) face(size float64, weight FontWeight, style FontStyle, family FontFamily) (font.Face, error) {
	if size <= 0 {
		size = 16
	}
	// The bundled font family does not have a separately slanted oblique face.
	// Use the italic face as the current oblique fallback and share its cache
	// entry rather than allocating a duplicate opentype face.
	if style == FontStyleOblique {
		style = FontStyleItalic
	}
	if family != FontFamilyMonospace {
		family = FontFamilySansSerif
	}
	key := faceKey{size: int(math.Round(size * 64)), weight: weight, style: style, family: family}
	if face := book.faces[key]; face != nil {
		return face, nil
	}

	parsedFont := book.regular
	if family == FontFamilyMonospace {
		parsedFont = book.monoRegular
	}
	if style == FontStyleItalic {
		if family == FontFamilyMonospace {
			parsedFont = book.monoItalic
		} else {
			parsedFont = book.italic
		}
	}
	if weight == FontWeightBold && style == FontStyleItalic {
		if family == FontFamilyMonospace {
			parsedFont = book.monoBoldItalic
		} else {
			parsedFont = book.boldItalic
		}
	} else if weight == FontWeightBold {
		if family == FontFamilyMonospace {
			parsedFont = book.monoBold
		} else {
			parsedFont = book.bold
		}
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
