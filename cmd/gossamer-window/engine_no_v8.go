//go:build !v8 && cgo && darwin && arm64

package main

import (
	"fmt"

	"github.com/JediWattson/gossamer/internal/browser"
)

func newStockV8Engine(string) (browser.Engine, error) {
	return nil, fmt.Errorf("stock V8 is unavailable in this build; use --engine=strand or rebuild with -tags=v8")
}
