package memory_test

import (
	"errors"
	"math"
	"testing"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func TestNativeArrayTracksHolesLengthAndReferenceEdges(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(30)
	arrayRegion := mustRegion(t, store, owner)
	valueRegion := mustRegion(t, store, owner)
	array, err := store.AllocArray(owner, arrayRegion, 3)
	if err != nil {
		t.Fatal(err)
	}
	text, err := store.AllocString(owner, valueRegion, "value")
	if err != nil {
		t.Fatal(err)
	}
	object, err := store.AllocObject(owner, valueRegion)
	if err != nil {
		t.Fatal(err)
	}

	if _, present, err := store.ArrayElement(owner, array, 1); err != nil || present {
		t.Fatalf("initial hole = present:%t err:%v", present, err)
	}
	if err := store.SetArrayElement(owner, array, 1, memory.UndefinedValue()); err != nil {
		t.Fatal(err)
	}
	if value, present, err := store.ArrayElement(owner, array, 1); err != nil || !present || value.Kind() != memory.ValueUndefined {
		t.Fatalf("present undefined = %#v, %t, %v", value, present, err)
	}
	if err := store.SetArrayElement(owner, array, 5, memory.RefValue(text)); err != nil {
		t.Fatal(err)
	}
	if err := store.SetArrayElement(owner, array, 4, memory.RefValue(object)); err != nil {
		t.Fatal(err)
	}
	if got := store.EdgeCount(arrayRegion, valueRegion); got != 2 {
		t.Fatalf("edge count = %d, want 2", got)
	}
	snapshot, err := store.DerefArray(owner, array)
	if err != nil || snapshot.Length != 6 || len(snapshot.Elements) != 3 {
		t.Fatalf("DerefArray() = %#v, %v", snapshot, err)
	}
	deleted, err := store.DeleteArrayElement(owner, array, 5)
	if err != nil || !deleted {
		t.Fatalf("DeleteArrayElement() = %t, %v", deleted, err)
	}
	if snapshot, _ := store.DerefArray(owner, array); snapshot.Length != 6 {
		t.Fatalf("delete changed length to %d, want 6", snapshot.Length)
	}
	if err := store.SetArrayLength(owner, array, 2); err != nil {
		t.Fatal(err)
	}
	if got := store.EdgeCount(arrayRegion, valueRegion); got != 0 {
		t.Fatalf("edge count after truncation = %d, want 0", got)
	}
	if snapshot, _ := store.DerefArray(owner, array); snapshot.Length != 2 || len(snapshot.Elements) != 1 || snapshot.Elements[0].Index != 1 {
		t.Fatalf("truncated Array = %#v", snapshot)
	}
	if err := store.SetArrayElement(owner, array, math.MaxUint32, memory.NumberValue(1)); !errors.Is(err, memory.ErrInvalidIndex) {
		t.Fatalf("max index error = %v, want ErrInvalidIndex", err)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func TestNativeArrayPromotionPreservesSparseAliases(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(31)
	reader := realmOwner(32)
	region := mustRegion(t, store, owner)
	array, err := store.AllocArray(owner, region, 10)
	if err != nil {
		t.Fatal(err)
	}
	value, err := store.AllocString(owner, region, "shared")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetArrayElement(owner, array, 2, memory.RefValue(value)); err != nil {
		t.Fatal(err)
	}
	if err := store.SetArrayElement(owner, array, 8, memory.RefValue(value)); err != nil {
		t.Fatal(err)
	}

	promoted, err := store.Promote(owner, array)
	if err != nil {
		t.Fatal(err)
	}
	copyArray, err := store.DerefArray(reader, promoted[0])
	if err != nil {
		t.Fatal(err)
	}
	if copyArray.Length != 10 || len(copyArray.Elements) != 2 {
		t.Fatalf("promoted Array = %#v", copyArray)
	}
	if copyArray.Elements[0].Value.Ref() != copyArray.Elements[1].Value.Ref() || copyArray.Elements[0].Value.Ref() == value {
		t.Fatal("promotion did not preserve alias while cloning source")
	}
	if err := store.ReleaseOwner(owner); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DerefArray(owner, array); !errors.Is(err, memory.ErrStaleRef) {
		t.Fatalf("source Array after release = %v, want ErrStaleRef", err)
	}
	if got, err := store.DerefString(reader, copyArray.Elements[0].Value.Ref()); err != nil || got != "shared" {
		t.Fatalf("promoted String = %q, %v", got, err)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}
