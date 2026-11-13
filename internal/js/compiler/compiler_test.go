package compiler_test

import (
	"context"
	"errors"
	"fmt"
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

func TestCompileFusesIdentifierUpdatesAndPreservesTheirResult(t *testing.T) {
	t.Parallel()

	image, err := compiler.Compile(`
let value = 4;
let postfixIncrement = value++;
let prefixIncrement = ++value;
let postfixDecrement = value--;
let prefixDecrement = --value;
postfixIncrement * 1000 + prefixIncrement * 100 + postfixDecrement * 10 + prefixDecrement;
`)
	if err != nil {
		t.Fatal(err)
	}
	entry, _ := image.Function(image.Entry())
	disassembly, err := browserruntime.Disassemble(entry.Code)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(disassembly, "UpdateBinding"); got != 4 {
		t.Fatalf("UpdateBinding count = %d, want 4:\n%s", got, disassembly)
	}
	if strings.Contains(disassembly, "Increment") || strings.Contains(disassembly, "Decrement") {
		t.Fatalf("identifier update was not fused:\n%s", disassembly)
	}
	result := execute(t, 811_001, image)
	if result.Kind() != memory.ValueNumber || result.Number() != 4664 {
		t.Fatalf("update result = %#v, want 4664", result)
	}
}

func TestFrameBindingCacheTracksTheActiveLexicalEnvironment(t *testing.T) {
	t.Parallel()

	image, err := compiler.Compile(`
let value = 1;
let inner = 0;
{
  let value = 10;
  value = value + 1;
  inner = value;
}
value * 100 + inner;
`)
	if err != nil {
		t.Fatal(err)
	}
	result := execute(t, 811_002, image)
	if result.Kind() != memory.ValueNumber || result.Number() != 111 {
		t.Fatalf("scoped binding result = %#v, want 111", result)
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

func TestCompileLowersForOfThroughIteratorProtocol(t *testing.T) {
	t.Parallel()

	image, err := compiler.Compile(`
let total = 0;
for (let value of [1, 2, 3]) {
  total = total + value;
  if (total === 3) break;
}
total;
`)
	if err != nil {
		t.Fatal(err)
	}
	entry, _ := image.Function(image.Entry())
	disassembly, err := browserruntime.Disassemble(entry.Code)
	if err != nil {
		t.Fatal(err)
	}
	for _, opcode := range []string{"GetIterator", "IteratorNext", "IteratorClose", "EnterTry", "EndFinally"} {
		if !strings.Contains(disassembly, opcode) {
			t.Fatalf("for-of disassembly does not contain %s:\n%s", opcode, disassembly)
		}
	}
	result := execute(t, 830, image)
	if result.Kind() != memory.ValueNumber || result.Number() != 3 {
		t.Fatalf("for-of result = %#v, want 3", result)
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

func TestCompileStartsAllFunctionsOnStackLocals(t *testing.T) {
	t.Parallel()

	image, err := compiler.Compile(`
function scoped(value) {
  let result = value;
  {
    let value = 10;
    result = result + value;
  }
  return result + value;
}
function make(value) {
  return () => value;
}
scoped(2) + make(3)();
`)
	if err != nil {
		t.Fatal(err)
	}
	scoped, _ := image.Function(1)
	scopedCode, err := browserruntime.Disassemble(scoped.Code)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(scopedCode, "EnterStackFunction") || strings.Contains(scopedCode, "EnterFunction ") {
		t.Fatalf("closure-free function does not use stack locals:\n%s", scopedCode)
	}
	make, _ := image.Function(2)
	makeCode, err := browserruntime.Disassemble(make.Code)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(makeCode, "EnterStackFunction") || strings.Contains(makeCode, "EnterFunction ") {
		t.Fatalf("capturing function does not start on stack locals:\n%s", makeCode)
	}
	result := execute(t, 814_001, image)
	if result.Kind() != memory.ValueNumber || result.Number() != 17 {
		t.Fatalf("stack-local result = %#v, want 17", result)
	}
}

func TestClosureMaterializationPreservesNestedScopesAndLateInitialization(t *testing.T) {
	t.Parallel()

	image, err := compiler.Compile(`
function nested() {
  let outer = 1;
  {
    let inner = 2;
    let read = () => outer + inner;
    inner = 3;
    return read;
  }
}
function late() {
  let read = () => later;
  let later = 9;
  return read;
}
nested()() + late()();
`)
	if err != nil {
		t.Fatal(err)
	}
	result := execute(t, 814_004, image)
	if result.Kind() != memory.ValueNumber || result.Number() != 13 {
		t.Fatalf("materialized closure result = %#v, want 13", result)
	}
}

func TestUntakenClosurePathDoesNotAllocateLexicalContext(t *testing.T) {
	t.Parallel()

	image, err := compiler.Compile(`
function maybe(flag) {
  let value = 7;
  if (flag) return () => value;
  return value;
}
undefined;
`)
	if err != nil {
		t.Fatal(err)
	}
	realm, err := browserruntime.NewRealm(814_005, nil)
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
		before := task.Realm.Store().Stats().LiveContexts
		value, err := interpreter.Execute(task, loaded.Functions[1], memory.BoolValue(false))
		if err != nil {
			return err
		}
		if value.Kind() != memory.ValueNumber || value.Number() != 7 {
			return fmt.Errorf("untaken closure result = %#v, want 7", value)
		}
		if after := task.Realm.Store().Stats().LiveContexts; after != before {
			return fmt.Errorf("untaken closure live Contexts = %d, want %d", after, before)
		}
		closure, err := interpreter.Execute(task, loaded.Functions[1], memory.BoolValue(true))
		if err != nil {
			return err
		}
		if after := task.Realm.Store().Stats().LiveContexts; after != before+1 {
			return fmt.Errorf("taken closure live Contexts = %d, want %d", after, before+1)
		}
		result, err := interpreter.Execute(task, closure.Ref())
		if err != nil {
			return err
		}
		if result.Kind() != memory.ValueNumber || result.Number() != 7 {
			return fmt.Errorf("materialized closure call = %#v, want 7", result)
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

func TestClosureFreeCallsDoNotAllocateLexicalContexts(t *testing.T) {
	t.Parallel()

	image, err := compiler.Compile(`
function sum(value) {
  let doubled = value * 2;
  { const one = 1; doubled = doubled + one; }
  return doubled;
}
undefined;
`)
	if err != nil {
		t.Fatal(err)
	}
	realm, err := browserruntime.NewRealm(814_002, nil)
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
		before := task.Realm.Store().Stats().LiveContexts
		for index := 0; index < 100; index++ {
			result, err := interpreter.Execute(task, loaded.Functions[1], memory.NumberValue(float64(index)))
			if err != nil {
				return err
			}
			if result.Kind() != memory.ValueNumber || result.Number() != float64(index*2+1) {
				return fmt.Errorf("result %d = %#v, want %d", index, result, index*2+1)
			}
		}
		if after := task.Realm.Store().Stats().LiveContexts; after != before {
			return fmt.Errorf("live Contexts = %d after calls, want %d", after, before)
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

func TestIterativeBytecodeStackRunsDeepCallsAndRoutesThrows(t *testing.T) {
	t.Parallel()

	image, err := compiler.Compile(`
function descend(value) {
  if (value === 0) return 0;
  return descend(value - 1) + 1;
}
function leaf() { throw 7; }
function middle() { return leaf(); }
function catchDeep() {
  try { return middle(); } catch (error) { return error + 1; }
}
descend(1500) + catchDeep();
`)
	if err != nil {
		t.Fatal(err)
	}
	result := executeWithConfig(t, 814_003, image, browserruntime.InterpreterConfig{MaxCallDepth: 2048})
	if result.Kind() != memory.ValueNumber || result.Number() != 1508 {
		t.Fatalf("iterative call result = %#v, want 1508", result)
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

func TestCompileLowersAsyncReturnAwaitToPromiseChains(t *testing.T) {
	t.Parallel()

	if _, err := compiler.Compile(`
async function compute(value) {
  return (await Promise.resolve(value + 1)) * 2;
}
const named = async function named(value) { return await compute(value); };
compute(2).then(value => value);
`); err != nil {
		t.Fatal(err)
	}
	if _, err := compiler.Compile(`
async function load(fetcher) {
  const response = await fetcher();
  if (!response.ok) throw new Error(await response.text());
  const payload = await response.json();
  return payload.value;
}
async function readError(response) {
  let message;
  try { message = (await response.json()).error; } catch {}
  throw new Error(message);
}
async function choose(response) {
  return response.ok ? await response.json() : {available: false};
}
async function objectResult(response) {
  return {value: (await response.json()).value, loaded: true};
}
async function release(run) {
  let active = true;
  try { await run(); } finally { active = false; }
  return active;
}
async function syncAll(values, send) {
  for (const value of values) {
    if (!value) continue;
    await send(value);
    if (value === "stop") break;
  }
  return values.length;
}
async function page(load) {
  let cursor = 0;
  for (; cursor < 3; cursor++) {
    const result = await load(cursor);
    if (!result) break;
  }
  return cursor;
}
async function write(open, send) {
  for (await open(); await send(); await send()) break;
}
async function dispatch(message, handle) {
  switch (message.type) {
    case "one": await handle(message); break;
    case "two": await handle(message); break;
    default: break;
  }
  return true;
}
async function catchUp(run, pending) {
  do { await run(); if (!pending()) break; } while (pending());
  return true;
}
`); err != nil {
		t.Fatalf("multi-statement async Function error = %v", err)
	}
}

func TestCompileLowersArrowDefaultParameters(t *testing.T) {
	t.Parallel()

	image, err := compiler.Compile(`
const apply = (input, mapper = value => value * 2, state = (mapper.tag || (mapper.tag = 3))) =>
  mapper(input) + state;
let missing;
(apply(2) === 7 && apply(3, missing, 4) === 10) ? 1 : 0;
`)
	if err != nil {
		t.Fatal(err)
	}
	result := execute(t, 823, image)
	if result.Kind() != memory.ValueNumber || result.Number() != 1 {
		t.Fatalf("arrow defaults = %#v, want 1", result)
	}
}

func TestCompileArrowFunctionsCaptureLexicalThis(t *testing.T) {
	t.Parallel()

	image, err := compiler.Compile(`
function makeArrow() {
  return () => this.value;
}
let first = {value: 7, makeArrow: makeArrow};
let second = {value: 11};
let arrow = first.makeArrow();
second.invoke = arrow;
let nonConstructible = false;
try { new arrow(); } catch (_) { nonConstructible = true; }
arrow() === 7 && second.invoke() === 7 && nonConstructible;
`)
	if err != nil {
		t.Fatal(err)
	}
	result := execute(t, 824, image)
	if result.Kind() != memory.ValueBool || !result.Bool() {
		t.Fatalf("arrow lexical this result = %#v, want true", result)
	}
}

func TestCompileExecutesOptionalChainsOnceAndPreservesReceivers(t *testing.T) {
	t.Parallel()

	image, err := compiler.Compile(`
let reads = 0;
let argumentsRead = 0;
function source(value) { reads++; return value; }
let object = {value: 4, add: function(value) { return this.value + value; }};
let absent = null;
let callable = function(value) { return value + 1; };
let missing;
let first = source(object)?.value;
let second = source(absent)?.child.value;
let third = object?.add(argumentsRead++);
let fourth = absent?.add(argumentsRead++);
let fifth = callable?.(4);
let sixth = missing?.(4);
let seventh = object.add?.(3);
let eighth = object.missing?.(argumentsRead++);
(first === 4 && typeof second === "undefined" && third === 4 && typeof fourth === "undefined" &&
 fifth === 5 && typeof sixth === "undefined" && seventh === 7 && typeof eighth === "undefined" &&
 reads === 2 && argumentsRead === 1) ? 1 : 0;
`)
	if err != nil {
		t.Fatal(err)
	}
	result := executeBootstrapped(t, 824, image)
	if result.Kind() != memory.ValueNumber || result.Number() != 1 {
		t.Fatalf("optional chains = %#v, want 1", result)
	}
}

func TestCompileExecutesPostfixMembersAfterConstruction(t *testing.T) {
	t.Parallel()

	image, err := compiler.Compile(`
function Box(value) { this.value = value; }
function Factory() { return function(value) { return {value: value + 1}; }; }
(new Box(4).value === 4 && new Factory()(4).value === 5) ? 1 : 0;
`)
	if err != nil {
		t.Fatal(err)
	}
	result := execute(t, 825, image)
	if result.Kind() != memory.ValueNumber || result.Number() != 1 {
		t.Fatalf("constructed postfix chain = %#v, want 1", result)
	}
}

func TestCompileExecutesObjectSpreadAndComputedPropertiesInOrder(t *testing.T) {
	t.Parallel()

	image, err := compiler.Compile(`
let reads = 0;
let source = {a: 1, get copied() { reads++; return 2; }};
let key = "chosen";
let result = {a: 0, ...source, [key]: 3, a: 4, ...null};
(result.a === 4 && result.copied === 2 && result.chosen === 3 && reads === 1) ? 1 : 0;
`)
	if err != nil {
		t.Fatal(err)
	}
	result := execute(t, 826, image)
	if result.Kind() != memory.ValueNumber || result.Number() != 1 {
		t.Fatalf("object spread/computed result = %#v, want 1", result)
	}
}

func TestCompileExecutesObjectBindingAliasesAndDefaults(t *testing.T) {
	t.Parallel()

	image, err := compiler.Compile(`
let missing;
let {first: renamed, second = renamed + 1, third = 9, nullable = 10} = {
  first: 4, second: missing, nullable: null
};
(renamed === 4 && second === 5 && third === 9 && nullable === null) ? 1 : 0;
`)
	if err != nil {
		t.Fatal(err)
	}
	result := execute(t, 827, image)
	if result.Kind() != memory.ValueNumber || result.Number() != 1 {
		t.Fatalf("object binding result = %#v, want 1", result)
	}
}

func TestCompileExecutesDestructuredFunctionParameters(t *testing.T) {
	t.Parallel()

	image, err := compiler.Compile(`
let missing;
function describe({task: value, state = "ready"}, [first], suffix = state) {
  return value + ":" + state + ":" + first + ":" + suffix;
}
const select = ({item}) => item;
(describe({task: "build", state: missing}, [3]) === "build:ready:3:ready" &&
 select({item: 9}) === 9) ? 1 : 0;
`)
	if err != nil {
		t.Fatal(err)
	}
	result := execute(t, 831, image)
	if result.Kind() != memory.ValueNumber || result.Number() != 1 {
		t.Fatalf("destructured parameter result = %#v, want 1", result)
	}
}

func TestCompileExecutesRecursiveBindingsAndAssignmentPatterns(t *testing.T) {
	t.Parallel()

	image, err := compiler.Compile(`
let missing;
const source = {signal: [2, 3], options: {retry: missing}, extra: 9};
const {signal: [first, second], options: {retry = 4}, ...rest} = source;
let a = 0, b = 0, tail = [], copied;
[a, b, ...tail] = [5, 6, 7, 8];
({extra: a, ...copied} = source);
(first === 2 && second === 3 && retry === 4 && rest.extra === 9 &&
 a === 9 && b === 6 && tail.length === 2 && copied.signal[0] === 2) ? 1 : 0;
`)
	if err != nil {
		t.Fatal(err)
	}
	result := executeBootstrapped(t, 844, image)
	if result.Kind() != memory.ValueNumber || result.Number() != 1 {
		t.Fatalf("recursive binding result = %#v, want 1", result)
	}
}

func executeBootstrapped(t *testing.T, realmID browserruntime.RealmID, image program.Program) memory.Value {
	t.Helper()
	realm, err := browserruntime.NewRealm(realmID, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer realm.Close()
	interpreter := browserruntime.NewInterpreter(browserruntime.InterpreterConfig{})
	var result memory.Value
	_, err = realm.EnqueueTask(func(task *browserruntime.TaskContext) error {
		intrinsics, err := interpreter.Bootstrap(task)
		if err != nil {
			return err
		}
		loaded, err := program.Load(task, image, memory.RefValue(intrinsics.Global))
		if err != nil {
			return err
		}
		result, err = interpreter.Execute(task, loaded.Entry)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestCompileLowersLazyForOfGenerator(t *testing.T) {
	t.Parallel()

	if _, err := compiler.Compile(`
function* matching(values, minimum) {
  for (let value of values) value > minimum && (yield value);
}
[...matching([1, 2, 3], 1)].join(":");
`); err != nil {
		t.Fatal(err)
	}
	if _, err := compiler.Compile(`function* unsupported() { yield 1; }`); !errors.Is(err, compiler.ErrCompile) {
		t.Fatalf("unsupported generator error = %v", err)
	}
}

func TestCompileAsyncSequentialBranchingUsesSharedJoinContinuation(t *testing.T) {
	t.Parallel()

	var source strings.Builder
	source.WriteString(`
let settled = 0;
let earlyResult = 0;
let switchResult = 0;
async function sequential(flag) {
  let acc = 0;
`)
	for index := 0; index < 24; index++ {
		fmt.Fprintf(&source, "  if (flag) { acc += await Promise.resolve(1); } else { acc += await Promise.resolve(2); }\n")
	}
	source.WriteString(`
  try {
    acc += await Promise.resolve(3);
  } catch (err) {
    acc += await Promise.resolve(100);
  }
  return acc;
}
async function earlyReturn(takeEarly) {
  let acc = 10;
  if (takeEarly) {
    return await Promise.resolve(acc + 5);
  }
  acc += await Promise.resolve(20);
  return acc;
}
async function switchWithNestedBreak(mode) {
  let acc = 1;
  switch (mode) {
    case "skip":
      if (acc === 1) {
        break;
      }
      acc += await Promise.resolve(50);
      break;
    default:
      acc += await Promise.resolve(5);
  }
  acc += await Promise.resolve(7);
  return acc;
}
sequential(true).then((v) => { settled = v; });
earlyReturn(true).then((v) => { earlyResult = v; });
switchWithNestedBreak("skip").then((v) => { switchResult = v; });
1;
`)
	image, err := compiler.Compile(source.String())
	if err != nil {
		t.Fatal(err)
	}
	if image.FunctionCount() > 260 {
		t.Fatalf("compiled async function count = %d, want linear bound <= 260", image.FunctionCount())
	}

	checkImage, err := compiler.Compile(source.String() + `
async function verify() {
  const a = await sequential(true);
  const b = await earlyReturn(true);
  const c = await earlyReturn(false);
  const d = await switchWithNestedBreak("skip");
  if (a !== 27 || b !== 15 || c !== 30 || d !== 8) {
    throw new Error("async continuation mismatch: " + [a, b, c, d].join(","));
  }
}
verify();
1;
`)
	if err != nil {
		t.Fatal(err)
	}
	result := executeBootstrapped(t, 845, checkImage)
	if result.Kind() != memory.ValueNumber || result.Number() != 1 {
		t.Fatalf("async continuation result = %#v, want 1", result)
	}
}

func TestNumericPropertyAccessAndUTF8StringOperationsAvoidIndexStringAllocations(t *testing.T) {
	t.Parallel()

	image, err := compiler.CompileWithOptions(`
const label = "g\u00f6ssamer-\u03bb";
if (label.length !== 10 || label[1] !== "\u00f6" || label.charAt(9) !== "\u03bb" ||
    label.at(-1) !== "\u03bb" || label.slice(1, 4) !== "\u00f6ss" ||
    label.substring(4, 1) !== "\u00f6ss" || !label.startsWith("ss", 2) ||
    !label.endsWith("g\u00f6ss", 4) || !label.includes("mer", 5) ||
    label.indexOf("\u03bb", 2) !== 9 || Object.assign({}, "a\u00f6")[1] !== "\u00f6") {
  throw new Error("UTF-8 string method mismatch");
}
const numbers = [1, 2, 3, 4];
const ascii = "abcd";
function runHotLoop() {
  let total = 0;
  for (let round = 0; round < 100; round++) {
    for (let i = 0; i < numbers.length; i++) {
      bytes[i] = numbers[i];
      total += bytes[i] + ascii.length + ascii.slice(0, 4).length;
    }
  }
  return total;
}
runHotLoop();
`, compiler.Options{AllowUnresolvedGlobals: true})
	if err != nil {
		t.Fatal(err)
	}
	realm, err := browserruntime.NewRealm(846, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer realm.Close()
	interpreter := browserruntime.NewInterpreter(browserruntime.InterpreterConfig{})
	_, err = realm.EnqueueTask(func(task *browserruntime.TaskContext) error {
		intrinsics, err := interpreter.Bootstrap(task)
		if err != nil {
			return err
		}
		bufferRef, err := task.NewArrayBuffer(make([]byte, 4))
		if err != nil {
			return err
		}
		typedRef, err := task.NewTypedArray(bufferRef, memory.ElementUint8, 0, 4)
		if err != nil {
			return err
		}
		bytesKey, err := task.NewString("bytes")
		if err != nil {
			return err
		}
		if err := task.DeclareBinding(intrinsics.Global, bytesKey, false); err != nil {
			return err
		}
		if err := task.InitializeBinding(intrinsics.Global, bytesKey, memory.RefValue(typedRef)); err != nil {
			return err
		}
		loaded, err := program.Load(task, image, memory.RefValue(intrinsics.Global))
		if err != nil {
			return err
		}
		before := task.Realm.Store().Stats().LiveStrings
		value, err := interpreter.Execute(task, loaded.Entry)
		if err != nil {
			return err
		}
		if value.Kind() != memory.ValueNumber || value.Number() != 4200 {
			return fmt.Errorf("hot loop total = %#v, want 4200", value)
		}
		after := task.Realm.Store().Stats().LiveStrings
		// Only the constants/results prior to the loop plus at most a handful
		// of strings should exist; 400 indexed array/typed-array accesses must
		// not allocate 800 numeric index strings.
		if after-before > 20 {
			return fmt.Errorf("LiveStrings grew by %d across 400 numeric index accesses (before=%d after=%d)", after-before, before, after)
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

func TestStatementHeaderRegExpSwitchLexicalScopeDateParsePromiseAllSettledAndProxyArrayIteration(t *testing.T) {
	t.Parallel()

	image, err := compiler.Compile(`
let matched = 0;
for (const key of ["jwt", "other", "Token"]) /^(?:jwt|token)$/i.test(key) && (matched += 1);
if (matched === 2) /^ok$/.test("ok") && (matched += 10);
let loops = 0;
while (loops < 1) /^loop$/.test("loop") && (loops += 1);

function classify(value) {
  switch (typeof value) {
    case "string":
      const upper = value.toUpperCase(), len = upper.length;
      return upper + ":" + len;
    case "number":
      let doubled = value * 2;
      return "num:" + doubled;
    default:
      return "other";
  }
}

const epochZero = Date.parse("1970-01-01T00:00:00Z");
const parsedDate = new Date("2020-01-02T03:04:05Z").getTime();
const invalidDate = Number.isNaN(Date.parse("not-a-date"));

const backing = ["a", "b"];
const proxyArray = new Proxy(backing, {});
proxyArray.push("c");
let joined = "";
for (const item of proxyArray) {
  joined += item;
}
const spreadCopy = [...proxyArray];
const proxyChecks = [
  joined === "abc",
  spreadCopy.join("-") === "a-b-c",
  proxyArray.includes("b"),
  proxyArray.indexOf("c") === 2,
  proxyArray.slice(1).join("") === "bc"
].every(Boolean);

async function verify() {
  const settled = await Promise.allSettled([Promise.resolve("ok"), Promise.reject("bad"), 7]);
  const settledSummary = settled.map(entry => entry.status + ":" + (entry.value ?? entry.reason)).join(",");
  const raceWinner = await Promise.race([Promise.resolve("first"), Promise.reject("second")]);
  const ok = [
    matched === 12,
    loops === 1,
    classify("hi") === "HI:2",
    classify(5) === "num:10",
    epochZero === 0,
    parsedDate === 1577934245000,
    invalidDate,
    proxyChecks,
    settledSummary === "fulfilled:ok,rejected:bad,fulfilled:7",
    raceWinner === "first"
  ].every(Boolean);
  if (!ok) {
    throw new Error("efchat compatibility checks failed");
  }
}
verify();
1;
`)
	if err != nil {
		t.Fatal(err)
	}
	result := executeBootstrapped(t, 847, image)
	if result.Kind() != memory.ValueNumber || result.Number() != 1 {
		t.Fatalf("efchat compatibility result = %#v, want 1", result)
	}
}

func TestCompileLowersAsyncTryFinallyEarlyReturnWithContinuation(t *testing.T) {
	t.Parallel()

	image, err := compiler.Compile(`
let log = [];
async function flow(mode) {
  try {
    const first = await Promise.resolve("a");
    log.push("try:" + first);
    if (mode === "early-try") {
      return await Promise.resolve("early-try-result");
    }
    if (mode === "empty-return") {
      return;
    }
    if (mode === "throw-catch" || mode === "throw-uncaught") {
      throw new Error("boom");
    }
    log.push("try-end");
  } catch (err) {
    log.push("catch:" + err.message);
    if (mode === "throw-catch") {
      return "caught";
    }
    throw err;
  } finally {
    log.push("finally");
  }
  log.push("after");
  return await Promise.resolve("fell-through");
}
const out = { result: "" };
(async () => {
  const a = await flow("early-try");
  const b = await flow("empty-return");
  const c = await flow("fallthrough");
  const d = await flow("throw-catch");
  let e = "no-throw";
  try { await flow("throw-uncaught"); } catch (err) { e = err.message; }
  out.result = [a, String(b), c, d, e, log.join(",")].join("|");
})();
out;
`)
	if err != nil {
		t.Fatal(err)
	}
	realm, err := browserruntime.NewRealm(848, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer realm.Close()
	interpreter := browserruntime.NewInterpreter(browserruntime.InterpreterConfig{})
	var check string
	_, err = realm.EnqueueTask(func(task *browserruntime.TaskContext) error {
		intrinsics, err := interpreter.Bootstrap(task)
		if err != nil {
			return err
		}
		loaded, err := program.Load(task, image, memory.RefValue(intrinsics.Global))
		if err != nil {
			return err
		}
		outVal, err := interpreter.Execute(task, loaded.Entry)
		if err != nil {
			return err
		}
		key, err := task.NewString("result")
		if err != nil {
			return err
		}
		value, found, err := task.GetOwnProperty(outVal.Ref(), key)
		if err != nil || !found || !value.IsRef() {
			return fmt.Errorf("missing __result: found=%v err=%v", found, err)
		}
		check, err = task.DerefString(value.Ref())
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := "early-try-result|undefined|fell-through|caught|boom|" +
		"try:a,finally," +
		"try:a,finally," +
		"try:a,try-end,finally,after," +
		"try:a,catch:boom,finally," +
		"try:a,catch:boom,finally"
	if check != want {
		t.Fatalf("async try/finally early return trace =\n  %q\nwant:\n  %q", check, want)
	}
}

func execute(t *testing.T, realmID browserruntime.RealmID, image program.Program) memory.Value {
	return executeWithConfig(t, realmID, image, browserruntime.InterpreterConfig{})
}

func executeWithConfig(t *testing.T, realmID browserruntime.RealmID, image program.Program, config browserruntime.InterpreterConfig) memory.Value {
	t.Helper()
	realm, err := browserruntime.NewRealm(realmID, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer realm.Close()
	interpreter := browserruntime.NewInterpreter(config)
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
			if thrown, ok := browserruntime.ThrownValue(err); ok && thrown.IsRef() {
				if object, derefErr := task.DerefError(thrown.Ref()); derefErr == nil {
					if message, messageErr := task.DerefString(object.Message.Ref()); messageErr == nil {
						return fmt.Errorf("%w (%s: %s)", err, object.Kind.Name(), message)
					}
				}
			}
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
