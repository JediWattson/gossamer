package runtime

import (
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
	keys, err := ownStringPropertyKeys(execution, argument(arguments, 0), true)
	if err != nil {
		return memory.Value{}, err
	}
	return valuesArray(execution.context, keys)
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
