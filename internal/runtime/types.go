// Package runtime implements Gossamer's ordered page-execution kernel. It is
// deliberately independent from any JavaScript engine.
package runtime

import (
	"errors"
	"fmt"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
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

	owner        ownership.OwnerID
	region       ownership.RegionID
	memoryRegion memory.RegionID
	objects      []ownership.ObjectID
	refs         []memory.Ref
}

// TaskContext exposes the current task's semantic lifetime and queue seams.
type TaskContext struct {
	Realm        *Realm
	TaskID       TaskID
	Owner        ownership.OwnerID
	Region       ownership.RegionID
	MemoryRegion memory.RegionID
	Refs         []memory.Ref
}

func (context *TaskContext) NewObject() (ownership.ObjectID, error) {
	if context == nil || context.Realm == nil {
		return 0, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.ledger.CreateObject(context.Region)
}

// NewCell allocates one synthetic native Cell in the task's private region.
func (context *TaskContext) NewCell() (memory.Ref, error) {
	if context == nil || context.Realm == nil {
		return memory.Ref{}, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.AllocCell(context.Owner, context.MemoryRegion)
}

// NewString allocates an immutable native string in the task's private region.
func (context *TaskContext) NewString(text string) (memory.Ref, error) {
	if context == nil || context.Realm == nil {
		return memory.Ref{}, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.AllocString(context.Owner, context.MemoryRegion, text)
}

func (context *TaskContext) Deref(ref memory.Ref) (memory.Cell, error) {
	if context == nil || context.Realm == nil {
		return memory.Cell{}, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.Deref(context.Owner, ref)
}

func (context *TaskContext) DerefString(ref memory.Ref) (string, error) {
	if context == nil || context.Realm == nil {
		return "", fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.DerefString(context.Owner, ref)
}

func (context *TaskContext) Set(ref memory.Ref, field int, value memory.Value) error {
	if context == nil || context.Realm == nil {
		return fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.Set(context.Owner, ref, field, value)
}

func (context *TaskContext) Free(ref memory.Ref) error {
	if context == nil || context.Realm == nil {
		return fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.Free(context.Owner, ref)
}

// PublishRefs explicitly makes the reachable region graph immutable and
// globally readable. Task completion never promotes refs automatically.
func (context *TaskContext) PublishRefs(refs ...memory.Ref) error {
	if context == nil || context.Realm == nil {
		return fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.Publish(context.Owner, refs...)
}

// PromoteRef explicitly copies one reachable private graph into immutable
// shared storage and returns the new root. The original Ref remains private.
func (context *TaskContext) PromoteRef(ref memory.Ref) (memory.Ref, error) {
	refs, err := context.PromoteRefs(ref)
	if err != nil {
		return memory.Ref{}, err
	}
	return refs[0], nil
}

// PromoteRefs is the multi-root form of PromoteRef. Shared subgraphs are
// copied once and preserve aliasing across the returned roots.
func (context *TaskContext) PromoteRefs(refs ...memory.Ref) ([]memory.Ref, error) {
	if context == nil || context.Realm == nil {
		return nil, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.Promote(context.Owner, refs...)
}

// Send enqueues already-published refs in another Realm. Private refs fail.
func (context *TaskContext) Send(target *Realm, run TaskFunc, refs ...memory.Ref) (TaskID, error) {
	return context.enqueueRefs(target, targetQueue(target), memorySend, run, refs)
}

// Transfer moves private regions through the target Realm's task queue.
func (context *TaskContext) Transfer(target *Realm, run TaskFunc, refs ...memory.Ref) (TaskID, error) {
	return context.enqueueRefs(target, targetQueue(target), memoryTransfer, run, refs)
}

// Publish explicitly publishes refs before enqueueing them in target.
func (context *TaskContext) Publish(target *Realm, run TaskFunc, refs ...memory.Ref) (TaskID, error) {
	return context.enqueueRefs(target, targetQueue(target), memoryPublish, run, refs)
}

// Copy deep-copies the reachable Cell graph into queue-owned storage before
// enqueueing it in target.
func (context *TaskContext) Copy(target *Realm, run TaskFunc, refs ...memory.Ref) (TaskID, error) {
	return context.enqueueRefs(target, targetQueue(target), memoryCopy, run, refs)
}

// QueueMicrotaskSend enqueues already-published refs in this Realm's
// microtask queue. Private refs fail before the microtask becomes visible.
func (context *TaskContext) QueueMicrotaskSend(run TaskFunc, refs ...memory.Ref) (TaskID, error) {
	return context.enqueueRefs(contextRealm(context), microtaskQueue(context), memorySend, run, refs)
}

// QueueMicrotaskTransfer moves private regions through the microtask queue.
func (context *TaskContext) QueueMicrotaskTransfer(run TaskFunc, refs ...memory.Ref) (TaskID, error) {
	return context.enqueueRefs(contextRealm(context), microtaskQueue(context), memoryTransfer, run, refs)
}

// QueueMicrotaskPublish publishes refs before enqueueing the microtask.
func (context *TaskContext) QueueMicrotaskPublish(run TaskFunc, refs ...memory.Ref) (TaskID, error) {
	return context.enqueueRefs(contextRealm(context), microtaskQueue(context), memoryPublish, run, refs)
}

// QueueMicrotaskCopy clones refs into queue-owned storage for the microtask.
func (context *TaskContext) QueueMicrotaskCopy(run TaskFunc, refs ...memory.Ref) (TaskID, error) {
	return context.enqueueRefs(contextRealm(context), microtaskQueue(context), memoryCopy, run, refs)
}

func (context *TaskContext) enqueueRefs(target *Realm, queue *TaskQueue, mode memorySendMode, run TaskFunc, refs []memory.Ref) (TaskID, error) {
	if context == nil || context.Realm == nil || target == nil || queue == nil {
		return 0, fmt.Errorf("runtime: nil task context or target realm")
	}
	if context.Realm.store != target.store {
		return 0, fmt.Errorf("runtime: realms do not share a RegionStore")
	}
	return target.enqueueMemory(queue, run, context.Owner, mode, refs)
}

func targetQueue(target *Realm) *TaskQueue {
	if target == nil {
		return nil
	}
	return target.Tasks
}

func contextRealm(context *TaskContext) *Realm {
	if context == nil {
		return nil
	}
	return context.Realm
}

func microtaskQueue(context *TaskContext) *TaskQueue {
	if context == nil || context.Realm == nil {
		return nil
	}
	return context.Realm.Microtasks
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

type memorySendMode uint8

const (
	memorySend memorySendMode = iota
	memoryTransfer
	memoryPublish
	memoryCopy
)
