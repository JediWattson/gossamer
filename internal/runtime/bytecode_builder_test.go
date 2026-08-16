package runtime_test

import (
	"errors"
	"strings"
	"testing"

	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func TestBytecodeBuilderPatchesLabelsInternsConstantsAndTracksLocations(t *testing.T) {
	t.Parallel()

	builder := browserruntime.NewBytecodeBuilder()
	zero, err := builder.AddConstant(memory.NumberValue(0))
	if err != nil {
		t.Fatal(err)
	}
	again, err := builder.AddConstant(memory.NumberValue(0))
	if err != nil {
		t.Fatal(err)
	}
	if zero != again {
		t.Fatalf("duplicate constant indexes = %d and %d", zero, again)
	}
	one, err := builder.AddConstant(memory.NumberValue(1))
	if err != nil {
		t.Fatal(err)
	}
	otherwise := builder.NewLabel()
	end := builder.NewLabel()
	if _, err := builder.EmitAt(browserruntime.Instruction{Op: browserruntime.OpConstant, A: zero}, browserruntime.SourceSpan{Start: 4, End: 5}); err != nil {
		t.Fatal(err)
	}
	if _, err := builder.EmitJump(browserruntime.OpJumpIfFalse, otherwise, browserruntime.SourceSpan{Start: 6, End: 8}); err != nil {
		t.Fatal(err)
	}
	if _, err := builder.Emit(browserruntime.Instruction{Op: browserruntime.OpConstant, A: one}); err != nil {
		t.Fatal(err)
	}
	if _, err := builder.EmitJump(browserruntime.OpJump, end, browserruntime.SourceSpan{}); err != nil {
		t.Fatal(err)
	}
	if err := builder.Mark(otherwise); err != nil {
		t.Fatal(err)
	}
	if _, err := builder.Emit(browserruntime.Instruction{Op: browserruntime.OpConstant, A: zero}); err != nil {
		t.Fatal(err)
	}
	if err := builder.Mark(end); err != nil {
		t.Fatal(err)
	}
	if _, err := builder.Emit(browserruntime.Instruction{Op: browserruntime.OpReturn}); err != nil {
		t.Fatal(err)
	}
	chunk, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	instructions, err := browserruntime.DecodeBytecode(chunk.Code)
	if err != nil {
		t.Fatal(err)
	}
	if instructions[1].A != 4 || instructions[3].A != 5 {
		t.Fatalf("patched jumps = %d and %d, want 4 and 5", instructions[1].A, instructions[3].A)
	}
	if len(chunk.Constants) != 2 || len(chunk.Locations) != 6 {
		t.Fatalf("chunk sizes = constants %d, locations %d", len(chunk.Constants), len(chunk.Locations))
	}
	if chunk.Locations[0] != (browserruntime.SourceSpan{Start: 4, End: 5}) {
		t.Fatalf("first location = %#v", chunk.Locations[0])
	}

	disassembly, err := browserruntime.Disassemble(chunk.Code)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"0000  Constant", "0001  JumpIfFalse", "0005  Return"} {
		if !strings.Contains(disassembly, fragment) {
			t.Fatalf("disassembly %q does not contain %q", disassembly, fragment)
		}
	}
}

func TestBytecodeBuilderRejectsInvalidLabelsAndPrograms(t *testing.T) {
	t.Parallel()

	builder := browserruntime.NewBytecodeBuilder()
	label := builder.NewLabel()
	if _, err := builder.EmitJump(browserruntime.OpAdd, label, browserruntime.SourceSpan{}); !errors.Is(err, browserruntime.ErrInvalidBytecode) {
		t.Fatalf("non-jump label reference error = %v", err)
	}
	if _, err := builder.EmitJump(browserruntime.OpJump, label, browserruntime.SourceSpan{}); err != nil {
		t.Fatal(err)
	}
	if _, err := builder.Build(); !errors.Is(err, browserruntime.ErrInvalidBytecode) {
		t.Fatalf("unmarked label error = %v", err)
	}
	if err := builder.Mark(label); err != nil {
		t.Fatal(err)
	}
	if err := builder.Mark(label); !errors.Is(err, browserruntime.ErrInvalidBytecode) {
		t.Fatalf("duplicate label error = %v", err)
	}
	if _, err := builder.EmitAt(browserruntime.Instruction{Op: browserruntime.OpReturn}, browserruntime.SourceSpan{Start: 9, End: 2}); !errors.Is(err, browserruntime.ErrInvalidBytecode) {
		t.Fatalf("invalid source span error = %v", err)
	}
}

func TestVerifyBytecodeRejectsStackAndControlFlowErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		code []byte
		want error
	}{
		{
			name: "underflow",
			code: browserruntime.Assemble(
				browserruntime.Instruction{Op: browserruntime.OpPop},
				browserruntime.Instruction{Op: browserruntime.OpReturn},
			),
			want: browserruntime.ErrStackUnderflow,
		},
		{
			name: "fallthrough",
			code: browserruntime.Assemble(browserruntime.Instruction{Op: browserruntime.OpUndefined}),
			want: browserruntime.ErrInvalidBytecode,
		},
		{
			name: "join mismatch",
			code: browserruntime.Assemble(
				browserruntime.Instruction{Op: browserruntime.OpTrue},
				browserruntime.Instruction{Op: browserruntime.OpJumpIfTrue, A: 4},
				browserruntime.Instruction{Op: browserruntime.OpUndefined},
				browserruntime.Instruction{Op: browserruntime.OpJump, A: 4},
				browserruntime.Instruction{Op: browserruntime.OpReturn},
			),
			want: browserruntime.ErrInvalidBytecode,
		},
	}
	for _, test := range tests {
		if err := browserruntime.VerifyBytecode(test.code, 0); !errors.Is(err, test.want) {
			t.Fatalf("%s error = %v, want %v", test.name, err, test.want)
		}
	}
}

func TestVerifyBytecodeAcceptsStructuredCatchFlow(t *testing.T) {
	t.Parallel()

	code := browserruntime.Assemble(
		browserruntime.Instruction{Op: browserruntime.OpEnterTry, A: 4, B: uint32(browserruntime.HandlerCatch)},
		browserruntime.Instruction{Op: browserruntime.OpConstant, A: 0},
		browserruntime.Instruction{Op: browserruntime.OpThrow},
		browserruntime.Instruction{Op: browserruntime.OpLeaveTry},
		browserruntime.Instruction{Op: browserruntime.OpEnterCatch},
		browserruntime.Instruction{Op: browserruntime.OpReturn},
	)
	if err := browserruntime.VerifyBytecode(code, 1); err != nil {
		t.Fatal(err)
	}
}

func FuzzVerifyBytecodeDoesNotPanic(f *testing.F) {
	f.Add([]byte{})
	f.Add(browserruntime.Assemble(browserruntime.Instruction{Op: browserruntime.OpUndefined}, browserruntime.Instruction{Op: browserruntime.OpReturn}))
	f.Fuzz(func(t *testing.T, code []byte) {
		_ = browserruntime.VerifyBytecode(code, len(code)%8)
		_, _ = browserruntime.Disassemble(code)
	})
}
