package memory_test

import (
	"errors"
	"testing"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func TestNativeFunctionOwnsCodeAndCapturedReferences(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(50)
	functionRegion := mustRegion(t, store, owner)
	captureRegion := mustRegion(t, store, owner)
	name, _ := store.AllocString(owner, captureRegion, "sum")
	environment, _ := store.AllocContext(owner, captureRegion, memory.NullValue())
	constant, _ := store.AllocObject(owner, captureRegion)
	code := []byte{1, 2, 3, 4}
	function, err := store.AllocBytecodeFunction(
		owner,
		functionRegion,
		memory.RefValue(name),
		memory.RefValue(environment),
		2,
		code,
		[]memory.Value{memory.RefValue(name), memory.RefValue(constant), memory.NumberValue(9)},
	)
	if err != nil {
		t.Fatal(err)
	}
	code[0] = 99
	snapshot, err := store.DerefFunction(owner, function)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Kind != memory.FunctionBytecode || snapshot.Arity != 2 || snapshot.Code[0] != 1 || len(snapshot.Constants) != 3 {
		t.Fatalf("DerefFunction() = %#v", snapshot)
	}
	snapshot.Code[0] = 88
	if reread, _ := store.DerefFunction(owner, function); reread.Code[0] != 1 {
		t.Fatal("DerefFunction exposed mutable code storage")
	}
	if got := store.EdgeCount(functionRegion, captureRegion); got != 4 {
		t.Fatalf("Function edge count = %d, want name, environment, and two constants", got)
	}
	before := store.Stats()
	if _, err := store.AllocBytecodeFunction(owner, functionRegion, memory.RefValue(constant), memory.NullValue(), 0, nil, nil); !errors.Is(err, memory.ErrTypeMismatch) {
		t.Fatalf("non-String name error = %v, want ErrTypeMismatch", err)
	}
	if after := store.Stats(); after.LiveSlots != before.LiveSlots || after.LiveFunctions != before.LiveFunctions {
		t.Fatalf("failed Function allocation leaked: before=%#v after=%#v", before, after)
	}
	if _, err := store.AllocNativeFunction(owner, functionRegion, memory.NullValue(), memory.NullValue(), 0, 0); !errors.Is(err, memory.ErrInvalidFunction) {
		t.Fatalf("zero native ID error = %v, want ErrInvalidFunction", err)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func TestNativeFunctionPromotionPreservesClosureAndAliases(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(51)
	reader := realmOwner(52)
	region := mustRegion(t, store, owner)
	name, _ := store.AllocString(owner, region, "closure")
	bindingName, _ := store.AllocString(owner, region, "captured")
	captured, _ := store.AllocString(owner, region, "payload")
	environment, _ := store.AllocContext(owner, region, memory.NullValue())
	if err := store.DeclareBinding(owner, environment, bindingName, false); err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeBinding(owner, environment, bindingName, memory.RefValue(captured)); err != nil {
		t.Fatal(err)
	}
	function, err := store.AllocBytecodeFunction(owner, region, memory.RefValue(name), memory.RefValue(environment), 0, []byte{0xaa}, []memory.Value{memory.RefValue(captured), memory.RefValue(captured)})
	if err != nil {
		t.Fatal(err)
	}

	promoted, err := store.Promote(owner, function)
	if err != nil {
		t.Fatal(err)
	}
	copyFunction, err := store.DerefFunction(reader, promoted[0])
	if err != nil {
		t.Fatal(err)
	}
	if copyFunction.Environment.Ref() == environment || copyFunction.Constants[0].Ref() == captured {
		t.Fatal("promotion reused source closure references")
	}
	if copyFunction.Constants[0].Ref() != copyFunction.Constants[1].Ref() {
		t.Fatal("promotion did not preserve constant alias")
	}
	copyEnvironment := copyFunction.Environment.Ref()
	copyContext, err := store.DerefContext(reader, copyEnvironment)
	if err != nil || len(copyContext.Bindings) != 1 {
		t.Fatalf("promoted environment = %#v, %v", copyContext, err)
	}
	resolved, found, err := store.ResolveBinding(reader, copyEnvironment, copyContext.Bindings[0].Name)
	if err != nil || !found || resolved.Ref() != copyFunction.Constants[0].Ref() {
		t.Fatalf("promoted capture = %#v, %t, %v", resolved, found, err)
	}
	if err := store.ReleaseOwner(owner); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DerefFunction(owner, function); !errors.Is(err, memory.ErrStaleRef) {
		t.Fatalf("source Function after release = %v, want ErrStaleRef", err)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}
