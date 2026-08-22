package nativeengine

import (
	"fmt"

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
)

const mutationObserverCallbackProperty = "\x00gossamer.mutation-observer.callback"
const intersectionObserverCallbackProperty = "\x00gossamer.intersection-observer.callback"

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
	state.Target = target
	state.Sequence = sequence
	return memory.UndefinedValue(), nil
}

func (realm *Realm) mutationObserverDisconnect(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, err := realm.mutationObserverState(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	state.Target = browser.NodeHandle{}
	if host, ok := realm.host.(browser.DOMMutationObserverHost); ok {
		state.Sequence, err = host.MutationSequence()
	}
	return memory.UndefinedValue(), err
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
				return err
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
				return err
			}
		}
		if !delivered {
			return nil
		}
	}
	return fmt.Errorf("nativeengine: MutationObserver delivery did not stabilize")
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
	result := make([]dom.MutationRecord, 0, len(records))
	for _, record := range records {
		target := browser.NodeHandle{Document: state.Target.Document, Node: record.Target}
		matches := target == state.Target
		if !matches && state.Subtree {
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
			result = append(result, record)
		}
	}
	return result, nil
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
