package ownership

import "unsafe"

// PhysicalStats attributes the compact semantic edge arena without pretending
// to know Go's map-bucket overhead. These gauges are separate from Stats,
// whose fields describe ownership operations and live semantic objects.
type PhysicalStats struct {
	EdgeRecordSizeBytes      uint64 `json:"edgeRecordSizeBytes"`
	EdgeSlabSizeBytes        uint64 `json:"edgeSlabSizeBytes"`
	LiveEdgeEntries          uint64 `json:"liveEdgeEntries"`
	EdgeArenaSlabs           uint64 `json:"edgeArenaSlabs"`
	EdgeArenaOccupiedBytes   uint64 `json:"edgeArenaOccupiedBytes"`
	EdgeArenaIndexBytes      uint64 `json:"edgeArenaIndexBytes"`
	EdgeArenaReservedBytes   uint64 `json:"edgeArenaReservedBytes"`
	EdgeArenaAttributedBytes uint64 `json:"edgeArenaAttributedBytes"`
}

// PhysicalStats returns exact arena backing-storage gauges at a profiling
// checkpoint. Object, region, and high-degree lookup-map storage remains in
// the process heap because its bucket sizes are an implementation detail of
// Go; their logical live-object count remains available through Stats.
func (ledger *Ledger) PhysicalStats() PhysicalStats {
	if ledger == nil {
		return PhysicalStats{}
	}
	ledger.mutex.Lock()
	defer ledger.mutex.Unlock()

	recordSize := uint64(unsafe.Sizeof(edgeRecord{}))
	slabSize := uint64(unsafe.Sizeof(edgeSlab{}))
	pointerSize := uint64(unsafe.Sizeof((*edgeSlab)(nil)))
	indexSize := uint64(unsafe.Sizeof(uint32(0)))
	// Every nil slab slot appears exactly once in freeSlabs, and the arena
	// resets both slices together when its final edge is released.
	liveSlabs := uint64(len(ledger.edges.slabs) - len(ledger.edges.freeSlabs))
	indexBytes := uint64(cap(ledger.edges.slabs))*pointerSize +
		uint64(cap(ledger.edges.available)+cap(ledger.edges.freeSlabs))*indexSize
	reserved := liveSlabs*slabSize + indexBytes
	return PhysicalStats{
		EdgeRecordSizeBytes:      recordSize,
		EdgeSlabSizeBytes:        slabSize,
		LiveEdgeEntries:          ledger.edges.live,
		EdgeArenaSlabs:           liveSlabs,
		EdgeArenaOccupiedBytes:   ledger.edges.live * recordSize,
		EdgeArenaIndexBytes:      indexBytes,
		EdgeArenaReservedBytes:   reserved,
		EdgeArenaAttributedBytes: reserved,
	}
}
