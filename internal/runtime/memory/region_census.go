package memory

import (
	"fmt"
	"sort"

	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

// RegionCensus is a point-in-time, read-only attribution of one physical
// region. Activity counters remain Store-wide; the embedded Stats contains
// only the live value, kind, and byte fields attributable to this region.
type RegionCensus struct {
	ID           RegionID            `json:"id"`
	Owner        ownership.OwnerID   `json:"owner"`
	Lifetime     ownership.OwnerKind `json:"lifetime"`
	State        RegionState         `json:"state"`
	Slots        uint64              `json:"slots"`
	SlotCapacity uint64              `json:"slotCapacity"`
	FreeSlots    uint64              `json:"freeSlots"`
	Live         Stats               `json:"live"`
}

// RegionCensus reports the live payload mix of one region without cloning its
// slots or payloads. It is intended for sparse profiling samples, not hot-path
// dispatch.
func (store *Store) RegionCensus(regionID RegionID) (RegionCensus, error) {
	if store == nil {
		return RegionCensus{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	region := store.regions[regionID]
	if region == nil {
		return RegionCensus{}, fmt.Errorf("%w: R%d", ErrUnknownRegion, regionID)
	}
	return regionCensusLocked(region), nil
}

// RegionCensuses reports every live physical region in deterministic ID order.
// One Store lock covers the complete snapshot so totals can be compared with
// Stats without mixing allocations from different task checkpoints.
func (store *Store) RegionCensuses() []RegionCensus {
	if store == nil {
		return nil
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	result := make([]RegionCensus, 0, len(store.regions))
	for _, region := range store.regions {
		if region != nil && region.State != RegionDestroyed {
			result = append(result, regionCensusLocked(region))
		}
	}
	sort.Slice(result, func(left, right int) bool { return result[left].ID < result[right].ID })
	return result
}

func regionCensusLocked(region *Region) RegionCensus {
	census := RegionCensus{
		ID:           region.ID,
		Owner:        region.Owner,
		Lifetime:     region.Lifetime,
		State:        region.State,
		Slots:        uint64(len(region.Slots)),
		SlotCapacity: uint64(cap(region.Slots)),
		FreeSlots:    uint64(len(region.free)),
	}
	if region.State == RegionDestroyed {
		return census
	}
	for index := range region.Slots {
		slot := &region.Slots[index]
		if !slot.Occupied {
			continue
		}
		census.Live.LiveSlots++
		recordCensusKind(&census.Live, slot.Kind)
		census.Live.LiveBytes += slotDynamicBytes(slot)
	}
	return census
}

func recordCensusKind(stats *Stats, kind HeapKind) {
	if stats == nil {
		return
	}
	switch kind {
	case HeapCell:
		stats.LiveCells++
	case HeapString:
		stats.LiveStrings++
	case HeapObject:
		stats.LiveObjects++
	case HeapArray:
		stats.LiveArrays++
	case HeapContext:
		stats.LiveContexts++
	case HeapFunction:
		stats.LiveFunctions++
	case HeapPromise:
		stats.LivePromises++
	case HeapBigInt:
		stats.LiveBigInts++
	case HeapSymbol:
		stats.LiveSymbols++
	case HeapArrayBuffer:
		stats.LiveArrayBuffers++
	case HeapTypedArray:
		stats.LiveTypedArrays++
	case HeapMap:
		stats.LiveMaps++
	case HeapSet:
		stats.LiveSets++
	case HeapDate:
		stats.LiveDates++
	case HeapRegExp:
		stats.LiveRegExps++
	case HeapError:
		stats.LiveErrors++
	case HeapWeakMap:
		stats.LiveWeakMaps++
	case HeapWeakSet:
		stats.LiveWeakSets++
	case HeapIterator:
		stats.LiveIterators++
	case HeapHostObject:
		stats.LiveHostObjects++
	}
}

func slotDynamicBytes(slot *Slot) uint64 {
	if slot == nil || !slot.Occupied {
		return 0
	}
	switch slot.Kind {
	case HeapString:
		return uint64(len(slot.stringObject().Text))
	case HeapFunction:
		return uint64(len(slot.function().Code)) + slot.function().Locations.StorageBytes()
	case HeapBigInt:
		return uint64(len(slot.bigInt().Magnitude))
	case HeapArrayBuffer:
		return uint64(len(slot.arrayBuffer().Bytes))
	default:
		return 0
	}
}
