package render

import (
	"fmt"
	"image"
	"image/color"
	"io"
	"math"
	"sync"

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
	systemRegular  *opentype.Font
	systemItalic   *opentype.Font
	faces          map[faceKey]font.Face
}

type textMetrics struct {
	width   float64
	height  float64
	ascent  float64
	descent float64
}

type bundledFontSet struct {
	regular, bold, italic, boldItalic *opentype.Font
	monoRegular, monoBold             *opentype.Font
	monoItalic, monoBoldItalic        *opentype.Font
}

var (
	bundledFontOnce sync.Once
	bundledFontBook bundledFontSet
	bundledFontErr  error
)

func newFontBook() (*fontBook, error) {
	bundledFontOnce.Do(func() {
		parse := func(name string, data []byte) *opentype.Font {
			if bundledFontErr != nil {
				return nil
			}
			font, err := opentype.Parse(data)
			if err != nil {
				bundledFontErr = fmt.Errorf("parse %s font: %w", name, err)
				return nil
			}
			return font
		}
		bundledFontBook = bundledFontSet{
			regular: parse("Go Regular", goregular.TTF), bold: parse("Go Bold", gobold.TTF),
			italic: parse("Go Italic", goitalic.TTF), boldItalic: parse("Go Bold Italic", gobolditalic.TTF),
			monoRegular: parse("Go Mono", gomono.TTF), monoBold: parse("Go Mono Bold", gomonobold.TTF),
			monoItalic: parse("Go Mono Italic", gomonoitalic.TTF), monoBoldItalic: parse("Go Mono Bold Italic", gomonobolditalic.TTF),
		}
	})
	if bundledFontErr != nil {
		return nil, bundledFontErr
	}
	return &fontBook{
		regular: bundledFontBook.regular, bold: bundledFontBook.bold,
		italic: bundledFontBook.italic, boldItalic: bundledFontBook.boldItalic,
		monoRegular: bundledFontBook.monoRegular, monoBold: bundledFontBook.monoBold,
		monoItalic: bundledFontBook.monoItalic, monoBoldItalic: bundledFontBook.monoBoldItalic,
		faces: make(map[faceKey]font.Face),
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
	if family != FontFamilyMonospace && family != FontFamilySystemUI {
		family = FontFamilySansSerif
	}
	key := faceKey{size: int(math.Round(size * 64)), weight: weight, style: style, family: family}
	if face := book.faces[key]; face != nil {
		return face, nil
	}

	parsedFont := book.regular
	if family == FontFamilySystemUI && book.systemRegular == nil {
		platform := loadPlatformFonts()
		book.systemRegular = platform.systemRegular
		book.systemItalic = platform.systemItalic
	}
	if family == FontFamilyMonospace {
		parsedFont = book.monoRegular
	} else if family == FontFamilySystemUI && book.systemRegular != nil {
		parsedFont = book.systemRegular
	}
	if style == FontStyleItalic {
		if family == FontFamilyMonospace {
			parsedFont = book.monoItalic
		} else if family == FontFamilySystemUI && book.systemItalic != nil {
			parsedFont = book.systemItalic
		} else {
			parsedFont = book.italic
		}
	}
	if weight == FontWeightBold && style == FontStyleItalic {
		if family == FontFamilyMonospace {
			parsedFont = book.monoBoldItalic
		} else if family == FontFamilySystemUI && book.systemItalic != nil {
			// x/image/opentype does not expose variable-font weight axes yet.
			// Retain the native italic face rather than changing family metrics.
			parsedFont = book.systemItalic
		} else {
			parsedFont = book.boldItalic
		}
	} else if weight == FontWeightBold {
		if family == FontFamilyMonospace {
			parsedFont = book.monoBold
		} else if family == FontFamilySystemUI && book.systemRegular != nil {
			parsedFont = book.systemRegular
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
