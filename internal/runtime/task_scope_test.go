package runtime_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func TestTaskContextWeaklyInternsOnlyBoundedShortStrings(t *testing.T) {
	t.Parallel()

	realm, err := browserruntime.NewRealm(1001, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer realm.Close()
	_, err = realm.EnqueueTask(func(task *browserruntime.TaskContext) error {
		first, err := task.NewString("repeated-property")
		if err != nil {
			return err
		}
		second, err := task.NewString("repeated-property")
		if err != nil {
			return err
		}
		if second != first {
			t.Fatalf("short string refs = %s and %s, want one canonical task ref", first, second)
		}
		if err := task.Realm.Store().Free(task.Owner, first); err != nil {
			return err
		}
		reused, err := task.NewString("repeated-property")
		if err != nil {
			return err
		}
		if reused == first || reused.Slot != first.Slot || reused.Gen == first.Gen {
			t.Fatalf("stale intern replacement = %s after %s", reused, first)
		}
		otherRegion, err := task.Realm.Store().NewRegion(task.Owner)
		if err != nil {
			return err
		}
		otherScope, err := task.WithMemoryRegion(otherRegion, nil)
		if err != nil {
			return err
		}
		other, err := otherScope.NewString("repeated-property")
		if err != nil {
			return err
		}
		if other.Region != otherRegion || other == reused {
			t.Fatalf("region-local intern ref = %s, want a new ref in R%d", other, otherRegion)
		}
		if err := task.Realm.Store().DestroyRegion(task.Owner, otherRegion); err != nil {
			return err
		}
		long := strings.Repeat("x", 65)
		longFirst, err := task.NewString(long)
		if err != nil {
			return err
		}
		longSecond, err := task.NewString(long)
		if err != nil {
			return err
		}
		if longFirst == longSecond {
			t.Fatalf("long application string unexpectedly interned as %s", longFirst)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestTaskContextWithMemoryRegionRequiresCurrentTaskOwnership(t *testing.T) {
	t.Parallel()

	realm, err := browserruntime.NewRealm(1002, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer realm.Close()
	var copied memory.Ref
	_, err = realm.EnqueueTask(func(task *browserruntime.TaskContext) error {
		local, err := task.NewString("realm state")
		if err != nil {
			return err
		}
		refs, err := task.Realm.Store().Copy(task.Owner, task.Owner, local)
		if err != nil {
			return err
		}
		copied = refs[0]
		scope, err := task.WithMemoryRegion(copied.Region, nil)
		if err != nil {
			return err
		}
		allocated, err := scope.NewString("same region")
		if err != nil {
			return err
		}
		if allocated.Region != copied.Region {
			t.Fatalf("allocated region = R%d, want R%d", allocated.Region, copied.Region)
		}

		realmRegion, err := task.Realm.Store().NewRegion(task.Realm.Owner())
		if err != nil {
			return err
		}
		if _, err := task.WithMemoryRegion(realmRegion, nil); !errors.Is(err, memory.ErrAccessDenied) {
			t.Fatalf("realm-owned scope error = %v", err)
		}
		return task.Realm.Store().DestroyRegion(task.Realm.Owner(), realmRegion)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := realm.Store().DerefString(realm.Owner(), copied); !errors.Is(err, memory.ErrStaleRef) {
		t.Fatalf("released copied ref error = %v, want stale Ref", err)
	}
}

func TestTaskContextCanBorrowRealmOwnedMemoryWithoutTakingOwnership(t *testing.T) {
	t.Parallel()

	realm, err := browserruntime.NewRealm(1003, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer realm.Close()
	realmRegion, err := realm.Store().NewRegion(realm.Owner())
	if err != nil {
		t.Fatal(err)
	}
	var borrowed memory.Ref
	_, err = realm.EnqueueTask(func(task *browserruntime.TaskContext) error {
		scope, err := task.WithBorrowedRealmMemoryRegion(realmRegion, nil)
		if err != nil {
			return err
		}
		borrowed, err = scope.NewString("persistent realm state")
		if err != nil {
			return err
		}
		if scope.Owner != realm.Owner() {
			t.Fatalf("borrowed owner = %s, want %s", scope.Owner, realm.Owner())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, err := realm.Store().DerefString(realm.Owner(), borrowed); err != nil || got != "persistent realm state" {
		t.Fatalf("borrowed string after task release = %q, %v", got, err)
	}
	if err := realm.Store().DestroyRegion(realm.Owner(), realmRegion); err != nil {
		t.Fatal(err)
	}
}
