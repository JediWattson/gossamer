package ownership

import (
	"fmt"
	"math/bits"
	"sort"
)

const (
	edgeArenaSlabShift  = 8
	edgeArenaSlabSize   = 1 << edgeArenaSlabShift
	maxEdgeArenaSlabs   = (1 << (32 - edgeArenaSlabShift)) - 1
	edgeLookupThreshold = 8
)

type edgeHandle uint32

// edgeRecord is one semantic shadow-graph edge. Outgoing and incoming
// adjacency share this record, so the ledger does not allocate two Go map
// entries per relationship. RegionStore heap fields are not stored here.
type edgeRecord struct {
	from ObjectID
	to   ObjectID

	nextOut edgeHandle
	prevOut edgeHandle
	nextIn  edgeHandle
	prevIn  edgeHandle
}

type edgeSlab struct {
	records           [edgeArenaSlabSize]edgeRecord
	used              [edgeArenaSlabSize / 64]uint64
	live              uint16
	availablePosition int32
}

// edgeArena stores independently reclaimable fixed-size slabs so growing the
// graph never copies existing edges and teardown can return empty slabs
// without invalidating handles in the remaining graph. Handle zero is the
// end-of-list sentinel.
type edgeArena struct {
	slabs     []*edgeSlab
	available []uint32
	freeSlabs []uint32
	live      uint64
}

func (arena *edgeArena) allocate(from, to ObjectID) (edgeHandle, error) {
	if len(arena.available) == 0 {
		if err := arena.addSlab(); err != nil {
			return 0, err
		}
	}
	slabIndex := arena.available[len(arena.available)-1]
	slab := arena.slabs[slabIndex]
	recordIndex := -1
	for wordIndex, word := range slab.used {
		if word == ^uint64(0) {
			continue
		}
		recordIndex = wordIndex*64 + bits.TrailingZeros64(^word)
		break
	}
	if recordIndex < 0 || recordIndex >= edgeArenaSlabSize {
		panic("ownership: edge slab marked available without a free record")
	}
	wordIndex := recordIndex / 64
	bit := uint(recordIndex % 64)
	slab.used[wordIndex] |= uint64(1) << bit
	slab.live++
	if int(slab.live) == edgeArenaSlabSize {
		arena.removeAvailable(slabIndex)
	}
	handle := makeEdgeHandle(slabIndex, uint8(recordIndex))
	record := &slab.records[recordIndex]
	record.from = from
	record.to = to
	arena.live++
	return handle, nil
}

func (arena *edgeArena) release(handle edgeHandle) {
	if handle == 0 {
		return
	}
	slabIndex, recordIndex := splitEdgeHandle(handle)
	slab := arena.slabs[slabIndex]
	wordIndex := int(recordIndex) / 64
	bit := uint(recordIndex % 64)
	mask := uint64(1) << bit
	if slab == nil || slab.used[wordIndex]&mask == 0 {
		panic("ownership: releasing stale edge handle")
	}
	wasFull := int(slab.live) == edgeArenaSlabSize
	slab.records[recordIndex] = edgeRecord{}
	slab.used[wordIndex] &^= mask
	slab.live--
	arena.live--
	if slab.live == 0 {
		if slab.availablePosition >= 0 {
			arena.removeAvailable(slabIndex)
		}
		arena.slabs[slabIndex] = nil
		arena.freeSlabs = append(arena.freeSlabs, slabIndex)
	} else if wasFull {
		arena.appendAvailable(slabIndex)
	}
	if arena.live == 0 {
		arena.slabs = nil
		arena.available = nil
		arena.freeSlabs = nil
	}
}

func (arena *edgeArena) record(handle edgeHandle) *edgeRecord {
	slabIndex, recordIndex := splitEdgeHandle(handle)
	return &arena.slabs[slabIndex].records[recordIndex]
}

func (arena *edgeArena) addSlab() error {
	slab := &edgeSlab{availablePosition: -1}
	var index uint32
	if len(arena.freeSlabs) != 0 {
		index = arena.freeSlabs[len(arena.freeSlabs)-1]
		arena.freeSlabs = arena.freeSlabs[:len(arena.freeSlabs)-1]
		arena.slabs[index] = slab
	} else {
		if len(arena.slabs) >= maxEdgeArenaSlabs {
			return fmt.Errorf("ownership: exhausted reference edge handles")
		}
		index = uint32(len(arena.slabs))
		arena.slabs = append(arena.slabs, slab)
	}
	arena.appendAvailable(index)
	return nil
}

func (arena *edgeArena) appendAvailable(slabIndex uint32) {
	slab := arena.slabs[slabIndex]
	if slab == nil || slab.availablePosition >= 0 {
		panic("ownership: invalid edge slab available insertion")
	}
	slab.availablePosition = int32(len(arena.available))
	arena.available = append(arena.available, slabIndex)
}

func (arena *edgeArena) removeAvailable(slabIndex uint32) {
	slab := arena.slabs[slabIndex]
	position := int(slab.availablePosition)
	if position < 0 || position >= len(arena.available) || arena.available[position] != slabIndex {
		panic("ownership: invalid edge slab available removal")
	}
	lastPosition := len(arena.available) - 1
	lastIndex := arena.available[lastPosition]
	arena.available[position] = lastIndex
	arena.available = arena.available[:lastPosition]
	slab.availablePosition = -1
	if position != lastPosition {
		arena.slabs[lastIndex].availablePosition = int32(position)
	}
}

func makeEdgeHandle(slab uint32, record uint8) edgeHandle {
	return edgeHandle((slab<<edgeArenaSlabShift)|uint32(record)) + 1
}

func splitEdgeHandle(handle edgeHandle) (uint32, uint8) {
	value := uint32(handle - 1)
	return value >> edgeArenaSlabShift, uint8(value)
}

func (ledger *Ledger) findEdgeLocked(from *objectRecord, to ObjectID) (edgeHandle, int) {
	if from.edgeLookup != nil {
		return from.edgeLookup[to], -1
	}
	degree := 0
	for handle := from.outgoing; handle != 0; handle = ledger.edges.record(handle).nextOut {
		degree++
		if ledger.edges.record(handle).to == to {
			return handle, degree
		}
	}
	return 0, degree
}

func (ledger *Ledger) attachEdgeLocked(from, to *objectRecord, handle edgeHandle, degree int) {
	record := ledger.edges.record(handle)
	record.nextOut = from.outgoing
	if from.outgoing != 0 {
		ledger.edges.record(from.outgoing).prevOut = handle
	}
	from.outgoing = handle

	record.nextIn = to.incoming
	if to.incoming != 0 {
		ledger.edges.record(to.incoming).prevIn = handle
	}
	to.incoming = handle

	if from.edgeLookup != nil {
		from.edgeLookup[record.to] = handle
		return
	}
	if degree+1 < edgeLookupThreshold {
		return
	}
	from.edgeLookup = make(map[ObjectID]edgeHandle, degree+1)
	for edge := from.outgoing; edge != 0; edge = ledger.edges.record(edge).nextOut {
		from.edgeLookup[ledger.edges.record(edge).to] = edge
	}
}

func (ledger *Ledger) detachEdgeLocked(handle edgeHandle, from, to *objectRecord) {
	record := *ledger.edges.record(handle)
	if from != nil {
		if record.prevOut == 0 {
			from.outgoing = record.nextOut
		} else {
			ledger.edges.record(record.prevOut).nextOut = record.nextOut
		}
		if record.nextOut != 0 {
			ledger.edges.record(record.nextOut).prevOut = record.prevOut
		}
		if from.edgeLookup != nil {
			delete(from.edgeLookup, record.to)
			if len(from.edgeLookup) < edgeLookupThreshold {
				from.edgeLookup = nil
			}
		}
	}
	if to != nil {
		if record.prevIn == 0 {
			to.incoming = record.nextIn
		} else {
			ledger.edges.record(record.prevIn).nextIn = record.nextIn
		}
		if record.nextIn != 0 {
			ledger.edges.record(record.nextIn).prevIn = record.prevIn
		}
	}
	ledger.edges.release(handle)
}

func (ledger *Ledger) sortedOutgoingTargetsLocked(object *objectRecord) []ObjectID {
	targets := make([]ObjectID, 0)
	for handle := object.outgoing; handle != 0; handle = ledger.edges.record(handle).nextOut {
		targets = append(targets, ledger.edges.record(handle).to)
	}
	sortObjectIDs(targets)
	return targets
}

func sortObjectIDs(objects []ObjectID) {
	sort.Slice(objects, func(left, right int) bool { return objects[left] < objects[right] })
}
