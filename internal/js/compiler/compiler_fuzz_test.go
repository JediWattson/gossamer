package compiler_test

import (
	"fmt"
	"testing"

	"github.com/JediWattson/gossamer/internal/js/compiler"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func FuzzCompileNeverPanicsAndEmitsVerifiedPrograms(f *testing.F) {
	f.Add("let answer = 40 + 2; answer;")
	f.Add("let i = 0; while (i < 4) { i++; } i;")
	f.Add("let value = {items: [1,,3]}; value.items;")
	f.Add("function make(x) { return function(y) { try { return x + y; } finally {} }; } make(40)(2);")
	f.Add(`function convert(value) { try { return +value == 42; } catch (error) { return error.name; } } convert("42");`)
	f.Fuzz(func(t *testing.T, source string) {
		for _, options := range []compiler.Options{{}, {AllowUnresolvedGlobals: true}} {
			image, err := compiler.CompileWithOptions(source, options)
			if err != nil {
				continue
			}
			if image.FunctionCount() == 0 {
				t.Fatal("successful compiler emitted no Functions")
			}
			for index := 0; index < image.FunctionCount(); index++ {
				function, ok := image.Function(uint32(index))
				if !ok {
					t.Fatalf("missing Function %d", index)
				}
				if err := browserruntime.VerifyBytecode(function.Code, len(function.Constants)); err != nil {
					t.Fatalf("Function %d failed verification: %v", index, err)
				}
			}
		}
	})
}

func FuzzCompiledClosuresPreserveExecutionAndStoreInvariants(f *testing.F) {
	f.Add(int16(40), int16(2))
	f.Add(int16(-7), int16(-3))
	f.Fuzz(func(t *testing.T, base, value int16) {
		source := fmt.Sprintf(`
function make(base) {
  return function(value) {
    try {
      if (value < 0) { throw value; }
      return base + value;
    } catch (problem) {
      return base - problem;
    } finally {
      let marker = 1;
      marker;
    }
  };
}
make(%d)(%d);
`, base, value)
		image, err := compiler.Compile(source)
		if err != nil {
			t.Fatal(err)
		}
		result := execute(t, 817, image)
		want := float64(base) + float64(value)
		if value < 0 {
			want = float64(base) - float64(value)
		}
		if result.Kind() != memory.ValueNumber || result.Number() != want {
			t.Fatalf("compiled result = %#v, want %v", result, want)
		}
	})
}
