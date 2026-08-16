package memory_test

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func TestShortLivedRegionsReuseProfiledSlotBuffers(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	owner := realmOwner(970)
	for iteration := 0; iteration < 5; iteration++ {
		region := mustRegion(t, store, owner)
		for slot := 0; slot < 7; slot++ {
			mustAlloc(t, store, owner, region)
		}
		if err := store.DestroyRegion(owner, region); err != nil {
			t.Fatal(err)
		}
		assertStoreInvariants(t, store, "short-lived region buffer reuse")
	}

	stats := store.Stats()
	if stats.Allocations != 35 || stats.Frees != 35 || stats.LiveSlots != 0 {
		t.Fatalf("slot lifecycle stats = %#v", stats)
	}
	if stats.SlotBufferAllocations != 1 || stats.SlotBufferReuses != 4 || stats.SlotBufferGrows != 0 {
		t.Fatalf("slot buffer activity = %#v", stats)
	}
	if stats.PooledSlotBuffers != 1 || stats.PooledSlotCapacity != 8 || stats.ReservedSlotCapacity != 8 || stats.PeakReservedSlotCapacity != 8 {
		t.Fatalf("slot buffer capacity = %#v", stats)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	stats = store.Stats()
	if stats.PooledSlotBuffers != 0 || stats.PooledSlotCapacity != 0 || stats.ReservedSlotCapacity != 0 {
		t.Fatalf("closed slot buffer pool = %#v", stats)
	}
	assertStoreInvariants(t, store, "closed slot buffer pool")
}

func TestRegionSlotBuffersGrowGeometrically(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(971)
	region := mustRegion(t, store, owner)
	for slot := 0; slot < 9; slot++ {
		mustAlloc(t, store, owner, region)
	}
	stats := store.Stats()
	if stats.SlotBufferAllocations != 2 || stats.SlotBufferGrows != 1 || stats.SlotBufferReuses != 0 {
		t.Fatalf("grown slot buffer activity = %#v", stats)
	}
	if stats.PooledSlotBuffers != 1 || stats.PooledSlotCapacity != 8 || stats.ReservedSlotCapacity != 24 || stats.PeakReservedSlotCapacity != 24 {
		t.Fatalf("grown slot buffer capacity = %#v", stats)
	}
	assertStoreInvariants(t, store, "grown region slot buffer")

	if err := store.DestroyRegion(owner, region); err != nil {
		t.Fatal(err)
	}
	stats = store.Stats()
	if stats.PooledSlotBuffers != 2 || stats.PooledSlotCapacity != 24 || stats.ReservedSlotCapacity != 24 {
		t.Fatalf("pooled grown slot buffers = %#v", stats)
	}
	assertStoreInvariants(t, store, "pooled grown slot buffers")
}
