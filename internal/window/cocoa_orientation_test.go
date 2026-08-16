//go:build darwin && cgo

package window

import "testing"

func TestCocoaPresentationKeepsRGBAFirstRowAtVisualTop(t *testing.T) {
	if cocoaPresentationIsTopLeft() {
		return
	}
	t.Fatal("Cocoa presentation vertically inverted the top-left RGBA frame")
}
