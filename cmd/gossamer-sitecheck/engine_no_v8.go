//go:build !v8

package main

import (
	"fmt"
	"strings"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/nativeengine"
)

func selectEngine(name, icuData string) (browser.Engine, error) {
	return selectEngineWithConsole(name, icuData, nil)
}

func selectEngineWithConsole(name, _ string, consoleSink func(nativeengine.ConsoleMessage)) (browser.Engine, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "strand":
		return nativeengine.New(nativeengine.Config{ConsoleSink: consoleSink}), nil
	case "v8":
		return nil, fmt.Errorf("stock V8 is unavailable in this build; rebuild with -tags=v8")
	default:
		return nil, fmt.Errorf("unknown engine %q (want strand or v8)", name)
	}
}
