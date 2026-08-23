package resource_test

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/JediWattson/gossamer/internal/resource"
	gofont "github.com/venusliang/go-font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
)

func TestDecodeFontRetainsValidatedSFNT(t *testing.T) {
	decoded, err := resource.DecodeFontBytes(goregular.TTF)
	if err != nil {
		t.Fatalf("DecodeFontBytes() error = %v", err)
	}
	if len(decoded) != len(goregular.TTF) {
		t.Fatalf("decoded bytes = %d, want %d", len(decoded), len(goregular.TTF))
	}
	if _, err := opentype.Parse(decoded); err != nil {
		t.Fatalf("normalized font is not renderable: %v", err)
	}
	decoded[0] ^= 0xff
	if decoded[0] == goregular.TTF[0] {
		t.Fatal("decoded font aliases source bytes")
	}
}

func TestDecodeFontNormalizesWOFF(t *testing.T) {
	compressed := testWOFF(t, goregular.TTF)
	decoded, err := resource.DecodeFontBytes(compressed)
	if err != nil {
		t.Fatalf("DecodeFontBytes(WOFF) error = %v", err)
	}
	if bytes.Equal(decoded, compressed) {
		t.Fatal("WOFF bytes were not normalized")
	}
	if _, err := opentype.Parse(decoded); err != nil {
		t.Fatalf("normalized WOFF is not renderable: %v", err)
	}
}

func TestDecodeFontNormalizesWOFF2(t *testing.T) {
	parsed, err := gofont.Parse(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	compressed, err := parsed.SerializeWOFF2()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := resource.DecodeFontBytes(compressed)
	if err != nil {
		t.Fatalf("DecodeFontBytes(WOFF2) error = %v", err)
	}
	if bytes.Equal(decoded, compressed) {
		t.Fatal("WOFF2 bytes were not normalized")
	}
	if _, err := opentype.Parse(decoded); err != nil {
		t.Fatalf("normalized WOFF2 is not renderable: %v", err)
	}
}

func TestDecodeFontRejectsCompressedExpansionBeforeDecode(t *testing.T) {
	data := make([]byte, 20)
	copy(data, "wOF2")
	binary.BigEndian.PutUint32(data[16:20], resource.DefaultMaxDecodedFontBytes+1)
	if _, err := resource.DecodeFontBytes(data); !errors.Is(err, resource.ErrResourceTooLarge) {
		t.Fatalf("DecodeFontBytes() error = %v, want ErrResourceTooLarge", err)
	}
}

func TestDecodeFontRejectsMalformedBytes(t *testing.T) {
	if _, err := resource.DecodeFontBytes([]byte("not a font")); err == nil {
		t.Fatal("DecodeFontBytes() accepted malformed bytes")
	}
}

func testWOFF(t *testing.T, sfnt []byte) []byte {
	t.Helper()
	if len(sfnt) < 12 {
		t.Fatal("test SFNT is truncated")
	}
	tableCount := int(binary.BigEndian.Uint16(sfnt[4:6]))
	type table struct {
		tag, checksum uint32
		original      []byte
		stored        []byte
	}
	tables := make([]table, 0, tableCount)
	for index := 0; index < tableCount; index++ {
		start := 12 + index*16
		if start+16 > len(sfnt) {
			t.Fatal("test SFNT directory is truncated")
		}
		offset := int(binary.BigEndian.Uint32(sfnt[start+8 : start+12]))
		length := int(binary.BigEndian.Uint32(sfnt[start+12 : start+16]))
		if offset < 0 || length < 0 || offset+length > len(sfnt) {
			t.Fatal("test SFNT table is out of range")
		}
		original := sfnt[offset : offset+length]
		var compressed bytes.Buffer
		writer := zlib.NewWriter(&compressed)
		if _, err := writer.Write(original); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		stored := compressed.Bytes()
		if len(stored) >= len(original) {
			stored = original
		}
		tables = append(tables, table{
			tag: binary.BigEndian.Uint32(sfnt[start : start+4]), checksum: binary.BigEndian.Uint32(sfnt[start+4 : start+8]),
			original: original, stored: append([]byte(nil), stored...),
		})
	}
	headerSize := 44 + tableCount*20
	totalSize := headerSize
	for _, current := range tables {
		totalSize += (len(current.stored) + 3) &^ 3
	}
	result := make([]byte, totalSize)
	copy(result[:4], "wOFF")
	copy(result[4:8], sfnt[:4])
	binary.BigEndian.PutUint32(result[8:12], uint32(totalSize))
	binary.BigEndian.PutUint16(result[12:14], uint16(tableCount))
	binary.BigEndian.PutUint32(result[16:20], uint32(len(sfnt)))
	offset := headerSize
	for index, current := range tables {
		entry := 44 + index*20
		binary.BigEndian.PutUint32(result[entry:entry+4], current.tag)
		binary.BigEndian.PutUint32(result[entry+4:entry+8], uint32(offset))
		binary.BigEndian.PutUint32(result[entry+8:entry+12], uint32(len(current.stored)))
		binary.BigEndian.PutUint32(result[entry+12:entry+16], uint32(len(current.original)))
		binary.BigEndian.PutUint32(result[entry+16:entry+20], current.checksum)
		copy(result[offset:], current.stored)
		offset += (len(current.stored) + 3) &^ 3
	}
	return result
}
