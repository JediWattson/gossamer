package memory_test

import (
	"errors"
	"testing"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func TestStructuredCloneTransfersArrayBufferAndPreservesAliases(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	source := realmOwner(130)
	destination := realmOwner(131)
	region := mustRegion(t, store, source)
	root := mustAlloc(t, store, source, region)
	buffer, err := store.AllocArrayBuffer(source, region, []byte{1, 2, 3, 4})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set(source, root, 0, memory.RefValue(buffer)); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(source, root, 1, memory.RefValue(buffer)); err != nil {
		t.Fatal(err)
	}

	roots, transfers, err := store.StructuredClone(source, destination, []memory.Ref{root}, []memory.Ref{buffer})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadArrayBuffer(source, buffer, 0, 0); !errors.Is(err, memory.ErrDetachedBuffer) {
		t.Fatalf("source buffer after transfer = %v, want ErrDetachedBuffer", err)
	}
	cloned, err := store.Deref(destination, roots[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(cloned.Fields) != 2 || !cloned.Fields[0].IsRef() || !cloned.Fields[1].IsRef() || cloned.Fields[0].Ref() != cloned.Fields[1].Ref() {
		t.Fatalf("cloned aliases = %#v", cloned.Fields)
	}
	if len(transfers) != 1 || transfers[0] != cloned.Fields[0].Ref() {
		t.Fatalf("cloned transfers = %#v, field = %s", transfers, cloned.Fields[0].Ref())
	}
	if got, err := store.ReadArrayBuffer(destination, transfers[0], 0, 4); err != nil || string(got) != string([]byte{1, 2, 3, 4}) {
		t.Fatalf("transferred bytes = %v, %v", got, err)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func TestStructuredCloneValidatesTransferListBeforeDetaching(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	source := realmOwner(132)
	destination := realmOwner(133)
	region := mustRegion(t, store, source)
	buffer, err := store.AllocArrayBuffer(source, region, []byte{5, 6})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.StructuredClone(source, destination, []memory.Ref{buffer}, []memory.Ref{buffer, buffer}); !errors.Is(err, memory.ErrDuplicateTransfer) {
		t.Fatalf("duplicate transfer error = %v, want ErrDuplicateTransfer", err)
	}
	if got, err := store.ReadArrayBuffer(source, buffer, 0, 2); err != nil || string(got) != string([]byte{5, 6}) {
		t.Fatalf("source bytes after rejected clone = %v, %v", got, err)
	}
}
