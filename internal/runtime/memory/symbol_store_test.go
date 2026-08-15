package memory_test

import (
	"errors"
	"testing"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func TestNativeSymbolsHaveUniqueIdentityAndTypedDescriptions(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(80)
	symbolRegion := mustRegion(t, store, owner)
	descriptionRegion := mustRegion(t, store, owner)
	description, _ := store.AllocString(owner, descriptionRegion, "token")
	first, err := store.AllocSymbol(owner, symbolRegion, memory.RefValue(description))
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.AllocSymbol(owner, symbolRegion, memory.RefValue(description))
	if err != nil {
		t.Fatal(err)
	}
	same, err := store.SameSymbol(owner, first, second)
	if err != nil || same {
		t.Fatalf("SameSymbol(fresh, fresh) = %t, %v", same, err)
	}
	if got := store.EdgeCount(symbolRegion, descriptionRegion); got != 2 {
		t.Fatalf("description edge count = %d, want 2", got)
	}
	object, _ := store.AllocObject(owner, descriptionRegion)
	before := store.Stats()
	if _, err := store.AllocSymbol(owner, symbolRegion, memory.RefValue(object)); !errors.Is(err, memory.ErrTypeMismatch) {
		t.Fatalf("non-String description error = %v, want ErrTypeMismatch", err)
	}
	if after := store.Stats(); after.LiveSlots != before.LiveSlots || after.LiveSymbols != before.LiveSymbols {
		t.Fatalf("failed Symbol allocation leaked: before=%#v after=%#v", before, after)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func TestNativeSymbolPromotionPreservesSemanticIdentity(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(81)
	reader := realmOwner(82)
	region := mustRegion(t, store, owner)
	description, _ := store.AllocString(owner, region, "shared identity")
	symbol, err := store.AllocSymbol(owner, region, memory.RefValue(description))
	if err != nil {
		t.Fatal(err)
	}
	promoted, err := store.Promote(owner, symbol)
	if err != nil {
		t.Fatal(err)
	}
	if promoted[0] == symbol {
		t.Fatal("promotion reused source Symbol Ref")
	}
	if same, err := store.SameSymbol(owner, symbol, promoted[0]); err != nil || !same {
		t.Fatalf("SameSymbol(source, promoted) = %t, %v", same, err)
	}
	sourceSnapshot, _ := store.DerefSymbol(owner, symbol)
	copySnapshot, err := store.DerefSymbol(reader, promoted[0])
	if err != nil || copySnapshot.ID != sourceSnapshot.ID || copySnapshot.Description.Ref() == description {
		t.Fatalf("promoted Symbol = %#v, %v; source=%#v", copySnapshot, err, sourceSnapshot)
	}
	if err := store.ReleaseOwner(owner); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DerefSymbol(owner, symbol); !errors.Is(err, memory.ErrStaleRef) {
		t.Fatalf("source Symbol after release = %v, want ErrStaleRef", err)
	}
	if got, err := store.DerefString(reader, copySnapshot.Description.Ref()); err != nil || got != "shared identity" {
		t.Fatalf("promoted description = %q, %v", got, err)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}
