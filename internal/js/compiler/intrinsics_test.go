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
