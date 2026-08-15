package runtime_test

import (
	"context"
	"errors"
	"testing"

	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

func TestRealmQueueRequiresExplicitTransferForPrivateRefs(t *testing.T) {
	t.Parallel()

	scheduler, err := browserruntime.NewScheduler(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer scheduler.Close()
	source, err := scheduler.NewRealm()
	if err != nil {
		t.Fatal(err)
	}
	destination, err := scheduler.NewRealm()
	if err != nil {
		t.Fatal(err)
	}

	var root memory.Ref
	var child memory.Ref
	var received bool
	_, err = source.EnqueueTask(func(task *browserruntime.TaskContext) error {
		var allocErr error
		root, allocErr = task.NewCell()
		if allocErr != nil {
			return allocErr
		}
		child, allocErr = task.NewCell()
		if allocErr != nil {
			return allocErr
		}
		if err := task.Set(root, 0, memory.RefValue(child)); err != nil {
			return err
		}
		if _, err := task.Send(destination, func(*browserruntime.TaskContext) error {
			t.Fatal("unqualified private send executed")
			return nil
		}, root); !errors.Is(err, memory.ErrExplicitSendRequired) {
			return errors.New("unqualified private send did not fail with ErrExplicitSendRequired")
		}
		_, err := task.Transfer(destination, func(task *browserruntime.TaskContext) error {
			received = true
			if len(task.Refs) != 1 || task.Refs[0] != root {
				t.Fatalf("received refs = %#v, want [%s]", task.Refs, root)
			}
			cell, err := task.Deref(task.Refs[0])
			if err != nil {
				return err
			}
			if len(cell.Fields) != 1 || !cell.Fields[0].IsRef() || cell.Fields[0].Ref() != child {
				t.Fatalf("received root = %#v", cell)
			}
			return nil
		}, root)
		if err != nil {
			return err
		}
		if _, err := task.Deref(root); !errors.Is(err, memory.ErrRegionInTransit) {
			t.Fatalf("producer access after enqueue = %v, want ErrRegionInTransit", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := source.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	queued, err := scheduler.Store().Region(root.Region)
	if err != nil {
		t.Fatal(err)
	}
	if queued.State != memory.RegionInTransit || queued.Owner != destination.Tasks.Owner() {
		t.Fatalf("queued region = %#v", queued)
	}
	if destination.Tasks.Len() != 1 {
		t.Fatalf("destination queue length = %d, want 1", destination.Tasks.Len())
	}
	if err := destination.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !received {
		t.Fatal("transferred task did not run")
	}
	if _, err := scheduler.Store().Deref(destination.Owner(), root); !errors.Is(err, memory.ErrStaleRef) {
		t.Fatalf("transferred ref after receiver task = %v, want ErrStaleRef", err)
	}
}

func TestRealmQueuePublishSharesImmutableRegion(t *testing.T) {
	t.Parallel()

	scheduler, err := browserruntime.NewScheduler(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer scheduler.Close()
	source, err := scheduler.NewRealm()
	if err != nil {
		t.Fatal(err)
	}
	destination, err := scheduler.NewRealm()
	if err != nil {
		t.Fatal(err)
	}
	var published memory.Ref
	var read bool
	if _, err := source.EnqueueTask(func(task *browserruntime.TaskContext) error {
		var allocErr error
		published, allocErr = task.NewCell()
		if allocErr != nil {
			return allocErr
		}
		if err := task.Set(published, 0, memory.StringValue("shared")); err != nil {
			return err
		}
		_, err := task.Publish(destination, func(task *browserruntime.TaskContext) error {
			cell, err := task.Deref(task.Refs[0])
			if err != nil {
				return err
			}
			read = len(cell.Fields) == 1 && cell.Fields[0].String() == "shared"
			if err := task.Set(task.Refs[0], 0, memory.StringValue("mutated")); !errors.Is(err, memory.ErrImmutableRegion) {
				t.Fatalf("published Set error = %v, want ErrImmutableRegion", err)
			}
			return nil
		}, published)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := source.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	region, err := scheduler.Store().Region(published.Region)
	if err != nil {
		t.Fatal(err)
	}
	if region.State != memory.RegionPublished || region.Owner.Kind != ownership.OwnerShared {
		t.Fatalf("published region = %#v", region)
	}
	if err := destination.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !read {
		t.Fatal("destination did not read published Cell")
	}
	if _, err := scheduler.Store().Deref(destination.Owner(), published); err != nil {
		t.Fatalf("published Cell did not survive task completion: %v", err)
	}
}

func TestRealmQueueCopyOutlivesSourceWithoutSharingMutation(t *testing.T) {
	t.Parallel()

	scheduler, err := browserruntime.NewScheduler(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer scheduler.Close()
	source, err := scheduler.NewRealm()
	if err != nil {
		t.Fatal(err)
	}
	destination, err := scheduler.NewRealm()
	if err != nil {
		t.Fatal(err)
	}
	var original memory.Ref
	var copied memory.Ref
	if _, err := source.EnqueueTask(func(task *browserruntime.TaskContext) error {
		var allocErr error
		original, allocErr = task.NewCell()
		if allocErr != nil {
			return allocErr
		}
		if err := task.Set(original, 0, memory.NumberValue(42)); err != nil {
			return err
		}
		_, err := task.Copy(destination, func(task *browserruntime.TaskContext) error {
			copied = task.Refs[0]
			if copied == original {
				t.Fatal("Copy reused source Ref")
			}
			cell, err := task.Deref(copied)
			if err != nil {
				return err
			}
			if len(cell.Fields) != 1 || cell.Fields[0].Number() != 42 {
				t.Fatalf("copied Cell = %#v", cell)
			}
			return task.Set(copied, 0, memory.NumberValue(7))
		}, original)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := source.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := scheduler.Store().Deref(source.Owner(), original); !errors.Is(err, memory.ErrStaleRef) {
		t.Fatalf("source Ref after producer completion = %v, want ErrStaleRef", err)
	}
	if err := destination.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if copied == (memory.Ref{}) {
		t.Fatal("copy receiver did not run")
	}
	if _, err := scheduler.Store().Deref(destination.Owner(), copied); !errors.Is(err, memory.ErrStaleRef) {
		t.Fatalf("copied Ref after receiver completion = %v, want ErrStaleRef", err)
	}
}

func TestMicrotaskCaptureIsBorrowedAndDoesNotEscapeTaskRegion(t *testing.T) {
	t.Parallel()

	realm, err := browserruntime.NewRealm(100, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer realm.Close()
	var captured memory.Ref
	var captureErr error
	if _, err := realm.EnqueueTask(func(task *browserruntime.TaskContext) error {
		var allocErr error
		captured, allocErr = task.NewCell()
		if allocErr != nil {
			return allocErr
		}
		_, err := task.QueueMicrotask(func(microtask *browserruntime.TaskContext) error {
			_, captureErr = microtask.Deref(captured)
			return nil
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(captureErr, memory.ErrStaleRef) {
		t.Fatalf("implicit microtask escape error = %v, want ErrStaleRef", captureErr)
	}
}

func TestMicrotaskTransferPromotesSubgraphAndReleasesOriginalRegion(t *testing.T) {
	t.Parallel()

	realm, err := browserruntime.NewRealm(101, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer realm.Close()
	var a memory.Ref
	var b memory.Ref
	var c memory.Ref
	var promotedB memory.Ref
	var promotedC memory.Ref
	var microtaskRan bool
	if _, err := realm.EnqueueTask(func(task *browserruntime.TaskContext) error {
		var allocErr error
		a, allocErr = task.NewCell()
		if allocErr != nil {
			return allocErr
		}
		b, allocErr = task.NewCell()
		if allocErr != nil {
			return allocErr
		}
		c, allocErr = task.NewCell()
		if allocErr != nil {
			return allocErr
		}
		if err := task.Set(a, 0, memory.RefValue(b)); err != nil {
			return err
		}
		if err := task.Set(b, 0, memory.RefValue(c)); err != nil {
			return err
		}
		if _, err := task.QueueMicrotaskSend(func(*browserruntime.TaskContext) error {
			t.Fatal("unqualified private microtask send executed")
			return nil
		}, b); !errors.Is(err, memory.ErrExplicitSendRequired) {
			t.Fatalf("QueueMicrotaskSend(private) error = %v, want ErrExplicitSendRequired", err)
		}
		_, err := task.QueueMicrotaskTransfer(func(microtask *browserruntime.TaskContext) error {
			microtaskRan = true
			if len(microtask.Refs) != 1 || microtask.Refs[0] != b {
				t.Fatalf("microtask refs = %#v, want [%s]", microtask.Refs, b)
			}
			if _, err := microtask.Deref(b); err != nil {
				return err
			}
			var promoteErr error
			promotedB, promoteErr = microtask.PromoteRef(b)
			if promoteErr != nil {
				return promoteErr
			}
			cell, err := microtask.Deref(promotedB)
			if err != nil {
				return err
			}
			if len(cell.Fields) != 1 || !cell.Fields[0].IsRef() {
				t.Fatalf("promoted B = %#v", cell)
			}
			promotedC = cell.Fields[0].Ref()
			return realm.Store().CheckInvariants()
		}, b)
		if err != nil {
			return err
		}
		if _, err := task.Deref(a); !errors.Is(err, memory.ErrRegionInTransit) {
			t.Fatalf("stack borrow after transfer = %v, want ErrRegionInTransit", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !microtaskRan {
		t.Fatal("transferred microtask did not run")
	}
	for _, original := range []memory.Ref{a, b, c} {
		if _, err := realm.Store().Deref(realm.Owner(), original); !errors.Is(err, memory.ErrStaleRef) {
			t.Errorf("original %s after microtask = %v, want ErrStaleRef", original, err)
		}
	}
	if promotedB == (memory.Ref{}) || promotedC == (memory.Ref{}) {
		t.Fatalf("promoted refs = B:%s C:%s", promotedB, promotedC)
	}
	for _, promoted := range []memory.Ref{promotedB, promotedC} {
		if _, err := realm.Store().Deref(realm.Owner(), promoted); err != nil {
			t.Errorf("published %s after microtask = %v", promoted, err)
		}
	}
	region, err := realm.Store().Region(promotedB.Region)
	if err != nil {
		t.Fatal(err)
	}
	if region.State != memory.RegionPublished || region.Owner.Kind != ownership.OwnerShared {
		t.Fatalf("promoted region = %#v", region)
	}
	stats := realm.Store().Stats()
	if stats.LiveCells != 2 || stats.LiveRegions != 1 {
		t.Fatalf("Stats() = %#v, want only promoted B and C", stats)
	}
	if err := realm.Store().CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}
