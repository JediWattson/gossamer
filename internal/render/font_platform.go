package render

import "golang.org/x/image/font/opentype"

// platformFonts are optional host faces. Missing or unreadable platform fonts
// deliberately fall back to the bundled Go faces so headless rendering stays
// available on every supported platform.
type platformFonts struct {
	systemRegular *opentype.Font
	systemItalic  *opentype.Font
	systemSymbols *opentype.Font
}
