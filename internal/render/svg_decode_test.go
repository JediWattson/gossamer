package render

import (
	"image/color"
	"testing"
)

func TestDecodeSVGImageRendersXMLDocument(t *testing.T) {
	t.Parallel()

	svgData := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE svg PUBLIC "-//W3C//DTD SVG 1.1//EN" "http://www.w3.org/Graphics/SVG/1.1/DTD/svg11.dtd">
<svg width="100%" height="100%" viewBox="0 0 20 20" xmlns="http://www.w3.org/2000/svg">
  <g style="fill:#00ff00">
    <rect x="0" y="0" width="20" height="20"/>
    <path d="M5,5 L15,5 L15,15 L5,15 Z" fill="#ff0000"/>
  </g>
</svg>`)

	if !LooksLikeSVG(svgData) {
		t.Fatal("LooksLikeSVG(valid) = false, want true")
	}
	img, err := DecodeSVGImage(svgData, 1024*1024)
	if err != nil {
		t.Fatalf("DecodeSVGImage() error = %v", err)
	}
	if bounds := img.Bounds(); bounds.Dx() != 20 || bounds.Dy() != 20 {
		t.Fatalf("SVG decoded bounds = %v, want 20x20", bounds)
	}
	corner := color.NRGBAModel.Convert(img.At(1, 1)).(color.NRGBA)
	center := color.NRGBAModel.Convert(img.At(10, 10)).(color.NRGBA)
	if corner != (color.NRGBA{G: 0xff, A: 0xff}) {
		t.Fatalf("corner pixel = %#v, want green", corner)
	}
	if center != (color.NRGBA{R: 0xff, A: 0xff}) {
		t.Fatalf("center pixel = %#v, want red", center)
	}
	if _, err := DecodeSVGImage(svgData, 10); err == nil {
		t.Fatal("DecodeSVGImage() succeeded despite 10-pixel budget")
	}
}
