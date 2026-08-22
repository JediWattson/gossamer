package runtime

import (
	"fmt"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

const (
	proxyTargetProperty  = "\x00gossamer.proxy.target"
	proxyHandlerProperty = "\x00gossamer.proxy.handler"
)

func (intrinsics *Intrinsics) installProxyBuiltins(context *TaskContext) (memory.Ref, memory.Ref, error) {
	name, err := context.NewString("Proxy")
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	constructor, err := context.NewNativeConstructor(memory.RefValue(name), memory.NullValue(), 2, nativeProxyConstructor)
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	reflectObject, err := context.NewHeapObject()
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	for _, method := range []struct {
		name  string
		arity uint32
		id    uint64
	}{
		{"ownKeys", 1, nativeReflectOwnKeys},
		{"getOwnPropertyDescriptor", 2, nativeReflectGetOwnPropertyDescriptor},
	} {
		callable, methodErr := intrinsics.newBuiltinMethod(context, method.name, method.arity, method.id)
		if methodErr != nil {
			return memory.Ref{}, memory.Ref{}, methodErr
		}
		if methodErr := defineData(context, reflectObject, method.name, memory.RefValue(callable), true, false, true); methodErr != nil {
			return memory.Ref{}, memory.Ref{}, methodErr
		}
	}
	return constructor, reflectObject, nil
}

func builtinProxyConstructor(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	target, err := requireObjectLike(execution.context, argument(arguments, 0), "Proxy target")
	if err != nil {
		return memory.Value{}, err
	}
	handler, err := requireObjectLike(execution.context, argument(arguments, 1), "Proxy handler")
	if err != nil {
		return memory.Value{}, err
	}
	proxy, err := execution.context.NewHeapObject()
	if err != nil {
		return memory.Value{}, err
	}
	targetHeader, err := execution.context.DerefObjectHeader(target)
	if err != nil {
		return memory.Value{}, err
	}
	if err := execution.context.SetPrototype(proxy, targetHeader.Prototype); err != nil {
		return memory.Value{}, err
	}
	if err := defineData(execution.context, proxy, proxyTargetProperty, memory.RefValue(target), false, false, false); err != nil {
		return memory.Value{}, err
	}
	if err := defineData(execution.context, proxy, proxyHandlerProperty, memory.RefValue(handler), false, false, false); err != nil {
		return memory.Value{}, err
	}
	return memory.RefValue(proxy), nil
}

func builtinReflectOwnKeys(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	target := argument(arguments, 0)
	if _, err := requireObjectLike(execution.context, target, "Reflect.ownKeys target"); err != nil {
		return memory.Value{}, err
	}
	keys, err := execution.ownPropertyKeys(target)
	if err != nil {
		return memory.Value{}, err
	}
	return valuesArray(execution.context, keys)
}

func builtinReflectGetOwnPropertyDescriptor(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	return builtinObjectGetOwnPropertyDescriptor(execution, memory.Ref{}, memory.Function{}, memory.UndefinedValue(), arguments)
}

func (execution *execution) proxyRecord(value memory.Value) (target, handler memory.Value, proxy bool, err error) {
	if !value.IsRef() {
		return memory.Value{}, memory.Value{}, false, nil
	}
	kind, err := execution.context.HeapKind(value.Ref())
	if err != nil || kind != memory.HeapObject {
		return memory.Value{}, memory.Value{}, false, err
	}
	targetName, err := execution.context.NewString(proxyTargetProperty)
	if err != nil {
		return memory.Value{}, memory.Value{}, false, err
	}
	target, found, err := execution.context.GetOwnProperty(value.Ref(), targetName)
	if err != nil || !found {
		return memory.Value{}, memory.Value{}, false, err
	}
	handlerName, err := execution.context.NewString(proxyHandlerProperty)
	if err != nil {
		return memory.Value{}, memory.Value{}, false, err
	}
	handler, found, err = execution.context.GetOwnProperty(value.Ref(), handlerName)
	if err != nil || !found {
		return memory.Value{}, memory.Value{}, false, err
	}
	if !target.IsRef() || !handler.IsRef() {
		return memory.Value{}, memory.Value{}, false, fmt.Errorf("%w: corrupt Proxy record", ErrOperandType)
	}
	return target, handler, true, nil
}

func (execution *execution) proxyTrap(handler memory.Value, name string) (memory.Value, bool, error) {
	key, err := execution.context.NewString(name)
	if err != nil {
		return memory.Value{}, false, err
	}
	trap, found, err := execution.getProperty(handler, memory.RefValue(key))
	if err != nil || !found || trap.Kind() == memory.ValueUndefined || trap.Kind() == memory.ValueNull {
		return memory.Value{}, false, err
	}
	if _, err := requireCallable(execution.context, trap); err != nil {
		return memory.Value{}, false, err
	}
	return trap, true, nil
}

func (execution *execution) proxyGet(proxy, target, handler, key memory.Value) (memory.Value, bool, error) {
	trap, found, err := execution.proxyTrap(handler, "get")
	if err != nil {
		return memory.Value{}, false, err
	}
	if !found {
		return execution.getProperty(target, key)
	}
	callable, _ := requireRef(trap, "Proxy get trap")
	value, err := execution.call(callable, handler, []memory.Value{target, key, proxy}, callAny)
	return value, true, err
}

func (execution *execution) proxySet(proxy, target, handler, key, value memory.Value) error {
	trap, found, err := execution.proxyTrap(handler, "set")
	if err != nil {
		return err
	}
	if !found {
		return execution.setPropertyValue(target, key, value)
	}
	callable, _ := requireRef(trap, "Proxy set trap")
	result, err := execution.call(callable, handler, []memory.Value{target, key, value, proxy}, callAny)
	if err != nil {
		return err
	}
	accepted, err := valueTruthy(execution.context, result)
	if err != nil {
		return err
	}
	if !accepted {
		return memory.ErrReadOnlyProperty
	}
	return nil
}

func (execution *execution) proxyHas(target, handler, key memory.Value) (bool, error) {
	trap, found, err := execution.proxyTrap(handler, "has")
	if err != nil {
		return false, err
	}
	if !found {
		return execution.hasProperty(target, key)
	}
	callable, _ := requireRef(trap, "Proxy has trap")
	result, err := execution.call(callable, handler, []memory.Value{target, key}, callAny)
	if err != nil {
		return false, err
	}
	return valueTruthy(execution.context, result)
}

func (execution *execution) proxyDelete(target, handler, key memory.Value) (bool, error) {
	trap, found, err := execution.proxyTrap(handler, "deleteProperty")
	if err != nil {
		return false, err
	}
	if !found {
		return execution.deletePropertyValue(target, key)
	}
	callable, _ := requireRef(trap, "Proxy deleteProperty trap")
	result, err := execution.call(callable, handler, []memory.Value{target, key}, callAny)
	if err != nil {
		return false, err
	}
	return valueTruthy(execution.context, result)
}

func (execution *execution) proxyOwnPropertyDescriptor(target, handler, key memory.Value) (memory.Value, bool, error) {
	trap, found, err := execution.proxyTrap(handler, "getOwnPropertyDescriptor")
	if err != nil {
		return memory.Value{}, false, err
	}
	if !found {
		return execution.ordinaryOwnPropertyDescriptor(target, key)
	}
	callable, _ := requireRef(trap, "Proxy getOwnPropertyDescriptor trap")
	result, err := execution.call(callable, handler, []memory.Value{target, key}, callAny)
	if err != nil {
		return memory.Value{}, false, err
	}
	if result.Kind() == memory.ValueUndefined {
		return result, false, nil
	}
	if _, err := requireObjectLike(execution.context, result, "Proxy property descriptor"); err != nil {
		return memory.Value{}, false, err
	}
	return result, true, nil
}

func (execution *execution) ordinaryOwnPropertyDescriptor(target, key memory.Value) (memory.Value, bool, error) {
	value, err := builtinObjectGetOwnPropertyDescriptorOrdinary(execution, target, key)
	return value, value.Kind() != memory.ValueUndefined, err
}

func (execution *execution) ownPropertyKeys(target memory.Value) ([]memory.Ref, error) {
	proxyTarget, handler, proxy, err := execution.proxyRecord(target)
	if err != nil {
		return nil, err
	}
	if proxy {
		trap, found, err := execution.proxyTrap(handler, "ownKeys")
		if err != nil {
			return nil, err
		}
		if found {
			callable, _ := requireRef(trap, "Proxy ownKeys trap")
			result, err := execution.call(callable, handler, []memory.Value{proxyTarget}, callAny)
			if err != nil {
				return nil, err
			}
			return propertyKeyList(execution.context, result)
		}
		target = proxyTarget
	}
	keys, err := ownStringPropertyKeys(execution, target, true)
	if err != nil {
		return nil, err
	}
	result := make([]memory.Ref, 0, len(keys))
	for _, key := range keys {
		result = append(result, key)
	}
	header, err := execution.context.DerefObjectHeader(target.Ref())
	if err != nil {
		return nil, err
	}
	for _, property := range header.Properties {
		kind, kindErr := execution.context.HeapKind(property.Name)
		if kindErr != nil {
			return nil, kindErr
		}
		if kind == memory.HeapSymbol {
			result = append(result, property.Name)
		}
	}
	return result, nil
}

func propertyKeyList(context *TaskContext, value memory.Value) ([]memory.Ref, error) {
	if !value.IsRef() {
		return nil, fmt.Errorf("%w: Proxy ownKeys result is not an Object", ErrOperandType)
	}
	kind, err := context.HeapKind(value.Ref())
	if err != nil {
		return nil, err
	}
	if kind != memory.HeapArray {
		return nil, fmt.Errorf("%w: Proxy ownKeys result is not an Array", ErrOperandType)
	}
	array, err := context.DerefArray(value.Ref())
	if err != nil {
		return nil, err
	}
	result := make([]memory.Ref, 0, len(array.Elements))
	for _, element := range array.Elements {
		if !element.Value.IsRef() {
			return nil, fmt.Errorf("%w: Proxy ownKeys returned a non-property key", ErrOperandType)
		}
		keyKind, kindErr := context.HeapKind(element.Value.Ref())
		if kindErr != nil {
			return nil, kindErr
		}
		if keyKind != memory.HeapString && keyKind != memory.HeapSymbol {
			return nil, fmt.Errorf("%w: Proxy ownKeys returned a non-property key", ErrOperandType)
		}
		result = append(result, element.Value.Ref())
	}
	return result, nil
}

func (execution *execution) arrayReceiver(value memory.Value) (memory.Ref, error) {
	seen := make(map[memory.Ref]bool)
	for {
		if !value.IsRef() {
			return memory.Ref{}, fmt.Errorf("%w: Array receiver", ErrOperandType)
		}
		if seen[value.Ref()] {
			return memory.Ref{}, fmt.Errorf("%w: cyclic Proxy target", ErrOperandType)
		}
		seen[value.Ref()] = true
		target, _, proxy, err := execution.proxyRecord(value)
		if err != nil {
			return memory.Ref{}, err
		}
		if !proxy {
			return requireArray(execution.context, value)
		}
		value = target
	}
}
