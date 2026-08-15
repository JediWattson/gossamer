package memory_test

import (
	"errors"
	"math"
	"testing"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func TestNativeSetUsesSameValueZeroAndMaintainsEdges(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(120)
	setRegion := mustRegion(t, store, owner)
	valueRegion := mustRegion(t, store, owner)
	ref, _ := store.AllocSet(owner, setRegion)
	first, _ := store.AllocString(owner, valueRegion, "same")
	second, _ := store.AllocString(owner, valueRegion, "same")
	if err := store.SetAdd(owner, ref, memory.RefValue(first)); err != nil {
		t.Fatal(err)
	}
	if err := store.SetAdd(owner, ref, memory.RefValue(second)); err != nil {
		t.Fatal(err)
	}
	if got := store.EdgeCount(setRegion, valueRegion); got != 1 {
		t.Fatalf("equal String edges = %d, want 1", got)
	}
	if has, err := store.SetHas(owner, ref, memory.RefValue(second)); err != nil || !has {
		t.Fatalf("SetHas(equal String) = %t, %v", has, err)
	}
	for _, value := range []memory.Value{
		memory.NumberValue(math.NaN()),
		memory.NumberValue(math.NaN()),
		memory.NumberValue(math.Copysign(0, -1)),
		memory.NumberValue(0),
	} {
		if err := store.SetAdd(owner, ref, value); err != nil {
			t.Fatal(err)
		}
	}
	bigA, _ := store.ParseBigInt(owner, valueRegion, "42", 10)
	bigB, _ := store.ParseBigInt(owner, valueRegion, "42", 10)
	if err := store.SetAdd(owner, ref, memory.RefValue(bigA)); err != nil {
		t.Fatal(err)
	}
	if err := store.SetAdd(owner, ref, memory.RefValue(bigB)); err != nil {
		t.Fatal(err)
	}
	description, _ := store.AllocString(owner, valueRegion, "symbol")
	symbolA, _ := store.AllocSymbol(owner, valueRegion, memory.RefValue(description))
	symbolB, _ := store.AllocSymbol(owner, valueRegion, memory.RefValue(description))
	if err := store.SetAdd(owner, ref, memory.RefValue(symbolA)); err != nil {
		t.Fatal(err)
	}
	if err := store.SetAdd(owner, ref, memory.RefValue(symbolB)); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.DerefSet(owner, ref)
	if err != nil || len(snapshot.Values) != 6 {
		t.Fatalf("Set values = %#v, %v; want String, NaN, zero, BigInt, two Symbols", snapshot.Values, err)
	}
	deleted, err := store.SetDelete(owner, ref, memory.RefValue(second))
	if err != nil || !deleted {
		t.Fatalf("SetDelete(equal String) = %t, %v", deleted, err)
	}
	if err := store.SetClear(owner, ref); err != nil {
		t.Fatal(err)
	}
	if got := store.EdgeCount(setRegion, valueRegion); got != 0 {
		t.Fatalf("edges after clear = %d, want 0", got)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func TestNativeSetPromotionPreservesOrder(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(121)
	reader := realmOwner(122)
	region := mustRegion(t, store, owner)
	ref, _ := store.AllocSet(owner, region)
	first, _ := store.AllocString(owner, region, "first")
	second, _ := store.AllocString(owner, region, "second")
	if err := store.SetAdd(owner, ref, memory.RefValue(first)); err != nil {
		t.Fatal(err)
	}
	if err := store.SetAdd(owner, ref, memory.RefValue(second)); err != nil {
		t.Fatal(err)
	}
	promoted, err := store.Promote(owner, ref)
	if err != nil {
		t.Fatal(err)
	}
	copySet, err := store.DerefSet(reader, promoted[0])
	if err != nil || len(copySet.Values) != 2 {
		t.Fatalf("promoted Set = %#v, %v", copySet, err)
	}
	for index, want := range []string{"first", "second"} {
		got, err := store.DerefString(reader, copySet.Values[index].Ref())
		if err != nil || got != want {
			t.Fatalf("promoted Set value %d = %q, %v", index, got, err)
		}
	}
	if err := store.ReleaseOwner(owner); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DerefSet(owner, ref); !errors.Is(err, memory.ErrStaleRef) {
		t.Fatalf("source Set after release = %v, want ErrStaleRef", err)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}
