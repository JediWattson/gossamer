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
	transfers    []memory.Ref
}

// TaskContext exposes the current task's semantic lifetime and queue seams.
type TaskContext struct {
	Realm        *Realm
	TaskID       TaskID
	Owner        ownership.OwnerID
	Region       ownership.RegionID
	MemoryRegion memory.RegionID
	Refs         []memory.Ref

	intrinsics *Intrinsics
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

func (context *TaskContext) HeapKind(ref memory.Ref) (memory.HeapKind, error) {
	if context == nil || context.Realm == nil {
		return memory.HeapInvalid, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.Kind(context.Owner, ref)
}

// NewHeapObject allocates an empty native object. NewObject remains the
// ownership-ledger test helper and intentionally returns a shadow ObjectID.
func (context *TaskContext) NewHeapObject() (memory.Ref, error) {
	if context == nil || context.Realm == nil {
		return memory.Ref{}, fmt.Errorf("runtime: nil task context")
	}
	ref, err := context.Realm.store.AllocObject(context.Owner, context.MemoryRegion)
	if err == nil && context.intrinsics != nil {
		err = context.SetPrototype(ref, memory.RefValue(context.intrinsics.ObjectPrototype))
	}
	return ref, err
}

func (context *TaskContext) DerefObject(ref memory.Ref) (memory.Object, error) {
	if context == nil || context.Realm == nil {
		return memory.Object{}, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.DerefObject(context.Owner, ref)
}

func (context *TaskContext) DerefObjectHeader(ref memory.Ref) (memory.ObjectHeader, error) {
	if context == nil || context.Realm == nil {
		return memory.ObjectHeader{}, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.DerefObjectHeader(context.Owner, ref)
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

func (context *TaskContext) GetOwnPropertyDescriptor(object, name memory.Ref) (memory.Property, bool, error) {
	if context == nil || context.Realm == nil {
		return memory.Property{}, false, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.GetOwnPropertyDescriptor(context.Owner, object, name)
}

func (context *TaskContext) DefineProperty(object, name memory.Ref, descriptor memory.Property) error {
	if context == nil || context.Realm == nil {
		return fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.DefineProperty(context.Owner, object, name, descriptor)
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
	ref, err := context.Realm.store.AllocArray(context.Owner, context.MemoryRegion, length)
	if err == nil && context.intrinsics != nil {
		err = context.SetPrototype(ref, memory.RefValue(context.intrinsics.ArrayPrototype))
	}
	return ref, err
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

func (context *TaskContext) DeclareIndirectBinding(contextRef, name, targetContext, targetName memory.Ref) error {
	if context == nil || context.Realm == nil {
		return fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.DeclareIndirectBinding(context.Owner, contextRef, name, targetContext, targetName)
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
	value, found, err := context.Realm.store.ResolveBinding(context.Owner, contextRef, name)
	if err != nil || found {
		return value, found, err
	}

	// Browser scripts use an Object-backed global environment: an own property
	// created through globalThis must also be visible to an identifier lookup.
	// Standalone interpreter contexts have no globalThis binding and retain the
	// closed lexical-environment behavior above.
	globalName, err := context.NewString("globalThis")
	if err != nil {
		return memory.Value{}, false, err
	}
	global, found, err := context.Realm.store.ResolveBinding(context.Owner, contextRef, globalName)
	if err != nil || !found || !global.IsRef() {
		return memory.Value{}, false, err
	}
	return context.Realm.store.GetOwnProperty(context.Owner, global.Ref(), name)
}

func (context *TaskContext) NewBytecodeFunction(name, environment memory.Value, arity uint32, code []byte, constants []memory.Value) (memory.Ref, error) {
	if context == nil || context.Realm == nil {
		return memory.Ref{}, fmt.Errorf("runtime: nil task context")
	}
	ref, err := context.Realm.store.AllocBytecodeFunction(context.Owner, context.MemoryRegion, name, environment, arity, code, constants)
	if err != nil || context.intrinsics == nil {
		return ref, err
	}
	if err := context.intrinsics.initializeFunction(context, ref, name, arity, true); err != nil {
		_ = context.Realm.store.Free(context.Owner, ref)
		return memory.Ref{}, err
	}
	return ref, nil
}

func (context *TaskContext) NewNativeFunction(name, environment memory.Value, arity uint32, nativeID uint64) (memory.Ref, error) {
	if context == nil || context.Realm == nil {
		return memory.Ref{}, fmt.Errorf("runtime: nil task context")
	}
	ref, err := context.Realm.store.AllocNativeFunction(context.Owner, context.MemoryRegion, name, environment, arity, nativeID)
	if err != nil || context.intrinsics == nil {
		return ref, err
	}
	if err := context.intrinsics.initializeFunction(context, ref, name, arity, false); err != nil {
		_ = context.Realm.store.Free(context.Owner, ref)
		return memory.Ref{}, err
	}
	return ref, nil
}

func (context *TaskContext) NewNativeConstructor(name, environment memory.Value, arity uint32, nativeID uint64) (memory.Ref, error) {
	if context == nil || context.Realm == nil {
		return memory.Ref{}, fmt.Errorf("runtime: nil task context")
	}
	ref, err := context.Realm.store.AllocNativeConstructor(context.Owner, context.MemoryRegion, name, environment, arity, nativeID)
	if err != nil || context.intrinsics == nil {
		return ref, err
	}
	if err := context.intrinsics.initializeFunction(context, ref, name, arity, true); err != nil {
		_ = context.Realm.store.Free(context.Owner, ref)
		return memory.Ref{}, err
	}
	return ref, nil
}

func (context *TaskContext) NewBoundNativeFunction(name, environment memory.Value, arity uint32, nativeID uint64, captures ...memory.Value) (memory.Ref, error) {
	if context == nil || context.Realm == nil {
		return memory.Ref{}, fmt.Errorf("runtime: nil task context")
	}
	ref, err := context.Realm.store.AllocBoundNativeFunction(context.Owner, context.MemoryRegion, name, environment, arity, nativeID, captures)
	if err != nil || context.intrinsics == nil {
		return ref, err
	}
	if err := context.intrinsics.initializeFunction(context, ref, name, arity, false); err != nil {
		_ = context.Realm.store.Free(context.Owner, ref)
		return memory.Ref{}, err
	}
	return ref, nil
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
	ref, err := context.Realm.store.AllocPromise(context.Owner, context.MemoryRegion)
	if err == nil && context.intrinsics != nil {
		err = context.SetPrototype(ref, memory.RefValue(context.intrinsics.PromisePrototype))
	}
	return ref, err
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

func (context *TaskContext) NewMap() (memory.Ref, error) {
	if context == nil || context.Realm == nil {
		return memory.Ref{}, fmt.Errorf("runtime: nil task context")
	}
	ref, err := context.Realm.store.AllocMap(context.Owner, context.MemoryRegion)
	if err == nil && context.intrinsics != nil {
		err = context.SetPrototype(ref, memory.RefValue(context.intrinsics.MapPrototype))
	}
	return ref, err
}

func (context *TaskContext) DerefMap(ref memory.Ref) (memory.Map, error) {
	if context == nil || context.Realm == nil {
		return memory.Map{}, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.DerefMap(context.Owner, ref)
}

func (context *TaskContext) MapSet(ref memory.Ref, key, value memory.Value) error {
	if context == nil || context.Realm == nil {
		return fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.MapSet(context.Owner, ref, key, value)
}

func (context *TaskContext) MapGet(ref memory.Ref, key memory.Value) (memory.Value, bool, error) {
	if context == nil || context.Realm == nil {
		return memory.Value{}, false, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.MapGet(context.Owner, ref, key)
}

func (context *TaskContext) MapDelete(ref memory.Ref, key memory.Value) (bool, error) {
	if context == nil || context.Realm == nil {
		return false, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.MapDelete(context.Owner, ref, key)
}

func (context *TaskContext) MapClear(ref memory.Ref) error {
	if context == nil || context.Realm == nil {
		return fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.MapClear(context.Owner, ref)
}

func (context *TaskContext) NewSet() (memory.Ref, error) {
	if context == nil || context.Realm == nil {
		return memory.Ref{}, fmt.Errorf("runtime: nil task context")
	}
	ref, err := context.Realm.store.AllocSet(context.Owner, context.MemoryRegion)
	if err == nil && context.intrinsics != nil {
		err = context.SetPrototype(ref, memory.RefValue(context.intrinsics.SetPrototype))
	}
	return ref, err
}

func (context *TaskContext) DerefSet(ref memory.Ref) (memory.Set, error) {
	if context == nil || context.Realm == nil {
		return memory.Set{}, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.DerefSet(context.Owner, ref)
}

func (context *TaskContext) SetAdd(ref memory.Ref, value memory.Value) error {
	if context == nil || context.Realm == nil {
		return fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.SetAdd(context.Owner, ref, value)
}

func (context *TaskContext) SetHas(ref memory.Ref, value memory.Value) (bool, error) {
	if context == nil || context.Realm == nil {
		return false, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.SetHas(context.Owner, ref, value)
}

func (context *TaskContext) SetDelete(ref memory.Ref, value memory.Value) (bool, error) {
	if context == nil || context.Realm == nil {
		return false, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.SetDelete(context.Owner, ref, value)
}

func (context *TaskContext) SetClear(ref memory.Ref) error {
	if context == nil || context.Realm == nil {
		return fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.SetClear(context.Owner, ref)
}

func (context *TaskContext) NewIterator(target memory.Ref, kind memory.IteratorKind) (memory.Ref, error) {
	if context == nil || context.Realm == nil {
		return memory.Ref{}, fmt.Errorf("runtime: nil task context")
	}
	ref, err := context.Realm.store.AllocIterator(context.Owner, context.MemoryRegion, target, kind)
	if err == nil && context.intrinsics != nil {
		err = context.SetPrototype(ref, memory.RefValue(context.intrinsics.IteratorPrototype))
	}
	return ref, err
}

func (context *TaskContext) DerefIterator(ref memory.Ref) (memory.Iterator, error) {
	if context == nil || context.Realm == nil {
		return memory.Iterator{}, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.DerefIterator(context.Owner, ref)
}

func (context *TaskContext) AdvanceIterator(ref memory.Ref) (memory.IteratorStep, error) {
	if context == nil || context.Realm == nil {
		return memory.IteratorStep{}, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.AdvanceIterator(context.Owner, ref)
}

func (context *TaskContext) NewWeakMap() (memory.Ref, error) {
	if context == nil || context.Realm == nil {
		return memory.Ref{}, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.AllocWeakMap(context.Owner, context.MemoryRegion)
}

func (context *TaskContext) DerefWeakMap(ref memory.Ref) (memory.WeakMap, error) {
	if context == nil || context.Realm == nil {
		return memory.WeakMap{}, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.DerefWeakMap(context.Owner, ref)
}

func (context *TaskContext) WeakMapSet(ref, key memory.Ref, value memory.Value) error {
	if context == nil || context.Realm == nil {
		return fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.WeakMapSet(context.Owner, ref, key, value)
}

func (context *TaskContext) WeakMapGet(ref, key memory.Ref) (memory.Value, bool, error) {
	if context == nil || context.Realm == nil {
		return memory.Value{}, false, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.WeakMapGet(context.Owner, ref, key)
}

func (context *TaskContext) WeakMapDelete(ref, key memory.Ref) (bool, error) {
	if context == nil || context.Realm == nil {
		return false, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.WeakMapDelete(context.Owner, ref, key)
}

func (context *TaskContext) WeakMapClear(ref memory.Ref) error {
	if context == nil || context.Realm == nil {
		return fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.WeakMapClear(context.Owner, ref)
}

func (context *TaskContext) NewWeakSet() (memory.Ref, error) {
	if context == nil || context.Realm == nil {
		return memory.Ref{}, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.AllocWeakSet(context.Owner, context.MemoryRegion)
}

func (context *TaskContext) DerefWeakSet(ref memory.Ref) (memory.WeakSet, error) {
	if context == nil || context.Realm == nil {
		return memory.WeakSet{}, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.DerefWeakSet(context.Owner, ref)
}

func (context *TaskContext) WeakSetAdd(ref, key memory.Ref) error {
	if context == nil || context.Realm == nil {
		return fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.WeakSetAdd(context.Owner, ref, key)
}

func (context *TaskContext) WeakSetHas(ref, key memory.Ref) (bool, error) {
	if context == nil || context.Realm == nil {
		return false, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.WeakSetHas(context.Owner, ref, key)
}

func (context *TaskContext) WeakSetDelete(ref, key memory.Ref) (bool, error) {
	if context == nil || context.Realm == nil {
		return false, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.WeakSetDelete(context.Owner, ref, key)
}

func (context *TaskContext) WeakSetClear(ref memory.Ref) error {
	if context == nil || context.Realm == nil {
		return fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.WeakSetClear(context.Owner, ref)
}

func (context *TaskContext) NewHostObject(value memory.HostObject) (memory.Ref, error) {
	if context == nil || context.Realm == nil {
		return memory.Ref{}, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.AllocHostObject(context.Owner, context.MemoryRegion, value)
}

func (context *TaskContext) DerefHostObject(ref memory.Ref) (memory.HostObject, error) {
	if context == nil || context.Realm == nil {
		return memory.HostObject{}, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.DerefHostObject(context.Owner, ref)
}

func (context *TaskContext) NewDate(milliseconds float64) (memory.Ref, error) {
	if context == nil || context.Realm == nil {
		return memory.Ref{}, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.AllocDate(context.Owner, context.MemoryRegion, milliseconds)
}

func (context *TaskContext) DerefDate(ref memory.Ref) (memory.Date, error) {
	if context == nil || context.Realm == nil {
		return memory.Date{}, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.DerefDate(context.Owner, ref)
}

func (context *TaskContext) SetDateTime(ref memory.Ref, milliseconds float64) error {
	if context == nil || context.Realm == nil {
		return fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.SetDateTime(context.Owner, ref, milliseconds)
}

func (context *TaskContext) NewRegExp(pattern memory.Ref, flags string) (memory.Ref, error) {
	if context == nil || context.Realm == nil {
		return memory.Ref{}, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.AllocRegExp(context.Owner, context.MemoryRegion, pattern, flags)
}

func (context *TaskContext) DerefRegExp(ref memory.Ref) (memory.RegExp, error) {
	if context == nil || context.Realm == nil {
		return memory.RegExp{}, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.DerefRegExp(context.Owner, ref)
}

func (context *TaskContext) SetRegExpLastIndex(ref memory.Ref, index uint64) error {
	if context == nil || context.Realm == nil {
		return fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.SetRegExpLastIndex(context.Owner, ref, index)
}

func (context *TaskContext) NewError(kind memory.ErrorKind, message memory.Value) (memory.Ref, error) {
	if context == nil || context.Realm == nil {
		return memory.Ref{}, fmt.Errorf("runtime: nil task context")
	}
	ref, err := context.Realm.store.AllocError(context.Owner, context.MemoryRegion, kind, message)
	if err != nil || context.intrinsics == nil {
		return ref, err
	}
	prototype := context.intrinsics.ErrorPrototype
	switch kind {
	case memory.ErrorType:
		prototype = context.intrinsics.TypeErrorPrototype
	case memory.ErrorRange:
		prototype = context.intrinsics.RangeErrorPrototype
	case memory.ErrorReference:
		prototype = context.intrinsics.ReferenceErrorPrototype
	}
	if err := context.SetPrototype(ref, memory.RefValue(prototype)); err != nil {
		return memory.Ref{}, err
	}
	if message.Kind() != memory.ValueNull {
		if err := defineData(context, ref, "message", message, true, false, true); err != nil {
			return memory.Ref{}, err
		}
	}
	return ref, nil
}

func (context *TaskContext) DerefError(ref memory.Ref) (memory.ErrorObject, error) {
	if context == nil || context.Realm == nil {
		return memory.ErrorObject{}, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.DerefError(context.Owner, ref)
}

func (context *TaskContext) SetErrorMessage(ref memory.Ref, message memory.Value) error {
	if context == nil || context.Realm == nil {
		return fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.SetErrorMessage(context.Owner, ref, message)
}

func (context *TaskContext) SetErrorStack(ref memory.Ref, stack memory.Value) error {
	if context == nil || context.Realm == nil {
		return fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.SetErrorStack(context.Owner, ref, stack)
}

func (context *TaskContext) SetErrorCause(ref memory.Ref, cause memory.Value) error {
	if context == nil || context.Realm == nil {
		return fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.SetErrorCause(context.Owner, ref, cause)
}

func (context *TaskContext) ClearErrorCause(ref memory.Ref) error {
	if context == nil || context.Realm == nil {
		return fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.ClearErrorCause(context.Owner, ref)
}

func (context *TaskContext) SetAggregateErrors(ref memory.Ref, errors []memory.Value) error {
	if context == nil || context.Realm == nil {
		return fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.SetAggregateErrors(context.Owner, ref, errors)
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
// globally readable. Ordinary task completion does not publish refs; writes
// into longer-lived native objects instead use the Store's copy-on-escape
// ownership barrier.
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

// StructuredClone deep-copies refs into target's task queue and atomically
// detaches the source ArrayBuffers listed in transfers. The receiving task sees
// only cloned roots; transfer-list entries are cloned as graph dependencies.
func (context *TaskContext) StructuredClone(target *Realm, run TaskFunc, transfers []memory.Ref, refs ...memory.Ref) (TaskID, error) {
	if context == nil || context.Realm == nil || target == nil {
		return 0, fmt.Errorf("runtime: nil task context or target realm")
	}
	if context.Realm.store != target.store {
		return 0, fmt.Errorf("runtime: realms do not share a RegionStore")
	}
	return target.enqueueStructuredClone(target.Tasks, run, context.Owner, refs, transfers)
}

// CopyToRealm deep-copies the reachable native graph into Realm-owned storage.
// It is the retention boundary for values that must outlive the current task
// but are not runnable until a later external event fires.
func (context *TaskContext) CopyToRealm(refs ...memory.Ref) ([]memory.Ref, error) {
	if context == nil || context.Realm == nil {
		return nil, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.store.Copy(context.Owner, context.Realm.owner, refs...)
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

// QueueMicrotaskStructuredClone is the microtask form of StructuredClone.
func (context *TaskContext) QueueMicrotaskStructuredClone(run TaskFunc, transfers []memory.Ref, refs ...memory.Ref) (TaskID, error) {
	if context == nil || context.Realm == nil {
		return 0, fmt.Errorf("runtime: nil task context")
	}
	return context.Realm.enqueueStructuredClone(context.Realm.Microtasks, run, context.Owner, refs, transfers)
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
	memoryStructuredClone
)
