package memory

import (
	"fmt"
	"math"
	"sort"

	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

// Collection reports one deterministic owner-local tracing checkpoint.
type Collection struct {
	Roots              uint64 `json:"roots"`
	MarkedSlots        uint64 `json:"markedSlots"`
	ReclaimedSlots     uint64 `json:"reclaimedSlots"`
	ReclaimedBytes     uint64 `json:"reclaimedBytes"`
	ClearedWeakEntries uint64 `json:"clearedWeakEntries"`
}

// Collect reclaims private slots owned by owner that are unreachable from the
// supplied semantic roots. Objects with a claim from another owner are also
// roots. All outgoing edges from the unreachable set are removed before any
// slot is released, which allows self-cycles and multi-object cycles to be
// reclaimed without trial reference-count mutations.
//
// Callers must invoke Collect at an ordered-executor checkpoint where roots
// cannot change concurrently. The Store mutex keeps the physical snapshot
// internally atomic.
func (store *Store) Collect(owner ownership.OwnerID, roots ...Ref) (Collection, error) {
	if store == nil {
		return Collection{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return store.collectLocked(owner, 0, roots...)
}

// CollectRegion reclaims unreachable slots from one private physical region.
// Other regions owned by the same semantic owner remain outside the sweep and
// their incoming references are treated as roots. This is the checkpoint used
// by a Realm whose browser-owned async envelopes share its OwnerID.
func (store *Store) CollectRegion(owner ownership.OwnerID, regionID RegionID, roots ...Ref) (Collection, error) {
	if store == nil {
		return Collection{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	region := store.regions[regionID]
	if region == nil {
		return Collection{}, fmt.Errorf("%w: R%d", ErrUnknownRegion, regionID)
	}
	if region.State != RegionPrivate || region.Owner != owner {
		return Collection{}, store.accessError(region, owner)
	}
	return store.collectLocked(owner, regionID, roots...)
}

func (store *Store) collectLocked(owner ownership.OwnerID, regionID RegionID, roots ...Ref) (Collection, error) {
	if store.closed {
		return Collection{}, ErrStoreClosed
	}
	if store.closedOwners[owner] {
		return Collection{}, fmt.Errorf("%w: %s", ownership.ErrRegionClosed, owner)
	}

	owned := make(map[Ref]struct{})
	byObject := make(map[ownership.ObjectID]Ref)
	for _, id := range sortedRegionIDs(store.regions) {
		region := store.regions[id]
		if region == nil || region.State != RegionPrivate || region.Owner != owner || regionID != 0 && id != regionID {
			continue
		}
		for index := range region.Slots {
			slot := &region.Slots[index]
			if !slot.Occupied {
				continue
			}
			ref := Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}
			owned[ref] = struct{}{}
			byObject[slot.object] = ref
		}
	}

	queue := make([]Ref, 0, len(roots))
	rootSet := make(map[Ref]struct{}, len(roots))
	for _, root := range uniqueRefs(roots) {
		region, _, err := store.slotLocked(root)
		if err != nil {
			return Collection{}, err
		}
		if region.State == RegionInTransit {
			return Collection{}, fmt.Errorf("%w: R%d", ErrRegionInTransit, region.ID)
		}
		if region.State == RegionPrivate && region.Owner != owner {
			return Collection{}, store.accessError(region, owner)
		}
		rootSet[root] = struct{}{}
		queue = append(queue, root)
	}

	for object, ref := range byObject {
		snapshot, err := store.ledger.Object(object)
		if err != nil {
			return Collection{}, err
		}
		if snapshot.References > 1 {
			if _, exists := rootSet[ref]; !exists {
				rootSet[ref] = struct{}{}
				queue = append(queue, ref)
			}
		}
	}
	for edge, count := range store.objectEdges {
		if count == 0 {
			continue
		}
		target, targetOwned := byObject[edge.to]
		_, sourceOwned := byObject[edge.from]
		if targetOwned && !sourceOwned {
			if _, exists := rootSet[target]; !exists {
				rootSet[target] = struct{}{}
				queue = append(queue, target)
			}
		}
	}

	marked := make(map[Ref]struct{})
	seen := make(map[Ref]struct{})
	drainStrongReferences := func() error {
		for len(queue) != 0 {
			ref := queue[0]
			queue = queue[1:]
			if _, exists := seen[ref]; exists {
				continue
			}
			seen[ref] = struct{}{}
			_, slot, err := store.slotLocked(ref)
			if err != nil {
				return err
			}
			if _, isOwned := owned[ref]; isOwned {
				marked[ref] = struct{}{}
			}
			for _, value := range slotReferences(slot) {
				if value.IsRef() {
					queue = append(queue, value.Ref())
				}
			}
		}
		return nil
	}
	if err := drainStrongReferences(); err != nil {
		return Collection{}, err
	}

	// Ephemeron values are traced only after both their table and key have
	// become live through ordinary strong reachability. Repeat to a fixed point
	// because one ephemeron value may reveal the key of another entry.
	for {
		added := false
		for ref := range marked {
			_, slot, _ := store.slotLocked(ref)
			if slot.Kind != HeapWeakMap {
				continue
			}
			for _, entry := range slot.WeakMap.Entries {
				if !store.weakKeyLiveLocked(entry.Key, owned, marked) || !store.weakValueValidLocked(entry.Value) {
					continue
				}
				if entry.Value.IsRef() {
					valueRef := entry.Value.Ref()
					if _, alreadySeen := seen[valueRef]; !alreadySeen {
						queue = append(queue, valueRef)
						added = true
					}
				}
			}
		}
		if !added {
			break
		}
		if err := drainStrongReferences(); err != nil {
			return Collection{}, err
		}
	}

	result := Collection{Roots: uint64(len(rootSet)), MarkedSlots: uint64(len(marked))}
	for ref := range marked {
		_, slot, _ := store.slotLocked(ref)
		switch slot.Kind {
		case HeapWeakMap:
			kept := slot.WeakMap.Entries[:0]
			for _, entry := range slot.WeakMap.Entries {
				if store.weakKeyLiveLocked(entry.Key, owned, marked) && store.weakValueValidLocked(entry.Value) {
					kept = append(kept, entry)
					continue
				}
				result.ClearedWeakEntries++
			}
			clear(slot.WeakMap.Entries[len(kept):])
			slot.WeakMap.Entries = kept
		case HeapWeakSet:
			kept := slot.WeakSet.Keys[:0]
			for _, key := range slot.WeakSet.Keys {
				if store.weakKeyLiveLocked(key, owned, marked) {
					kept = append(kept, key)
					continue
				}
				result.ClearedWeakEntries++
			}
			clear(slot.WeakSet.Keys[len(kept):])
			slot.WeakSet.Keys = kept
		}
	}

	candidates := make([]Ref, 0, len(owned)-len(marked))
	candidateObjects := make(map[ownership.ObjectID]struct{})
	for ref := range owned {
		if _, live := marked[ref]; live {
			continue
		}
		candidates = append(candidates, ref)
		_, slot, _ := store.slotLocked(ref)
		candidateObjects[slot.object] = struct{}{}
	}
	sort.Slice(candidates, func(left, right int) bool {
		if candidates[left].Region != candidates[right].Region {
			return candidates[left].Region < candidates[right].Region
		}
		return candidates[left].Slot < candidates[right].Slot
	})
	for edge, count := range store.objectEdges {
		if count == 0 {
			continue
		}
		if _, target := candidateObjects[edge.to]; !target {
			continue
		}
		if _, source := candidateObjects[edge.from]; !source {
			return Collection{}, fmt.Errorf("%w: object %d -> %d", ErrObjectReferenced, edge.from, edge.to)
		}
	}

	for _, ref := range candidates {
		region, slot, _ := store.slotLocked(ref)
		if err := store.unlinkSlotLocked(region, slot); err != nil {
			return Collection{}, err
		}
	}
	for _, ref := range candidates {
		region, slot, _ := store.slotLocked(ref)
		bytes := slotLiveBytes(slot)
		if err := store.ledger.Release(slot.object, region.Owner); err != nil {
			return Collection{}, err
		}
		store.recordKindFreeLocked(slot)
		clearSlotPayload(slot)
		delete(store.objectRegions, slot.object)
		slot.object = 0
		slot.Occupied = false
		if slot.Generation != math.MaxUint32 {
			slot.Generation++
			region.free = append(region.free, ref.Slot)
		}
		store.stats.Frees++
		store.stats.LiveSlots--
		result.ReclaimedSlots++
		result.ReclaimedBytes += bytes
	}
	store.stats.Collections++
	store.stats.CollectedSlots += result.ReclaimedSlots
	store.stats.CollectedBytes += result.ReclaimedBytes
	store.stats.WeakEntriesCleared += result.ClearedWeakEntries
	return result, nil
}

func (store *Store) weakKeyLiveLocked(key Ref, owned, marked map[Ref]struct{}) bool {
	if _, isOwned := owned[key]; isOwned {
		_, live := marked[key]
		return live
	}
	_, _, err := store.slotLocked(key)
	return err == nil
}

func (store *Store) weakValueValidLocked(value Value) bool {
	if !value.IsRef() {
		return true
	}
	_, _, err := store.slotLocked(value.Ref())
	return err == nil
}

func slotLiveBytes(slot *Slot) uint64 {
	if slot == nil || !slot.Occupied {
		return 0
	}
	switch slot.Kind {
	case HeapString:
		return uint64(len(slot.String.Text))
	case HeapFunction:
		return uint64(len(slot.Function.Code)) + uint64(len(slot.Function.Locations))*8
	case HeapBigInt:
		return uint64(len(slot.BigInt.Magnitude))
	case HeapArrayBuffer:
		return uint64(len(slot.ArrayBuffer.Bytes))
	default:
		return 0
	}
}

func sortedRegionIDs(regions map[RegionID]*Region) []RegionID {
	ids := make([]RegionID, 0, len(regions))
	for id := range regions {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(left, right int) bool { return ids[left] < ids[right] })
	return ids
}
