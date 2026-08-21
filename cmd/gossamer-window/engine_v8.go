//go:build v8 && cgo && darwin && arm64

package main

import (
	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/v8engine"
)

func newStockV8Engine(icuData string) (browser.Engine, error) {
	return v8engine.New(v8engine.Config{ICUDataPath: icuData})
}
