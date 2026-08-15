package memory_test

import (
	"errors"
	"testing"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func TestNativePromiseSettlementAndReactionDrain(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(60)
	promiseRegion := mustRegion(t, store, owner)
	valueRegion := mustRegion(t, store, owner)
	promise, _ := store.AllocPromise(owner, promiseRegion)
	downstream, _ := store.AllocPromise(owner, valueRegion)
	handler, _ := store.AllocNativeFunction(owner, valueRegion, memory.NullValue(), memory.NullValue(), 1, 1)
	result, _ := store.AllocString(owner, valueRegion, "done")

	if _, err := store.DrainPromiseReactions(owner, promise); !errors.Is(err, memory.ErrPromisePending) {
		t.Fatalf("Drain(pending) = %v, want ErrPromisePending", err)
	}
	reaction := memory.PromiseReaction{
		OnFulfilled: memory.RefValue(handler),
		OnRejected:  memory.RefValue(handler),
		Downstream:  memory.RefValue(downstream),
	}
	if err := store.AddPromiseReaction(owner, promise, reaction); err != nil {
		t.Fatal(err)
	}
	if got := store.EdgeCount(promiseRegion, valueRegion); got != 3 {
		t.Fatalf("reaction edge count = %d, want 3", got)
	}
	if err := store.ResolvePromise(owner, promise, memory.RefValue(promise)); !errors.Is(err, memory.ErrPromiseSelfResolution) {
		t.Fatalf("self resolution = %v, want ErrPromiseSelfResolution", err)
	}
	if err := store.ResolvePromise(owner, promise, memory.RefValue(result)); err != nil {
		t.Fatal(err)
	}
	if err := store.RejectPromise(owner, promise, memory.NumberValue(1)); !errors.Is(err, memory.ErrPromiseSettled) {
		t.Fatalf("second settlement = %v, want ErrPromiseSettled", err)
	}
	if err := store.MarkPromiseHandled(owner, promise); err != nil {
		t.Fatal(err)
	}
	if got := store.EdgeCount(promiseRegion, valueRegion); got != 4 {
		t.Fatalf("settled edge count = %d, want reaction plus result", got)
	}
	settlement, err := store.DrainPromiseReactions(owner, promise)
	if err != nil {
		t.Fatal(err)
	}
	if settlement.State != memory.PromiseFulfilled || settlement.Result.Ref() != result || len(settlement.Reactions) != 1 {
		t.Fatalf("settlement = %#v", settlement)
	}
	if got := store.EdgeCount(promiseRegion, valueRegion); got != 1 {
		t.Fatalf("edge count after drain = %d, want retained result only", got)
	}
	snapshot, err := store.DerefPromise(owner, promise)
	if err != nil || !snapshot.Handled || len(snapshot.Reactions) != 0 {
		t.Fatalf("Promise after drain = %#v, %v", snapshot, err)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func TestNativePromisePromotionPreservesSettlementAndReactions(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(61)
	reader := realmOwner(62)
	region := mustRegion(t, store, owner)
	promise, _ := store.AllocPromise(owner, region)
	downstream, _ := store.AllocPromise(owner, region)
	handler, _ := store.AllocNativeFunction(owner, region, memory.NullValue(), memory.NullValue(), 1, 2)
	result, _ := store.AllocString(owner, region, "rejected")
	if err := store.AddPromiseReaction(owner, promise, memory.PromiseReaction{
		OnFulfilled: memory.RefValue(handler),
		OnRejected:  memory.RefValue(handler),
		Downstream:  memory.RefValue(downstream),
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.RejectPromise(owner, promise, memory.RefValue(result)); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkPromiseHandled(owner, promise); err != nil {
		t.Fatal(err)
	}

	promoted, err := store.Promote(owner, promise)
	if err != nil {
		t.Fatal(err)
	}
	copyPromise, err := store.DerefPromise(reader, promoted[0])
	if err != nil {
		t.Fatal(err)
	}
	if copyPromise.State != memory.PromiseRejected || !copyPromise.Handled || len(copyPromise.Reactions) != 1 {
		t.Fatalf("promoted Promise = %#v", copyPromise)
	}
	if copyPromise.Result.Ref() == result || copyPromise.Reactions[0].OnFulfilled.Ref() == handler {
		t.Fatal("promotion reused source Promise graph")
	}
	if copyPromise.Reactions[0].OnFulfilled.Ref() != copyPromise.Reactions[0].OnRejected.Ref() {
		t.Fatal("promotion did not preserve shared handler alias")
	}
	if err := store.ReleaseOwner(owner); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DerefPromise(owner, promise); !errors.Is(err, memory.ErrStaleRef) {
		t.Fatalf("source Promise after release = %v, want ErrStaleRef", err)
	}
	if got, err := store.DerefString(reader, copyPromise.Result.Ref()); err != nil || got != "rejected" {
		t.Fatalf("promoted result = %q, %v", got, err)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}
