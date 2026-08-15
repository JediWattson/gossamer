package memory_test

import (
	"errors"
	"testing"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func TestNativeTypedArraysShareCheckedBufferViews(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(100)
	viewRegion := mustRegion(t, store, owner)
	bufferRegion := mustRegion(t, store, owner)
	buffer, _ := store.AllocArrayBuffer(owner, bufferRegion, make([]byte, 8))
	unsigned, err := store.AllocTypedArray(owner, viewRegion, buffer, memory.ElementUint16, 0, 4)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := store.AllocTypedArray(owner, viewRegion, buffer, memory.ElementInt16, 0, 4)
	if err != nil {
		t.Fatal(err)
	}
	clamped, err := store.AllocTypedArray(owner, viewRegion, buffer, memory.ElementUint8Clamped, 0, 8)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WriteTypedArrayElement(owner, unsigned, 1, -1); err != nil {
		t.Fatal(err)
	}
	if got, err := store.ReadTypedArrayElement(owner, unsigned, 1); err != nil || got != 65535 {
		t.Fatalf("Uint16 read = %v, %v", got, err)
	}
	if got, err := store.ReadTypedArrayElement(owner, signed, 1); err != nil || got != -1 {
		t.Fatalf("shared Int16 read = %v, %v", got, err)
	}
	if err := store.WriteTypedArrayElement(owner, clamped, 0, 254.5); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.ReadTypedArrayElement(owner, clamped, 0); got != 254 {
		t.Fatalf("Uint8Clamped tie = %v, want 254", got)
	}
	before := store.Stats()
	if _, err := store.AllocTypedArray(owner, viewRegion, buffer, memory.ElementUint32, 2, 1); !errors.Is(err, memory.ErrInvalidTypedArray) {
		t.Fatalf("unaligned view = %v, want ErrInvalidTypedArray", err)
	}
	if after := store.Stats(); after.LiveSlots != before.LiveSlots || after.LiveTypedArrays != before.LiveTypedArrays {
		t.Fatalf("failed view allocation leaked: before=%#v after=%#v", before, after)
	}
	if got := store.EdgeCount(viewRegion, bufferRegion); got != 3 {
		t.Fatalf("view -> buffer edge count = %d, want 3", got)
	}
	if err := store.DetachArrayBuffer(owner, buffer); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadTypedArrayElement(owner, signed, 0); !errors.Is(err, memory.ErrDetachedBuffer) {
		t.Fatalf("view of detached buffer = %v, want ErrDetachedBuffer", err)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func TestNativeTypedArrayPromotionClonesViewAndBuffer(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(101)
	reader := realmOwner(102)
	region := mustRegion(t, store, owner)
	buffer, _ := store.AllocArrayBuffer(owner, region, make([]byte, 16))
	view, err := store.AllocTypedArray(owner, region, buffer, memory.ElementFloat64, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WriteTypedArrayElement(owner, view, 0, 3.5); err != nil {
		t.Fatal(err)
	}
	promoted, err := store.Promote(owner, view)
	if err != nil {
		t.Fatal(err)
	}
	copyView, err := store.DerefTypedArray(reader, promoted[0])
	if err != nil || copyView.Buffer == buffer {
		t.Fatalf("promoted view = %#v, %v", copyView, err)
	}
	if err := store.WriteTypedArrayElement(owner, view, 0, 9.25); err != nil {
		t.Fatal(err)
	}
	if got, err := store.ReadTypedArrayElement(reader, promoted[0], 0); err != nil || got != 3.5 {
		t.Fatalf("promoted element = %v, %v", got, err)
	}
	if err := store.WriteTypedArrayElement(reader, promoted[0], 0, 1); !errors.Is(err, memory.ErrImmutableRegion) {
		t.Fatalf("published view write = %v, want ErrImmutableRegion", err)
	}
	if err := store.ReleaseOwner(owner); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DerefTypedArray(owner, view); !errors.Is(err, memory.ErrStaleRef) {
		t.Fatalf("source view after release = %v, want ErrStaleRef", err)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}
