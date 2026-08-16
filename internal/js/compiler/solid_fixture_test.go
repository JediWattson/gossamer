package compiler_test

import (
	"os"
	"testing"

	"github.com/JediWattson/gossamer/internal/js/compiler"
)

func TestCompileProductionSolidFixture(t *testing.T) {
	source, err := os.ReadFile("../../engineparity/testdata/vite-solid/dist/solid-counter-1.9.14.production.js")
	if err != nil {
		t.Fatal(err)
	}
	image, err := compiler.CompileWithOptions(string(source), compiler.Options{AllowUnresolvedGlobals: true})
	if err != nil {
		t.Fatal(err)
	}
	if image.FunctionCount() < 40 {
		t.Fatalf("Solid function templates = %d, want at least 40", image.FunctionCount())
	}
}
