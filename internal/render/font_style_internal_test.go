package render

import "testing"

func TestFontBookCachesDistinctNormalItalicAndBoldItalicFaces(t *testing.T) {
	book, err := newFontBook()
	if err != nil {
		t.Fatal(err)
	}
	defer book.Close()
	normal, err := book.face(16, FontWeightNormal, FontStyleNormal)
	if err != nil {
		t.Fatal(err)
	}
	italic, err := book.face(16, FontWeightNormal, FontStyleItalic)
	if err != nil {
		t.Fatal(err)
	}
	oblique, err := book.face(16, FontWeightNormal, FontStyleOblique)
	if err != nil {
		t.Fatal(err)
	}
	boldItalic, err := book.face(16, FontWeightBold, FontStyleItalic)
	if err != nil {
		t.Fatal(err)
	}
	if normal == italic || normal == boldItalic || italic == boldItalic {
		t.Fatal("font book reused a face across normal, italic, or bold italic")
	}
	if oblique != italic {
		t.Fatal("current oblique fallback did not reuse the italic face")
	}
}
