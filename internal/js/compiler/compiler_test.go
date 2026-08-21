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

func TestCompileEmitsSpreadCallsArraysConstructionAndAccessors(t *testing.T) {
	t.Parallel()

	image, err := compiler.Compile(`
function sum(a, b, c) { return a + b + c; }
function Box(value) { this.value = value; }
let receiver = {
  base: 2,
  add(a, b) { return this.base + a + b; },
  get doubled() { return this.base * 2; },
  set doubled(value) { this.base = value / 2; }
};
let values = [0, ...[1, 2, 3], ...[4, 5]];
sum(...values);
receiver.add(...[3, 4]);
new Box(...[9]);
`)
	if err != nil {
		t.Fatal(err)
	}
	entry, _ := image.Function(image.Entry())
	disassembly, err := browserruntime.Disassemble(entry.Code)
	if err != nil {
		t.Fatal(err)
	}
	for _, opcode := range []string{"CallSpread", "CallMethodSpread", "ConstructSpread", "DefineAccessor"} {
		if !strings.Contains(disassembly, opcode) {
			t.Fatalf("disassembly does not contain %s:\n%s", opcode, disassembly)
		}
	}
}

func TestCompileExecutesReactControlFlowAndOperators(t *testing.T) {
	t.Parallel()

	image, err := compiler.Compile(`
let sum = 0;
outer: for (var i = 0; i < 5; i++) {
  if (i === 1) continue;
  switch (i) {
    case 2: sum += 10; break;
    case 3: break outer;
    default: sum += i;
  }
}
let object = {a: 1, b: 2};
for (var key in object) sum += object[key];
let joined = "";
for (let value of ["x", "y"]) joined = joined + value;
let count = 0;
do { count++; } while (count < 2);
let bits = 7;
bits &= 3;
bits <<= 2;
let add = value => value + 1;
let sequence = (sum = sum + 1, sum + 1);
sum === 14 && sequence === 15 && joined === "xy" && count === 2 && bits === 12 &&
add(4) === 5 && "a" in object && typeof /x/g === "object";
`)
	if err != nil {
		t.Fatal(err)
	}
	result := execute(t, 829, image)
	if result.Kind() != memory.ValueBool || !result.Bool() {
		t.Fatalf("React control-flow result = %#v, want true", result)
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

func TestCompileRejectsSemanticWorkOutsideN7(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		"missing;",
		"const fixed = 1; fixed = 2;",
		"break;",
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

func TestCompileWithOptionsDefersUnknownGlobalResolutionToRuntime(t *testing.T) {
	t.Parallel()

	image, err := compiler.CompileWithOptions("earlierBinding + 2;", compiler.Options{AllowUnresolvedGlobals: true})
	if err != nil {
		t.Fatal(err)
	}
	if image.FunctionCount() != 1 {
		t.Fatalf("function count = %d, want 1", image.FunctionCount())
	}
	if _, err := compiler.Compile("earlierBinding + 2;"); !errors.Is(err, compiler.ErrCompile) {
		t.Fatalf("closed-world Compile error = %v", err)
	}
}

func TestCompileUsesPropertyReferencesAndMethodReceivers(t *testing.T) {
	t.Parallel()

	image, err := compiler.Compile(`
function Box() {
  this.count = 1;
  this.bump = function(delta) {
    this.count = this.count + delta;
    return this.count;
  };
}
let box = new Box();
let calls = 0;
function choose() { calls++; return box; }
let old = choose()["count"]++;
let current = box["bump"](2);
box[0] = 5;
let values = [3];
let before = values["0"]++;
old * 1000 + current * 100 + calls * 10 + before + values[0] + box["0"] + values.length;
`)
	if err != nil {
		t.Fatal(err)
	}
	entry, _ := image.Function(image.Entry())
	disassembly, err := browserruntime.Disassemble(entry.Code)
	if err != nil {
		t.Fatal(err)
	}
	for _, opcode := range []string{"GetProperty", "SetProperty", "CallMethod", "UpdateProperty"} {
		if !strings.Contains(disassembly, opcode) {
			t.Fatalf("disassembly does not contain %s:\n%s", opcode, disassembly)
		}
	}
	result := execute(t, 820, image)
	if result.Kind() != memory.ValueNumber || result.Number() != 1423 {
		t.Fatalf("property result = %#v, want 1423", result)
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
function readInsideTDZ() {
  try {
    let value = 1;
    { value; let value = 2; }
  } catch (error) {
    return error.name === "ReferenceError";
  }
  return false;
}
readInsideTDZ() ? 1 : 0;
`)
	if err != nil {
		t.Fatal(err)
	}
	if result := execute(t, 819, image); result.Kind() != memory.ValueNumber || result.Number() != 1 {
		t.Fatalf("caught TDZ result = %#v, want 1", result)
	}

	if _, err := compiler.Compile("const missing;"); err == nil {
		t.Fatalf("const without initializer error = %v", err)
	}
}

func TestCompileExecutesPrimitiveCoercionAndLooseEquality(t *testing.T) {
	t.Parallel()

	image, err := compiler.Compile(`
function primitives() {
  let absent;
  let total = 0;
  if ("42" == 42) { total++; }
  if (false == 0) { total++; }
  if (null == absent) { total++; }
  if (("4" + 2) === "42") { total++; }
  if ("10" < "2") { total++; }
  let object = {
    amount: "5",
    valueOf: function() { return this.amount; }
  };
  if (object == 5) { total++; }
	if (object == "5") { total++; }
	object[true] = 1;
	total = total + object["true"];
  return total + +"40" + ("6" * 7) + +object;
}
primitives();
`)
	if err != nil {
		t.Fatal(err)
	}
	result := execute(t, 821, image)
	if result.Kind() != memory.ValueNumber || result.Number() != 95 {
		t.Fatalf("coercion result = %#v, want 95", result)
	}
}

func TestCompileCatchesNativeLanguageErrors(t *testing.T) {
	t.Parallel()

	image, err := compiler.Compile(`
function catchesTypeError() {
	let finalized = 0;
	try {
		try { return +{}; }
		finally { finalized = 1; }
	} catch (error) {
		return finalized === 1 && error.name === "TypeError" && error.message != "";
	}
}
function catchesReferenceError() {
  try { { value; let value = 1; } }
  catch (error) { return error.name === "ReferenceError"; }
  return false;
}
(catchesTypeError() && catchesReferenceError()) ? 1 : 0;
`)
	if err != nil {
		t.Fatal(err)
	}
	result := execute(t, 822, image)
	if result.Kind() != memory.ValueNumber || result.Number() != 1 {
		t.Fatalf("caught language errors = %#v, want 1", result)
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
