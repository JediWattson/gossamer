package nativeengine

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/JediWattson/gossamer/internal/browser"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

const (
	nativeWebSocketConstructor uint64 = 24_000 + iota
	nativeWebSocketSend
	nativeWebSocketClose
	nativeWebSocketAddEventListener
	nativeWebSocketRemoveEventListener
)

const webSocketIDProperty = "\x00gossamer.websocket.id"

func (realm *Realm) newWebSocketConstructor(context *browserruntime.TaskContext) (memory.Ref, memory.Ref, error) {
	name, err := context.NewString("WebSocket")
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	constructor, err := context.NewNativeConstructor(memory.RefValue(name), memory.RefValue(realm.active.Global), 1, nativeWebSocketConstructor)
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	prototype, err := constructorPrototype(context, constructor, "WebSocket")
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	for _, method := range []struct {
		name  string
		arity uint32
		id    uint64
	}{{"send", 1, nativeWebSocketSend}, {"close", 2, nativeWebSocketClose}, {"addEventListener", 2, nativeWebSocketAddEventListener}, {"removeEventListener", 2, nativeWebSocketRemoveEventListener}} {
		function, err := realm.newNativeFunction(context, method.name, method.arity, method.id)
		if err != nil {
			return memory.Ref{}, memory.Ref{}, err
		}
		if err := defineData(context, prototype, method.name, memory.RefValue(function), true, false, true); err != nil {
			return memory.Ref{}, memory.Ref{}, err
		}
	}
	for name, value := range map[string]float64{"CONNECTING": 0, "OPEN": 1, "CLOSING": 2, "CLOSED": 3} {
		for _, target := range []memory.Ref{constructor, prototype} {
			if err := defineData(context, target, name, memory.NumberValue(value), false, false, false); err != nil {
				return memory.Ref{}, memory.Ref{}, err
			}
		}
	}
	tag, err := newString(context, "WebSocket")
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	if err := context.DefineProperty(prototype, realm.active.SymbolToStringTag, memory.DataProperty(tag, false, false, true)); err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	return constructor, prototype, nil
}

func (realm *Realm) webSocketConstructor(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	if !this.IsRef() {
		return memory.Value{}, fmt.Errorf("%w: WebSocket constructor requires new", browserruntime.ErrOperandType)
	}
	rawURL, err := stringArgument(context, arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	protocols, err := webSocketProtocols(context, argument(arguments, 1))
	if err != nil {
		return memory.Value{}, err
	}
	host, ok := realm.host.(browser.WebSocketHost)
	if !ok {
		return memory.Value{}, fmt.Errorf("nativeengine: browser host does not expose WebSocket")
	}
	id, protocol, err := host.OpenWebSocket(rawURL, protocols)
	if err != nil {
		return memory.Value{}, err
	}
	properties := []struct {
		name  string
		value memory.Value
	}{
		{webSocketIDProperty, memory.NumberValue(float64(id))},
		{"readyState", memory.NumberValue(0)}, {"bufferedAmount", memory.NumberValue(0)},
	}
	for _, property := range properties {
		if err := defineData(context, this.Ref(), property.name, property.value, property.name != webSocketIDProperty, property.name != webSocketIDProperty, true); err != nil {
			return memory.Value{}, err
		}
	}
	for _, property := range []struct{ name, value string }{{"url", rawURL}, {"protocol", protocol}, {"extensions", ""}, {"binaryType", "blob"}} {
		if err := defineStringData(context, this.Ref(), property.name, property.value, property.name == "binaryType", true); err != nil {
			return memory.Value{}, err
		}
	}
	for _, name := range []string{"onopen", "onmessage", "onerror", "onclose"} {
		if err := defineData(context, this.Ref(), name, memory.NullValue(), true, true, true); err != nil {
			return memory.Value{}, err
		}
	}
	if err := context.MapSet(realm.bindings.webSocketCache, memory.NumberValue(float64(id)), this); err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), nil
}

func webSocketProtocols(context *browserruntime.TaskContext, value memory.Value) ([]string, error) {
	if value.Kind() == memory.ValueUndefined {
		return nil, nil
	}
	if value.IsRef() {
		kind, err := context.HeapKind(value.Ref())
		if err != nil {
			return nil, err
		}
		if kind == memory.HeapString {
			protocol, err := context.DerefString(value.Ref())
			return []string{protocol}, err
		}
		if kind == memory.HeapArray {
			array, err := context.DerefArray(value.Ref())
			if err != nil {
				return nil, err
			}
			protocols := make([]string, 0, array.Length)
			seen := make(map[string]struct{})
			for index := uint32(0); index < array.Length; index++ {
				item, found, err := context.ArrayElement(value.Ref(), index)
				if err != nil || !found {
					return nil, fmt.Errorf("%w: invalid WebSocket protocol list", browserruntime.ErrOperandType)
				}
				protocol, err := valueString(context, item)
				if err != nil {
					return nil, err
				}
				if _, duplicate := seen[protocol]; duplicate {
					return nil, fmt.Errorf("%w: duplicate WebSocket protocol %q", browserruntime.ErrOperandType, protocol)
				}
				seen[protocol] = struct{}{}
				protocols = append(protocols, protocol)
			}
			return protocols, nil
		}
	}
	return nil, fmt.Errorf("%w: invalid WebSocket protocols", browserruntime.ErrOperandType)
}

func (realm *Realm) webSocketSend(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	host, id, err := realm.webSocketHost(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	state, _, err := ownValue(context, this.Ref(), "readyState")
	if err != nil || state.Kind() != memory.ValueNumber || state.Number() != 1 {
		return memory.Value{}, fmt.Errorf("%w: WebSocket is not open", browserruntime.ErrOperandType)
	}
	value := argument(arguments, 0)
	message := browser.WebSocketBinaryMessage
	if value.IsRef() {
		if kind, kindErr := context.HeapKind(value.Ref()); kindErr == nil && kind == memory.HeapString {
			message = browser.WebSocketTextMessage
		}
	} else {
		message = browser.WebSocketTextMessage
	}
	data, err := bodyBytes(context, value)
	if err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), host.SendWebSocket(id, message, data)
}

func (realm *Realm) webSocketClose(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	host, id, err := realm.webSocketHost(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	code := uint16(1000)
	if value := argument(arguments, 0); value.Kind() != memory.ValueUndefined {
		if value.Kind() != memory.ValueNumber || math.IsNaN(value.Number()) || value.Number() != math.Trunc(value.Number()) ||
			(value.Number() != 1000 && (value.Number() < 3000 || value.Number() > 4999)) {
			return memory.Value{}, fmt.Errorf("%w: invalid WebSocket close code", browserruntime.ErrOperandType)
		}
		code = uint16(value.Number())
	}
	reason := ""
	if argument(arguments, 1).Kind() != memory.ValueUndefined {
		reason, err = stringArgument(context, arguments, 1)
		if err != nil {
			return memory.Value{}, err
		}
	}
	if len([]byte(reason)) > 123 {
		return memory.Value{}, fmt.Errorf("%w: WebSocket close reason exceeds 123 bytes", browserruntime.ErrOperandType)
	}
	if err := setOwnValue(context, this.Ref(), "readyState", memory.NumberValue(2)); err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), host.CloseWebSocket(id, code, reason)
}

func (realm *Realm) webSocketAddEventListener(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	if _, _, err := realm.webSocketHost(context, this); err != nil {
		return memory.Value{}, err
	}
	eventType, err := stringArgument(context, arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	callback := argument(arguments, 1)
	if err := requireFunction(context, callback); err != nil {
		return memory.Value{}, err
	}
	array, err := webSocketListenerArray(context, this.Ref(), eventType, true)
	if err != nil {
		return memory.Value{}, err
	}
	snapshot, err := context.DerefArray(array)
	if err != nil {
		return memory.Value{}, err
	}
	for index := uint32(0); index < snapshot.Length; index++ {
		if existing, found, _ := context.ArrayElement(array, index); found && existing == callback {
			return memory.UndefinedValue(), nil
		}
	}
	if err := context.SetArrayElement(array, snapshot.Length, callback); err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), context.SetArrayLength(array, snapshot.Length+1)
}

func (realm *Realm) webSocketRemoveEventListener(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	if _, _, err := realm.webSocketHost(context, this); err != nil {
		return memory.Value{}, err
	}
	eventType, err := stringArgument(context, arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	callback := argument(arguments, 1)
	array, err := webSocketListenerArray(context, this.Ref(), eventType, false)
	if err != nil || array == (memory.Ref{}) {
		return memory.UndefinedValue(), err
	}
	snapshot, err := context.DerefArray(array)
	if err != nil {
		return memory.Value{}, err
	}
	write := uint32(0)
	for index := uint32(0); index < snapshot.Length; index++ {
		value, found, readErr := context.ArrayElement(array, index)
		if readErr != nil {
			return memory.Value{}, readErr
		}
		if found && value != callback {
			if err := context.SetArrayElement(array, write, value); err != nil {
				return memory.Value{}, err
			}
			write++
		}
	}
	return memory.UndefinedValue(), context.SetArrayLength(array, write)
}

func webSocketListenerArray(context *browserruntime.TaskContext, object memory.Ref, eventType string, create bool) (memory.Ref, error) {
	property := "\x00gossamer.websocket.listeners." + strings.ToLower(eventType)
	value, found, err := ownValue(context, object, property)
	if err != nil {
		return memory.Ref{}, err
	}
	if found && value.IsRef() {
		return value.Ref(), nil
	}
	if !create {
		return memory.Ref{}, nil
	}
	array, err := context.NewArray(0)
	if err != nil {
		return memory.Ref{}, err
	}
	return array, defineData(context, object, property, memory.RefValue(array), false, false, false)
}

func (realm *Realm) webSocketHost(context *browserruntime.TaskContext, this memory.Value) (browser.WebSocketHost, browser.WebSocketID, error) {
	if !this.IsRef() {
		return nil, 0, fmt.Errorf("%w: incompatible WebSocket receiver", browserruntime.ErrOperandType)
	}
	value, found, err := ownValue(context, this.Ref(), webSocketIDProperty)
	if err != nil || !found || value.Kind() != memory.ValueNumber {
		return nil, 0, fmt.Errorf("%w: incompatible WebSocket receiver", browserruntime.ErrOperandType)
	}
	host, ok := realm.host.(browser.WebSocketHost)
	if !ok {
		return nil, 0, fmt.Errorf("nativeengine: browser host does not expose WebSocket")
	}
	return host, browser.WebSocketID(value.Number()), nil
}

func (realm *Realm) DispatchWebSocket(host browser.Host, id browser.WebSocketID, event browser.WebSocketEvent) error {
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
		return err
	}
	value, found, err := context.MapGet(realm.bindings.webSocketCache, memory.NumberValue(float64(id)))
	if err != nil || !found || !value.IsRef() {
		return err
	}
	object := value.Ref()
	eventType := map[browser.WebSocketEventType]string{
		browser.WebSocketOpenEvent: "open", browser.WebSocketMessageEvent: "message",
		browser.WebSocketErrorEvent: "error", browser.WebSocketCloseEvent: "close",
	}[event.Type]
	if eventType == "" {
		return fmt.Errorf("nativeengine: invalid WebSocket event %d", event.Type)
	}
	if event.Type == browser.WebSocketOpenEvent {
		_ = setOwnValue(context, object, "readyState", memory.NumberValue(1))
	}
	if event.Type == browser.WebSocketCloseEvent {
		_ = setOwnValue(context, object, "readyState", memory.NumberValue(3))
	}
	eventObject, err := context.NewHeapObject()
	if err != nil {
		return err
	}
	typeValue, err := newString(context, eventType)
	if err != nil {
		return err
	}
	for _, property := range []struct {
		name  string
		value memory.Value
	}{{"type", typeValue}, {"target", value}, {"currentTarget", value}} {
		if err := defineData(context, eventObject, property.name, property.value, false, true, true); err != nil {
			return err
		}
	}
	if event.Type == browser.WebSocketMessageEvent {
		var data memory.Value
		if event.Message == browser.WebSocketTextMessage {
			data, err = newString(context, string(event.Data))
		} else {
			data, err = newUint8Array(context, event.Data)
		}
		if err != nil {
			return err
		}
		if err := defineData(context, eventObject, "data", data, false, true, true); err != nil {
			return err
		}
	}
	if event.Type == browser.WebSocketCloseEvent {
		reason, err := newString(context, event.Reason)
		if err != nil {
			return err
		}
		for _, property := range []struct {
			name  string
			value memory.Value
		}{{"code", memory.NumberValue(float64(event.Code))}, {"reason", reason}, {"wasClean", memory.BoolValue(event.WasClean)}} {
			if err := defineData(context, eventObject, property.name, property.value, false, true, true); err != nil {
				return err
			}
		}
	}
	var result error
	if handler, found, lookupErr := ownValue(context, object, "on"+eventType); lookupErr != nil {
		result = errors.Join(result, lookupErr)
	} else if found && handler.IsRef() {
		if _, callErr := realm.interpreter.CallWithoutCheckpoint(context, handler.Ref(), value, memory.RefValue(eventObject)); callErr != nil {
			result = errors.Join(result, callErr)
		}
	}
	listeners, listenerErr := webSocketListenerArray(context, object, eventType, false)
	result = errors.Join(result, listenerErr)
	if listeners != (memory.Ref{}) {
		snapshot, snapshotErr := context.DerefArray(listeners)
		result = errors.Join(result, snapshotErr)
		if snapshotErr == nil {
			for index := uint32(0); index < snapshot.Length; index++ {
				callback, found, readErr := context.ArrayElement(listeners, index)
				result = errors.Join(result, readErr)
				if readErr == nil && found && callback.IsRef() {
					_, callErr := realm.interpreter.CallWithoutCheckpoint(context, callback.Ref(), value, memory.RefValue(eventObject))
					result = errors.Join(result, callErr)
				}
			}
		}
	}
	if event.Type == browser.WebSocketCloseEvent {
		_, removeErr := context.MapDelete(realm.bindings.webSocketCache, memory.NumberValue(float64(id)))
		result = errors.Join(result, removeErr)
	}
	return result
}

func setOwnValue(context *browserruntime.TaskContext, object memory.Ref, name string, value memory.Value) error {
	nameRef, err := context.NewString(name)
	if err != nil {
		return err
	}
	return context.SetProperty(object, nameRef, value)
}

var _ browser.JSWebSocketRealm = (*Realm)(nil)
