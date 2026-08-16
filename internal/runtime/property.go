package runtime

import (
	"fmt"
	"math"
	"strconv"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func (execution *execution) getProperty(base, key memory.Value) (memory.Value, bool, error) {
	context := execution.context
	ref, err := requireRef(base, "property base")
	if err != nil {
		return memory.Value{}, false, err
	}
	kind, err := context.HeapKind(ref)
	if err != nil {
		return memory.Value{}, false, err
	}
	switch kind {
	case memory.HeapObject:
		name, err := execution.propertyName(key)
		if err != nil {
			return memory.Value{}, false, err
		}
		_, descriptor, found, err := resolveObjectProperty(context, ref, name)
		if err != nil || !found {
			return memory.Value{}, found, err
		}
		if descriptor.Kind == memory.PropertyData {
			return descriptor.Value, true, nil
		}
		if descriptor.Getter.Kind() == memory.ValueUndefined {
			return memory.UndefinedValue(), true, nil
		}
		getter, err := requireRef(descriptor.Getter, "property getter")
		if err != nil {
			return memory.Value{}, false, err
		}
		value, err := execution.call(getter, base, nil, callAny)
		return value, true, err
	case memory.HeapArray:
		index, length, err := execution.arrayPropertyKey(key)
		if err != nil {
			return memory.Value{}, false, err
		}
		if length {
			array, err := context.DerefArray(ref)
			if err != nil {
				return memory.Value{}, false, err
			}
			return memory.NumberValue(float64(array.Length)), true, nil
		}
		return context.ArrayElement(ref, index)
	case memory.HeapError:
		name, err := execution.propertyName(key)
		if err != nil {
			return memory.Value{}, false, err
		}
		keyText, err := context.DerefString(name)
		if err != nil {
			return memory.Value{}, false, err
		}
		errorObject, err := context.DerefError(ref)
		if err != nil {
			return memory.Value{}, false, err
		}
		switch keyText {
		case "name":
			nameRef, err := context.NewString(errorObject.Kind.Name())
			return memory.RefValue(nameRef), true, err
		case "message":
			return errorObject.Message, true, nil
		default:
			return memory.Value{}, false, nil
		}
	default:
		return memory.Value{}, false, fmt.Errorf("%w: HeapKind(%d) has no properties", ErrOperandType, kind)
	}
}

func (execution *execution) setPropertyValue(base, key, value memory.Value) error {
	context := execution.context
	ref, err := requireRef(base, "property base")
	if err != nil {
		return err
	}
	kind, err := context.HeapKind(ref)
	if err != nil {
		return err
	}
	switch kind {
	case memory.HeapObject:
		name, err := execution.propertyName(key)
		if err != nil {
			return err
		}
		holder, descriptor, found, err := resolveObjectProperty(context, ref, name)
		if err != nil {
			return err
		}
		if !found {
			return context.SetProperty(ref, name, value)
		}
		if descriptor.Kind == memory.PropertyAccessor {
			if descriptor.Setter.Kind() == memory.ValueUndefined {
				return memory.ErrReadOnlyProperty
			}
			setter, err := requireRef(descriptor.Setter, "property setter")
			if err != nil {
				return err
			}
			_, err = execution.call(setter, base, []memory.Value{value}, callAny)
			return err
		}
		if !descriptor.Writable {
			return memory.ErrReadOnlyProperty
		}
		if holder != ref {
			return context.SetProperty(ref, name, value)
		}
		return context.SetProperty(ref, name, value)
	case memory.HeapArray:
		index, length, err := execution.arrayPropertyKey(key)
		if err != nil {
			return err
		}
		if length {
			lengthValue, err := requireUint32(value, "Array length", true)
			if err != nil {
				return err
			}
			return context.SetArrayLength(ref, lengthValue)
		}
		return context.SetArrayElement(ref, index, value)
	default:
		return fmt.Errorf("%w: HeapKind(%d) has no properties", ErrOperandType, kind)
	}
}

func resolveObjectProperty(context *TaskContext, object, name memory.Ref) (memory.Ref, memory.Property, bool, error) {
	seen := make(map[memory.Ref]struct{})
	current := object
	for {
		if _, duplicate := seen[current]; duplicate {
			return memory.Ref{}, memory.Property{}, false, memory.ErrPrototypeCycle
		}
		seen[current] = struct{}{}
		descriptor, found, err := context.GetOwnPropertyDescriptor(current, name)
		if err != nil {
			return memory.Ref{}, memory.Property{}, false, err
		}
		if found {
			return current, descriptor, true, nil
		}
		snapshot, err := context.DerefObject(current)
		if err != nil {
			return memory.Ref{}, memory.Property{}, false, err
		}
		if snapshot.Prototype.Kind() == memory.ValueNull {
			return memory.Ref{}, memory.Property{}, false, nil
		}
		current = snapshot.Prototype.Ref()
	}
}

func (execution *execution) deletePropertyValue(base, key memory.Value) (bool, error) {
	context := execution.context
	ref, err := requireRef(base, "property base")
	if err != nil {
		return false, err
	}
	kind, err := context.HeapKind(ref)
	if err != nil {
		return false, err
	}
	switch kind {
	case memory.HeapObject:
		name, err := execution.propertyName(key)
		if err != nil {
			return false, err
		}
		if _, found, err := context.GetOwnPropertyDescriptor(ref, name); err != nil {
			return false, err
		} else if !found {
			return true, nil
		}
		return context.DeleteProperty(ref, name)
	case memory.HeapArray:
		index, length, err := execution.arrayPropertyKey(key)
		if err != nil {
			return false, err
		}
		if length {
			return false, nil
		}
		if _, err := context.DeleteArrayElement(ref, index); err != nil {
			return false, err
		}
		return true, nil
	default:
		return false, fmt.Errorf("%w: HeapKind(%d) has no properties", ErrOperandType, kind)
	}
}

func (execution *execution) propertyName(key memory.Value) (memory.Ref, error) {
	primitive, err := execution.toPrimitive(key, hintString)
	if err != nil {
		return memory.Ref{}, err
	}
	if primitive.IsRef() {
		kind, err := execution.context.HeapKind(primitive.Ref())
		if err != nil {
			return memory.Ref{}, err
		}
		if kind == memory.HeapString {
			return primitive.Ref(), nil
		}
		if kind == memory.HeapSymbol {
			return memory.Ref{}, fmt.Errorf("%w: Symbol property keys are not implemented", ErrOperandType)
		}
	}
	text, err := execution.toString(primitive)
	if err != nil {
		return memory.Ref{}, err
	}
	return execution.context.NewString(text)
}

func (execution *execution) arrayPropertyKey(key memory.Value) (index uint32, length bool, err error) {
	name, err := execution.propertyName(key)
	if err != nil {
		return 0, false, err
	}
	text, err := execution.context.DerefString(name)
	if err != nil {
		return 0, false, err
	}
	if text == "length" {
		return 0, true, nil
	}
	parsed, parseErr := strconv.ParseUint(text, 10, 32)
	if parseErr != nil || parsed == math.MaxUint32 || strconv.FormatUint(parsed, 10) != text {
		return 0, false, fmt.Errorf("%w: Array property %q is not an index", ErrOperandType, text)
	}
	return uint32(parsed), false, nil
}
