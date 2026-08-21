package webapi

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

func NormalizeUTF8Label(label string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "unicode-1-1-utf-8", "unicode11utf8", "unicode20utf8", "utf-8", "utf8", "x-unicode20utf8":
		return "utf-8", true
	default:
		return "", false
	}
}

func EncodeUTF8(text string) []byte {
	return []byte(strings.ToValidUTF8(text, "\uFFFD"))
}

func DecodeUTF8(bytes []byte, fatal bool) (string, error) {
	if fatal && !utf8.Valid(bytes) {
		return "", fmt.Errorf("invalid UTF-8 input")
	}
	return strings.ToValidUTF8(string(bytes), "\uFFFD"), nil
}
