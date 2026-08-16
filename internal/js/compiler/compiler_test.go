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

func TestCompileExecutesFunctionsClosuresAndFreshInvocationScopes(t *testing.T) {
	t.Parallel()

	image, err := compiler.Compile(`
function add(a, b) { return a + b; }
function make(base) {
  let offset = 2;
  return function(value) { return base + offset + value; };
}
function factorial(n) {
  if (n <= 1) { return 1; }
  return n * factorial(n - 1);
}
let first = make(10);
let second = make(20);
add(first(1), second(2)) + factorial(5);
`)
	if err != nil {
		t.Fatal(err)
	}
	if image.FunctionCount() != 5 {
		t.Fatalf("Function count = %d, want 5", image.FunctionCount())
	}
	result := execute(t, 814, image)
	if result.Kind() != memory.ValueNumber || result.Number() != 157 {
		t.Fatalf("Function result = %#v, want 157", result)
	}
}

func TestCompileExecutesConstructionWithExplicitThis(t *testing.T) {
	t.Parallel()

	image, err := compiler.Compile(`
function Box(value) {
  this.value = value;
}
let box = new Box(77);
box.value;
`)
	if err != nil {
		t.Fatal(err)
	}
	result := execute(t, 815, image)
	if result.Kind() != memory.ValueNumber || result.Number() != 77 {
		t.Fatalf("constructed value = %#v, want 77", result)
	}
}

func TestCompileExecutesCatchRethrowAndReturnThroughFinally(t *testing.T) {
	t.Parallel()

	image, err := compiler.Compile(`
function caught(value) {
  try {
    if (value) { throw 13; }
    return 5;
  } catch (problem) {
    return problem + 1;
  } finally {
    let touched = 1;
    touched;
  }
}
function override() {
  try { return 1; } finally { return 2; }
}
function nested() {
  try {
    try { throw 7; } catch (inner) { throw inner + 1; }
  } catch (outer) {
    return outer;
  }
}
function capture() {
  try { throw 3; } catch (captured) {
    return function() { return captured; };
  }
}
caught(true) + caught(false) + override() + nested() + capture()();
`)
	if err != nil {
		t.Fatal(err)
	}
	result := execute(t, 816, image)
	if result.Kind() != memory.ValueNumber || result.Number() != 32 {
		t.Fatalf("exception result = %#v, want 32", result)
	}
}

func TestCompileRoutesLoopCompletionsThroughFinally(t *testing.T) {
	t.Parallel()

	image, err := compiler.Compile(`
function breakFinally() {
  let total = 0;
  while (true) {
    try { break; } finally { total = total + 1; }
  }
  return total;
}
function continueFinally() {
  let index = 0;
  let total = 0;
  while (index < 3) {
    index = index + 1;
    try { continue; } finally { total = total + index; }
  }
  return total;
}
function nestedFinally() {
  let total = 0;
  while (true) {
    try {
      try { break; } finally { total = total + 1; }
    } finally { total = total + 10; }
  }
  return total;
}
function overridingFinally() {
  while (true) {
    try { break; } finally { return 7; }
  }
  return 1;
}
function discardCatch() {
  while (true) {
    try { break; } catch (problem) { return problem; }
  }
  return 2;
}
breakFinally() + continueFinally() + nestedFinally() + overridingFinally() + discardCatch();
`)
	if err != nil {
		t.Fatal(err)
	}
	result := execute(t, 817, image)
	if result.Kind() != memory.ValueNumber || result.Number() != 27 {
		t.Fatalf("completion result = %#v, want 27", result)
	}
}

func TestCompileInstantiatesHoistedAndBlockBindings(t *testing.T) {
	t.Parallel()

	image, err := compiler.Compile(`
let outer = 1;
let captured;
{
  let outer = 40;
  captured = function() { return outer + 2; };
}
function callBeforeDeclaration() {
  return answer();
  function answer() { return 41; }
}
function functionScopedVar() {
  if (true) { var value = 8; }
  return value;
}
captured() + outer + callBeforeDeclaration() + functionScopedVar();
`)
	if err != nil {
		t.Fatal(err)
	}
	result := execute(t, 818, image)
	if result.Kind() != memory.ValueNumber || result.Number() != 92 {
		t.Fatalf("scope result = %#v, want 92", result)
	}
}

func TestCompileLexicalBindingsHaveTemporalDeadZones(t *testing.T) {
	t.Parallel()

	image, err := compiler.Compile(`
let value = 1;
{
  value;
  let value = 2;
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if err := executeError(t, 819, image); !errors.Is(err, memory.ErrBindingUninitialized) {
		t.Fatalf("TDZ error = %v, want ErrBindingUninitialized", err)
	}

	if _, err := compiler.Compile("const missing;"); err == nil {
		t.Fatalf("const without initializer error = %v", err)
	}
}

func TestCompileRejectsInvalidFunctionControlFlow(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		"return 1;",
		"function duplicate(value, value) { return value; } duplicate(1, 2);",
	} {
		_, err := compiler.Compile(source)
		if !errors.Is(err, compiler.ErrCompile) {
			t.Fatalf("Compile(%q) error = %v", source, err)
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

func executeError(t *testing.T, realmID browserruntime.RealmID, image program.Program) error {
	t.Helper()
	realm, err := browserruntime.NewRealm(realmID, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer realm.Close()
	interpreter := browserruntime.NewInterpreter(browserruntime.InterpreterConfig{})
	_, err = realm.EnqueueTask(func(task *browserruntime.TaskContext) error {
		environment, err := task.NewContext(memory.NullValue())
		if err != nil {
			return err
		}
		loaded, err := program.Load(task, image, memory.RefValue(environment))
		if err != nil {
			return err
		}
		_, err = interpreter.Execute(task, loaded.Entry)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return realm.RunOne(context.Background())
}
