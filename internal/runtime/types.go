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

// NewHeapObject allocates an empty native object. NewObject remains the
// ownership-ledger test helper and intentionally returns a shadow ObjectID.
func (context *TaskContext) NewHeapObject() (memory.Ref, error) {
	if context == nil || context.Realm == nil {
		return memory.Ref{}, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.AllocObject(context.Owner, context.MemoryRegion)
}

func (context *TaskContext) DerefObject(ref memory.Ref) (memory.Object, error) {
	if context == nil || context.Realm == nil {
		return memory.Object{}, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.DerefObject(context.Owner, ref)
}

func (context *TaskContext) SetPrototype(object memory.Ref, prototype memory.Value) error {
	if context == nil || context.Realm == nil {
		return fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.SetPrototype(context.Owner, object, prototype)
}

func (context *TaskContext) SetProperty(object, name memory.Ref, value memory.Value) error {
	if context == nil || context.Realm == nil {
		return fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.SetProperty(context.Owner, object, name, value)
}

func (context *TaskContext) GetOwnProperty(object, name memory.Ref) (memory.Value, bool, error) {
	if context == nil || context.Realm == nil {
		return memory.Value{}, false, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.GetOwnProperty(context.Owner, object, name)
}

func (context *TaskContext) DeleteProperty(object, name memory.Ref) (bool, error) {
	if context == nil || context.Realm == nil {
		return false, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.DeleteProperty(context.Owner, object, name)
}

func (context *TaskContext) NewArray(length uint32) (memory.Ref, error) {
	if context == nil || context.Realm == nil {
		return memory.Ref{}, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.AllocArray(context.Owner, context.MemoryRegion, length)
}

func (context *TaskContext) DerefArray(ref memory.Ref) (memory.Array, error) {
	if context == nil || context.Realm == nil {
		return memory.Array{}, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.DerefArray(context.Owner, ref)
}

func (context *TaskContext) ArrayElement(array memory.Ref, index uint32) (memory.Value, bool, error) {
	if context == nil || context.Realm == nil {
		return memory.Value{}, false, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.ArrayElement(context.Owner, array, index)
}

func (context *TaskContext) SetArrayElement(array memory.Ref, index uint32, value memory.Value) error {
	if context == nil || context.Realm == nil {
		return fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.SetArrayElement(context.Owner, array, index, value)
}

func (context *TaskContext) DeleteArrayElement(array memory.Ref, index uint32) (bool, error) {
	if context == nil || context.Realm == nil {
		return false, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.DeleteArrayElement(context.Owner, array, index)
}

func (context *TaskContext) SetArrayLength(array memory.Ref, length uint32) error {
	if context == nil || context.Realm == nil {
		return fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.SetArrayLength(context.Owner, array, length)
}

func (context *TaskContext) NewContext(parent memory.Value) (memory.Ref, error) {
	if context == nil || context.Realm == nil {
		return memory.Ref{}, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.AllocContext(context.Owner, context.MemoryRegion, parent)
}

func (context *TaskContext) DerefContext(ref memory.Ref) (memory.Context, error) {
	if context == nil || context.Realm == nil {
		return memory.Context{}, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.DerefContext(context.Owner, ref)
}

func (context *TaskContext) DeclareBinding(contextRef, name memory.Ref, mutable bool) error {
	if context == nil || context.Realm == nil {
		return fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.DeclareBinding(context.Owner, contextRef, name, mutable)
}

func (context *TaskContext) InitializeBinding(contextRef, name memory.Ref, value memory.Value) error {
	if context == nil || context.Realm == nil {
		return fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.InitializeBinding(context.Owner, contextRef, name, value)
}

func (context *TaskContext) SetBinding(contextRef, name memory.Ref, value memory.Value) error {
	if context == nil || context.Realm == nil {
		return fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.SetBinding(context.Owner, contextRef, name, value)
}

func (context *TaskContext) ResolveBinding(contextRef, name memory.Ref) (memory.Value, bool, error) {
	if context == nil || context.Realm == nil {
		return memory.Value{}, false, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.ResolveBinding(context.Owner, contextRef, name)
}

func (context *TaskContext) NewBytecodeFunction(name, environment memory.Value, arity uint32, code []byte, constants []memory.Value) (memory.Ref, error) {
	if context == nil || context.Realm == nil {
		return memory.Ref{}, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.AllocBytecodeFunction(context.Owner, context.MemoryRegion, name, environment, arity, code, constants)
}

func (context *TaskContext) NewNativeFunction(name, environment memory.Value, arity uint32, nativeID uint64) (memory.Ref, error) {
	if context == nil || context.Realm == nil {
		return memory.Ref{}, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.AllocNativeFunction(context.Owner, context.MemoryRegion, name, environment, arity, nativeID)
}

func (context *TaskContext) DerefFunction(ref memory.Ref) (memory.Function, error) {
	if context == nil || context.Realm == nil {
		return memory.Function{}, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.DerefFunction(context.Owner, ref)
}

func (context *TaskContext) NewPromise() (memory.Ref, error) {
	if context == nil || context.Realm == nil {
		return memory.Ref{}, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.AllocPromise(context.Owner, context.MemoryRegion)
}

func (context *TaskContext) DerefPromise(ref memory.Ref) (memory.Promise, error) {
	if context == nil || context.Realm == nil {
		return memory.Promise{}, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.DerefPromise(context.Owner, ref)
}

func (context *TaskContext) AddPromiseReaction(promise memory.Ref, reaction memory.PromiseReaction) error {
	if context == nil || context.Realm == nil {
		return fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.AddPromiseReaction(context.Owner, promise, reaction)
}

func (context *TaskContext) ResolvePromise(promise memory.Ref, result memory.Value) error {
	if context == nil || context.Realm == nil {
		return fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.ResolvePromise(context.Owner, promise, result)
}

func (context *TaskContext) RejectPromise(promise memory.Ref, reason memory.Value) error {
	if context == nil || context.Realm == nil {
		return fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.RejectPromise(context.Owner, promise, reason)
}

func (context *TaskContext) MarkPromiseHandled(promise memory.Ref) error {
	if context == nil || context.Realm == nil {
		return fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.MarkPromiseHandled(context.Owner, promise)
}

func (context *TaskContext) DrainPromiseReactions(promise memory.Ref) (memory.PromiseSettlement, error) {
	if context == nil || context.Realm == nil {
		return memory.PromiseSettlement{}, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.DrainPromiseReactions(context.Owner, promise)
}

func (context *TaskContext) NewBigInt(negative bool, magnitude []byte) (memory.Ref, error) {
	if context == nil || context.Realm == nil {
		return memory.Ref{}, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.AllocBigInt(context.Owner, context.MemoryRegion, negative, magnitude)
}

func (context *TaskContext) ParseBigInt(text string, base int) (memory.Ref, error) {
	if context == nil || context.Realm == nil {
		return memory.Ref{}, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.ParseBigInt(context.Owner, context.MemoryRegion, text, base)
}

func (context *TaskContext) DerefBigInt(ref memory.Ref) (memory.BigInt, error) {
	if context == nil || context.Realm == nil {
		return memory.BigInt{}, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.DerefBigInt(context.Owner, ref)
}

func (context *TaskContext) NewSymbol(description memory.Value) (memory.Ref, error) {
	if context == nil || context.Realm == nil {
		return memory.Ref{}, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.AllocSymbol(context.Owner, context.MemoryRegion, description)
}

func (context *TaskContext) DerefSymbol(ref memory.Ref) (memory.Symbol, error) {
	if context == nil || context.Realm == nil {
		return memory.Symbol{}, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.DerefSymbol(context.Owner, ref)
}

func (context *TaskContext) NewArrayBuffer(bytes []byte) (memory.Ref, error) {
	if context == nil || context.Realm == nil {
		return memory.Ref{}, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.AllocArrayBuffer(context.Owner, context.MemoryRegion, bytes)
}

func (context *TaskContext) DerefArrayBuffer(ref memory.Ref) (memory.ArrayBuffer, error) {
	if context == nil || context.Realm == nil {
		return memory.ArrayBuffer{}, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.DerefArrayBuffer(context.Owner, ref)
}

func (context *TaskContext) ReadArrayBuffer(ref memory.Ref, offset, length uint64) ([]byte, error) {
	if context == nil || context.Realm == nil {
		return nil, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.ReadArrayBuffer(context.Owner, ref, offset, length)
}

func (context *TaskContext) WriteArrayBuffer(ref memory.Ref, offset uint64, bytes []byte) error {
	if context == nil || context.Realm == nil {
		return fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.WriteArrayBuffer(context.Owner, ref, offset, bytes)
}

func (context *TaskContext) DetachArrayBuffer(ref memory.Ref) error {
	if context == nil || context.Realm == nil {
		return fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.DetachArrayBuffer(context.Owner, ref)
}

func (context *TaskContext) NewTypedArray(buffer memory.Ref, element memory.ElementKind, byteOffset, length uint64) (memory.Ref, error) {
	if context == nil || context.Realm == nil {
		return memory.Ref{}, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.AllocTypedArray(context.Owner, context.MemoryRegion, buffer, element, byteOffset, length)
}

func (context *TaskContext) DerefTypedArray(ref memory.Ref) (memory.TypedArray, error) {
	if context == nil || context.Realm == nil {
		return memory.TypedArray{}, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.DerefTypedArray(context.Owner, ref)
}

func (context *TaskContext) ReadTypedArrayElement(ref memory.Ref, index uint64) (float64, error) {
	if context == nil || context.Realm == nil {
		return 0, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.ReadTypedArrayElement(context.Owner, ref, index)
}

func (context *TaskContext) WriteTypedArrayElement(ref memory.Ref, index uint64, number float64) error {
	if context == nil || context.Realm == nil {
		return fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.WriteTypedArrayElement(context.Owner, ref, index, number)
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
