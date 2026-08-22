package memory

import "testing"

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
	payloads := make(map[HeapKind]*slotPayload)
	for kind := HeapCell; kind <= HeapHostObject; kind++ {
		payload := allocator.allocate(kind)
		payloads[kind] = payload
		slot := &Slot{Kind: kind, Occupied: true, slotPayload: payload}
		if slotHasOtherPayload(slot, kind) {
			t.Fatalf("%s payload has missing or additional typed pointer", kind)
		}
		if err := allocator.checkSlot(kind, payload); err != nil {
			t.Fatalf("%s payload: %v", kind, err)
		}
	}
	if physical := allocator.physical(); physical.live != uint64(HeapHostObject-HeapCell+1) || physical.slabs != uint64(HeapHostObject-HeapCell+1) {
		t.Fatalf("physical with every heap kind = %#v", physical)
	}
	for kind := HeapCell; kind <= HeapHostObject; kind++ {
		if err := allocator.release(kind, payloads[kind]); err != nil {
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
