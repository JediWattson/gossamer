//go:build ignore

// Command generate_vertical_orientation converts the pinned Unicode data used
// by vertical text layout into compact, deterministic Go range tables.
//
// Usage:
//
//	go run generate_vertical_orientation.go \
//	  -input VerticalOrientation.txt \
//	  -grapheme-break GraphemeBreakProperty.txt \
//	  -emoji-data emoji-data.txt \
//	  -derived-core DerivedCoreProperties.txt \
//	  -output vertical_orientation_data.go
package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"flag"
	"fmt"
	"go/format"
	"os"
	"sort"
	"strconv"
	"strings"
)

const (
	wantVersion = "17.0.0"

	verticalSHA256 = "dcef09c3fb24d356b042569c328ec341efc5b53447700d799f2fb4834c3cd3cd"
	graphemeSHA256 = "d6b51d1d2ae5c33b451b7ed994b48f1f4dc62b2272a5831e7fd418514a6bae89"
	emojiSHA256    = "2cb2bb9455cda83e8481541ecf5b6dfda66a3bb89efa3fa7c5297eccf607b72b"
	derivedSHA256  = "24c7fed1195c482faaefd5c1e7eb821c5ee1fb6de07ecdbaa64b56a99da22c08"

	verticalURL = "https://www.unicode.org/Public/17.0.0/ucd/VerticalOrientation.txt"
	graphemeURL = "https://www.unicode.org/Public/17.0.0/ucd/auxiliary/GraphemeBreakProperty.txt"
	emojiURL    = "https://www.unicode.org/Public/17.0.0/ucd/emoji/emoji-data.txt"
	derivedURL  = "https://www.unicode.org/Public/17.0.0/ucd/DerivedCoreProperties.txt"
)

type unicodeRange struct {
	first uint32
	last  uint32
	value string
}

func main() {
	verticalPath := flag.String("input", "VerticalOrientation.txt", "path to Unicode VerticalOrientation.txt")
	graphemePath := flag.String("grapheme-break", "GraphemeBreakProperty.txt", "path to Unicode GraphemeBreakProperty.txt")
	emojiPath := flag.String("emoji-data", "emoji-data.txt", "path to Unicode emoji-data.txt")
	derivedPath := flag.String("derived-core", "DerivedCoreProperties.txt", "path to Unicode DerivedCoreProperties.txt")
	outputPath := flag.String("output", "vertical_orientation_data.go", "generated Go output")
	flag.Parse()

	vertical := readVerified(*verticalPath, verticalSHA256, "VerticalOrientation-"+wantVersion+".txt")
	grapheme := readVerified(*graphemePath, graphemeSHA256, "GraphemeBreakProperty-"+wantVersion+".txt")
	emoji := readVerified(*emojiPath, emojiSHA256, "Version: 17.0")
	derived := readVerified(*derivedPath, derivedSHA256, "DerivedCoreProperties-"+wantVersion+".txt")

	verticalRanges := mustParse(*verticalPath, vertical, 1, setOf("R", "U", "Tr", "Tu"))
	verticalRanges = filterRanges(verticalRanges, func(item unicodeRange) bool { return item.value != "R" })
	graphemeRanges := mustParse(*graphemePath, grapheme, 1, setOf(
		"CR", "Control", "Extend", "L", "LF", "LV", "LVT", "Prepend",
		"Regional_Indicator", "SpacingMark", "T", "V", "ZWJ",
	))
	emojiRanges := mustParse(*emojiPath, emoji, 1, setOf("Extended_Pictographic"))
	indicRanges := mustParse(*derivedPath, derived, 2, setOf("Consonant", "Extend", "Linker"))

	generated := generate(verticalRanges, graphemeRanges, emojiRanges, indicRanges)
	formatted, err := format.Source(generated)
	if err != nil {
		fatalf("format generated source: %v\n%s", err, generated)
	}
	if err := os.WriteFile(*outputPath, formatted, 0o644); err != nil {
		fatalf("write %s: %v", *outputPath, err)
	}
}

func readVerified(path, wantSum, marker string) []byte {
	source, err := os.ReadFile(path)
	if err != nil {
		fatalf("read %s: %v", path, err)
	}
	sum := fmt.Sprintf("%x", sha256.Sum256(source))
	if sum != wantSum {
		fatalf("%s SHA-256 = %s, want %s", path, sum, wantSum)
	}
	if !bytes.Contains(source, []byte(marker)) {
		fatalf("%s does not contain version marker %q", path, marker)
	}
	return source
}

func setOf(values ...string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

func mustParse(path string, source []byte, valueField int, accepted map[string]bool) []unicodeRange {
	ranges, err := parsePropertyRanges(source, valueField, accepted)
	if err != nil {
		fatalf("parse %s: %v", path, err)
	}
	return ranges
}

func parsePropertyRanges(source []byte, valueField int, accepted map[string]bool) ([]unicodeRange, error) {
	var ranges []unicodeRange
	scanner := bufio.NewScanner(bytes.NewReader(source))
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(strings.SplitN(scanner.Text(), "#", 2)[0])
		if line == "" {
			continue
		}
		fields := strings.Split(line, ";")
		if len(fields) <= valueField {
			continue
		}
		value := strings.TrimSpace(fields[valueField])
		if !accepted[value] {
			continue
		}
		bounds := strings.Split(strings.TrimSpace(fields[0]), "..")
		first, err := strconv.ParseUint(bounds[0], 16, 32)
		if err != nil {
			return nil, fmt.Errorf("line %d: first code point: %w", lineNumber, err)
		}
		last := first
		if len(bounds) == 2 {
			last, err = strconv.ParseUint(bounds[1], 16, 32)
			if err != nil {
				return nil, fmt.Errorf("line %d: last code point: %w", lineNumber, err)
			}
		} else if len(bounds) != 1 {
			return nil, fmt.Errorf("line %d: malformed range", lineNumber)
		}
		next := unicodeRange{first: uint32(first), last: uint32(last), value: value}
		if count := len(ranges); count != 0 && ranges[count-1].value == next.value && ranges[count-1].last+1 == next.first {
			ranges[count-1].last = next.last
			continue
		}
		ranges = append(ranges, next)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	sort.Slice(ranges, func(left, right int) bool {
		if ranges[left].first != ranges[right].first {
			return ranges[left].first < ranges[right].first
		}
		return ranges[left].last < ranges[right].last
	})
	for index := 1; index < len(ranges); index++ {
		if ranges[index-1].last >= ranges[index].first {
			return nil, fmt.Errorf("overlapping ranges U+%04X..U+%04X and U+%04X..U+%04X", ranges[index-1].first, ranges[index-1].last, ranges[index].first, ranges[index].last)
		}
	}
	return ranges, nil
}

func filterRanges(source []unicodeRange, keep func(unicodeRange) bool) []unicodeRange {
	result := make([]unicodeRange, 0, len(source))
	for _, item := range source {
		if keep(item) {
			result = append(result, item)
		}
	}
	return result
}

func generate(vertical, grapheme, emoji, indic []unicodeRange) []byte {
	var output bytes.Buffer
	fmt.Fprintln(&output, "// Code generated by generate_vertical_orientation.go; DO NOT EDIT.")
	fmt.Fprintf(&output, "// Unicode vertical text data %s.\n", wantVersion)
	for _, source := range [][2]string{
		{verticalURL, verticalSHA256}, {graphemeURL, graphemeSHA256},
		{emojiURL, emojiSHA256}, {derivedURL, derivedSHA256},
	} {
		fmt.Fprintf(&output, "// Source: %s\n// SHA-256: %s\n", source[0], source[1])
	}
	fmt.Fprintln(&output)
	fmt.Fprintln(&output, "package render")
	fmt.Fprintln(&output)
	fmt.Fprintln(&output, "const (")
	fmt.Fprintf(&output, "\tunicodeVerticalOrientationVersion = %q\n", wantVersion)
	fmt.Fprintf(&output, "\tunicodeVerticalOrientationSourceSHA256 = %q\n", verticalSHA256)
	fmt.Fprintf(&output, "\tunicodeGraphemeBreakSourceSHA256 = %q\n", graphemeSHA256)
	fmt.Fprintf(&output, "\tunicodeEmojiDataSourceSHA256 = %q\n", emojiSHA256)
	fmt.Fprintf(&output, "\tunicodeDerivedCoreSourceSHA256 = %q\n", derivedSHA256)
	fmt.Fprintln(&output, ")")
	fmt.Fprintln(&output)

	emitPropertyRanges(&output, "unicodeVerticalOrientationRanges", "unicodeVerticalOrientationRange", vertical, "orientation", map[string]string{
		"U": "unicodeVerticalUpright", "Tu": "unicodeVerticalTransformedUpright", "Tr": "unicodeVerticalTransformedRotated",
	})
	emitPropertyRanges(&output, "unicodeGraphemeBreakRanges", "unicodeGraphemeBreakRange", grapheme, "property", map[string]string{
		"CR": "graphemeBreakCR", "LF": "graphemeBreakLF", "Control": "graphemeBreakControl",
		"Extend": "graphemeBreakExtend", "ZWJ": "graphemeBreakZWJ", "Regional_Indicator": "graphemeBreakRegionalIndicator",
		"Prepend": "graphemeBreakPrepend", "SpacingMark": "graphemeBreakSpacingMark",
		"L": "graphemeBreakL", "V": "graphemeBreakV", "T": "graphemeBreakT", "LV": "graphemeBreakLV", "LVT": "graphemeBreakLVT",
	})
	emitCodePointRanges(&output, "unicodeExtendedPictographicRanges", emoji)
	emitPropertyRanges(&output, "unicodeIndicConjunctBreakRanges", "unicodeIndicConjunctBreakRange", indic, "property", map[string]string{
		"Consonant": "indicConjunctConsonant", "Extend": "indicConjunctExtend", "Linker": "indicConjunctLinker",
	})
	return output.Bytes()
}

func emitPropertyRanges(output *bytes.Buffer, variable, rangeType string, ranges []unicodeRange, field string, names map[string]string) {
	fmt.Fprintf(output, "var %s = [...]%s{\n", variable, rangeType)
	for _, item := range ranges {
		fmt.Fprintf(output, "\t{first: 0x%X, last: 0x%X, %s: %s},\n", item.first, item.last, field, names[item.value])
	}
	fmt.Fprintln(output, "}")
	fmt.Fprintln(output)
}

func emitCodePointRanges(output *bytes.Buffer, variable string, ranges []unicodeRange) {
	fmt.Fprintf(output, "var %s = [...]unicodeCodePointRange{\n", variable)
	for _, item := range ranges {
		fmt.Fprintf(output, "\t{first: 0x%X, last: 0x%X},\n", item.first, item.last)
	}
	fmt.Fprintln(output, "}")
	fmt.Fprintln(output)
}

func fatalf(format string, arguments ...any) {
	fmt.Fprintf(os.Stderr, "generate_vertical_orientation: "+format+"\n", arguments...)
	os.Exit(1)
}
