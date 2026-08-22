package memory

import (
	"testing"
	"unsafe"

	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

func TestPhysicalStatsAttributeSlotAndPayloadStorage(t *testing.T) {
	store := NewStore(nil)
	owner := ownership.OwnerID{Kind: ownership.OwnerRealm, Value: 71}
	region, err := store.NewRegion(owner)
	if err != nil {
		t.Fatal(err)
	}
	cell, err := store.AllocCell(owner, region)
	if err != nil {
		t.Fatal(err)
	}
	text, err := store.AllocString(owner, region, "profiled")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set(owner, cell, 3, RefValue(text)); err != nil {
		t.Fatal(err)
	}

	physical := store.PhysicalStats()
	if physical.SlotSizeBytes != uint64(unsafe.Sizeof(Slot{})) || physical.RefSizeBytes != uint64(unsafe.Sizeof(Ref{})) || physical.ValueSizeBytes != uint64(unsafe.Sizeof(Value{})) {
		t.Fatalf("physical scalar sizes = %#v", physical)
	}
	if physical.SlotPayloadSizeBytes != uint64(unsafe.Sizeof(slotPayload{})) {
		t.Fatalf("slot payload size = %d", physical.SlotPayloadSizeBytes)
	}
	if physical.PayloadArenaSlabs != 2 || physical.ReservedTypedPayloadBytes == 0 || physical.OccupiedTypedPayloadBytes == 0 {
		t.Fatalf("typed payload attribution = %#v", physical)
	}
	if physical.ReservedSlotBytes != uint64(profiledRegionSlotCapacity)*physical.SlotSizeBytes {
		t.Fatalf("reserved slot bytes = %d, want %d", physical.ReservedSlotBytes, uint64(profiledRegionSlotCapacity)*physical.SlotSizeBytes)
	}
	if physical.OccupiedSlotBytes != 2*physical.SlotSizeBytes {
		t.Fatalf("occupied slot bytes = %d", physical.OccupiedSlotBytes)
	}
	minimumPayload := 2*physical.SlotPayloadSizeBytes + uint64(len("profiled")) + uint64(4)*uint64(unsafe.Sizeof(Value{}))
	if physical.PayloadBytes < minimumPayload || physical.AttributedBytes != physical.ReservedSlotBytes+physical.PayloadBytes+physical.FreeListBytes+physical.WeakTargetUseBytes {
		t.Fatalf("physical payload attribution = %#v, want at least %d payload bytes", physical, minimumPayload)
	}
}

func TestPhysicalStatsAttributeWeakReverseUses(t *testing.T) {
	store := NewStore(nil)
	defer store.Close()
	owner := ownership.OwnerID{Kind: ownership.OwnerRealm, Value: 74}
	region, _ := store.NewRegion(owner)
	weakMap, _ := store.AllocWeakMap(owner, region)
	weakSet, _ := store.AllocWeakSet(owner, region)
	key, _ := store.AllocCell(owner, region)
	if err := store.WeakMapSet(owner, weakMap, key, RefValue(key)); err != nil {
		t.Fatal(err)
	}
	if err := store.WeakSetAdd(owner, weakSet, key); err != nil {
		t.Fatal(err)
	}
	physical := store.PhysicalStats()
	if physical.WeakTargetEntries != 1 || physical.WeakTargetReferences != 3 {
		t.Fatalf("weak reverse counts = %#v", physical)
	}
	if physical.WeakTargetUseBytes < 3*uint64(unsafe.Sizeof(weakUse{})) {
		t.Fatalf("weak reverse bytes = %d", physical.WeakTargetUseBytes)
	}
	if physical.AttributedBytes != physical.ReservedSlotBytes+physical.PayloadBytes+physical.FreeListBytes+physical.WeakTargetUseBytes {
		t.Fatalf("weak attributed bytes = %#v", physical)
	}
}

func TestPhysicalStatsVacantSlotRetainsOnlyStableHeader(t *testing.T) {
	store := NewStore(nil)
	owner := ownership.OwnerID{Kind: ownership.OwnerRealm, Value: 73}
	region, err := store.NewRegion(owner)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := store.AllocString(owner, region, "temporary-payload")
	if err != nil {
		t.Fatal(err)
	}
	before := store.PhysicalStats()
	if before.SlotSizeBytes >= before.SlotPayloadSizeBytes {
		t.Fatalf("slot header %d bytes is not smaller than payload union %d bytes", before.SlotSizeBytes, before.SlotPayloadSizeBytes)
	}
	if err := store.Free(owner, ref); err != nil {
		t.Fatal(err)
	}
	after := store.PhysicalStats()
	if after.ReservedSlotBytes != before.ReservedSlotBytes {
		t.Fatalf("reserved slot headers changed after free: %d -> %d", before.ReservedSlotBytes, after.ReservedSlotBytes)
	}
	if after.PayloadBytes != 0 || after.OccupiedSlotBytes != 0 || after.PayloadArenaSlabs != 0 || after.ReservedTypedPayloadBytes != 0 {
		t.Fatalf("vacant slot retained physical payload: %#v", after)
	}
}

func TestMapLenDoesNotRequirePayloadClone(t *testing.T) {
	store := NewStore(nil)
	owner := ownership.OwnerID{Kind: ownership.OwnerRealm, Value: 72}
	region, err := store.NewRegion(owner)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := store.AllocMap(owner, region)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MapSet(owner, ref, NumberValue(1), NumberValue(2)); err != nil {
		t.Fatal(err)
	}
	if length, err := store.MapLen(owner, ref); err != nil || length != 1 {
		t.Fatalf("MapLen = %d, %v", length, err)
	}
}
