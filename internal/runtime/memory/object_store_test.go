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
