//go:build !darwin || !cgo

package window

import (
	"context"
	"fmt"
	"image"
)

type unsupportedBackend struct{}

func NewNativeBackend() Backend { return &unsupportedBackend{} }

func (*unsupportedBackend) Open(Config) error {
	return fmt.Errorf("window: native backend requires macOS with cgo")
}
func (*unsupportedBackend) NextEvent(context.Context) (Event, error) {
	return Event{}, fmt.Errorf("window: native backend is unavailable")
}
func (*unsupportedBackend) Present(*image.RGBA) error {
	return fmt.Errorf("window: native backend is unavailable")
}
func (*unsupportedBackend) Close() error { return nil }
