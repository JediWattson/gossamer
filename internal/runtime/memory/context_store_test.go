package memory_test

import (
	"errors"
	"testing"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func TestNativeContextResolvesAndMutatesLexicalBindings(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(40)
	innerRegion := mustRegion(t, store, owner)
	outerRegion := mustRegion(t, store, owner)
	outer, err := store.AllocContext(owner, outerRegion, memory.NullValue())
	if err != nil {
		t.Fatal(err)
	}
	inner, err := store.AllocContext(owner, innerRegion, memory.RefValue(outer))
	if err != nil {
		t.Fatal(err)
	}
	x, _ := store.AllocString(owner, outerRegion, "x")
	y, _ := store.AllocString(owner, outerRegion, "y")
	z, _ := store.AllocString(owner, outerRegion, "z")
	child, _ := store.AllocObject(owner, outerRegion)

	if err := store.DeclareBinding(owner, outer, x, true); err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeBinding(owner, outer, x, memory.RefValue(child)); err != nil {
		t.Fatal(err)
	}
	if err := store.DeclareBinding(owner, inner, y, false); err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeBinding(owner, inner, y, memory.NumberValue(7)); err != nil {
		t.Fatal(err)
	}
	if err := store.DeclareBinding(owner, inner, z, true); err != nil {
		t.Fatal(err)
	}
	if value, found, err := store.ResolveBinding(owner, inner, x); err != nil || !found || value.Ref() != child {
		t.Fatalf("ResolveBinding(x) = %#v, %t, %v", value, found, err)
	}
	if _, found, err := store.ResolveBinding(owner, inner, z); !found || !errors.Is(err, memory.ErrBindingUninitialized) {
		t.Fatalf("ResolveBinding(z) = found:%t err:%v", found, err)
	}
	if err := store.SetBinding(owner, inner, y, memory.NumberValue(8)); !errors.Is(err, memory.ErrImmutableBinding) {
		t.Fatalf("SetBinding(immutable) = %v, want ErrImmutableBinding", err)
	}
	if err := store.SetBinding(owner, inner, x, memory.NumberValue(42)); err != nil {
		t.Fatal(err)
	}
	if value, found, err := store.ResolveBinding(owner, outer, x); err != nil || !found || value.Number() != 42 {
		t.Fatalf("ResolveBinding(updated x) = %#v, %t, %v", value, found, err)
	}
	if err := store.SetContextParent(owner, outer, memory.RefValue(inner)); !errors.Is(err, memory.ErrContextCycle) {
		t.Fatalf("SetContextParent(cycle) = %v, want ErrContextCycle", err)
	}
	if got := store.EdgeCount(innerRegion, outerRegion); got != 3 {
		t.Fatalf("inner -> outer edge count = %d, want parent plus two names", got)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func TestNativeContextPromotionPreservesParentAndBindingState(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(41)
	reader := realmOwner(42)
	region := mustRegion(t, store, owner)
	outer, _ := store.AllocContext(owner, region, memory.NullValue())
	inner, _ := store.AllocContext(owner, region, memory.RefValue(outer))
	name, _ := store.AllocString(owner, region, "captured")
	value, _ := store.AllocString(owner, region, "value")
	if err := store.DeclareBinding(owner, outer, name, true); err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeBinding(owner, outer, name, memory.RefValue(value)); err != nil {
		t.Fatal(err)

	}
	promoted, err := store.Promote(owner, inner)
	if err != nil {
		t.Fatal(err)
	}
	copyInner, err := store.DerefContext(reader, promoted[0])
	if err != nil || !copyInner.Parent.IsRef() || copyInner.Parent.Ref() == outer {
		t.Fatalf("promoted inner = %#v, %v", copyInner, err)
	}
	copyOuter, err := store.DerefContext(reader, copyInner.Parent.Ref())
	if err != nil || len(copyOuter.Bindings) != 1 || !copyOuter.Bindings[0].Initialized || !copyOuter.Bindings[0].Mutable {
		t.Fatalf("promoted outer = %#v, %v", copyOuter, err)
	}
	copyName := copyOuter.Bindings[0].Name
	resolved, found, err := store.ResolveBinding(reader, promoted[0], copyName)
	if err != nil || !found {
		t.Fatalf("promoted ResolveBinding() = %#v, %t, %v", resolved, found, err)
	}
	if got, err := store.DerefString(reader, resolved.Ref()); err != nil || got != "value" {
		t.Fatalf("promoted binding value = %q, %v", got, err)
	}
	if err := store.ReleaseOwner(owner); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DerefContext(owner, inner); !errors.Is(err, memory.ErrStaleRef) {
		t.Fatalf("source Context after release = %v, want ErrStaleRef", err)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func TestIndirectContextBindingIsLiveImmutableAndCopied(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(43)
	reader := realmOwner(44)
	exportRegion := mustRegion(t, store, owner)
	importRegion := mustRegion(t, store, owner)
	exporter, _ := store.AllocContext(owner, exportRegion, memory.NullValue())
	importer, _ := store.AllocContext(owner, importRegion, memory.NullValue())
	exportedName, _ := store.AllocString(owner, exportRegion, "counter")
	localName, _ := store.AllocString(owner, importRegion, "value")
	if err := store.DeclareBinding(owner, exporter, exportedName, true); err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeBinding(owner, exporter, exportedName, memory.NumberValue(1)); err != nil {
		t.Fatal(err)
	}
	if err := store.DeclareIndirectBinding(owner, importer, localName, exporter, exportedName); err != nil {
		t.Fatal(err)
	}
	if value, found, err := store.ResolveBinding(owner, importer, localName); err != nil || !found || value.Number() != 1 {
		t.Fatalf("initial indirect value = %#v, %t, %v", value, found, err)
	}
	if err := store.SetBinding(owner, exporter, exportedName, memory.NumberValue(2)); err != nil {
		t.Fatal(err)
	}
	if value, found, err := store.ResolveBinding(owner, importer, localName); err != nil || !found || value.Number() != 2 {
		t.Fatalf("updated indirect value = %#v, %t, %v", value, found, err)
	}
	if err := store.SetBinding(owner, importer, localName, memory.NumberValue(3)); !errors.Is(err, memory.ErrImmutableBinding) {
		t.Fatalf("SetBinding(indirect) = %v, want ErrImmutableBinding", err)
	}
	if got := store.EdgeCount(importRegion, exportRegion); got != 2 {
		t.Fatalf("import -> export edge count = %d, want target Context and name", got)
	}

	copied, err := store.Copy(owner, reader, importer)
	if err != nil {
		t.Fatal(err)
	}
	copyContext, err := store.DerefContext(reader, copied[0])
	if err != nil || len(copyContext.Bindings) != 1 || !copyContext.Bindings[0].Indirect {
		t.Fatalf("copied importer = %#v, %v", copyContext, err)
	}
	copyName := copyContext.Bindings[0].Name
	if value, found, err := store.ResolveBinding(reader, copied[0], copyName); err != nil || !found || value.Number() != 2 {
		t.Fatalf("copied indirect value = %#v, %t, %v", value, found, err)
	}
	if err := store.ReleaseOwner(owner); err != nil {
		t.Fatal(err)
	}
	if value, found, err := store.ResolveBinding(reader, copied[0], copyName); err != nil || !found || value.Number() != 2 {
		t.Fatalf("copied indirect value after source release = %#v, %t, %v", value, found, err)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func TestIndirectContextBindingRejectsAliasCycles(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(45)
	region := mustRegion(t, store, owner)
	left, _ := store.AllocContext(owner, region, memory.NullValue())
	right, _ := store.AllocContext(owner, region, memory.NullValue())
	leftName, _ := store.AllocString(owner, region, "left")
	rightName, _ := store.AllocString(owner, region, "right")
	if err := store.DeclareIndirectBinding(owner, left, leftName, right, rightName); err != nil {
		t.Fatal(err)
	}
	if err := store.DeclareIndirectBinding(owner, right, rightName, left, leftName); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.ResolveBinding(owner, left, leftName); !found || !errors.Is(err, memory.ErrBindingCycle) {
		t.Fatalf("ResolveBinding(alias cycle) = found:%t err:%v", found, err)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}
