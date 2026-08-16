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
			{name: "jump bounds", vm: browserruntime.NewInterpreter(browserruntime.InterpreterConfig{}), code: browserruntime.Assemble(browserruntime.Instruction{Op: browserruntime.OpJump, A: 9}, browserruntime.Instruction{Op: browserruntime.OpReturn}), want: browserruntime.ErrInvalidBytecode},
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

func TestInterpreterOperatorsAndControlFlow(t *testing.T) {
	t.Parallel()

	realm, err := browserruntime.NewRealm(704, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer realm.Close()
	interpreter := browserruntime.NewInterpreter(browserruntime.InterpreterConfig{})
	_, err = realm.EnqueueTask(func(task *browserruntime.TaskContext) error {
		numeric := []struct {
			op    browserruntime.Opcode
			left  float64
			right float64
			want  float64
		}{
			{browserruntime.OpAdd, 8, 3, 11},
			{browserruntime.OpSubtract, 8, 3, 5},
			{browserruntime.OpMultiply, 8, 3, 24},
			{browserruntime.OpDivide, 8, 2, 4},
			{browserruntime.OpRemainder, 8, 3, 2},
			{browserruntime.OpBitwiseAnd, 6, 3, 2},
			{browserruntime.OpBitwiseOr, 6, 3, 7},
			{browserruntime.OpBitwiseXor, 6, 3, 5},
			{browserruntime.OpShiftLeft, 3, 2, 12},
			{browserruntime.OpShiftRight, -8, 1, -4},
			{browserruntime.OpUnsignedShiftRight, -1, 1, 2147483647},
		}
		for _, test := range numeric {
			function, err := task.NewBytecodeFunction(
				memory.NullValue(), memory.NullValue(), 0,
				browserruntime.Assemble(
					browserruntime.Instruction{Op: browserruntime.OpConstant, A: 0},
					browserruntime.Instruction{Op: browserruntime.OpConstant, A: 1},
					browserruntime.Instruction{Op: test.op},
					browserruntime.Instruction{Op: browserruntime.OpReturn},
				),
				[]memory.Value{memory.NumberValue(test.left), memory.NumberValue(test.right)},
			)
			if err != nil {
				return err
			}
			result, err := interpreter.Execute(task, function)
			if err != nil || result.Kind() != memory.ValueNumber || result.Number() != test.want {
				t.Fatalf("%s(%v, %v) = %#v, %v; want %v", test.op, test.left, test.right, result, err, test.want)
			}
		}

		comparisons := []struct {
			op    browserruntime.Opcode
			left  float64
			right float64
			want  bool
		}{
			{browserruntime.OpStrictEqual, 3, 3, true},
			{browserruntime.OpStrictNotEqual, 3, 4, true},
			{browserruntime.OpLessThan, 3, 4, true},
			{browserruntime.OpLessThanOrEqual, 3, 3, true},
			{browserruntime.OpGreaterThan, 4, 3, true},
			{browserruntime.OpGreaterThanOrEqual, 4, 4, true},
		}
		for _, test := range comparisons {
			function, err := task.NewBytecodeFunction(
				memory.NullValue(), memory.NullValue(), 0,
				browserruntime.Assemble(
					browserruntime.Instruction{Op: browserruntime.OpConstant, A: 0},
					browserruntime.Instruction{Op: browserruntime.OpConstant, A: 1},
					browserruntime.Instruction{Op: test.op},
					browserruntime.Instruction{Op: browserruntime.OpReturn},
				),
				[]memory.Value{memory.NumberValue(test.left), memory.NumberValue(test.right)},
			)
			if err != nil {
				return err
			}
			result, err := interpreter.Execute(task, function)
			if err != nil || result.Kind() != memory.ValueBool || result.Bool() != test.want {
				t.Fatalf("%s(%v, %v) = %#v, %v; want %t", test.op, test.left, test.right, result, err, test.want)
			}
		}

		unary := []struct {
			op    browserruntime.Opcode
			value float64
			want  float64
		}{
			{browserruntime.OpNegate, 3, -3},
			{browserruntime.OpIncrement, 3, 4},
			{browserruntime.OpDecrement, 3, 2},
		}
		for _, test := range unary {
			function, err := task.NewBytecodeFunction(
				memory.NullValue(), memory.NullValue(), 0,
				browserruntime.Assemble(
					browserruntime.Instruction{Op: browserruntime.OpConstant, A: 0},
					browserruntime.Instruction{Op: test.op},
					browserruntime.Instruction{Op: browserruntime.OpReturn},
				), []memory.Value{memory.NumberValue(test.value)},
			)
			if err != nil {
				return err
			}
			result, err := interpreter.Execute(task, function)
			if err != nil || result.Number() != test.want {
				t.Fatalf("%s(%v) = %#v, %v; want %v", test.op, test.value, result, err, test.want)
			}
		}

		branches := []struct {
			op        browserruntime.Opcode
			condition memory.Value
		}{
			{browserruntime.OpJumpIfTrue, memory.BoolValue(true)},
			{browserruntime.OpJumpIfFalse, memory.BoolValue(false)},
			{browserruntime.OpJumpIfNullish, memory.NullValue()},
		}
		for _, test := range branches {
			function, err := task.NewBytecodeFunction(
				memory.NullValue(), memory.NullValue(), 0,
				browserruntime.Assemble(
					browserruntime.Instruction{Op: browserruntime.OpConstant, A: 0},
					browserruntime.Instruction{Op: test.op, A: 4},
					browserruntime.Instruction{Op: browserruntime.OpConstant, A: 1},
					browserruntime.Instruction{Op: browserruntime.OpJump, A: 5},
					browserruntime.Instruction{Op: browserruntime.OpConstant, A: 2},
					browserruntime.Instruction{Op: browserruntime.OpReturn},
				), []memory.Value{test.condition, memory.NumberValue(0), memory.NumberValue(1)},
			)
			if err != nil {
				return err
			}
			result, err := interpreter.Execute(task, function)
			if err != nil || result.Number() != 1 {
				t.Fatalf("%s branch = %#v, %v; want 1", test.op, result, err)
			}
		}

		logicalNot, err := task.NewBytecodeFunction(
			memory.NullValue(), memory.NullValue(), 0,
			browserruntime.Assemble(
				browserruntime.Instruction{Op: browserruntime.OpFalse},
				browserruntime.Instruction{Op: browserruntime.OpLogicalNot},
				browserruntime.Instruction{Op: browserruntime.OpReturn},
			), nil,
		)
		if err != nil {
			return err
		}
		if result, err := interpreter.Execute(task, logicalNot); err != nil || !result.Bool() {
			t.Fatalf("LogicalNot(false) = %#v, %v", result, err)
		}

		left, err := task.NewString("gos")
		if err != nil {
			return err
		}
		right, err := task.NewString("samer")
		if err != nil {
			return err
		}
		stringFunction, err := task.NewBytecodeFunction(
			memory.NullValue(), memory.NullValue(), 0,
			browserruntime.Assemble(
				browserruntime.Instruction{Op: browserruntime.OpConstant, A: 0},
				browserruntime.Instruction{Op: browserruntime.OpConstant, A: 1},
				browserruntime.Instruction{Op: browserruntime.OpAdd},
				browserruntime.Instruction{Op: browserruntime.OpTypeOf},
				browserruntime.Instruction{Op: browserruntime.OpReturn},
			), []memory.Value{memory.RefValue(left), memory.RefValue(right)},
		)
		if err != nil {
			return err
		}
		typeValue, err := interpreter.Execute(task, stringFunction)
		if err != nil {
			return err
		}
		if got, err := task.DerefString(typeValue.Ref()); err != nil || got != "string" {
			t.Fatalf("typeof concatenated String = %q, %v", got, err)
		}

		iName, err := task.NewString("i")
		if err != nil {
			return err
		}
		sumName, err := task.NewString("sum")
		if err != nil {
			return err
		}
		environment, err := task.NewContext(memory.NullValue())
		if err != nil {
			return err
		}
		loop, err := task.NewBytecodeFunction(
			memory.NullValue(), memory.RefValue(environment), 0,
			browserruntime.Assemble(
				browserruntime.Instruction{Op: browserruntime.OpDeclareBinding, A: 0, B: 1},
				browserruntime.Instruction{Op: browserruntime.OpConstant, A: 2},
				browserruntime.Instruction{Op: browserruntime.OpInitializeBinding, A: 0},
				browserruntime.Instruction{Op: browserruntime.OpPop},
				browserruntime.Instruction{Op: browserruntime.OpDeclareBinding, A: 1, B: 1},
				browserruntime.Instruction{Op: browserruntime.OpConstant, A: 2},
				browserruntime.Instruction{Op: browserruntime.OpInitializeBinding, A: 1},
				browserruntime.Instruction{Op: browserruntime.OpPop},
				browserruntime.Instruction{Op: browserruntime.OpLoadBinding, A: 0},
				browserruntime.Instruction{Op: browserruntime.OpConstant, A: 3},
				browserruntime.Instruction{Op: browserruntime.OpLessThan},
				browserruntime.Instruction{Op: browserruntime.OpJumpIfFalse, A: 22},
				browserruntime.Instruction{Op: browserruntime.OpLoadBinding, A: 1},
				browserruntime.Instruction{Op: browserruntime.OpLoadBinding, A: 0},
				browserruntime.Instruction{Op: browserruntime.OpAdd},
				browserruntime.Instruction{Op: browserruntime.OpStoreBinding, A: 1},
				browserruntime.Instruction{Op: browserruntime.OpPop},
				browserruntime.Instruction{Op: browserruntime.OpLoadBinding, A: 0},
				browserruntime.Instruction{Op: browserruntime.OpIncrement},
				browserruntime.Instruction{Op: browserruntime.OpStoreBinding, A: 0},
				browserruntime.Instruction{Op: browserruntime.OpPop},
				browserruntime.Instruction{Op: browserruntime.OpJump, A: 8},
				browserruntime.Instruction{Op: browserruntime.OpLoadBinding, A: 1},
				browserruntime.Instruction{Op: browserruntime.OpReturn},
			),
			[]memory.Value{memory.RefValue(iName), memory.RefValue(sumName), memory.NumberValue(0), memory.NumberValue(5)},
		)
		if err != nil {
			return err
		}
		loopResult, err := interpreter.Execute(task, loop)
		if err != nil || loopResult.Number() != 10 {
			t.Fatalf("loop result = %#v, %v; want 10", loopResult, err)
		}
		return task.Realm.Store().CheckInvariants()
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestInterpreterCallsClosuresConstructorsAndNativeDispatch(t *testing.T) {
	t.Parallel()

	realm, err := browserruntime.NewRealm(705, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer realm.Close()
	interpreter := browserruntime.NewInterpreter(browserruntime.InterpreterConfig{MaxCallDepth: 8})
	if err := interpreter.RegisterNative(1, func(_ *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
		if this.Kind() != memory.ValueUndefined || len(arguments) != 2 {
			return memory.Value{}, errors.New("unexpected native invocation")
		}
		return memory.NumberValue(arguments[0].Number() + arguments[1].Number()), nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := interpreter.RegisterNative(1, func(*browserruntime.TaskContext, memory.Value, []memory.Value) (memory.Value, error) {
		return memory.Value{}, nil
	}); !errors.Is(err, browserruntime.ErrNativeFunction) {
		t.Fatalf("duplicate native registration = %v", err)
	}

	var promotedConstructed memory.Ref
	_, err = realm.EnqueueTask(func(task *browserruntime.TaskContext) error {
		multiply, err := task.NewBytecodeFunction(
			memory.NullValue(), memory.NullValue(), 2,
			browserruntime.Assemble(
				browserruntime.Instruction{Op: browserruntime.OpArgument, A: 0},
				browserruntime.Instruction{Op: browserruntime.OpArgument, A: 1},
				browserruntime.Instruction{Op: browserruntime.OpMultiply},
				browserruntime.Instruction{Op: browserruntime.OpReturn},
			), nil,
		)
		if err != nil {
			return err
		}
		caller, err := task.NewBytecodeFunction(
			memory.NullValue(), memory.NullValue(), 0,
			browserruntime.Assemble(
				browserruntime.Instruction{Op: browserruntime.OpConstant, A: 0},
				browserruntime.Instruction{Op: browserruntime.OpConstant, A: 1},
				browserruntime.Instruction{Op: browserruntime.OpConstant, A: 2},
				browserruntime.Instruction{Op: browserruntime.OpCall, A: 2},
				browserruntime.Instruction{Op: browserruntime.OpReturn},
			), []memory.Value{memory.RefValue(multiply), memory.NumberValue(6), memory.NumberValue(7)},
		)
		if err != nil {
			return err
		}
		if result, err := interpreter.Execute(task, caller); err != nil || result.Number() != 42 {
			t.Fatalf("bytecode Call = %#v, %v", result, err)
		}

		native, err := task.NewNativeFunction(memory.NullValue(), memory.NullValue(), 2, 1)
		if err != nil {
			return err
		}
		nativeCaller, err := task.NewBytecodeFunction(
			memory.NullValue(), memory.NullValue(), 0,
			browserruntime.Assemble(
				browserruntime.Instruction{Op: browserruntime.OpConstant, A: 0},
				browserruntime.Instruction{Op: browserruntime.OpConstant, A: 1},
				browserruntime.Instruction{Op: browserruntime.OpConstant, A: 2},
				browserruntime.Instruction{Op: browserruntime.OpCallNative, A: 2},
				browserruntime.Instruction{Op: browserruntime.OpReturn},
			), []memory.Value{memory.RefValue(native), memory.NumberValue(10), memory.NumberValue(5)},
		)
		if err != nil {
			return err
		}
		if result, err := interpreter.Execute(task, nativeCaller); err != nil || result.Number() != 15 {
			t.Fatalf("native Call = %#v, %v", result, err)
		}

		capturedName, err := task.NewString("captured")
		if err != nil {
			return err
		}
		environment, err := task.NewContext(memory.NullValue())
		if err != nil {
			return err
		}
		if err := task.DeclareBinding(environment, capturedName, false); err != nil {
			return err
		}
		if err := task.InitializeBinding(environment, capturedName, memory.NumberValue(91)); err != nil {
			return err
		}
		template, err := task.NewBytecodeFunction(
			memory.NullValue(), memory.NullValue(), 0,
			browserruntime.Assemble(
				browserruntime.Instruction{Op: browserruntime.OpLoadBinding, A: 0},
				browserruntime.Instruction{Op: browserruntime.OpReturn},
			), []memory.Value{memory.RefValue(capturedName)},
		)
		if err != nil {
			return err
		}
		factory, err := task.NewBytecodeFunction(
			memory.NullValue(), memory.RefValue(environment), 0,
			browserruntime.Assemble(
				browserruntime.Instruction{Op: browserruntime.OpCreateClosure, A: 0},
				browserruntime.Instruction{Op: browserruntime.OpReturn},
			), []memory.Value{memory.RefValue(template)},
		)
		if err != nil {
			return err
		}
		closure, err := interpreter.Execute(task, factory)
		if err != nil {
			return err
		}
		if result, err := interpreter.Execute(task, closure.Ref()); err != nil || result.Number() != 91 {
			t.Fatalf("closure result = %#v, %v", result, err)
		}

		propertyName, err := task.NewString("value")
		if err != nil {
			return err
		}
		constructor, err := task.NewBytecodeFunction(
			memory.NullValue(), memory.NullValue(), 1,
			browserruntime.Assemble(
				browserruntime.Instruction{Op: browserruntime.OpLoadThis},
				browserruntime.Instruction{Op: browserruntime.OpConstant, A: 0},
				browserruntime.Instruction{Op: browserruntime.OpArgument, A: 0},
				browserruntime.Instruction{Op: browserruntime.OpSetOwnProperty},
				browserruntime.Instruction{Op: browserruntime.OpPop},
				browserruntime.Instruction{Op: browserruntime.OpUndefined},
				browserruntime.Instruction{Op: browserruntime.OpReturn},
			), []memory.Value{memory.RefValue(propertyName)},
		)
		if err != nil {
			return err
		}
		constructCaller, err := task.NewBytecodeFunction(
			memory.NullValue(), memory.NullValue(), 0,
			browserruntime.Assemble(
				browserruntime.Instruction{Op: browserruntime.OpConstant, A: 0},
				browserruntime.Instruction{Op: browserruntime.OpConstant, A: 1},
				browserruntime.Instruction{Op: browserruntime.OpConstruct, A: 1},
				browserruntime.Instruction{Op: browserruntime.OpReturn},
			), []memory.Value{memory.RefValue(constructor), memory.NumberValue(77)},
		)
		if err != nil {
			return err
		}
		constructed, err := interpreter.Execute(task, constructCaller)
		if err != nil {
			return err
		}
		promotedConstructed, err = task.PromoteRef(constructed.Ref())
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
	constructed, err := realm.Store().DerefObject(realm.Owner(), promotedConstructed)
	if err != nil || len(constructed.Properties) != 1 || constructed.Properties[0].Value.Number() != 77 {
		t.Fatalf("constructed Object = %#v, %v", constructed, err)
	}
	if err := realm.Store().CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func TestInterpreterEnforcesSharedCallDepth(t *testing.T) {
	t.Parallel()

	realm, err := browserruntime.NewRealm(706, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer realm.Close()
	interpreter := browserruntime.NewInterpreter(browserruntime.InterpreterConfig{MaxCallDepth: 4})
	_, err = realm.EnqueueTask(func(task *browserruntime.TaskContext) error {
		selfName, err := task.NewString("self")
		if err != nil {
			return err
		}
		environment, err := task.NewContext(memory.NullValue())
		if err != nil {
			return err
		}
		if err := task.DeclareBinding(environment, selfName, false); err != nil {
			return err
		}
		function, err := task.NewBytecodeFunction(
			memory.NullValue(), memory.RefValue(environment), 0,
			browserruntime.Assemble(
				browserruntime.Instruction{Op: browserruntime.OpLoadBinding, A: 0},
				browserruntime.Instruction{Op: browserruntime.OpCall},
				browserruntime.Instruction{Op: browserruntime.OpReturn},
			), []memory.Value{memory.RefValue(selfName)},
		)
		if err != nil {
			return err
		}
		if err := task.InitializeBinding(environment, selfName, memory.RefValue(function)); err != nil {
			return err
		}
		if _, err := interpreter.Execute(task, function); !errors.Is(err, browserruntime.ErrCallDepth) {
			t.Fatalf("recursive call error = %v, want ErrCallDepth", err)
		}
		return task.Realm.Store().CheckInvariants()
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
