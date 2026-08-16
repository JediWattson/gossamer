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
