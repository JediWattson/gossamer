package runtime

import (
	"fmt"
	"math"
	"strconv"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func builtinArrayFill(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	arrayRef, err := execution.arrayReceiver(this)
	if err != nil {
		return memory.Value{}, err
	}
	array, err := execution.context.DerefArray(arrayRef)
	if err != nil {
		return memory.Value{}, err
	}
	start, err := relativeIndex(execution, argument(arguments, 1), array.Length, 0)
	if err != nil {
		return memory.Value{}, err
	}
	end, err := relativeIndex(execution, argument(arguments, 2), array.Length, array.Length)
	if err != nil {
		return memory.Value{}, err
	}
	for index := start; index < end; index++ {
		key, err := execution.context.NewString(strconv.FormatUint(uint64(index), 10))
		if err != nil {
			return memory.Value{}, err
		}
		if err := execution.setPropertyValue(this, memory.RefValue(key), argument(arguments, 0)); err != nil {
			return memory.Value{}, err
		}
	}
	return this, nil
}

func builtinArrayReverse(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, _ []memory.Value) (memory.Value, error) {
	arrayRef, err := execution.arrayReceiver(this)
	if err != nil {
		return memory.Value{}, err
	}
	array, err := execution.context.DerefArray(arrayRef)
	if err != nil {
		return memory.Value{}, err
	}
	for lower := uint32(0); lower < array.Length/2; lower++ {
		upper := array.Length - lower - 1
		lowerKey, err := execution.context.NewString(strconv.FormatUint(uint64(lower), 10))
		if err != nil {
			return memory.Value{}, err
		}
		upperKey, err := execution.context.NewString(strconv.FormatUint(uint64(upper), 10))
		if err != nil {
			return memory.Value{}, err
		}
		lowerValue, lowerExists, err := arrayProperty(execution, this, memory.RefValue(lowerKey))
		if err != nil {
			return memory.Value{}, err
		}
		upperValue, upperExists, err := arrayProperty(execution, this, memory.RefValue(upperKey))
		if err != nil {
			return memory.Value{}, err
		}
		switch {
		case lowerExists && upperExists:
			if err := execution.setPropertyValue(this, memory.RefValue(lowerKey), upperValue); err != nil {
				return memory.Value{}, err
			}
			if err := execution.setPropertyValue(this, memory.RefValue(upperKey), lowerValue); err != nil {
				return memory.Value{}, err
			}
		case upperExists:
			if err := execution.setPropertyValue(this, memory.RefValue(lowerKey), upperValue); err != nil {
				return memory.Value{}, err
			}
			if _, err := execution.deletePropertyValue(this, memory.RefValue(upperKey)); err != nil {
				return memory.Value{}, err
			}
		case lowerExists:
			if _, err := execution.deletePropertyValue(this, memory.RefValue(lowerKey)); err != nil {
				return memory.Value{}, err
			}
			if err := execution.setPropertyValue(this, memory.RefValue(upperKey), lowerValue); err != nil {
				return memory.Value{}, err
			}
		}
	}
	return this, nil
}

func builtinArrayFlat(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	arrayRef, err := execution.arrayReceiver(this)
	if err != nil {
		return memory.Value{}, err
	}
	depth := int64(1)
	if value := argument(arguments, 0); value.Kind() != memory.ValueUndefined {
		number, err := execution.toNumber(value)
		if err != nil {
			return memory.Value{}, err
		}
		switch {
		case math.IsNaN(number), number <= 0:
			depth = 0
		case math.IsInf(number, 1), number >= math.MaxInt64:
			depth = math.MaxInt64
		default:
			depth = int64(math.Trunc(number))
		}
	}

	result, err := execution.context.NewArray(0)
	if err != nil {
		return memory.Value{}, err
	}
	next := uint64(0)
	if err := flattenArrayInto(execution, result, arrayRef, depth, &next); err != nil {
		return memory.Value{}, err
	}
	return memory.RefValue(result), nil
}

func builtinArrayFlatMap(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	arrayRef, err := execution.arrayReceiver(this)
	if err != nil {
		return memory.Value{}, err
	}
	callback, err := requireCallable(execution.context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	snapshot, err := execution.context.DerefArray(arrayRef)
	if err != nil {
		return memory.Value{}, err
	}
	result, err := execution.context.NewArray(0)
	if err != nil {
		return memory.Value{}, err
	}
	next := uint64(0)
	thisArg := argument(arguments, 1)
	for _, element := range snapshot.Elements {
		mapped, err := execution.call(callback, thisArg, []memory.Value{
			element.Value, memory.NumberValue(float64(element.Index)), this,
		}, callAny)
		if err != nil {
			return memory.Value{}, err
		}
		if mapped.IsRef() {
			kind, err := execution.context.HeapKind(mapped.Ref())
			if err != nil {
				return memory.Value{}, err
			}
			if kind == memory.HeapArray {
				if err := flattenArrayInto(execution, result, mapped.Ref(), 0, &next); err != nil {
					return memory.Value{}, err
				}
				continue
			}
		}
		if err := appendFlatValue(execution, result, mapped, &next); err != nil {
			return memory.Value{}, err
		}
	}
	return memory.RefValue(result), nil
}

func flattenArrayInto(execution *execution, result, source memory.Ref, depth int64, next *uint64) error {
	array, err := execution.context.DerefArray(source)
	if err != nil {
		return err
	}
	for _, element := range array.Elements {
		value := element.Value
		if depth > 0 && value.IsRef() {
			kind, err := execution.context.HeapKind(value.Ref())
			if err != nil {
				return err
			}
			if kind == memory.HeapArray {
				nextDepth := depth - 1
				if depth == math.MaxInt64 {
					nextDepth = depth
				}
				if err := flattenArrayInto(execution, result, value.Ref(), nextDepth, next); err != nil {
					return err
				}
				continue
			}
		}
		if err := appendFlatValue(execution, result, value, next); err != nil {
			return err
		}
	}
	return nil
}

func appendFlatValue(execution *execution, result memory.Ref, value memory.Value, next *uint64) error {
	if *next >= math.MaxUint32 {
		return fmt.Errorf("%w: flattened Array result exceeds uint32 length", memory.ErrInvalidIndex)
	}
	if err := execution.context.SetArrayElement(result, uint32(*next), value); err != nil {
		return err
	}
	*next++
	return nil
}

func arrayProperty(execution *execution, receiver, key memory.Value) (memory.Value, bool, error) {
	found, err := execution.hasProperty(receiver, key)
	if err != nil || !found {
		return memory.Value{}, found, err
	}
	value, _, err := execution.getProperty(receiver, key)
	return value, true, err
}

func builtinArrayConcat(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	receiver, err := requireArray(execution.context, this)
	if err != nil {
		return memory.Value{}, err
	}
	result, err := execution.context.NewArray(0)
	if err != nil {
		return memory.Value{}, err
	}
	offset := uint32(0)
	appendValue := func(value memory.Value) error {
		if value.IsRef() {
			kind, err := execution.context.HeapKind(value.Ref())
			if err != nil {
				return err
			}
			if kind == memory.HeapArray {
				array, err := execution.context.DerefArray(value.Ref())
				if err != nil {
					return err
				}
				if uint64(offset)+uint64(array.Length) > math.MaxUint32 {
					return ErrOperandType
				}
				for _, element := range array.Elements {
					if err := execution.context.SetArrayElement(result, offset+element.Index, element.Value); err != nil {
						return err
					}
				}
				offset += array.Length
				return execution.context.SetArrayLength(result, offset)
			}
		}
		if offset == math.MaxUint32 {
			return ErrOperandType
		}
		if err := execution.context.SetArrayElement(result, offset, value); err != nil {
			return err
		}
		offset++
		return nil
	}
	if err := appendValue(memory.RefValue(receiver)); err != nil {
		return memory.Value{}, err
	}
	for _, value := range arguments {
		if err := appendValue(value); err != nil {
			return memory.Value{}, err
		}
	}
	return memory.RefValue(result), nil
}

func builtinArrayShift(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, _ []memory.Value) (memory.Value, error) {
	arrayRef, err := requireArray(execution.context, this)
	if err != nil {
		return memory.Value{}, err
	}
	array, err := execution.context.DerefArray(arrayRef)
	if err != nil {
		return memory.Value{}, err
	}
	if array.Length == 0 {
		return memory.UndefinedValue(), nil
	}
	first, found, err := execution.context.ArrayElement(arrayRef, 0)
	if err != nil {
		return memory.Value{}, err
	}
	if err := clearArrayElements(execution.context, arrayRef, array); err != nil {
		return memory.Value{}, err
	}
	if err := execution.context.SetArrayLength(arrayRef, array.Length-1); err != nil {
		return memory.Value{}, err
	}
	for _, element := range array.Elements {
		if element.Index == 0 {
			continue
		}
		if err := execution.context.SetArrayElement(arrayRef, element.Index-1, element.Value); err != nil {
			return memory.Value{}, err
		}
	}
	if !found {
		return memory.UndefinedValue(), nil
	}
	return first, nil
}

func builtinArrayUnshift(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	arrayRef, err := requireArray(execution.context, this)
	if err != nil {
		return memory.Value{}, err
	}
	array, err := execution.context.DerefArray(arrayRef)
	if err != nil {
		return memory.Value{}, err
	}
	if uint64(array.Length)+uint64(len(arguments)) > math.MaxUint32 {
		return memory.Value{}, ErrOperandType
	}
	if err := clearArrayElements(execution.context, arrayRef, array); err != nil {
		return memory.Value{}, err
	}
	newLength := array.Length + uint32(len(arguments))
	if err := execution.context.SetArrayLength(arrayRef, newLength); err != nil {
		return memory.Value{}, err
	}
	for index, value := range arguments {
		if err := execution.context.SetArrayElement(arrayRef, uint32(index), value); err != nil {
			return memory.Value{}, err
		}
	}
	offset := uint32(len(arguments))
	for _, element := range array.Elements {
		if err := execution.context.SetArrayElement(arrayRef, offset+element.Index, element.Value); err != nil {
			return memory.Value{}, err
		}
	}
	return memory.NumberValue(float64(newLength)), nil
}

func builtinArraySplice(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	arrayRef, err := requireArray(execution.context, this)
	if err != nil {
		return memory.Value{}, err
	}
	array, err := execution.context.DerefArray(arrayRef)
	if err != nil {
		return memory.Value{}, err
	}
	start, err := relativeIndex(execution, argument(arguments, 0), array.Length, 0)
	if err != nil {
		return memory.Value{}, err
	}
	deleteCount := array.Length - start
	if len(arguments) > 1 {
		count, err := integerArgument(execution, arguments, 1, 0)
		if err != nil {
			return memory.Value{}, err
		}
		if count < 0 {
			count = 0
		}
		if uint64(count) < uint64(deleteCount) {
			deleteCount = uint32(count)
		}
	}
	items := arguments[min(2, len(arguments)):]
	newLength64 := uint64(array.Length) - uint64(deleteCount) + uint64(len(items))
	if newLength64 > math.MaxUint32 {
		return memory.Value{}, ErrOperandType
	}
	removed, err := execution.context.NewArray(deleteCount)
	if err != nil {
		return memory.Value{}, err
	}
	for _, element := range array.Elements {
		if element.Index >= start && element.Index < start+deleteCount {
			if err := execution.context.SetArrayElement(removed, element.Index-start, element.Value); err != nil {
				return memory.Value{}, err
			}
		}
	}
	if err := clearArrayElements(execution.context, arrayRef, array); err != nil {
		return memory.Value{}, err
	}
	newLength := uint32(newLength64)
	if err := execution.context.SetArrayLength(arrayRef, newLength); err != nil {
		return memory.Value{}, err
	}
	for _, element := range array.Elements {
		switch {
		case element.Index < start:
			if err := execution.context.SetArrayElement(arrayRef, element.Index, element.Value); err != nil {
				return memory.Value{}, err
			}
		case element.Index >= start+deleteCount:
			destination := uint64(element.Index) - uint64(deleteCount) + uint64(len(items))
			if err := execution.context.SetArrayElement(arrayRef, uint32(destination), element.Value); err != nil {
				return memory.Value{}, err
			}
		}
	}
	for index, value := range items {
		if err := execution.context.SetArrayElement(arrayRef, start+uint32(index), value); err != nil {
			return memory.Value{}, err
		}
	}
	return memory.RefValue(removed), nil
}

func clearArrayElements(context *TaskContext, ref memory.Ref, array memory.Array) error {
	for _, element := range array.Elements {
		if _, err := context.DeleteArrayElement(ref, element.Index); err != nil {
			return err
		}
	}
	return nil
}
