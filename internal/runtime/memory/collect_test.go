package memory_test

import (
	"errors"
	"testing"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func TestCollectReclaimsUnreachableCyclesAtOwnerCheckpoint(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(950)
	region := mustRegion(t, store, owner)
	root := mustAlloc(t, store, owner, region)
	liveLeaf := mustAlloc(t, store, owner, region)
	first := mustAlloc(t, store, owner, region)
	second := mustAlloc(t, store, owner, region)
	self := mustAlloc(t, store, owner, region)
	if err := store.Set(owner, root, 0, memory.RefValue(liveLeaf)); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(owner, first, 0, memory.RefValue(second)); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(owner, second, 0, memory.RefValue(first)); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(owner, self, 0, memory.RefValue(self)); err != nil {
		t.Fatal(err)
	}
	assertStoreInvariants(t, store, "before cycle collection")

	result, err := store.Collect(owner, root)
	if err != nil {
		t.Fatal(err)
	}
	if result.Roots != 1 || result.MarkedSlots != 2 || result.ReclaimedSlots != 3 {
		t.Fatalf("collection = %#v", result)
	}
	for _, ref := range []memory.Ref{first, second, self} {
		if _, err := store.Deref(owner, ref); !errors.Is(err, memory.ErrStaleRef) {
			t.Errorf("collected %s = %v, want stale", ref, err)
		}
	}
	if _, err := store.Deref(owner, root); err != nil {
		t.Fatalf("root after collection: %v", err)
	}
	if _, err := store.Deref(owner, liveLeaf); err != nil {
		t.Fatalf("live leaf after collection: %v", err)
	}
	stats := store.Stats()
	if stats.Collections != 1 || stats.CollectedSlots != 3 || stats.LiveSlots != 2 || stats.Frees != 3 {
		t.Fatalf("collection stats = %#v", stats)
	}
	assertStoreInvariants(t, store, "after cycle collection")

	reused := mustAlloc(t, store, owner, region)
	if reused.Slot != self.Slot || reused.Gen == self.Gen {
		t.Fatalf("reused ref = %s, last collected=%s", reused, self)
	}
	assertStoreInvariants(t, store, "after collected slot reuse")
}

func TestCollectWithoutRootsReclaimsWholePrivateHeap(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(951)
	region := mustRegion(t, store, owner)
	value, err := store.AllocString(owner, region, "cycle-checkpoint")
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.Collect(owner)
	if err != nil {
		t.Fatal(err)
	}
	if result.Roots != 0 || result.MarkedSlots != 0 || result.ReclaimedSlots != 1 || result.ReclaimedBytes != uint64(len("cycle-checkpoint")) {
		t.Fatalf("collection = %#v", result)
	}
	if _, err := store.DerefString(owner, value); !errors.Is(err, memory.ErrStaleRef) {
		t.Fatalf("collected string = %v, want stale", err)
	}
	assertStoreInvariants(t, store, "rootless collection")
}

func TestCollectRegionPreservesSiblingOwnerRegionsAndTheirIncomingEdges(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(952)
	persistent := mustRegion(t, store, owner)
	envelope := mustRegion(t, store, owner)
	root := mustAlloc(t, store, owner, persistent)
	retainedByEnvelope := mustAlloc(t, store, owner, persistent)
	unreachable := mustAlloc(t, store, owner, persistent)
	envelopeRoot := mustAlloc(t, store, owner, envelope)
	if err := store.Set(owner, envelopeRoot, 0, memory.RefValue(retainedByEnvelope)); err != nil {
		t.Fatal(err)
	}

	result, err := store.CollectRegion(owner, persistent, root)
	if err != nil {
		t.Fatal(err)
	}
	if result.MarkedSlots != 2 || result.ReclaimedSlots != 1 {
		t.Fatalf("region collection = %#v", result)
	}
	if _, err := store.Deref(owner, unreachable); !errors.Is(err, memory.ErrStaleRef) {
		t.Fatalf("unreachable persistent ref = %v, want stale", err)
	}
	for _, ref := range []memory.Ref{root, retainedByEnvelope, envelopeRoot} {
		if _, err := store.Deref(owner, ref); err != nil {
			t.Fatalf("preserved %s: %v", ref, err)
		}
	}
	assertStoreInvariants(t, store, "region-scoped collection")
}

func TestCollectRegionPreservesValueFromLiveSiblingEphemeron(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(953)
	tableRegion := mustRegion(t, store, owner)
	valueRegion := mustRegion(t, store, owner)
	table, err := store.AllocWeakMap(owner, tableRegion)
	if err != nil {
		t.Fatal(err)
	}
	key := mustAlloc(t, store, owner, tableRegion)
	value := mustAlloc(t, store, owner, valueRegion)
	if err := store.WeakMapSet(owner, table, key, memory.RefValue(value)); err != nil {
		t.Fatal(err)
	}

	result, err := store.CollectRegion(owner, valueRegion)
	if err != nil {
		t.Fatal(err)
	}
	if result.MarkedSlots != 1 || result.ReclaimedSlots != 0 || result.ClearedWeakEntries != 0 {
		t.Fatalf("region collection = %#v", result)
	}
	if _, err := store.Deref(owner, value); err != nil {
		t.Fatalf("live sibling ephemeron value was collected: %v", err)
	}
	snapshot, err := store.DerefWeakMap(owner, table)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Entries) != 1 || !snapshot.Entries[0].Value.IsRef() || snapshot.Entries[0].Value.Ref() != value {
		t.Fatalf("WeakMap after collection = %#v", snapshot)
	}
	assertStoreInvariants(t, store, "sibling ephemeron collection")
}
