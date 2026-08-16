package runtime_test

import (
	"context"
	"errors"
	"testing"

	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func TestInterpreterResultRequiresExplicitEscapeBeforeMicrotask(t *testing.T) {
	t.Parallel()

	realm, err := browserruntime.NewRealm(708, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer realm.Close()
	interpreter := browserruntime.NewInterpreter(browserruntime.InterpreterConfig{})
	var source memory.Ref
	var promoted memory.Ref
	var observed bool
	_, err = realm.EnqueueTask(func(task *browserruntime.TaskContext) error {
		name, err := task.NewString("answer")
		if err != nil {
			return err
		}
		function, err := task.NewBytecodeFunction(
			memory.NullValue(), memory.NullValue(), 0,
			browserruntime.Assemble(
				browserruntime.Instruction{Op: browserruntime.OpNewObject},
				browserruntime.Instruction{Op: browserruntime.OpDup},
				browserruntime.Instruction{Op: browserruntime.OpConstant, A: 0},
				browserruntime.Instruction{Op: browserruntime.OpConstant, A: 1},
				browserruntime.Instruction{Op: browserruntime.OpSetOwnProperty},
				browserruntime.Instruction{Op: browserruntime.OpPop},
				browserruntime.Instruction{Op: browserruntime.OpReturn},
			), []memory.Value{memory.RefValue(name), memory.NumberValue(42)},
		)
		if err != nil {
			return err
		}
		result, err := interpreter.Execute(task, function)
		if err != nil {
			return err
		}
		source = result.Ref()
		if _, err := task.QueueMicrotaskSend(func(*browserruntime.TaskContext) error {
			t.Fatal("private interpreter result crossed an unqualified queue boundary")
			return nil
		}, source); !errors.Is(err, memory.ErrExplicitSendRequired) {
			t.Fatalf("private result send = %v, want ErrExplicitSendRequired", err)
		}
		promoted, err = task.PromoteRef(source)
		if err != nil {
			return err
		}
		_, err = task.QueueMicrotaskSend(func(microtask *browserruntime.TaskContext) error {
			if _, err := microtask.DerefObject(source); !errors.Is(err, memory.ErrStaleRef) {
				t.Fatalf("source result in microtask = %v, want ErrStaleRef", err)
			}
			object, err := microtask.DerefObject(microtask.Refs[0])
			if err != nil {
				return err
			}
			if len(object.Properties) != 1 || object.Properties[0].Value.Number() != 42 {
				t.Fatalf("promoted interpreter result = %#v", object)
			}
			if err := microtask.SetProperty(microtask.Refs[0], object.Properties[0].Name, memory.NumberValue(0)); !errors.Is(err, memory.ErrImmutableRegion) {
				t.Fatalf("published interpreter result mutation = %v, want ErrImmutableRegion", err)
			}
			observed = true
			return microtask.Realm.Store().CheckInvariants()
		}, promoted)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !observed {
		t.Fatal("microtask did not observe promoted interpreter result")
	}
	if _, err := realm.Store().DerefObject(realm.Owner(), source); !errors.Is(err, memory.ErrStaleRef) {
		t.Fatalf("source result after task = %v, want ErrStaleRef", err)
	}
	if _, err := realm.Store().DerefObject(realm.Owner(), promoted); err != nil {
		t.Fatalf("promoted result after microtask = %v", err)
	}
	if err := realm.Store().CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}
