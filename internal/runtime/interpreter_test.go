package runtime_test

import (
	"context"
	"errors"
	"testing"

	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func TestInterpreterExecutesCoreFrameAndStackVerbs(t *testing.T) {
	t.Parallel()

	realm, err := browserruntime.NewRealm(700, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer realm.Close()
	interpreter := browserruntime.NewInterpreter(browserruntime.InterpreterConfig{})
	var result memory.Value
	var missing memory.Value
	_, err = realm.EnqueueTask(func(task *browserruntime.TaskContext) error {
		function, err := task.NewBytecodeFunction(
			memory.NullValue(),
			memory.NullValue(),
			1,
			browserruntime.Assemble(
				browserruntime.Instruction{Op: browserruntime.OpConstant, A: 0},
				browserruntime.Instruction{Op: browserruntime.OpDup},
				browserruntime.Instruction{Op: browserruntime.OpPop},
				browserruntime.Instruction{Op: browserruntime.OpArgument, A: 0},
				browserruntime.Instruction{Op: browserruntime.OpPop},
				browserruntime.Instruction{Op: browserruntime.OpReturn},
			),
			[]memory.Value{memory.NumberValue(42)},
		)
		if err != nil {
			return err
		}
		result, err = interpreter.Execute(task, function, memory.NumberValue(99))
		if err != nil {
			return err
		}
		missingFunction, err := task.NewBytecodeFunction(
			memory.NullValue(),
			memory.NullValue(),
			0,
			browserruntime.Assemble(
				browserruntime.Instruction{Op: browserruntime.OpArgument, A: 4},
				browserruntime.Instruction{Op: browserruntime.OpReturn},
			),
			nil,
		)
		if err != nil {
			return err
		}
		missing, err = interpreter.Execute(task, missingFunction)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if result.Kind() != memory.ValueNumber || result.Number() != 42 {
		t.Fatalf("core result = %#v, want 42", result)
	}
	if missing.Kind() != memory.ValueUndefined {
		t.Fatalf("missing argument = %#v, want undefined", missing)
	}
}

func TestInterpreterRejectsMalformedAndUnsafePrograms(t *testing.T) {
	t.Parallel()

	if _, err := browserruntime.DecodeBytecode([]byte{1, 2}); !errors.Is(err, browserruntime.ErrInvalidBytecode) {
		t.Fatalf("short bytecode error = %v", err)
	}
	invalid := browserruntime.Assemble(browserruntime.Instruction{Op: browserruntime.Opcode(255)})
	if _, err := browserruntime.DecodeBytecode(invalid); !errors.Is(err, browserruntime.ErrInvalidBytecode) {
		t.Fatalf("unknown opcode error = %v", err)
	}

	realm, err := browserruntime.NewRealm(701, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer realm.Close()
	_, err = realm.EnqueueTask(func(task *browserruntime.TaskContext) error {
		tests := []struct {
			name string
			vm   *browserruntime.Interpreter
			code []byte
			want error
		}{
			{name: "constant bounds", vm: browserruntime.NewInterpreter(browserruntime.InterpreterConfig{}), code: browserruntime.Assemble(browserruntime.Instruction{Op: browserruntime.OpConstant, A: 1}, browserruntime.Instruction{Op: browserruntime.OpReturn}), want: browserruntime.ErrConstantBounds},
			{name: "stack underflow", vm: browserruntime.NewInterpreter(browserruntime.InterpreterConfig{}), code: browserruntime.Assemble(browserruntime.Instruction{Op: browserruntime.OpPop}, browserruntime.Instruction{Op: browserruntime.OpReturn}), want: browserruntime.ErrStackUnderflow},
			{name: "missing return", vm: browserruntime.NewInterpreter(browserruntime.InterpreterConfig{}), code: browserruntime.Assemble(browserruntime.Instruction{Op: browserruntime.OpUndefined}), want: browserruntime.ErrInvalidBytecode},
			{name: "instruction limit", vm: browserruntime.NewInterpreter(browserruntime.InterpreterConfig{MaxInstructions: 1}), code: browserruntime.Assemble(browserruntime.Instruction{Op: browserruntime.OpUndefined}, browserruntime.Instruction{Op: browserruntime.OpReturn}), want: browserruntime.ErrInstructionLimit},
		}
		for _, test := range tests {
			function, allocErr := task.NewBytecodeFunction(memory.NullValue(), memory.NullValue(), 0, test.code, nil)
			if allocErr != nil {
				return allocErr
			}
			if _, executeErr := test.vm.Execute(task, function); !errors.Is(executeErr, test.want) {
				t.Fatalf("%s error = %v, want %v", test.name, executeErr, test.want)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestFrameVisitsBorrowedRefsWithoutCreatingOwnership(t *testing.T) {
	t.Parallel()

	refs := []memory.Ref{
		{Region: 1, Slot: 1, Gen: 1},
		{Region: 2, Slot: 2, Gen: 2},
		{Region: 3, Slot: 3, Gen: 3},
		{Region: 4, Slot: 4, Gen: 4},
	}
	frame := &browserruntime.Frame{
		Function:    refs[0],
		Environment: memory.RefValue(refs[1]),
		This:        memory.UndefinedValue(),
		Arguments:   []memory.Value{memory.NumberValue(1), memory.RefValue(refs[2])},
		Stack:       []memory.Value{memory.RefValue(refs[3]), memory.BoolValue(true)},
	}
	var visited []memory.Ref
	if err := frame.VisitRefs(func(ref memory.Ref) error {
		visited = append(visited, ref)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(visited) != len(refs) {
		t.Fatalf("visited refs = %#v, want %#v", visited, refs)
	}
	for index := range refs {
		if visited[index] != refs[index] {
			t.Fatalf("visited[%d] = %s, want %s", index, visited[index], refs[index])
		}
	}
}
