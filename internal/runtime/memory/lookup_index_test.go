package memory

import (
	"errors"
	"fmt"
	"testing"

	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

func lookupTestOwner(id uint64) ownership.OwnerID {
	return ownership.OwnerID{Kind: ownership.OwnerRealm, Value: id}
}

func lookupTestRegion(t testing.TB, store *Store, owner ownership.OwnerID) RegionID {
	t.Helper()
	region, err := store.NewRegion(owner)
	if err != nil {
		t.Fatal(err)
	}
	return region
}

func TestAdaptiveBindingLookupPreservesSemanticNamesAndGraphCopy(t *testing.T) {
	t.Parallel()

	store := NewStore(nil)
	defer store.Close()
	owner := lookupTestOwner(1001)
	reader := lookupTestOwner(1002)
	region := lookupTestRegion(t, store, owner)
	context, err := store.AllocContext(owner, region, NullValue())
	if err != nil {
		t.Fatal(err)
	}
	var names []Ref
	for index := 0; index < 12; index++ {
		name, allocErr := store.AllocString(owner, region, fmt.Sprintf("binding-%02d", index))
		if allocErr != nil {
			t.Fatal(allocErr)
		}
		names = append(names, name)
		if err := store.DeclareBinding(owner, context, name, true); err != nil {
			t.Fatal(err)
		}
		if err := store.InitializeBinding(owner, context, name, NumberValue(float64(index))); err != nil {
			t.Fatal(err)
		}
	}
	equivalent, err := store.AllocString(owner, region, "binding-10")
	if err != nil {
		t.Fatal(err)
	}
	if value, found, err := store.ResolveBinding(owner, context, equivalent); err != nil || !found || value.Number() != 10 {
		t.Fatalf("indexed ResolveBinding(equal String) = %#v, %t, %v", value, found, err)
	}

	store.mutex.Lock()
	_, slot, _ := store.slotLocked(context)
	if got := len(slot.context().bindingLookup); got != len(names) {
		store.mutex.Unlock()
		t.Fatalf("binding lookup entries = %d, want %d", got, len(names))
	}
	store.mutex.Unlock()

	copied, err := store.Copy(owner, reader, context)
	if err != nil {
		t.Fatal(err)
	}
	copySnapshot, err := store.DerefContext(reader, copied[0])
	if err != nil {
		t.Fatal(err)
	}
	if copySnapshot.bindingLookup != nil {
		t.Fatal("DerefContext leaked the Store's private binding lookup")
	}
	copyLookup, err := store.AllocString(reader, copied[0].Region, "binding-10")
	if err != nil {
		t.Fatal(err)
	}
	if value, found, err := store.ResolveBinding(reader, copied[0], copyLookup); err != nil || !found || value.Number() != 10 {
		t.Fatalf("copied indexed ResolveBinding = %#v, %t, %v", value, found, err)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func TestAdaptivePropertyLookupHandlesStringsSymbolsReplacementDeletionAndPromotion(t *testing.T) {
	t.Parallel()

	store := NewStore(nil)
	defer store.Close()
	owner := lookupTestOwner(1011)
	reader := lookupTestOwner(1012)
	region := lookupTestRegion(t, store, owner)
	object, err := store.AllocObject(owner, region)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]Ref, 10)
	for index := range names {
		names[index], err = store.AllocString(owner, region, fmt.Sprintf("property-%02d", index))
		if err != nil {
			t.Fatal(err)
		}
		if err := store.SetProperty(owner, object, names[index], NumberValue(float64(index))); err != nil {
			t.Fatal(err)
		}
	}
	description, _ := store.AllocString(owner, region, "same description")
	firstSymbol, _ := store.AllocSymbol(owner, region, RefValue(description))
	secondSymbol, _ := store.AllocSymbol(owner, region, RefValue(description))
	if err := store.SetProperty(owner, object, firstSymbol, NumberValue(100)); err != nil {
		t.Fatal(err)
	}
	if err := store.SetProperty(owner, object, secondSymbol, NumberValue(200)); err != nil {
		t.Fatal(err)
	}

	equivalent, _ := store.AllocString(owner, region, "property-05")
	if err := store.SetProperty(owner, object, equivalent, NumberValue(55)); err != nil {
		t.Fatal(err)
	}
	if value, found, err := store.GetOwnProperty(owner, object, names[5]); err != nil || !found || value.Number() != 55 {
		t.Fatalf("replaced indexed String property = %#v, %t, %v", value, found, err)
	}
	for _, check := range []struct {
		name Ref
		want float64
	}{{firstSymbol, 100}, {secondSymbol, 200}} {
		if value, found, err := store.GetOwnProperty(owner, object, check.name); err != nil || !found || value.Number() != check.want {
			t.Fatalf("indexed Symbol property %s = %#v, %t, %v", check.name, value, found, err)
		}
	}

	if deleted, err := store.DeleteProperty(owner, object, names[2]); err != nil || !deleted {
		t.Fatalf("DeleteProperty(indexed) = %t, %v", deleted, err)
	}
	if _, found, err := store.GetOwnProperty(owner, object, names[2]); err != nil || found {
		t.Fatalf("deleted indexed property found = %t, %v", found, err)
	}
	if value, found, err := store.GetOwnProperty(owner, object, names[9]); err != nil || !found || value.Number() != 9 {
		t.Fatalf("shifted indexed property = %#v, %t, %v", value, found, err)
	}

	promoted, err := store.Promote(owner, object)
	if err != nil {
		t.Fatal(err)
	}
	promotedHeader, err := store.DerefObjectHeader(reader, promoted[0])
	if err != nil {
		t.Fatal(err)
	}
	if promotedHeader.propertyLookup != nil {
		t.Fatal("DerefObjectHeader leaked the Store's private property lookup")
	}
	var promotedName Ref
	for _, property := range promotedHeader.Properties {
		if text, textErr := store.DerefString(reader, property.Name); textErr == nil && text == "property-09" {
			promotedName = property.Name
			break
		}
	}
	if promotedName == (Ref{}) {
		t.Fatal("promoted property-09 name not found")
	}
	if value, found, err := store.GetOwnProperty(reader, promoted[0], promotedName); err != nil || !found || value.Number() != 9 {
		t.Fatalf("promoted indexed property = %#v, %t, %v", value, found, err)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func TestResolvePropertyDescriptorObservesPrototypeMutationAndShadowing(t *testing.T) {
	t.Parallel()

	store := NewStore(nil)
	defer store.Close()
	owner := lookupTestOwner(1021)
	other := lookupTestOwner(1022)
	region := lookupTestRegion(t, store, owner)
	firstPrototype, _ := store.AllocObject(owner, region)
	secondPrototype, _ := store.AllocObject(owner, region)
	child, _ := store.AllocObject(owner, region)
	for index := 0; index < 10; index++ {
		name, _ := store.AllocString(owner, region, fmt.Sprintf("padding-%02d", index))
		if err := store.SetProperty(owner, firstPrototype, name, NumberValue(float64(index))); err != nil {
			t.Fatal(err)
		}
	}
	name, _ := store.AllocString(owner, region, "answer")
	getter, _ := store.AllocNativeFunction(owner, region, NullValue(), NullValue(), 0, 99)
	if err := store.DefineProperty(owner, firstPrototype, name, AccessorProperty(RefValue(getter), UndefinedValue(), true, true)); err != nil {
		t.Fatal(err)
	}
	if err := store.SetProperty(owner, secondPrototype, name, NumberValue(22)); err != nil {
		t.Fatal(err)
	}
	if err := store.SetPrototype(owner, child, RefValue(firstPrototype)); err != nil {
		t.Fatal(err)
	}

	holder, descriptor, found, err := store.ResolvePropertyDescriptor(owner, child, name)
	if err != nil || !found || holder != firstPrototype || descriptor.Kind != PropertyAccessor || descriptor.Getter().Ref() != getter {
		t.Fatalf("inherited accessor = %s, %#v, %t, %v", holder, descriptor, found, err)
	}
	if err := store.SetProperty(owner, child, name, NumberValue(11)); err != nil {
		t.Fatal(err)
	}
	if holder, descriptor, found, err = store.ResolvePropertyDescriptor(owner, child, name); err != nil || !found || holder != child || descriptor.Value().Number() != 11 {
		t.Fatalf("own shadow = %s, %#v, %t, %v", holder, descriptor, found, err)
	}
	if deleted, err := store.DeleteProperty(owner, child, name); err != nil || !deleted {
		t.Fatalf("delete shadow = %t, %v", deleted, err)
	}
	if err := store.SetPrototype(owner, child, RefValue(secondPrototype)); err != nil {
		t.Fatal(err)
	}
	if holder, descriptor, found, err = store.ResolvePropertyDescriptor(owner, child, name); err != nil || !found || holder != secondPrototype || descriptor.Value().Number() != 22 {
		t.Fatalf("mutated prototype = %s, %#v, %t, %v", holder, descriptor, found, err)
	}
	if _, _, _, err := store.ResolvePropertyDescriptor(other, child, name); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("foreign ResolvePropertyDescriptor error = %v, want ErrAccessDenied", err)
	}
	if err := store.ReleaseOwner(owner); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := store.ResolvePropertyDescriptor(owner, child, name); !errors.Is(err, ErrStaleRef) {
		t.Fatalf("stale ResolvePropertyDescriptor error = %v, want ErrStaleRef", err)
	}
}

func TestResolvePropertyDescriptorDoesNotAllocateAcrossPrototypeMisses(t *testing.T) {
	store := NewStore(nil)
	defer store.Close()
	owner := lookupTestOwner(1023)
	region := lookupTestRegion(t, store, owner)
	root, _ := store.AllocObject(owner, region)
	middle, _ := store.AllocObject(owner, region)
	leaf, _ := store.AllocObject(owner, region)
	if err := store.SetPrototype(owner, middle, RefValue(root)); err != nil {
		t.Fatal(err)
	}
	if err := store.SetPrototype(owner, leaf, RefValue(middle)); err != nil {
		t.Fatal(err)
	}
	name, _ := store.AllocString(owner, region, "answer")
	if err := store.SetProperty(owner, root, name, NumberValue(42)); err != nil {
		t.Fatal(err)
	}
	allocations := testing.AllocsPerRun(100, func() {
		holder, descriptor, found, err := store.ResolvePropertyDescriptor(owner, leaf, name)
		if err != nil || !found || holder != root || descriptor.Value().Number() != 42 {
			panic("prototype descriptor resolution failed")
		}
	})
	if allocations != 0 {
		t.Fatalf("ResolvePropertyDescriptor allocated %.2f objects, want zero", allocations)
	}
}

func TestSemanticLookupIndexesStayLazyAndDropOnReferenceRewrite(t *testing.T) {
	t.Parallel()

	store := NewStore(nil)
	defer store.Close()
	owner := lookupTestOwner(1031)
	region := lookupTestRegion(t, store, owner)
	object, _ := store.AllocObject(owner, region)
	context, _ := store.AllocContext(owner, region, NullValue())
	for index := 0; index < semanticLookupIndexThreshold-1; index++ {
		name, _ := store.AllocString(owner, region, fmt.Sprintf("small-%02d", index))
		if err := store.SetProperty(owner, object, name, NumberValue(float64(index))); err != nil {
			t.Fatal(err)
		}
		if err := store.DeclareBinding(owner, context, name, true); err != nil {
			t.Fatal(err)
		}
	}
	store.mutex.Lock()
	_, objectSlot, _ := store.slotLocked(object)
	_, contextSlot, _ := store.slotLocked(context)
	if objectSlot.object().propertyLookup != nil || contextSlot.context().bindingLookup != nil {
		store.mutex.Unlock()
		t.Fatal("low-degree payload allocated a semantic lookup index")
	}
	store.mutex.Unlock()

	old := Ref{Region: 1, Slot: 2, Gen: 3}
	replacement := Ref{Region: 4, Slot: 5, Gen: 6}
	objectPayload := Object{ObjectHeader: ObjectHeader{
		Properties:     []Property{{Name: old}},
		propertyLookup: map[propertyKey]uint32{{kind: HeapString, text: "old"}: 0},
	}}
	objectRewrite := Slot{Kind: HeapObject, Occupied: true, payload: slotPayload{pointer: clonedPayloadPointer(objectPayload)}}
	contextPayload := Context{
		Bindings:      []Binding{{Name: old}},
		bindingLookup: map[string]uint32{"old": 0},
	}
	contextRewrite := Slot{Kind: HeapContext, Occupied: true, payload: slotPayload{pointer: clonedPayloadPointer(contextPayload)}}
	rewriteSlotReferences(&objectRewrite, map[Ref]Ref{old: replacement})
	rewriteSlotReferences(&contextRewrite, map[Ref]Ref{old: replacement})
	if objectRewrite.object().Properties[0].Name != replacement || objectRewrite.object().propertyLookup != nil {
		t.Fatalf("object rewrite retained stale lookup: %#v", objectRewrite.object())
	}
	if contextRewrite.context().Bindings[0].Name != replacement || contextRewrite.context().bindingLookup != nil {
		t.Fatalf("context rewrite retained stale lookup: %#v", contextRewrite.context())
	}
}

var lookupBenchmarkIndex int

func BenchmarkSemanticPropertyLookup(b *testing.B) {
	store := NewStore(nil)
	defer store.Close()
	owner := lookupTestOwner(2001)
	region := lookupTestRegion(b, store, owner)
	object, _ := store.AllocObject(owner, region)
	var lookup Ref
	for index := 0; index < 64; index++ {
		name, _ := store.AllocString(owner, region, fmt.Sprintf("property-%02d", index))
		_ = store.SetProperty(owner, object, name, NumberValue(float64(index)))
		if index == 63 {
			lookup = name
		}
	}
	store.mutex.Lock()
	_, slot, _ := store.slotLocked(object)
	key, _ := store.propertyKeyLocked(lookup)
	_, _ = store.findPropertyLocked(slot, key)
	store.mutex.Unlock()

	b.Run("indexed-64", func(b *testing.B) {
		b.ReportAllocs()
		for iteration := 0; iteration < b.N; iteration++ {
			index, err := store.findPropertyLocked(slot, key)
			if err != nil {
				b.Fatal(err)
			}
			lookupBenchmarkIndex = index
		}
	})
	b.Run("linear-baseline-64", func(b *testing.B) {
		b.ReportAllocs()
		for iteration := 0; iteration < b.N; iteration++ {
			index := -1
			for candidate, property := range slot.object().Properties {
				candidateKey, err := store.propertyKeyLocked(property.Name)
				if err != nil {
					b.Fatal(err)
				}
				if candidateKey == key {
					index = candidate
					break
				}
			}
			lookupBenchmarkIndex = index
		}
	})
}

func BenchmarkSemanticBindingLookup(b *testing.B) {
	store := NewStore(nil)
	defer store.Close()
	owner := lookupTestOwner(2002)
	region := lookupTestRegion(b, store, owner)
	context, _ := store.AllocContext(owner, region, NullValue())
	for index := 0; index < 64; index++ {
		name, _ := store.AllocString(owner, region, fmt.Sprintf("binding-%02d", index))
		_ = store.DeclareBinding(owner, context, name, true)
	}
	store.mutex.Lock()
	_, slot, _ := store.slotLocked(context)
	_, _ = store.findBindingLocked(slot, "binding-63")
	store.mutex.Unlock()

	b.Run("indexed-64", func(b *testing.B) {
		b.ReportAllocs()
		for iteration := 0; iteration < b.N; iteration++ {
			index, err := store.findBindingLocked(slot, "binding-63")
			if err != nil {
				b.Fatal(err)
			}
			lookupBenchmarkIndex = index
		}
	})
	b.Run("linear-baseline-64", func(b *testing.B) {
		b.ReportAllocs()
		for iteration := 0; iteration < b.N; iteration++ {
			index := -1
			for candidate, binding := range slot.context().Bindings {
				_, nameSlot, err := store.slotLocked(binding.Name)
				if err != nil {
					b.Fatal(err)
				}
				if nameSlot.stringObject().Text == "binding-63" {
					index = candidate
					break
				}
			}
			lookupBenchmarkIndex = index
		}
	})
}
