package memory_test

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
	"github.com/JediWattson/gossamer/internal/runtime/ownership"
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
	weakMap := must(store.AllocWeakMap(owner, region))
	if err := store.WeakMapSet(owner, weakMap, object, memory.RefValue(aggregate)); err != nil {
		t.Fatal(err)
	}
	weakSet := must(store.AllocWeakSet(owner, region))
	if err := store.WeakSetAdd(owner, weakSet, object); err != nil {
		t.Fatal(err)
	}
	hostObject := must(store.AllocHostObject(owner, region, memory.HostObject{Class: 7, Scope: 11, Identity: 13}))
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

	refs := []memory.Ref{name, pattern, stack, cause, object, array, context, function, promise, bigint, symbol, buffer, view, nativeMap, nativeSet, date, expression, aggregate, weakMap, weakSet, hostObject}
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
	copiedWeakMap, err := store.DerefWeakMap(copyOwner, copyRoot.Fields[18].Ref())
	if err != nil || len(copiedWeakMap.Entries) != 1 || copiedWeakMap.Entries[0].Key != copyRoot.Fields[4].Ref() || copiedWeakMap.Entries[0].Value.Ref() != copyRoot.Fields[17].Ref() {
		t.Fatalf("copied weak map = %#v, %v", copiedWeakMap, err)
	}
	copiedWeakSet, err := store.DerefWeakSet(copyOwner, copyRoot.Fields[19].Ref())
	if err != nil || len(copiedWeakSet.Keys) != 1 || copiedWeakSet.Keys[0] != copyRoot.Fields[4].Ref() {
		t.Fatalf("copied weak set = %#v, %v", copiedWeakSet, err)
	}
	if copiedHost, err := store.DerefHostObject(copyOwner, copyRoot.Fields[20].Ref()); err != nil || copiedHost != (memory.HostObject{Class: 7, Scope: 11, Identity: 13}) {
		t.Fatalf("copied host object = %#v, %v", copiedHost, err)
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

func TestTypedWriteBarriersReuseOneEscapedGraph(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	task := ownership.OwnerID{Kind: ownership.OwnerTask, Value: 902}
	document := ownership.OwnerID{Kind: ownership.OwnerDocument, Value: 902}
	taskRegion := mustRegion(t, store, task)
	documentRegion := mustRegion(t, store, document)
	source := mustAlloc(t, store, task, taskRegion)
	leaf := mustAlloc(t, store, task, taskRegion)
	if err := store.Set(task, source, 0, memory.RefValue(leaf)); err != nil {
		t.Fatal(err)
	}
	name, err := store.AllocString(document, documentRegion, "escaped")
	if err != nil {
		t.Fatal(err)
	}
	object, err := store.AllocObject(document, documentRegion)
	if err != nil {
		t.Fatal(err)
	}
	array, err := store.AllocArray(document, documentRegion, 0)
	if err != nil {
		t.Fatal(err)
	}
	nativeMap, err := store.AllocMap(document, documentRegion)
	if err != nil {
		t.Fatal(err)
	}
	nativeSet, err := store.AllocSet(document, documentRegion)
	if err != nil {
		t.Fatal(err)
	}
	errorRef, err := store.AllocError(document, documentRegion, memory.ErrorType, memory.NullValue())
	if err != nil {
		t.Fatal(err)
	}

	if err := store.SetProperty(document, object, name, memory.RefValue(source)); err != nil {
		t.Fatal(err)
	}
	if err := store.SetArrayElement(document, array, 0, memory.RefValue(source)); err != nil {
		t.Fatal(err)
	}
	if err := store.MapSet(document, nativeMap, memory.RefValue(name), memory.RefValue(source)); err != nil {
		t.Fatal(err)
	}
	if err := store.SetAdd(document, nativeSet, memory.RefValue(source)); err != nil {
		t.Fatal(err)
	}
	if err := store.SetErrorCause(document, errorRef, memory.RefValue(source)); err != nil {
		t.Fatal(err)
	}

	objectValue, found, err := store.GetOwnProperty(document, object, name)
	if err != nil || !found {
		t.Fatalf("object escaped value = %v, %t, %v", objectValue, found, err)
	}
	promoted := objectValue.Ref()
	arrayValue, found, err := store.ArrayElement(document, array, 0)
	if err != nil || !found || arrayValue.Ref() != promoted {
		t.Fatalf("array escaped value = %v, %t, %v, want %v", arrayValue, found, err, promoted)
	}
	mapValue, found, err := store.MapGet(document, nativeMap, memory.RefValue(name))
	if err != nil || !found || mapValue.Ref() != promoted {
		t.Fatalf("map escaped value = %v, %t, %v, want %v", mapValue, found, err, promoted)
	}
	nativeSetValue, err := store.DerefSet(document, nativeSet)
	if err != nil || len(nativeSetValue.Values) != 1 || nativeSetValue.Values[0].Ref() != promoted {
		t.Fatalf("set escaped value = %#v, %v, want %v", nativeSetValue, err, promoted)
	}
	errorValue, err := store.DerefError(document, errorRef)
	if err != nil || !errorValue.HasCause || errorValue.Cause.Ref() != promoted {
		t.Fatalf("error escaped cause = %#v, %v, want %v", errorValue, err, promoted)
	}
	if stats := store.Stats(); stats.AutomaticPromotions != 1 || stats.PromotionCacheHits != 4 {
		t.Fatalf("typed promotion stats = %#v", stats)
	}
	if err := store.ReleaseOwner(task); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Deref(document, promoted); err != nil {
		t.Fatalf("escaped graph after task release: %v", err)
	}
	assertStoreInvariants(t, store, "typed escape barriers")
}

func assertEveryHeapKindCount(t *testing.T, stats memory.Stats, multiplier uint64) {
	t.Helper()
	if stats.LiveSlots != 22*multiplier || stats.LiveCells != 2*multiplier || stats.LiveStrings != 3*multiplier || stats.LiveObjects != multiplier || stats.LiveArrays != multiplier || stats.LiveContexts != multiplier || stats.LiveFunctions != multiplier || stats.LivePromises != multiplier || stats.LiveBigInts != multiplier || stats.LiveSymbols != multiplier || stats.LiveArrayBuffers != multiplier || stats.LiveTypedArrays != multiplier || stats.LiveMaps != multiplier || stats.LiveSets != multiplier || stats.LiveDates != multiplier || stats.LiveRegExps != multiplier || stats.LiveErrors != multiplier || stats.LiveWeakMaps != multiplier || stats.LiveWeakSets != multiplier || stats.LiveHostObjects != multiplier || stats.LiveRegions != multiplier || stats.LiveBytes != 34*multiplier {
		t.Fatalf("typed stats at multiplier %d = %#v", multiplier, stats)
	}
}
