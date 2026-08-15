package memory_test

import (
	"errors"
	"testing"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func TestNativeRegExpValidatesFlagsPatternAndLastIndex(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(140)
	expressionRegion := mustRegion(t, store, owner)
	patternRegion := mustRegion(t, store, owner)
	pattern, _ := store.AllocString(owner, patternRegion, "a+")
	expression, err := store.AllocRegExp(owner, expressionRegion, pattern, "yigd")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.DerefRegExp(owner, expression)
	if err != nil || snapshot.Flags.String() != "dgiy" || snapshot.LastIndex != 0 {
		t.Fatalf("DerefRegExp() = %#v, %v", snapshot, err)
	}
	if err := store.SetRegExpLastIndex(owner, expression, 17); err != nil {
		t.Fatal(err)
	}
	if snapshot, _ := store.DerefRegExp(owner, expression); snapshot.LastIndex != 17 {
		t.Fatalf("lastIndex = %d, want 17", snapshot.LastIndex)
	}
	if got := store.EdgeCount(expressionRegion, patternRegion); got != 1 {
		t.Fatalf("pattern edge count = %d, want 1", got)
	}
	for _, flags := range []string{"gg", "x", "uv"} {
		if _, err := store.AllocRegExp(owner, expressionRegion, pattern, flags); !errors.Is(err, memory.ErrInvalidRegExp) {
			t.Fatalf("flags %q error = %v, want ErrInvalidRegExp", flags, err)
		}
	}
	object, _ := store.AllocObject(owner, patternRegion)
	before := store.Stats()
	if _, err := store.AllocRegExp(owner, expressionRegion, object, ""); !errors.Is(err, memory.ErrTypeMismatch) {
		t.Fatalf("non-String pattern error = %v, want ErrTypeMismatch", err)
	}
	if after := store.Stats(); after.LiveSlots != before.LiveSlots || after.LiveRegExps != before.LiveRegExps {
		t.Fatalf("failed RegExp allocation leaked: before=%#v after=%#v", before, after)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func TestNativeRegExpPromotionPreservesDescriptor(t *testing.T) {
	t.Parallel()

	store := memory.NewStore(nil)
	defer store.Close()
	owner := realmOwner(141)
	reader := realmOwner(142)
	region := mustRegion(t, store, owner)
	pattern, _ := store.AllocString(owner, region, "(?<word>\\w+)")
	expression, _ := store.AllocRegExp(owner, region, pattern, "dgu")
	if err := store.SetRegExpLastIndex(owner, expression, 9); err != nil {
		t.Fatal(err)
	}
	promoted, err := store.Promote(owner, expression)
	if err != nil {
		t.Fatal(err)
	}
	copyExpression, err := store.DerefRegExp(reader, promoted[0])
	if err != nil || copyExpression.Pattern == pattern || copyExpression.Flags.String() != "dgu" || copyExpression.LastIndex != 9 {
		t.Fatalf("promoted RegExp = %#v, %v", copyExpression, err)
	}
	if got, err := store.DerefString(reader, copyExpression.Pattern); err != nil || got != "(?<word>\\w+)" {
		t.Fatalf("promoted pattern = %q, %v", got, err)
	}
	if err := store.SetRegExpLastIndex(reader, promoted[0], 0); !errors.Is(err, memory.ErrImmutableRegion) {
		t.Fatalf("published lastIndex mutation = %v, want ErrImmutableRegion", err)
	}
	if err := store.ReleaseOwner(owner); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DerefRegExp(owner, expression); !errors.Is(err, memory.ErrStaleRef) {
		t.Fatalf("source RegExp after release = %v, want ErrStaleRef", err)
	}
	if err := store.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}
