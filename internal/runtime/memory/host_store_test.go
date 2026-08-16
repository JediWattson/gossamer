package memory_test

import (
	"errors"
	"testing"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func TestHostObjectStoresOpaqueImmutableIdentity(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(980)
	region := mustRegion(t, store, owner)
	before := store.Stats()
	for _, invalid := range []memory.HostObject{
		{},
		{Class: 1, Scope: 1},
		{Class: 1, Identity: 1},
		{Scope: 1, Identity: 1},
	} {
		if _, err := store.AllocHostObject(owner, region, invalid); !errors.Is(err, memory.ErrInvalidHostObject) {
			t.Fatalf("AllocHostObject(%#v) error = %v", invalid, err)
		}
	}
	if after := store.Stats(); after != before {
		t.Fatalf("invalid host allocations changed stats: before=%#v after=%#v", before, after)
	}

	want := memory.HostObject{Class: 9, Scope: 27, Identity: 81}
	ref, err := store.AllocHostObject(owner, region, want)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := store.DerefHostObject(owner, ref); err != nil || got != want {
		t.Fatalf("DerefHostObject() = %#v, %v", got, err)
	}
	if object, err := store.ObjectID(owner, ref); err != nil || object == 0 {
		t.Fatalf("ObjectID() = %d, %v", object, err)
	}
	if _, err := store.DerefCell(owner, ref); !errors.Is(err, memory.ErrTypeMismatch) {
		t.Fatalf("DerefCell(HostObject) error = %v", err)
	}
	if stats := store.Stats(); stats.LiveHostObjects != 1 || stats.LiveSlots != 1 {
		t.Fatalf("host stats = %#v", stats)
	}
	assertStoreInvariants(t, store, "host identity record")
}
