package nativeengine

import (
	"errors"
	"fmt"
	"sort"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

const (
	nativeMutationObserverConstructor uint64 = 13_000 + iota
	nativeMutationObserverObserve
	nativeMutationObserverDisconnect
	nativeMutationObserverTakeRecords
	nativeIntersectionObserverConstructor
	nativeIntersectionObserverObserve
	nativeIntersectionObserverUnobserve
	nativeIntersectionObserverDisconnect
	nativeIntersectionObserverTakeRecords
	nativeResizeObserverConstructor
	nativeResizeObserverObserve
	nativeResizeObserverUnobserve
	nativeResizeObserverDisconnect
)

const mutationObserverCallbackProperty = "\x00gossamer.mutation-observer.callback"
const intersectionObserverCallbackProperty = "\x00gossamer.intersection-observer.callback"
const resizeObserverCallbackProperty = "\x00gossamer.resize-observer.callback"
const observerTargetsProperty = "\x00gossamer.observer.targets"

type mutationObserverState struct {
	ID                uint64
	Callback          browser.ValueHandle
	Target            browser.NodeHandle
	Sequence          uint64
	ChildList         bool
	Attributes        bool
	CharacterData     bool
	Subtree           bool
	AttributeOldValue bool
	CharacterOldValue bool
}

type resizeObservation struct {
	Target    browser.NodeHandle
	Width     float64
	Height    float64
	Delivered bool
}

type resizeObserverState struct {
	ID           uint64
	Callback     browser.ValueHandle
	Observations []resizeObservation
}

func (realm *Realm) newMutationObserverConstructor(context *browserruntime.TaskContext) (memory.Ref, memory.Ref, error) {
	name, err := newString(context, "MutationObserver")
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	constructor, err := context.NewNativeConstructor(name, memory.RefValue(realm.active.Global), 1, nativeMutationObserverConstructor)
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	prototypeName, err := context.NewString("prototype")
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	prototype, found, err := context.GetOwnProperty(constructor, prototypeName)
	if err != nil || !found || !prototype.IsRef() {
		return memory.Ref{}, memory.Ref{}, fmt.Errorf("nativeengine: MutationObserver constructor lost its prototype")
	}
	return constructor, prototype.Ref(), nil
}

func (realm *Realm) newIntersectionObserverConstructor(context *browserruntime.TaskContext) (memory.Ref, memory.Ref, error) {
	name, err := newString(context, "IntersectionObserver")
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	constructor, err := context.NewNativeConstructor(name, memory.RefValue(realm.active.Global), 1, nativeIntersectionObserverConstructor)
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	prototypeName, err := context.NewString("prototype")
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	prototype, found, err := context.GetOwnProperty(constructor, prototypeName)
	if err != nil || !found || !prototype.IsRef() {
		return memory.Ref{}, memory.Ref{}, fmt.Errorf("nativeengine: IntersectionObserver constructor lost its prototype")
	}
	return constructor, prototype.Ref(), nil
}

func (realm *Realm) newResizeObserverConstructor(context *browserruntime.TaskContext) (memory.Ref, memory.Ref, error) {
	name, err := newString(context, "ResizeObserver")
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	constructor, err := context.NewNativeConstructor(name, memory.RefValue(realm.active.Global), 1, nativeResizeObserverConstructor)
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	prototypeName, err := context.NewString("prototype")
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	prototype, found, err := context.GetOwnProperty(constructor, prototypeName)
	if err != nil || !found || !prototype.IsRef() {
		return memory.Ref{}, memory.Ref{}, fmt.Errorf("nativeengine: ResizeObserver constructor lost its prototype")
	}
	return constructor, prototype.Ref(), nil
}

func resizeObserverCacheKey(id uint64) memory.Value {
	return memory.NumberValue(-float64(id))
}

func (realm *Realm) resizeObserverConstructor(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	if !this.IsRef() {
		return memory.Value{}, fmt.Errorf("%w: ResizeObserver requires new", browserruntime.ErrOperandType)
	}
	callback := argument(arguments, 0)
	if err := requireFunction(context, callback); err != nil {
		return memory.Value{}, err
	}
	if _, ok := realm.host.(browser.DOMGeometryHost); !ok {
		return memory.Value{}, fmt.Errorf("nativeengine: browser host does not expose geometry")
	}
	handle, err := realm.retainCallbackLocked(context, callback)
	if err != nil {
		return memory.Value{}, err
	}
	realm.nextResizeObserver++
	if realm.nextResizeObserver == 0 || realm.nextResizeObserver > maxExactInteger {
		return memory.Value{}, fmt.Errorf("nativeengine: ResizeObserver identity exhausted")
	}
	id := realm.nextResizeObserver
	metadata, err := realm.documentMetadata()
	if err != nil {
		return memory.Value{}, err
	}
	record, err := context.NewHostObject(memory.HostObject{
		Class: hostClassResizeObserver, Scope: uint64(metadata.Root.Document), Identity: id,
	})
	if err != nil {
		return memory.Value{}, err
	}
	if err := defineData(context, this.Ref(), hostRecordProperty, memory.RefValue(record), false, false, false); err != nil {
		return memory.Value{}, err
	}
	if err := defineData(context, this.Ref(), resizeObserverCallbackProperty, callback, false, false, false); err != nil {
		return memory.Value{}, err
	}
	targets, err := context.NewArray(0)
	if err != nil {
		return memory.Value{}, err
	}
	if err := defineData(context, this.Ref(), observerTargetsProperty, memory.RefValue(targets), false, false, false); err != nil {
		return memory.Value{}, err
	}
	if err := context.MapSet(realm.bindings.observerCache, resizeObserverCacheKey(id), this); err != nil {
		return memory.Value{}, err
	}
	realm.resizeObservers[id] = &resizeObserverState{ID: id, Callback: handle}
	return memory.UndefinedValue(), nil
}

func (realm *Realm) resizeObserverObserve(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	state, err := realm.resizeObserverState(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	target, err := realm.unwrapNode(context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	for _, observation := range state.Observations {
		if observation.Target == target {
			return memory.UndefinedValue(), nil
		}
	}
	lifetime, err := realm.observerLifetime()
	if err != nil {
		return memory.Value{}, err
	}
	if err := lifetime.RetainNodeObserverTarget(target); err != nil {
		return memory.Value{}, err
	}
	targets, err := observerTargets(context, this)
	if err != nil {
		_ = lifetime.ReleaseNodeObserverTarget(target)
		return memory.Value{}, err
	}
	if err := context.SetArrayElement(targets, uint32(len(state.Observations)), argument(arguments, 0)); err != nil {
		_ = lifetime.ReleaseNodeObserverTarget(target)
		return memory.Value{}, err
	}
	state.Observations = append(state.Observations, resizeObservation{Target: target})
	return memory.UndefinedValue(), nil
}

func (realm *Realm) resizeObserverUnobserve(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	state, err := realm.resizeObserverState(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	target, err := realm.unwrapNode(context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	targets, err := observerTargets(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	for index, observation := range state.Observations {
		if observation.Target != target {
			continue
		}
		for shifted := index; shifted+1 < len(state.Observations); shifted++ {
			value, found, err := context.ArrayElement(targets, uint32(shifted+1))
			if err != nil {
				return memory.Value{}, err
			}
			if !found {
				return memory.Value{}, fmt.Errorf("nativeengine: ResizeObserver %d lost target %d", state.ID, shifted+1)
			}
			if err := context.SetArrayElement(targets, uint32(shifted), value); err != nil {
				return memory.Value{}, err
			}
		}
		if err := context.SetArrayLength(targets, uint32(len(state.Observations)-1)); err != nil {
			return memory.Value{}, err
		}
		copy(state.Observations[index:], state.Observations[index+1:])
		state.Observations[len(state.Observations)-1] = resizeObservation{}
		state.Observations = state.Observations[:len(state.Observations)-1]
		lifetime, err := realm.observerLifetime()
		if err != nil {
			return memory.Value{}, err
		}
		return memory.UndefinedValue(), lifetime.ReleaseNodeObserverTarget(target)
	}
	return memory.UndefinedValue(), nil
}

func (realm *Realm) resizeObserverDisconnect(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, err := realm.resizeObserverState(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	targets, err := observerTargets(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	if err := context.SetArrayLength(targets, 0); err != nil {
		return memory.Value{}, err
	}
	observations := append([]resizeObservation(nil), state.Observations...)
	clear(state.Observations)
	state.Observations = nil
	if len(observations) == 0 {
		return memory.UndefinedValue(), nil
	}
	lifetime, err := realm.observerLifetime()
	if err != nil {
		return memory.Value{}, err
	}
	var releaseErr error
	for _, observation := range observations {
		releaseErr = errors.Join(releaseErr, lifetime.ReleaseNodeObserverTarget(observation.Target))
	}
	return memory.UndefinedValue(), releaseErr
}

func (realm *Realm) resizeObserverState(context *browserruntime.TaskContext, this memory.Value) (*resizeObserverState, error) {
	if !this.IsRef() {
		return nil, fmt.Errorf("%w: invalid ResizeObserver receiver", browserruntime.ErrOperandType)
	}
	name, err := context.NewString(hostRecordProperty)
	if err != nil {
		return nil, err
	}
	value, found, err := context.GetOwnProperty(this.Ref(), name)
	if err != nil || !found || !value.IsRef() {
		return nil, fmt.Errorf("%w: invalid ResizeObserver receiver", browserruntime.ErrOperandType)
	}
	record, err := context.DerefHostObject(value.Ref())
	if err != nil || record.Class != hostClassResizeObserver {
		return nil, fmt.Errorf("%w: invalid ResizeObserver receiver", browserruntime.ErrOperandType)
	}
	state := realm.resizeObservers[record.Identity]
	if state == nil {
		return nil, fmt.Errorf("nativeengine: unknown ResizeObserver %d", record.Identity)
	}
	return state, nil
}

func (realm *Realm) deliverResizeObserversLocked(context *browserruntime.TaskContext) error {
	if len(realm.resizeObservers) == 0 {
		return nil
	}
	host, ok := realm.host.(browser.DOMGeometryHost)
	if !ok {
		return fmt.Errorf("nativeengine: browser host does not expose geometry")
	}
	ids := make([]uint64, 0, len(realm.resizeObservers))
	for id := range realm.resizeObservers {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(left, right int) bool { return ids[left] < ids[right] })
	for cycle := 0; cycle < 100; cycle++ {
		delivered := false
		for _, id := range ids {
			state := realm.resizeObservers[id]
			if state == nil || len(state.Observations) == 0 {
				continue
			}
			entries := make([]memory.Value, 0, len(state.Observations))
			for index := range state.Observations {
				observation := &state.Observations[index]
				geometry, err := host.ElementGeometry(observation.Target)
				if err != nil {
					return fmt.Errorf("observer %d target %#v geometry: %w", state.ID, observation.Target, err)
				}
				width, height := geometry.Rect.Width, geometry.Rect.Height
				if observation.Delivered && observation.Width == width && observation.Height == height {
					continue
				}
				observation.Width, observation.Height, observation.Delivered = width, height, true
				entry, err := realm.newResizeObserverEntry(context, observation.Target, geometry)
				if err != nil {
					return err
				}
				entries = append(entries, entry)
			}
			if len(entries) == 0 {
				continue
			}
			delivered = true
			array, err := context.NewArray(uint32(len(entries)))
			if err != nil {
				return err
			}
			for index, entry := range entries {
				if err := context.SetArrayElement(array, uint32(index), entry); err != nil {
					return err
				}
			}
			observer, found, err := context.MapGet(realm.bindings.observerCache, resizeObserverCacheKey(state.ID))
			if err != nil || !found {
				return fmt.Errorf("nativeengine: ResizeObserver %d lost its wrapper", state.ID)
			}
			if err := realm.invokeCallbackArgumentsLocked(context, state.Callback, false, memory.UndefinedValue(), memory.RefValue(array), observer); err != nil {
				if realm.reportCallbackExceptionLocked(context, "ResizeObserver callback", err) {
					continue
				}
				return err
			}
		}
		if !delivered {
			return nil
		}
	}
	return fmt.Errorf("nativeengine: ResizeObserver delivery did not stabilize")
}

func (realm *Realm) newResizeObserverEntry(context *browserruntime.TaskContext, target browser.NodeHandle, geometry browser.DOMElementGeometry) (memory.Value, error) {
	entry, err := context.NewHeapObject()
	if err != nil {
		return memory.Value{}, err
	}
	targetValue, err := realm.wrappedNodeValue(context, target)
	if err != nil {
		return memory.Value{}, err
	}
	contentWidth, contentHeight := geometry.ClientWidth, geometry.ClientHeight
	if contentWidth == 0 && geometry.Rect.Width != 0 {
		contentWidth = geometry.Rect.Width
	}
	if contentHeight == 0 && geometry.Rect.Height != 0 {
		contentHeight = geometry.Rect.Height
	}
	contentRect, err := realm.newDOMRect(context, browser.DOMRect{Width: contentWidth, Height: contentHeight})
	if err != nil {
		return memory.Value{}, err
	}
	contentSize, err := resizeObserverSizeArray(context, contentWidth, contentHeight)
	if err != nil {
		return memory.Value{}, err
	}
	borderSize, err := resizeObserverSizeArray(context, geometry.Rect.Width, geometry.Rect.Height)
	if err != nil {
		return memory.Value{}, err
	}
	for name, value := range map[string]memory.Value{
		"target": targetValue, "contentRect": contentRect,
		"contentBoxSize": contentSize, "borderBoxSize": borderSize,
		"devicePixelContentBoxSize": contentSize,
	} {
		if err := defineData(context, entry, name, value, false, true, true); err != nil {
			return memory.Value{}, err
		}
	}
	return memory.RefValue(entry), nil
}

func resizeObserverSizeArray(context *browserruntime.TaskContext, width, height float64) (memory.Value, error) {
	size, err := context.NewHeapObject()
	if err != nil {
		return memory.Value{}, err
	}
	if err := defineData(context, size, "inlineSize", memory.NumberValue(width), false, true, true); err != nil {
		return memory.Value{}, err
	}
	if err := defineData(context, size, "blockSize", memory.NumberValue(height), false, true, true); err != nil {
		return memory.Value{}, err
	}
	array, err := context.NewArray(1)
	if err != nil {
		return memory.Value{}, err
	}
	if err := context.SetArrayElement(array, 0, memory.RefValue(size)); err != nil {
		return memory.Value{}, err
	}
	return memory.RefValue(array), nil
}

// IntersectionObserver is deliberately deterministic in Strand today: it
// owns its callback and validates observed DOM nodes, but does not synthesize
// geometry changes. That is enough for applications to install visibility
// sentinels without inventing a viewport event stream the host cannot prove.
func (realm *Realm) intersectionObserverConstructor(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	if !this.IsRef() {
		return memory.Value{}, fmt.Errorf("%w: IntersectionObserver requires new", browserruntime.ErrOperandType)
	}
	callback := argument(arguments, 0)
	if err := requireFunction(context, callback); err != nil {
		return memory.Value{}, err
	}
	if err := defineData(context, this.Ref(), intersectionObserverCallbackProperty, callback, false, false, false); err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), nil
}

func (realm *Realm) intersectionObserverObserve(context *browserruntime.TaskContext, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	if _, err := realm.unwrapNode(context, argument(arguments, 0)); err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), nil
}

func (realm *Realm) intersectionObserverUnobserve(context *browserruntime.TaskContext, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	if _, err := realm.unwrapNode(context, argument(arguments, 0)); err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), nil
}

func (realm *Realm) intersectionObserverDisconnect(_ *browserruntime.TaskContext, _ memory.Value, _ []memory.Value) (memory.Value, error) {
	return memory.UndefinedValue(), nil
}

func (realm *Realm) intersectionObserverTakeRecords(context *browserruntime.TaskContext, _ memory.Value, _ []memory.Value) (memory.Value, error) {
	records, err := context.NewArray(0)
	return memory.RefValue(records), err
}

func (realm *Realm) mutationObserverConstructor(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	if !this.IsRef() {
		return memory.Value{}, fmt.Errorf("%w: MutationObserver requires new", browserruntime.ErrOperandType)
	}
	callback := argument(arguments, 0)
	if err := requireFunction(context, callback); err != nil {
		return memory.Value{}, err
	}
	host, ok := realm.host.(browser.DOMMutationObserverHost)
	if !ok {
		return memory.Value{}, fmt.Errorf("nativeengine: browser host does not expose mutation records")
	}
	sequence, err := host.MutationSequence()
	if err != nil {
		return memory.Value{}, err
	}
	handle, err := realm.retainCallbackLocked(context, callback)
	if err != nil {
		return memory.Value{}, err
	}
	realm.nextMutationObserver++
	if realm.nextMutationObserver == 0 || realm.nextMutationObserver > maxExactInteger {
		return memory.Value{}, fmt.Errorf("nativeengine: MutationObserver identity exhausted")
	}
	id := realm.nextMutationObserver
	metadata, err := realm.documentMetadata()
	if err != nil {
		return memory.Value{}, err
	}
	record, err := context.NewHostObject(memory.HostObject{
		Class: hostClassMutationObserver, Scope: uint64(metadata.Root.Document), Identity: id,
	})
	if err != nil {
		return memory.Value{}, err
	}
	if err := defineData(context, this.Ref(), hostRecordProperty, memory.RefValue(record), false, false, false); err != nil {
		return memory.Value{}, err
	}
	// The wrapper owns its callback in the traced heap. The numeric callback
	// cache is temporarily evacuated at GC checkpoints, so an unreachable
	// observer cannot keep its lexical environment alive forever.
	if err := defineData(context, this.Ref(), mutationObserverCallbackProperty, callback, false, false, false); err != nil {
		return memory.Value{}, err
	}
	targets, err := context.NewArray(0)
	if err != nil {
		return memory.Value{}, err
	}
	if err := defineData(context, this.Ref(), observerTargetsProperty, memory.RefValue(targets), false, false, false); err != nil {
		return memory.Value{}, err
	}
	if err := context.MapSet(realm.bindings.observerCache, memory.NumberValue(float64(id)), this); err != nil {
		return memory.Value{}, err
	}
	realm.mutationObservers[id] = &mutationObserverState{ID: id, Callback: handle, Sequence: sequence}
	return memory.UndefinedValue(), nil
}

func (realm *Realm) mutationObserverObserve(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	state, err := realm.mutationObserverState(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	target, err := realm.unwrapNode(context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	options := argument(arguments, 1)
	if !options.IsRef() {
		return memory.Value{}, fmt.Errorf("%w: MutationObserver options must be an object", browserruntime.ErrOperandType)
	}
	state.ChildList, err = objectBool(context, options.Ref(), "childList")
	if err != nil {
		return memory.Value{}, err
	}
	state.Attributes, err = objectBool(context, options.Ref(), "attributes")
	if err != nil {
		return memory.Value{}, err
	}
	state.CharacterData, err = objectBool(context, options.Ref(), "characterData")
	if err != nil {
		return memory.Value{}, err
	}
	state.Subtree, err = objectBool(context, options.Ref(), "subtree")
	if err != nil {
		return memory.Value{}, err
	}
	state.AttributeOldValue, err = objectBool(context, options.Ref(), "attributeOldValue")
	if err != nil {
		return memory.Value{}, err
	}
	state.CharacterOldValue, err = objectBool(context, options.Ref(), "characterDataOldValue")
	if err != nil {
		return memory.Value{}, err
	}
	if state.AttributeOldValue {
		state.Attributes = true
	}
	if state.CharacterOldValue {
		state.CharacterData = true
	}
	if !state.ChildList && !state.Attributes && !state.CharacterData {
		return memory.Value{}, fmt.Errorf("%w: MutationObserver options select no mutation type", browserruntime.ErrOperandType)
	}
	host := realm.host.(browser.DOMMutationObserverHost)
	sequence, err := host.MutationSequence()
	if err != nil {
		return memory.Value{}, err
	}
	previous := state.Target
	var lifetime browser.NodeObserverLifetimeHost
	if target != previous {
		lifetime, err = realm.observerLifetime()
		if err != nil {
			return memory.Value{}, err
		}
		if err := lifetime.RetainNodeObserverTarget(target); err != nil {
			return memory.Value{}, err
		}
	}
	targets, err := observerTargets(context, this)
	if err != nil {
		if lifetime != nil {
			_ = lifetime.ReleaseNodeObserverTarget(target)
		}
		return memory.Value{}, err
	}
	if err := context.SetArrayElement(targets, 0, argument(arguments, 0)); err != nil {
		if lifetime != nil {
			_ = lifetime.ReleaseNodeObserverTarget(target)
		}
		return memory.Value{}, err
	}
	if err := context.SetArrayLength(targets, 1); err != nil {
		if lifetime != nil {
			_ = lifetime.ReleaseNodeObserverTarget(target)
		}
		return memory.Value{}, err
	}
	state.Target = target
	state.Sequence = sequence
	if lifetime != nil && previous != (browser.NodeHandle{}) {
		if err := lifetime.ReleaseNodeObserverTarget(previous); err != nil {
			return memory.Value{}, err
		}
	}
	return memory.UndefinedValue(), nil
}

func (realm *Realm) mutationObserverDisconnect(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, err := realm.mutationObserverState(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	targets, err := observerTargets(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	if err := context.SetArrayLength(targets, 0); err != nil {
		return memory.Value{}, err
	}
	target := state.Target
	state.Target = browser.NodeHandle{}
	var sequenceErr error
	if host, ok := realm.host.(browser.DOMMutationObserverHost); ok {
		state.Sequence, sequenceErr = host.MutationSequence()
	}
	var releaseErr error
	if target != (browser.NodeHandle{}) {
		lifetime, lifetimeErr := realm.observerLifetime()
		if lifetimeErr != nil {
			releaseErr = lifetimeErr
		} else {
			releaseErr = lifetime.ReleaseNodeObserverTarget(target)
		}
	}
	return memory.UndefinedValue(), errors.Join(sequenceErr, releaseErr)
}

func (realm *Realm) observerLifetime() (browser.NodeObserverLifetimeHost, error) {
	lifetime, ok := realm.host.(browser.NodeObserverLifetimeHost)
	if !ok {
		return nil, fmt.Errorf("nativeengine: browser host does not expose observer target lifetimes")
	}
	return lifetime, nil
}

func observerTargets(context *browserruntime.TaskContext, observer memory.Value) (memory.Ref, error) {
	if !observer.IsRef() {
		return memory.Ref{}, fmt.Errorf("%w: invalid observer receiver", browserruntime.ErrOperandType)
	}
	name, err := context.NewString(observerTargetsProperty)
	if err != nil {
		return memory.Ref{}, err
	}
	value, found, err := context.GetOwnProperty(observer.Ref(), name)
	if err != nil {
		return memory.Ref{}, err
	}
	if !found || !value.IsRef() {
		return memory.Ref{}, fmt.Errorf("nativeengine: observer lost its target roots")
	}
	if _, err := context.DerefArray(value.Ref()); err != nil {
		return memory.Ref{}, fmt.Errorf("nativeengine: observer target roots are invalid: %w", err)
	}
	return value.Ref(), nil
}

func (realm *Realm) mutationObserverTakeRecords(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, err := realm.mutationObserverState(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	records, err := realm.takeMutationRecordsLocked(state)
	if err != nil {
		return memory.Value{}, err
	}
	return realm.mutationRecordArray(context, state.Target.Document, records)
}

func (realm *Realm) mutationObserverState(context *browserruntime.TaskContext, this memory.Value) (*mutationObserverState, error) {
	if !this.IsRef() {
		return nil, fmt.Errorf("%w: invalid MutationObserver receiver", browserruntime.ErrOperandType)
	}
	name, err := context.NewString(hostRecordProperty)
	if err != nil {
		return nil, err
	}
	value, found, err := context.GetOwnProperty(this.Ref(), name)
	if err != nil || !found || !value.IsRef() {
		return nil, fmt.Errorf("%w: invalid MutationObserver receiver", browserruntime.ErrOperandType)
	}
	record, err := context.DerefHostObject(value.Ref())
	if err != nil || record.Class != hostClassMutationObserver {
		return nil, fmt.Errorf("%w: invalid MutationObserver receiver", browserruntime.ErrOperandType)
	}
	state := realm.mutationObservers[record.Identity]
	if state == nil {
		return nil, fmt.Errorf("nativeengine: unknown MutationObserver %d", record.Identity)
	}
	return state, nil
}

func (realm *Realm) deliverMutationObserversLocked(context *browserruntime.TaskContext) error {
	for cycle := 0; cycle < 100; cycle++ {
		delivered := false
		for _, state := range realm.mutationObservers {
			if state.Target == (browser.NodeHandle{}) {
				continue
			}
			records, err := realm.takeMutationRecordsLocked(state)
			if err != nil {
				return fmt.Errorf("observer %d target %#v records: %w", state.ID, state.Target, err)
			}
			if len(records) == 0 {
				continue
			}
			delivered = true
			array, err := realm.mutationRecordArray(context, state.Target.Document, records)
			if err != nil {
				return err
			}
			observer, found, err := context.MapGet(realm.bindings.observerCache, memory.NumberValue(float64(state.ID)))
			if err != nil || !found {
				return fmt.Errorf("nativeengine: MutationObserver %d lost its wrapper", state.ID)
			}
			if err := realm.invokeCallbackArgumentsLocked(context, state.Callback, false, memory.UndefinedValue(), array, observer); err != nil {
				if realm.reportCallbackExceptionLocked(context, "MutationObserver callback", err) {
					continue
				}
				return err
			}
		}
		if !delivered {
			return nil
		}
	}
	return fmt.Errorf("nativeengine: MutationObserver delivery did not stabilize")
}

// reportCallbackExceptionLocked implements browser callback exception
// boundaries: JavaScript exceptions are reported and delivery continues,
// while native runtime/storage failures still abort the checkpoint.
func (realm *Realm) reportCallbackExceptionLocked(context *browserruntime.TaskContext, source string, err error) bool {
	value, thrown := browserruntime.ThrownValue(err)
	if !thrown {
		return false
	}
	realm.reportedExceptions++
	if realm.consoleSink != nil {
		realm.consoleSink(ConsoleMessage{
			Method:    "error",
			Arguments: []string{source + ": " + consoleValue(context, value)},
		})
	}
	return true
}

func (realm *Realm) takeMutationRecordsLocked(state *mutationObserverState) ([]dom.MutationRecord, error) {
	host, ok := realm.host.(browser.DOMMutationObserverHost)
	if !ok {
		return nil, fmt.Errorf("nativeengine: browser host does not expose mutation records")
	}
	records, latest, err := host.MutationRecordsSince(state.Sequence)
	if err != nil {
		return nil, err
	}
	state.Sequence = latest
	if state.Target == (browser.NodeHandle{}) {
		return nil, nil
	}
	elementHost, err := realm.elementHost()
	if err != nil {
		return nil, err
	}
	targetMetadata, err := elementHost.NodeMetadata(state.Target)
	if err != nil {
		return nil, err
	}
	result := make([]dom.MutationRecord, 0, len(records))
	for _, record := range records {
		target := browser.NodeHandle{Document: state.Target.Document, Node: record.Target}
		matches := target == state.Target
		if !matches && state.Subtree {
			recordMetadata, metadataErr := elementHost.NodeMetadata(target)
			if errors.Is(metadataErr, dom.ErrUnknownNode) {
				// The document-wide journal may outlive an unrelated detached
				// component. Such a record cannot be delivered with a valid Node
				// wrapper and is outside this live observer target's current tree.
				continue
			}
			if metadataErr != nil {
				return nil, metadataErr
			}
			if recordMetadata.Connected != targetMetadata.Connected {
				continue
			}
			matches, err = elementHost.Contains(state.Target, target)
			if err != nil {
				return nil, err
			}
		}
		if !matches {
			continue
		}
		switch record.Type {
		case dom.MutationChildList:
			matches = state.ChildList
		case dom.MutationAttributes:
			matches = state.Attributes
		case dom.MutationCharacterData:
			matches = state.CharacterData
		}
		if matches {
			record.AddedNodes, err = liveMutationNodeIDs(elementHost, state.Target.Document, record.AddedNodes)
			if err != nil {
				return nil, err
			}
			record.RemovedNodes, err = liveMutationNodeIDs(elementHost, state.Target.Document, record.RemovedNodes)
			if err != nil {
				return nil, err
			}
			result = append(result, record)
		}
	}
	return result, nil
}

func liveMutationNodeIDs(host browser.DOMElementHost, generation browser.DocumentGeneration, ids []dom.NodeID) ([]dom.NodeID, error) {
	live := ids[:0]
	for _, id := range ids {
		_, err := host.NodeMetadata(browser.NodeHandle{Document: generation, Node: id})
		if errors.Is(err, dom.ErrUnknownNode) {
			continue
		}
		if err != nil {
			return nil, err
		}
		live = append(live, id)
	}
	return live, nil
}

func (realm *Realm) mutationRecordArray(context *browserruntime.TaskContext, generation browser.DocumentGeneration, records []dom.MutationRecord) (memory.Value, error) {
	array, err := context.NewArray(uint32(len(records)))
	if err != nil {
		return memory.Value{}, err
	}
	for index, record := range records {
		object, err := context.NewHeapObject()
		if err != nil {
			return memory.Value{}, err
		}
		typeName := "childList"
		if record.Type == dom.MutationAttributes {
			typeName = "attributes"
		} else if record.Type == dom.MutationCharacterData {
			typeName = "characterData"
		}
		typeValue, err := newString(context, typeName)
		if err != nil {
			return memory.Value{}, err
		}
		target, err := realm.wrappedNodeValue(context, browser.NodeHandle{Document: generation, Node: record.Target})
		if err != nil {
			return memory.Value{}, err
		}
		added, err := realm.mutationNodeArray(context, generation, record.AddedNodes)
		if err != nil {
			return memory.Value{}, err
		}
		removed, err := realm.mutationNodeArray(context, generation, record.RemovedNodes)
		if err != nil {
			return memory.Value{}, err
		}
		attributeName := memory.NullValue()
		if record.Type == dom.MutationAttributes {
			attributeName, err = newString(context, record.AttributeName)
			if err != nil {
				return memory.Value{}, err
			}
		}
		oldValue := memory.NullValue()
		if record.OldValuePresent {
			oldValue, err = newString(context, record.OldValue)
			if err != nil {
				return memory.Value{}, err
			}
		}
		for name, value := range map[string]memory.Value{
			"type": typeValue, "target": target, "addedNodes": added,
			"removedNodes": removed, "attributeName": attributeName, "oldValue": oldValue,
		} {
			if err := defineData(context, object, name, value, false, true, true); err != nil {
				return memory.Value{}, err
			}
		}
		if err := context.SetArrayElement(array, uint32(index), memory.RefValue(object)); err != nil {
			return memory.Value{}, err
		}
	}
	return memory.RefValue(array), nil
}

func (realm *Realm) mutationNodeArray(context *browserruntime.TaskContext, generation browser.DocumentGeneration, ids []dom.NodeID) (memory.Value, error) {
	handles := make([]browser.NodeHandle, len(ids))
	for index, id := range ids {
		handles[index] = browser.NodeHandle{Document: generation, Node: id}
	}
	return realm.nodeArray(context, handles)
}

func objectBool(context *browserruntime.TaskContext, object memory.Ref, name string) (bool, error) {
	nameRef, err := context.NewString(name)
	if err != nil {
		return false, err
	}
	value, found, err := context.GetOwnProperty(object, nameRef)
	if err != nil || !found {
		return false, err
	}
	return truthy(value), nil
}
