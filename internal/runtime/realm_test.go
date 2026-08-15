package runtime_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

func TestRealmExecutesTasksInOrderAndDrainsMicrotasks(t *testing.T) {
	t.Parallel()

	realm, err := browserruntime.NewRealm(1, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer realm.Close()
	var order []string
	_, err = realm.EnqueueTask(func(context *browserruntime.TaskContext) error {
		order = append(order, "task:1")
		_, err := context.QueueMicrotask(func(context *browserruntime.TaskContext) error {
			order = append(order, "microtask:1")
			_, err := context.QueueMicrotask(func(*browserruntime.TaskContext) error {
				order = append(order, "microtask:2")
				return nil
			})
			return err
		})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = realm.EnqueueTask(func(*browserruntime.TaskContext) error {
		order = append(order, "task:2")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertOrder(t, order, []string{"task:1", "microtask:1", "microtask:2"})
	if err := realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertOrder(t, order, []string{"task:1", "microtask:1", "microtask:2", "task:2"})
}

func TestQueuedObjectTransfersBetweenTaskRegions(t *testing.T) {
	t.Parallel()

	ledger := ownership.NewLedger()
	realm, err := browserruntime.NewRealm(2, ledger)
	if err != nil {
		t.Fatal(err)
	}
	defer realm.Close()
	var object ownership.ObjectID
	var consumer ownership.OwnerID
	_, err = realm.EnqueueTask(func(context *browserruntime.TaskContext) error {
		var err error
		object, err = context.NewObject()
		if err != nil {
			return err
		}
		_, err = context.QueueTask(func(context *browserruntime.TaskContext) error {
			consumer = context.Owner
			snapshot, err := ledger.Object(object)
			if err != nil {
				return err
			}
			if snapshot.Owners[context.Owner] != 1 || snapshot.References != 1 {
				t.Fatalf("object while consumer runs = %#v", snapshot)
			}
			return nil
		}, object)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	queued, err := ledger.Object(object)
	if err != nil {
		t.Fatal(err)
	}
	if !queued.Alive || queued.Owners[realm.Tasks.Owner()] != 1 || queued.References != 1 {
		t.Fatalf("object after producer task = %#v", queued)
	}
	if err := realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	destroyed, err := ledger.Object(object)
	if err != nil {
		t.Fatal(err)
	}
	if destroyed.Alive || destroyed.References != 0 {
		t.Fatalf("object after consumer task = %#v", destroyed)
	}

	var published, transferred, consumerRelease, destroyedEvent bool
	for _, event := range ledger.Events() {
		if event.Object != object {
			continue
		}
		switch event.Kind {
		case ownership.ObjectPublished:
			published = event.To == realm.Tasks.Owner()
		case ownership.ObjectTransferred:
			transferred = event.From == realm.Tasks.Owner() && event.To == consumer
		case ownership.ObjectReleased:
			consumerRelease = consumerRelease || event.Owner == consumer
		case ownership.ObjectDestroyed:
			destroyedEvent = true
		}
	}
	if !published || !transferred || !consumerRelease || !destroyedEvent {
		t.Fatalf("lifecycle flags = publish:%t transfer:%t release:%t destroy:%t", published, transferred, consumerRelease, destroyedEvent)
	}
}

func TestRealmPublishedObjectLivesUntilRealmCloses(t *testing.T) {
	t.Parallel()

	ledger := ownership.NewLedger()
	realm, err := browserruntime.NewRealm(5, ledger)
	if err != nil {
		t.Fatal(err)
	}
	var object ownership.ObjectID
	_, err = realm.EnqueueTask(func(context *browserruntime.TaskContext) error {
		var err error
		object, err = context.NewObject()
		if err != nil {
			return err
		}
		return context.PublishToRealm(object)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	persistent, err := ledger.Object(object)
	if err != nil {
		t.Fatal(err)
	}
	if !persistent.Alive || persistent.Owners[realm.Owner()] != 1 || ledger.Stats().PersistentObjects != 1 {
		t.Fatalf("realm-published object = %#v stats=%#v", persistent, ledger.Stats())
	}
	if err := realm.Close(); err != nil {
		t.Fatal(err)
	}
	destroyed, err := ledger.Object(object)
	if err != nil {
		t.Fatal(err)
	}
	if destroyed.Alive || destroyed.References != 0 {
		t.Fatalf("object after Realm.Close = %#v", destroyed)
	}
}

func TestQueueKeepsOneRegionClaimAcrossMultipleQueuedAliases(t *testing.T) {
	t.Parallel()

	ledger := ownership.NewLedger()
	realm, err := browserruntime.NewRealm(6, ledger)
	if err != nil {
		t.Fatal(err)
	}
	defer realm.Close()
	var object ownership.ObjectID
	consumerClaims := make([]int, 0, 2)
	_, err = realm.EnqueueTask(func(context *browserruntime.TaskContext) error {
		var err error
		object, err = context.NewObject()
		if err != nil {
			return err
		}
		consume := func(context *browserruntime.TaskContext) error {
			snapshot, err := ledger.Object(object)
			if err != nil {
				return err
			}
			consumerClaims = append(consumerClaims, snapshot.References)
			return nil
		}
		if _, err := context.QueueTask(consume, object, object); err != nil {
			return err
		}
		_, err = context.QueueTask(consume, object)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	queued, err := ledger.Object(object)
	if err != nil {
		t.Fatal(err)
	}
	if queued.References != 1 || queued.Owners[realm.Tasks.Owner()] != 1 {
		t.Fatalf("queued aliases produced extra claims: %#v", queued)
	}
	if err := realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	between, err := ledger.Object(object)
	if err != nil {
		t.Fatal(err)
	}
	if !between.Alive || between.References != 1 || between.Owners[realm.Tasks.Owner()] != 1 {
		t.Fatalf("queue lost its shared claim after first consumer: %#v", between)
	}
	if err := realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	final, err := ledger.Object(object)
	if err != nil {
		t.Fatal(err)
	}
	if final.Alive {
		t.Fatalf("object survived final queued alias: %#v", final)
	}
	if len(consumerClaims) != 2 || consumerClaims[0] != 2 || consumerClaims[1] != 1 {
		t.Fatalf("consumer claim counts = %#v, want [2 1]", consumerClaims)
	}
}

func TestRealmRejectsASecondLogicalExecutor(t *testing.T) {
	t.Parallel()

	realm, err := browserruntime.NewRealm(3, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer realm.Close()
	started := make(chan struct{})
	release := make(chan struct{})
	_, err = realm.EnqueueTask(func(*browserruntime.TaskContext) error {
		close(started)
		<-release
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	firstResult := make(chan error, 1)
	go func() { firstResult <- realm.RunOne(context.Background()) }()
	waitSignal(t, started, "first executor")
	if err := realm.RunOne(context.Background()); !errors.Is(err, browserruntime.ErrRealmRunning) {
		t.Fatalf("second RunOne() error = %v, want ErrRealmRunning", err)
	}
	close(release)
	if err := waitResult(t, firstResult, "first executor result"); err != nil {
		t.Fatal(err)
	}
}

func TestSchedulerRunsRealmsConcurrently(t *testing.T) {
	t.Parallel()

	scheduler, err := browserruntime.NewScheduler(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer scheduler.Close()
	first, err := scheduler.NewRealm()
	if err != nil {
		t.Fatal(err)
	}
	second, err := scheduler.NewRealm()
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan browserruntime.RealmID, 2)
	release := make(chan struct{})
	for _, realm := range []*browserruntime.Realm{first, second} {
		realm := realm
		if _, err := realm.EnqueueTask(func(*browserruntime.TaskContext) error {
			entered <- realm.ID
			<-release
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	firstResult, err := scheduler.Start(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	secondResult, err := scheduler.Start(ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[browserruntime.RealmID]bool{}
	for range 2 {
		select {
		case id := <-entered:
			seen[id] = true
		case <-time.After(time.Second):
			t.Fatal("realms did not execute concurrently")
		}
	}
	if !seen[first.ID] || !seen[second.ID] {
		t.Fatalf("entered realms = %#v", seen)
	}
	close(release)
	cancel()
	if err := waitResult(t, firstResult, "first realm"); !errors.Is(err, context.Canceled) {
		t.Fatalf("first realm result = %v, want context.Canceled", err)
	}
	if err := waitResult(t, secondResult, "second realm"); !errors.Is(err, context.Canceled) {
		t.Fatalf("second realm result = %v, want context.Canceled", err)
	}
}

func TestSchedulerPublishesExternalCompletionEnvelopeThroughRealmQueue(t *testing.T) {
	t.Parallel()

	scheduler, err := browserruntime.NewScheduler(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer scheduler.Close()
	realm, err := scheduler.NewRealm()
	if err != nil {
		t.Fatal(err)
	}
	executed := false
	_, envelope, err := scheduler.EnqueueExternalTask(realm, func(*browserruntime.TaskContext) error {
		executed = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	queued, err := scheduler.Ledger().Object(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if queued.References != 1 || !queued.Alive {
		t.Fatalf("queued completion envelope = %#v", queued)
	}
	if err := realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !executed {
		t.Fatal("external completion did not execute")
	}
	completed, err := scheduler.Ledger().Object(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Alive || completed.References != 0 {
		t.Fatalf("completed envelope = %#v", completed)
	}

	var published, transferred bool
	for _, event := range scheduler.Ledger().Events() {
		if event.Object != envelope {
			continue
		}
		published = published || event.Kind == ownership.ObjectPublished &&
			event.From.Kind == ownership.OwnerBrowser && event.To.Kind == ownership.OwnerQueue
		transferred = transferred || event.Kind == ownership.ObjectTransferred &&
			event.From.Kind == ownership.OwnerQueue && event.To.Kind == ownership.OwnerTask
	}
	if !published || !transferred {
		t.Fatalf("completion ownership transitions: published=%t transferred=%t", published, transferred)
	}
}

func TestRealmTransfersPersistentCallbackIntoFiringTask(t *testing.T) {
	t.Parallel()

	realm, err := browserruntime.NewRealm(9, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer realm.Close()
	var callback ownership.ObjectID
	if _, err := realm.EnqueueTask(func(task *browserruntime.TaskContext) error {
		var createErr error
		callback, createErr = task.NewObject()
		if createErr != nil {
			return createErr
		}
		return task.PublishToRealm(callback)
	}); err != nil {
		t.Fatal(err)
	}
	if err := realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	persistent, err := realm.Ledger().Object(callback)
	if err != nil {
		t.Fatal(err)
	}
	if persistent.References != 1 || persistent.Owners[realm.Owner()] != 1 {
		t.Fatalf("persistent callback = %#v", persistent)
	}

	fired := false
	if _, err := realm.EnqueueRealmTask(func(*browserruntime.TaskContext) error {
		fired = true
		return nil
	}, callback); err != nil {
		t.Fatal(err)
	}
	queued, err := realm.Ledger().Object(callback)
	if err != nil {
		t.Fatal(err)
	}
	if queued.References != 1 || queued.Owners[realm.Tasks.Owner()] != 1 {
		t.Fatalf("queued callback = %#v", queued)
	}
	if err := realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !fired {
		t.Fatal("persistent callback did not fire")
	}
	completed, err := realm.Ledger().Object(callback)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Alive || completed.References != 0 {
		t.Fatalf("completed callback = %#v", completed)
	}
}

func TestConcurrentEnqueuePreservesEveryTask(t *testing.T) {
	t.Parallel()

	realm, err := browserruntime.NewRealm(4, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer realm.Close()
	const tasks = 100
	var wait sync.WaitGroup
	wait.Add(tasks)
	for range tasks {
		go func() {
			defer wait.Done()
			if _, err := realm.EnqueueTask(func(*browserruntime.TaskContext) error { return nil }); err != nil {
				t.Errorf("EnqueueTask() error = %v", err)
			}
		}()
	}
	wait.Wait()
	if got := realm.Tasks.Len(); got != tasks {
		t.Fatalf("Tasks.Len() = %d, want %d", got, tasks)
	}
	for range tasks {
		if err := realm.RunOne(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
}

func assertOrder(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("execution order = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("execution order = %#v, want %#v", got, want)
		}
	}
}

func waitSignal(t *testing.T, signal <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}

func waitResult(t *testing.T, result <-chan error, name string) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", name)
		return nil
	}
}
