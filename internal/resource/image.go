package resource

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/webp"
)

const DefaultMaxImagePixels int64 = 16_000_000

// ErrImageTooLarge marks an image whose decoded dimensions exceed the pixel
// budget, independent of its compressed response size.
var ErrImageTooLarge = errors.New("decoded image exceeds pixel limit")

// DecodedImage contains raster pixels and the decoder format selected by
// content sniffing (for example png, jpeg, gif, or webp).
type DecodedImage struct {
	Image  image.Image
	Format string
}

// DecodeImage decodes a fetched bitmap using the default pixel budget.
func DecodeImage(asset *Asset) (*DecodedImage, error) {
	return DecodeImageWithLimit(asset, DefaultMaxImagePixels)
}

// DecodeImageWithLimit checks dimensions before allocating the full decoded
// bitmap. A non-positive limit selects DefaultMaxImagePixels.
func DecodeImageWithLimit(asset *Asset, maxPixels int64) (*DecodedImage, error) {
	if asset == nil {
		return nil, fmt.Errorf("resource: nil image asset")
	}
	if maxPixels <= 0 {
		maxPixels = DefaultMaxImagePixels
	}
	configuration, format, err := image.DecodeConfig(bytes.NewReader(asset.data))
	if err != nil {
		return nil, fmt.Errorf("decode image configuration: %w", err)
	}
	if configuration.Width <= 0 || configuration.Height <= 0 ||
		int64(configuration.Width) > maxPixels/int64(configuration.Height) {
		return nil, fmt.Errorf(
			"%w: %dx%d is larger than %d pixels",
			ErrImageTooLarge,
			configuration.Width,
			configuration.Height,
			maxPixels,
		)
	}

	decoded, decodedFormat, err := image.Decode(bytes.NewReader(asset.data))
	if err != nil {
		return nil, fmt.Errorf("decode %s image: %w", format, err)
	}
	return &DecodedImage{Image: decoded, Format: decodedFormat}, nil
}
