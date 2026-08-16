package memory

const (
	profiledRegionSlotCapacity = 8
	maxPooledSlotCapacity      = 256
	maxPooledSlotBuffers       = 16
)

// ensureRegionSlotCapacityLocked gives short-lived regions an explicitly
// sized slot buffer. The joint browser profile's native task graph uses seven
// slots, so an eight-slot first page removes append growth while remaining
// small. Larger regions grow geometrically without changing Ref identity.
func (store *Store) ensureRegionSlotCapacityLocked(region *Region, needed int) {
	if cap(region.Slots) >= needed {
		return
	}
	target := profiledRegionSlotCapacity
	if current := cap(region.Slots); current > target {
		target = current
	}
	for target < needed {
		target *= 2
	}

	buffer := store.takeSlotBufferLocked(target)
	if buffer == nil {
		buffer = make([]Slot, 0, target)
		store.stats.SlotBufferAllocations++
		store.stats.ReservedSlotCapacity += uint64(cap(buffer))
		if store.stats.ReservedSlotCapacity > store.stats.PeakReservedSlotCapacity {
			store.stats.PeakReservedSlotCapacity = store.stats.ReservedSlotCapacity
		}
	}
	buffer = buffer[:len(region.Slots)]
	copy(buffer, region.Slots)
	old := region.Slots
	region.Slots = buffer
	if cap(old) != 0 {
		store.stats.SlotBufferGrows++
		store.releaseSlotBufferLocked(old)
	}
}

func (store *Store) takeSlotBufferLocked(minimum int) []Slot {
	best := -1
	for index, buffer := range store.slotBuffers {
		if cap(buffer) < minimum {
			continue
		}
		if best == -1 || cap(buffer) < cap(store.slotBuffers[best]) {
			best = index
		}
	}
	if best == -1 {
		return nil
	}
	buffer := store.slotBuffers[best]
	copy(store.slotBuffers[best:], store.slotBuffers[best+1:])
	store.slotBuffers[len(store.slotBuffers)-1] = nil
	store.slotBuffers = store.slotBuffers[:len(store.slotBuffers)-1]
	store.stats.SlotBufferReuses++
	store.stats.PooledSlotBuffers--
	store.stats.PooledSlotCapacity -= uint64(cap(buffer))
	return buffer[:0]
}

func (store *Store) releaseSlotBufferLocked(buffer []Slot) {
	if cap(buffer) == 0 {
		return
	}
	buffer = buffer[:cap(buffer)]
	clear(buffer)
	capacity := cap(buffer)
	if capacity > maxPooledSlotCapacity || len(store.slotBuffers) >= maxPooledSlotBuffers {
		store.stats.ReservedSlotCapacity -= uint64(capacity)
		return
	}
	store.slotBuffers = append(store.slotBuffers, buffer[:0])
	store.stats.PooledSlotBuffers++
	store.stats.PooledSlotCapacity += uint64(capacity)
}

func (store *Store) releaseAllSlotBuffersLocked() {
	store.slotBuffers = nil
	store.stats.ReservedSlotCapacity -= store.stats.PooledSlotCapacity
	store.stats.PooledSlotBuffers = 0
	store.stats.PooledSlotCapacity = 0
}
