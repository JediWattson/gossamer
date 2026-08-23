package nativeengine

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/JediWattson/gossamer/internal/browser"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func (realm *Realm) windowParent(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	document, err := realm.windowDocument(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	host, ok := realm.host.(browser.WindowProxyHost)
	if !ok {
		return memory.Value{}, fmt.Errorf("nativeengine: browser host does not expose WindowProxy relationships")
	}
	parent, found, err := host.ParentWindow(document)
	if err != nil {
		return memory.Value{}, err
	}
	if !found {
		return memory.NullValue(), nil
	}
	return realm.windowProxyValue(context, parent)
}

func (realm *Realm) windowTop(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	document, err := realm.windowDocument(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	host, ok := realm.host.(browser.WindowProxyHost)
	if !ok {
		return memory.Value{}, fmt.Errorf("nativeengine: browser host does not expose WindowProxy relationships")
	}
	top, found, err := host.TopWindow(document)
	if err != nil {
		return memory.Value{}, err
	}
	if !found {
		return memory.NullValue(), nil
	}
	return realm.windowProxyValue(context, top)
}

func (realm *Realm) iframeContentWindow(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	iframe, err := realm.unwrapNode(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	host, ok := realm.host.(browser.WindowProxyHost)
	if !ok {
		return memory.Value{}, fmt.Errorf("nativeengine: browser host does not expose WindowProxy relationships")
	}
	child, found, err := host.ChildWindow(iframe)
	if err != nil {
		return memory.Value{}, err
	}
	if !found {
		return memory.NullValue(), nil
	}
	return realm.windowProxyValue(context, child)
}

func (realm *Realm) windowPostMessage(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	target, err := realm.windowDocument(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	targetOrigin := "/"
	if len(arguments) > 1 {
		targetOrigin, err = stringArgument(context, arguments, 1)
		if err != nil {
			return memory.Value{}, err
		}
	}
	payload, undefined, err := realm.serializeWindowMessage(context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	host, ok := realm.host.(browser.WindowProxyHost)
	if !ok {
		return memory.Value{}, fmt.Errorf("nativeengine: browser host does not expose WindowProxy messaging")
	}
	if err := host.PostWindowMessage(target, targetOrigin, payload, undefined); err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), nil
}

func (realm *Realm) windowDocument(context *browserruntime.TaskContext, value memory.Value) (browser.DocumentGeneration, error) {
	if !value.IsRef() {
		return 0, fmt.Errorf("%w: Window method called with an invalid receiver", browserruntime.ErrOperandType)
	}
	recordValue, found, err := ownProperty(context, value.Ref(), hostRecordProperty)
	if err != nil || !found || !recordValue.IsRef() {
		return 0, fmt.Errorf("%w: Window method called with an invalid receiver", browserruntime.ErrOperandType)
	}
	record, err := context.DerefHostObject(recordValue.Ref())
	if err != nil || record.Class != hostClassWindow || record.Scope == 0 {
		return 0, fmt.Errorf("%w: Window method called with an invalid receiver", browserruntime.ErrOperandType)
	}
	return browser.DocumentGeneration(record.Scope), nil
}

func (realm *Realm) windowProxyValue(context *browserruntime.TaskContext, document browser.DocumentGeneration) (memory.Value, error) {
	if document == 0 {
		return memory.NullValue(), nil
	}
	current, err := realm.windowDocument(context, memory.RefValue(realm.bindings.window))
	if err != nil {
		return memory.Value{}, err
	}
	if document == current {
		return memory.RefValue(realm.bindings.window), nil
	}
	key, err := newString(context, "window:"+strconv.FormatUint(uint64(document), 10))
	if err != nil {
		return memory.Value{}, err
	}
	if cached, found, err := context.MapGet(realm.bindings.wrapperCache, key); err != nil {
		return memory.Value{}, err
	} else if found && cached.IsRef() {
		return cached, nil
	}
	wrapper, err := realm.newHostWrapperLocked(context, memory.HostObject{
		Class: hostClassWindow, Scope: uint64(document), Identity: 1,
	}, realm.bindings.windowPrototype)
	if err != nil {
		return memory.Value{}, err
	}
	if err := context.MapSet(realm.bindings.wrapperCache, key, memory.RefValue(wrapper)); err != nil {
		return memory.Value{}, err
	}
	return memory.RefValue(wrapper), nil
}

func (realm *Realm) serializeWindowMessage(context *browserruntime.TaskContext, value memory.Value) (string, bool, error) {
	if value.Kind() == memory.ValueUndefined {
		return "", true, nil
	}
	result, err := realm.callJSONMethod(context, "stringify", value)
	if err != nil {
		return "", false, err
	}
	if result.Kind() == memory.ValueUndefined {
		return "", false, fmt.Errorf("%w: postMessage data is not cloneable", browserruntime.ErrOperandType)
	}
	if !result.IsRef() {
		return "", false, fmt.Errorf("nativeengine: JSON.stringify returned a non-string value")
	}
	payload, err := context.DerefString(result.Ref())
	return payload, false, err
}

func (realm *Realm) callJSONMethod(context *browserruntime.TaskContext, method string, value memory.Value) (memory.Value, error) {
	jsonObject, found, err := globalRef(context, realm.active.Global, "JSON")
	if err != nil || !found {
		return memory.Value{}, fmt.Errorf("nativeengine: JSON binding is unavailable")
	}
	methodValue, found, err := ownProperty(context, jsonObject, method)
	if err != nil || !found || !methodValue.IsRef() {
		return memory.Value{}, fmt.Errorf("nativeengine: JSON.%s is unavailable", method)
	}
	return realm.interpreter.CallWithoutCheckpoint(context, methodValue.Ref(), memory.RefValue(jsonObject), value)
}

func (realm *Realm) DispatchWindowMessage(host browser.Host, message browser.WindowMessage) error {
	if realm == nil {
		return ErrRealmClosed
	}
	realm.mutex.Lock()
	defer realm.mutex.Unlock()
	if realm.closed {
		return ErrRealmClosed
	}
	realm.host = host
	defer func() { realm.host = nil }()
	task, err := runtimeTask(host)
	if err != nil {
		return err
	}
	context, err := realm.beginTaskLocked(task)
	if err != nil {
		return fmt.Errorf("nativeengine: begin window message task %d: %w", task.TaskID, err)
	}
	data := memory.UndefinedValue()
	if !message.Undefined {
		encoded, err := newString(context, message.JSON)
		if err != nil {
			return err
		}
		data, err = realm.callJSONMethod(context, "parse", encoded)
		if err != nil {
			return err
		}
	}
	origin, err := newString(context, message.Origin)
	if err != nil {
		return err
	}
	source, err := realm.windowProxyValue(context, message.Source)
	if err != nil {
		return err
	}
	typeValue, err := newString(context, "message")
	if err != nil {
		return err
	}
	lastEventID, err := newString(context, "")
	if err != nil {
		return err
	}
	event, err := context.NewHeapObject()
	if err != nil {
		return err
	}
	if err := context.SetPrototype(event, memory.RefValue(realm.bindings.eventPrototype)); err != nil {
		return err
	}
	for _, property := range []struct {
		name  string
		value memory.Value
	}{
		{"type", typeValue}, {"target", memory.RefValue(realm.bindings.window)},
		{"currentTarget", memory.NullValue()}, {"eventPhase", memory.NumberValue(eventPhaseNone)},
		{"bubbles", memory.BoolValue(false)}, {"cancelable", memory.BoolValue(false)},
		{"defaultPrevented", memory.BoolValue(false)}, {"isTrusted", memory.BoolValue(true)},
		{"data", data}, {"origin", origin}, {"source", source}, {"lastEventId", lastEventID},
	} {
		if err := defineData(context, event, property.name, property.value, true, true, true); err != nil {
			return err
		}
	}
	if err := defineData(context, event, eventBrandProperty, memory.BoolValue(true), false, false, false); err != nil {
		return err
	}
	documentHost, ok := realm.host.(browser.DOMDocumentHost)
	if !ok {
		return fmt.Errorf("nativeengine: browser host does not expose document metadata")
	}
	metadata, err := documentHost.DocumentMetadata()
	if err != nil {
		return err
	}
	_, dispatchErr := realm.dispatchEventObjectLocked(
		context, windowEventTarget(metadata.Root.Document), event, "message", false, false,
	)
	var handlerErr error
	if handler, found, lookupErr := ownValue(context, realm.bindings.window, "onmessage"); lookupErr != nil {
		handlerErr = lookupErr
	} else if found && handler.IsRef() {
		_, callErr := realm.interpreter.CallWithoutCheckpoint(
			context, handler.Ref(), memory.RefValue(realm.bindings.window), memory.RefValue(event),
		)
		if callErr != nil && !realm.reportCallbackExceptionLocked(context, "window onmessage handler", callErr) {
			handlerErr = callErr
		}
	}
	return errors.Join(dispatchErr, handlerErr)
}

var _ browser.JSWindowMessageRealm = (*Realm)(nil)
