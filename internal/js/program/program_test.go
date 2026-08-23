package program_test

import (
	"context"
	"errors"
	"math"
	"testing"
	"unsafe"

	"github.com/JediWattson/gossamer/internal/js/program"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func TestCompactConstantPreservesPortableVariants(t *testing.T) {
	if size := unsafe.Sizeof(program.Constant{}); size != 32 {
		t.Fatalf("Constant size = %d bytes, want 32", size)
	}
	nan := math.Float64frombits(0x7ff8000000000042)
	for _, number := range []float64{0, math.Copysign(0, -1), 42.5, math.Inf(1), math.Inf(-1), nan} {
		constant := program.Number(number)
		if constant.Kind() != program.ConstantNumber || math.Float64bits(constant.Number()) != math.Float64bits(number) {
			t.Fatalf("Number(%v) = kind %d, bits %016x", number, constant.Kind(), math.Float64bits(constant.Number()))
		}
	}
	if constant := program.Bool(true); constant.Kind() != program.ConstantBool || !constant.Bool() {
		t.Fatalf("Bool(true) = %#v", constant)
	}
	if constant := program.String("name"); constant.Kind() != program.ConstantString || constant.String() != "name" {
		t.Fatalf("String(name) = %#v", constant)
	}
	if constant := program.BigInt("0x2a"); constant.Kind() != program.ConstantBigInt || constant.String() != "0x2a" {
		t.Fatalf("BigInt(0x2a) = %#v", constant)
	}
	if constant := program.Function(42); constant.Kind() != program.ConstantFunction || constant.Function() != 42 {
		t.Fatalf("Function(42) = %#v", constant)
	}
	if constant := program.RegExp("pattern", "ygid"); constant.Kind() != program.ConstantRegExp || constant.String() != "pattern" || constant.Flags() != "ygid" {
		t.Fatalf("RegExp(pattern, ygid) = source %q flags %q", constant.String(), constant.Flags())
	}
	if program.String("same") != program.String("same") || program.Number(nan) != program.Number(nan) {
		t.Fatal("equivalent constants are not comparable deduplication keys")
	}
}

func TestProgramRejectsCompactRegExpWithInvalidFlags(t *testing.T) {
	code := browserruntime.Assemble(
		browserruntime.Instruction{Op: browserruntime.OpConstant, A: 0},
		browserruntime.Instruction{Op: browserruntime.OpReturn},
	)
	_, err := program.New([]program.FunctionTemplate{{
		Code: code, Constants: []program.Constant{program.RegExp("pattern", "gg")},
	}}, 0)
	if !errors.Is(err, program.ErrInvalidProgram) {
		t.Fatalf("invalid RegExp flags error = %v", err)
	}
}

func TestProgramDeepCopiesAndValidatesPortableFunctions(t *testing.T) {
	t.Parallel()

	code := browserruntime.Assemble(
		browserruntime.Instruction{Op: browserruntime.OpConstant, A: 0},
		browserruntime.Instruction{Op: browserruntime.OpReturn},
	)
	functions := []program.FunctionTemplate{{
		Name:      "answer",
		Code:      code,
		Constants: []program.Constant{program.Number(42)},
		Locations: []browserruntime.SourceSpan{{Start: 0, End: 2}, {Start: 3, End: 9}},
	}}
	image, err := program.New(functions, 0)
	if err != nil {
		t.Fatal(err)
	}
	functions[0].Code[0] = byte(browserruntime.OpPop)
	functions[0].Constants[0] = program.Number(99)
	copy, ok := image.Function(0)
	if !ok || copy.Code[0] != byte(browserruntime.OpConstant) || copy.Constants[0].Number() != 42 {
		t.Fatalf("immutable Function copy = %#v, %t", copy, ok)
	}
	copy.Code[0] = byte(browserruntime.OpPop)
	again, _ := image.Function(0)
	if again.Code[0] != byte(browserruntime.OpConstant) {
		t.Fatal("Function accessor exposed Program storage")
	}

	_, err = program.New([]program.FunctionTemplate{{
		Code: browserruntime.Assemble(
			browserruntime.Instruction{Op: browserruntime.OpCreateClosure, A: 0},
			browserruntime.Instruction{Op: browserruntime.OpReturn},
		),
		Constants: []program.Constant{program.Function(0)},
	}}, 0)
	if !errors.Is(err, program.ErrInvalidProgram) {
		t.Fatalf("cyclic Program error = %v", err)
	}
}

func TestLoadInstantiatesAndReleasesPortableProgram(t *testing.T) {
	t.Parallel()

	child := program.FunctionTemplate{
		Name:  "add",
		Arity: 2,
		Code: browserruntime.Assemble(
			browserruntime.Instruction{Op: browserruntime.OpArgument, A: 0},
			browserruntime.Instruction{Op: browserruntime.OpArgument, A: 1},
			browserruntime.Instruction{Op: browserruntime.OpAdd},
			browserruntime.Instruction{Op: browserruntime.OpReturn},
		),
	}
	entry := program.FunctionTemplate{
		Name: "script",
		Code: browserruntime.Assemble(
			browserruntime.Instruction{Op: browserruntime.OpCreateClosure, A: 0},
			browserruntime.Instruction{Op: browserruntime.OpConstant, A: 1},
			browserruntime.Instruction{Op: browserruntime.OpConstant, A: 2},
			browserruntime.Instruction{Op: browserruntime.OpCall, A: 2},
			browserruntime.Instruction{Op: browserruntime.OpReturn},
		),
		Constants: []program.Constant{program.Function(1), program.Number(40), program.Number(2)},
	}
	image, err := program.New([]program.FunctionTemplate{entry, child}, 0)
	if err != nil {
		t.Fatal(err)
	}

	realm, err := browserruntime.NewRealm(800, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer realm.Close()
	interpreter := browserruntime.NewInterpreter(browserruntime.InterpreterConfig{})
	var firstEntry memory.Ref
	for iteration := 0; iteration < 2; iteration++ {
		var result memory.Value
		_, err := realm.EnqueueTask(func(task *browserruntime.TaskContext) error {
			loaded, err := program.Load(task, image, memory.NullValue())
			if err != nil {
				return err
			}
			if iteration == 0 {
				firstEntry = loaded.Entry
			} else if loaded.Entry == firstEntry {
				t.Fatal("separate loads reused Ref identity")
			}
			result, err = interpreter.Execute(task, loaded.Entry)
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
		if result.Kind() != memory.ValueNumber || result.Number() != 42 {
			t.Fatalf("iteration %d result = %#v", iteration, result)
		}
		if _, err := realm.Store().DerefFunction(realm.Owner(), firstEntry); !errors.Is(err, memory.ErrStaleRef) {
			t.Fatalf("released entry error = %v", err)
		}
	}
	if err := realm.Store().CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}
