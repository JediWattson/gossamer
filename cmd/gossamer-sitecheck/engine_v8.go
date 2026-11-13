//go:build v8 && cgo && darwin && arm64

package main

import (
	"fmt"
	"strings"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/nativeengine"
	"github.com/JediWattson/gossamer/internal/v8engine"
)

func selectEngine(name, icuData string) (browser.Engine, error) {
	return selectEngineWithConsole(name, icuData, nil)
}

func selectEngineWithConsole(name, icuData string, consoleSink func(nativeengine.ConsoleMessage)) (browser.Engine, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "strand":
		return nativeengine.New(nativeengine.Config{ConsoleSink: consoleSink}), nil
	case "v8":
		return v8engine.New(v8engine.Config{ICUDataPath: icuData})
	default:
		return nil, fmt.Errorf("unknown engine %q (want strand or v8)", name)
	}
}
