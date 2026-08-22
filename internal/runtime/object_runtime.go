package runtime

import (
	"fmt"
	"math"
	"strconv"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func builtinObjectAssign(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	target := argument(arguments, 0)
	targetRef, err := requireObjectLike(execution.context, target, "Object.assign target")
	if err != nil {
		return memory.Value{}, err
	}
	for _, source := range arguments[1:] {
		if source.Kind() == memory.ValueUndefined || source.Kind() == memory.ValueNull {
			continue
		}
		keys, err := enumerableOwnPropertyKeys(execution, source)
		if err != nil {
			return memory.Value{}, err
		}
		for _, key := range keys {
			value, found, err := execution.getProperty(source, memory.RefValue(key))
			if err != nil {
				return memory.Value{}, err
			}
			if found {
				if err := execution.setPropertyValue(memory.RefValue(targetRef), memory.RefValue(key), value); err != nil {
					return memory.Value{}, err
				}
			}
		}
	}
	return memory.RefValue(targetRef), nil
}

func builtinObjectGetOwnPropertyNames(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	keys, err := execution.ownPropertyKeys(argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	strings := make([]memory.Ref, 0, len(keys))
	for _, key := range keys {
		kind, kindErr := execution.context.HeapKind(key)
		if kindErr != nil {
			return memory.Value{}, kindErr
		}
		if kind == memory.HeapString {
			strings = append(strings, key)
		}
	}
	return valuesArray(execution.context, strings)
}

func builtinObjectIs(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	left := argument(arguments, 0)
	right := argument(arguments, 1)
	if left.Kind() == memory.ValueNumber && right.Kind() == memory.ValueNumber {
		if math.IsNaN(left.Number()) && math.IsNaN(right.Number()) {
			return memory.BoolValue(true), nil
		}
		if left.Number() == 0 && right.Number() == 0 {
			return memory.BoolValue(math.Signbit(left.Number()) == math.Signbit(right.Number())), nil
		}
	}
	equal, err := strictEqual(execution.context, left, right)
	return memory.BoolValue(equal), err
}

func builtinObjectEntries(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	return builtinObjectEnumerableValues(execution, argument(arguments, 0), true)
}

func builtinObjectValues(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	return builtinObjectEnumerableValues(execution, argument(arguments, 0), false)
}

func builtinObjectEnumerableValues(execution *execution, target memory.Value, entries bool) (memory.Value, error) {
	if _, err := requireObjectLike(execution.context, target, "Object enumerable target"); err != nil {
		return memory.Value{}, err
	}
	keys, err := enumerableOwnPropertyKeys(execution, target)
	if err != nil {
		return memory.Value{}, err
	}
	values := make([]memory.Value, 0, len(keys))
	for _, key := range keys {
		kind, kindErr := execution.context.HeapKind(key)
		if kindErr != nil {
			return memory.Value{}, kindErr
		}
		if kind != memory.HeapString {
			continue
		}
		value, _, propertyErr := execution.getProperty(target, memory.RefValue(key))
		if propertyErr != nil {
			return memory.Value{}, propertyErr
		}
		if !entries {
			values = append(values, value)
			continue
		}
		pair, pairErr := execution.context.NewArray(2)
		if pairErr != nil {
			return memory.Value{}, pairErr
		}
		if pairErr := execution.context.SetArrayElement(pair, 0, memory.RefValue(key)); pairErr != nil {
			return memory.Value{}, pairErr
		}
		if pairErr := execution.context.SetArrayElement(pair, 1, value); pairErr != nil {
			return memory.Value{}, pairErr
		}
		values = append(values, memory.RefValue(pair))
	}
	result, err := execution.context.NewArray(uint32(len(values)))
	if err != nil {
		return memory.Value{}, err
	}
	for index, value := range values {
		if err := execution.context.SetArrayElement(result, uint32(index), value); err != nil {
			return memory.Value{}, err
		}
	}
	return memory.RefValue(result), nil
}

func builtinObjectFreeze(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	target := argument(arguments, 0)
	if !target.IsRef() {
		return target, nil
	}
	if proxyTarget, _, proxy, err := execution.proxyRecord(target); err != nil {
		return memory.Value{}, err
	} else if proxy {
		target = proxyTarget
	}
	header, err := execution.context.DerefObjectHeader(target.Ref())
	if err != nil {
		return argument(arguments, 0), nil
	}
	for _, property := range header.Properties {
		descriptor := property
		descriptor.Configurable = false
		if descriptor.Kind == memory.PropertyData {
			descriptor.Writable = false
		}
		if err := execution.context.DefineProperty(target.Ref(), property.Name, descriptor); err != nil {
			return memory.Value{}, err
		}
	}
	if err := execution.context.SetObjectIntegrity(target.Ref(), true, true); err != nil {
		return memory.Value{}, err
	}
	return argument(arguments, 0), nil
}

func builtinObjectFromEntries(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	source, err := execution.arrayReceiver(argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	array, err := execution.context.DerefArray(source)
	if err != nil {
		return memory.Value{}, err
	}
	result, err := execution.context.NewHeapObject()
	if err != nil {
		return memory.Value{}, err
	}
	for _, element := range array.Elements {
		pair, err := execution.arrayReceiver(element.Value)
		if err != nil {
			return memory.Value{}, fmt.Errorf("%w: Object.fromEntries entry", ErrOperandType)
		}
		key, found, err := execution.context.ArrayElement(pair, 0)
		if err != nil || !found {
			return memory.Value{}, fmt.Errorf("%w: Object.fromEntries key", ErrOperandType)
		}
		value, found, err := execution.context.ArrayElement(pair, 1)
		if err != nil || !found {
			return memory.Value{}, fmt.Errorf("%w: Object.fromEntries value", ErrOperandType)
		}
		name, err := execution.propertyName(key)
		if err != nil {
			return memory.Value{}, err
		}
		if err := execution.context.DefineProperty(result, name, memory.DataProperty(value, true, true, true)); err != nil {
			return memory.Value{}, err
		}
	}
	return memory.RefValue(result), nil
}

func builtinObjectHasOwn(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	key, err := execution.propertyName(argument(arguments, 1))
	if err != nil {
		return memory.Value{}, err
	}
	found, err := execution.hasOwnProperty(argument(arguments, 0), key)
	return memory.BoolValue(found), err
}

func builtinObjectGetOwnPropertyDescriptors(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	target := argument(arguments, 0)
	if _, err := requireObjectLike(execution.context, target, "Object.getOwnPropertyDescriptors target"); err != nil {
		return memory.Value{}, err
	}
	keys, err := execution.ownPropertyKeys(target)
	if err != nil {
		return memory.Value{}, err
	}
	result, err := execution.context.NewHeapObject()
	if err != nil {
		return memory.Value{}, err
	}
	for _, key := range keys {
		descriptor, err := builtinObjectGetOwnPropertyDescriptor(
			execution,
			memory.Ref{},
			memory.Function{},
			memory.UndefinedValue(),
			[]memory.Value{target, memory.RefValue(key)},
		)
		if err != nil {
			return memory.Value{}, err
		}
		if descriptor.Kind() == memory.ValueUndefined {
			continue
		}
		if err := execution.context.DefineProperty(result, key, memory.DataProperty(descriptor, true, true, true)); err != nil {
			return memory.Value{}, err
		}
	}
	return memory.RefValue(result), nil
}

func builtinObjectIsFrozen(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	target := argument(arguments, 0)
	if !target.IsRef() {
		return memory.BoolValue(true), nil
	}
	header, err := execution.context.DerefObjectHeader(target.Ref())
	if err != nil {
		// Non-object heap values are primitives for Object.isFrozen.
		return memory.BoolValue(true), nil
	}
	if !header.NonExtensible {
		return memory.BoolValue(false), nil
	}
	for _, property := range header.Properties {
		if property.Configurable || (property.Kind == memory.PropertyData && property.Writable) {
			return memory.BoolValue(false), nil
		}
	}
	if kind, kindErr := execution.context.HeapKind(target.Ref()); kindErr != nil {
		return memory.Value{}, kindErr
	} else if kind == memory.HeapArray {
		array, arrayErr := execution.context.DerefArray(target.Ref())
		if arrayErr != nil {
			return memory.Value{}, arrayErr
		}
		if len(array.Elements) != 0 {
			return memory.BoolValue(false), nil
		}
	}
	return memory.BoolValue(true), nil
}

func builtinObjectPrototypeHasOwnProperty(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	key, err := execution.propertyName(argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	found, err := execution.hasOwnProperty(this, key)
	return memory.BoolValue(found), err
}

func builtinArrayIsArray(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	value := argument(arguments, 0)
	if !value.IsRef() {
		return memory.BoolValue(false), nil
	}
	if target, _, proxy, err := execution.proxyRecord(value); err != nil {
		return memory.Value{}, err
	} else if proxy {
		value = target
	}
	kind, err := execution.context.HeapKind(value.Ref())
	if err != nil {
		return memory.Value{}, err
	}
	return memory.BoolValue(kind == memory.HeapArray), nil
}

func (execution *execution) hasOwnProperty(value memory.Value, key memory.Ref) (bool, error) {
	if !value.IsRef() {
		return false, nil
	}
	if target, handler, proxy, err := execution.proxyRecord(value); err != nil {
		return false, err
	} else if proxy {
		_, found, err := execution.proxyOwnPropertyDescriptor(target, handler, memory.RefValue(key))
		return found, err
	}
	kind, err := execution.context.HeapKind(value.Ref())
	if err != nil {
		return false, err
	}
	switch kind {
	case memory.HeapArray:
		index, length, indexed, _, err := execution.arrayPropertyKey(memory.RefValue(key))
		if err != nil {
			return false, err
		}
		if length {
			return true, nil
		}
		if indexed {
			_, found, err := execution.context.ArrayElement(value.Ref(), index)
			return found, err
		}
	case memory.HeapString:
		text, stringKey, err := execution.stringPropertyName(key)
		if err != nil || !stringKey {
			return false, err
		}
		if text == "length" {
			return true, nil
		}
		index, parseErr := strconv.ParseUint(text, 10, 32)
		if parseErr != nil || strconv.FormatUint(index, 10) != text {
			return false, nil
		}
		contents, err := execution.context.DerefString(value.Ref())
		return index < uint64(len([]rune(contents))), err
	case memory.HeapBigInt, memory.HeapSymbol:
		return false, nil
	}
	if _, err := execution.context.DerefObjectHeader(value.Ref()); err != nil {
		return false, err
	}
	_, found, err := execution.context.GetOwnPropertyDescriptor(value.Ref(), key)
	return found, err
}

func enumerableOwnPropertyKeys(execution *execution, value memory.Value) ([]memory.Ref, error) {
	if !value.IsRef() {
		return nil, nil
	}
	if target, handler, proxy, err := execution.proxyRecord(value); err != nil {
		return nil, err
	} else if proxy {
		keys, err := execution.ownPropertyKeys(value)
		if err != nil {
			return nil, err
		}
		result := make([]memory.Ref, 0, len(keys))
		for _, key := range keys {
			descriptor, found, descriptorErr := execution.proxyOwnPropertyDescriptor(target, handler, memory.RefValue(key))
			if descriptorErr != nil {
				return nil, descriptorErr
			}
			if !found {
				continue
			}
			enumerableName, nameErr := execution.context.NewString("enumerable")
			if nameErr != nil {
				return nil, nameErr
			}
			enumerable, _, propertyErr := execution.getProperty(descriptor, memory.RefValue(enumerableName))
			if propertyErr != nil {
				return nil, propertyErr
			}
			visible, truthErr := valueTruthy(execution.context, enumerable)
			if truthErr != nil {
				return nil, truthErr
			}
			if visible {
				result = append(result, key)
			}
		}
		return result, nil
	}
	kind, err := execution.context.HeapKind(value.Ref())
	if err != nil {
		return nil, err
	}
	if kind == memory.HeapString {
		text, err := execution.context.DerefString(value.Ref())
		if err != nil {
			return nil, err
		}
		keys := make([]memory.Ref, 0, len([]rune(text)))
		for index := range []rune(text) {
			key, err := execution.context.NewString(strconv.Itoa(index))
			if err != nil {
				return nil, err
			}
			keys = append(keys, key)
		}
		return keys, nil
	}
	if _, err := execution.context.DerefObjectHeader(value.Ref()); err != nil {
		return nil, nil
	}
	strings, err := ownStringPropertyKeys(execution, value, false)
	if err != nil {
		return nil, err
	}
	header, err := execution.context.DerefObjectHeader(value.Ref())
	if err != nil {
		return nil, err
	}
	symbols := make([]memory.Ref, 0)
	for _, property := range header.Properties {
		if !property.Enumerable {
			continue
		}
		kind, err := execution.context.HeapKind(property.Name)
		if err != nil {
			return nil, err
		}
		if kind == memory.HeapSymbol {
			symbols = append(symbols, property.Name)
		}
	}
	return append(strings, symbols...), nil
}

func ownStringPropertyKeys(execution *execution, value memory.Value, includeNonEnumerable bool) ([]memory.Ref, error) {
	if !value.IsRef() {
		return nil, ErrOperandType
	}
	kind, err := execution.context.HeapKind(value.Ref())
	if err != nil {
		return nil, err
	}
	keys := make([]memory.Ref, 0)
	if kind == memory.HeapString {
		text, err := execution.context.DerefString(value.Ref())
		if err != nil {
			return nil, err
		}
		for index := range []rune(text) {
			key, err := execution.context.NewString(strconv.Itoa(index))
			if err != nil {
				return nil, err
			}
			keys = append(keys, key)
		}
		if includeNonEnumerable {
			length, err := execution.context.NewString("length")
			if err != nil {
				return nil, err
			}
			keys = append(keys, length)
		}
		return keys, nil
	}
	if kind == memory.HeapArray {
		array, err := execution.context.DerefArray(value.Ref())
		if err != nil {
			return nil, err
		}
		for _, element := range array.Elements {
			key, err := execution.context.NewString(strconv.FormatUint(uint64(element.Index), 10))
			if err != nil {
				return nil, err
			}
			keys = append(keys, key)
		}
		if includeNonEnumerable {
			length, err := execution.context.NewString("length")
			if err != nil {
				return nil, err
			}
			keys = append(keys, length)
		}
	}
	header, err := execution.context.DerefObjectHeader(value.Ref())
	if err != nil {
		return nil, err
	}
	for _, property := range header.Properties {
		if !includeNonEnumerable && !property.Enumerable {
			continue
		}
		kind, err := execution.context.HeapKind(property.Name)
		if err != nil {
			return nil, err
		}
		if kind == memory.HeapString {
			keys = append(keys, property.Name)
		}
	}
	return keys, nil
}

func valuesArray(context *TaskContext, values []memory.Ref) (memory.Value, error) {
	array, err := context.NewArray(uint32(len(values)))
	if err != nil {
		return memory.Value{}, err
	}
	for index, value := range values {
		if err := context.SetArrayElement(array, uint32(index), memory.RefValue(value)); err != nil {
			return memory.Value{}, err
		}
	}
	return memory.RefValue(array), nil
}
