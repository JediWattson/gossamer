// Package runtime implements Gossamer's ordered page-execution kernel. It is
// deliberately independent from any JavaScript engine.
package runtime

import (
	"errors"
	"fmt"

	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

type RealmID uint64
type TaskID uint64
type QueueID uint64

// TaskFunc executes on a realm's single logical executor.
type TaskFunc func(*TaskContext) error

// Task is one queued unit of ordered realm work.
type Task struct {
	ID  TaskID
	Run TaskFunc

	owner   ownership.OwnerID
	region  ownership.RegionID
	objects []ownership.ObjectID
}

// TaskContext exposes the current task's semantic lifetime and queue seams.
type TaskContext struct {
	Realm  *Realm
	TaskID TaskID
	Owner  ownership.OwnerID
	Region ownership.RegionID
}

func (context *TaskContext) NewObject() (ownership.ObjectID, error) {
	if context == nil || context.Realm == nil {
		return 0, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.ledger.CreateObject(context.Region)
}

func (context *TaskContext) QueueTask(run TaskFunc, objects ...ownership.ObjectID) (TaskID, error) {
	if context == nil || context.Realm == nil {
		return 0, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.enqueue(context.Realm.Tasks, run, context.Owner, objects)
}

func (context *TaskContext) QueueMicrotask(run TaskFunc, objects ...ownership.ObjectID) (TaskID, error) {
	if context == nil || context.Realm == nil {
		return 0, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.enqueue(context.Realm.Microtasks, run, context.Owner, objects)
}

func (context *TaskContext) PublishToRealm(object ownership.ObjectID) error {
	if context == nil || context.Realm == nil {
		return fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.ledger.Publish(object, context.Owner, context.Realm.owner)
}

var (
	ErrNilTask      = errors.New("runtime: nil task")
	ErrQueueClosed  = errors.New("runtime: task queue is closed")
	ErrRealmRunning = errors.New("runtime: realm already has an executor")
	ErrRealmClosed  = errors.New("runtime: realm is closed")
)
