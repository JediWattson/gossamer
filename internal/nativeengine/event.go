package nativeengine

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

const (
	nativeEventTargetAdd uint64 = 12_000 + iota
	nativeEventTargetRemove
	nativeEventTargetDispatch
	nativeEventConstructor
	nativeCustomEventConstructor
	nativeEventPreventDefault
	nativeEventStopPropagation
	nativeEventStopImmediatePropagation
)

const eventBrandProperty = "\x00gossamer.event"

const (
	eventPhaseNone      = 0
	eventPhaseCapturing = 1
	eventPhaseTarget    = 2
	eventPhaseBubbling  = 3
)

type eventTargetID struct {
	Window   bool
	Document browser.DocumentGeneration
	Node     dom.NodeID
}

func nodeEventTarget(handle browser.NodeHandle) eventTargetID {
	return eventTargetID{Document: handle.Document, Node: handle.Node}
}

func windowEventTarget(document browser.DocumentGeneration) eventTargetID {
	return eventTargetID{Window: true, Document: document}
}

func (target eventTargetID) nodeHandle() browser.NodeHandle {
	return browser.NodeHandle{Document: target.Document, Node: target.Node}
}

type eventListenerKey struct {
	Target eventTargetID
	Type   string
}

type eventListener struct {
	Handle  browser.ValueHandle
	Capture bool
	Once    bool
	Passive bool
}

type eventState struct {
	object             memory.Ref
	defaultPrevented   bool
	propagationStopped bool
	immediateStopped   bool
	passive            bool
	cancelable         bool
}

type eventListenerOptions struct {
	capture bool
	once    bool
	passive bool
}

func (realm *Realm) newEventConstructor(
	context *browserruntime.TaskContext,
	name string,
	nativeID uint64,
	parentPrototype memory.Ref,
) (memory.Ref, memory.Ref, error) {
	nameValue, err := newString(context, name)
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	constructor, err := context.NewNativeConstructor(nameValue, memory.RefValue(realm.active.Global), 1, nativeID)
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	prototypeName, err := context.NewString("prototype")
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	prototype, found, err := context.GetOwnProperty(constructor, prototypeName)
	if err != nil || !found || !prototype.IsRef() {
		return memory.Ref{}, memory.Ref{}, fmt.Errorf("nativeengine: %s constructor lost its prototype", name)
	}
	if parentPrototype != (memory.Ref{}) {
		if err := context.SetPrototype(prototype.Ref(), memory.RefValue(parentPrototype)); err != nil {
			return memory.Ref{}, memory.Ref{}, err
		}
	}
	if name == "Event" {
		for constant, value := range map[string]float64{
			"NONE": eventPhaseNone, "CAPTURING_PHASE": eventPhaseCapturing,
			"AT_TARGET": eventPhaseTarget, "BUBBLING_PHASE": eventPhaseBubbling,
		} {
			if err := defineData(context, constructor, constant, memory.NumberValue(value), false, false, false); err != nil {
				return memory.Ref{}, memory.Ref{}, err
			}
			if err := defineData(context, prototype.Ref(), constant, memory.NumberValue(value), false, false, false); err != nil {
				return memory.Ref{}, memory.Ref{}, err
			}
		}
	}
	return constructor, prototype.Ref(), nil
}

func (realm *Realm) eventConstructor(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	return realm.initializeConstructedEvent(context, this, arguments, false)
}

func (realm *Realm) customEventConstructor(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	return realm.initializeConstructedEvent(context, this, arguments, true)
}

func (realm *Realm) initializeConstructedEvent(
	context *browserruntime.TaskContext,
	this memory.Value,
	arguments []memory.Value,
	custom bool,
) (memory.Value, error) {
	if !this.IsRef() || len(arguments) == 0 {
		return memory.Value{}, fmt.Errorf("%w: Event constructor requires new and a type", browserruntime.ErrOperandType)
	}
	eventType, err := stringArgument(context, arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	bubbles, cancelable, composed, detail, err := eventInit(context, argument(arguments, 1))
	if err != nil {
		return memory.Value{}, err
	}
	typeValue, err := newString(context, eventType)
	if err != nil {
		return memory.Value{}, err
	}
	properties := []struct {
		name  string
		value memory.Value
	}{
		{"type", typeValue},
		{"target", memory.NullValue()},
		{"currentTarget", memory.NullValue()},
		{"eventPhase", memory.NumberValue(eventPhaseNone)},
		{"bubbles", memory.BoolValue(bubbles)},
		{"cancelable", memory.BoolValue(cancelable)},
		{"composed", memory.BoolValue(composed)},
		{"defaultPrevented", memory.BoolValue(false)},
		{"isTrusted", memory.BoolValue(false)},
		{"timeStamp", memory.NumberValue(0)},
	}
	if custom {
		properties = append(properties, struct {
			name  string
			value memory.Value
		}{"detail", detail})
	}
	for _, property := range properties {
		if err := defineData(context, this.Ref(), property.name, property.value, true, true, true); err != nil {
			return memory.Value{}, err
		}
	}
	if err := defineData(context, this.Ref(), eventBrandProperty, memory.BoolValue(true), false, false, false); err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), nil
}

func eventInit(context *browserruntime.TaskContext, value memory.Value) (bool, bool, bool, memory.Value, error) {
	detail := memory.NullValue()
	if !value.IsRef() {
		return false, false, false, detail, nil
	}
	kind, err := context.HeapKind(value.Ref())
	if err != nil {
		return false, false, false, memory.Value{}, err
	}
	if kind != memory.HeapObject {
		return false, false, false, detail, nil
	}
	var bubbles, cancelable, composed bool
	for _, option := range []struct {
		name        string
		destination *bool
	}{
		{"bubbles", &bubbles},
		{"cancelable", &cancelable},
		{"composed", &composed},
	} {
		nameRef, err := context.NewString(option.name)
		if err != nil {
			return false, false, false, memory.Value{}, err
		}
		property, found, err := context.GetOwnProperty(value.Ref(), nameRef)
		if err != nil {
			return false, false, false, memory.Value{}, err
		}
		if found {
			*option.destination = truthy(property)
		}
	}
	detailName, err := context.NewString("detail")
	if err != nil {
		return false, false, false, memory.Value{}, err
	}
	if property, found, err := context.GetOwnProperty(value.Ref(), detailName); err != nil {
		return false, false, false, memory.Value{}, err
	} else if found {
		detail = property
	}
	return bubbles, cancelable, composed, detail, nil
}

func (realm *Realm) eventTargetAdd(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	eventType, err := stringArgument(context, arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	callback := argument(arguments, 1)
	if callback.Kind() == memory.ValueNull || callback.Kind() == memory.ValueUndefined {
		return memory.UndefinedValue(), nil
	}
	if err := requireFunction(context, callback); err != nil {
		return memory.Value{}, err
	}
	target, err := realm.eventTarget(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	options, err := listenerOptions(context, argument(arguments, 2))
	if err != nil {
		return memory.Value{}, err
	}
	key := eventListenerKey{Target: target, Type: eventType}
	for _, listener := range realm.listeners[key] {
		if listener.Capture != options.capture {
			continue
		}
		retained, found, lookupErr := realm.callbackLocked(context, listener.Handle)
		if lookupErr != nil {
			return memory.Value{}, lookupErr
		}
		if found && retained == callback {
			return memory.UndefinedValue(), nil
		}
	}
	handle, err := realm.retainCallbackLocked(context, callback)
	if err != nil {
		return memory.Value{}, err
	}
	if err := defineData(context, this.Ref(), eventListenerCallbackProperty(handle), callback, false, false, true); err != nil {
		_, _ = context.MapDelete(realm.bindings.callbackCache, memory.NumberValue(float64(handle)))
		return memory.Value{}, err
	}
	if !target.Window && realm.listenerTargets[target] == 0 {
		lifetime, ok := realm.host.(browser.NodeEventListenerLifetimeHost)
		if !ok {
			_, _ = context.MapDelete(realm.bindings.callbackCache, memory.NumberValue(float64(handle)))
			_ = realm.deleteEventListenerCallbackProperty(context, this.Ref(), handle)
			return memory.Value{}, fmt.Errorf("nativeengine: browser host does not expose event listener lifetimes")
		}
		if err := lifetime.RetainNodeEventTarget(target.nodeHandle()); err != nil {
			_, _ = context.MapDelete(realm.bindings.callbackCache, memory.NumberValue(float64(handle)))
			_ = realm.deleteEventListenerCallbackProperty(context, this.Ref(), handle)
			return memory.Value{}, err
		}
	}
	realm.listeners[key] = append(realm.listeners[key], eventListener{
		Handle: handle, Capture: options.capture, Once: options.once, Passive: options.passive,
	})
	realm.listenerTargets[target]++
	return memory.UndefinedValue(), nil
}

func (realm *Realm) eventTargetRemove(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	eventType, err := stringArgument(context, arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	callback := argument(arguments, 1)
	if callback.Kind() == memory.ValueNull || callback.Kind() == memory.ValueUndefined {
		return memory.UndefinedValue(), nil
	}
	if err := requireFunction(context, callback); err != nil {
		return memory.UndefinedValue(), nil
	}
	target, err := realm.eventTarget(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	options, err := listenerOptions(context, argument(arguments, 2))
	if err != nil {
		return memory.Value{}, err
	}
	key := eventListenerKey{Target: target, Type: eventType}
	for _, listener := range realm.listeners[key] {
		if listener.Capture != options.capture {
			continue
		}
		retained, found, lookupErr := realm.callbackLocked(context, listener.Handle)
		if lookupErr != nil {
			return memory.Value{}, lookupErr
		}
		if found && retained == callback {
			return memory.UndefinedValue(), realm.removeEventListenerLocked(context, key, listener.Handle)
		}
	}
	return memory.UndefinedValue(), nil
}

func (realm *Realm) eventPreventDefault(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	event, err := realm.requireEventObject(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	state := realm.activeEvent
	if state != nil && state.object == event {
		if state.cancelable && !state.passive {
			state.defaultPrevented = true
			if err := setDataValue(context, event, "defaultPrevented", memory.BoolValue(true)); err != nil {
				return memory.Value{}, err
			}
		}
		return memory.UndefinedValue(), nil
	}
	cancelable, err := eventBool(context, event, "cancelable")
	if err != nil {
		return memory.Value{}, err
	}
	if cancelable {
		if err := setDataValue(context, event, "defaultPrevented", memory.BoolValue(true)); err != nil {
			return memory.Value{}, err
		}
	}
	return memory.UndefinedValue(), nil
}

func (realm *Realm) eventStopPropagation(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	event, err := realm.requireEventObject(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	if state := realm.activeEvent; state != nil && state.object == event {
		state.propagationStopped = true
	}
	return memory.UndefinedValue(), nil
}

func (realm *Realm) eventStopImmediatePropagation(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	event, err := realm.requireEventObject(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	if state := realm.activeEvent; state != nil && state.object == event {
		state.propagationStopped = true
		state.immediateStopped = true
	}
	return memory.UndefinedValue(), nil
}

func (realm *Realm) requireEventObject(context *browserruntime.TaskContext, value memory.Value) (memory.Ref, error) {
	if !value.IsRef() {
		return memory.Ref{}, fmt.Errorf("%w: Event method called with an invalid receiver", browserruntime.ErrOperandType)
	}
	brand, found, err := ownProperty(context, value.Ref(), eventBrandProperty)
	if err != nil || !found || brand.Kind() != memory.ValueBool || !brand.Bool() {
		return memory.Ref{}, fmt.Errorf("%w: Event method called with an invalid receiver", browserruntime.ErrOperandType)
	}
	return value.Ref(), nil
}

func (realm *Realm) dispatchEventLocked(context *browserruntime.TaskContext, input browser.InputEvent) (browser.EventDispatchResult, error) {
	if input.Type.String() == "" || input.Target.Document == 0 {
		return browser.EventDispatchResult{}, fmt.Errorf("nativeengine: invalid input event")
	}
	event, err := realm.newInputEvent(context, input)
	if err != nil {
		return browser.EventDispatchResult{}, err
	}
	target := nodeEventTarget(input.Target)
	if input.Target.Node == dom.InvalidNodeID {
		target = windowEventTarget(input.Target.Document)
	}
	prevented, err := realm.dispatchEventObjectLocked(
		context, target, event, input.Type.String(), inputEventBubbles(input.Type), inputEventCancelable(input.Type),
	)
	return browser.EventDispatchResult{DefaultPrevented: prevented}, err
}

func (realm *Realm) eventTargetDispatch(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	target, err := realm.eventTarget(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	event, err := realm.requireEventObject(context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	if realm.activeEvent != nil && realm.activeEvent.object == event {
		return memory.Value{}, fmt.Errorf("%w: Event is already being dispatched", browserruntime.ErrOperandType)
	}
	typeValue, found, err := ownProperty(context, event, "type")
	if err != nil || !found || !typeValue.IsRef() {
		return memory.Value{}, fmt.Errorf("%w: Event has no type", browserruntime.ErrOperandType)
	}
	eventType, err := context.DerefString(typeValue.Ref())
	if err != nil {
		return memory.Value{}, err
	}
	bubbles, err := eventBool(context, event, "bubbles")
	if err != nil {
		return memory.Value{}, err
	}
	cancelable, err := eventBool(context, event, "cancelable")
	if err != nil {
		return memory.Value{}, err
	}
	prevented, err := realm.dispatchEventObjectLocked(context, target, event, eventType, bubbles, cancelable)
	if err != nil {
		return memory.Value{}, err
	}
	return memory.BoolValue(!prevented), nil
}

func (realm *Realm) dispatchEventObjectLocked(
	context *browserruntime.TaskContext,
	target eventTargetID,
	event memory.Ref,
	eventType string,
	bubbles bool,
	cancelable bool,
) (bool, error) {
	var path []eventTargetID
	if target.Window {
		path = []eventTargetID{target}
	} else {
		var err error
		path, err = realm.eventPath(context, target.nodeHandle())
		if err != nil {
			return false, err
		}
	}
	targetValue, err := realm.eventTargetValue(context, target)
	if err != nil {
		return false, err
	}
	if err := setDataValue(context, event, "target", targetValue); err != nil {
		return false, err
	}
	defaultPrevented, err := eventBool(context, event, "defaultPrevented")
	if err != nil {
		return false, err
	}
	state := &eventState{object: event, cancelable: cancelable, defaultPrevented: defaultPrevented}
	previous := realm.activeEvent
	realm.activeEvent = state
	defer func() { realm.activeEvent = previous }()

	var dispatchErr error
	reachedTarget := true
	for index := len(path) - 1; index >= 1; index-- {
		dispatchErr = errors.Join(dispatchErr, realm.invokeEventTargetLocked(context, path[index], eventType, event, eventPhaseCapturing, true))
		if state.propagationStopped {
			reachedTarget = false
			break
		}
	}
	if reachedTarget {
		dispatchErr = errors.Join(dispatchErr, realm.invokeEventTargetLocked(context, path[0], eventType, event, eventPhaseTarget, true))
		if !state.immediateStopped {
			dispatchErr = errors.Join(dispatchErr, realm.invokeEventTargetLocked(context, path[0], eventType, event, eventPhaseTarget, false))
		}
		if bubbles && !state.propagationStopped {
			for index := 1; index < len(path); index++ {
				dispatchErr = errors.Join(dispatchErr, realm.invokeEventTargetLocked(context, path[index], eventType, event, eventPhaseBubbling, false))
				if state.propagationStopped {
					break
				}
			}
		}
	}
	state.passive = false
	if err := setDataValue(context, event, "currentTarget", memory.NullValue()); err != nil {
		dispatchErr = errors.Join(dispatchErr, err)
	}
	if err := setDataValue(context, event, "eventPhase", memory.NumberValue(eventPhaseNone)); err != nil {
		dispatchErr = errors.Join(dispatchErr, err)
	}
	return state.defaultPrevented, dispatchErr
}

func ownProperty(context *browserruntime.TaskContext, object memory.Ref, name string) (memory.Value, bool, error) {
	nameRef, err := context.NewString(name)
	if err != nil {
		return memory.Value{}, false, err
	}
	return context.GetOwnProperty(object, nameRef)
}

func eventBool(context *browserruntime.TaskContext, event memory.Ref, name string) (bool, error) {
	value, found, err := ownProperty(context, event, name)
	if err != nil {
		return false, err
	}
	return found && truthy(value), nil
}

func (realm *Realm) eventPath(context *browserruntime.TaskContext, target browser.NodeHandle) ([]eventTargetID, error) {
	host, ok := realm.host.(browser.DOMElementHost)
	if !ok {
		return nil, fmt.Errorf("nativeengine: browser host does not expose event traversal")
	}
	path := []eventTargetID{nodeEventTarget(target)}
	cursor := target
	for {
		parent, found, err := host.RelatedNode(cursor, browser.RelationParentNode)
		if err != nil {
			return nil, err
		}
		if !found {
			break
		}
		path = append(path, nodeEventTarget(parent))
		cursor = parent
	}
	documentHost, ok := realm.host.(browser.DOMDocumentHost)
	if !ok {
		return nil, fmt.Errorf("nativeengine: browser host does not expose document metadata")
	}
	metadata, err := documentHost.DocumentMetadata()
	if err != nil {
		return nil, err
	}
	if path[len(path)-1] == nodeEventTarget(metadata.Root) {
		path = append(path, windowEventTarget(metadata.Root.Document))
	}
	return path, nil
}

func (realm *Realm) invokeEventTargetLocked(
	context *browserruntime.TaskContext,
	target eventTargetID,
	eventType string,
	event memory.Ref,
	phase int,
	capture bool,
) error {
	state := realm.activeEvent
	if state == nil {
		return fmt.Errorf("nativeengine: event dispatch state is unavailable")
	}
	targetValue, err := realm.eventTargetValue(context, target)
	if err != nil {
		return err
	}
	if err := setDataValue(context, event, "currentTarget", targetValue); err != nil {
		return err
	}
	if err := setDataValue(context, event, "eventPhase", memory.NumberValue(float64(phase))); err != nil {
		return err
	}
	key := eventListenerKey{Target: target, Type: eventType}
	snapshot := append([]eventListener(nil), realm.listeners[key]...)
	var result error
	for _, candidate := range snapshot {
		listener, found := realm.liveEventListener(key, candidate.Handle)
		if !found || listener.Capture != capture {
			continue
		}
		callback, found, lookupErr := realm.callbackLocked(context, listener.Handle)
		if lookupErr != nil {
			result = errors.Join(result, lookupErr)
			continue
		}
		if !found || !callback.IsRef() {
			continue
		}
		if listener.Once {
			if removeErr := realm.removeEventListenerLocked(context, key, listener.Handle); removeErr != nil {
				result = errors.Join(result, removeErr)
				continue
			}
		}
		state.passive = listener.Passive
		_, callErr := realm.interpreter.CallWithoutCheckpoint(context, callback.Ref(), targetValue, memory.RefValue(event))
		state.passive = false
		result = errors.Join(result, callErr)
		if state.immediateStopped {
			break
		}
	}
	return result
}

func (realm *Realm) liveEventListener(key eventListenerKey, handle browser.ValueHandle) (eventListener, bool) {
	for _, listener := range realm.listeners[key] {
		if listener.Handle == handle {
			return listener, true
		}
	}
	return eventListener{}, false
}

func (realm *Realm) removeEventListenerLocked(context *browserruntime.TaskContext, key eventListenerKey, handle browser.ValueHandle) error {
	listeners := realm.listeners[key]
	index := -1
	for candidate, listener := range listeners {
		if listener.Handle == handle {
			index = candidate
			break
		}
	}
	if index < 0 {
		return nil
	}
	copy(listeners[index:], listeners[index+1:])
	listeners[len(listeners)-1] = eventListener{}
	listeners = listeners[:len(listeners)-1]
	if len(listeners) == 0 {
		delete(realm.listeners, key)
	} else {
		realm.listeners[key] = listeners
	}
	if _, err := context.MapDelete(realm.bindings.callbackCache, memory.NumberValue(float64(handle))); err != nil {
		return err
	}
	targetValue, err := realm.eventTargetValue(context, key.Target)
	if err != nil {
		return err
	}
	if err := realm.deleteEventListenerCallbackProperty(context, targetValue.Ref(), handle); err != nil {
		return err
	}
	count := realm.listenerTargets[key.Target]
	if count <= 1 {
		delete(realm.listenerTargets, key.Target)
		if !key.Target.Window {
			lifetime, ok := realm.host.(browser.NodeEventListenerLifetimeHost)
			if !ok {
				return fmt.Errorf("nativeengine: browser host does not expose event listener lifetimes")
			}
			return lifetime.ReleaseNodeEventTarget(key.Target.nodeHandle())
		}
	} else {
		realm.listenerTargets[key.Target] = count - 1
	}
	return nil
}

func eventListenerCallbackProperty(handle browser.ValueHandle) string {
	return "\x00gossamer.event-listener." + strconv.FormatUint(uint64(handle), 10)
}

func (realm *Realm) deleteEventListenerCallbackProperty(context *browserruntime.TaskContext, target memory.Ref, handle browser.ValueHandle) error {
	name, err := context.NewString(eventListenerCallbackProperty(handle))
	if err != nil {
		return err
	}
	_, err = context.DeleteProperty(target, name)
	return err
}

func (realm *Realm) callbackLocked(context *browserruntime.TaskContext, handle browser.ValueHandle) (memory.Value, bool, error) {
	if handle == 0 || realm.bindings == nil {
		return memory.Value{}, false, nil
	}
	return context.MapGet(realm.bindings.callbackCache, memory.NumberValue(float64(handle)))
}

func (realm *Realm) eventTarget(context *browserruntime.TaskContext, value memory.Value) (eventTargetID, error) {
	if value.IsRef() && realm.bindings != nil && value.Ref() == realm.bindings.window {
		documentHost, ok := realm.host.(browser.DOMDocumentHost)
		if !ok {
			return eventTargetID{}, fmt.Errorf("nativeengine: browser host does not expose document metadata")
		}
		metadata, err := documentHost.DocumentMetadata()
		if err != nil {
			return eventTargetID{}, err
		}
		return windowEventTarget(metadata.Root.Document), nil
	}
	handle, err := realm.unwrapNode(context, value)
	if err != nil {
		return eventTargetID{}, err
	}
	return nodeEventTarget(handle), nil
}

func (realm *Realm) eventTargetValue(context *browserruntime.TaskContext, target eventTargetID) (memory.Value, error) {
	if target.Window {
		return memory.RefValue(realm.bindings.window), nil
	}
	return realm.wrappedNodeValue(context, target.nodeHandle())
}

func (realm *Realm) newInputEvent(context *browserruntime.TaskContext, input browser.InputEvent) (memory.Ref, error) {
	event, err := context.NewHeapObject()
	if err != nil {
		return memory.Ref{}, err
	}
	if err := context.SetPrototype(event, memory.RefValue(realm.bindings.eventPrototype)); err != nil {
		return memory.Ref{}, err
	}
	target := memory.RefValue(realm.bindings.window)
	if input.Target.Node != dom.InvalidNodeID {
		target, err = realm.wrappedNodeValue(context, input.Target)
		if err != nil {
			return memory.Ref{}, err
		}
	}
	related := memory.NullValue()
	if input.RelatedTarget.Node != dom.InvalidNodeID {
		related, err = realm.wrappedNodeValue(context, input.RelatedTarget)
		if err != nil {
			return memory.Ref{}, err
		}
	}
	typeValue, err := newString(context, input.Type.String())
	if err != nil {
		return memory.Ref{}, err
	}
	pointerType, err := newString(context, input.PointerType)
	if err != nil {
		return memory.Ref{}, err
	}
	key, err := newString(context, input.Key)
	if err != nil {
		return memory.Ref{}, err
	}
	code, err := newString(context, input.Code)
	if err != nil {
		return memory.Ref{}, err
	}
	data, err := newString(context, input.Data)
	if err != nil {
		return memory.Ref{}, err
	}
	inputType, err := newString(context, input.InputType)
	if err != nil {
		return memory.Ref{}, err
	}
	properties := []struct {
		name  string
		value memory.Value
	}{
		{"type", typeValue},
		{"target", target},
		{"currentTarget", memory.NullValue()},
		{"relatedTarget", related},
		{"eventPhase", memory.NumberValue(eventPhaseNone)},
		{"bubbles", memory.BoolValue(inputEventBubbles(input.Type))},
		{"cancelable", memory.BoolValue(inputEventCancelable(input.Type))},
		{"defaultPrevented", memory.BoolValue(false)},
		{"isTrusted", memory.BoolValue(true)},
		{"clientX", memory.NumberValue(input.X)},
		{"clientY", memory.NumberValue(input.Y)},
		{"button", memory.NumberValue(float64(input.Button))},
		{"buttons", memory.NumberValue(float64(input.Buttons))},
		{"pointerId", memory.NumberValue(float64(input.PointerID))},
		{"pointerType", pointerType},
		{"isPrimary", memory.BoolValue(input.IsPrimary)},
		{"key", key},
		{"code", code},
		{"data", data},
		{"inputType", inputType},
		{"repeat", memory.BoolValue(input.Repeat)},
		{"isComposing", memory.BoolValue(input.IsComposing)},
		{"altKey", memory.BoolValue(input.AltKey)},
		{"ctrlKey", memory.BoolValue(input.CtrlKey)},
		{"metaKey", memory.BoolValue(input.MetaKey)},
		{"shiftKey", memory.BoolValue(input.ShiftKey)},
	}
	for _, property := range properties {
		if err := defineData(context, event, property.name, property.value, true, true, true); err != nil {
			return memory.Ref{}, err
		}
	}
	if err := defineData(context, event, eventBrandProperty, memory.BoolValue(true), false, false, false); err != nil {
		return memory.Ref{}, err
	}
	return event, nil
}

func listenerOptions(context *browserruntime.TaskContext, value memory.Value) (eventListenerOptions, error) {
	options := eventListenerOptions{}
	if value.Kind() == memory.ValueBool {
		options.capture = value.Bool()
		return options, nil
	}
	if !value.IsRef() {
		return options, nil
	}
	kind, err := context.HeapKind(value.Ref())
	if err != nil {
		return options, err
	}
	if kind != memory.HeapObject {
		return options, nil
	}
	for _, option := range []struct {
		name        string
		destination *bool
	}{
		{"capture", &options.capture},
		{"once", &options.once},
		{"passive", &options.passive},
	} {
		name, err := context.NewString(option.name)
		if err != nil {
			return options, err
		}
		property, found, err := context.GetOwnProperty(value.Ref(), name)
		if err != nil {
			return options, err
		}
		if found {
			*option.destination = truthy(property)
		}
	}
	return options, nil
}

func setDataValue(context *browserruntime.TaskContext, object memory.Ref, name string, value memory.Value) error {
	nameRef, err := context.NewString(name)
	if err != nil {
		return err
	}
	// Event dispatch state is represented as writable own data properties in
	// the first native surface. A framework may intentionally shadow one with
	// an own accessor (Solid does this for delegated currentTarget). Native
	// dispatch updates its internal state without invoking or replacing that
	// language-level override.
	descriptor, found, err := context.GetOwnPropertyDescriptor(object, nameRef)
	if err != nil {
		return err
	}
	if found && descriptor.Kind == memory.PropertyAccessor {
		return nil
	}
	return context.SetProperty(object, nameRef, value)
}

func inputEventBubbles(eventType browser.InputEventType) bool {
	switch eventType {
	case browser.InputPointerEnter, browser.InputPointerLeave, browser.InputFocus, browser.InputBlur, browser.InputScroll, browser.InputResize:
		return false
	default:
		return true
	}
}

func inputEventCancelable(eventType browser.InputEventType) bool {
	switch eventType {
	case browser.InputInput, browser.InputFocus, browser.InputBlur, browser.InputFocusIn, browser.InputFocusOut,
		browser.InputChange, browser.InputCompositionStart, browser.InputCompositionUpdate, browser.InputCompositionEnd,
		browser.InputScroll, browser.InputResize:
		return false
	default:
		return true
	}
}
