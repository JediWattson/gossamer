package memory_test

import (
	"errors"
	"testing"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

func TestStaleRefNeverResolvesAfterSlotOrRegionReuse(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(1)
	region := mustRegion(t, store, owner)
	first := mustAlloc(t, store, owner, region)
	if err := store.Set(owner, first, 0, memory.NumberValue(1)); err != nil {
		t.Fatal(err)
	}
	if err := store.Free(owner, first); err != nil {
		t.Fatal(err)
	}
	second := mustAlloc(t, store, owner, region)
	if second.Slot != first.Slot {
		t.Fatalf("reused slot = %d, want %d", second.Slot, first.Slot)
	}
	if second.Gen == first.Gen {
		t.Fatalf("reused generation = %d, want a new generation", second.Gen)
	}
	if _, err := store.Deref(owner, first); !errors.Is(err, memory.ErrStaleRef) {
		t.Fatalf("Deref(stale slot) error = %v, want ErrStaleRef", err)
	}

	if err := store.DestroyRegion(owner, region); err != nil {
		t.Fatal(err)
	}
	replacement := mustRegion(t, store, owner)
	if replacement == region {
		t.Fatalf("destroyed region ID R%d was reused", region)
	}
	if _, err := store.Deref(owner, second); !errors.Is(err, memory.ErrStaleRef) {
		t.Fatalf("Deref(destroyed region) error = %v, want ErrStaleRef", err)
	}
}

func TestNativeStringHasTypedLifetimeAndPromotion(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(14)
	reader := realmOwner(15)
	region := mustRegion(t, store, owner)
	root := mustAlloc(t, store, owner, region)
	text, err := store.AllocString(owner, region, "hello, region heap")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := store.DerefString(owner, text); err != nil || got != "hello, region heap" {
		t.Fatalf("DerefString() = %q, %v", got, err)
	}
	if _, err := store.DerefCell(owner, text); !errors.Is(err, memory.ErrTypeMismatch) {
		t.Fatalf("DerefCell(String) error = %v, want ErrTypeMismatch", err)
	}
	if _, err := store.DerefString(owner, root); !errors.Is(err, memory.ErrTypeMismatch) {
		t.Fatalf("DerefString(Cell) error = %v, want ErrTypeMismatch", err)
	}
	if err := store.Set(owner, root, 0, memory.RefValue(text)); err != nil {
		t.Fatal(err)
	}

	promoted, err := store.Promote(owner, root)
	if err != nil {
		t.Fatal(err)
	}
	promotedRoot, err := store.DerefCell(reader, promoted[0])
	if err != nil {
		t.Fatal(err)
	}
	promotedText := promotedRoot.Fields[0].Ref()
	if promotedText == text {
		t.Fatal("promotion reused source String Ref")
	}
	if got, err := store.DerefString(reader, promotedText); err != nil || got != "hello, region heap" {
		t.Fatalf("promoted DerefString() = %q, %v", got, err)
	}
	if err := store.ReleaseOwner(owner); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DerefString(owner, text); !errors.Is(err, memory.ErrStaleRef) {
		t.Fatalf("source String after release = %v, want ErrStaleRef", err)
	}
	stats := store.Stats()
	if stats.LiveSlots != 2 || stats.LiveCells != 1 || stats.LiveStrings != 1 || stats.LiveBytes != uint64(len("hello, region heap")) {
		t.Fatalf("Stats() = %#v", stats)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func TestWriteBarrierCountsEveryCrossRegionField(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(2)
	fromRegion := mustRegion(t, store, owner)
	toRegion := mustRegion(t, store, owner)
	from := mustAlloc(t, store, owner, fromRegion)
	to := mustAlloc(t, store, owner, toRegion)

	if err := store.Set(owner, from, 0, memory.RefValue(to)); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(owner, from, 1, memory.RefValue(to)); err != nil {
		t.Fatal(err)
	}
	if got := store.EdgeCount(fromRegion, toRegion); got != 2 {
		t.Fatalf("edge count after two fields = %d, want 2", got)
	}
	edges := store.Edges()
	if len(edges) != 1 || edges[0] != (memory.RegionEdge{From: fromRegion, To: toRegion, Count: 2}) {
		t.Fatalf("Edges() = %#v", edges)
	}

	if err := store.Set(owner, from, 0, memory.UndefinedValue()); err != nil {
		t.Fatal(err)
	}
	if got := store.EdgeCount(fromRegion, toRegion); got != 1 {
		t.Fatalf("edge count after one unlink = %d, want 1", got)
	}
	if err := store.Set(owner, from, 1, memory.UndefinedValue()); err != nil {
		t.Fatal(err)
	}
	if got := store.EdgeCount(fromRegion, toRegion); got != 0 {
		t.Fatalf("edge count after final unlink = %d, want 0", got)
	}
	if edges := store.Edges(); len(edges) != 0 {
		t.Fatalf("zero-count edge was retained: %#v", edges)
	}
}

func TestPrivateRegionRejectsNonOwnerAccessAndReferences(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	firstOwner := realmOwner(3)
	secondOwner := realmOwner(4)
	firstRegion := mustRegion(t, store, firstOwner)
	secondRegion := mustRegion(t, store, secondOwner)
	first := mustAlloc(t, store, firstOwner, firstRegion)
	second := mustAlloc(t, store, secondOwner, secondRegion)

	if _, err := store.Deref(secondOwner, first); !errors.Is(err, memory.ErrAccessDenied) {
		t.Fatalf("foreign Deref error = %v, want ErrAccessDenied", err)
	}
	if _, err := store.Alloc(secondOwner, firstRegion); !errors.Is(err, memory.ErrAccessDenied) {
		t.Fatalf("foreign Alloc error = %v, want ErrAccessDenied", err)
	}
	if err := store.Set(secondOwner, first, 0, memory.NumberValue(1)); !errors.Is(err, memory.ErrAccessDenied) {
		t.Fatalf("foreign Set error = %v, want ErrAccessDenied", err)
	}
	if err := store.Set(firstOwner, first, 0, memory.RefValue(second)); !errors.Is(err, memory.ErrAccessDenied) {
		t.Fatalf("cross-owner private Set error = %v, want ErrAccessDenied", err)
	}
}

func TestDestroyRegionFailsWithIncomingReference(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(5)
	fromRegion := mustRegion(t, store, owner)
	toRegion := mustRegion(t, store, owner)
	from := mustAlloc(t, store, owner, fromRegion)
	to := mustAlloc(t, store, owner, toRegion)
	if err := store.Set(owner, from, 0, memory.RefValue(to)); err != nil {
		t.Fatal(err)
	}

	if err := store.DestroyRegion(owner, toRegion); !errors.Is(err, memory.ErrRegionReferenced) {
		t.Fatalf("DestroyRegion(referenced) error = %v, want ErrRegionReferenced", err)
	}
	if _, err := store.Deref(owner, to); err != nil {
		t.Fatalf("failed destruction damaged target: %v", err)
	}
	if err := store.Set(owner, from, 0, memory.UndefinedValue()); err != nil {
		t.Fatal(err)
	}
	if err := store.DestroyRegion(owner, toRegion); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Deref(owner, to); !errors.Is(err, memory.ErrStaleRef) {
		t.Fatalf("destroyed target error = %v, want ErrStaleRef", err)
	}
}

func TestFreeFailsWhileAnotherCellStillReferencesSlot(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(51)
	region := mustRegion(t, store, owner)
	parent := mustAlloc(t, store, owner, region)
	child := mustAlloc(t, store, owner, region)
	if err := store.Set(owner, parent, 0, memory.RefValue(child)); err != nil {
		t.Fatal(err)
	}
	if err := store.Free(owner, child); !errors.Is(err, memory.ErrCellReferenced) {
		t.Fatalf("Free(referenced) error = %v, want ErrCellReferenced", err)
	}
	if err := store.Set(owner, parent, 0, memory.UndefinedValue()); err != nil {
		t.Fatal(err)
	}
	if err := store.Free(owner, child); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Deref(owner, child); !errors.Is(err, memory.ErrStaleRef) {
		t.Fatalf("freed child error = %v, want ErrStaleRef", err)
	}
}

func TestBulkOwnerReleaseDestroysAnInternalGraph(t *testing.T) {
	t.Parallel()

	ledger := ownership.NewLedger()
	store := memory.NewStore(ledger)
	defer store.Close()
	owner := realmOwner(6)
	region := mustRegion(t, store, owner)
	a := mustAlloc(t, store, owner, region)
	b := mustAlloc(t, store, owner, region)
	c := mustAlloc(t, store, owner, region)
	if err := store.Set(owner, a, 0, memory.RefValue(b)); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(owner, b, 0, memory.RefValue(c)); err != nil {
		t.Fatal(err)
	}

	if err := store.ReleaseOwner(owner); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []memory.Ref{a, b, c} {
		if _, err := store.Deref(owner, ref); !errors.Is(err, memory.ErrStaleRef) {
			t.Errorf("Deref(%s) error = %v, want ErrStaleRef", ref, err)
		}
	}
	stats := store.Stats()
	if stats.LiveCells != 0 || stats.LiveRegions != 0 || stats.BulkRegionReleases != 1 {
		t.Fatalf("Store Stats() = %#v", stats)
	}
	ledgerStats := ledger.Stats()
	if ledgerStats.ObjectsDestroyed != 3 || ledgerStats.LiveObjects != 0 || ledgerStats.BulkRegionReleases != 1 {
		t.Fatalf("Ledger Stats() = %#v", ledgerStats)
	}
}

func TestTransferMovesConnectedRegionsThroughQueue(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	source := realmOwner(7)
	queue := ownership.OwnerID{Kind: ownership.OwnerQueue, Value: 70}
	destination := realmOwner(8)
	if err := store.RegisterOwner(queue); err != nil {
		t.Fatal(err)
	}
	if err := store.RegisterOwner(destination); err != nil {
		t.Fatal(err)
	}
	firstRegion := mustRegion(t, store, source)
	secondRegion := mustRegion(t, store, source)
	first := mustAlloc(t, store, source, firstRegion)
	second := mustAlloc(t, store, source, secondRegion)
	if err := store.Set(source, first, 0, memory.RefValue(second)); err != nil {
		t.Fatal(err)
	}

	if err := store.Transfer(source, queue, first); err != nil {
		t.Fatal(err)
	}
	for _, id := range []memory.RegionID{firstRegion, secondRegion} {
		region, err := store.Region(id)
		if err != nil {
			t.Fatal(err)
		}
		if region.State != memory.RegionInTransit || region.Owner != queue {
			t.Fatalf("queued region R%d = %#v", id, region)
		}
	}
	if _, err := store.Deref(source, first); !errors.Is(err, memory.ErrRegionInTransit) {
		t.Fatalf("source access while queued = %v, want ErrRegionInTransit", err)
	}
	if err := store.ReleaseOwner(source); err != nil {
		t.Fatal(err)
	}
	if err := store.Accept(queue, destination, first); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Deref(destination, first); err != nil {
		t.Fatalf("destination Deref() = %v", err)
	}
	if _, err := store.Deref(source, first); !errors.Is(err, memory.ErrAccessDenied) {
		t.Fatalf("former owner Deref() = %v, want ErrAccessDenied", err)
	}
	if err := store.ReleaseOwner(queue); err != nil {
		t.Fatal(err)
	}
	if err := store.ReleaseOwner(destination); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Deref(destination, second); !errors.Is(err, memory.ErrStaleRef) {
		t.Fatalf("Deref after destination release = %v, want ErrStaleRef", err)
	}
}

func TestPublishMakesOutgoingRegionGraphImmutableAndShared(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	publisher := realmOwner(9)
	reader := realmOwner(10)
	firstRegion := mustRegion(t, store, publisher)
	secondRegion := mustRegion(t, store, publisher)
	first := mustAlloc(t, store, publisher, firstRegion)
	second := mustAlloc(t, store, publisher, secondRegion)
	if err := store.Set(publisher, first, 0, memory.RefValue(second)); err != nil {
		t.Fatal(err)
	}
	if err := store.Publish(publisher, first); err != nil {
		t.Fatal(err)
	}
	if err := store.ReleaseOwner(publisher); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []memory.Ref{first, second} {
		if _, err := store.Deref(reader, ref); err != nil {
			t.Errorf("published Deref(%s) = %v", ref, err)
		}
	}
	if err := store.Set(reader, first, 0, memory.UndefinedValue()); !errors.Is(err, memory.ErrImmutableRegion) {
		t.Fatalf("Set(published) error = %v, want ErrImmutableRegion", err)
	}
	if err := store.ValidateSend(reader, first); err != nil {
		t.Fatalf("ValidateSend(published) = %v", err)
	}
}

func TestPromoteCopiesOnlyReachableCellsIntoSharedRegion(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(91)
	reader := realmOwner(92)
	region := mustRegion(t, store, owner)
	a := mustAlloc(t, store, owner, region)
	b := mustAlloc(t, store, owner, region)
	c := mustAlloc(t, store, owner, region)
	if err := store.Set(owner, a, 0, memory.RefValue(b)); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(owner, b, 0, memory.RefValue(c)); err != nil {
		t.Fatal(err)
	}

	promoted, err := store.Promote(owner, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(promoted) != 1 || promoted[0] == b || promoted[0].Region == region {
		t.Fatalf("Promote(B) = %#v", promoted)
	}
	promotedB := promoted[0]
	promotedCell, err := store.Deref(reader, promotedB)
	if err != nil {
		t.Fatal(err)
	}
	if len(promotedCell.Fields) != 1 || !promotedCell.Fields[0].IsRef() {
		t.Fatalf("promoted B = %#v", promotedCell)
	}
	promotedC := promotedCell.Fields[0].Ref()
	if promotedC.Region != promotedB.Region || promotedC == c {
		t.Fatalf("promoted C = %s, promoted B = %s, original C = %s", promotedC, promotedB, c)
	}
	if snapshot, err := store.Region(promotedB.Region); err != nil {
		t.Fatal(err)
	} else if snapshot.State != memory.RegionPublished || snapshot.Owner.Kind != ownership.OwnerShared {
		t.Fatalf("promoted region = %#v", snapshot)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}

	if err := store.ReleaseOwner(owner); err != nil {
		t.Fatal(err)
	}
	for _, original := range []memory.Ref{a, b, c} {
		if _, err := store.Deref(owner, original); !errors.Is(err, memory.ErrStaleRef) {
			t.Errorf("original %s after release = %v, want ErrStaleRef", original, err)
		}
	}
	if _, err := store.Deref(reader, promotedB); err != nil {
		t.Fatalf("promoted B did not survive source release: %v", err)
	}
	stats := store.Stats()
	if stats.LiveCells != 2 || stats.LiveRegions != 1 {
		t.Fatalf("Stats() after source release = %#v, want only B' and C'", stats)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func TestCopyClonesCyclesWithoutSharingMutableCells(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	source := realmOwner(11)
	queue := ownership.OwnerID{Kind: ownership.OwnerQueue, Value: 110}
	destination := realmOwner(12)
	if err := store.RegisterOwner(queue); err != nil {
		t.Fatal(err)
	}
	if err := store.RegisterOwner(destination); err != nil {
		t.Fatal(err)
	}
	region := mustRegion(t, store, source)
	first := mustAlloc(t, store, source, region)
	second := mustAlloc(t, store, source, region)
	if err := store.Set(source, first, 0, memory.RefValue(second)); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(source, second, 0, memory.RefValue(first)); err != nil {
		t.Fatal(err)
	}

	copied, err := store.Copy(source, queue, first)
	if err != nil {
		t.Fatal(err)
	}
	if len(copied) != 1 || copied[0] == first {
		t.Fatalf("Copy() = %#v", copied)
	}
	if err := store.Accept(queue, destination, copied...); err != nil {
		t.Fatal(err)
	}
	copyFirst, err := store.Deref(destination, copied[0])
	if err != nil {
		t.Fatal(err)
	}
	copySecond := copyFirst.Fields[0].Ref()
	if copySecond.Region != copied[0].Region || copySecond == second {
		t.Fatalf("copied child = %s, root = %s, source child = %s", copySecond, copied[0], second)
	}
	copySecondCell, err := store.Deref(destination, copySecond)
	if err != nil {
		t.Fatal(err)
	}
	if back := copySecondCell.Fields[0].Ref(); back != copied[0] {
		t.Fatalf("copied cycle points to %s, want %s", back, copied[0])
	}
	if err := store.Set(destination, copied[0], 0, memory.UndefinedValue()); err != nil {
		t.Fatal(err)
	}
	sourceCell, err := store.Deref(source, first)
	if err != nil {
		t.Fatal(err)
	}
	if !sourceCell.Fields[0].IsRef() || sourceCell.Fields[0].Ref() != second {
		t.Fatalf("copy mutation changed source: %#v", sourceCell)
	}
}

func TestUnqualifiedSendRejectsPrivateRef(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(13)
	region := mustRegion(t, store, owner)
	ref := mustAlloc(t, store, owner, region)
	if err := store.ValidateSend(owner, ref); !errors.Is(err, memory.ErrExplicitSendRequired) {
		t.Fatalf("ValidateSend(private) error = %v, want ErrExplicitSendRequired", err)
	}
}

func realmOwner(id uint64) ownership.OwnerID {
	return ownership.OwnerID{Kind: ownership.OwnerRealm, Value: id}
}

func mustRegion(t *testing.T, store *memory.Store, owner ownership.OwnerID) memory.RegionID {
	t.Helper()
	region, err := store.NewRegion(owner)
	if err != nil {
		t.Fatal(err)
	}
	return region
}

func mustAlloc(t *testing.T, store *memory.Store, owner ownership.OwnerID, region memory.RegionID) memory.Ref {
	t.Helper()
	ref, err := store.Alloc(owner, region)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}
