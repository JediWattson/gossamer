package memory_test

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func TestEveryHeapKindSurvivesCopyPromotionAndBulkRelease(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(401)
	copyOwner := realmOwner(402)
	reader := realmOwner(403)
	region := mustRegion(t, store, owner)
	must := func(ref memory.Ref, err error) memory.Ref {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return ref
	}

	name := must(store.AllocString(owner, region, "value"))
	pattern := must(store.AllocString(owner, region, "v+"))
	stack := must(store.AllocString(owner, region, "at typed graph"))
	root := mustAlloc(t, store, owner, region)
	cause := mustAlloc(t, store, owner, region)
	object := must(store.AllocObject(owner, region))
	array := must(store.AllocArray(owner, region, 1))
	if err := store.SetArrayElement(owner, array, 0, memory.RefValue(object)); err != nil {
		t.Fatal(err)
	}
	context := must(store.AllocContext(owner, region, memory.NullValue()))
	if err := store.DeclareBinding(owner, context, name, true); err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeBinding(owner, context, name, memory.RefValue(array)); err != nil {
		t.Fatal(err)
	}
	bigint := must(store.AllocBigInt(owner, region, false, []byte{0x12, 0x34}))
	function := must(store.AllocBytecodeFunction(owner, region, memory.RefValue(name), memory.RefValue(context), 1, []byte{1, 2, 3}, []memory.Value{memory.RefValue(object), memory.RefValue(bigint)}))
	symbol := must(store.AllocSymbol(owner, region, memory.RefValue(name)))
	buffer := must(store.AllocArrayBuffer(owner, region, []byte{0, 1, 2, 3, 4, 5, 6, 7}))
	view := must(store.AllocTypedArray(owner, region, buffer, memory.ElementUint16, 0, 4))
	nativeMap := must(store.AllocMap(owner, region))
	if err := store.MapSet(owner, nativeMap, memory.RefValue(symbol), memory.RefValue(view)); err != nil {
		t.Fatal(err)
	}
	nativeSet := must(store.AllocSet(owner, region))
	if err := store.SetAdd(owner, nativeSet, memory.RefValue(nativeMap)); err != nil {
		t.Fatal(err)
	}
	date := must(store.AllocDate(owner, region, 1234.9))
	expression := must(store.AllocRegExp(owner, region, pattern, "gi"))
	aggregate := must(store.AllocError(owner, region, memory.ErrorAggregate, memory.RefValue(name)))
	if err := store.SetErrorStack(owner, aggregate, memory.RefValue(stack)); err != nil {
		t.Fatal(err)
	}
	if err := store.SetErrorCause(owner, aggregate, memory.RefValue(cause)); err != nil {
		t.Fatal(err)
	}
	if err := store.SetAggregateErrors(owner, aggregate, []memory.Value{memory.RefValue(date), memory.RefValue(expression), memory.RefValue(cause)}); err != nil {
		t.Fatal(err)
	}
	promise := must(store.AllocPromise(owner, region))
	if err := store.AddPromiseReaction(owner, promise, memory.PromiseReaction{OnFulfilled: memory.RefValue(function), OnRejected: memory.NullValue(), Downstream: memory.NullValue()}); err != nil {
		t.Fatal(err)
	}
	if err := store.ResolvePromise(owner, promise, memory.RefValue(aggregate)); err != nil {
		t.Fatal(err)
	}
	if err := store.SetProperty(owner, object, name, memory.RefValue(promise)); err != nil {
		t.Fatal(err)
	}

	refs := []memory.Ref{name, pattern, stack, cause, object, array, context, function, promise, bigint, symbol, buffer, view, nativeMap, nativeSet, date, expression, aggregate}
	for field, ref := range refs {
		if err := store.Set(owner, root, field, memory.RefValue(ref)); err != nil {
			t.Fatal(err)
		}
	}
	assertStoreInvariants(t, store, "all heap kinds linked")
	base := store.Stats()
	assertEveryHeapKindCount(t, base, 1)

	copied, err := store.Copy(owner, copyOwner, root)
	if err != nil {
		t.Fatal(err)
	}
	assertStoreInvariants(t, store, "all heap kinds copied")
	assertEveryHeapKindCount(t, store.Stats(), 2)
	copyRoot, err := store.Deref(copyOwner, copied[0])
	if err != nil || len(copyRoot.Fields) != len(refs) {
		t.Fatalf("copied root = %#v, %v", copyRoot, err)
	}

	promoted, err := store.Promote(owner, root)
	if err != nil {
		t.Fatal(err)
	}
	assertStoreInvariants(t, store, "all heap kinds promoted")
	assertEveryHeapKindCount(t, store.Stats(), 3)
	if promotedRoot, err := store.Deref(reader, promoted[0]); err != nil || len(promotedRoot.Fields) != len(refs) {
		t.Fatalf("promoted root = %#v, %v", promotedRoot, err)
	}

	if err := store.ReleaseOwner(owner); err != nil {
		t.Fatal(err)
	}
	if err := store.ReleaseOwner(copyOwner); err != nil {
		t.Fatal(err)
	}
	assertStoreInvariants(t, store, "private graphs released")
	assertEveryHeapKindCount(t, store.Stats(), 1)
	if _, err := store.Deref(reader, promoted[0]); err != nil {
		t.Fatalf("published graph after private release: %v", err)
	}
}

func assertEveryHeapKindCount(t *testing.T, stats memory.Stats, multiplier uint64) {
	t.Helper()
	if stats.LiveSlots != 19*multiplier || stats.LiveCells != 2*multiplier || stats.LiveStrings != 3*multiplier || stats.LiveObjects != multiplier || stats.LiveArrays != multiplier || stats.LiveContexts != multiplier || stats.LiveFunctions != multiplier || stats.LivePromises != multiplier || stats.LiveBigInts != multiplier || stats.LiveSymbols != multiplier || stats.LiveArrayBuffers != multiplier || stats.LiveTypedArrays != multiplier || stats.LiveMaps != multiplier || stats.LiveSets != multiplier || stats.LiveDates != multiplier || stats.LiveRegExps != multiplier || stats.LiveErrors != multiplier || stats.LiveRegions != multiplier || stats.LiveBytes != 34*multiplier {
		t.Fatalf("typed stats at multiplier %d = %#v", multiplier, stats)
	}
}
