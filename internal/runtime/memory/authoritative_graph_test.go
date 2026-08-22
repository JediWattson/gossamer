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
