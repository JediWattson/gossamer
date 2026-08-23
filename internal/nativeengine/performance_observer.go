package nativeengine

import (
	"fmt"

	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

const performanceObserverCallbackProperty = "\x00gossamer.performance-observer.callback"

func (realm *Realm) newPerformanceObserverConstructor(context *browserruntime.TaskContext) (memory.Ref, memory.Ref, error) {
	name, err := newString(context, "PerformanceObserver")
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	constructor, err := context.NewNativeConstructor(name, memory.RefValue(realm.active.Global), 1, nativePerformanceObserverConstructor)
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	prototypeName, err := context.NewString("prototype")
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	prototype, found, err := context.GetOwnProperty(constructor, prototypeName)
	if err != nil || !found || !prototype.IsRef() {
		return memory.Ref{}, memory.Ref{}, fmt.Errorf("nativeengine: PerformanceObserver constructor lost its prototype")
	}
	supported, err := context.NewArray(1)
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	navigation, err := newString(context, "navigation")
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	if err := context.SetArrayElement(supported, 0, navigation); err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	if err := defineData(context, constructor, "supportedEntryTypes", memory.RefValue(supported), false, false, false); err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	return constructor, prototype.Ref(), nil
}

func (realm *Realm) performanceObserverConstructor(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	if !this.IsRef() {
		return memory.Value{}, fmt.Errorf("%w: PerformanceObserver requires new", browserruntime.ErrOperandType)
	}
	callback := argument(arguments, 0)
	if err := requireFunction(context, callback); err != nil {
		return memory.Value{}, err
	}
	if err := defineData(context, this.Ref(), performanceObserverCallbackProperty, callback, false, false, false); err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), nil
}

// Strand exposes navigation timing but does not invent resource timing
// records the loader cannot currently prove. Observing is therefore inert
// until a supported entry is queued, while retaining the callback through the
// ordinary traced object graph.
func (realm *Realm) performanceObserverObserve(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	if err := requirePerformanceObserver(context, this); err != nil {
		return memory.Value{}, err
	}
	options := argument(arguments, 0)
	if !options.IsRef() {
		return memory.Value{}, fmt.Errorf("%w: PerformanceObserver options must be an object", browserruntime.ErrOperandType)
	}
	return memory.UndefinedValue(), nil
}

func (realm *Realm) performanceObserverDisconnect(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	if err := requirePerformanceObserver(context, this); err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), nil
}

func (realm *Realm) performanceObserverTakeRecords(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	if err := requirePerformanceObserver(context, this); err != nil {
		return memory.Value{}, err
	}
	records, err := context.NewArray(0)
	return memory.RefValue(records), err
}

func requirePerformanceObserver(context *browserruntime.TaskContext, this memory.Value) error {
	if !this.IsRef() {
		return fmt.Errorf("%w: incompatible PerformanceObserver receiver", browserruntime.ErrOperandType)
	}
	name, err := context.NewString(performanceObserverCallbackProperty)
	if err != nil {
		return err
	}
	callback, found, err := context.GetOwnProperty(this.Ref(), name)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%w: incompatible PerformanceObserver receiver", browserruntime.ErrOperandType)
	}
	return requireFunction(context, callback)
}
