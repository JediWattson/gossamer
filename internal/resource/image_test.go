package resource

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestDecodeImage(t *testing.T) {
	t.Parallel()

	source := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	source.SetNRGBA(0, 0, color.NRGBA{R: 0xff, A: 0xff})
	source.SetNRGBA(1, 0, color.NRGBA{B: 0xff, A: 0xff})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatalf("png.Encode() error = %v", err)
	}

	decoded, err := DecodeImage(&Asset{data: encoded.Bytes()})
	if err != nil {
		t.Fatalf("DecodeImage() error = %v", err)
	}
	if decoded.Format != "png" {
		t.Errorf("Format = %q, want png", decoded.Format)
	}
	if got, want := decoded.Image.Bounds(), image.Rect(0, 0, 2, 1); got != want {
		t.Errorf("Bounds = %v, want %v", got, want)
	}
}

func TestDecodeImageEnforcesPixelBudget(t *testing.T) {
	t.Parallel()

	source := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatalf("png.Encode() error = %v", err)
	}

	decoded, err := DecodeImageWithLimit(&Asset{data: encoded.Bytes()}, 3)
	if decoded != nil {
		t.Errorf("DecodeImageWithLimit() image = %#v, want nil", decoded)
	}
	if !errors.Is(err, ErrImageTooLarge) {
		t.Errorf("DecodeImageWithLimit() error = %v, want ErrImageTooLarge", err)
	}
}

func TestDecodeImageRejectsInvalidData(t *testing.T) {
	t.Parallel()

	if _, err := DecodeImage(nil); err == nil {
		t.Error("DecodeImage(nil) error = nil")
	}
	if _, err := DecodeImage(&Asset{data: []byte("not an image")}); err == nil {
		t.Error("DecodeImage(invalid) error = nil")
	}
}
