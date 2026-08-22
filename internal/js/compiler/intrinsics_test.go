package compiler_test

import (
	"context"
	"testing"

	"github.com/JediWattson/gossamer/internal/js/compiler"
	"github.com/JediWattson/gossamer/internal/js/program"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func TestN8IntrinsicsConstructorsAndEssentialBuiltins(t *testing.T) {
	t.Parallel()

	image, err := compiler.Compile(`
function Box(value) { this.value = value; }
Box.prototype.read = function() { return this.value; };

let box = new Box(42);
let inherited = Object.create(Box.prototype);
inherited.value = 7;

let descriptorTarget = {};
Object.defineProperty(descriptorTarget, "hidden", {
  value: 9,
  writable: false,
  enumerable: false,
  configurable: false
});

let values = [1, 2];
let pushed = values.push(3);
let popped = values.pop();
let sliced = values.slice(0, 2);

box.read() === 42 &&
inherited.read() === 7 &&
Object.getPrototypeOf(box) === Box.prototype &&
Object.getPrototypeOf(values) === Array.prototype &&
box instanceof Box && values instanceof Array &&
pushed === 3 && popped === 3 &&
values.join("-") === "1-2" && sliced.join(":") === "1:2" &&
Object.keys(descriptorTarget).length === 0 &&
Object.getOwnPropertyDescriptor(descriptorTarget, "hidden").writable === false;
`)
	if err != nil {
		t.Fatal(err)
	}

	realm, err := browserruntime.NewRealm(826, nil)
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
	if result.Kind() != memory.ValueBool || !result.Bool() {
		t.Fatalf("N8 result = %#v, want true", result)
	}
}

func TestN9StringsCollectionsAndIterators(t *testing.T) {
	t.Parallel()

	image, err := compiler.Compile(`
let total = 0;
let values = [1, 2, 3];
let mapped = values.map(function(value) { return value * 2; });
let filtered = mapped.filter(function(value) { return value > 2; });
filtered.forEach(function(value) { total = total + value; });

let map = new Map([["a", 1], ["b", 2]]);
map.set("c", 3);
let mapEntry = map.entries().next().value;

let set = new Set([1, 2, 2]);
set.add(3);
let setValue = set.values().next().value;

let stringIterator = "go".values();
let firstCharacter = stringIterator.next();
let secondCharacter = stringIterator.next();
let stringDone = stringIterator.next();
let searchPattern = /s+/g;
searchPattern.lastIndex = 4;
let searchIndex = "Gossamer".search(searchPattern);

total === 10 &&
mapped.join(",") === "2,4,6" && filtered.join(",") === "4,6" &&
values.includes(2) && values.indexOf(3) === 2 &&
map.size === 3 && map.get("b") === 2 && map.has("c") && mapEntry.join(":") === "a:1" &&
set.size === 3 && set.has(2) && setValue === 1 &&
String(42) === "42" && "  Gossamer  ".trim().toLowerCase() === "gossamer" &&
"a-b-c".split("-").join(":") === "a:b:c" &&
searchIndex === 2 && searchPattern.lastIndex === 4 &&
"a.b".search(".") === 0 && "plain".search() === 0 && "plain".search(/z/) === -1 &&
firstCharacter.value === "g" && !firstCharacter.done &&
secondCharacter.value === "o" && stringDone.done;
`)
	if err != nil {
		t.Fatal(err)
	}

	realm, err := browserruntime.NewRealm(827, nil)
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
	if result.Kind() != memory.ValueBool || !result.Bool() {
		t.Fatalf("N9 result = %#v, want true", result)
	}
}

func TestArrayFlatDepthAndIdentity(t *testing.T) {
	t.Parallel()

	image, err := compiler.Compile(`
let nested = [1, [2, [3, [4]]], 5];
let defaultDepth = nested.flat();
let doubleDepth = nested.flat(2);
let fullDepth = nested.flat(3);
let zeroDepth = nested.flat(0);
let mapped = [1, 2, 3].flatMap(function(value) { return [value, value * 10]; });

typeof Array.prototype.flat === "function" &&
defaultDepth.length === 4 && defaultDepth[0] === 1 && defaultDepth[1] === 2 &&
Array.isArray(defaultDepth[2]) && defaultDepth[3] === 5 &&
doubleDepth.length === 5 && Array.isArray(doubleDepth[3]) &&
fullDepth.join(",") === "1,2,3,4,5" &&
mapped.join(",") === "1,10,2,20,3,30" &&
zeroDepth.length === 3 && Array.isArray(zeroDepth[1]) &&
nested.length === 3 && Array.isArray(nested[1]);
`)
	if err != nil {
		t.Fatal(err)
	}

	realm, err := browserruntime.NewRealm(830, nil)
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
	if result.Kind() != memory.ValueBool || !result.Bool() {
		t.Fatalf("Array.flat result = %#v, want true", result)
	}
}

func TestNativeSymbolIntrinsicAndPropertyKeys(t *testing.T) {
	t.Parallel()

	image, err := compiler.Compile(`
let first = Symbol("token");
let second = Symbol("token");
let registered = Symbol.for("react.element");
let object = {};
object[first] = 1;
object[second] = 2;
object[Symbol.iterator] = function() { return "iterator"; };
let rejectedConstructor = false;
try { new Symbol("nope"); } catch (error) { rejectedConstructor = error instanceof TypeError; }

typeof Symbol === "function" && typeof first === "symbol" &&
first !== second && registered === Symbol.for("react.element") &&
Symbol.iterator === Symbol.iterator &&
first.description === "token" && String(first) === "Symbol(token)" &&
object[first] === 1 && object[second] === 2 && object[Symbol.iterator]() === "iterator" &&
Object.keys(object).length === 0 &&
Array.prototype[Symbol.iterator] === Array.prototype.values &&
"go"[Symbol.iterator]().next().value === "g" && rejectedConstructor;
`)
	if err != nil {
		t.Fatal(err)
	}

	realm, err := browserruntime.NewRealm(829, nil)
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
	if result.Kind() != memory.ValueBool || !result.Bool() {
		t.Fatalf("Symbol result = %#v, want true", result)
	}
}

func TestNativeObjectAndArrayBootstrapBuiltins(t *testing.T) {
	t.Parallel()

	image, err := compiler.Compile(`
let key = Symbol("copied");
let source = {visible: 7};
source[key] = 8;
Object.defineProperty(source, "hidden", {value: 9, enumerable: false});
let target = Object.assign({base: 6}, null, source, undefined);
let names = Object.getOwnPropertyNames(source);
let stringTarget = Object.assign({}, "go");

target.base === 6 && target.visible === 7 && target[key] === 8 &&
target.hidden === undefined &&
names.length === 2 && names[0] === "visible" && names[1] === "hidden" &&
source.hasOwnProperty("visible") && source.hasOwnProperty(key) &&
!source.hasOwnProperty("missing") &&
Array.isArray([]) && !Array.isArray({}) &&
Object.is(NaN, NaN) && !Object.is(0, -0) && Object.is(source, source) &&
stringTarget[0] === "g" && stringTarget[1] === "o" &&
typeof definitelyMissing === "undefined";
`)
	if err != nil {
		t.Fatal(err)
	}

	realm, err := browserruntime.NewRealm(830, nil)
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
	if result.Kind() != memory.ValueBool || !result.Bool() {
		t.Fatalf("Object/Array bootstrap result = %#v, want true", result)
	}
}

func TestNativeFunctionInvocationBuiltinsAndArguments(t *testing.T) {
	t.Parallel()

	image, err := compiler.Compile(`
function total(left, right) {
  return this.base + left + right + arguments.length;
}
let receiver = {base: 10};
let bound = total.bind(receiver, 5);
let argumentsResult = (function(first) { return arguments[1] + arguments.length; })(1, 7);

total.call(receiver, 1, 2) === 15 &&
total.apply(receiver, [3, 4]) === 19 &&
bound(6) === 23 && bound.length === 1 &&
argumentsResult === 9;
`)
	if err != nil {
		t.Fatal(err)
	}

	realm, err := browserruntime.NewRealm(831, nil)
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
	if result.Kind() != memory.ValueBool || !result.Bool() {
		t.Fatalf("Function invocation result = %#v, want true", result)
	}
}

func TestNativeNumericAndTimeBootstrapBuiltins(t *testing.T) {
	t.Parallel()

	image, err := compiler.Compile(`
let before = Date.now();
let random = Math.random();
let after = Date.now();
Math.clz32(0) === 32 && Math.clz32(1) === 31 &&
Math.floor(-1.2) === -2 && Math.log(1) === 0 &&
Math.min(4, -2, 9) === -2 && Math.min() === Infinity &&
Math.LN2 > 0.69 && Math.LN2 < 0.70 &&
random >= 0 && random < 1 && before <= after &&
isNaN("not-a-number") && !isNaN("42") &&
Number("42") === 42 && (255).toString(16) === "ff" &&
random.toString(36).slice(0, 2) === "0.";
`)
	if err != nil {
		t.Fatal(err)
	}

	realm, err := browserruntime.NewRealm(832, nil)
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
	if result.Kind() != memory.ValueBool || !result.Bool() {
		t.Fatalf("numeric bootstrap result = %#v, want true", result)
	}
}

func TestNativeRegExpBootstrapBuiltins(t *testing.T) {
	t.Parallel()

	image, err := compiler.Compile(`
let expression = RegExp("^go+$", "i");
let globalExpression = new RegExp("a", "g");
let firstMatch = globalExpression.test("ba");
let firstIndex = globalExpression.lastIndex;
let secondMatch = globalExpression.test("ba");

expression.test("GOO") && !expression.test("stop") &&
expression.source === "^go+$" && expression.flags === "i" &&
expression.toString() === "/^go+$/i" &&
firstMatch && firstIndex === 2 && !secondMatch && globalExpression.lastIndex === 0;
`)
	if err != nil {
		t.Fatal(err)
	}

	realm, err := browserruntime.NewRealm(833, nil)
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
	if result.Kind() != memory.ValueBool || !result.Bool() {
		t.Fatalf("RegExp bootstrap result = %#v, want true", result)
	}
}

func TestNativeWeakCollectionBuiltins(t *testing.T) {
	t.Parallel()

	image, err := compiler.Compile(`
let weakKey = {};
let weakOther = {};
let weakMap = new WeakMap();
let weakSet = new WeakSet();
let mapSetResult = weakMap.set(weakKey, 7);
let setAddResult = weakSet.add(weakKey);

mapSetResult === weakMap && setAddResult === weakSet &&
weakMap.get(weakKey) === 7 && weakMap.has(weakKey) && !weakMap.has(weakOther) &&
weakMap.get(1) === undefined && !weakMap.has(1) &&
weakSet.has(weakKey) && !weakSet.has(weakOther) && !weakSet.has(1) &&
weakMap.delete(weakKey) && !weakMap.has(weakKey) &&
weakSet.delete(weakKey) && !weakSet.has(weakKey);
`)
	if err != nil {
		t.Fatal(err)
	}

	realm, err := browserruntime.NewRealm(834, nil)
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
	if result.Kind() != memory.ValueBool || !result.Bool() {
		t.Fatalf("weak collection result = %#v, want true", result)
	}
}

func TestNativeArrayMutationAndConcatBuiltins(t *testing.T) {
	t.Parallel()

	image, err := compiler.Compile(`
let concatenated = [1, 2].concat([3, 4], 5);
let shifted = [1, 2];
let unshiftLength = shifted.unshift(0);
let first = shifted.shift();
let spliced = [1, 2, 3, 4];
let removed = spliced.splice(1, 2, 7, 8, 9);

concatenated.join(",") === "1,2,3,4,5" &&
unshiftLength === 3 && first === 0 && shifted.join(",") === "1,2" &&
removed.join(",") === "2,3" && spliced.join(",") === "1,7,8,9,4";
`)
	if err != nil {
		t.Fatal(err)
	}

	realm, err := browserruntime.NewRealm(835, nil)
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
	if result.Kind() != memory.ValueBool || !result.Bool() {
		t.Fatalf("Array mutation result = %#v, want true", result)
	}
}

func TestN10PromisesAndDeterministicMicrotaskCheckpoint(t *testing.T) {
	t.Parallel()

	image, err := compiler.Compile(`
let order = [];
queueMicrotask(function() {
  order.push(1);
  queueMicrotask(function() { order.push(4); });
});
queueMicrotask(function() { order.push(2); });

let chain = Promise.resolve(3).then(function(value) {
  order.push(value); return value + 1;
}).then(function(value) { order.push(value); return value * 10; });

let recovered = Promise.reject("bad").catch(function(reason) {
  order.push(reason); return 9;
});

let constructedBase = new Promise(function(resolve, reject) {
  resolve(5);
  resolve(6);
});
let constructed = constructedBase.then(function(value) { return value + 1; });

let thrown = Promise.resolve(1).then(function() {
  throw new TypeError("boom");
}).catch(function(error) { return error.name; });

let adopted = new Promise(function(resolve) { resolve(Promise.resolve(11)); });
let selfResolve = null;
let self = new Promise(function(resolve) { selfResolve = resolve; });
selfResolve(self);

[order, chain, recovered, constructed, thrown, adopted, self];
`)
	if err != nil {
		t.Fatal(err)
	}

	realm, err := browserruntime.NewRealm(828, nil)
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
		loaded, err := program.Load(task, image, memory.RefValue(intrinsics.Global))
		if err != nil {
			return err
		}
		result, err := interpreter.Execute(task, loaded.Entry)
		if err != nil {
			return err
		}
		outer, err := task.DerefArray(result.Ref())
		if err != nil {
			return err
		}
		order, err := task.DerefArray(outer.Elements[0].Value.Ref())
		if err != nil {
			return err
		}
		if order.Length != 6 {
			t.Fatalf("microtask order length = %d, want 6", order.Length)
		}
		wantNumbers := map[uint32]float64{0: 1, 1: 2, 2: 3, 4: 4, 5: 4}
		for index, want := range wantNumbers {
			value, found, err := task.ArrayElement(outer.Elements[0].Value.Ref(), index)
			if err != nil || !found || value.Number() != want {
				t.Fatalf("order[%d] = %#v, %t, %v; want %v", index, value, found, err, want)
			}
		}
		bad, found, err := task.ArrayElement(outer.Elements[0].Value.Ref(), 3)
		if err != nil || !found {
			return err
		}
		if text, err := task.DerefString(bad.Ref()); err != nil || text != "bad" {
			t.Fatalf("order[3] = %q, %v", text, err)
		}
		for index, want := range []float64{40, 9, 6} {
			promiseRef := outer.Elements[index+1].Value.Ref()
			promise, err := task.DerefPromise(promiseRef)
			if err != nil || promise.State != memory.PromiseFulfilled || promise.Result.Number() != want {
				t.Fatalf("promise %d = %#v, %v; want %v", index, promise, err, want)
			}
		}
		thrown, err := task.DerefPromise(outer.Elements[4].Value.Ref())
		if err != nil || thrown.State != memory.PromiseFulfilled {
			t.Fatalf("thrown chain = %#v, %v", thrown, err)
		}
		if text, err := task.DerefString(thrown.Result.Ref()); err != nil || text != "TypeError" {
			t.Fatalf("thrown result = %q, %v", text, err)
		}
		adopted, err := task.DerefPromise(outer.Elements[5].Value.Ref())
		if err != nil || adopted.State != memory.PromiseFulfilled || adopted.Result.Number() != 11 {
			t.Fatalf("adopted promise = %#v, %v", adopted, err)
		}
		self, err := task.DerefPromise(outer.Elements[6].Value.Ref())
		if err != nil || self.State != memory.PromiseRejected {
			t.Fatalf("self-resolved promise = %#v, %v", self, err)
		}
		selfError, err := task.DerefError(self.Result.Ref())
		if err != nil || selfError.Kind != memory.ErrorType {
			t.Fatalf("self-resolution reason = %#v, %v", selfError, err)
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
