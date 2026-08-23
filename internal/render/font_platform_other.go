//go:build !darwin

package render

func loadPlatformFonts() platformFonts { return platformFonts{} }
