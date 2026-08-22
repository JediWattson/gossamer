package memory_test

import (
	"errors"
	"testing"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

func TestWeakMapTracesValueOnlyWhileKeyIsIndependentlyLive(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(960)
	region := mustRegion(t, store, owner)
	weakMap, err := store.AllocWeakMap(owner, region)
	if err != nil {
		t.Fatal(err)
	}
	key := mustAlloc(t, store, owner, region)
	value := mustAlloc(t, store, owner, region)
	leaf := mustAlloc(t, store, owner, region)
	if err := store.Set(owner, value, 0, memory.RefValue(leaf)); err != nil {
		t.Fatal(err)
	}
	if err := store.WeakMapSet(owner, weakMap, key, memory.RefValue(value)); err != nil {
		t.Fatal(err)
	}
	assertStoreInvariants(t, store, "live ephemeron")

	result, err := store.Collect(owner, weakMap, key)
	if err != nil {
		t.Fatal(err)
	}
	if result.MarkedSlots != 4 || result.ReclaimedSlots != 0 || result.ClearedWeakEntries != 0 {
		t.Fatalf("live-key collection = %#v", result)
	}
	if got, found, err := store.WeakMapGet(owner, weakMap, key); err != nil || !found || got.Ref() != value {
		t.Fatalf("WeakMapGet() = %v, %t, %v", got, found, err)
	}

	result, err = store.Collect(owner, weakMap)
	if err != nil {
		t.Fatal(err)
	}
	if result.MarkedSlots != 1 || result.ReclaimedSlots != 3 || result.ClearedWeakEntries != 1 {
		t.Fatalf("dead-key collection = %#v", result)
	}
	for _, ref := range []memory.Ref{key, value, leaf} {
		if _, err := store.Deref(owner, ref); !errors.Is(err, memory.ErrStaleRef) {
			t.Errorf("Deref(%s) = %v, want stale", ref, err)
		}
	}
	got, err := store.DerefWeakMap(owner, weakMap)
	if err != nil || len(got.Entries) != 0 {
		t.Fatalf("DerefWeakMap() = %#v, %v", got, err)
	}
	if stats := store.Stats(); stats.WeakEntriesCleared != 1 || stats.LiveWeakMaps != 1 {
		t.Fatalf("weak-map stats = %#v", stats)
	}
	assertStoreInvariants(t, store, "dead ephemeron swept")
}

func TestWeakMapEphemeronsReachFixedPoint(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(961)
	region := mustRegion(t, store, owner)
	firstMap, _ := store.AllocWeakMap(owner, region)
	secondMap, _ := store.AllocWeakMap(owner, region)
	firstKey := mustAlloc(t, store, owner, region)
	secondKey := mustAlloc(t, store, owner, region)
	finalValue := mustAlloc(t, store, owner, region)
	if err := store.WeakMapSet(owner, firstMap, firstKey, memory.RefValue(secondKey)); err != nil {
		t.Fatal(err)
	}
	if err := store.WeakMapSet(owner, secondMap, secondKey, memory.RefValue(finalValue)); err != nil {
		t.Fatal(err)
	}

	result, err := store.Collect(owner, firstMap, secondMap, firstKey)
	if err != nil {
		t.Fatal(err)
	}
	if result.MarkedSlots != 5 || result.ReclaimedSlots != 0 {
		t.Fatalf("fixed-point collection = %#v", result)
	}
	if _, err := store.Deref(owner, finalValue); err != nil {
		t.Fatalf("second ephemeron value: %v", err)
	}
	assertStoreInvariants(t, store, "ephemeron fixed point")
}

func TestWeakSetDoesNotRetainItsKey(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(962)
	region := mustRegion(t, store, owner)
	weakSet, err := store.AllocWeakSet(owner, region)
	if err != nil {
		t.Fatal(err)
	}
	key := mustAlloc(t, store, owner, region)
	if err := store.WeakSetAdd(owner, weakSet, key); err != nil {
		t.Fatal(err)
	}
	if found, err := store.WeakSetHas(owner, weakSet, key); err != nil || !found {
		t.Fatalf("WeakSetHas() = %t, %v", found, err)
	}

	result, err := store.Collect(owner, weakSet)
	if err != nil {
		t.Fatal(err)
	}
	if result.ReclaimedSlots != 1 || result.ClearedWeakEntries != 1 {
		t.Fatalf("weak-set collection = %#v", result)
	}
	got, err := store.DerefWeakSet(owner, weakSet)
	if err != nil || len(got.Keys) != 0 {
		t.Fatalf("DerefWeakSet() = %#v, %v", got, err)
	}
	assertStoreInvariants(t, store, "weak-set key swept")
}

func TestWeakOwnershipRejectsShorterLivedKeysAndPromotesValues(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	task := ownership.OwnerID{Kind: ownership.OwnerTask, Value: 963}
	document := ownership.OwnerID{Kind: ownership.OwnerDocument, Value: 963}
	taskRegion := mustRegion(t, store, task)
	documentRegion := mustRegion(t, store, document)
	weakMap, _ := store.AllocWeakMap(document, documentRegion)
	key := mustAlloc(t, store, document, documentRegion)
	shortKey := mustAlloc(t, store, task, taskRegion)
	value := mustAlloc(t, store, task, taskRegion)
	leaf := mustAlloc(t, store, task, taskRegion)
	if err := store.Set(task, value, 0, memory.RefValue(leaf)); err != nil {
		t.Fatal(err)
	}
	if err := store.WeakMapSet(document, weakMap, shortKey, memory.RefValue(value)); !errors.Is(err, memory.ErrWeakKeyLifetime) {
		t.Fatalf("short weak key error = %v, want ErrWeakKeyLifetime", err)
	}
	if err := store.WeakMapSet(document, weakMap, key, memory.RefValue(value)); err != nil {
		t.Fatal(err)
	}
	stored, found, err := store.WeakMapGet(document, weakMap, key)
	if err != nil || !found || !stored.IsRef() || stored.Ref() == value {
		t.Fatalf("promoted weak value = %v, %t, %v", stored, found, err)
	}
	promoted := stored.Ref()
	if err := store.ReleaseOwner(task); err != nil {
		t.Fatal(err)
	}
	result, err := store.Collect(document, weakMap, key)
	if err != nil {
		t.Fatal(err)
	}
	if result.ReclaimedSlots != 0 || result.MarkedSlots != 4 {
		t.Fatalf("promoted ephemeron collection = %#v", result)
	}
	if _, err := store.Deref(document, promoted); err != nil {
		t.Fatalf("promoted value after source release: %v", err)
	}
	assertStoreInvariants(t, store, "promoted weak value")
}

func TestCopyPreservesLiveWeakMapEphemeronAndDropsDeadWeakEntries(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	source := realmOwner(964)
	destination := realmOwner(965)
	region := mustRegion(t, store, source)
	weakMap, _ := store.AllocWeakMap(source, region)
	weakSet, _ := store.AllocWeakSet(source, region)
	liveKey := mustAlloc(t, store, source, region)
	deadKey := mustAlloc(t, store, source, region)
	value := mustAlloc(t, store, source, region)
	leaf := mustAlloc(t, store, source, region)
	if err := store.Set(source, value, 0, memory.RefValue(leaf)); err != nil {
		t.Fatal(err)
	}
	if err := store.WeakMapSet(source, weakMap, liveKey, memory.RefValue(value)); err != nil {
		t.Fatal(err)
	}
	if err := store.WeakMapSet(source, weakMap, deadKey, memory.NumberValue(9)); err != nil {
		t.Fatal(err)
	}
	if err := store.WeakSetAdd(source, weakSet, liveKey); err != nil {
		t.Fatal(err)
	}
	if err := store.WeakSetAdd(source, weakSet, deadKey); err != nil {
		t.Fatal(err)
	}

	copied, err := store.Copy(source, destination, weakMap, weakSet, liveKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(copied) != 3 {
		t.Fatalf("copied roots = %v", copied)
	}
	copiedMap, copiedSet, copiedKey := copied[0], copied[1], copied[2]
	copiedValue, found, err := store.WeakMapGet(destination, copiedMap, copiedKey)
	if err != nil || !found || !copiedValue.IsRef() {
		t.Fatalf("copied live ephemeron = %v, %t, %v", copiedValue, found, err)
	}
	cell, err := store.Deref(destination, copiedValue.Ref())
	if err != nil || len(cell.Fields) != 1 || !cell.Fields[0].IsRef() {
		t.Fatalf("copied ephemeron value = %#v, %v", cell, err)
	}
	if _, err := store.Deref(destination, cell.Fields[0].Ref()); err != nil {
		t.Fatalf("copied ephemeron leaf: %v", err)
	}
	mapSnapshot, err := store.DerefWeakMap(destination, copiedMap)
	if err != nil || len(mapSnapshot.Entries) != 1 {
		t.Fatalf("copied WeakMap = %#v, %v", mapSnapshot, err)
	}
	setSnapshot, err := store.DerefWeakSet(destination, copiedSet)
	if err != nil || len(setSnapshot.Keys) != 1 || setSnapshot.Keys[0] != copiedKey {
		t.Fatalf("copied WeakSet = %#v, %v", setSnapshot, err)
	}
	if err := store.ReleaseOwner(source); err != nil {
		t.Fatal(err)
	}
	assertStoreInvariants(t, store, "copied weak ephemeron")
}
