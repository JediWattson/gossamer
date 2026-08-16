package render

import "testing"

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
