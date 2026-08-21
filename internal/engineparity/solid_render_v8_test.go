//go:build v8 && cgo && darwin && arm64

package engineparity

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/v8engine"
)

func TestStockV8RunsProductionSolidCounterLifecycle(t *testing.T) {
	engine, err := v8engine.New(v8engine.Config{})
	if err != nil {
		t.Fatal(err)
	}
	runProductionSolidCounterParity(t, engine)
}

func TestStockV8RunsProductionSolidKeyedForAndShow(t *testing.T) {
	engine, err := v8engine.New(v8engine.Config{})
	if err != nil {
		t.Fatal(err)
	}
	runProductionSolidKeyedForAndShow(t, engine)
}
