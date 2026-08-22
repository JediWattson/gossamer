package memory

import (
	"errors"
	"fmt"
	"testing"

	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

func TestHeapPayloadsOwnReferencesWithoutShadowLedgerObjects(t *testing.T) {
	ledger := ownership.NewLedgerWithEventLimit(0)
	store := NewStore(ledger)
	defer store.Close()
	owner := ownership.OwnerID{Kind: ownership.OwnerRealm, Value: 801}
	firstRegion, err := store.NewRegion(owner)
	if err != nil {
		t.Fatal(err)
	}
	secondRegion, err := store.NewRegion(owner)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := store.AllocCell(owner, firstRegion)
	if err != nil {
		t.Fatal(err)
	}
	target, err := store.AllocCell(owner, firstRegion)
	if err != nil {
		t.Fatal(err)
	}
	external, err := store.AllocCell(owner, secondRegion)
	if err != nil {
		t.Fatal(err)
	}

	if err := store.Set(owner, parent, 0, RefValue(target)); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(owner, parent, 1, RefValue(target)); err != nil {
		t.Fatal(err)
	}
	if got := store.EdgeCount(firstRegion, firstRegion); got != 0 {
		t.Fatalf("same-region barrier count = %d, want 0", got)
	}
	if err := store.Set(owner, external, 0, RefValue(target)); err != nil {
		t.Fatal(err)
	}
	if got := store.EdgeCount(secondRegion, firstRegion); got != 1 {
		t.Fatalf("cross-region barrier count = %d, want 1", got)
	}

	store.mutex.Lock()
	_, targetSlot, slotErr := store.slotLocked(target)
	incoming := uint32(0)
	if slotErr == nil {
		incoming = targetSlot.incoming
	}
	store.mutex.Unlock()
	if slotErr != nil || incoming != 3 {
		t.Fatalf("target incoming count = %d, %v, want 3", incoming, slotErr)
	}
	if stats := ledger.Stats(); stats.ObjectsCreated != 0 || stats.LiveObjects != 0 || stats.LocalReferences != 0 {
		t.Fatalf("heap allocation populated shadow ledger: %#v", stats)
	}
	physical := store.PhysicalStats()
	if physical.ObjectEdgeEntries != 0 || physical.ObjectEdgeReservedBytes != 0 || physical.ObjectRegionEntries != 0 {
		t.Fatalf("heap profile reports a mirrored object graph: %#v", physical)
	}
	if err := store.Free(owner, target); !errors.Is(err, ErrObjectReferenced) {
		t.Fatalf("Free(referenced target) error = %v, want ErrObjectReferenced", err)
	}

	if err := store.Set(owner, external, 0, UndefinedValue()); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(owner, parent, 0, UndefinedValue()); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(owner, parent, 1, UndefinedValue()); err != nil {
		t.Fatal(err)
	}
	if err := store.Free(owner, target); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(owner, parent, 0, RefValue(parent)); err != nil {
		t.Fatal(err)
	}
	if err := store.Free(owner, parent); err != nil {
		t.Fatalf("Free(self-cycle) = %v", err)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func TestWeakIndexesArePartOfTheStoreInvariant(t *testing.T) {
	store := NewStore(nil)
	defer store.Close()
	owner := ownership.OwnerID{Kind: ownership.OwnerRealm, Value: 802}
	region, err := store.NewRegion(owner)
	if err != nil {
		t.Fatal(err)
	}
	table, err := store.AllocWeakMap(owner, region)
	if err != nil {
		t.Fatal(err)
	}
	key, err := store.AllocCell(owner, region)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WeakMapSet(owner, table, key, NumberValue(1)); err != nil {
		t.Fatal(err)
	}

	store.mutex.Lock()
	delete(store.weakTables, table)
	store.mutex.Unlock()
	if err := store.CheckInvariants(); !errors.Is(err, ErrInvariantViolation) {
		t.Fatalf("missing weak table index error = %v, want ErrInvariantViolation", err)
	}
	store.mutex.Lock()
	store.weakTables[table] = struct{}{}
	store.weakTargets[key] = append(store.weakTargets[key], store.weakTargets[key][0])
	store.mutex.Unlock()
	if err := store.CheckInvariants(); !errors.Is(err, ErrInvariantViolation) {
		t.Fatalf("corrupt weak target use error = %v, want ErrInvariantViolation", err)
	}
	store.mutex.Lock()
	store.weakTargets[key] = store.weakTargets[key][:1]
	store.mutex.Unlock()
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func TestRegionClosureTraversesALongCountedEdgeChain(t *testing.T) {
	store := NewStore(nil)
	defer store.Close()
	owner := ownership.OwnerID{Kind: ownership.OwnerRealm, Value: 803}
	const regions = 512
	refs := make([]Ref, regions)
	for index := range refs {
		region, err := store.NewRegion(owner)
		if err != nil {
			t.Fatal(err)
		}
		refs[index], err = store.AllocCell(owner, region)
		if err != nil {
			t.Fatal(err)
		}
		if index != 0 {
			if err := store.Set(owner, refs[index-1], 0, RefValue(refs[index])); err != nil {
				t.Fatal(err)
			}
		}
	}
	store.mutex.Lock()
	closure := store.outgoingRegionsLocked(map[RegionID]struct{}{refs[0].Region: {}}, func(region *Region) bool {
		return region.State == RegionPrivate && region.Owner == owner
	})
	store.mutex.Unlock()
	if len(closure) != regions {
		t.Fatalf("long-chain closure has %d regions, want %d", len(closure), regions)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func TestReleaseOwnerRetiresPhysicalRegionAndClaimMetadata(t *testing.T) {
	store := NewStore(nil)
	defer store.Close()
	const tasks = 10_000
	var firstRegion RegionID
	var firstRef Ref
	for index := 0; index < tasks; index++ {
		owner := ownership.OwnerID{Kind: ownership.OwnerTask, Value: uint64(index + 1)}
		region, err := store.NewRegion(owner)
		if err != nil {
			t.Fatal(err)
		}
		ref, err := store.AllocCell(owner, region)
		if err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			firstRegion, firstRef = region, ref
		}
		if err := store.ReleaseOwner(owner); err != nil {
			t.Fatal(err)
		}
	}
	if len(store.regions) != 0 || len(store.owners) != 0 {
		t.Fatalf("retained metadata: regions=%d owners=%d", len(store.regions), len(store.owners))
	}
	physical := store.PhysicalStats()
	if physical.RegionRecords != 0 || physical.OwnerClaimEntries != 0 {
		t.Fatalf("physical metadata after task churn = %#v", physical)
	}
	if _, err := store.Deref(ownership.OwnerID{Kind: ownership.OwnerTask, Value: 1}, firstRef); !errors.Is(err, ErrStaleRef) {
		t.Fatalf("first ref after churn = %v, want ErrStaleRef", err)
	}
	region, err := store.Region(firstRegion)
	if err != nil || region.State != RegionDestroyed {
		t.Fatalf("retired Region(%d) = %#v, %v", firstRegion, region, err)
	}
	if err := store.DestroyRegion(ownership.OwnerID{Kind: ownership.OwnerTask, Value: 1}, firstRegion); err != nil {
		t.Fatalf("second DestroyRegion() = %v", err)
	}
	if _, err := store.Region(store.nextRegion + 1); !errors.Is(err, ErrUnknownRegion) {
		t.Fatalf("future Region() = %v, want ErrUnknownRegion", err)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func TestPromotionIndexesRetireWithEitherEndpoint(t *testing.T) {
	for _, retireDestination := range []bool{false, true} {
		t.Run(fmt.Sprintf("destination-first-%t", retireDestination), func(t *testing.T) {
			store := NewStore(nil)
			defer store.Close()
			task := ownership.OwnerID{Kind: ownership.OwnerTask, Value: 20_001}
			document := ownership.OwnerID{Kind: ownership.OwnerDocument, Value: 20_001}
			taskRegion, _ := store.NewRegion(task)
			documentRegion, _ := store.NewRegion(document)
			source, _ := store.AllocCell(task, taskRegion)
			holder, _ := store.AllocCell(document, documentRegion)
			if err := store.Set(document, holder, 0, RefValue(source)); err != nil {
				t.Fatal(err)
			}
			cell, err := store.Deref(document, holder)
			if err != nil || len(cell.Fields) != 1 || !cell.Fields[0].IsRef() {
				t.Fatalf("promoted holder = %#v, %v", cell, err)
			}
			promoted := cell.Fields[0].Ref()
			if len(store.promotions) != 1 || len(store.promotionsBySource) != 1 || len(store.promotionsByDestination) != 1 {
				t.Fatalf("promotion indexes were not populated")
			}
			if retireDestination {
				if err := store.Set(document, holder, 0, UndefinedValue()); err != nil {
					t.Fatal(err)
				}
				if err := store.Free(document, promoted); err != nil {
					t.Fatal(err)
				}
			} else if err := store.Free(task, source); err != nil {
				t.Fatal(err)
			}
			if len(store.promotions) != 0 || len(store.promotionsBySource) != 0 || len(store.promotionsByDestination) != 0 {
				t.Fatalf("promotion indexes retained an endpoint")
			}
			if err := store.CheckInvariants(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPromotionIndexesRetireWhenDestinationRegionMoves(t *testing.T) {
	for _, operation := range []string{"transfer", "publish"} {
		t.Run(operation, func(t *testing.T) {
			store := NewStore(nil)
			defer store.Close()
			task := ownership.OwnerID{Kind: ownership.OwnerTask, Value: 21_001}
			document := ownership.OwnerID{Kind: ownership.OwnerDocument, Value: 21_001}
			queue := ownership.OwnerID{Kind: ownership.OwnerQueue, Value: 21_001}
			receiver := ownership.OwnerID{Kind: ownership.OwnerRealm, Value: 21_001}
			taskRegion, _ := store.NewRegion(task)
			documentRegion, _ := store.NewRegion(document)
			source, _ := store.AllocCell(task, taskRegion)
			holder, _ := store.AllocCell(document, documentRegion)
			if err := store.Set(document, holder, 0, RefValue(source)); err != nil {
				t.Fatal(err)
			}
			if len(store.promotions) != 1 {
				t.Fatalf("promotion cache has %d entries, want 1", len(store.promotions))
			}

			switch operation {
			case "transfer":
				if err := store.Transfer(document, queue, holder); err != nil {
					t.Fatal(err)
				}
				if len(store.promotions) != 0 || len(store.promotionsBySource) != 0 || len(store.promotionsByDestination) != 0 {
					t.Fatal("transfer retained a promotion cache entry whose destination moved")
				}
				if err := store.Accept(queue, receiver, holder); err != nil {
					t.Fatal(err)
				}
			case "publish":
				if err := store.Publish(document, holder); err != nil {
					t.Fatal(err)
				}
				if len(store.promotions) != 0 || len(store.promotionsBySource) != 0 || len(store.promotionsByDestination) != 0 {
					t.Fatal("publish retained a promotion cache entry whose destination moved")
				}
			}
			if err := store.CheckInvariants(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestEnsureOwnerDoesNotSnapshotSemanticObjects(t *testing.T) {
	ledger := ownership.NewLedgerWithEventLimit(0)
	store := NewStore(ledger)
	defer store.Close()
	owner := ownership.OwnerID{Kind: ownership.OwnerRealm, Value: 22_001}
	if _, err := store.NewRegion(owner); err != nil {
		t.Fatal(err)
	}
	claim, err := ledger.OwnerRegion(owner)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 10_000; index++ {
		if _, err := ledger.CreateObject(claim); err != nil {
			t.Fatal(err)
		}
	}
	allocations := testing.AllocsPerRun(100, func() {
		store.mutex.Lock()
		got, ensureErr := store.ensureOwnerLocked(owner)
		store.mutex.Unlock()
		if ensureErr != nil || got != claim {
			panic(fmt.Sprintf("ensureOwnerLocked() = %d, %v; want %d", got, ensureErr, claim))
		}
	})
	if allocations != 0 {
		t.Fatalf("ensureOwnerLocked() allocated %.2f times with 10k semantic objects", allocations)
	}
}

func TestShortLivedSameOwnerGraphPromotesWithoutCopyingPersistentTail(t *testing.T) {
	store := NewStore(nil)
	defer store.Close()
	owner := ownership.OwnerID{Kind: ownership.OwnerRealm, Value: 23_001}
	persistentRegion, err := store.NewRegion(owner)
	if err != nil {
		t.Fatal(err)
	}
	scratchRegion, err := store.NewRegionWithLifetime(owner, ownership.OwnerTask)
	if err != nil {
		t.Fatal(err)
	}
	persistent, _ := store.AllocCell(owner, persistentRegion)
	holder, _ := store.AllocCell(owner, persistentRegion)
	root, _ := store.AllocCell(owner, scratchRegion)
	child, _ := store.AllocCell(owner, scratchRegion)
	if err := store.Set(owner, root, 0, RefValue(child)); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(owner, root, 1, RefValue(persistent)); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(owner, holder, 0, RefValue(root)); err != nil {
		t.Fatal(err)
	}

	holderCell, err := store.Deref(owner, holder)
	if err != nil || len(holderCell.Fields) != 1 || !holderCell.Fields[0].IsRef() {
		t.Fatalf("holder = %#v, %v", holderCell, err)
	}
	if holderCell.Fields[0].Ref() != root {
		t.Fatal("same-owner store eagerly copied the short-lived source")
	}
	if err := store.Set(owner, root, 2, NumberValue(42)); err != nil {
		t.Fatal(err)
	}
	promotedCount, err := store.PromoteEscapes(owner, scratchRegion)
	if err != nil {
		t.Fatal(err)
	}
	if promotedCount != 1 {
		t.Fatalf("promoted source count = %d, want 1", promotedCount)
	}
	holderCell, err = store.Deref(owner, holder)
	if err != nil || len(holderCell.Fields) != 1 || !holderCell.Fields[0].IsRef() {
		t.Fatalf("promoted holder = %#v, %v", holderCell, err)
	}
	promotedRoot := holderCell.Fields[0].Ref()
	if promotedRoot == root {
		t.Fatal("checkpoint retained the short-lived source ref")
	}
	promotedRegion, err := store.RegionMetadata(promotedRoot.Region)
	if err != nil || promotedRegion.Owner != owner || promotedRegion.Lifetime != ownership.OwnerRealm {
		t.Fatalf("promoted region = %#v, %v", promotedRegion, err)
	}
	promoted, err := store.Deref(owner, promotedRoot)
	if err != nil || len(promoted.Fields) != 3 || !promoted.Fields[0].IsRef() || promoted.Fields[1].Ref() != persistent || promoted.Fields[2] != NumberValue(42) {
		t.Fatalf("promoted graph = %#v, %v", promoted, err)
	}
	if promoted.Fields[0].Ref() == child {
		t.Fatal("short-lived child was not promoted")
	}
	if stats := store.Stats(); stats.AutomaticPromotions != 1 {
		t.Fatalf("automatic promotions = %d, want 1", stats.AutomaticPromotions)
	}
	if err := store.DestroyRegion(owner, scratchRegion); err != nil {
		t.Fatalf("destroy scratch region: %v", err)
	}
	if _, err := store.Deref(owner, promotedRoot); err != nil {
		t.Fatalf("promoted root after scratch release: %v", err)
	}
	if _, err := store.Deref(owner, persistent); err != nil {
		t.Fatalf("persistent tail after scratch release: %v", err)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func TestNewRegionRejectsLifetimeLongerThanOwner(t *testing.T) {
	store := NewStore(nil)
	defer store.Close()
	owner := ownership.OwnerID{Kind: ownership.OwnerTask, Value: 23_002}
	if _, err := store.NewRegionWithLifetime(owner, ownership.OwnerRealm); err == nil {
		t.Fatal("task owner created Realm-lifetime storage")
	}
	if stats := store.Stats(); stats.LiveRegions != 0 {
		t.Fatalf("invalid lifetime retained %d regions", stats.LiveRegions)
	}
}

func TestCheckpointPromotionPreservesAliasesAcrossEscapeRoots(t *testing.T) {
	store := NewStore(nil)
	defer store.Close()
	owner := ownership.OwnerID{Kind: ownership.OwnerRealm, Value: 23_004}
	persistentRegion, err := store.NewRegion(owner)
	if err != nil {
		t.Fatal(err)
	}
	scratchRegion, err := store.NewRegionWithLifetime(owner, ownership.OwnerTask)
	if err != nil {
		t.Fatal(err)
	}
	holder, _ := store.AllocCell(owner, persistentRegion)
	root, _ := store.AllocCell(owner, scratchRegion)
	shared, _ := store.AllocCell(owner, scratchRegion)
	if err := store.Set(owner, root, 0, RefValue(shared)); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(owner, holder, 0, RefValue(root)); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(owner, holder, 1, RefValue(shared)); err != nil {
		t.Fatal(err)
	}
	promotedCount, err := store.PromoteEscapes(owner, scratchRegion)
	if err != nil {
		t.Fatal(err)
	}
	if promotedCount != 2 {
		t.Fatalf("promoted source count = %d, want 2", promotedCount)
	}
	persistent, err := store.Deref(owner, holder)
	if err != nil || len(persistent.Fields) != 2 {
		t.Fatalf("holder = %#v, %v", persistent, err)
	}
	promotedRoot := persistent.Fields[0].Ref()
	promotedShared := persistent.Fields[1].Ref()
	rootCell, err := store.Deref(owner, promotedRoot)
	if err != nil || len(rootCell.Fields) != 1 || rootCell.Fields[0].Ref() != promotedShared {
		t.Fatalf("promoted aliases diverged: root=%#v shared=%s, %v", rootCell, promotedShared, err)
	}
	if promotedRoot.Region != promotedShared.Region {
		t.Fatalf("batched roots landed in R%d and R%d", promotedRoot.Region, promotedShared.Region)
	}
	if stats := store.Stats(); stats.AutomaticPromotions != 1 {
		t.Fatalf("batched automatic promotions = %d, want 1", stats.AutomaticPromotions)
	}
	if err := store.DestroyRegion(owner, scratchRegion); err != nil {
		t.Fatal(err)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func TestOwnerCollectionRetiresEmptyPromotionRegion(t *testing.T) {
	store := NewStore(nil)
	defer store.Close()
	owner := ownership.OwnerID{Kind: ownership.OwnerRealm, Value: 23_003}
	persistentRegion, err := store.NewRegion(owner)
	if err != nil {
		t.Fatal(err)
	}
	scratchRegion, err := store.NewRegionWithLifetime(owner, ownership.OwnerTask)
	if err != nil {
		t.Fatal(err)
	}
	holder, _ := store.AllocCell(owner, persistentRegion)
	escape, _ := store.AllocCell(owner, scratchRegion)
	if err := store.Set(owner, holder, 0, RefValue(escape)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PromoteEscapes(owner, scratchRegion); err != nil {
		t.Fatal(err)
	}
	holderCell, err := store.Deref(owner, holder)
	if err != nil || len(holderCell.Fields) != 1 || !holderCell.Fields[0].IsRef() {
		t.Fatalf("holder = %#v, %v", holderCell, err)
	}
	promotionRegion := holderCell.Fields[0].Ref().Region
	if promotionRegion == persistentRegion || promotionRegion == scratchRegion {
		t.Fatalf("promotion region = R%d, want a distinct region", promotionRegion)
	}
	if err := store.DestroyRegion(owner, scratchRegion); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(owner, holder, 0, UndefinedValue()); err != nil {
		t.Fatal(err)
	}
	result, err := store.Collect(owner, holder)
	if err != nil {
		t.Fatal(err)
	}
	if result.ReclaimedSlots != 1 {
		t.Fatalf("collection = %#v, want one reclaimed promoted slot", result)
	}
	retired, err := store.Region(promotionRegion)
	if err != nil || retired.State != RegionDestroyed {
		t.Fatalf("empty promotion Region(R%d) = %#v, %v, want destroyed", promotionRegion, retired, err)
	}
	if _, err := store.Region(persistentRegion); err != nil {
		t.Fatalf("persistent region retired: %v", err)
	}
	if stats := store.Stats(); stats.LiveRegions != 1 || stats.BulkRegionReleases != 2 {
		t.Fatalf("region retirement stats = %#v", stats)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func TestCheckpointRewriteCoversEveryStrongTypedLocation(t *testing.T) {
	old := Ref{Region: 1, Slot: 2, Gen: 3}
	replacement := Ref{Region: 4, Slot: 5, Gen: 6}
	value := RefValue(old)
	header := func() ObjectHeader {
		return ObjectHeader{
			Prototype: value,
			Properties: []Property{{
				Name: old, Value: value, Getter: value, Setter: value,
			}},
		}
	}
	slots := map[string]Slot{
		"cell": {
			Kind: HeapCell, Occupied: true,
			slotPayload: &slotPayload{Cell: &Cell{Fields: []Value{value}}},
		},
		"object": {
			Kind: HeapObject, Occupied: true,
			slotPayload: &slotPayload{Object: &Object{ObjectHeader: header()}},
		},
		"array": {
			Kind: HeapArray, Occupied: true,
			slotPayload: &slotPayload{Array: &Array{ObjectHeader: header(), Elements: []ArrayElement{{Value: value}}}},
		},
		"context": {
			Kind: HeapContext, Occupied: true,
			slotPayload: &slotPayload{Context: &Context{Parent: value, Bindings: []Binding{
				{Name: old, Value: value, Initialized: true},
				{Name: old, Indirect: true, Target: old, TargetName: old},
			}}},
		},
		"function": {
			Kind: HeapFunction, Occupied: true,
			slotPayload: &slotPayload{Function: &Function{
				ObjectHeader: header(), Name: value, Environment: value,
				ThisMode: FunctionThisLexical, LexicalThis: value,
				Constants: []Value{value}, Captures: []Value{value},
			}},
		},
		"promise": {
			Kind: HeapPromise, Occupied: true,
			slotPayload: &slotPayload{Promise: &Promise{
				ObjectHeader: header(), State: PromiseFulfilled, Result: value,
				Reactions: []PromiseReaction{{OnFulfilled: value, OnRejected: value, Downstream: value}},
			}},
		},
		"symbol": {
			Kind: HeapSymbol, Occupied: true,
			slotPayload: &slotPayload{Symbol: &Symbol{Description: value}},
		},
		"typed-array": {
			Kind: HeapTypedArray, Occupied: true,
			slotPayload: &slotPayload{TypedArray: &TypedArray{Buffer: old}},
		},
		"map": {
			Kind: HeapMap, Occupied: true,
			slotPayload: &slotPayload{Map: &Map{ObjectHeader: header(), Entries: []MapEntry{{Key: value, Value: value}}}},
		},
		"set": {
			Kind: HeapSet, Occupied: true,
			slotPayload: &slotPayload{Set: &Set{ObjectHeader: header(), Values: []Value{value}}},
		},
		"regexp": {
			Kind: HeapRegExp, Occupied: true,
			slotPayload: &slotPayload{RegExp: &RegExp{Pattern: old}},
		},
		"error": {
			Kind: HeapError, Occupied: true,
			slotPayload: &slotPayload{Error: &ErrorObject{
				ObjectHeader: header(), Message: value, Stack: value,
				Cause: value, HasCause: true, Errors: []Value{value},
			}},
		},
		"iterator": {
			Kind: HeapIterator, Occupied: true,
			slotPayload: &slotPayload{Iterator: &Iterator{ObjectHeader: header(), Target: old}},
		},
	}
	for name, slot := range slots {
		t.Run(name, func(t *testing.T) {
			rewriteSlotReferences(&slot, map[Ref]Ref{old: replacement})
			references := appendSlotReferences(nil, &slot)
			if len(references) == 0 {
				t.Fatal("test slot did not expose any strong references")
			}
			for index, reference := range references {
				if !reference.IsRef() || reference.Ref() != replacement {
					t.Fatalf("strong reference %d = %v, want %s", index, reference, replacement)
				}
			}
		})
	}
}
