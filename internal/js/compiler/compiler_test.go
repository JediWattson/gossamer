package compiler_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/js/compiler"
	"github.com/JediWattson/gossamer/internal/js/program"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func TestCompileExecutesCoreSourceThroughRegionStore(t *testing.T) {
	t.Parallel()

	source := `
let result = {value: 40 + 2, list: [1,,3]};
while (result.value < 45) {
  result.value = result.value + 1;
}
if (result.value === 45) {
  result.value = result.value - 3;
} else {
  result.value = 0;
}
result.value;
`
	image, err := compiler.Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	if image.FunctionCount() != 1 {
		t.Fatalf("function count = %d", image.FunctionCount())
	}
	entry, _ := image.Function(image.Entry())
	if len(entry.Locations) != len(entry.Code)/browserruntime.InstructionWidth {
		t.Fatalf("locations = %d, instructions = %d", len(entry.Locations), len(entry.Code)/browserruntime.InstructionWidth)
	}
	disassembly, err := browserruntime.Disassemble(entry.Code)
	if err != nil {
		t.Fatal(err)
	}
	for _, opcode := range []string{"NewObject", "SetOwnProperty", "JumpIfFalse", "Add", "Subtract"} {
		if !strings.Contains(disassembly, opcode) {
			t.Fatalf("disassembly does not contain %s:\n%s", opcode, disassembly)
		}
	}

	result := execute(t, 810, image)
	if result.Kind() != memory.ValueNumber || result.Number() != 42 {
		t.Fatalf("compiled result = %#v, want 42", result)
	}
}

func TestCompileExecutesArraysLogicalBranchesAndLoopControl(t *testing.T) {
	t.Parallel()

	image, err := compiler.Compile(`
let values = [10,,30];
let chosen = false || values[0];
let fallback = null ?? values[2];
let i = 0;
let sum = 0;
while (i < 10) {
  i++;
  if (i === 3) { continue; }
  if (i === 6) { break; }
  sum = sum + i;
}
(chosen === 10 && fallback === 30) ? sum : 0;
`)
	if err != nil {
		t.Fatal(err)
	}
	result := execute(t, 811, image)
	if result.Kind() != memory.ValueNumber || result.Number() != 12 {
		t.Fatalf("control result = %#v, want 12", result)
	}
}

func TestCompileExecutesStringObjectAndDeleteVerbs(t *testing.T) {
	t.Parallel()

	image, err := compiler.Compile(`
const prefix = "gos";
let object = {name: prefix + "samer", gone: 1};
delete object.gone;
object.name;
`)
	if err != nil {
		t.Fatal(err)
	}
	// The task owns this String, so explicitly promote it before task release.
	realm, err := browserruntime.NewRealm(812, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer realm.Close()
	interpreter := browserruntime.NewInterpreter(browserruntime.InterpreterConfig{})
	var promoted memory.Ref
	_, err = realm.EnqueueTask(func(task *browserruntime.TaskContext) error {
		environment, err := task.NewContext(memory.NullValue())
		if err != nil {
			return err
		}
		loaded, err := program.Load(task, image, memory.RefValue(environment))
		if err != nil {
			return err
		}
		value, err := interpreter.Execute(task, loaded.Entry)
		if err != nil {
			return err
		}
		promoted, err = task.PromoteRef(value.Ref())
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if text, err := realm.Store().DerefString(realm.Owner(), promoted); err != nil || text != "gossamer" {
		t.Fatalf("compiled String = %q, %v", text, err)
	}
}

func TestCompileRejectsSemanticWorkOutsideN5(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		"missing;",
		"const fixed = 1; fixed = 2;",
		"let value = 1; +value;",
		"let value = 1; value == 1;",
		"break;",
		"let object = {value: 1}; object.value++;",
	} {
		_, err := compiler.Compile(source)
		if !errors.Is(err, compiler.ErrCompile) {
			t.Fatalf("Compile(%q) error = %v", source, err)
		}
		var problem *compiler.Error
		if !errors.As(err, &problem) || problem.Span.Start.Line == 0 || problem.Span.Start.Column == 0 {
			t.Fatalf("Compile(%q) diagnostic = %#v", source, problem)
		}
	}
}

func execute(t *testing.T, realmID browserruntime.RealmID, image program.Program) memory.Value {
	t.Helper()
	realm, err := browserruntime.NewRealm(realmID, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer realm.Close()
	interpreter := browserruntime.NewInterpreter(browserruntime.InterpreterConfig{})
	var result memory.Value
	_, err = realm.EnqueueTask(func(task *browserruntime.TaskContext) error {
		environment, err := task.NewContext(memory.NullValue())
		if err != nil {
			return err
		}
		loaded, err := program.Load(task, image, memory.RefValue(environment))
		if err != nil {
			return err
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
	if err := realm.Store().CheckInvariants(); err != nil {
		t.Fatal(err)
	}
	return result
}
