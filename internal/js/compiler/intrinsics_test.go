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

total === 10 &&
mapped.join(",") === "2,4,6" && filtered.join(",") === "4,6" &&
values.includes(2) && values.indexOf(3) === 2 &&
map.size === 3 && map.get("b") === 2 && map.has("c") && mapEntry.join(":") === "a:1" &&
set.size === 3 && set.has(2) && setValue === 1 &&
String(42) === "42" && "  Gossamer  ".trim().toLowerCase() === "gossamer" &&
"a-b-c".split("-").join(":") === "a:b:c" &&
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
