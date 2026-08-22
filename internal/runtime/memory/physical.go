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
	RegionOutgoingIndexes     uint64 `json:"regionOutgoingIndexes"`
	RegionIncomingIndexes     uint64 `json:"regionIncomingIndexes"`
	WeakTableEntries          uint64 `json:"weakTableEntries"`
	WeakTargetEntries         uint64 `json:"weakTargetEntries"`
	WeakTargetReferences      uint64 `json:"weakTargetReferences"`
	WeakTargetUseBytes        uint64 `json:"weakTargetUseBytes"`
	// ObjectEdge and ObjectRegion fields are retained in the profile schema so
	// old baselines remain comparable. They stay zero now that typed payloads,
	// rather than a shadow ledger graph, own the complete heap topology.
	ObjectEdgeEntries           uint64 `json:"objectEdgeEntries"`
	ObjectEdgeReservedEntries   uint64 `json:"objectEdgeReservedEntries"`
	ObjectEdgeOccupiedBytes     uint64 `json:"objectEdgeOccupiedBytes"`
	ObjectEdgeReservedBytes     uint64 `json:"objectEdgeReservedBytes"`
	ObjectRegionEntries         uint64 `json:"objectRegionEntries"`
	PromotionEntries            uint64 `json:"promotionEntries"`
	PromotionSourceEntries      uint64 `json:"promotionSourceEntries"`
	PromotionDestinationEntries uint64 `json:"promotionDestinationEntries"`
	OwnerClaimEntries           uint64 `json:"ownerClaimEntries"`
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
		SlotSizeBytes:               uint64(unsafe.Sizeof(Slot{})),
		SlotPayloadSizeBytes:        uint64(unsafe.Sizeof(slotPayload{})),
		RefSizeBytes:                uint64(unsafe.Sizeof(Ref{})),
		ValueSizeBytes:              uint64(unsafe.Sizeof(Value{})),
		RegionRecords:               uint64(len(store.regions)),
		WeakTableEntries:            uint64(len(store.weakTables)),
		WeakTargetEntries:           uint64(len(store.weakTargets)),
		PromotionEntries:            uint64(len(store.promotions)),
		PromotionSourceEntries:      uint64(len(store.promotionsBySource)),
		PromotionDestinationEntries: uint64(len(store.promotionsByDestination)),
		OwnerClaimEntries:           uint64(len(store.owners)),
	}
	if store.barrier != nil {
		result.RegionEdgeEntries = uint64(len(store.barrier.edges))
		result.RegionOutgoingIndexes = uint64(len(store.barrier.outgoing))
		result.RegionIncomingIndexes = uint64(len(store.barrier.incoming))
	}
	for _, uses := range store.weakTargets {
		result.WeakTargetReferences += uint64(len(uses))
		result.WeakTargetUseBytes += uint64(cap(uses)) * uint64(unsafe.Sizeof(weakUse{}))
	}
	functionStorage := make(map[unsafe.Pointer]struct{})
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
			result.PayloadBytes += result.SlotPayloadSizeBytes + slotPayloadCapacityBytes(slot, functionStorage)
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
	result.AttributedBytes = result.ReservedSlotBytes + result.PayloadBytes + result.FreeListBytes + result.WeakTargetUseBytes
	return result
}

func slotPayloadCapacityBytes(slot *Slot, functionStorage map[unsafe.Pointer]struct{}) uint64 {
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
		bytes += uniqueSliceCapacityBytes(slot.Function.Code, functionStorage)
		bytes += uniqueSliceCapacityBytes(slot.Function.Locations, functionStorage)
		bytes += uniqueSliceCapacityBytes(slot.Function.Constants, functionStorage)
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
		bytes += sliceCapacityBytes(slot.WeakSet.uses)
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

func uniqueSliceCapacityBytes[T any](values []T, seen map[unsafe.Pointer]struct{}) uint64 {
	if cap(values) == 0 {
		return 0
	}
	pointer := unsafe.Pointer(unsafe.SliceData(values))
	if _, exists := seen[pointer]; exists {
		return 0
	}
	seen[pointer] = struct{}{}
	return sliceCapacityBytes(values)
}
