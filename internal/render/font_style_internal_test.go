package render

import (
	"image"
	"image/color"
	"testing"
)

func TestFontBookCachesDistinctNormalItalicAndBoldItalicFaces(t *testing.T) {
	book, err := newFontBook()
	if err != nil {
		t.Fatal(err)
	}
	defer book.Close()
	normal, err := book.face(16, FontWeightNormal, FontStyleNormal, FontFamilySansSerif)
	if err != nil {
		t.Fatal(err)
	}
	italic, err := book.face(16, FontWeightNormal, FontStyleItalic, FontFamilySansSerif)
	if err != nil {
		t.Fatal(err)
	}
	oblique, err := book.face(16, FontWeightNormal, FontStyleOblique, FontFamilySansSerif)
	if err != nil {
		t.Fatal(err)
	}
	boldItalic, err := book.face(16, FontWeightBold, FontStyleItalic, FontFamilySansSerif)
	if err != nil {
		t.Fatal(err)
	}
	if normal == italic || normal == boldItalic || italic == boldItalic {
		t.Fatal("font book reused a face across normal, italic, or bold italic")
	}
	if oblique != italic {
		t.Fatal("current oblique fallback did not reuse the italic face")
	}
	mono, err := book.face(16, FontWeightNormal, FontStyleNormal, FontFamilyMonospace)
	if err != nil {
		t.Fatal(err)
	}
	monoBoldItalic, err := book.face(16, FontWeightBold, FontStyleItalic, FontFamilyMonospace)
	if err != nil {
		t.Fatal(err)
	}
	if mono == normal || monoBoldItalic == boldItalic || mono == monoBoldItalic {
		t.Fatal("font book reused a face across sans-serif and monospace families")
	}
	sansMetrics, err := book.metrics("iiiiiiii", 16, FontWeightNormal, FontStyleNormal, FontFamilySansSerif)
	if err != nil {
		t.Fatal(err)
	}
	monoMetrics, err := book.metrics("iiiiiiii", 16, FontWeightNormal, FontStyleNormal, FontFamilyMonospace)
	if err != nil {
		t.Fatal(err)
	}
	if monoMetrics.width == sansMetrics.width {
		t.Fatal("monospace selection did not change measured text width")
	}
}

func TestFontBookShapesMissingGlyphAdvanceZeroWidthAndReplacementBox(t *testing.T) {
	book, err := newFontBook()
	if err != nil {
		t.Fatal(err)
	}
	defer book.Close()

	base, err := book.metrics("ab", 16, FontWeightNormal, FontStyleNormal, FontFamilySansSerif)
	if err != nil {
		t.Fatal(err)
	}
	withZWJ, err := book.metrics("a\uFE0F\u200Db", 16, FontWeightNormal, FontStyleNormal, FontFamilySansSerif)
	if err != nil {
		t.Fatal(err)
	}
	if withZWJ.width != base.width {
		t.Fatalf("zero-width formatters altered width: got %v want %v", withZWJ.width, base.width)
	}

	const missingSample = "\U000F0001"
	withMissing, err := book.metrics("a"+missingSample+"b", 16, FontWeightNormal, FontStyleNormal, FontFamilySansSerif)
	if err != nil {
		t.Fatal(err)
	}
	if withMissing.width <= base.width {
		t.Fatalf("missing glyph did not advance text width: got %v want > %v", withMissing.width, base.width)
	}

	canvas := image.NewRGBA(image.Rect(0, 0, 48, 32))
	inkColor := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	if err := book.draw(canvas, missingSample, 4, 24, 16, FontWeightNormal, FontStyleNormal, FontFamilySansSerif, inkColor); err != nil {
		t.Fatal(err)
	}
	bounds, exact := book.horizontalTextInkBounds(Command{
		Kind:            DrawTextCommand,
		Text:            missingSample,
		X:               4,
		BaselineY:       24,
		FontSize:        16,
		FontWeight:      FontWeightNormal,
		FontStyle:       FontStyleNormal,
		FontFamily:      FontFamilySansSerif,
		textOrientation: textPaintHorizontal,
	})
	if !exact || bounds.Empty() {
		t.Fatalf("expected exact non-empty missing glyph bounds, got exact=%t bounds=%v", exact, bounds)
	}
	center := image.Pt((bounds.Min.X+bounds.Max.X)/2, (bounds.Min.Y+bounds.Max.Y)/2)
	if got := canvas.RGBAAt(center.X, center.Y); got.A != 0 {
		t.Fatalf("expected hollow replacement box interior at %v, got %+v", center, got)
	}
	if got := canvas.RGBAAt(bounds.Min.X, bounds.Min.Y); got.A == 0 {
		t.Fatalf("expected replacement border ink at top-left %v", bounds.Min)
	}
}
