package memory_test

import (
	"errors"
	"testing"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func TestNativeBigIntCanonicalizesAndOwnsMagnitude(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(70)
	region := mustRegion(t, store, owner)
	magnitude := []byte{0, 0, 0x12, 0x34}
	ref, err := store.AllocBigInt(owner, region, true, magnitude)
	if err != nil {
		t.Fatal(err)
	}
	magnitude[2] = 0
	value, err := store.DerefBigInt(owner, ref)
	if err != nil || !value.Negative || len(value.Magnitude) != 2 || value.Magnitude[0] != 0x12 {
		t.Fatalf("DerefBigInt() = %#v, %v", value, err)
	}
	value.Magnitude[0] = 0
	if reread, _ := store.DerefBigInt(owner, ref); reread.Magnitude[0] != 0x12 {
		t.Fatal("DerefBigInt exposed mutable magnitude storage")
	}
	zero, err := store.AllocBigInt(owner, region, true, []byte{0, 0})
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := store.DerefBigInt(owner, zero); value.Negative || len(value.Magnitude) != 0 {
		t.Fatalf("canonical zero = %#v", value)
	}
	if got, err := store.BigIntText(owner, ref, 16); err != nil || got != "-1234" {
		t.Fatalf("BigIntText() = %q, %v", got, err)
	}
	if _, err := store.ParseBigInt(owner, region, "not-an-integer", 10); !errors.Is(err, memory.ErrInvalidBigInt) {
		t.Fatalf("ParseBigInt(invalid) = %v, want ErrInvalidBigInt", err)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func TestNativeBigIntPromotionClonesBytes(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(71)
	reader := realmOwner(72)
	region := mustRegion(t, store, owner)
	ref, err := store.ParseBigInt(owner, region, "123456789012345678901234567890", 10)
	if err != nil {
		t.Fatal(err)
	}
	promoted, err := store.Promote(owner, ref)
	if err != nil {
		t.Fatal(err)
	}
	if promoted[0] == ref {
		t.Fatal("promotion reused source BigInt Ref")
	}
	if got, err := store.BigIntText(reader, promoted[0], 10); err != nil || got != "123456789012345678901234567890" {
		t.Fatalf("promoted BigInt = %q, %v", got, err)
	}
	if err := store.ReleaseOwner(owner); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DerefBigInt(owner, ref); !errors.Is(err, memory.ErrStaleRef) {
		t.Fatalf("source BigInt after release = %v, want ErrStaleRef", err)
	}
	stats := store.Stats()
	if stats.LiveBigInts != 1 || stats.LiveBytes == 0 {
		t.Fatalf("Stats() = %#v", stats)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}
