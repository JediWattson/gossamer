package browser

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/dom"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func TestTimerRetainsCallbackInRealmThenTransfersItToFiringTask(t *testing.T) {
	t.Parallel()

	script := &timerTestRealm{}
	engine, err := NewWithEngine(&timerTestEngine{realm: script})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	location, _ := url.Parse("https://example.test/")
	page, err := engine.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}

	var timerID TimerID
	if _, err := page.Realm.EnqueueTask(func(task *browserruntime.TaskContext) error {
		var timerErr error
		timerID, timerErr = page.setTimeoutFromTask(task, 7, time.Hour)
		return timerErr
	}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	page.mutex.RLock()
	timer := page.timers[timerID]
	page.mutex.RUnlock()
	if timer == nil {
		t.Fatal("timer was not retained by Page")
	}
	persistent, err := page.Realm.Store().Region(timer.ref.Region)
	if err != nil {
		t.Fatal(err)
	}
	if persistent.State != memory.RegionPrivate || persistent.Owner != page.Realm.Owner() {
		t.Fatalf("persistent timer callback = %#v", persistent)
	}
	wantRecord := memory.HostObject{Class: browserTimerHostClass, Scope: uint64(timer.generation), Identity: uint64(timer.id)}
	if record, err := page.Realm.Store().DerefHostObject(page.Realm.Owner(), timer.ref); err != nil || record != wantRecord {
		t.Fatalf("persistent timer record = %#v, %v, want %#v", record, err, wantRecord)
	}

	page.fireTimer(timerID)
	queued, err := page.Realm.Store().Region(timer.ref.Region)
	if err != nil {
		t.Fatal(err)
	}
	if queued.State != memory.RegionInTransit || queued.Owner != page.Realm.Tasks.Owner() {
		t.Fatalf("queued timer callback = %#v", queued)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if script.invoked != 7 {
		t.Fatalf("invoked callback = %d, want 7", script.invoked)
	}
	if script.drains != 1 {
		t.Fatalf("engine microtask checkpoints = %d, want 1", script.drains)
	}
	completed, err := page.Realm.Store().Region(timer.ref.Region)
	if err != nil {
		t.Fatal(err)
	}
	if completed.State != memory.RegionDestroyed {
		t.Fatalf("completed timer callback = %#v", completed)
	}
	if _, err := page.Realm.Store().DerefHostObject(page.Realm.Owner(), timer.ref); !errors.Is(err, memory.ErrStaleRef) {
		t.Fatalf("completed timer record error = %v, want ErrStaleRef", err)
	}

	var staleTimer TimerID
	if _, err := page.Realm.EnqueueTask(func(task *browserruntime.TaskContext) error {
		var timerErr error
		staleTimer, timerErr = page.setTimeoutFromTask(task, 8, time.Hour)
		return timerErr
	}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	page.fireTimer(staleTimer)
	page.mutex.Lock()
	page.documentGeneration++ // Model navigation commit after firing but before callback execution.
	page.mutex.Unlock()
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if script.invoked != 7 {
		t.Fatalf("stale timer invoked callback %d after document replacement", script.invoked)
	}
}

func TestClearTimeoutDestroysRealmOwnedNativeRecord(t *testing.T) {
	t.Parallel()

	script := &timerTestRealm{}
	engine, err := NewWithEngine(&timerTestEngine{realm: script})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	location, _ := url.Parse("https://example.test/")
	page, err := engine.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}

	var timerID TimerID
	if _, err := page.Realm.EnqueueTask(func(task *browserruntime.TaskContext) error {
		var timerErr error
		timerID, timerErr = page.setTimeoutFromTask(task, 9, time.Hour)
		return timerErr
	}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	page.mutex.RLock()
	ref := page.timers[timerID].ref
	page.mutex.RUnlock()
	if err := page.clearTimeout(timerID); err != nil {
		t.Fatal(err)
	}
	region, err := page.Realm.Store().Region(ref.Region)
	if err != nil {
		t.Fatal(err)
	}
	if region.State != memory.RegionDestroyed {
		t.Fatalf("cleared timer region = %#v", region)
	}
}

func TestQueuedCallbacksUseQueueOwnedNativeRecords(t *testing.T) {
	t.Parallel()

	script := &timerTestRealm{}
	engine, err := NewWithEngine(&timerTestEngine{realm: script})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	location, _ := url.Parse("https://example.test/")
	page, err := engine.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	baseline := page.Realm.Store().Stats().LiveHostObjects

	if _, err := page.Realm.EnqueueTask(func(task *browserruntime.TaskContext) error {
		host := &taskHost{page: page, task: task, generation: page.DocumentGeneration()}
		if err := host.QueueCallback(11); err != nil {
			return err
		}
		if got := page.Realm.Store().Stats().LiveHostObjects; got != baseline+2 {
			t.Fatalf("live callback records before producer release = %d, want %d", got, baseline+2)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := page.Realm.Store().Stats().LiveHostObjects; got != baseline+1 {
		t.Fatalf("live callback records while queued = %d, want %d", got, baseline+1)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if script.invoked != 11 {
		t.Fatalf("queued callback invoked = %d, want 11", script.invoked)
	}
	if got := page.Realm.Store().Stats().LiveHostObjects; got != baseline {
		t.Fatalf("live callback records after execution = %d, want %d", got, baseline)
	}

	if _, err := page.Realm.EnqueueTask(func(task *browserruntime.TaskContext) error {
		host := &taskHost{page: page, task: task, generation: page.DocumentGeneration()}
		if err := host.QueueMicrotask(12); err != nil {
			return err
		}
		if got := page.Realm.Store().Stats().LiveHostObjects; got != baseline+2 {
			t.Fatalf("live microtask records before producer release = %d, want %d", got, baseline+2)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if script.invoked != 12 {
		t.Fatalf("queued microtask invoked = %d, want 12", script.invoked)
	}
	if got := page.Realm.Store().Stats().LiveHostObjects; got != baseline {
		t.Fatalf("live microtask records after execution = %d, want %d", got, baseline)
	}
}

type timerTestEngine struct {
	realm *timerTestRealm
}

func (engine *timerTestEngine) NewRealm() (JSRealm, error) { return engine.realm, nil }
func (*timerTestEngine) Close() error                      { return nil }

type timerTestRealm struct {
	invoked          ValueHandle
	drains           int
	animationInvoked []ValueHandle
	animationTimes   []float64
	dispatched       []InputEvent
}

func (*timerTestRealm) Evaluate(Host, ScriptSource) error { return nil }
func (realm *timerTestRealm) DispatchEvent(_ Host, event InputEvent) (EventDispatchResult, error) {
	realm.dispatched = append(realm.dispatched, event)
	return EventDispatchResult{}, nil
}
func (realm *timerTestRealm) Invoke(_ Host, callback ValueHandle) error {
	realm.invoked = callback
	return nil
}
func (realm *timerTestRealm) DrainMicrotasks(Host) error {
	realm.drains++
	return nil
}
func (realm *timerTestRealm) InvokeAnimationFrame(_ Host, callback ValueHandle, timestamp float64) error {
	realm.animationInvoked = append(realm.animationInvoked, callback)
	realm.animationTimes = append(realm.animationTimes, timestamp)
	return nil
}
func (*timerTestRealm) Close() error { return nil }
