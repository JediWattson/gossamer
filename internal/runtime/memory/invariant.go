package memory

import (
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

var ErrInvariantViolation = errors.New("memory: invariant violation")

type slotLocation struct {
	region RegionID
	slot   uint32
}

// CheckInvariants independently derives the Store's live slot, object-edge,
// region-edge, owner, and ledger state from typed heap contents. Tests and debug
// builds can call it after arbitrary operation sequences without mutating the
// heap.
func (store *Store) CheckInvariants() error {
	if store == nil {
		return fmt.Errorf("%w: nil store", ErrInvariantViolation)
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()

	objects := make(map[ownership.ObjectID]slotLocation)
	liveSlots := uint64(0)
	liveCells := uint64(0)
	liveStrings := uint64(0)
	liveObjects := uint64(0)
	liveBytes := uint64(0)
	liveRegions := uint64(0)
	for owner, claim := range store.ownerClaims {
		if claim == 0 {
			return invariantError("%s has claim 0", owner)
		}
		snapshot, err := store.ledger.Region(claim)
		if err != nil {
			return invariantError("%s claim %d: %v", owner, claim, err)
		}
		if snapshot.Owner != owner {
			return invariantError("%s claim %d belongs to %s", owner, claim, snapshot.Owner)
		}
		if snapshot.Closed != store.closedOwners[owner] {
			return invariantError("%s claim closed=%t, Store records closed=%t", owner, snapshot.Closed, store.closedOwners[owner])
		}
	}
	for id, region := range store.regions {
		if region == nil {
			return invariantError("R%d has a nil record", id)
		}
		if id == 0 || region.ID != id {
			return invariantError("region map key R%d contains ID R%d", id, region.ID)
		}
		if region.State > RegionDestroyed {
			return invariantError("R%d has unknown state %d", id, region.State)
		}
		if region.State == RegionDestroyed {
			for index, slot := range region.Slots {
				if slot.Occupied || slot.object != 0 || !slotStorageEmpty(&slot) {
					return invariantError("destroyed R%d slot %d still owns storage", id, index)
				}
				if slot.Generation == 0 {
					return invariantError("destroyed R%d slot %d has generation 0", id, index)
				}
			}
			continue
		}

		liveRegions++
		if region.Owner.Value == 0 {
			return invariantError("live R%d has no owner", id)
		}
		switch region.State {
		case RegionPrivate:
			if region.Owner.Kind == ownership.OwnerQueue || region.Owner.Kind == ownership.OwnerShared {
				return invariantError("private R%d has invalid owner %s", id, region.Owner)
			}
		case RegionInTransit:
			if region.Owner.Kind != ownership.OwnerQueue {
				return invariantError("in-transit R%d is owned by %s", id, region.Owner)
			}
		case RegionPublished:
			if region.Owner.Kind != ownership.OwnerShared || region.Owner != store.sharedOwner {
				return invariantError("published R%d is owned by %s, shared owner is %s", id, region.Owner, store.sharedOwner)
			}
		}
		if store.closedOwners[region.Owner] {
			return invariantError("live R%d is owned by closed %s", id, region.Owner)
		}
		claim := store.ownerClaims[region.Owner]
		if claim == 0 || region.claim != claim {
			return invariantError("R%d claim %d does not match %s claim %d", id, region.claim, region.Owner, claim)
		}
		claimSnapshot, err := store.ledger.Region(claim)
		if err != nil {
			return invariantError("R%d claim %d: %v", id, claim, err)
		}
		if claimSnapshot.Closed || claimSnapshot.Owner != region.Owner {
			return invariantError("R%d has invalid claim snapshot %#v", id, claimSnapshot)
		}

		free := make(map[uint32]struct{}, len(region.free))
		for _, index := range region.free {
			if uint64(index) >= uint64(len(region.Slots)) {
				return invariantError("R%d free slot %d is out of range", id, index)
			}
			if _, duplicate := free[index]; duplicate {
				return invariantError("R%d free slot %d is duplicated", id, index)
			}
			free[index] = struct{}{}
			slot := region.Slots[index]
			if slot.Occupied || slot.object != 0 || !slotStorageEmpty(&slot) || slot.Generation == math.MaxUint32 {
				return invariantError("R%d free slot %d has live or retired state", id, index)
			}
		}
		for index := range region.Slots {
			slot := &region.Slots[index]
			if slot.Generation == 0 {
				return invariantError("R%d slot %d has generation 0", id, index)
			}
			_, onFreeList := free[uint32(index)]
			if !slot.Occupied {
				if slot.object != 0 || !slotStorageEmpty(slot) {
					return invariantError("R%d vacant slot %d retains storage", id, index)
				}
				if slot.Generation != math.MaxUint32 && !onFreeList {
					return invariantError("R%d reusable slot %d is missing from free list", id, index)
				}
				continue
			}
			if onFreeList {
				return invariantError("R%d occupied slot %d is on free list", id, index)
			}
			if slot.object == 0 {
				return invariantError("R%d occupied slot %d has no ledger object", id, index)
			}
			if previous, duplicate := objects[slot.object]; duplicate {
				return invariantError("ledger object %d appears at R%d:%d and R%d:%d", slot.object, previous.region, previous.slot, id, index)
			}
			objects[slot.object] = slotLocation{region: id, slot: uint32(index)}
			liveSlots++
			switch slot.Kind {
			case HeapCell:
				liveCells++
				if slot.String.Text != "" || slot.Object.Prototype != (Value{}) || len(slot.Object.Properties) != 0 {
					return invariantError("Cell %s retains another typed payload", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation})
				}
			case HeapString:
				liveStrings++
				liveBytes += uint64(len(slot.String.Text))
				if len(slot.Cell.Fields) != 0 || slot.Object.Prototype != (Value{}) || len(slot.Object.Properties) != 0 {
					return invariantError("String %s retains another typed payload", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation})
				}
			case HeapObject:
				liveObjects++
				if len(slot.Cell.Fields) != 0 || slot.String.Text != "" {
					return invariantError("Object %s retains another typed payload", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation})
				}
				if slot.Object.Prototype.Kind() != ValueNull && !slot.Object.Prototype.IsRef() {
					return invariantError("Object %s has invalid prototype kind %d", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, slot.Object.Prototype.Kind())
				}
				if slot.Object.Prototype.IsRef() {
					prototype := slot.Object.Prototype.Ref()
					prototypeRegion := store.regions[prototype.Region]
					if prototypeRegion == nil || prototypeRegion.State == RegionDestroyed || uint64(prototype.Slot) >= uint64(len(prototypeRegion.Slots)) {
						return invariantError("Object %s has stale prototype %s", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, prototype)
					}
					prototypeSlot := &prototypeRegion.Slots[prototype.Slot]
					if !prototypeSlot.Occupied || prototypeSlot.Generation != prototype.Gen || prototypeSlot.Kind != HeapObject {
						return invariantError("Object %s has non-Object prototype %s", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, prototype)
					}
				}
				names := make(map[string]struct{}, len(slot.Object.Properties))
				for propertyIndex, property := range slot.Object.Properties {
					targetRegion := store.regions[property.Name.Region]
					if targetRegion == nil || targetRegion.State == RegionDestroyed || uint64(property.Name.Slot) >= uint64(len(targetRegion.Slots)) {
						return invariantError("Object %s property %d has stale name %s", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, propertyIndex, property.Name)
					}
					nameSlot := &targetRegion.Slots[property.Name.Slot]
					if !nameSlot.Occupied || nameSlot.Generation != property.Name.Gen || nameSlot.Kind != HeapString {
						return invariantError("Object %s property %d has non-String name %s", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, propertyIndex, property.Name)
					}
					if _, duplicate := names[nameSlot.String.Text]; duplicate {
						return invariantError("Object %s has duplicate property %q", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, nameSlot.String.Text)
					}
					names[nameSlot.String.Text] = struct{}{}
				}
			default:
				return invariantError("R%d occupied slot %d has unknown heap kind %d", id, index, slot.Kind)
			}
		}
	}

	expectedObjectEdges := make(map[objectEdge]uint32)
	expectedRegionEdges := make(map[edgeKey]uint32)
	expectedLedgerEdges := make(map[ownership.ObjectID]map[ownership.ObjectID]struct{})
	for id, region := range store.regions {
		if region == nil || region.State == RegionDestroyed {
			continue
		}
		for index := range region.Slots {
			slot := &region.Slots[index]
			if !slot.Occupied {
				continue
			}
			for field, value := range slotReferences(slot) {
				if !value.IsRef() {
					continue
				}
				targetRegion := store.regions[value.Ref().Region]
				if targetRegion == nil || targetRegion.State == RegionDestroyed || uint64(value.Ref().Slot) >= uint64(len(targetRegion.Slots)) {
					return invariantError("R%d slot %d field %d contains stale %s", id, index, field, value.Ref())
				}
				targetSlot := &targetRegion.Slots[value.Ref().Slot]
				if !targetSlot.Occupied || targetSlot.Generation != value.Ref().Gen {
					return invariantError("R%d slot %d field %d contains stale %s", id, index, field, value.Ref())
				}
				objectKey := objectEdge{from: slot.object, to: targetSlot.object}
				if expectedObjectEdges[objectKey] == math.MaxUint32 {
					return invariantError("object edge %d -> %d overflows", slot.object, targetSlot.object)
				}
				expectedObjectEdges[objectKey]++
				if expectedLedgerEdges[slot.object] == nil {
					expectedLedgerEdges[slot.object] = make(map[ownership.ObjectID]struct{})
				}
				expectedLedgerEdges[slot.object][targetSlot.object] = struct{}{}
				if id != targetRegion.ID {
					regionKey := edgeKey{from: id, to: targetRegion.ID}
					if expectedRegionEdges[regionKey] == math.MaxUint32 {
						return invariantError("region edge R%d -> R%d overflows", id, targetRegion.ID)
					}
					expectedRegionEdges[regionKey]++
				}
			}
		}
	}

	if err := compareObjectEdges(store.objectEdges, expectedObjectEdges); err != nil {
		return err
	}
	if err := compareRegionEdges(store.barrier.edges, expectedRegionEdges); err != nil {
		return err
	}
	for object, location := range objects {
		snapshot, err := store.ledger.Object(object)
		if err != nil {
			return invariantError("R%d slot %d ledger object %d: %v", location.region, location.slot, object, err)
		}
		region := store.regions[location.region]
		if !snapshot.Alive || snapshot.References != 1 || len(snapshot.Owners) != 1 || snapshot.Owners[region.Owner] != 1 {
			return invariantError("R%d slot %d has invalid ledger object %#v", location.region, location.slot, snapshot)
		}
		wantTargets := sortedObjectSet(expectedLedgerEdges[object])
		if !equalObjectIDs(snapshot.Edges, wantTargets) {
			return invariantError("ledger object %d edges %v, want %v", object, snapshot.Edges, wantTargets)
		}
	}
	if store.stats.LiveSlots != liveSlots || store.stats.LiveCells != liveCells || store.stats.LiveStrings != liveStrings || store.stats.LiveObjects != liveObjects || store.stats.LiveBytes != liveBytes || store.stats.LiveRegions != liveRegions {
		return invariantError("stats slots/cells/strings/objects/bytes/regions = %d/%d/%d/%d/%d/%d, derived %d/%d/%d/%d/%d/%d", store.stats.LiveSlots, store.stats.LiveCells, store.stats.LiveStrings, store.stats.LiveObjects, store.stats.LiveBytes, store.stats.LiveRegions, liveSlots, liveCells, liveStrings, liveObjects, liveBytes, liveRegions)
	}
	if store.closed && (liveSlots != 0 || liveRegions != 0) {
		return invariantError("closed store retains %d slots in %d regions", liveSlots, liveRegions)
	}
	if store.sharedOwner.Value == 0 {
		if store.sharedClaim != 0 {
			return invariantError("shared claim %d exists without a shared owner", store.sharedClaim)
		}
	} else if store.sharedOwner.Kind != ownership.OwnerShared || store.ownerClaims[store.sharedOwner] != store.sharedClaim {
		return invariantError("shared owner %s and claim %d are inconsistent", store.sharedOwner, store.sharedClaim)
	}
	return nil
}

func compareObjectEdges(got, want map[objectEdge]uint32) error {
	if len(got) != len(want) {
		return invariantError("object edge map has %d entries, want %d", len(got), len(want))
	}
	for edge, count := range want {
		if got[edge] != count {
			return invariantError("object edge %d -> %d count %d, want %d", edge.from, edge.to, got[edge], count)
		}
	}
	return nil
}

func compareRegionEdges(got, want map[edgeKey]uint32) error {
	if len(got) != len(want) {
		return invariantError("region edge map has %d entries, want %d", len(got), len(want))
	}
	for edge, count := range want {
		if got[edge] != count {
			return invariantError("region edge R%d -> R%d count %d, want %d", edge.from, edge.to, got[edge], count)
		}
	}
	return nil
}

func sortedObjectSet(objects map[ownership.ObjectID]struct{}) []ownership.ObjectID {
	result := make([]ownership.ObjectID, 0, len(objects))
	for object := range objects {
		result = append(result, object)
	}
	sort.Slice(result, func(left, right int) bool { return result[left] < result[right] })
	return result
}

func equalObjectIDs(left, right []ownership.ObjectID) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func invariantError(format string, arguments ...any) error {
	return fmt.Errorf("%w: %s", ErrInvariantViolation, fmt.Sprintf(format, arguments...))
}
