package compiler_test

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/js/compiler"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
)

func FuzzCompileNeverPanicsAndEmitsVerifiedPrograms(f *testing.F) {
	f.Add("let answer = 40 + 2; answer;")
	f.Add("let i = 0; while (i < 4) { i++; } i;")
	f.Add("let value = {items: [1,,3]}; value.items;")
	f.Fuzz(func(t *testing.T, source string) {
		image, err := compiler.Compile(source)
		if err != nil {
			return
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
	})
}
