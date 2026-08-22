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

func TestBytecodeClosureSharesImmutableExecutableStorage(t *testing.T) {
	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(56)
	functionRegion := mustRegion(t, store, owner)
	valueRegion := mustRegion(t, store, owner)
	captured, err := store.AllocObject(owner, valueRegion)
	if err != nil {
		t.Fatal(err)
	}
	code := make([]byte, 1024)
	for index := range code {
		code[index] = byte(index)
	}
	template, err := store.AllocBytecodeFunction(owner, functionRegion, memory.NullValue(), memory.NullValue(), 0, code, []memory.Value{memory.RefValue(captured)})
	if err != nil {
		t.Fatal(err)
	}
	locations := []memory.SourceSpan{{Start: 1, End: 2}, {Start: 3, End: 5}}
	if err := store.SetFunctionLocations(owner, template, locations); err != nil {
		t.Fatal(err)
	}
	before := store.PhysicalStats()
	closure, err := store.AllocBytecodeClosure(owner, functionRegion, template, memory.NullValue())
	if err != nil {
		t.Fatal(err)
	}
	after := store.PhysicalStats()
	if delta := after.PayloadBytes - before.PayloadBytes; delta != after.SlotPayloadSizeBytes {
		t.Fatalf("closure payload grew by %d bytes, want only its %d-byte slot payload", delta, after.SlotPayloadSizeBytes)
	}
	if got := store.EdgeCount(functionRegion, valueRegion); got != 2 {
		t.Fatalf("template and closure edge count = %d, want 2", got)
	}
	loaded, err := store.LoadFunction(owner, closure)
	if err != nil || len(loaded.Code) != len(code) || len(loaded.Locations) != len(locations) || len(loaded.Constants) != 1 || loaded.Constants[0].Ref() != captured {
		t.Fatalf("LoadFunction(closure) = %#v, %v", loaded, err)
	}
	if allocations := testing.AllocsPerRun(100, func() {
		if _, err := store.LoadFunction(owner, closure); err != nil {
			t.Fatal(err)
		}
	}); allocations != 0 {
		t.Fatalf("LoadFunction allocated %.2f objects per read", allocations)
	}
	if err := store.Free(owner, template); err != nil {
		t.Fatal(err)
	}
	if got := store.EdgeCount(functionRegion, valueRegion); got != 1 {
		t.Fatalf("closure edge count after template free = %d, want 1", got)
	}
	snapshot, err := store.DerefFunction(owner, closure)
	if err != nil || snapshot.Code[513] != code[513] || snapshot.Locations[1] != locations[1] {
		t.Fatalf("closure after template free = %#v, %v", snapshot, err)
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

func TestBoundNativeFunctionCapturesParticipateInGraphCopy(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(52)
	functionRegion := mustRegion(t, store, owner)
	valueRegion := mustRegion(t, store, owner)
	value, _ := store.AllocObject(owner, valueRegion)
	function, err := store.AllocBoundNativeFunction(
		owner, functionRegion, memory.NullValue(), memory.NullValue(), 1, 99,
		[]memory.Value{memory.RefValue(value), memory.RefValue(value)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := store.EdgeCount(functionRegion, valueRegion); got != 2 {
		t.Fatalf("capture edge count = %d, want 2", got)
	}
	reader := realmOwner(53)
	copied, err := store.Copy(owner, reader, function)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.DerefFunction(reader, copied[0])
	if err != nil || len(snapshot.Captures) != 2 || snapshot.Captures[0] != snapshot.Captures[1] || snapshot.Captures[0].Ref() == value {
		t.Fatalf("copied captures = %#v, %v", snapshot.Captures, err)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func TestArrowFunctionLexicalThisParticipatesInGraphCopy(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(54)
	functionRegion := mustRegion(t, store, owner)
	receiverRegion := mustRegion(t, store, owner)
	receiver, _ := store.AllocObject(owner, receiverRegion)
	function, err := store.AllocArrowBytecodeFunction(
		owner,
		functionRegion,
		memory.NullValue(),
		memory.NullValue(),
		memory.RefValue(receiver),
		0,
		[]byte{0xaa},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := store.EdgeCount(functionRegion, receiverRegion); got != 1 {
		t.Fatalf("lexical receiver edge count = %d, want 1", got)
	}
	snapshot, err := store.DerefFunction(owner, function)
	if err != nil || snapshot.ThisMode != memory.FunctionThisLexical || snapshot.Constructible || snapshot.LexicalThis.Ref() != receiver {
		t.Fatalf("arrow descriptor = %#v, %v", snapshot, err)
	}

	reader := realmOwner(55)
	copied, err := store.Copy(owner, reader, function)
	if err != nil {
		t.Fatal(err)
	}
	copyFunction, err := store.DerefFunction(reader, copied[0])
	if err != nil || copyFunction.ThisMode != memory.FunctionThisLexical || copyFunction.Constructible || !copyFunction.LexicalThis.IsRef() || copyFunction.LexicalThis.Ref() == receiver {
		t.Fatalf("copied arrow descriptor = %#v, %v", copyFunction, err)
	}
	if _, err := store.DerefObjectHeader(reader, copyFunction.LexicalThis.Ref()); err != nil {
		t.Fatalf("copied lexical receiver = %v", err)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}
