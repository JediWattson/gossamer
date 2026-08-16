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

func TestInterpreterObjectAndArrayVerbsUseRegionStore(t *testing.T) {
	t.Parallel()

	realm, err := browserruntime.NewRealm(702, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer realm.Close()
	interpreter := browserruntime.NewInterpreter(browserruntime.InterpreterConfig{})
	var sourceObject memory.Ref
	var sourceArray memory.Ref
	var promotedArray memory.Ref
	_, err = realm.EnqueueTask(func(task *browserruntime.TaskContext) error {
		name, err := task.NewString("answer")
		if err != nil {
			return err
		}
		objectFunction, err := task.NewBytecodeFunction(
			memory.NullValue(), memory.NullValue(), 0,
			browserruntime.Assemble(
				browserruntime.Instruction{Op: browserruntime.OpNewObject},
				browserruntime.Instruction{Op: browserruntime.OpDup},
				browserruntime.Instruction{Op: browserruntime.OpConstant, A: 0},
				browserruntime.Instruction{Op: browserruntime.OpConstant, A: 1},
				browserruntime.Instruction{Op: browserruntime.OpSetOwnProperty},
				browserruntime.Instruction{Op: browserruntime.OpPop},
				browserruntime.Instruction{Op: browserruntime.OpDup},
				browserruntime.Instruction{Op: browserruntime.OpConstant, A: 0},
				browserruntime.Instruction{Op: browserruntime.OpGetOwnProperty},
				browserruntime.Instruction{Op: browserruntime.OpPop},
				browserruntime.Instruction{Op: browserruntime.OpDup},
				browserruntime.Instruction{Op: browserruntime.OpConstant, A: 0},
				browserruntime.Instruction{Op: browserruntime.OpDeleteOwnProperty},
				browserruntime.Instruction{Op: browserruntime.OpPop},
				browserruntime.Instruction{Op: browserruntime.OpDup},
				browserruntime.Instruction{Op: browserruntime.OpConstant, A: 0},
				browserruntime.Instruction{Op: browserruntime.OpConstant, A: 1},
				browserruntime.Instruction{Op: browserruntime.OpSetOwnProperty},
				browserruntime.Instruction{Op: browserruntime.OpPop},
				browserruntime.Instruction{Op: browserruntime.OpReturn},
			),
			[]memory.Value{memory.RefValue(name), memory.NumberValue(42)},
		)
		if err != nil {
			return err
		}
		objectValue, err := interpreter.Execute(task, objectFunction)
		if err != nil {
			return err
		}
		sourceObject = objectValue.Ref()

		arrayFunction, err := task.NewBytecodeFunction(
			memory.NullValue(), memory.NullValue(), 1,
			browserruntime.Assemble(
				browserruntime.Instruction{Op: browserruntime.OpNewArray},
				browserruntime.Instruction{Op: browserruntime.OpDup},
				browserruntime.Instruction{Op: browserruntime.OpConstant, A: 0},
				browserruntime.Instruction{Op: browserruntime.OpArgument, A: 0},
				browserruntime.Instruction{Op: browserruntime.OpSetElement},
				browserruntime.Instruction{Op: browserruntime.OpPop},
				browserruntime.Instruction{Op: browserruntime.OpDup},
				browserruntime.Instruction{Op: browserruntime.OpGetLength},
				browserruntime.Instruction{Op: browserruntime.OpPop},
				browserruntime.Instruction{Op: browserruntime.OpDup},
				browserruntime.Instruction{Op: browserruntime.OpConstant, A: 0},
				browserruntime.Instruction{Op: browserruntime.OpGetElement},
				browserruntime.Instruction{Op: browserruntime.OpPop},
				browserruntime.Instruction{Op: browserruntime.OpDup},
				browserruntime.Instruction{Op: browserruntime.OpConstant, A: 1},
				browserruntime.Instruction{Op: browserruntime.OpDeleteElement},
				browserruntime.Instruction{Op: browserruntime.OpPop},
				browserruntime.Instruction{Op: browserruntime.OpDup},
				browserruntime.Instruction{Op: browserruntime.OpConstant, A: 2},
				browserruntime.Instruction{Op: browserruntime.OpSetLength},
				browserruntime.Instruction{Op: browserruntime.OpPop},
				browserruntime.Instruction{Op: browserruntime.OpReturn},
			),
			[]memory.Value{memory.NumberValue(0), memory.NumberValue(1), memory.NumberValue(3)},
		)
		if err != nil {
			return err
		}
		arrayValue, err := interpreter.Execute(task, arrayFunction, objectValue)
		if err != nil {
			return err
		}
		sourceArray = arrayValue.Ref()
		promotedArray, err = task.PromoteRef(sourceArray)
		if err != nil {
			return err
		}
		return task.Realm.Store().CheckInvariants()
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := realm.Store().DerefObject(realm.Owner(), sourceObject); !errors.Is(err, memory.ErrStaleRef) {
		t.Fatalf("source Object after task = %v, want ErrStaleRef", err)
	}
	if _, err := realm.Store().DerefArray(realm.Owner(), sourceArray); !errors.Is(err, memory.ErrStaleRef) {
		t.Fatalf("source Array after task = %v, want ErrStaleRef", err)
	}
	array, err := realm.Store().DerefArray(realm.Owner(), promotedArray)
	if err != nil || array.Length != 3 || len(array.Elements) != 1 || !array.Elements[0].Value.IsRef() {
		t.Fatalf("promoted Array = %#v, %v", array, err)
	}
	object, err := realm.Store().DerefObject(realm.Owner(), array.Elements[0].Value.Ref())
	if err != nil || len(object.Properties) != 1 || object.Properties[0].Value.Number() != 42 {
		t.Fatalf("promoted Object = %#v, %v", object, err)
	}
	if err := realm.Store().CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func TestInterpreterBindingVerbsUseCapturedContexts(t *testing.T) {
	t.Parallel()

	realm, err := browserruntime.NewRealm(703, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer realm.Close()
	interpreter := browserruntime.NewInterpreter(browserruntime.InterpreterConfig{})
	var result memory.Value
	_, err = realm.EnqueueTask(func(task *browserruntime.TaskContext) error {
		x, err := task.NewString("x")
		if err != nil {
			return err
		}
		y, err := task.NewString("y")
		if err != nil {
			return err
		}
		parent, err := task.NewContext(memory.NullValue())
		if err != nil {
			return err
		}
		if err := task.DeclareBinding(parent, x, false); err != nil {
			return err
		}
		if err := task.InitializeBinding(parent, x, memory.NumberValue(11)); err != nil {
			return err
		}
		child, err := task.NewContext(memory.RefValue(parent))
		if err != nil {
			return err
		}
		function, err := task.NewBytecodeFunction(
			memory.NullValue(), memory.RefValue(child), 0,
			browserruntime.Assemble(
				browserruntime.Instruction{Op: browserruntime.OpLoadBinding, A: 0},
				browserruntime.Instruction{Op: browserruntime.OpPop},
				browserruntime.Instruction{Op: browserruntime.OpDeclareBinding, A: 1, B: 1},
				browserruntime.Instruction{Op: browserruntime.OpConstant, A: 2},
				browserruntime.Instruction{Op: browserruntime.OpInitializeBinding, A: 1},
				browserruntime.Instruction{Op: browserruntime.OpPop},
				browserruntime.Instruction{Op: browserruntime.OpConstant, A: 3},
				browserruntime.Instruction{Op: browserruntime.OpStoreBinding, A: 1},
				browserruntime.Instruction{Op: browserruntime.OpPop},
				browserruntime.Instruction{Op: browserruntime.OpLoadBinding, A: 1},
				browserruntime.Instruction{Op: browserruntime.OpReturn},
			),
			[]memory.Value{memory.RefValue(x), memory.RefValue(y), memory.NumberValue(5), memory.NumberValue(8)},
		)
		if err != nil {
			return err
		}
		result, err = interpreter.Execute(task, function)
		if err != nil {
			return err
		}

		immutableStore, err := task.NewBytecodeFunction(
			memory.NullValue(), memory.RefValue(child), 0,
			browserruntime.Assemble(
				browserruntime.Instruction{Op: browserruntime.OpConstant, A: 1},
				browserruntime.Instruction{Op: browserruntime.OpStoreBinding, A: 0},
				browserruntime.Instruction{Op: browserruntime.OpReturn},
			),
			[]memory.Value{memory.RefValue(x), memory.NumberValue(12)},
		)
		if err != nil {
			return err
		}
		if _, err := interpreter.Execute(task, immutableStore); !errors.Is(err, memory.ErrImmutableBinding) {
			t.Fatalf("immutable binding store = %v, want ErrImmutableBinding", err)
		}

		thisFunction, err := task.NewBytecodeFunction(
			memory.NullValue(), memory.NullValue(), 0,
			browserruntime.Assemble(
				browserruntime.Instruction{Op: browserruntime.OpLoadThis},
				browserruntime.Instruction{Op: browserruntime.OpReturn},
			), nil,
		)
		if err != nil {
			return err
		}
		if this, err := interpreter.Execute(task, thisFunction); err != nil || this.Kind() != memory.ValueUndefined {
			t.Fatalf("top-level this = %#v, %v", this, err)
		}
		return task.Realm.Store().CheckInvariants()
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if result.Kind() != memory.ValueNumber || result.Number() != 8 {
		t.Fatalf("binding result = %#v, want 8", result)
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
