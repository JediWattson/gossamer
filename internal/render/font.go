package render

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"io"
	"math"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/JediWattson/gossamer/internal/css"
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
	web    uint32
}

type parsedWebFont struct {
	descriptor WebFontFace
	font       *opentype.Font
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
	webFonts       []parsedWebFont
	webByFamily    map[string][]uint32
	familyLists    map[string][]string
}

type textMetrics struct {
	width   float64
	height  float64
	ascent  float64
	descent float64
}

// shapedRun is the renderer's retained positioning boundary. The current
// shaper emits one glyph per rune with face kerning; GSUB/GPOS and bidi can
// replace this implementation later without changing layout or paint APIs.
type shapedRun struct {
	glyphs  []shapedGlyph
	advance fixed.Int26_6
}

type shapedGlyph struct {
	rune rune
	x    fixed.Int26_6
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

func newFontBook(webFontSets ...[]WebFontFace) (*fontBook, error) {
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
	book := &fontBook{
		regular: bundledFontBook.regular, bold: bundledFontBook.bold,
		italic: bundledFontBook.italic, boldItalic: bundledFontBook.boldItalic,
		monoRegular: bundledFontBook.monoRegular, monoBold: bundledFontBook.monoBold,
		monoItalic: bundledFontBook.monoItalic, monoBoldItalic: bundledFontBook.monoBoldItalic,
		faces:       make(map[faceKey]font.Face),
		webByFamily: make(map[string][]uint32), familyLists: make(map[string][]string),
	}
	if len(webFontSets) != 0 {
		book.webFonts = make([]parsedWebFont, len(webFontSets[0]))
		parsedBySource := make(map[string]*opentype.Font)
		for index, source := range webFontSets[0] {
			book.webFonts[index].descriptor = source
			parsed := parsedBySource[source.Source]
			if parsed == nil || source.Source == "" {
				var parseErr error
				parsed, parseErr = opentype.Parse(source.Data)
				if parseErr != nil {
					continue
				}
				if source.Source != "" {
					parsedBySource[source.Source] = parsed
				}
			}
			book.webFonts[index].font = parsed
			family := normalizeWebFontFamily(source.Family)
			if family != "" {
				book.webByFamily[family] = append(book.webByFamily[family], uint32(index+1))
			}
		}
	}
	return book, nil
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

func (book *fontBook) metrics(text string, size float64, weight FontWeight, style FontStyle, family FontFamily, web ...uint32) (textMetrics, error) {
	face, err := book.face(size, weight, style, family, web...)
	if err != nil {
		return textMetrics{}, err
	}
	metrics := face.Metrics()
	run := shapeText(face, text)
	return textMetrics{
		width:   float64(run.advance) / 64,
		height:  float64(metrics.Height) / 64,
		ascent:  float64(metrics.Ascent) / 64,
		descent: float64(metrics.Descent) / 64,
	}, nil
}

func (book *fontBook) metricsForStyle(text string, style computedStyle) (textMetrics, error) {
	web := book.resolveWebFont(style.FontFamilyValue(), style.FontWeightValue(), style.FontStyle(), text)
	return book.metrics(text, style.FontSize(), style.FontWeight(), style.FontStyle(), style.FontFamily(), web)
}

func (book *fontBook) xHeightForStyle(style computedStyle) (float64, error) {
	web := book.resolveWebFont(style.FontFamilyValue(), style.FontWeightValue(), style.FontStyle(), "x")
	return book.xHeight(style.FontSize(), style.FontWeight(), style.FontStyle(), style.FontFamily(), web)
}

func (book *fontBook) xHeight(size float64, weight FontWeight, style FontStyle, family FontFamily, web ...uint32) (float64, error) {
	face, err := book.face(size, weight, style, family, web...)
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
	web ...uint32,
) error {
	face, err := book.face(size, weight, style, family, web...)
	if err != nil {
		return err
	}

	run := shapeText(face, text)
	origin := fixed.Point26_6{
		X: fixed.Int26_6(math.Round(x * 64)),
		Y: fixed.Int26_6(math.Round(baselineY * 64)),
	}
	source := image.NewUniform(textColor)
	for _, glyph := range run.glyphs {
		dot := origin
		dot.X += glyph.x
		rectangle, mask, maskPoint, _, _ := face.Glyph(dot, glyph.rune)
		if !rectangle.Empty() {
			draw.DrawMask(destination, rectangle, source, image.Point{}, mask, maskPoint, draw.Over)
		}
	}
	return nil
}

func shapeText(face font.Face, text string) shapedRun {
	run := shapedRun{glyphs: make([]shapedGlyph, 0, len(text))}
	previous := rune(-1)
	for _, current := range text {
		if previous >= 0 {
			run.advance += face.Kern(previous, current)
		}
		run.glyphs = append(run.glyphs, shapedGlyph{rune: current, x: run.advance})
		advance, _ := face.GlyphAdvance(current)
		run.advance += advance
		previous = current
	}
	return run
}

func (book *fontBook) face(size float64, weight FontWeight, style FontStyle, family FontFamily, web ...uint32) (font.Face, error) {
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
	webID := uint32(0)
	if len(web) != 0 && web[0] > 0 && int(web[0]) <= len(book.webFonts) && book.webFonts[web[0]-1].font != nil {
		webID = web[0]
	}
	key := faceKey{size: int(math.Round(size * 64)), weight: weight, style: style, family: family, web: webID}
	if face := book.faces[key]; face != nil {
		return face, nil
	}

	parsedFont := book.regular
	if webID != 0 {
		parsedFont = book.webFonts[webID-1].font
	}
	if webID == 0 && family == FontFamilySystemUI && book.systemRegular == nil {
		platform := loadPlatformFonts()
		book.systemRegular = platform.systemRegular
		book.systemItalic = platform.systemItalic
	}
	if webID != 0 {
		// A selected @font-face already represents the requested descriptors.
	} else if family == FontFamilyMonospace {
		parsedFont = book.monoRegular
	} else if family == FontFamilySystemUI && book.systemRegular != nil {
		parsedFont = book.systemRegular
	}
	if webID == 0 && style == FontStyleItalic {
		if family == FontFamilyMonospace {
			parsedFont = book.monoItalic
		} else if family == FontFamilySystemUI && book.systemItalic != nil {
			parsedFont = book.systemItalic
		} else {
			parsedFont = book.italic
		}
	}
	if webID == 0 && weight == FontWeightBold && style == FontStyleItalic {
		if family == FontFamilyMonospace {
			parsedFont = book.monoBoldItalic
		} else if family == FontFamilySystemUI && book.systemItalic != nil {
			// x/image/opentype does not expose variable-font weight axes yet.
			// Retain the native italic face rather than changing family metrics.
			parsedFont = book.systemItalic
		} else {
			parsedFont = book.boldItalic
		}
	} else if webID == 0 && weight == FontWeightBold {
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

func (book *fontBook) resolveWebFont(familyList string, requestedWeight int, style FontStyle, text string) uint32 {
	if book == nil || len(book.webFonts) == 0 || strings.TrimSpace(familyList) == "" {
		return 0
	}
	names, ok := book.familyLists[familyList]
	if !ok {
		names = parseComputedFontFamilies(familyList)
		book.familyLists[familyList] = names
	}
	if requestedWeight < 1 || requestedWeight > 1000 {
		requestedWeight = 400
	}
	for _, name := range names {
		candidates := book.webByFamily[normalizeWebFontFamily(name)]
		bestID, bestScore := uint32(0), int(^uint(0)>>1)
		bestMissing := 0
		for _, id := range candidates {
			parsedCandidate := book.webFonts[id-1]
			candidate := parsedCandidate.descriptor
			styleScore := 0
			candidateStyle := candidate.Style
			if candidateStyle == FontStyleOblique {
				candidateStyle = FontStyleItalic
			}
			requestedStyle := style
			if requestedStyle == FontStyleOblique {
				requestedStyle = FontStyleItalic
			}
			if candidateStyle != requestedStyle {
				styleScore = 2000
			}
			minWeight, maxWeight := candidate.WeightMin, candidate.WeightMax
			if minWeight <= 0 {
				minWeight = 400
			}
			if maxWeight < minWeight {
				maxWeight = minWeight
			}
			weightScore := 0
			if requestedWeight < minWeight {
				weightScore = minWeight - requestedWeight
			} else if requestedWeight > maxWeight {
				weightScore = requestedWeight - maxWeight
			}
			missing := webFontMissingGlyphs(parsedCandidate, text)
			score := missing*10000 + styleScore + weightScore
			if score < bestScore {
				bestID, bestScore = id, score
				bestMissing = missing
			}
		}
		if bestID != 0 {
			if runeCount := utf8.RuneCountInString(text); runeCount != 0 && bestMissing >= runeCount {
				continue
			}
			return bestID
		}
	}
	return 0
}

func webFontMissingGlyphs(candidate parsedWebFont, text string) int {
	missing := 0
	for _, current := range text {
		if !webFontRangeContains(candidate.descriptor.UnicodeRanges, current) {
			missing++
			continue
		}
		glyph, err := candidate.font.GlyphIndex(nil, current)
		if err != nil || glyph == 0 {
			missing++
		}
	}
	return missing
}

func webFontRangeContains(ranges []css.UnicodeRange, value rune) bool {
	if len(ranges) == 0 {
		return true
	}
	for _, current := range ranges {
		if value >= current.Start && value <= current.End {
			return true
		}
	}
	return false
}

func normalizeWebFontFamily(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func parseComputedFontFamilies(source string) []string {
	values, err := css.ParseComponentValues(source)
	if err != nil {
		return nil
	}
	groups := [][]css.ComponentValue{{}}
	for _, value := range values {
		if value.Kind == css.ComponentToken && value.Token.Kind == css.TokenComma {
			groups = append(groups, nil)
			continue
		}
		groups[len(groups)-1] = append(groups[len(groups)-1], value)
	}
	result := make([]string, 0, len(groups))
	for _, group := range groups {
		parts := make([]string, 0, len(group))
		for _, value := range group {
			if value.Kind != css.ComponentToken || value.Token.Incomplete {
				parts = nil
				break
			}
			switch value.Token.Kind {
			case css.TokenWhitespace:
				continue
			case css.TokenString:
				if len(group) != 1 {
					parts = nil
				} else {
					parts = append(parts, value.Token.Value)
				}
			case css.TokenIdent:
				parts = append(parts, value.Token.Value)
			default:
				parts = nil
			}
			if parts == nil {
				break
			}
		}
		if len(parts) != 0 {
			result = append(result, strings.Join(parts, " "))
		}
	}
	return result
}

func cloneWebFonts(fonts []WebFontFace) []WebFontFace {
	result := make([]WebFontFace, len(fonts))
	dataBySource := make(map[string][]byte)
	for index, face := range fonts {
		result[index] = face
		data := dataBySource[face.Source]
		if data == nil || face.Source == "" {
			data = append([]byte(nil), face.Data...)
			if face.Source != "" {
				dataBySource[face.Source] = data
			}
		}
		result[index].Data = data
		result[index].UnicodeRanges = append([]css.UnicodeRange(nil), face.UnicodeRanges...)
	}
	return result
}
