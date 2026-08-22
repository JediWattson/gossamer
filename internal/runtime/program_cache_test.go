package runtime

import (
	"errors"
	"testing"
)

func TestInterpreterCachesDecodedVerifiedBytecodeByContent(t *testing.T) {
	interpreter := NewInterpreter(InterpreterConfig{})
	code := Assemble(
		Instruction{Op: OpUndefined},
		Instruction{Op: OpReturn},
	)
	first, err := interpreter.decodeProgram(code, 0)
	if err != nil {
		t.Fatal(err)
	}
	second, err := interpreter.decodeProgram(append([]byte(nil), code...), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) == 0 || &first[0] != &second[0] {
		t.Fatal("equivalent Function bodies did not share decoded instructions")
	}

	constantCode := Assemble(
		Instruction{Op: OpConstant, A: 0},
		Instruction{Op: OpReturn},
	)
	if _, err := interpreter.decodeProgram(constantCode, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := interpreter.decodeProgram(constantCode, 0); !errors.Is(err, ErrConstantBounds) {
		t.Fatalf("constant-count-specific verification = %v", err)
	}
}

func TestInterpreterBoundsDecodedBytecodeCache(t *testing.T) {
	interpreter := NewInterpreter(InterpreterConfig{})
	for index := 0; index < defaultBytecodeCacheLimit+1; index++ {
		code := Assemble(
			Instruction{Op: OpArgument, A: uint32(index)},
			Instruction{Op: OpReturn},
		)
		if _, err := interpreter.decodeProgram(code, 0); err != nil {
			t.Fatal(err)
		}
	}
	if len(interpreter.programClock) != defaultBytecodeCacheLimit {
		t.Fatalf("program clock entries = %d", len(interpreter.programClock))
	}
	entries := 0
	for _, bucket := range interpreter.programs {
		entries += len(bucket)
	}
	if entries != defaultBytecodeCacheLimit {
		t.Fatalf("program cache entries = %d", entries)
	}
}
