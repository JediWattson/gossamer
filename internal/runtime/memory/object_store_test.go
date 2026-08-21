package memory_test

import (
	"errors"
	"testing"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func TestNativeObjectPropertiesAndPrototypeMaintainEdges(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(20)
	objectRegion := mustRegion(t, store, owner)
	valueRegion := mustRegion(t, store, owner)
	object, err := store.AllocObject(owner, objectRegion)
	if err != nil {
		t.Fatal(err)
	}
	prototype, err := store.AllocObject(owner, valueRegion)
	if err != nil {
		t.Fatal(err)
	}
	child, err := store.AllocObject(owner, valueRegion)
	if err != nil {
		t.Fatal(err)
	}
	name, err := store.AllocString(owner, valueRegion, "answer")
	if err != nil {
		t.Fatal(err)
	}
	equivalentName, err := store.AllocString(owner, valueRegion, "answer")
	if err != nil {
		t.Fatal(err)
	}

	if err := store.SetPrototype(owner, object, memory.RefValue(prototype)); err != nil {
		t.Fatal(err)
	}
	if err := store.SetProperty(owner, object, name, memory.RefValue(child)); err != nil {
		t.Fatal(err)
	}
	if got := store.EdgeCount(objectRegion, valueRegion); got != 3 {
		t.Fatalf("edge count after prototype and property = %d, want 3", got)
	}
	value, found, err := store.GetOwnProperty(owner, object, equivalentName)
	if err != nil || !found || !value.IsRef() || value.Ref() != child {
		t.Fatalf("GetOwnProperty(equal String) = %#v, %t, %v", value, found, err)
	}
	if err := store.SetProperty(owner, object, equivalentName, memory.NumberValue(42)); err != nil {
		t.Fatal(err)
	}
	if got := store.EdgeCount(objectRegion, valueRegion); got != 2 {
		t.Fatalf("edge count after value replacement = %d, want 2", got)
	}
	snapshot, err := store.DerefObject(owner, object)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Properties) != 1 || snapshot.Properties[0].Name != name {
		t.Fatalf("property insertion identity/order changed: %#v", snapshot.Properties)
	}
	deleted, err := store.DeleteProperty(owner, object, equivalentName)
	if err != nil || !deleted {
		t.Fatalf("DeleteProperty() = %t, %v", deleted, err)
	}
	if got := store.EdgeCount(objectRegion, valueRegion); got != 1 {
		t.Fatalf("edge count after delete = %d, want prototype only", got)
	}
	if err := store.SetPrototype(owner, object, memory.NullValue()); err != nil {
		t.Fatal(err)
	}
	if got := store.EdgeCount(objectRegion, valueRegion); got != 0 {
		t.Fatalf("edge count after clearing prototype = %d, want 0", got)
	}
	if err := store.Set(owner, object, 0, memory.NumberValue(1)); !errors.Is(err, memory.ErrTypeMismatch) {
		t.Fatalf("Cell Set(Object) error = %v, want ErrTypeMismatch", err)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func TestNativeObjectIntegrityFlagsSurviveCopy(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(28)
	reader := realmOwner(29)
	region := mustRegion(t, store, owner)
	object, _ := store.AllocObject(owner, region)
	existing, _ := store.AllocString(owner, region, "existing")
	extra, _ := store.AllocString(owner, region, "extra")
	prototype, _ := store.AllocObject(owner, region)
	if err := store.DefineProperty(owner, object, existing, memory.DataProperty(memory.NumberValue(1), true, true, false)); err != nil {
		t.Fatal(err)
	}
	if err := store.SetPrototype(owner, object, memory.NullValue()); err != nil {
		t.Fatal(err)
	}
	if err := store.SetObjectIntegrity(owner, object, true, true); err != nil {
		t.Fatal(err)
	}
	if err := store.SetPrototype(owner, object, memory.NullValue()); err != nil {
		t.Fatalf("setting the existing immutable prototype: %v", err)
	}
	if err := store.SetPrototype(owner, object, memory.RefValue(prototype)); !errors.Is(err, memory.ErrReadOnlyProperty) {
		t.Fatalf("changing immutable prototype error = %v", err)
	}
	if err := store.SetProperty(owner, object, extra, memory.NumberValue(2)); !errors.Is(err, memory.ErrReadOnlyProperty) {
		t.Fatalf("extending locked Object error = %v", err)
	}
	if err := store.SetProperty(owner, object, existing, memory.NumberValue(3)); err != nil {
		t.Fatalf("updating existing writable property: %v", err)
	}

	copied, err := store.Copy(owner, reader, object)
	if err != nil {
		t.Fatal(err)
	}
	copyHeader, err := store.DerefObjectHeader(reader, copied[0])
	if err != nil {
		t.Fatal(err)
	}
	if !copyHeader.NonExtensible || !copyHeader.ImmutablePrototype || copyHeader.Prototype.Kind() != memory.ValueNull {
		t.Fatalf("copied Object integrity = %#v", copyHeader)
	}
	copyExtra, _ := store.AllocString(reader, copied[0].Region, "copy-extra")
	if err := store.SetProperty(reader, copied[0], copyExtra, memory.NumberValue(4)); !errors.Is(err, memory.ErrReadOnlyProperty) {
		t.Fatalf("extending copied locked Object error = %v", err)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func TestNativeObjectSymbolPropertiesUseSemanticIdentity(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(23)
	reader := realmOwner(24)
	region := mustRegion(t, store, owner)
	object, _ := store.AllocObject(owner, region)
	description, _ := store.AllocString(owner, region, "token")
	first, _ := store.AllocSymbol(owner, region, memory.RefValue(description))
	second, _ := store.AllocSymbol(owner, region, memory.RefValue(description))
	stringKey, _ := store.AllocString(owner, region, "token")

	if err := store.SetProperty(owner, object, first, memory.NumberValue(1)); err != nil {
		t.Fatal(err)
	}
	if err := store.SetProperty(owner, object, second, memory.NumberValue(2)); err != nil {
		t.Fatal(err)
	}
	if err := store.SetProperty(owner, object, stringKey, memory.NumberValue(3)); err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct {
		key  memory.Ref
		want float64
	}{{first, 1}, {second, 2}, {stringKey, 3}} {
		value, found, err := store.GetOwnProperty(owner, object, check.key)
		if err != nil || !found || value.Number() != check.want {
			t.Fatalf("GetOwnProperty(%s) = %#v, %t, %v; want %v", check.key, value, found, err, check.want)
		}
	}

	copied, err := store.Copy(owner, reader, object, first, second)
	if err != nil {
		t.Fatal(err)
	}
	copiedObject, err := store.DerefObject(reader, copied[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(copiedObject.Properties) != 3 {
		firstSymbol, _ := store.DerefSymbol(reader, copied[1])
		secondSymbol, _ := store.DerefSymbol(reader, copied[2])
		t.Fatalf("copied property count = %d, want 3: %#v; symbols=%#v/%#v", len(copiedObject.Properties), copiedObject.Properties, firstSymbol, secondSymbol)
	}
	copiedFirst, err := store.DerefSymbol(reader, copiedObject.Properties[0].Name)
	if err != nil {
		t.Fatal(err)
	}
	copiedSecond, err := store.DerefSymbol(reader, copiedObject.Properties[1].Name)
	if err != nil {
		t.Fatal(err)
	}
	if copiedFirst.ID == copiedSecond.ID {
		t.Fatalf("distinct copied Symbol keys share identity %d", copiedFirst.ID)
	}
	value, found, err := store.GetOwnProperty(reader, copied[0], copied[1])
	if err != nil || !found || value.Number() != 1 {
		t.Fatalf("copied Symbol property = %#v, %t, %v; want 1", value, found, err)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func TestNativeObjectPromotionPreservesGraphAndPropertyOrder(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(21)
	reader := realmOwner(22)
	region := mustRegion(t, store, owner)
	prototype, _ := store.AllocObject(owner, region)
	object, _ := store.AllocObject(owner, region)
	child, _ := store.AllocObject(owner, region)
	firstName, _ := store.AllocString(owner, region, "first")
	secondName, _ := store.AllocString(owner, region, "second")
	if err := store.SetPrototype(owner, object, memory.RefValue(prototype)); err != nil {
		t.Fatal(err)
	}
	if err := store.SetProperty(owner, object, firstName, memory.RefValue(child)); err != nil {
		t.Fatal(err)
	}
	if err := store.SetProperty(owner, object, secondName, memory.RefValue(child)); err != nil {
		t.Fatal(err)
	}

	promoted, err := store.Promote(owner, object)
	if err != nil {
		t.Fatal(err)
	}
	copyObject, err := store.DerefObject(reader, promoted[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(copyObject.Properties) != 2 || !copyObject.Prototype.IsRef() {
		t.Fatalf("promoted Object = %#v", copyObject)
	}
	if copyObject.Properties[0].Value.Ref() != copyObject.Properties[1].Value.Ref() {
		t.Fatal("promotion did not preserve shared property alias")
	}
	for index, want := range []string{"first", "second"} {
		got, err := store.DerefString(reader, copyObject.Properties[index].Name)
		if err != nil || got != want {
			t.Fatalf("promoted property %d name = %q, %v; want %q", index, got, err, want)
		}
	}
	if err := store.ReleaseOwner(owner); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DerefObject(owner, object); !errors.Is(err, memory.ErrStaleRef) {
		t.Fatalf("source Object after release = %v, want ErrStaleRef", err)
	}
	if _, err := store.DerefObject(reader, promoted[0]); err != nil {
		t.Fatalf("promoted Object after source release = %v", err)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func TestNativeObjectDescriptorsOwnTheirAccessorGraph(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(23)
	reader := realmOwner(24)
	objectRegion := mustRegion(t, store, owner)
	valueRegion := mustRegion(t, store, owner)
	object, _ := store.AllocObject(owner, objectRegion)
	accessorName, _ := store.AllocString(owner, valueRegion, "answer")
	fixedName, _ := store.AllocString(owner, valueRegion, "fixed")
	getter, _ := store.AllocNativeFunction(owner, valueRegion, memory.NullValue(), memory.NullValue(), 0, 1)
	setter, _ := store.AllocNativeFunction(owner, valueRegion, memory.NullValue(), memory.NullValue(), 1, 2)
	child, _ := store.AllocObject(owner, valueRegion)

	if err := store.DefineProperty(owner, object, accessorName, memory.AccessorProperty(memory.RefValue(getter), memory.RefValue(setter), true, true)); err != nil {
		t.Fatal(err)
	}
	if err := store.DefineProperty(owner, object, fixedName, memory.DataProperty(memory.RefValue(child), false, false, false)); err != nil {
		t.Fatal(err)
	}
	if got := store.EdgeCount(objectRegion, valueRegion); got != 5 {
		t.Fatalf("descriptor edge count = %d, want 5", got)
	}
	if err := store.SetProperty(owner, object, fixedName, memory.NumberValue(2)); !errors.Is(err, memory.ErrReadOnlyProperty) {
		t.Fatalf("read-only SetProperty error = %v", err)
	}
	if deleted, err := store.DeleteProperty(owner, object, fixedName); err != nil || deleted {
		t.Fatalf("delete non-configurable = %t, %v", deleted, err)
	}
	if err := store.DefineProperty(owner, object, fixedName, memory.DataProperty(memory.RefValue(child), true, false, false)); !errors.Is(err, memory.ErrReadOnlyProperty) {
		t.Fatalf("reconfigure fixed property error = %v", err)
	}

	promoted, err := store.Promote(owner, object)
	if err != nil {
		t.Fatal(err)
	}
	copyObject, err := store.DerefObject(reader, promoted[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(copyObject.Properties) != 2 || copyObject.Properties[0].Kind != memory.PropertyAccessor || copyObject.Properties[1].Writable {
		t.Fatalf("promoted descriptors = %#v", copyObject.Properties)
	}
	if !copyObject.Properties[0].Getter.IsRef() || !copyObject.Properties[0].Setter.IsRef() {
		t.Fatalf("promoted accessor lost callables: %#v", copyObject.Properties[0])
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func TestNativeObjectRejectsPrototypeCycles(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(25)
	region := mustRegion(t, store, owner)
	first, _ := store.AllocObject(owner, region)
	second, _ := store.AllocObject(owner, region)
	if err := store.SetPrototype(owner, first, memory.RefValue(second)); err != nil {
		t.Fatal(err)
	}
	if err := store.SetPrototype(owner, second, memory.RefValue(first)); !errors.Is(err, memory.ErrPrototypeCycle) {
		t.Fatalf("prototype cycle error = %v", err)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func TestSharedObjectHeadersSurviveGraphPromotion(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(26)
	region := mustRegion(t, store, owner)
	prototype, _ := store.AllocObject(owner, region)
	name, _ := store.AllocString(owner, region, "marker")
	array, _ := store.AllocArray(owner, region, 0)
	function, _ := store.AllocBytecodeFunction(owner, region, memory.NullValue(), memory.NullValue(), 0, nil, nil)
	promise, _ := store.AllocPromise(owner, region)
	mapRef, _ := store.AllocMap(owner, region)
	setRef, _ := store.AllocSet(owner, region)
	errorRef, _ := store.AllocError(owner, region, memory.ErrorType, memory.NullValue())
	objects := []memory.Ref{array, function, promise, mapRef, setRef, errorRef}
	for _, object := range objects {
		if err := store.SetPrototype(owner, object, memory.RefValue(prototype)); err != nil {
			t.Fatal(err)
		}
		if err := store.DefineProperty(owner, object, name, memory.DataProperty(memory.NumberValue(42), true, true, true)); err != nil {
			t.Fatal(err)
		}
	}
	root, _ := store.AllocCell(owner, region)
	for index, object := range objects {
		if err := store.Set(owner, root, index, memory.RefValue(object)); err != nil {
			t.Fatal(err)
		}
	}
	promoted, err := store.Promote(owner, root)
	if err != nil {
		t.Fatal(err)
	}
	reader := realmOwner(27)
	copyRoot, err := store.Deref(reader, promoted[0])
	if err != nil {
		t.Fatal(err)
	}
	for index, value := range copyRoot.Fields {
		header, err := store.DerefObjectHeader(reader, value.Ref())
		if err != nil {
			t.Fatalf("header %d: %v", index, err)
		}
		if !header.Prototype.IsRef() || header.Prototype.Ref() == prototype || len(header.Properties) != 1 || header.Properties[0].Value.Number() != 42 {
			t.Fatalf("header %d = %#v", index, header)
		}
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}
