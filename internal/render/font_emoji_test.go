package render

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"runtime"
	"testing"
)

func TestFontBookRendersColorEmojiClustersOnDarwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS Apple Color Emoji.ttc test")
	}
	book, err := newFontBook()
	if err != nil {
		t.Fatal(err)
	}
	defer book.Close()

	for _, sample := range []string{"🇺🇸", "😀", "👩🏽‍💻", "1️⃣", "❤️"} {
		t.Run(sample, func(t *testing.T) {
			metrics, err := book.metrics(sample, 24, FontWeightNormal, FontStyleNormal, FontFamilySystemUI)
			if err != nil {
				t.Fatalf("metrics(%q) error = %v", sample, err)
			}
			if metrics.width <= 10 || metrics.width > 40 {
				t.Fatalf("metrics(%q).width = %.2f, want single emoji cluster advance (~24)", sample, metrics.width)
			}
			canvas := image.NewRGBA(image.Rect(0, 0, 48, 48))
			if err := book.draw(canvas, sample, 4, 32, 24, FontWeightNormal, FontStyleNormal, FontFamilySystemUI, color.NRGBA{A: 0xff}); err != nil {
				t.Fatalf("draw(%q) error = %v", sample, err)
			}
			inkBounds, exact := book.horizontalTextInkBounds(Command{
				Kind:       DrawTextCommand,
				Text:       sample,
				X:          4,
				BaselineY:  32,
				FontSize:   24,
				FontFamily: FontFamilySystemUI,
			})
			if !exact || inkBounds.Empty() {
				t.Fatalf("horizontalTextInkBounds(%q) = %v, exact=%v, want non-empty exact bounds", sample, inkBounds, exact)
			}
			coloredPixels := 0
			for y := 0; y < 48; y++ {
				for x := 0; x < 48; x++ {
					px := canvas.RGBAAt(x, y)
					if px.A > 0 && (px.R != px.G || px.G != px.B) {
						coloredPixels++
					}
				}
			}
			if coloredPixels == 0 {
				t.Fatalf("rendered %q had zero colored pixels (fallback monochrome/missing box?)", sample)
			}
		})
	}
}

func TestParseSyntheticSBIXEmojiFontAndFlip(t *testing.T) {
	t.Parallel()

	// Build a 4x4 PNG whose left pixel is red and right pixel is blue.
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		img.SetNRGBA(0, y, color.NRGBA{R: 0xff, A: 0xff})
		img.SetNRGBA(3, y, color.NRGBA{B: 0xff, A: 0xff})
	}
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		t.Fatal(err)
	}

	buildTable := func(tag string, data []byte) ([]byte, []byte) {
		return []byte(tag), data
	}
	_ = buildTable

	head := make([]byte, 54)
	binary.BigEndian.PutUint16(head[18:20], 800)

	hhea := make([]byte, 36)
	binary.BigEndian.PutUint16(hhea[34:36], 3)

	maxp := make([]byte, 6)
	binary.BigEndian.PutUint16(maxp[4:6], 3)

	hmtx := make([]byte, 12)
	binary.BigEndian.PutUint16(hmtx[4:6], 800)
	binary.BigEndian.PutUint16(hmtx[8:10], 1000)

	// post v2.0 with glyph 1 = u1F1FA_u1F1F8, glyph 2 = u1F1E8_u1F1E6
	var post bytes.Buffer
	_ = binary.Write(&post, binary.BigEndian, uint32(0x00020000))
	post.Write(make([]byte, 28))
	_ = binary.Write(&post, binary.BigEndian, uint16(3))
	_ = binary.Write(&post, binary.BigEndian, uint16(0))
	_ = binary.Write(&post, binary.BigEndian, uint16(258))
	_ = binary.Write(&post, binary.BigEndian, uint16(259))
	for _, name := range []string{"u1F1FA_u1F1F8", "u1F1E8_u1F1E6"} {
		post.WriteByte(byte(len(name)))
		post.WriteString(name)
	}

	// sbix with 1 strike ppem=32, glyph 1 = "png ", glyph 2 = "flip" referencing glyph 1.
	var sbix bytes.Buffer
	_ = binary.Write(&sbix, binary.BigEndian, uint16(1))
	_ = binary.Write(&sbix, binary.BigEndian, uint16(1))
	_ = binary.Write(&sbix, binary.BigEndian, uint32(1))
	_ = binary.Write(&sbix, binary.BigEndian, uint32(12)) // strike offset
	// strike header
	_ = binary.Write(&sbix, binary.BigEndian, uint16(32))
	_ = binary.Write(&sbix, binary.BigEndian, uint16(72))
	glyph1Start := uint32(4 + 4*4)
	glyph1Len := uint32(8 + pngBuf.Len())
	glyph2Start := glyph1Start + glyph1Len
	glyph2Len := uint32(8 + 2)
	glyph3Start := glyph2Start + glyph2Len
	_ = binary.Write(&sbix, binary.BigEndian, uint32(0))
	_ = binary.Write(&sbix, binary.BigEndian, glyph1Start)
	_ = binary.Write(&sbix, binary.BigEndian, glyph2Start)
	_ = binary.Write(&sbix, binary.BigEndian, glyph3Start)
	// glyph 1 data
	_ = binary.Write(&sbix, binary.BigEndian, int16(0))
	_ = binary.Write(&sbix, binary.BigEndian, int16(0))
	sbix.WriteString("png ")
	sbix.Write(pngBuf.Bytes())
	// glyph 2 flip -> glyph 1
	_ = binary.Write(&sbix, binary.BigEndian, int16(0))
	_ = binary.Write(&sbix, binary.BigEndian, int16(0))
	sbix.WriteString("flip")
	_ = binary.Write(&sbix, binary.BigEndian, uint16(1))

	type tbl struct {
		tag  string
		data []byte
	}
	tbls := []tbl{
		{"head", head},
		{"hhea", hhea},
		{"hmtx", hmtx},
		{"maxp", maxp},
		{"post", post.Bytes()},
		{"sbix", sbix.Bytes()},
	}
	var sfnt bytes.Buffer
	_ = binary.Write(&sfnt, binary.BigEndian, uint32(0x00010000))
	_ = binary.Write(&sfnt, binary.BigEndian, uint16(len(tbls)))
	_ = binary.Write(&sfnt, binary.BigEndian, uint16(0))
	_ = binary.Write(&sfnt, binary.BigEndian, uint16(0))
	_ = binary.Write(&sfnt, binary.BigEndian, uint16(0))
	offset := uint32(12 + len(tbls)*16)
	for _, t := range tbls {
		sfnt.WriteString(t.tag)
		_ = binary.Write(&sfnt, binary.BigEndian, uint32(0))
		_ = binary.Write(&sfnt, binary.BigEndian, offset)
		_ = binary.Write(&sfnt, binary.BigEndian, uint32(len(t.data)))
		offset += uint32(len(t.data))
	}
	for _, t := range tbls {
		sfnt.Write(t.data)
	}

	parsed, err := parseColorEmojiFont(bytes.NewReader(sfnt.Bytes()))
	if err != nil {
		t.Fatalf("parseColorEmojiFont() error = %v", err)
	}
	gid1, adv1, exp1, ok1 := parsed.lookupCluster("🇺🇸")
	if !ok1 || !exp1 || gid1 != 1 || adv1 != 800 {
		t.Fatalf("lookupCluster(US) = (%d, %d, %v, %v), want (1, 800, true, true)", gid1, adv1, exp1, ok1)
	}
	gid2, adv2, _, ok2 := parsed.lookupCluster("🇨🇦")
	if !ok2 || gid2 != 2 || adv2 != 1000 {
		t.Fatalf("lookupCluster(CA) = (%d, %d, %v), want (2, 1000, true)", gid2, adv2, ok2)
	}
	bmp2, _, ok := parsed.glyphBitmap(gid2, 4)
	if !ok {
		t.Fatal("glyphBitmap(flip) failed")
	}
	// Glyph 1 had red at x=0 and blue at x=3; horizontally flipped glyph 2 has blue at x=0 and red at x=3.
	if got := bmp2.NRGBAAt(0, 0); got != (color.NRGBA{B: 0xff, A: 0xff}) {
		t.Fatalf("flipped pixel (0,0) = %#v, want blue", got)
	}
	if got := bmp2.NRGBAAt(3, 0); got != (color.NRGBA{R: 0xff, A: 0xff}) {
		t.Fatalf("flipped pixel (3,0) = %#v, want red", got)
	}
}
