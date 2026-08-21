//go:build v8 && cgo && darwin && arm64

package engineparity

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/v8engine"
)

func TestStockV8AsyncCheckpointParity(t *testing.T) {
	engine, err := v8engine.New(v8engine.Config{})
	if err != nil {
		t.Fatal(err)
	}
	runAsyncCheckpointParity(t, engine)
}

func TestStockV8AsyncFunctionParity(t *testing.T) {
	engine, err := v8engine.New(v8engine.Config{})
	if err != nil {
		t.Fatal(err)
	}
	runAsyncFunctionParity(t, engine)
}
