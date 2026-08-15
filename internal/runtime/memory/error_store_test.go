package memory_test

import (
	"errors"
	"testing"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func TestNativeErrorMaintainsDiagnosticEdges(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(150)
	errorRegion := mustRegion(t, store, owner)
	textRegion := mustRegion(t, store, owner)
	causeRegion := mustRegion(t, store, owner)
	message, _ := store.AllocString(owner, textRegion, "failed")
	stack, _ := store.AllocString(owner, textRegion, "at task")
	cause, _ := store.Alloc(owner, causeRegion)

	aggregate, err := store.AllocError(owner, errorRegion, memory.ErrorAggregate, memory.RefValue(message))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetErrorStack(owner, aggregate, memory.RefValue(stack)); err != nil {
		t.Fatal(err)
	}
	if err := store.SetErrorCause(owner, aggregate, memory.RefValue(cause)); err != nil {
		t.Fatal(err)
	}
	members := []memory.Value{memory.RefValue(cause), memory.RefValue(stack), memory.RefValue(cause)}
	if err := store.SetAggregateErrors(owner, aggregate, members); err != nil {
		t.Fatal(err)
	}
	if got := store.EdgeCount(errorRegion, textRegion); got != 3 {
		t.Fatalf("text edge count = %d, want 3", got)
	}
	if got := store.EdgeCount(errorRegion, causeRegion); got != 3 {
		t.Fatalf("cause edge count = %d, want 3", got)
	}

	snapshot, err := store.DerefError(owner, aggregate)
	if err != nil || snapshot.Kind != memory.ErrorAggregate || !snapshot.HasCause || len(snapshot.Errors) != 3 {
		t.Fatalf("DerefError() = %#v, %v", snapshot, err)
	}
	snapshot.Errors[0] = memory.NullValue()
	if current, _ := store.DerefError(owner, aggregate); !current.Errors[0].IsRef() || current.Errors[0].Ref() != cause {
		t.Fatalf("DerefError returned aliased aggregate storage: %#v", current.Errors)
	}

	if err := store.ClearErrorCause(owner, aggregate); err != nil {
		t.Fatal(err)
	}
	if got := store.EdgeCount(errorRegion, causeRegion); got != 2 {
		t.Fatalf("cause edge count after clear = %d, want 2", got)
	}
	if err := store.SetAggregateErrors(owner, aggregate, []memory.Value{memory.RefValue(message)}); err != nil {
		t.Fatal(err)
	}
	if got := store.EdgeCount(errorRegion, causeRegion); got != 0 {
		t.Fatalf("cause edge count after replacement = %d, want 0", got)
	}
	if err := store.SetErrorMessage(owner, aggregate, memory.NullValue()); err != nil {
		t.Fatal(err)
	}
	if err := store.SetErrorStack(owner, aggregate, memory.NullValue()); err != nil {
		t.Fatal(err)
	}
	if err := store.SetAggregateErrors(owner, aggregate, nil); err != nil {
		t.Fatal(err)
	}
	if got := store.EdgeCount(errorRegion, textRegion); got != 0 {
		t.Fatalf("text edge count after clears = %d, want 0", got)
	}

	ordinary, err := store.AllocError(owner, errorRegion, memory.ErrorType, memory.NullValue())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetAggregateErrors(owner, ordinary, []memory.Value{memory.NumberValue(1)}); !errors.Is(err, memory.ErrInvalidError) {
		t.Fatalf("ordinary aggregate members error = %v, want ErrInvalidError", err)
	}
	if err := store.SetErrorMessage(owner, ordinary, memory.RefValue(cause)); !errors.Is(err, memory.ErrTypeMismatch) {
		t.Fatalf("non-String message error = %v, want ErrTypeMismatch", err)
	}
	before := store.Stats()
	if _, err := store.AllocError(owner, errorRegion, memory.ErrorKind(99), memory.NullValue()); !errors.Is(err, memory.ErrInvalidError) {
		t.Fatalf("invalid Error kind = %v, want ErrInvalidError", err)
	}
	if after := store.Stats(); after.LiveSlots != before.LiveSlots || after.LiveErrors != before.LiveErrors {
		t.Fatalf("failed Error allocation leaked: before=%#v after=%#v", before, after)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func TestNativeErrorPromotionPreservesDiagnosticGraph(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(151)
	reader := realmOwner(152)
	region := mustRegion(t, store, owner)
	message, _ := store.AllocString(owner, region, "many failures")
	stack, _ := store.AllocString(owner, region, "at aggregate")
	cause, _ := store.Alloc(owner, region)
	aggregate, _ := store.AllocError(owner, region, memory.ErrorAggregate, memory.RefValue(message))
	if err := store.SetErrorStack(owner, aggregate, memory.RefValue(stack)); err != nil {
		t.Fatal(err)
	}
	if err := store.SetErrorCause(owner, aggregate, memory.RefValue(cause)); err != nil {
		t.Fatal(err)
	}
	if err := store.SetAggregateErrors(owner, aggregate, []memory.Value{memory.RefValue(cause), memory.NumberValue(7), memory.RefValue(stack), memory.RefValue(cause)}); err != nil {
		t.Fatal(err)
	}

	promoted, err := store.Promote(owner, aggregate)
	if err != nil {
		t.Fatal(err)
	}
	copyError, err := store.DerefError(reader, promoted[0])
	if err != nil {
		t.Fatal(err)
	}
	if copyError.Kind != memory.ErrorAggregate || !copyError.HasCause || len(copyError.Errors) != 4 {
		t.Fatalf("promoted Error = %#v", copyError)
	}
	copyCause := copyError.Cause.Ref()
	if copyCause == cause || copyError.Errors[0].Ref() != copyCause || copyError.Errors[3].Ref() != copyCause {
		t.Fatalf("promoted cause aliasing = cause %s, errors %#v", copyCause, copyError.Errors)
	}
	if copyError.Errors[1].Number() != 7 || copyError.Errors[2] != copyError.Stack {
		t.Fatalf("promoted member order/aliases = %#v", copyError.Errors)
	}
	if got, err := store.DerefString(reader, copyError.Message.Ref()); err != nil || got != "many failures" {
		t.Fatalf("promoted message = %q, %v", got, err)
	}
	if got, err := store.DerefString(reader, copyError.Stack.Ref()); err != nil || got != "at aggregate" {
		t.Fatalf("promoted stack = %q, %v", got, err)
	}
	if err := store.SetErrorCause(reader, promoted[0], memory.NullValue()); !errors.Is(err, memory.ErrImmutableRegion) {
		t.Fatalf("published cause mutation = %v, want ErrImmutableRegion", err)
	}
	if err := store.ReleaseOwner(owner); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DerefError(owner, aggregate); !errors.Is(err, memory.ErrStaleRef) {
		t.Fatalf("source Error after release = %v, want ErrStaleRef", err)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}
