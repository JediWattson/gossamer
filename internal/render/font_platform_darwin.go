//go:build darwin

package render

import (
	"os"
	"sync"

	"golang.org/x/image/font/opentype"
)

var (
	platformFontOnce sync.Once
	platformFontBook platformFonts
)

func loadPlatformFonts() platformFonts {
	platformFontOnce.Do(func() {
		platformFontBook = platformFonts{
			systemRegular: parsePlatformFont("/System/Library/Fonts/SFNS.ttf"),
			systemItalic:  parsePlatformFont("/System/Library/Fonts/SFNSItalic.ttf"),
			systemSymbols: parsePlatformFont("/System/Library/Fonts/Apple Symbols.ttf"),
		}
	})
	return platformFontBook
}

func parsePlatformFont(path string) *opentype.Font {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	parsed, err := opentype.Parse(data)
	if err != nil {
		return nil
	}
	return parsed
}
