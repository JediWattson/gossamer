package render

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/math/fixed"
)

const maxEmojiBitmapCacheEntries = 1024

type emojiTableEntry struct {
	offset uint32
	length uint32
}

type sbixStrike struct {
	offset uint32
	ppem   uint16
}

type emojiCacheKey struct {
	glyph  uint16
	target int
}

type colorEmojiFont struct {
	file       *os.File
	reader     io.ReaderAt
	unitsPerEm uint16
	numGlyphs  int
	advances   []uint16
	sbixOffset uint32
	strikes    []sbixStrike
	sequences  map[string]uint16
	cmapRunes  map[rune]uint16

	mu    sync.Mutex
	cache map[emojiCacheKey]*image.NRGBA
}

func openColorEmojiFont(path string) *colorEmojiFont {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	parsed, err := parseColorEmojiFont(file)
	if err != nil {
		_ = file.Close()
		return nil
	}
	parsed.file = file
	return parsed
}

func parseColorEmojiFont(reader io.ReaderAt) (*colorEmojiFont, error) {
	if reader == nil {
		return nil, errors.New("emoji font: nil reader")
	}
	header := make([]byte, 16)
	if _, err := reader.ReadAt(header[:12], 0); err != nil {
		return nil, err
	}
	sfntOffset := uint32(0)
	if string(header[:4]) == "ttcf" {
		if _, err := reader.ReadAt(header[:16], 0); err != nil {
			return nil, err
		}
		if binary.BigEndian.Uint32(header[8:12]) == 0 {
			return nil, errors.New("emoji font: empty collection")
		}
		sfntOffset = binary.BigEndian.Uint32(header[12:16])
	}
	sfntHdr := make([]byte, 12)
	if _, err := reader.ReadAt(sfntHdr, int64(sfntOffset)); err != nil {
		return nil, err
	}
	numTables := int(binary.BigEndian.Uint16(sfntHdr[4:6]))
	dir := make([]byte, numTables*16)
	if _, err := reader.ReadAt(dir, int64(sfntOffset)+12); err != nil {
		return nil, err
	}
	tables := make(map[string]emojiTableEntry, numTables)
	for index := 0; index < numTables; index++ {
		base := index * 16
		tag := string(dir[base : base+4])
		tables[tag] = emojiTableEntry{
			offset: binary.BigEndian.Uint32(dir[base+8 : base+12]),
			length: binary.BigEndian.Uint32(dir[base+12 : base+16]),
		}
	}
	headTable, ok := tables["head"]
	if !ok || headTable.length < 54 {
		return nil, errors.New("emoji font: missing head")
	}
	headBuf := make([]byte, 54)
	if _, err := reader.ReadAt(headBuf, int64(headTable.offset)); err != nil {
		return nil, err
	}
	unitsPerEm := binary.BigEndian.Uint16(headBuf[18:20])
	if unitsPerEm == 0 {
		unitsPerEm = 800
	}

	hheaTable, ok := tables["hhea"]
	if !ok || hheaTable.length < 36 {
		return nil, errors.New("emoji font: missing hhea")
	}
	hheaBuf := make([]byte, 36)
	if _, err := reader.ReadAt(hheaBuf, int64(hheaTable.offset)); err != nil {
		return nil, err
	}
	numHMetrics := int(binary.BigEndian.Uint16(hheaBuf[34:36]))

	maxpTable, ok := tables["maxp"]
	if !ok || maxpTable.length < 6 {
		return nil, errors.New("emoji font: missing maxp")
	}
	maxpBuf := make([]byte, 6)
	if _, err := reader.ReadAt(maxpBuf, int64(maxpTable.offset)); err != nil {
		return nil, err
	}
	numGlyphs := int(binary.BigEndian.Uint16(maxpBuf[4:6]))

	hmtxTable, ok := tables["hmtx"]
	if !ok || numHMetrics == 0 || int(hmtxTable.length) < numHMetrics*4 {
		return nil, errors.New("emoji font: missing hmtx")
	}
	hmtxBuf := make([]byte, numHMetrics*4)
	if _, err := reader.ReadAt(hmtxBuf, int64(hmtxTable.offset)); err != nil {
		return nil, err
	}
	advances := make([]uint16, numHMetrics)
	for index := 0; index < numHMetrics; index++ {
		advances[index] = binary.BigEndian.Uint16(hmtxBuf[index*4 : index*4+2])
	}

	sbixTable, ok := tables["sbix"]
	if !ok || sbixTable.length < 8 {
		return nil, errors.New("emoji font: missing sbix")
	}
	sbixHdr := make([]byte, 8)
	if _, err := reader.ReadAt(sbixHdr, int64(sbixTable.offset)); err != nil {
		return nil, err
	}
	numStrikes := int(binary.BigEndian.Uint32(sbixHdr[4:8]))
	if numStrikes == 0 {
		return nil, errors.New("emoji font: no sbix strikes")
	}
	strikeOffBuf := make([]byte, numStrikes*4)
	if _, err := reader.ReadAt(strikeOffBuf, int64(sbixTable.offset)+8); err != nil {
		return nil, err
	}
	strikes := make([]sbixStrike, 0, numStrikes)
	var strikeHdr [4]byte
	for index := 0; index < numStrikes; index++ {
		off := binary.BigEndian.Uint32(strikeOffBuf[index*4 : index*4+4])
		if _, err := reader.ReadAt(strikeHdr[:], int64(sbixTable.offset)+int64(off)); err != nil {
			continue
		}
		ppem := binary.BigEndian.Uint16(strikeHdr[:2])
		if ppem > 0 {
			strikes = append(strikes, sbixStrike{offset: off, ppem: ppem})
		}
	}
	sort.Slice(strikes, func(i, j int) bool { return strikes[i].ppem < strikes[j].ppem })
	if len(strikes) == 0 {
		return nil, errors.New("emoji font: no usable sbix strikes")
	}

	cmapRunes := make(map[rune]uint16)
	if cmapTable, hasCmap := tables["cmap"]; hasCmap && cmapTable.length <= 2<<20 {
		cmapBuf := make([]byte, cmapTable.length)
		if _, err := reader.ReadAt(cmapBuf, int64(cmapTable.offset)); err == nil {
			parseEmojiCmapFormat12(cmapBuf, cmapRunes)
		}
	}

	sequences := make(map[string]uint16)
	if postTable, hasPost := tables["post"]; hasPost && postTable.length <= 4<<20 {
		postBuf := make([]byte, postTable.length)
		if _, err := reader.ReadAt(postBuf, int64(postTable.offset)); err == nil {
			parseEmojiPostSequences(postBuf, sequences)
		}
	}
	if len(cmapRunes) == 0 && len(sequences) == 0 {
		return nil, errors.New("emoji font: empty glyph index")
	}

	return &colorEmojiFont{
		reader:     reader,
		unitsPerEm: unitsPerEm,
		numGlyphs:  numGlyphs,
		advances:   advances,
		sbixOffset: sbixTable.offset,
		strikes:    strikes,
		sequences:  sequences,
		cmapRunes:  cmapRunes,
		cache:      make(map[emojiCacheKey]*image.NRGBA),
	}, nil
}

func parseEmojiCmapFormat12(data []byte, out map[rune]uint16) {
	if len(data) < 4 {
		return
	}
	numSubtables := int(binary.BigEndian.Uint16(data[2:4]))
	for index := 0; index < numSubtables; index++ {
		rec := 4 + index*8
		if rec+8 > len(data) {
			return
		}
		offset := int(binary.BigEndian.Uint32(data[rec+4 : rec+8]))
		if offset+16 > len(data) || binary.BigEndian.Uint16(data[offset:offset+2]) != 12 {
			continue
		}
		numGroups := int(binary.BigEndian.Uint32(data[offset+12 : offset+16]))
		for g := 0; g < numGroups; g++ {
			pos := offset + 16 + g*12
			if pos+12 > len(data) {
				break
			}
			startCode := binary.BigEndian.Uint32(data[pos : pos+4])
			endCode := binary.BigEndian.Uint32(data[pos+4 : pos+8])
			startGID := binary.BigEndian.Uint32(data[pos+8 : pos+12])
			for code := startCode; code <= endCode && code <= 0x10FFFF; code++ {
				gid := startGID + (code - startCode)
				if gid > 0 && gid <= 0xFFFF {
					out[rune(code)] = uint16(gid)
				}
			}
		}
		return
	}
}

func parseEmojiPostSequences(data []byte, out map[string]uint16) {
	if len(data) < 34 || binary.BigEndian.Uint32(data[0:4]) != 0x00020000 {
		return
	}
	numGlyphs := int(binary.BigEndian.Uint16(data[32:34]))
	idxStart := 34
	pos := idxStart + numGlyphs*2
	if pos > len(data) {
		return
	}
	var names []string
	for pos < len(data) {
		nameLen := int(data[pos])
		pos++
		if pos+nameLen > len(data) {
			break
		}
		names = append(names, string(data[pos:pos+nameLen]))
		pos += nameLen
	}
	for gid := 0; gid < numGlyphs; gid++ {
		idx := int(binary.BigEndian.Uint16(data[idxStart+gid*2 : idxStart+gid*2+2]))
		if idx < 258 || idx-258 >= len(names) {
			continue
		}
		indexAppleEmojiPostName(names[idx-258], uint16(gid), out)
	}
}

func indexAppleEmojiPostName(name string, gid uint16, out map[string]uint16) {
	if !strings.HasPrefix(name, "u") {
		return
	}
	parts := strings.Split(name, ".")
	var base [8]rune
	baseCount := 0
	appendHexCode := func(token string) bool {
		if !strings.HasPrefix(token, "u") || len(token) < 2 || baseCount >= len(base) {
			return false
		}
		value, err := strconv.ParseUint(token[1:], 16, 32)
		if err != nil || value == 0 || value > 0x10FFFF {
			return false
		}
		base[baseCount] = rune(value)
		baseCount++
		return true
	}
	for _, chunk := range strings.Split(parts[0], "_") {
		if !appendHexCode(chunk) {
			return
		}
	}
	var skins string
	for _, part := range parts[1:] {
		switch {
		case part == "L" || part == "R" || part == "66":
			return
		case part == "M" || part == "W" || part == "RA":
			continue
		case strings.HasPrefix(part, "u"):
			if !appendHexCode(part) {
				return
			}
		case isAllEmojiDigits(part):
			skins = part
		default:
			return
		}
	}
	if skins == "0" {
		neutralKey := formatEmojiKey(base[:baseCount], "")
		if _, exists := out[neutralKey]; !exists {
			out[neutralKey] = gid
		}
	}
	out[formatEmojiKey(base[:baseCount], skins)] = gid
}

func isAllEmojiDigits(s string) bool {
	if s == "" {
		return false
	}
	for index := 0; index < len(s); index++ {
		if s[index] < '0' || s[index] > '5' {
			return false
		}
	}
	return true
}

func formatEmojiKey(base []rune, skins string) string {
	var builder strings.Builder
	for index, r := range base {
		if index > 0 {
			builder.WriteByte('_')
		}
		builder.WriteString(strconv.FormatInt(int64(r), 16))
	}
	builder.WriteByte('|')
	builder.WriteString(skins)
	return builder.String()
}

func (font *colorEmojiFont) advanceForGlyph(gid uint16) uint16 {
	if font == nil || len(font.advances) == 0 {
		return 0
	}
	if int(gid) < len(font.advances) {
		return font.advances[gid]
	}
	return font.advances[len(font.advances)-1]
}

func (font *colorEmojiFont) lookupCluster(cluster string) (gid uint16, advanceUnits uint16, explicitEmoji bool, ok bool) {
	if font == nil || cluster == "" {
		return 0, 0, false, false
	}
	var base [12]rune
	baseCount := 0
	var skinBytes [4]byte
	skinCount := 0
	hasVS16 := false
	hasVS15 := false
	for _, r := range cluster {
		switch {
		case r == '\u200D':
			continue
		case r == '\uFE0F':
			hasVS16 = true
		case r == '\uFE0E':
			hasVS15 = true
		case r >= 0x1F3FB && r <= 0x1F3FF:
			if skinCount < len(skinBytes) {
				skinBytes[skinCount] = byte('1' + (r - 0x1F3FB))
				skinCount++
			}
		default:
			if baseCount < len(base) {
				base[baseCount] = r
				baseCount++
			}
		}
	}
	if baseCount == 0 {
		return 0, 0, false, false
	}
	skins := string(skinBytes[:skinCount])
	if matched, found := font.sequences[formatEmojiKey(base[:baseCount], skins)]; found {
		gid, ok = matched, true
	} else if skinCount > 0 {
		if matched, found := font.sequences[formatEmojiKey(base[:baseCount], "")]; found {
			gid, ok = matched, true
		}
	}
	if !ok && baseCount == 1 && skinCount == 0 {
		if matched, found := font.cmapRunes[base[0]]; found {
			gid, ok = matched, true
		}
	}
	if !ok {
		return 0, 0, false, false
	}
	if !hasVS15 {
		explicitEmoji = hasVS16 || baseCount >= 2 || skinCount > 0 || isDefaultEmojiPresentation(base[0])
	}
	return gid, font.advanceForGlyph(gid), explicitEmoji, true
}

func (font *colorEmojiFont) glyphAdvanceFixed(advanceUnits uint16, size float64) fixed.Int26_6 {
	if font == nil || font.unitsPerEm == 0 || advanceUnits == 0 {
		return fixed.Int26_6(math.Round(size * 64))
	}
	return fixed.Int26_6(math.Round(size * float64(advanceUnits) / float64(font.unitsPerEm) * 64))
}

func (font *colorEmojiFont) targetSize(size float64) (box int, ascentPx int) {
	if size <= 0 {
		size = 16
	}
	box = max(1, int(math.Round(size)))
	ascentPx = int(math.Round(size * 0.82))
	return box, ascentPx
}

func (font *colorEmojiFont) glyphBitmap(gid uint16, size float64) (*image.NRGBA, int, bool) {
	if font == nil || font.reader == nil || gid == 0 || int(gid) >= font.numGlyphs {
		return nil, 0, false
	}
	target, ascentPx := font.targetSize(size)
	key := emojiCacheKey{glyph: gid, target: target}

	font.mu.Lock()
	if cached := font.cache[key]; cached != nil {
		font.mu.Unlock()
		return cached, ascentPx, true
	}
	font.mu.Unlock()

	decoded, err := font.decodeStrikeGlyph(gid, target, 0)
	if err != nil || decoded == nil {
		return nil, 0, false
	}
	scaled := scaleEmojiNRGBA(decoded, target)

	font.mu.Lock()
	if len(font.cache) >= maxEmojiBitmapCacheEntries {
		clear(font.cache)
	}
	font.cache[key] = scaled
	font.mu.Unlock()
	return scaled, ascentPx, true
}

func (font *colorEmojiFont) selectStrike(target int) sbixStrike {
	for _, strike := range font.strikes {
		if int(strike.ppem) >= target {
			return strike
		}
	}
	return font.strikes[len(font.strikes)-1]
}

func (font *colorEmojiFont) decodeStrikeGlyph(gid uint16, target int, depth int) (image.Image, error) {
	if depth > 2 || int(gid) >= font.numGlyphs {
		return nil, errors.New("emoji font: invalid glyph")
	}
	strike := font.selectStrike(target)
	strikeBase := int64(font.sbixOffset) + int64(strike.offset)
	var offBuf [8]byte
	if _, err := font.reader.ReadAt(offBuf[:], strikeBase+4+int64(gid)*4); err != nil {
		return nil, err
	}
	start := binary.BigEndian.Uint32(offBuf[0:4])
	end := binary.BigEndian.Uint32(offBuf[4:8])
	if end <= start+8 || end-start > 4<<20 {
		return nil, errors.New("emoji font: empty sbix glyph")
	}
	payload := make([]byte, end-start)
	if _, err := font.reader.ReadAt(payload, strikeBase+int64(start)); err != nil {
		return nil, err
	}
	graphicType := string(payload[4:8])
	data := payload[8:]
	switch graphicType {
	case "png ":
		return png.Decode(bytes.NewReader(data))
	case "flip":
		if len(data) < 2 {
			return nil, errors.New("emoji font: invalid flip")
		}
		targetGID := binary.BigEndian.Uint16(data[:2])
		src, err := font.decodeStrikeGlyph(targetGID, target, depth+1)
		if err != nil {
			return nil, err
		}
		return flipImageHorizontal(src), nil
	default:
		return nil, errors.New("emoji font: unsupported sbix format")
	}
}

func flipImageHorizontal(src image.Image) *image.NRGBA {
	bounds := src.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dst.Set(w-1-x, y, color.NRGBAModel.Convert(src.At(bounds.Min.X+x, bounds.Min.Y+y)))
		}
	}
	return dst
}

func scaleEmojiNRGBA(src image.Image, target int) *image.NRGBA {
	srcBounds := src.Bounds()
	dstBounds := image.Rect(0, 0, target, target)
	dst := image.NewNRGBA(dstBounds)
	if srcBounds.Dx() == target && srcBounds.Dy() == target {
		draw.Draw(dst, dstBounds, src, srcBounds.Min, draw.Src)
		return dst
	}
	scaler := xdraw.ApproxBiLinear
	if target < srcBounds.Dx() || target < srcBounds.Dy() {
		scaler = xdraw.CatmullRom
	}
	scaler.Scale(dst, dstBounds, src, srcBounds, draw.Src, nil)
	return dst
}

func mayContainClusterOrEmoji(text string) bool {
	for index := 0; index < len(text); index++ {
		if text[index] >= 0x80 {
			return true
		}
	}
	return false
}

func isDefaultEmojiPresentation(r rune) bool {
	if r >= 0x1F000 {
		return r == 0x1F004 || r == 0x1F0CF || r == 0x1F18E ||
			(r >= 0x1F191 && r <= 0x1F19A) ||
			(r >= 0x1F1E6 && r <= 0x1F1FF) ||
			r == 0x1F201 || r == 0x1F21A || r == 0x1F22F ||
			(r >= 0x1F232 && r <= 0x1F236) ||
			(r >= 0x1F238 && r <= 0x1F23A) ||
			(r >= 0x1F250 && r <= 0x1F251) ||
			r >= 0x1F300
	}
	switch {
	case r == 0x231A || r == 0x231B || r == 0x23E9 || r == 0x23EA || r == 0x23EB || r == 0x23EC ||
		r == 0x23F0 || r == 0x23F3 || r == 0x25FD || r == 0x25FE || r == 0x2614 || r == 0x2615 ||
		r == 0x267F || r == 0x2693 || r == 0x26A1 || r == 0x26AA || r == 0x26AB ||
		r == 0x26BD || r == 0x26BE || r == 0x26C4 || r == 0x26C5 || r == 0x26CE || r == 0x26D4 ||
		r == 0x26EA || r == 0x26F2 || r == 0x26F3 || r == 0x26F5 || r == 0x26FA || r == 0x26FD ||
		r == 0x2705 || r == 0x270A || r == 0x270B || r == 0x2728 || r == 0x274C || r == 0x274E ||
		r == 0x2753 || r == 0x2754 || r == 0x2755 || r == 0x2757 ||
		r == 0x2795 || r == 0x2796 || r == 0x2797 || r == 0x27B0 || r == 0x27BF ||
		r == 0x2B1B || r == 0x2B1C || r == 0x2B50 || r == 0x2B55:
		return true
	case r >= 0x2648 && r <= 0x2653:
		return true
	default:
		return false
	}
}
