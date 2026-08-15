package memory_test

import (
	"errors"
	"testing"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func TestNativeArrayBufferOwnsBytesAndChecksMutation(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(90)
	region := mustRegion(t, store, owner)
	input := []byte{1, 2, 3, 4}
	buffer, err := store.AllocArrayBuffer(owner, region, input)
	if err != nil {
		t.Fatal(err)
	}
	input[0] = 99
	if got, err := store.ReadArrayBuffer(owner, buffer, 0, 4); err != nil || got[0] != 1 {
		t.Fatalf("ReadArrayBuffer() = %v, %v", got, err)
	}
	if err := store.WriteArrayBuffer(owner, buffer, 1, []byte{8, 9}); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.ReadArrayBuffer(owner, buffer, 0, 4); string(got) != string([]byte{1, 8, 9, 4}) {
		t.Fatalf("written bytes = %v", got)
	}
	if _, err := store.ReadArrayBuffer(owner, buffer, 3, 2); !errors.Is(err, memory.ErrBufferBounds) {
		t.Fatalf("out-of-bounds read = %v, want ErrBufferBounds", err)
	}
	if err := store.WriteArrayBuffer(owner, buffer, 4, []byte{1}); !errors.Is(err, memory.ErrBufferBounds) {
		t.Fatalf("out-of-bounds write = %v, want ErrBufferBounds", err)
	}
	if err := store.DetachArrayBuffer(owner, buffer); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadArrayBuffer(owner, buffer, 0, 0); !errors.Is(err, memory.ErrDetachedBuffer) {
		t.Fatalf("detached read = %v, want ErrDetachedBuffer", err)
	}
	if err := store.DetachArrayBuffer(owner, buffer); err != nil {
		t.Fatalf("second detach = %v", err)
	}
	stats := store.Stats()
	if stats.LiveArrayBuffers != 1 || stats.LiveBytes != 0 {
		t.Fatalf("Stats() after detach = %#v", stats)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func TestNativeArrayBufferPromotionClonesMutableBytes(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(91)
	reader := realmOwner(92)
	region := mustRegion(t, store, owner)
	buffer, err := store.AllocArrayBuffer(owner, region, []byte{10, 20, 30})
	if err != nil {
		t.Fatal(err)
	}
	promoted, err := store.Promote(owner, buffer)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WriteArrayBuffer(owner, buffer, 0, []byte{99}); err != nil {
		t.Fatal(err)
	}
	if got, err := store.ReadArrayBuffer(reader, promoted[0], 0, 3); err != nil || got[0] != 10 {
		t.Fatalf("promoted bytes = %v, %v", got, err)
	}
	if err := store.WriteArrayBuffer(reader, promoted[0], 0, []byte{1}); !errors.Is(err, memory.ErrImmutableRegion) {
		t.Fatalf("published buffer write = %v, want ErrImmutableRegion", err)
	}
	if err := store.ReleaseOwner(owner); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DerefArrayBuffer(owner, buffer); !errors.Is(err, memory.ErrStaleRef) {
		t.Fatalf("source buffer after release = %v, want ErrStaleRef", err)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}
