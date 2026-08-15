package memory_test

import (
	"errors"
	"math"
	"testing"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func TestNativeDateAppliesTimeClipAndMutationRules(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(130)
	region := mustRegion(t, store, owner)
	ref, err := store.AllocDate(owner, region, 1234.9)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := store.DerefDate(owner, ref); err != nil || got.Milliseconds != 1234 {
		t.Fatalf("DerefDate() = %#v, %v", got, err)
	}
	if err := store.SetDateTime(owner, ref, -1.9); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.DerefDate(owner, ref); got.Milliseconds != -1 {
		t.Fatalf("clipped negative time = %v", got.Milliseconds)
	}
	if err := store.SetDateTime(owner, ref, 8.64e15+1); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.DerefDate(owner, ref); !math.IsNaN(got.Milliseconds) {
		t.Fatalf("out-of-range Date = %v, want NaN", got.Milliseconds)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func TestNativeDatePromotionCopiesValueAndBecomesImmutable(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(131)
	reader := realmOwner(132)
	region := mustRegion(t, store, owner)
	ref, _ := store.AllocDate(owner, region, 42)
	promoted, err := store.Promote(owner, ref)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetDateTime(owner, ref, 99); err != nil {
		t.Fatal(err)
	}
	if got, err := store.DerefDate(reader, promoted[0]); err != nil || got.Milliseconds != 42 {
		t.Fatalf("promoted Date = %#v, %v", got, err)
	}
	if err := store.SetDateTime(reader, promoted[0], 1); !errors.Is(err, memory.ErrImmutableRegion) {
		t.Fatalf("published Date mutation = %v, want ErrImmutableRegion", err)
	}
	if err := store.ReleaseOwner(owner); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DerefDate(owner, ref); !errors.Is(err, memory.ErrStaleRef) {
		t.Fatalf("source Date after release = %v, want ErrStaleRef", err)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}
