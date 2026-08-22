package memory

import (
	"testing"
	"unsafe"

	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

func TestSlotEmbedsCompactPayloadHandle(t *testing.T) {
	if size := unsafe.Sizeof(slotPayload{}); size != 16 {
		t.Fatalf("compact payload handle is %d bytes, want 16", size)
	}
	if size := unsafe.Sizeof(Slot{}); size != 32 {
		t.Fatalf("slot is %d bytes, want 32", size)
	}
}

func TestHotSlotReuseDoesNotAllocatePayloadFacade(t *testing.T) {
	store := NewStore(nil)
	defer store.Close()
	owner := ownership.OwnerID{Kind: ownership.OwnerRealm, Value: 24_001}
	region, err := store.NewRegion(owner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AllocCell(owner, region); err != nil {
		t.Fatal(err)
	}
	allocations := testing.AllocsPerRun(100, func() {
		ref, allocErr := store.AllocCell(owner, region)
		if allocErr != nil {
			panic(allocErr)
		}
		if freeErr := store.Free(owner, ref); freeErr != nil {
			panic(freeErr)
		}
	})
	if allocations != 0 {
		t.Fatalf("hot slot allocate/free used %.2f Go allocations, want zero", allocations)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func TestTypedPayloadArenaReleasesEmptySlabs(t *testing.T) {
	var arena typedPayloadArena[StringObject]
	values := make([]*StringObject, payloadSlabCapacity+1)
	handles := make([]payloadHandle, len(values))
	for index := range values {
		values[index], handles[index] = arena.allocate()
		values[index].Text = "live"
	}
	if physical := arena.physical(); physical.slabs != 2 || physical.live != payloadSlabCapacity+1 {
		t.Fatalf("physical after allocation = %#v", physical)
	}
	for index := 0; index < payloadSlabCapacity; index++ {
		if err := arena.release(handles[index], values[index]); err != nil {
			t.Fatal(err)
		}
	}
	if physical := arena.physical(); physical.slabs != 1 || physical.live != 1 {
		t.Fatalf("physical after first slab release = %#v", physical)
	}
	if err := arena.check(1); err != nil {
		t.Fatal(err)
	}
	if err := arena.release(handles[payloadSlabCapacity], values[payloadSlabCapacity]); err != nil {
		t.Fatal(err)
	}
	if physical := arena.physical(); physical.slabs != 0 || physical.live != 0 || physical.reservedBytes != 0 {
		t.Fatalf("physical after complete release = %#v", physical)
	}
	if err := arena.check(0); err != nil {
		t.Fatal(err)
	}
}

func TestPayloadAllocatorTagsAndReleasesEveryHeapKind(t *testing.T) {
	var allocator payloadAllocator
	payloads := make(map[HeapKind]slotPayload)
	for kind := HeapCell; kind <= HeapHostObject; kind++ {
		payload := allocator.allocate(kind)
		payloads[kind] = payload
		if err := allocator.checkSlot(kind, &payload); err != nil {
			t.Fatalf("%s payload: %v", kind, err)
		}
	}
	corrupt := payloads[HeapCell]
	if err := allocator.checkSlot(HeapString, &corrupt); err == nil {
		t.Fatal("Cell payload handle resolved through String arena")
	}
	if physical := allocator.physical(); physical.live != uint64(HeapHostObject-HeapCell+1) || physical.slabs != uint64(HeapHostObject-HeapCell+1) {
		t.Fatalf("physical with every heap kind = %#v", physical)
	}
	for kind := HeapCell; kind <= HeapHostObject; kind++ {
		payload := payloads[kind]
		if err := allocator.release(kind, &payload); err != nil {
			t.Fatalf("release %s: %v", kind, err)
		}
	}
	if physical := allocator.physical(); physical.live != 0 || physical.slabs != 0 || physical.reservedBytes != 0 {
		t.Fatalf("physical after every heap kind release = %#v", physical)
	}
	if err := allocator.check(Stats{}); err != nil {
		t.Fatal(err)
	}
}
