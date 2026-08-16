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
	case memory.HeapObject, memory.HeapFunction, memory.HeapPromise, memory.HeapMap, memory.HeapSet, memory.HeapIterator:
		name, err := execution.propertyName(key)
		if err != nil {
			return memory.Value{}, false, err
		}
		return execution.getNamedProperty(base, ref, name)
	case memory.HeapArray:
		index, length, indexed, name, err := execution.arrayPropertyKey(key)
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
		if indexed {
			return context.ArrayElement(ref, index)
		}
		return execution.getNamedProperty(base, ref, name)
	case memory.HeapString:
		name, err := execution.propertyName(key)
		if err != nil {
			return memory.Value{}, false, err
		}
		keyText, err := context.DerefString(name)
		if err != nil {
			return memory.Value{}, false, err
		}
		text, err := context.DerefString(ref)
		if err != nil {
			return memory.Value{}, false, err
		}
		units := []rune(text)
		if keyText == "length" {
			return memory.NumberValue(float64(len(units))), true, nil
		}
		if parsed, parseErr := strconv.ParseUint(keyText, 10, 32); parseErr == nil && strconv.FormatUint(parsed, 10) == keyText && parsed < uint64(len(units)) {
			character := string(units[parsed])
			value, err := context.NewString(character)
			return memory.RefValue(value), true, err
		}
		if context.intrinsics == nil {
			return memory.Value{}, false, nil
		}
		return execution.getNamedProperty(base, context.intrinsics.StringPrototype, name)
	case memory.HeapError:
		name, err := execution.propertyName(key)
		if err != nil {
			return memory.Value{}, false, err
		}
		if value, found, err := execution.getNamedProperty(base, ref, name); err != nil || found {
			return value, found, err
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

func (execution *execution) hasProperty(base, key memory.Value) (bool, error) {
	_, found, err := execution.getProperty(base, key)
	return found, err
}

func (execution *execution) instanceOf(value, constructor memory.Value) (bool, error) {
	constructorRef, err := requireRef(constructor, "instanceof constructor")
	if err != nil {
		return false, err
	}
	descriptor, err := execution.context.DerefFunction(constructorRef)
	if err != nil {
		return false, ErrNotCallable
	}
	_ = descriptor
	prototypeName, err := execution.context.NewString("prototype")
	if err != nil {
		return false, err
	}
	prototype, found, err := execution.getProperty(constructor, memory.RefValue(prototypeName))
	if err != nil {
		return false, err
	}
	if !found || !prototype.IsRef() {
		return false, ErrOperandType
	}
	if !value.IsRef() {
		return false, nil
	}
	header, err := execution.context.DerefObjectHeader(value.Ref())
	if err != nil {
		return false, nil
	}
	seen := make(map[memory.Ref]struct{})
	for header.Prototype.IsRef() {
		current := header.Prototype.Ref()
		if current == prototype.Ref() {
			return true, nil
		}
		if _, duplicate := seen[current]; duplicate {
			return false, memory.ErrPrototypeCycle
		}
		seen[current] = struct{}{}
		header, err = execution.context.DerefObjectHeader(current)
		if err != nil {
			return false, err
		}
	}
	return false, nil
}

func (execution *execution) getNamedProperty(base memory.Value, ref, name memory.Ref) (memory.Value, bool, error) {
	_, descriptor, found, err := resolveObjectProperty(execution.context, ref, name)
	if err != nil {
		return memory.Value{}, found, err
	}
	if !found {
		nameText, nameErr := execution.context.DerefString(name)
		if nameErr != nil {
			return memory.Value{}, false, nameErr
		}
		for _, interceptor := range execution.interpreter.propertyInterceptors() {
			if interceptor.Get == nil {
				continue
			}
			value, interceptedFound, handled, interceptErr := interceptor.Get(execution.context, ref, nameText)
			if interceptErr != nil || handled {
				return value, interceptedFound, interceptErr
			}
		}
		return memory.Value{}, false, nil
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
	case memory.HeapObject, memory.HeapFunction, memory.HeapPromise, memory.HeapMap, memory.HeapSet, memory.HeapError, memory.HeapIterator:
		name, err := execution.propertyName(key)
		if err != nil {
			return err
		}
		holder, descriptor, found, err := resolveObjectProperty(context, ref, name)
		if err != nil {
			return err
		}
		if !found {
			nameText, nameErr := context.DerefString(name)
			if nameErr != nil {
				return nameErr
			}
			for _, interceptor := range execution.interpreter.propertyInterceptors() {
				if interceptor.Set == nil {
					continue
				}
				handled, interceptErr := interceptor.Set(context, ref, nameText, value)
				if interceptErr != nil || handled {
					return interceptErr
				}
			}
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
		index, length, indexed, name, err := execution.arrayPropertyKey(key)
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
		if indexed {
			return context.SetArrayElement(ref, index, value)
		}
		return execution.setNamedProperty(base, ref, name, value)
	case memory.HeapString:
		return memory.ErrReadOnlyProperty
	default:
		return fmt.Errorf("%w: HeapKind(%d) has no properties", ErrOperandType, kind)
	}
}

func (execution *execution) setNamedProperty(base memory.Value, ref, name memory.Ref, value memory.Value) error {
	context := execution.context
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
	_ = holder
	return context.SetProperty(ref, name, value)
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
		snapshot, err := context.DerefObjectHeader(current)
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
	case memory.HeapObject, memory.HeapFunction, memory.HeapPromise, memory.HeapMap, memory.HeapSet, memory.HeapError, memory.HeapIterator:
		name, err := execution.propertyName(key)
		if err != nil {
			return false, err
		}
		if _, found, err := context.GetOwnPropertyDescriptor(ref, name); err != nil {
			return false, err
		} else if !found {
			nameText, nameErr := context.DerefString(name)
			if nameErr != nil {
				return false, nameErr
			}
			for _, interceptor := range execution.interpreter.propertyInterceptors() {
				if interceptor.Delete == nil {
					continue
				}
				deleted, handled, interceptErr := interceptor.Delete(context, ref, nameText)
				if interceptErr != nil || handled {
					return deleted, interceptErr
				}
			}
			return true, nil
		}
		return context.DeleteProperty(ref, name)
	case memory.HeapArray:
		index, length, indexed, name, err := execution.arrayPropertyKey(key)
		if err != nil {
			return false, err
		}
		if length {
			return false, nil
		}
		if indexed {
			if _, err := context.DeleteArrayElement(ref, index); err != nil {
				return false, err
			}
			return true, nil
		}
		if _, found, err := context.GetOwnPropertyDescriptor(ref, name); err != nil {
			return false, err
		} else if !found {
			return true, nil
		}
		return context.DeleteProperty(ref, name)
	case memory.HeapString:
		return false, nil
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

func (execution *execution) arrayPropertyKey(key memory.Value) (index uint32, length, indexed bool, name memory.Ref, err error) {
	name, err = execution.propertyName(key)
	if err != nil {
		return 0, false, false, memory.Ref{}, err
	}
	text, err := execution.context.DerefString(name)
	if err != nil {
		return 0, false, false, memory.Ref{}, err
	}
	if text == "length" {
		return 0, true, false, name, nil
	}
	parsed, parseErr := strconv.ParseUint(text, 10, 32)
	if parseErr != nil || parsed == math.MaxUint32 || strconv.FormatUint(parsed, 10) != text {
		return 0, false, false, name, nil
	}
	return uint32(parsed), false, true, name, nil
}
