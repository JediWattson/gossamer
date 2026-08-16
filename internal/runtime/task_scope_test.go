package runtime_test

import (
	"context"
	"errors"
	"testing"

	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

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
