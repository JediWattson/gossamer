package nativeengine

import (
	"crypto/rand"
	"fmt"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/css"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

const (
	nativeMatchMedia uint64 = 20_000 + iota
	nativeMediaQueryNoop
	nativeMediaQueryDispatch
	nativeCryptoRandomUUID
	nativeCryptoGetRandomValues
	nativeAbortControllerConstructor
	nativeAbortControllerAbort
	nativeAbortSignalConstructor
	nativeAbortSignalAbort
	nativeAbortSignalAny
	nativeAbortSignalThrowIfAborted
	nativeAbortSignalAddEventListener
	nativeAbortSignalRemoveEventListener
)

const (
	abortSignalListenersProperty  = "\x00gossamer.abort.listeners"
	abortSignalDependentsProperty = "\x00gossamer.abort.dependents"
)

func (realm *Realm) newCrypto(context *browserruntime.TaskContext) (memory.Ref, error) {
	cryptoObject, err := context.NewHeapObject()
	if err != nil {
		return memory.Ref{}, err
	}
	randomUUID, err := realm.newNativeFunction(context, "randomUUID", 0, nativeCryptoRandomUUID)
	if err != nil {
		return memory.Ref{}, err
	}
	if err := defineData(context, cryptoObject, "randomUUID", memory.RefValue(randomUUID), true, false, true); err != nil {
		return memory.Ref{}, err
	}
	getRandomValues, err := realm.newNativeFunction(context, "getRandomValues", 1, nativeCryptoGetRandomValues)
	if err != nil {
		return memory.Ref{}, err
	}
	if err := defineData(context, cryptoObject, "getRandomValues", memory.RefValue(getRandomValues), true, false, true); err != nil {
		return memory.Ref{}, err
	}
	return cryptoObject, nil
}

func (realm *Realm) cryptoGetRandomValues(context *browserruntime.TaskContext, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	value := argument(arguments, 0)
	view, elemSize, err := requireAnyTypedArray(context, value)
	if err != nil {
		return memory.Value{}, err
	}
	if view.Element == memory.ElementFloat32 || view.Element == memory.ElementFloat64 {
		exception, exceptionErr := realm.newDOMExceptionValue(context, "TypeMismatchError", "The typed array must be an integer type.")
		if exceptionErr != nil {
			return memory.Value{}, exceptionErr
		}
		return memory.Value{}, browserruntime.Throw(exception)
	}
	byteLength := view.Length * elemSize
	if byteLength > 65_536 {
		exception, exceptionErr := realm.newDOMExceptionValue(context, "QuotaExceededError", "The requested random byte length exceeds 65536.")
		if exceptionErr != nil {
			return memory.Value{}, exceptionErr
		}
		return memory.Value{}, browserruntime.Throw(exception)
	}
	bytes := make([]byte, byteLength)
	if _, err := rand.Read(bytes); err != nil {
		return memory.Value{}, fmt.Errorf("nativeengine: getRandomValues: %w", err)
	}
	if err := context.WriteArrayBuffer(view.Buffer, view.ByteOffset, bytes); err != nil {
		return memory.Value{}, err
	}
	return value, nil
}

func (realm *Realm) cryptoRandomUUID(context *browserruntime.TaskContext, _ memory.Value, _ []memory.Value) (memory.Value, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return memory.Value{}, fmt.Errorf("nativeengine: randomUUID: %w", err)
	}
	bytes[6] = bytes[6]&0x0f | 0x40
	bytes[8] = bytes[8]&0x3f | 0x80
	return newString(context, fmt.Sprintf("%x-%x-%x-%x-%x", bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:16]))
}

func (realm *Realm) newNavigator(context *browserruntime.TaskContext) (memory.Ref, error) {
	navigator, err := context.NewHeapObject()
	if err != nil {
		return memory.Ref{}, err
	}
	languages, err := context.NewArray(1)
	if err != nil {
		return memory.Ref{}, err
	}
	language, err := newString(context, "en-US")
	if err != nil {
		return memory.Ref{}, err
	}
	if err := context.SetArrayElement(languages, 0, language); err != nil {
		return memory.Ref{}, err
	}
	userAgent, err := newString(context, "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Gossamer/0.1")
	if err != nil {
		return memory.Ref{}, err
	}
	appVersion, err := newString(context, "5.0 (Macintosh; Intel Mac OS X 10_15_7) Gossamer/0.1")
	if err != nil {
		return memory.Ref{}, err
	}
	platform, err := newString(context, "MacIntel")
	if err != nil {
		return memory.Ref{}, err
	}
	vendor, err := newString(context, "")
	if err != nil {
		return memory.Ref{}, err
	}
	properties := []struct {
		name  string
		value memory.Value
	}{
		{"userAgent", userAgent},
		{"appVersion", appVersion},
		{"platform", platform},
		{"vendor", vendor},
		{"language", language},
		{"languages", memory.RefValue(languages)},
		{"hardwareConcurrency", memory.NumberValue(4)},
		{"deviceMemory", memory.NumberValue(8)},
		{"maxTouchPoints", memory.NumberValue(0)},
		{"onLine", memory.BoolValue(true)},
		{"cookieEnabled", memory.BoolValue(true)},
		{"standalone", memory.BoolValue(false)},
	}
	for _, property := range properties {
		if err := defineData(context, navigator, property.name, property.value, false, true, true); err != nil {
			return memory.Ref{}, err
		}
	}
	return navigator, nil
}

func (realm *Realm) matchMedia(context *browserruntime.TaskContext, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	query, err := valueString(context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	host, ok := realm.host.(browser.DOMGeometryHost)
	if !ok {
		return memory.Value{}, fmt.Errorf("nativeengine: browser host does not expose viewport geometry")
	}
	geometry, err := host.ViewportGeometry()
	if err != nil {
		return memory.Value{}, err
	}
	matches := css.MediaQueryListMatches(query, css.MediaEnvironment{
		Type:            "screen",
		Width:           geometry.InnerWidth,
		Height:          geometry.InnerHeight,
		InitialFontSize: 16,
	})
	object, err := context.NewHeapObject()
	if err != nil {
		return memory.Value{}, err
	}
	media, err := newString(context, query)
	if err != nil {
		return memory.Value{}, err
	}
	noop, err := realm.newNativeFunction(context, "addEventListener", 2, nativeMediaQueryNoop)
	if err != nil {
		return memory.Value{}, err
	}
	dispatch, err := realm.newNativeFunction(context, "dispatchEvent", 1, nativeMediaQueryDispatch)
	if err != nil {
		return memory.Value{}, err
	}
	for _, property := range []struct {
		name  string
		value memory.Value
	}{
		{"matches", memory.BoolValue(matches)}, {"media", media}, {"onchange", memory.NullValue()},
		{"addListener", memory.RefValue(noop)}, {"removeListener", memory.RefValue(noop)},
		{"addEventListener", memory.RefValue(noop)}, {"removeEventListener", memory.RefValue(noop)},
		{"dispatchEvent", memory.RefValue(dispatch)},
	} {
		if err := defineData(context, object, property.name, property.value, property.name == "onchange", true, true); err != nil {
			return memory.Value{}, err
		}
	}
	return memory.RefValue(object), nil
}

func (realm *Realm) mediaQueryNoop(_ *browserruntime.TaskContext, _ memory.Value, _ []memory.Value) (memory.Value, error) {
	return memory.UndefinedValue(), nil
}

func (realm *Realm) mediaQueryDispatch(_ *browserruntime.TaskContext, _ memory.Value, _ []memory.Value) (memory.Value, error) {
	return memory.BoolValue(false), nil
}

func (realm *Realm) newAbortConstructors(context *browserruntime.TaskContext) (memory.Ref, memory.Ref, memory.Ref, memory.Ref, error) {
	controllerName, err := newString(context, "AbortController")
	if err != nil {
		return memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, err
	}
	controllerConstructor, err := context.NewNativeConstructor(controllerName, memory.RefValue(realm.active.Global), 0, nativeAbortControllerConstructor)
	if err != nil {
		return memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, err
	}
	controllerPrototype, err := constructorPrototype(context, controllerConstructor, "AbortController")
	if err != nil {
		return memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, err
	}
	if err := context.SetPrototype(controllerPrototype, memory.RefValue(realm.active.ObjectPrototype)); err != nil {
		return memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, err
	}
	abortFn, err := realm.newNativeFunction(context, "abort", 1, nativeAbortControllerAbort)
	if err != nil {
		return memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, err
	}
	if err := defineData(context, controllerPrototype, "abort", memory.RefValue(abortFn), true, false, true); err != nil {
		return memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, err
	}

	signalName, err := newString(context, "AbortSignal")
	if err != nil {
		return memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, err
	}
	signalConstructor, err := context.NewNativeConstructor(signalName, memory.RefValue(realm.active.Global), 0, nativeAbortSignalConstructor)
	if err != nil {
		return memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, err
	}
	signalPrototype, err := constructorPrototype(context, signalConstructor, "AbortSignal")
	if err != nil {
		return memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, err
	}
	if err := context.SetPrototype(signalPrototype, memory.RefValue(realm.active.ObjectPrototype)); err != nil {
		return memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, err
	}
	for _, method := range []struct {
		target memory.Ref
		name   string
		arity  uint32
		id     uint64
	}{
		{signalConstructor, "abort", 1, nativeAbortSignalAbort},
		{signalConstructor, "any", 1, nativeAbortSignalAny},
		{signalPrototype, "throwIfAborted", 0, nativeAbortSignalThrowIfAborted},
		{signalPrototype, "addEventListener", 3, nativeAbortSignalAddEventListener},
		{signalPrototype, "removeEventListener", 3, nativeAbortSignalRemoveEventListener},
		{signalPrototype, "dispatchEvent", 1, nativeMediaQueryDispatch},
	} {
		fn, err := realm.newNativeFunction(context, method.name, method.arity, method.id)
		if err != nil {
			return memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, err
		}
		if err := defineData(context, method.target, method.name, memory.RefValue(fn), true, false, true); err != nil {
			return memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, err
		}
	}
	return controllerConstructor, controllerPrototype, signalConstructor, signalPrototype, nil
}

func (realm *Realm) newAbortSignalObject(context *browserruntime.TaskContext, aborted bool, reason memory.Value) (memory.Ref, error) {
	signal, err := context.NewHeapObject()
	if err != nil {
		return memory.Ref{}, err
	}
	if realm.bindings != nil && realm.bindings.abortSignalPrototype != (memory.Ref{}) {
		if err := context.SetPrototype(signal, memory.RefValue(realm.bindings.abortSignalPrototype)); err != nil {
			return memory.Ref{}, err
		}
	}
	listeners, err := context.NewArray(0)
	if err != nil {
		return memory.Ref{}, err
	}
	dependents, err := context.NewArray(0)
	if err != nil {
		return memory.Ref{}, err
	}
	for _, prop := range []struct {
		name       string
		value      memory.Value
		enumerable bool
	}{
		{"aborted", memory.BoolValue(aborted), true},
		{"reason", reason, true},
		{"onabort", memory.NullValue(), true},
		{abortSignalListenersProperty, memory.RefValue(listeners), false},
		{abortSignalDependentsProperty, memory.RefValue(dependents), false},
	} {
		if err := defineData(context, signal, prop.name, prop.value, true, prop.enumerable, true); err != nil {
			return memory.Ref{}, err
		}
	}
	return signal, nil
}

func (realm *Realm) abortControllerConstructor(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	if !this.IsRef() {
		return memory.Value{}, fmt.Errorf("%w: AbortController constructor requires new", browserruntime.ErrOperandType)
	}
	signal, err := realm.newAbortSignalObject(context, false, memory.UndefinedValue())
	if err != nil {
		return memory.Value{}, err
	}
	if err := defineData(context, this.Ref(), "signal", memory.RefValue(signal), false, true, true); err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), nil
}

func (realm *Realm) abortSignalConstructor(_ *browserruntime.TaskContext, _ memory.Value, _ []memory.Value) (memory.Value, error) {
	return memory.Value{}, fmt.Errorf("%w: Illegal constructor", browserruntime.ErrOperandType)
}

func (realm *Realm) defaultAbortReason(context *browserruntime.TaskContext, provided memory.Value) (memory.Value, error) {
	if provided.Kind() != memory.ValueUndefined {
		return provided, nil
	}
	return realm.newDOMExceptionValue(context, "AbortError", "The operation was aborted.")
}

func (realm *Realm) triggerAbortSignal(context *browserruntime.TaskContext, signal memory.Ref, reason memory.Value) error {
	abortedKey, err := context.NewString("aborted")
	if err != nil {
		return err
	}
	abortedVal, found, err := context.GetOwnProperty(signal, abortedKey)
	if err != nil {
		return err
	}
	if found && abortedVal.Kind() == memory.ValueBool && abortedVal.Bool() {
		return nil
	}
	resolvedReason, err := realm.defaultAbortReason(context, reason)
	if err != nil {
		return err
	}
	if err := defineData(context, signal, "aborted", memory.BoolValue(true), true, true, true); err != nil {
		return err
	}
	if err := defineData(context, signal, "reason", resolvedReason, true, true, true); err != nil {
		return err
	}
	onabortKey, err := context.NewString("onabort")
	if err != nil {
		return err
	}
	if onabortVal, found, err := context.GetOwnProperty(signal, onabortKey); err != nil {
		return err
	} else if found && onabortVal.IsRef() {
		if kind, kindErr := context.HeapKind(onabortVal.Ref()); kindErr == nil && kind == memory.HeapFunction {
			if _, callErr := realm.interpreter.CallWithoutCheckpoint(context, onabortVal.Ref(), memory.RefValue(signal)); callErr != nil {
				return callErr
			}
		}
	}
	listenersKey, err := context.NewString(abortSignalListenersProperty)
	if err != nil {
		return err
	}
	listenersVal, found, err := context.GetOwnProperty(signal, listenersKey)
	if err != nil {
		return err
	}
	if found && listenersVal.IsRef() {
		snapshot, err := context.DerefArray(listenersVal.Ref())
		if err != nil {
			return err
		}
		emptyListeners, err := context.NewArray(0)
		if err != nil {
			return err
		}
		if err := defineData(context, signal, abortSignalListenersProperty, memory.RefValue(emptyListeners), true, false, true); err != nil {
			return err
		}
		for index := uint32(0); index < snapshot.Length; index++ {
			callback, exists, elemErr := context.ArrayElement(listenersVal.Ref(), index)
			if elemErr != nil {
				return elemErr
			}
			if !exists || !callback.IsRef() {
				continue
			}
			if kind, kindErr := context.HeapKind(callback.Ref()); kindErr == nil && kind == memory.HeapFunction {
				if _, callErr := realm.interpreter.CallWithoutCheckpoint(context, callback.Ref(), memory.RefValue(signal)); callErr != nil {
					return callErr
				}
			}
		}
	}
	dependentsKey, err := context.NewString(abortSignalDependentsProperty)
	if err != nil {
		return err
	}
	dependentsVal, found, err := context.GetOwnProperty(signal, dependentsKey)
	if err != nil || !found || !dependentsVal.IsRef() {
		return err
	}
	depSnapshot, err := context.DerefArray(dependentsVal.Ref())
	if err != nil {
		return err
	}
	emptyDependents, err := context.NewArray(0)
	if err != nil {
		return err
	}
	if err := defineData(context, signal, abortSignalDependentsProperty, memory.RefValue(emptyDependents), true, false, true); err != nil {
		return err
	}
	for index := uint32(0); index < depSnapshot.Length; index++ {
		dependent, exists, elemErr := context.ArrayElement(dependentsVal.Ref(), index)
		if elemErr != nil {
			return elemErr
		}
		if !exists || !dependent.IsRef() {
			continue
		}
		if err := realm.triggerAbortSignal(context, dependent.Ref(), resolvedReason); err != nil {
			return err
		}
	}
	return nil
}

func (realm *Realm) abortControllerAbort(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	if !this.IsRef() {
		return memory.Value{}, fmt.Errorf("%w: invalid AbortController receiver", browserruntime.ErrOperandType)
	}
	signalKey, err := context.NewString("signal")
	if err != nil {
		return memory.Value{}, err
	}
	signalVal, found, err := context.GetOwnProperty(this.Ref(), signalKey)
	if err != nil || !found || !signalVal.IsRef() {
		return memory.UndefinedValue(), err
	}
	if err := realm.triggerAbortSignal(context, signalVal.Ref(), argument(arguments, 0)); err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), nil
}

func (realm *Realm) abortSignalAbort(context *browserruntime.TaskContext, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	reason, err := realm.defaultAbortReason(context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	signal, err := realm.newAbortSignalObject(context, true, reason)
	if err != nil {
		return memory.Value{}, err
	}
	return memory.RefValue(signal), nil
}

func (realm *Realm) abortSignalAny(context *browserruntime.TaskContext, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	list := argument(arguments, 0)
	var sources []memory.Ref
	if list.IsRef() {
		if kind, err := context.HeapKind(list.Ref()); err == nil && kind == memory.HeapArray {
			snapshot, err := context.DerefArray(list.Ref())
			if err != nil {
				return memory.Value{}, err
			}
			abortedKey, err := context.NewString("aborted")
			if err != nil {
				return memory.Value{}, err
			}
			reasonKey, err := context.NewString("reason")
			if err != nil {
				return memory.Value{}, err
			}
			for index := uint32(0); index < snapshot.Length; index++ {
				item, found, err := context.ArrayElement(list.Ref(), index)
				if err != nil {
					return memory.Value{}, err
				}
				if !found || !item.IsRef() {
					continue
				}
				abortedVal, hasAborted, err := context.GetOwnProperty(item.Ref(), abortedKey)
				if err != nil {
					return memory.Value{}, err
				}
				if hasAborted && abortedVal.Kind() == memory.ValueBool && abortedVal.Bool() {
					reasonVal, _, _ := context.GetOwnProperty(item.Ref(), reasonKey)
					signal, err := realm.newAbortSignalObject(context, true, reasonVal)
					if err != nil {
						return memory.Value{}, err
					}
					return memory.RefValue(signal), nil
				}
				sources = append(sources, item.Ref())
			}
		}
	}
	signal, err := realm.newAbortSignalObject(context, false, memory.UndefinedValue())
	if err != nil {
		return memory.Value{}, err
	}
	if len(sources) > 0 {
		dependentsKey, err := context.NewString(abortSignalDependentsProperty)
		if err != nil {
			return memory.Value{}, err
		}
		for _, source := range sources {
			dependentsVal, found, err := context.GetOwnProperty(source, dependentsKey)
			if err != nil || !found || !dependentsVal.IsRef() {
				continue
			}
			depSnapshot, err := context.DerefArray(dependentsVal.Ref())
			if err != nil {
				continue
			}
			if err := context.SetArrayElement(dependentsVal.Ref(), depSnapshot.Length, memory.RefValue(signal)); err != nil {
				return memory.Value{}, err
			}
		}
	}
	return memory.RefValue(signal), nil
}

func (realm *Realm) abortSignalThrowIfAborted(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	if !this.IsRef() {
		return memory.UndefinedValue(), nil
	}
	abortedKey, err := context.NewString("aborted")
	if err != nil {
		return memory.Value{}, err
	}
	abortedVal, found, err := context.GetOwnProperty(this.Ref(), abortedKey)
	if err != nil {
		return memory.Value{}, err
	}
	if found && abortedVal.Kind() == memory.ValueBool && abortedVal.Bool() {
		reasonKey, err := context.NewString("reason")
		if err != nil {
			return memory.Value{}, err
		}
		reasonVal, _, err := context.GetOwnProperty(this.Ref(), reasonKey)
		if err != nil {
			return memory.Value{}, err
		}
		return memory.Value{}, browserruntime.Throw(reasonVal)
	}
	return memory.UndefinedValue(), nil
}

func (realm *Realm) abortSignalAddEventListener(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	if !this.IsRef() || len(arguments) < 2 {
		return memory.UndefinedValue(), nil
	}
	eventType, err := valueString(context, arguments[0])
	if err != nil || eventType != "abort" {
		return memory.UndefinedValue(), err
	}
	listener := arguments[1]
	if !listener.IsRef() {
		return memory.UndefinedValue(), nil
	}
	abortedKey, err := context.NewString("aborted")
	if err != nil {
		return memory.Value{}, err
	}
	abortedVal, found, err := context.GetOwnProperty(this.Ref(), abortedKey)
	if err != nil {
		return memory.Value{}, err
	}
	if found && abortedVal.Kind() == memory.ValueBool && abortedVal.Bool() {
		return memory.UndefinedValue(), nil
	}
	listenersKey, err := context.NewString(abortSignalListenersProperty)
	if err != nil {
		return memory.Value{}, err
	}
	listenersVal, found, err := context.GetOwnProperty(this.Ref(), listenersKey)
	if err != nil || !found || !listenersVal.IsRef() {
		return memory.UndefinedValue(), err
	}
	snapshot, err := context.DerefArray(listenersVal.Ref())
	if err != nil {
		return memory.Value{}, err
	}
	if err := context.SetArrayElement(listenersVal.Ref(), snapshot.Length, listener); err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), nil
}

func (realm *Realm) abortSignalRemoveEventListener(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	if !this.IsRef() || len(arguments) < 2 {
		return memory.UndefinedValue(), nil
	}
	eventType, err := valueString(context, arguments[0])
	if err != nil || eventType != "abort" {
		return memory.UndefinedValue(), err
	}
	listener := arguments[1]
	if !listener.IsRef() {
		return memory.UndefinedValue(), nil
	}
	listenersKey, err := context.NewString(abortSignalListenersProperty)
	if err != nil {
		return memory.Value{}, err
	}
	listenersVal, found, err := context.GetOwnProperty(this.Ref(), listenersKey)
	if err != nil || !found || !listenersVal.IsRef() {
		return memory.UndefinedValue(), err
	}
	snapshot, err := context.DerefArray(listenersVal.Ref())
	if err != nil {
		return memory.Value{}, err
	}
	for index := uint32(0); index < snapshot.Length; index++ {
		current, exists, elemErr := context.ArrayElement(listenersVal.Ref(), index)
		if elemErr != nil {
			return memory.Value{}, elemErr
		}
		if exists && current.IsRef() && current.Ref() == listener.Ref() {
			_, _ = context.DeleteArrayElement(listenersVal.Ref(), index)
		}
	}
	return memory.UndefinedValue(), nil
}
