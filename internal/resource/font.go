package resource

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sort"

	gofont "github.com/venusliang/go-font"
	"golang.org/x/image/font/opentype"
)

// DefaultMaxDecodedFontBytes bounds the expanded SFNT retained for one web
// font. WOFF and WOFF2 headers advertise this size before decompression.
const DefaultMaxDecodedFontBytes = 32 << 20

// DecodeFont normalizes a fetched TrueType, OpenType, WOFF, or WOFF2 asset to
// an immutable SFNT byte slice accepted by the renderer.
func DecodeFont(asset *Asset) ([]byte, error) {
	if asset == nil {
		return nil, fmt.Errorf("resource: nil font asset")
	}
	return DecodeFontBytes(asset.data)
}

// DecodeFontBytes is DecodeFont for inline and test-owned bytes.
func DecodeFontBytes(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("resource: empty font")
	}
	if len(data) > DefaultMaxDecodedFontBytes {
		return nil, fmt.Errorf("%w: font is larger than %d bytes", ErrResourceTooLarge, DefaultMaxDecodedFontBytes)
	}
	if expanded, compressed := advertisedFontSize(data); compressed && (expanded <= 0 || expanded > DefaultMaxDecodedFontBytes) {
		return nil, fmt.Errorf("%w: expanded font is larger than %d bytes", ErrResourceTooLarge, DefaultMaxDecodedFontBytes)
	}

	// Avoid rewriting already-normalized SFNT files. opentype.Parse performs
	// the validation required by the rasterizer and retains the input bytes.
	if _, err := opentype.Parse(data); err == nil {
		return append([]byte(nil), data...), nil
	}

	var parsed gofont.TrueTypeFont
	var err error
	switch string(data[:min(4, len(data))]) {
	case "wOFF":
		parsed, err = gofont.ParseWOFF(data)
	case "wOF2":
		parsed, err = gofont.ParseWOFF2(data)
	default:
		parsed, err = gofont.Parse(data)
	}
	if err != nil {
		return nil, fmt.Errorf("resource: decode web font: %w", err)
	}
	result, err := parsed.Serialize()
	if err != nil {
		return nil, fmt.Errorf("resource: normalize web font: %w", err)
	}
	if len(result) == 0 || len(result) > DefaultMaxDecodedFontBytes {
		return nil, fmt.Errorf("%w: normalized font is larger than %d bytes", ErrResourceTooLarge, DefaultMaxDecodedFontBytes)
	}
	if err := sortSFNTDirectory(result); err != nil {
		return nil, err
	}
	if _, err := opentype.Parse(result); err != nil {
		return nil, fmt.Errorf("resource: validate normalized web font: %w", err)
	}
	return append([]byte(nil), result...), nil
}

func sortSFNTDirectory(data []byte) error {
	if len(data) < 12 {
		return fmt.Errorf("resource: normalized font header is truncated")
	}
	count := int(binary.BigEndian.Uint16(data[4:6]))
	if count < 0 || count > (len(data)-12)/16 {
		return fmt.Errorf("resource: normalized font directory is truncated")
	}
	entries := make([][]byte, count)
	for index := 0; index < count; index++ {
		start := 12 + index*16
		entries[index] = append([]byte(nil), data[start:start+16]...)
	}
	sort.Slice(entries, func(left, right int) bool {
		return bytes.Compare(entries[left][:4], entries[right][:4]) < 0
	})
	for index, entry := range entries {
		copy(data[12+index*16:], entry)
	}
	return nil
}

func advertisedFontSize(data []byte) (int, bool) {
	if len(data) < 20 || (!bytes.Equal(data[:4], []byte("wOFF")) && !bytes.Equal(data[:4], []byte("wOF2"))) {
		return 0, false
	}
	return int(binary.BigEndian.Uint32(data[16:20])), true
}
