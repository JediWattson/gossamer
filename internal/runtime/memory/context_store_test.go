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
