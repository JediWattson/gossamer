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
		if store.assignedRegionIDLocked(regionID) {
			return Collection{}, fmt.Errorf("%w: R%d", ErrRegionDestroyed, regionID)
		}
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
	owned := make(map[Ref]struct{})
	ownedRefs := make([]Ref, 0)
	var regionIDs []RegionID
	if regionID != 0 {
		regionIDs = []RegionID{regionID}
	} else if record, exists := store.owners[owner]; exists {
		regionIDs = sortedRegionSet(ownerRegionSet(record))
	}
	for _, id := range regionIDs {
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
			ownedRefs = append(ownedRefs, ref)
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

	// A region-local checkpoint treats every real heap reference from outside
	// the sweep set as a root. The per-slot total incoming count lets us derive
	// this by scanning only sources in the sweep set, independent of unrelated
	// regions elsewhere in the Store.
	internalIncoming, err := store.incomingFromSourcesLocked(ownedRefs, owned)
	if err != nil {
		return Collection{}, err
	}
	for _, root := range ownedRefs {
		_, slot, _ := store.slotLocked(root)
		inside := internalIncoming[root]
		if slot.incoming < inside {
			return Collection{}, fmt.Errorf("%w: %s incoming count %d is below %d references from the sweep set", ErrInvariantViolation, root, slot.incoming, inside)
		}
		if slot.incoming == inside {
			continue
		}
		if _, exists := rootSet[root]; exists {
			continue
		}
		rootSet[root] = struct{}{}
		queue = append(queue, root)
	}

	marked := make(map[Ref]struct{})
	seen := make(map[Ref]struct{})
	type ephemeronLink struct {
		table Ref
		key   Ref
		value Ref
	}
	byTable := make(map[Ref][]ephemeronLink)
	byKey := make(map[Ref][]ephemeronLink)
	links := make([]ephemeronLink, 0)
	// Reverse value uses identify only ephemerons capable of retaining a value
	// in this sweep. Unrelated weak collections never enter the checkpoint.
	for _, value := range ownedRefs {
		for _, use := range store.weakTargets[value] {
			if use.role != weakMapValueUse {
				continue
			}
			_, tableSlot, err := store.slotLocked(use.table)
			if err != nil || tableSlot.Kind != HeapWeakMap || uint64(use.entry) >= uint64(len(tableSlot.WeakMap.Entries)) {
				return Collection{}, fmt.Errorf("%w: invalid ephemeron use for %s", ErrInvariantViolation, value)
			}
			entry := tableSlot.WeakMap.Entries[use.entry]
			link := ephemeronLink{table: use.table, key: entry.Key, value: value}
			links = append(links, link)
			byTable[link.table] = append(byTable[link.table], link)
			byKey[link.key] = append(byKey[link.key], link)
		}
	}
	isLive := func(ref Ref) bool {
		if _, selected := owned[ref]; selected {
			_, live := marked[ref]
			return live
		}
		_, _, err := store.slotLocked(ref)
		return err == nil
	}
	ephemeronQueued := make(map[Ref]struct{})
	enqueueEphemeron := func(link ephemeronLink) {
		if _, done := seen[link.value]; done {
			return
		}
		if _, queued := ephemeronQueued[link.value]; queued {
			return
		}
		ephemeronQueued[link.value] = struct{}{}
		queue = append(queue, link.value)
	}
	activateEphemerons := func(ref Ref) {
		for _, link := range byTable[ref] {
			if isLive(link.key) {
				enqueueEphemeron(link)
			}
		}
		for _, link := range byKey[ref] {
			if isLive(link.table) {
				enqueueEphemeron(link)
			}
		}
	}
	for _, link := range links {
		if isLive(link.table) && isLive(link.key) {
			enqueueEphemeron(link)
		}
	}
	references := make([]Value, 0, 16)
	drainStrongReferences := func() error {
		for cursor := 0; cursor < len(queue); cursor++ {
			ref := queue[cursor]
			if _, exists := seen[ref]; exists {
				continue
			}
			seen[ref] = struct{}{}
			_, slot, err := store.slotLocked(ref)
			if err != nil {
				return err
			}
			if _, isOwned := owned[ref]; !isOwned {
				// External sources were already represented by total incoming
				// counts above. Validate them, but do not traverse a persistent or
				// sibling graph during this local checkpoint.
				continue
			}
			marked[ref] = struct{}{}
			activateEphemerons(ref)
			references = appendSlotReferences(references[:0], slot)
			for _, value := range references {
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

	result := Collection{Roots: uint64(len(rootSet)), MarkedSlots: uint64(len(marked))}
	for ref := range marked {
		_, slot, _ := store.slotLocked(ref)
		switch slot.Kind {
		case HeapWeakMap:
			for index := 0; index < len(slot.WeakMap.Entries); {
				entry := slot.WeakMap.Entries[index]
				if store.weakKeyLiveLocked(entry.Key, owned, marked) && store.weakValueValidLocked(entry.Value) {
					index++
					continue
				}
				if err := store.removeWeakMapEntryLocked(ref, slot, uint32(index)); err != nil {
					return Collection{}, err
				}
				result.ClearedWeakEntries++
			}
		case HeapWeakSet:
			for index := 0; index < len(slot.WeakSet.Keys); {
				key := slot.WeakSet.Keys[index]
				if store.weakKeyLiveLocked(key, owned, marked) {
					index++
					continue
				}
				if err := store.removeWeakSetEntryLocked(ref, slot, uint32(index)); err != nil {
					return Collection{}, err
				}
				result.ClearedWeakEntries++
			}
		}
	}

	candidates := make([]Ref, 0, len(owned)-len(marked))
	candidateSet := make(map[Ref]struct{})
	for ref := range owned {
		if _, live := marked[ref]; live {
			continue
		}
		candidates = append(candidates, ref)
		candidateSet[ref] = struct{}{}
	}
	sort.Slice(candidates, func(left, right int) bool {
		if candidates[left].Region != candidates[right].Region {
			return candidates[left].Region < candidates[right].Region
		}
		return candidates[left].Slot < candidates[right].Slot
	})
	candidateIncoming, err := store.incomingFromSourcesLocked(candidates, candidateSet)
	if err != nil {
		return Collection{}, err
	}
	for _, candidate := range candidates {
		_, slot, _ := store.slotLocked(candidate)
		if slot.incoming != candidateIncoming[candidate] {
			return Collection{}, fmt.Errorf("%w: %s has %d incoming references, only %d from the collection set", ErrObjectReferenced, candidate, slot.incoming, candidateIncoming[candidate])
		}
	}
	var cleared uint64
	for _, target := range candidates {
		removed, err := store.dropWeakTargetUsesLocked(target, nil, func(table Ref) bool {
			_, tableCollected := candidateSet[table]
			return !tableCollected
		})
		if err != nil {
			return Collection{}, err
		}
		cleared += removed
	}
	result.ClearedWeakEntries += cleared

	references = references[:0]
	for _, ref := range candidates {
		region, slot, _ := store.slotLocked(ref)
		var err error
		references, err = store.unlinkSlotWithScratchLocked(region, slot, references)
		if err != nil {
			return Collection{}, err
		}
	}
	for _, ref := range candidates {
		region, slot, _ := store.slotLocked(ref)
		bytes := slotLiveBytes(slot)
		if slot.incoming != 0 {
			return Collection{}, fmt.Errorf("%w: collected %s retains %d incoming references", ErrInvariantViolation, ref, slot.incoming)
		}
		store.forgetPromotionsFromSourceLocked(ref)
		store.forgetPromotionsToDestinationLocked(ref)
		store.recordKindFreeLocked(slot)
		if err := store.forgetWeakTableLocked(ref, slot); err != nil {
			return Collection{}, err
		}
		if err := store.clearSlotPayloadLocked(slot); err != nil {
			return Collection{}, err
		}
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

// incomingFromSourcesLocked counts authoritative strong references from one
// source set into one target set. Weak keys and ephemeron values are absent
// from appendSlotReferences and remain governed by the fixed point above.
func (store *Store) incomingFromSourcesLocked(sources []Ref, targets map[Ref]struct{}) (map[Ref]uint32, error) {
	counts := make(map[Ref]uint32)
	references := make([]Value, 0, 16)
	for _, source := range sources {
		_, slot, err := store.slotLocked(source)
		if err != nil {
			return nil, err
		}
		references = appendSlotReferences(references[:0], slot)
		for _, value := range references {
			if !value.IsRef() {
				continue
			}
			target := value.Ref()
			if _, _, err := store.slotLocked(target); err != nil {
				return nil, fmt.Errorf("memory: %s contains %s: %w", source, target, err)
			}
			if _, selected := targets[target]; !selected {
				continue
			}
			if counts[target] == math.MaxUint32 {
				return nil, fmt.Errorf("memory: incoming reference count overflows for %s", target)
			}
			counts[target]++
		}
	}
	return counts, nil
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
