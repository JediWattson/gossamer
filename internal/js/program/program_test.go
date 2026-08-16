package program_test

import (
	"context"
	"errors"
	"testing"

	"github.com/JediWattson/gossamer/internal/js/program"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

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
