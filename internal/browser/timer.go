package browser

import (
	"errors"
	"fmt"
	"time"

	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

type TimerID uint64

type pageTimer struct {
	id         TimerID
	callback   ValueHandle
	object     ownership.ObjectID
	generation DocumentGeneration
	clock      *time.Timer
	done       chan struct{}
}

func (page *Page) setTimeoutFromTask(
	task *browserruntime.TaskContext,
	callback ValueHandle,
	delay time.Duration,
) (TimerID, error) {
	if callback == 0 {
		return 0, fmt.Errorf("browser: invalid callback handle 0")
	}
	if delay < 0 {
		delay = 0
	}
	object, err := task.NewObject()
	if err != nil {
		return 0, err
	}
	if err := task.PublishToRealm(object); err != nil {
		return 0, err
	}

	page.mutex.Lock()
	if page.closed {
		page.mutex.Unlock()
		_ = page.Realm.Ledger().Release(object, page.Realm.Owner())
		return 0, ErrPageClosed
	}
	page.nextTimer++
	id := page.nextTimer
	timer := &pageTimer{
		id:         id,
		callback:   callback,
		object:     object,
		generation: page.documentGeneration,
		clock:      time.NewTimer(delay),
		done:       make(chan struct{}),
	}
	page.timers[id] = timer
	page.mutex.Unlock()

	go func() {
		select {
		case <-timer.clock.C:
			page.fireTimer(id)
		case <-timer.done:
		}
	}()
	return id, nil
}

func (page *Page) clearTimeout(id TimerID) error {
	if page == nil || id == 0 {
		return nil
	}
	page.mutex.Lock()
	timer := page.timers[id]
	if timer != nil {
		delete(page.timers, id)
		timer.clock.Stop()
		close(timer.done)
	}
	page.mutex.Unlock()
	if timer == nil {
		return nil
	}
	err := page.Realm.Ledger().Release(timer.object, page.Realm.Owner())
	if errors.Is(err, ownership.ErrObjectDestroyed) || errors.Is(err, ownership.ErrNotOwned) {
		return nil
	}
	return err
}

func (page *Page) fireTimer(id TimerID) {
	page.mutex.Lock()
	timer := page.timers[id]
	if timer == nil {
		page.mutex.Unlock()
		return
	}
	delete(page.timers, id)
	current := !page.closed && timer.generation == page.documentGeneration && page.script != nil
	page.mutex.Unlock()

	if !current {
		_ = page.Realm.Ledger().Release(timer.object, page.Realm.Owner())
		return
	}
	_, err := page.Realm.EnqueueRealmTask(func(task *browserruntime.TaskContext) error {
		return page.invokeScript(task, timer.generation, timer.callback, true)
	}, timer.object)
	if err != nil {
		_ = page.Realm.Ledger().Release(timer.object, page.Realm.Owner())
	}
}

func (page *Page) takeTimersLocked() []*pageTimer {
	timers := make([]*pageTimer, 0, len(page.timers))
	for id, timer := range page.timers {
		delete(page.timers, id)
		timer.clock.Stop()
		close(timer.done)
		timers = append(timers, timer)
	}
	return timers
}

func (page *Page) releaseTimers(timers []*pageTimer) error {
	var result error
	for _, timer := range timers {
		err := page.Realm.Ledger().Release(timer.object, page.Realm.Owner())
		if errors.Is(err, ownership.ErrObjectDestroyed) || errors.Is(err, ownership.ErrNotOwned) {
			continue
		}
		result = errors.Join(result, err)
	}
	return result
}
