package render

import (
	"bytes"
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
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

type faceKey struct {
	size     int
	weight   FontWeight
	style    FontStyle
	family   FontFamily
	web      uint32
	fallback uint8
}

type parsedWebFont struct {
	descriptor WebFontFace
	font       *opentype.Font
}

// webFontIdentity is a zero-copy identity for immutable frame font data. The
// Frame contract keeps Data and UnicodeRanges immutable while retained, so
// backing-array identity plus descriptor values can be compared on every
// frame without hashing or copying multi-megabyte font programs.
type webFontIdentity struct {
	family        string
	style         FontStyle
	weightMin     int
	weightMax     int
	source        string
	data          *byte
	dataLength    int
	dataBytes     []byte
	unicodeRanges *css.UnicodeRange
	rangeLength   int
	ranges        []css.UnicodeRange
}

func snapshotWebFonts(fonts []WebFontFace) []webFontIdentity {
	if len(fonts) == 0 {
		return nil
	}
	result := make([]webFontIdentity, len(fonts))
	for index := range fonts {
		result[index] = identifyWebFont(fonts[index])
	}
	return result
}

func sameWebFonts(snapshot []webFontIdentity, fonts []WebFontFace) bool {
	if len(snapshot) != len(fonts) {
		return false
	}
	for index := range fonts {
		if !sameWebFont(snapshot[index], fonts[index]) {
			return false
		}
	}
	return true
}

func sameWebFont(snapshot webFontIdentity, face WebFontFace) bool {
	current := identifyWebFont(face)
	if snapshot.family != current.family || snapshot.style != current.style ||
		snapshot.weightMin != current.weightMin || snapshot.weightMax != current.weightMax ||
		snapshot.source != current.source || snapshot.dataLength != current.dataLength ||
		snapshot.rangeLength != current.rangeLength {
		return false
	}
	if snapshot.data != current.data && !bytes.Equal(snapshot.dataBytes, current.dataBytes) {
		return false
	}
	if snapshot.unicodeRanges == current.unicodeRanges {
		return true
	}
	for index := range snapshot.ranges {
		if snapshot.ranges[index] != current.ranges[index] {
			return false
		}
	}
	return true
}

func identifyWebFont(face WebFontFace) webFontIdentity {
	var data *byte
	if len(face.Data) != 0 {
		data = &face.Data[0]
	}
	var ranges *css.UnicodeRange
	if len(face.UnicodeRanges) != 0 {
		ranges = &face.UnicodeRanges[0]
	}
	return webFontIdentity{
		family: face.Family, style: face.Style,
		weightMin: face.WeightMin, weightMax: face.WeightMax,
		source: face.Source, data: data, dataLength: len(face.Data), dataBytes: face.Data,
		unicodeRanges: ranges, rangeLength: len(face.UnicodeRanges), ranges: face.UnicodeRanges,
	}
}

type fontBook struct {
	mutex          sync.Mutex
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
	systemSymbols  *opentype.Font
	emoji          *colorEmojiFont
	platformLoaded bool
	glyphBuffer    sfnt.Buffer
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
	rune       rune
	x          fixed.Int26_6
	advance    fixed.Int26_6
	face       font.Face
	emoji      *colorEmojiFont
	emojiGlyph uint16
	missing    bool
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
	book.mutex.Lock()
	defer book.mutex.Unlock()
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
	book.mutex.Lock()
	defer book.mutex.Unlock()
	webID := uint32(0)
	if len(web) != 0 {
		webID = web[0]
	}
	face, parsed, _, err := book.primaryFaceLocked(size, weight, style, family, webID)
	if err != nil {
		return textMetrics{}, err
	}
	metrics := face.Metrics()
	run, err := book.shapeRunLocked(face, parsed, size, weight, style, family, webID, text)
	if err != nil {
		return textMetrics{}, err
	}
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
	book.mutex.Lock()
	defer book.mutex.Unlock()
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
	book.mutex.Lock()
	defer book.mutex.Unlock()
	webID := uint32(0)
	if len(web) != 0 {
		webID = web[0]
	}
	face, parsed, _, err := book.primaryFaceLocked(size, weight, style, family, webID)
	if err != nil {
		return err
	}

	run, err := book.shapeRunLocked(face, parsed, size, weight, style, family, webID, text)
	if err != nil {
		return err
	}
	origin := fixed.Point26_6{
		X: fixed.Int26_6(math.Round(x * 64)),
		Y: fixed.Int26_6(math.Round(baselineY * 64)),
	}
	source := image.NewUniform(textColor)
	for _, glyph := range run.glyphs {
		dot := origin
		dot.X += glyph.x
		if glyph.emoji != nil {
			if emojiImg, ascentPx, ok := glyph.emoji.glyphBitmap(glyph.emojiGlyph, size); ok {
				rect := emojiImg.Bounds().Add(image.Pt(dot.X.Round(), dot.Y.Round()-ascentPx))
				if textColor.A == 255 {
					draw.Draw(destination, rect, emojiImg, emojiImg.Bounds().Min, draw.Over)
				} else if textColor.A > 0 {
					draw.DrawMask(destination, rect, emojiImg, emojiImg.Bounds().Min, image.NewUniform(color.Alpha{A: textColor.A}), image.Point{}, draw.Over)
				}
			}
			continue
		}
		if glyph.missing {
			drawMissingGlyphBox(destination, missingGlyphRect(dot, size, glyph.advance), textColor)
			continue
		}
		if glyph.face == nil {
			continue
		}
		rectangle, mask, maskPoint, _, _ := glyph.face.Glyph(dot, glyph.rune)
		if !rectangle.Empty() {
			draw.DrawMask(destination, rectangle, source, image.Point{}, mask, maskPoint, draw.Over)
		}
	}
	return nil
}

// horizontalTextInkBounds returns the exact hinted device-pixel rectangles
// used by draw for one already device-scaled command. Line/advance geometry is
// deliberately insufficient here because italic and web-font glyphs may
// overhang it. Vertical text is painted through intermediate surfaces and is
// reported as inexact so callers can choose a conservative clip.
func (book *fontBook) horizontalTextInkBounds(command Command) (image.Rectangle, bool) {
	if book == nil || command.Kind != DrawTextCommand || command.textOrientation != textPaintHorizontal {
		return image.Rectangle{}, false
	}
	book.mutex.Lock()
	defer book.mutex.Unlock()
	face, parsed, _, err := book.primaryFaceLocked(command.FontSize, command.FontWeight, command.FontStyle, command.FontFamily, command.WebFont)
	if err != nil {
		return image.Rectangle{}, false
	}
	run, err := book.shapeRunLocked(face, parsed, command.FontSize, command.FontWeight, command.FontStyle, command.FontFamily, command.WebFont, command.Text)
	if err != nil {
		return image.Rectangle{}, false
	}
	origin := fixed.Point26_6{
		X: fixed.Int26_6(math.Round(command.X * 64)),
		Y: fixed.Int26_6(math.Round(command.BaselineY * 64)),
	}
	var bounds image.Rectangle
	for _, glyph := range run.glyphs {
		dot := origin
		dot.X += glyph.x
		var rectangle image.Rectangle
		if glyph.emoji != nil {
			targetBox, ascentPx := glyph.emoji.targetSize(command.FontSize)
			left := dot.X.Round()
			top := dot.Y.Round() - ascentPx
			rectangle = image.Rect(left, top, left+targetBox, top+targetBox)
		} else if glyph.missing {
			rectangle = missingGlyphRect(dot, command.FontSize, glyph.advance)
		} else if glyph.face != nil {
			rectangle, _, _, _, _ = glyph.face.Glyph(dot, glyph.rune)
		}
		if rectangle.Empty() {
			continue
		}
		if bounds.Empty() {
			bounds = rectangle
		} else {
			bounds = bounds.Union(rectangle)
		}
	}
	return bounds, true
}

func (book *fontBook) shapeRunLocked(
	primaryFace font.Face,
	primaryFont *opentype.Font,
	size float64,
	weight FontWeight,
	style FontStyle,
	family FontFamily,
	webID uint32,
	text string,
) (shapedRun, error) {
	if size <= 0 {
		size = 16
	}
	run := shapedRun{glyphs: make([]shapedGlyph, 0, len(text))}
	var prevFace font.Face
	previous := rune(-1)
	appendRune := func(current rune) {
		if isZeroWidthIgnorable(current) {
			return
		}
		activeFace, activeOK := book.resolveRuneFaceLocked(primaryFace, primaryFont, size, weight, style, family, webID, current)
		if !activeOK {
			if book.emoji != nil {
				if gid, advUnits, _, emojiOK := book.emoji.lookupCluster(string(current)); emojiOK {
					adv := book.emoji.glyphAdvanceFixed(advUnits, size)
					run.glyphs = append(run.glyphs, shapedGlyph{rune: current, x: run.advance, advance: adv, emoji: book.emoji, emojiGlyph: gid})
					run.advance += adv
					prevFace = nil
					previous = -1
					return
				}
			}
			adv := missingGlyphAdvance(size)
			run.glyphs = append(run.glyphs, shapedGlyph{rune: current, x: run.advance, advance: adv, missing: true})
			run.advance += adv
			prevFace = nil
			previous = -1
			return
		}
		if prevFace == activeFace && previous >= 0 {
			run.advance += activeFace.Kern(previous, current)
		}
		advance, ok := activeFace.GlyphAdvance(current)
		if !ok || advance < 0 {
			advance = 0
		}
		run.glyphs = append(run.glyphs, shapedGlyph{rune: current, x: run.advance, advance: advance, face: activeFace})
		run.advance += advance
		prevFace = activeFace
		previous = current
	}
	if !mayContainClusterOrEmoji(text) {
		for _, current := range text {
			appendRune(current)
		}
		return run, nil
	}
	book.ensurePlatformFontsLocked()
	for _, unit := range splitVerticalTextUnits(text) {
		if book.emoji != nil {
			if gid, advUnits, explicitEmoji, ok := book.emoji.lookupCluster(unit); ok && explicitEmoji {
				adv := book.emoji.glyphAdvanceFixed(advUnits, size)
				run.glyphs = append(run.glyphs, shapedGlyph{x: run.advance, advance: adv, emoji: book.emoji, emojiGlyph: gid})
				run.advance += adv
				prevFace = nil
				previous = -1
				continue
			}
		}
		for _, current := range unit {
			appendRune(current)
		}
	}
	return run, nil
}

func (book *fontBook) resolveRuneFaceLocked(
	primaryFace font.Face,
	primaryFont *opentype.Font,
	size float64,
	weight FontWeight,
	style FontStyle,
	family FontFamily,
	webID uint32,
	r rune,
) (font.Face, bool) {
	if book.fontHasGlyphLocked(primaryFont, r) {
		return primaryFace, true
	}
	bundledFallback := book.bundledFontForStyleLocked(weight, style, FontFamilySansSerif)
	if bundledFallback != nil && bundledFallback != primaryFont && book.fontHasGlyphLocked(bundledFallback, r) {
		face, err := book.cachedFaceLocked(size, weight, style, FontFamilySansSerif, 0, 1, bundledFallback)
		if err == nil {
			return face, true
		}
	}
	book.ensurePlatformFontsLocked()
	systemCandidate := book.systemRegular
	if style == FontStyleItalic || style == FontStyleOblique {
		if book.systemItalic != nil {
			systemCandidate = book.systemItalic
		}
	}
	if systemCandidate != nil && systemCandidate != primaryFont && book.fontHasGlyphLocked(systemCandidate, r) {
		face, err := book.cachedFaceLocked(size, weight, style, FontFamilySystemUI, 0, 2, systemCandidate)
		if err == nil {
			return face, true
		}
	}
	if book.systemSymbols != nil && book.systemSymbols != primaryFont && book.fontHasGlyphLocked(book.systemSymbols, r) {
		face, err := book.cachedFaceLocked(size, FontWeightNormal, FontStyleNormal, FontFamilySystemUI, 0, 3, book.systemSymbols)
		if err == nil {
			return face, true
		}
	}
	return nil, false
}

func (book *fontBook) fontHasGlyphLocked(parsed *opentype.Font, r rune) bool {
	if parsed == nil {
		return false
	}
	glyph, err := parsed.GlyphIndex(&book.glyphBuffer, r)
	return err == nil && glyph != 0
}

func (book *fontBook) ensurePlatformFontsLocked() {
	if book.platformLoaded {
		return
	}
	book.platformLoaded = true
	platform := loadPlatformFonts()
	book.systemRegular = platform.systemRegular
	book.systemItalic = platform.systemItalic
	book.systemSymbols = platform.systemSymbols
	book.emoji = platform.emoji
}

func isZeroWidthIgnorable(r rune) bool {
	return r == '\u200B' || r == '\u200C' || r == '\u200D' || r == '\u2060' || r == '\uFEFF' || r == '\u00AD' ||
		(r >= 0xFE00 && r <= 0xFE0F) || (r >= 0xE0100 && r <= 0xE01EF)
}

func missingGlyphAdvance(size float64) fixed.Int26_6 {
	if size <= 0 {
		size = 16
	}
	return fixed.Int26_6(math.Round(size * 0.65 * 64))
}

func missingGlyphRect(dot fixed.Point26_6, size float64, advance fixed.Int26_6) image.Rectangle {
	if size <= 0 {
		size = 16
	}
	advPx := int(math.Round(float64(advance) / 64))
	if advPx < 4 {
		advPx = int(math.Round(math.Max(size*0.6, 4)))
	}
	height := int(math.Round(size * 0.72))
	if height < 4 {
		height = 4
	}
	x0 := dot.X.Round() + 1
	x1 := dot.X.Round() + advPx - 1
	if x1 <= x0 {
		x1 = x0 + 2
	}
	baseline := dot.Y.Round()
	return image.Rect(x0, baseline-height, x1, baseline)
}

func drawMissingGlyphBox(destination *image.RGBA, rect image.Rectangle, textColor color.NRGBA) {
	if destination == nil || rect.Empty() || textColor.A == 0 {
		return
	}
	clipped := rect.Intersect(destination.Bounds())
	if clipped.Empty() {
		return
	}
	borderColor := textColor
	borderColor.A = uint8(max(1, int(borderColor.A)*2/3))
	source := image.NewUniform(borderColor)
	edges := [4]image.Rectangle{
		image.Rect(rect.Min.X, rect.Min.Y, rect.Max.X, rect.Min.Y+1),
		image.Rect(rect.Min.X, rect.Max.Y-1, rect.Max.X, rect.Max.Y),
		image.Rect(rect.Min.X, rect.Min.Y+1, rect.Min.X+1, rect.Max.Y-1),
		image.Rect(rect.Max.X-1, rect.Min.Y+1, rect.Max.X, rect.Max.Y-1),
	}
	for _, edge := range edges {
		if edgeClipped := edge.Intersect(clipped); !edgeClipped.Empty() {
			draw.Draw(destination, edgeClipped, source, image.Point{}, draw.Over)
		}
	}
}

func (book *fontBook) primaryFaceLocked(size float64, weight FontWeight, style FontStyle, family FontFamily, webID uint32) (font.Face, *opentype.Font, uint32, error) {
	if size <= 0 {
		size = 16
	}
	if style == FontStyleOblique {
		style = FontStyleItalic
	}
	if family != FontFamilyMonospace && family != FontFamilySystemUI {
		family = FontFamilySansSerif
	}
	resolvedWeb := uint32(0)
	if webID > 0 && int(webID) <= len(book.webFonts) && book.webFonts[webID-1].font != nil {
		resolvedWeb = webID
	}
	parsedFont := book.regular
	if resolvedWeb != 0 {
		parsedFont = book.webFonts[resolvedWeb-1].font
	} else {
		if family == FontFamilySystemUI {
			book.ensurePlatformFontsLocked()
		}
		parsedFont = book.bundledFontForStyleLocked(weight, style, family)
	}
	face, err := book.cachedFaceLocked(size, weight, style, family, resolvedWeb, 0, parsedFont)
	if err != nil {
		return nil, nil, 0, err
	}
	return face, parsedFont, resolvedWeb, nil
}

func (book *fontBook) bundledFontForStyleLocked(weight FontWeight, style FontStyle, family FontFamily) *opentype.Font {
	parsedFont := book.regular
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
	return parsedFont
}

func (book *fontBook) cachedFaceLocked(size float64, weight FontWeight, style FontStyle, family FontFamily, webID uint32, fallback uint8, parsedFont *opentype.Font) (font.Face, error) {
	if size <= 0 {
		size = 16
	}
	key := faceKey{size: int(math.Round(size * 64)), weight: weight, style: style, family: family, web: webID, fallback: fallback}
	if face := book.faces[key]; face != nil {
		return face, nil
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

func (book *fontBook) face(size float64, weight FontWeight, style FontStyle, family FontFamily, web ...uint32) (font.Face, error) {
	webID := uint32(0)
	if len(web) != 0 {
		webID = web[0]
	}
	face, _, _, err := book.primaryFaceLocked(size, weight, style, family, webID)
	return face, err
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
