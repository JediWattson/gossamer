package memory

import "unsafe"

// PhysicalStats attributes the Store-owned memory that can be measured
// without relying on Go runtime heap sampling. It intentionally reports map
// entry counts separately: Go's map bucket overhead is implementation-defined
// and belongs in the process heap profile rather than an invented byte total.
type PhysicalStats struct {
	SlotSizeBytes             uint64 `json:"slotSizeBytes"`
	SlotPayloadSizeBytes      uint64 `json:"slotPayloadSizeBytes"`
	RefSizeBytes              uint64 `json:"refSizeBytes"`
	ValueSizeBytes            uint64 `json:"valueSizeBytes"`
	ReservedSlotBytes         uint64 `json:"reservedSlotBytes"`
	OccupiedSlotBytes         uint64 `json:"occupiedSlotBytes"`
	PayloadBytes              uint64 `json:"payloadBytes"`
	PayloadArenaSlabs         uint64 `json:"payloadArenaSlabs"`
	ReservedTypedPayloadBytes uint64 `json:"reservedTypedPayloadBytes"`
	OccupiedTypedPayloadBytes uint64 `json:"occupiedTypedPayloadBytes"`
	FreeListBytes             uint64 `json:"freeListBytes"`
	AttributedBytes           uint64 `json:"attributedBytes"`
	RegionRecords             uint64 `json:"regionRecords"`
	PooledSlotBuffers         uint64 `json:"pooledSlotBuffers"`
	RegionEdgeEntries         uint64 `json:"regionEdgeEntries"`
	ObjectEdgeEntries         uint64 `json:"objectEdgeEntries"`
	ObjectRegionEntries       uint64 `json:"objectRegionEntries"`
	PromotionEntries          uint64 `json:"promotionEntries"`
	OwnerClaimEntries         uint64 `json:"ownerClaimEntries"`
}

// PhysicalStats scans the Store at a profiling checkpoint. Ordinary runtime
// telemetry continues to use Stats, whose counters are O(1) to read.
func (store *Store) PhysicalStats() PhysicalStats {
	if store == nil {
		return PhysicalStats{}
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()

	result := PhysicalStats{
		SlotSizeBytes:        uint64(unsafe.Sizeof(Slot{})),
		SlotPayloadSizeBytes: uint64(unsafe.Sizeof(slotPayload{})),
		RefSizeBytes:         uint64(unsafe.Sizeof(Ref{})),
		ValueSizeBytes:       uint64(unsafe.Sizeof(Value{})),
		RegionRecords:        uint64(len(store.regions)),
		ObjectEdgeEntries:    uint64(len(store.objectEdges)),
		ObjectRegionEntries:  uint64(len(store.objectRegions)),
		PromotionEntries:     uint64(len(store.promotions)),
		OwnerClaimEntries:    uint64(len(store.ownerClaims)),
	}
	if store.barrier != nil {
		result.RegionEdgeEntries = uint64(len(store.barrier.edges))
	}
	for _, region := range store.regions {
		if region == nil {
			continue
		}
		result.ReservedSlotBytes += uint64(cap(region.Slots)) * result.SlotSizeBytes
		result.FreeListBytes += uint64(cap(region.free)) * uint64(unsafe.Sizeof(uint32(0)))
		for index := range region.Slots {
			slot := &region.Slots[index]
			if !slot.Occupied {
				continue
			}
			result.OccupiedSlotBytes += result.SlotSizeBytes
			result.PayloadBytes += result.SlotPayloadSizeBytes + slotPayloadCapacityBytes(slot)
		}
	}
	for _, buffer := range store.slotBuffers {
		result.PooledSlotBuffers++
		result.ReservedSlotBytes += uint64(cap(buffer)) * result.SlotSizeBytes
	}
	typedPayloads := store.payloads.physical()
	result.PayloadArenaSlabs = typedPayloads.slabs
	result.ReservedTypedPayloadBytes = typedPayloads.reservedBytes
	result.OccupiedTypedPayloadBytes = typedPayloads.occupiedBytes
	result.PayloadBytes += typedPayloads.reservedBytes
	result.AttributedBytes = result.ReservedSlotBytes + result.PayloadBytes + result.FreeListBytes
	return result
}

func slotPayloadCapacityBytes(slot *Slot) uint64 {
	if slot == nil || !slot.Occupied {
		return 0
	}
	bytes := objectHeaderCapacityBytes(slot)
	switch slot.Kind {
	case HeapCell:
		bytes += sliceCapacityBytes(slot.Cell.Fields)
	case HeapString:
		bytes += uint64(len(slot.String.Text))
	case HeapArray:
		bytes += sliceCapacityBytes(slot.Array.Elements)
	case HeapContext:
		bytes += sliceCapacityBytes(slot.Context.Bindings)
	case HeapFunction:
		bytes += uint64(cap(slot.Function.Code))
		bytes += sliceCapacityBytes(slot.Function.Locations)
		bytes += sliceCapacityBytes(slot.Function.Constants)
		bytes += sliceCapacityBytes(slot.Function.Captures)
	case HeapPromise:
		bytes += sliceCapacityBytes(slot.Promise.Reactions)
	case HeapBigInt:
		bytes += uint64(cap(slot.BigInt.Magnitude))
	case HeapArrayBuffer:
		bytes += uint64(cap(slot.ArrayBuffer.Bytes))
	case HeapMap:
		bytes += sliceCapacityBytes(slot.Map.Entries)
	case HeapSet:
		bytes += sliceCapacityBytes(slot.Set.Values)
	case HeapError:
		bytes += sliceCapacityBytes(slot.Error.Errors)
	case HeapWeakMap:
		bytes += sliceCapacityBytes(slot.WeakMap.Entries)
	case HeapWeakSet:
		bytes += sliceCapacityBytes(slot.WeakSet.Keys)
	}
	return bytes
}

func objectHeaderCapacityBytes(slot *Slot) uint64 {
	header, ok := objectHeaderForSlot(slot)
	if !ok {
		return 0
	}
	return sliceCapacityBytes(header.Properties)
}

func sliceCapacityBytes[T any](values []T) uint64 {
	var value T
	return uint64(cap(values)) * uint64(unsafe.Sizeof(value))
}
