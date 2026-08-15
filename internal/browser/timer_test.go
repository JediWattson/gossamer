package browser

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/dom"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
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
	persistent, err := page.Realm.Ledger().Object(timer.object)
	if err != nil {
		t.Fatal(err)
	}
	if persistent.References != 1 || persistent.Owners[page.Realm.Owner()] != 1 {
		t.Fatalf("persistent timer callback = %#v", persistent)
	}

	page.fireTimer(timerID)
	queued, err := page.Realm.Ledger().Object(timer.object)
	if err != nil {
		t.Fatal(err)
	}
	if queued.References != 1 || queued.Owners[page.Realm.Tasks.Owner()] != 1 {
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
	completed, err := page.Realm.Ledger().Object(timer.object)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Alive || completed.References != 0 {
		t.Fatalf("completed timer callback = %#v", completed)
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

type timerTestEngine struct {
	realm *timerTestRealm
}

func (engine *timerTestEngine) NewRealm() (JSRealm, error) { return engine.realm, nil }
func (*timerTestEngine) Close() error                      { return nil }

type timerTestRealm struct {
	invoked ValueHandle
	drains  int
}

func (*timerTestRealm) Evaluate(Host, ScriptSource) error { return nil }
func (*timerTestRealm) DispatchEvent(Host, InputEvent) (EventDispatchResult, error) {
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
func (*timerTestRealm) Close() error { return nil }
