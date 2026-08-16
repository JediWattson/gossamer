package memory_test

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func TestNativeIteratorRetainsPositionTargetAndGraphCopy(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(145)
	iteratorRegion := mustRegion(t, store, owner)
	targetRegion := mustRegion(t, store, owner)
	array, _ := store.AllocArray(owner, targetRegion, 3)
	if err := store.SetArrayElement(owner, array, 1, memory.NumberValue(7)); err != nil {
		t.Fatal(err)
	}
	iterator, err := store.AllocIterator(owner, iteratorRegion, array, memory.IteratorArrayEntries)
	if err != nil {
		t.Fatal(err)
	}
	if got := store.EdgeCount(iteratorRegion, targetRegion); got != 1 {
		t.Fatalf("iterator edge = %d, want 1", got)
	}
	first, err := store.AdvanceIterator(owner, iterator)
	if err != nil || first.Done || !first.Pair || first.Key.Number() != 0 || first.Value.Kind() != memory.ValueUndefined {
		t.Fatalf("first step = %#v, %v", first, err)
	}
	reader := realmOwner(146)
	promoted, err := store.Copy(owner, reader, iterator)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.AdvanceIterator(reader, promoted[0])
	if err != nil || second.Done || second.Key.Number() != 1 || second.Value.Number() != 7 {
		t.Fatalf("promoted second step = %#v, %v", second, err)
	}
	snapshot, err := store.DerefIterator(reader, promoted[0])
	if err != nil || snapshot.Next != 2 || snapshot.Target == array {
		t.Fatalf("promoted iterator = %#v, %v", snapshot, err)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}
