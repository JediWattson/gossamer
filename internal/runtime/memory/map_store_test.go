package memory_test

import (
	"errors"
	"math"
	"testing"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func TestNativeMapUsesSameValueZeroAndMaintainsEdges(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(110)
	mapRegion := mustRegion(t, store, owner)
	valueRegion := mustRegion(t, store, owner)
	ref, _ := store.AllocMap(owner, mapRegion)
	key, _ := store.AllocString(owner, valueRegion, "key")
	equivalentKey, _ := store.AllocString(owner, valueRegion, "key")
	value, _ := store.AllocObject(owner, valueRegion)
	if err := store.MapSet(owner, ref, memory.RefValue(key), memory.RefValue(value)); err != nil {
		t.Fatal(err)
	}
	if err := store.MapSet(owner, ref, memory.RefValue(equivalentKey), memory.NumberValue(2)); err != nil {
		t.Fatal(err)
	}
	if got := store.EdgeCount(mapRegion, valueRegion); got != 1 {
		t.Fatalf("edge count after equivalent-key replacement = %d, want retained original key", got)
	}
	if got, found, err := store.MapGet(owner, ref, memory.RefValue(equivalentKey)); err != nil || !found || got.Number() != 2 {
		t.Fatalf("MapGet(equal String) = %#v, %t, %v", got, found, err)
	}
	if err := store.MapSet(owner, ref, memory.NumberValue(math.NaN()), memory.NumberValue(1)); err != nil {
		t.Fatal(err)
	}
	if err := store.MapSet(owner, ref, memory.NumberValue(math.NaN()), memory.NumberValue(3)); err != nil {
		t.Fatal(err)
	}
	if err := store.MapSet(owner, ref, memory.NumberValue(math.Copysign(0, -1)), memory.NumberValue(4)); err != nil {
		t.Fatal(err)
	}
	if err := store.MapSet(owner, ref, memory.NumberValue(0), memory.NumberValue(5)); err != nil {
		t.Fatal(err)
	}
	firstBigInt, _ := store.ParseBigInt(owner, valueRegion, "9007199254740993", 10)
	secondBigInt, _ := store.ParseBigInt(owner, valueRegion, "9007199254740993", 10)
	if err := store.MapSet(owner, ref, memory.RefValue(firstBigInt), memory.BoolValue(true)); err != nil {
		t.Fatal(err)
	}
	if err := store.MapSet(owner, ref, memory.RefValue(secondBigInt), memory.BoolValue(false)); err != nil {
		t.Fatal(err)
	}
	description, _ := store.AllocString(owner, valueRegion, "symbol")
	firstSymbol, _ := store.AllocSymbol(owner, valueRegion, memory.RefValue(description))
	secondSymbol, _ := store.AllocSymbol(owner, valueRegion, memory.RefValue(description))
	if err := store.MapSet(owner, ref, memory.RefValue(firstSymbol), memory.NumberValue(1)); err != nil {
		t.Fatal(err)
	}
	if err := store.MapSet(owner, ref, memory.RefValue(secondSymbol), memory.NumberValue(2)); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.DerefMap(owner, ref)
	if err != nil || len(snapshot.Entries) != 6 {
		t.Fatalf("Map entries = %#v, %v; want String, NaN, zero, BigInt, and two Symbols", snapshot.Entries, err)
	}
	if err := store.MapClear(owner, ref); err != nil {
		t.Fatal(err)
	}
	if got := store.EdgeCount(mapRegion, valueRegion); got != 0 {
		t.Fatalf("edge count after clear = %d, want 0", got)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func TestNativeMapPromotionPreservesOrderAndAliases(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(111)
	reader := realmOwner(112)
	region := mustRegion(t, store, owner)
	ref, _ := store.AllocMap(owner, region)
	first, _ := store.AllocString(owner, region, "first")
	second, _ := store.AllocString(owner, region, "second")
	shared, _ := store.AllocObject(owner, region)
	if err := store.MapSet(owner, ref, memory.RefValue(first), memory.RefValue(shared)); err != nil {
		t.Fatal(err)
	}
	if err := store.MapSet(owner, ref, memory.RefValue(second), memory.RefValue(shared)); err != nil {
		t.Fatal(err)
	}
	promoted, err := store.Promote(owner, ref)
	if err != nil {
		t.Fatal(err)
	}
	copyMap, err := store.DerefMap(reader, promoted[0])
	if err != nil || len(copyMap.Entries) != 2 {
		t.Fatalf("promoted Map = %#v, %v", copyMap, err)
	}
	if copyMap.Entries[0].Value.Ref() != copyMap.Entries[1].Value.Ref() || copyMap.Entries[0].Value.Ref() == shared {
		t.Fatal("promotion did not preserve shared value alias")
	}
	for index, want := range []string{"first", "second"} {
		got, err := store.DerefString(reader, copyMap.Entries[index].Key.Ref())
		if err != nil || got != want {
			t.Fatalf("promoted key %d = %q, %v", index, got, err)
		}
	}
	if err := store.ReleaseOwner(owner); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DerefMap(owner, ref); !errors.Is(err, memory.ErrStaleRef) {
		t.Fatalf("source Map after release = %v, want ErrStaleRef", err)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}
