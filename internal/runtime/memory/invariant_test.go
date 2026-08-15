package memory_test

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

func TestCheckInvariantsAcrossOwnershipStateMachine(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	source := realmOwner(201)
	queue := ownership.OwnerID{Kind: ownership.OwnerQueue, Value: 201}
	destination := realmOwner(202)
	if err := store.RegisterOwner(queue); err != nil {
		t.Fatal(err)
	}
	if err := store.RegisterOwner(destination); err != nil {
		t.Fatal(err)
	}
	assertStoreInvariants(t, store, "registered owners")

	firstRegion := mustRegion(t, store, source)
	secondRegion := mustRegion(t, store, source)
	a := mustAlloc(t, store, source, firstRegion)
	b := mustAlloc(t, store, source, secondRegion)
	c := mustAlloc(t, store, source, secondRegion)
	assertStoreInvariants(t, store, "allocation")

	if err := store.Set(source, a, 0, memory.RefValue(b)); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(source, a, 1, memory.RefValue(b)); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(source, b, 0, memory.RefValue(c)); err != nil {
		t.Fatal(err)
	}
	assertStoreInvariants(t, store, "counted links")

	copied, err := store.Copy(source, queue, a)
	if err != nil {
		t.Fatal(err)
	}
	assertStoreInvariants(t, store, "queue-owned copy")
	if err := store.Accept(queue, destination, copied...); err != nil {
		t.Fatal(err)
	}
	assertStoreInvariants(t, store, "accepted copy")

	promoted, err := store.Promote(source, b)
	if err != nil {
		t.Fatal(err)
	}
	assertStoreInvariants(t, store, "promotion")
	if err := store.Transfer(source, queue, a); err != nil {
		t.Fatal(err)
	}
	assertStoreInvariants(t, store, "transfer")
	if err := store.Accept(queue, destination, a); err != nil {
		t.Fatal(err)
	}
	assertStoreInvariants(t, store, "transfer acceptance")

	if err := store.ReleaseOwner(source); err != nil {
		t.Fatal(err)
	}
	if err := store.ReleaseOwner(queue); err != nil {
		t.Fatal(err)
	}
	if err := store.ReleaseOwner(destination); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Deref(realmOwner(999), promoted[0]); err != nil {
		t.Fatalf("promoted graph did not survive owner releases: %v", err)
	}
	assertStoreInvariants(t, store, "owner releases")
}

func FuzzStoreCheckInvariants(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0, 0, 1, 17, 2, 33, 3, 49, 4, 65})
	f.Add([]byte{0, 0, 0, 2, 2, 3, 6, 7, 5, 1, 4})

	f.Fuzz(func(t *testing.T, operations []byte) {
		if len(operations) > 128 {
			operations = operations[:128]
		}
		store := memory.NewStore(nil)
		owner := realmOwner(301)
		regions := []memory.RegionID{mustRegion(t, store, owner), mustRegion(t, store, owner)}
		refs := make([]memory.Ref, 0)

		for step, operation := range operations {
			switch operation % 8 {
			case 0:
				if store.Stats().LiveCells < 64 {
					region := regions[int(operation>>3)%len(regions)]
					if ref, err := store.Alloc(owner, region); err == nil {
						refs = append(refs, ref)
					}
				}
			case 1:
				if len(refs) != 0 {
					source := refs[int(operation>>3)%len(refs)]
					_ = store.Set(owner, source, int(operation>>5)%3, memory.NumberValue(float64(operation)))
				}
			case 2:
				if len(refs) > 1 {
					source := refs[int(operation>>3)%len(refs)]
					target := refs[int(operation>>5)%len(refs)]
					_ = store.Set(owner, source, int(operation>>6)%2, memory.RefValue(target))
				}
			case 3:
				if len(refs) != 0 {
					source := refs[int(operation>>3)%len(refs)]
					_ = store.Set(owner, source, int(operation>>5)%3, memory.UndefinedValue())
				}
			case 4:
				if len(refs) != 0 {
					index := int(operation>>3) % len(refs)
					ref := refs[index]
					if err := store.Free(owner, ref); err == nil {
						refs = append(refs[:index], refs[index+1:]...)
					}
				}
			case 5:
				if len(regions) < 4 {
					if region, err := store.NewRegion(owner); err == nil {
						regions = append(regions, region)
					}
				}
			case 6:
				if len(refs) != 0 && store.Stats().LiveCells < 48 {
					root := refs[int(operation>>3)%len(refs)]
					if copied, err := store.Copy(owner, owner, root); err == nil {
						refs = append(refs, copied...)
					}
				}
			case 7:
				if len(refs) != 0 && store.Stats().LiveCells < 48 {
					root := refs[int(operation>>3)%len(refs)]
					_, _ = store.Promote(owner, root)
				}
			}
			if err := store.CheckInvariants(); err != nil {
				t.Fatalf("step %d operation %d: %v", step, operation, err)
			}
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		if err := store.CheckInvariants(); err != nil {
			t.Fatalf("after Close: %v", err)
		}
	})
}

func assertStoreInvariants(t *testing.T, store *memory.Store, stage string) {
	t.Helper()
	if err := store.CheckInvariants(); err != nil {
		t.Fatalf("%s: %v", stage, err)
	}
}
