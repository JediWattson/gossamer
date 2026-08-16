package compiler_test

import (
	"os"
	"testing"

	"github.com/JediWattson/gossamer/internal/js/compiler"
)

func TestCompileProductionReactFixture(t *testing.T) {
	source, err := os.ReadFile("../../v8engine/testdata/react-19.2.7.production.js")
	if err != nil {
		t.Fatal(err)
	}
	image, err := compiler.CompileWithOptions(string(source), compiler.Options{AllowUnresolvedGlobals: true})
	if err != nil {
		t.Fatal(err)
	}
	if image.FunctionCount() < 500 {
		t.Fatalf("React function templates = %d, want at least 500", image.FunctionCount())
	}
}
