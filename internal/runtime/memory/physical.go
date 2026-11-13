package memory

import "unsafe"

// PhysicalStats attributes the Store-owned memory that can be measured
// without relying on Go runtime heap sampling. It intentionally reports map
// entry counts separately: Go's map bucket overhead is implementation-defined
// and belongs in the process heap profile rather than an invented byte total.
type PhysicalStats struct {
	SlotSizeBytes               uint64 `json:"slotSizeBytes"`
	SlotPayloadSizeBytes        uint64 `json:"slotPayloadSizeBytes"`
	SlotPayloadHandleSizeBytes  uint64 `json:"slotPayloadHandleSizeBytes"`
	RefSizeBytes                uint64 `json:"refSizeBytes"`
	ValueSizeBytes              uint64 `json:"valueSizeBytes"`
	PropertySizeBytes           uint64 `json:"propertySizeBytes"`
	ReservedSlotBytes           uint64 `json:"reservedSlotBytes"`
	OccupiedSlotBytes           uint64 `json:"occupiedSlotBytes"`
	PayloadBytes                uint64 `json:"payloadBytes"`
	FunctionCodeBytes           uint64 `json:"functionCodeBytes"`
	FunctionSourceLocationBytes uint64 `json:"functionSourceLocationBytes"`
	FunctionConstantBytes       uint64 `json:"functionConstantBytes"`
	FunctionCaptureBytes        uint64 `json:"functionCaptureBytes"`
	PayloadArenaSlabs           uint64 `json:"payloadArenaSlabs"`
	ReservedTypedPayloadBytes   uint64 `json:"reservedTypedPayloadBytes"`
	OccupiedTypedPayloadBytes   uint64 `json:"occupiedTypedPayloadBytes"`
	FreeListBytes               uint64 `json:"freeListBytes"`
	AttributedBytes             uint64 `json:"attributedBytes"`
	RegionRecords               uint64 `json:"regionRecords"`
	PooledSlotBuffers           uint64 `json:"pooledSlotBuffers"`
	RegionEdgeEntries           uint64 `json:"regionEdgeEntries"`
	RegionOutgoingIndexes       uint64 `json:"regionOutgoingIndexes"`
	RegionIncomingIndexes       uint64 `json:"regionIncomingIndexes"`
	WeakTableEntries            uint64 `json:"weakTableEntries"`
	WeakTargetEntries           uint64 `json:"weakTargetEntries"`
	WeakTargetReferences        uint64 `json:"weakTargetReferences"`
	WeakTargetUseBytes          uint64 `json:"weakTargetUseBytes"`
	SemanticLookupIndexes       uint64 `json:"semanticLookupIndexes"`
	SemanticLookupEntries       uint64 `json:"semanticLookupEntries"`
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
		SlotPayloadSizeBytes:        0,
		SlotPayloadHandleSizeBytes:  uint64(unsafe.Sizeof(slotPayload{})),
		RefSizeBytes:                uint64(unsafe.Sizeof(Ref{})),
		ValueSizeBytes:              uint64(unsafe.Sizeof(Value{})),
		PropertySizeBytes:           uint64(unsafe.Sizeof(Property{})),
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
	functionCodeStorage := make(map[unsafe.Pointer]struct{})
	functionLocationStorage := make(map[unsafe.Pointer]struct{})
	functionConstantStorage := make(map[unsafe.Pointer]struct{})
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
			result.PayloadBytes += slotPayloadCapacityBytes(slot, functionStorage)
			if header, ok := objectHeaderForSlot(slot); ok && header.propertyLookup != nil {
				result.SemanticLookupIndexes++
				result.SemanticLookupEntries += uint64(len(header.propertyLookup))
			}
			if slot.Kind == HeapContext && slot.context().bindingLookup != nil {
				result.SemanticLookupIndexes++
				result.SemanticLookupEntries += uint64(len(slot.context().bindingLookup))
			}
			if slot.Kind == HeapFunction {
				result.FunctionCodeBytes += uniqueSliceCapacityBytes(slot.function().Code, functionCodeStorage)
				result.FunctionSourceLocationBytes += uniqueSliceCapacityBytes(slot.function().Locations.storage(), functionLocationStorage)
				result.FunctionConstantBytes += uniqueSliceCapacityBytes(slot.function().Constants, functionConstantStorage)
				result.FunctionCaptureBytes += sliceCapacityBytes(slot.function().Captures)
			}
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
		bytes += sliceCapacityBytes(slot.cell().Fields)
	case HeapString:
		bytes += uint64(len(slot.stringObject().Text))
	case HeapArray:
		bytes += sliceCapacityBytes(slot.array().Elements)
	case HeapContext:
		bytes += sliceCapacityBytes(slot.context().Bindings)
	case HeapFunction:
		bytes += uniqueSliceCapacityBytes(slot.function().Code, functionStorage)
		bytes += uniqueSliceCapacityBytes(slot.function().Locations.storage(), functionStorage)
		bytes += uniqueSliceCapacityBytes(slot.function().Constants, functionStorage)
		bytes += sliceCapacityBytes(slot.function().Captures)
	case HeapPromise:
		bytes += sliceCapacityBytes(slot.promise().Reactions)
	case HeapBigInt:
		bytes += uint64(cap(slot.bigInt().Magnitude))
	case HeapArrayBuffer:
		bytes += uint64(cap(slot.arrayBuffer().Bytes))
	case HeapMap:
		bytes += sliceCapacityBytes(slot.nativeMap().Entries)
	case HeapSet:
		bytes += sliceCapacityBytes(slot.nativeSet().Values)
	case HeapError:
		bytes += sliceCapacityBytes(slot.errorObject().Errors)
	case HeapWeakMap:
		bytes += sliceCapacityBytes(slot.weakMap().Entries)
	case HeapWeakSet:
		bytes += sliceCapacityBytes(slot.weakSet().Keys)
		bytes += sliceCapacityBytes(slot.weakSet().uses)
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
