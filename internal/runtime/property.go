package runtime

import (
	"fmt"
	"math"
	"strconv"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func (execution *execution) getProperty(base, key memory.Value) (memory.Value, bool, error) {
	context := execution.context
	if base.Kind() == memory.ValueNumber {
		name, err := execution.propertyName(key)
		if err != nil {
			return memory.Value{}, false, err
		}
		if context.intrinsics == nil {
			return memory.Value{}, false, nil
		}
		return execution.getNamedProperty(base, context.intrinsics.NumberPrototype, name)
	}
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
	case memory.HeapTypedArray:
		index, length, indexed, name, err := execution.arrayPropertyKey(key)
		if err != nil {
			return memory.Value{}, false, err
		}
		view, err := context.DerefTypedArray(ref)
		if err != nil {
			return memory.Value{}, false, err
		}
		if length {
			return memory.NumberValue(float64(view.Length)), true, nil
		}
		if indexed {
			if uint64(index) >= view.Length {
				return memory.Value{}, false, nil
			}
			value, err := context.ReadTypedArrayElement(ref, uint64(index))
			return memory.NumberValue(value), true, err
		}
		nameText, stringKey, err := execution.stringPropertyName(name)
		if err != nil {
			return memory.Value{}, false, err
		}
		if stringKey {
			switch nameText {
			case "byteLength":
				return memory.NumberValue(float64(view.Length * typedArrayElementSize(view.Element))), true, nil
			case "byteOffset":
				return memory.NumberValue(float64(view.ByteOffset)), true, nil
			case "buffer":
				return memory.RefValue(view.Buffer), true, nil
			}
			for _, interceptor := range execution.interpreter.propertyInterceptors() {
				if interceptor.Get == nil {
					continue
				}
				value, found, handled, interceptErr := interceptor.Get(context, ref, nameText)
				if interceptErr != nil || handled {
					return value, found, interceptErr
				}
			}
		}
		return memory.Value{}, false, nil
	case memory.HeapString:
		name, err := execution.propertyName(key)
		if err != nil {
			return memory.Value{}, false, err
		}
		keyText, stringKey, err := execution.stringPropertyName(name)
		if err != nil {
			return memory.Value{}, false, err
		}
		if stringKey {
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
		}
		if context.intrinsics == nil {
			return memory.Value{}, false, nil
		}
		return execution.getNamedProperty(base, context.intrinsics.StringPrototype, name)
	case memory.HeapSymbol:
		name, err := execution.propertyName(key)
		if err != nil {
			return memory.Value{}, false, err
		}
		if context.intrinsics == nil {
			return memory.Value{}, false, nil
		}
		return execution.getNamedProperty(base, context.intrinsics.SymbolPrototype, name)
	case memory.HeapError:
		name, err := execution.propertyName(key)
		if err != nil {
			return memory.Value{}, false, err
		}
		if value, found, err := execution.getNamedProperty(base, ref, name); err != nil || found {
			return value, found, err
		}
		keyText, stringKey, err := execution.stringPropertyName(name)
		if err != nil {
			return memory.Value{}, false, err
		}
		if !stringKey {
			return memory.Value{}, false, nil
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
	case memory.HeapRegExp:
		name, err := execution.propertyName(key)
		if err != nil {
			return memory.Value{}, false, err
		}
		nameText, stringKey, err := execution.stringPropertyName(name)
		if err != nil {
			return memory.Value{}, false, err
		}
		if stringKey {
			expression, err := context.DerefRegExp(ref)
			if err != nil {
				return memory.Value{}, false, err
			}
			switch nameText {
			case "source":
				return memory.RefValue(expression.Pattern), true, nil
			case "flags":
				flags, err := context.NewString(expression.Flags.String())
				return memory.RefValue(flags), true, err
			case "lastIndex":
				return memory.NumberValue(float64(expression.LastIndex)), true, nil
			}
		}
		if context.intrinsics == nil {
			return memory.Value{}, false, nil
		}
		return execution.getNamedProperty(base, context.intrinsics.RegExpPrototype, name)
	case memory.HeapWeakMap, memory.HeapWeakSet:
		name, err := execution.propertyName(key)
		if err != nil {
			return memory.Value{}, false, err
		}
		if context.intrinsics == nil {
			return memory.Value{}, false, nil
		}
		prototype := context.intrinsics.WeakMapPrototype
		if kind == memory.HeapWeakSet {
			prototype = context.intrinsics.WeakSetPrototype
		}
		return execution.getNamedProperty(base, prototype, name)
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
	if value.IsRef() {
		kind, kindErr := execution.context.HeapKind(value.Ref())
		if kindErr != nil {
			return false, kindErr
		}
		if kind == memory.HeapTypedArray && descriptor.Name.IsRef() {
			name, nameErr := execution.context.DerefString(descriptor.Name.Ref())
			if nameErr == nil && name == "Uint8Array" {
				view, viewErr := execution.context.DerefTypedArray(value.Ref())
				return viewErr == nil && view.Element == memory.ElementUint8, viewErr
			}
		}
	}
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
		nameText, stringKey, nameErr := execution.stringPropertyName(name)
		if nameErr != nil {
			return memory.Value{}, false, nameErr
		}
		if stringKey {
			for _, interceptor := range execution.interpreter.propertyInterceptors() {
				if interceptor.Get == nil {
					continue
				}
				value, interceptedFound, handled, interceptErr := interceptor.Get(execution.context, ref, nameText)
				if interceptErr != nil || handled {
					return value, interceptedFound, interceptErr
				}
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
			nameText, stringKey, nameErr := execution.stringPropertyName(name)
			if nameErr != nil {
				return nameErr
			}
			if stringKey {
				for _, interceptor := range execution.interpreter.propertyInterceptors() {
					if interceptor.Set == nil {
						continue
					}
					handled, interceptErr := interceptor.Set(context, ref, nameText, value)
					if interceptErr != nil || handled {
						return interceptErr
					}
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
	case memory.HeapTypedArray:
		index, length, indexed, _, err := execution.arrayPropertyKey(key)
		if err != nil {
			return err
		}
		if length {
			return memory.ErrReadOnlyProperty
		}
		if indexed {
			view, err := context.DerefTypedArray(ref)
			if err != nil {
				return err
			}
			if uint64(index) >= view.Length {
				return nil
			}
			number, err := execution.toNumber(value)
			if err != nil {
				return err
			}
			return context.WriteTypedArrayElement(ref, uint64(index), number)
		}
		return memory.ErrReadOnlyProperty
	case memory.HeapRegExp:
		name, err := execution.propertyName(key)
		if err != nil {
			return err
		}
		nameText, stringKey, err := execution.stringPropertyName(name)
		if err != nil {
			return err
		}
		if stringKey && nameText == "lastIndex" {
			index, err := execution.toNumber(value)
			if err != nil {
				return err
			}
			return context.SetRegExpLastIndex(ref, uint64(toUint32(index)))
		}
		return memory.ErrReadOnlyProperty
	case memory.HeapString, memory.HeapSymbol:
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
			nameText, stringKey, nameErr := execution.stringPropertyName(name)
			if nameErr != nil {
				return false, nameErr
			}
			if stringKey {
				for _, interceptor := range execution.interpreter.propertyInterceptors() {
					if interceptor.Delete == nil {
						continue
					}
					deleted, handled, interceptErr := interceptor.Delete(context, ref, nameText)
					if interceptErr != nil || handled {
						return deleted, interceptErr
					}
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
	case memory.HeapTypedArray:
		index, length, indexed, _, err := execution.arrayPropertyKey(key)
		if err != nil {
			return false, err
		}
		if length {
			return false, nil
		}
		if indexed {
			view, err := context.DerefTypedArray(ref)
			if err != nil {
				return false, err
			}
			return uint64(index) >= view.Length, nil
		}
		return true, nil
	case memory.HeapString, memory.HeapSymbol:
		return false, nil
	default:
		return false, fmt.Errorf("%w: HeapKind(%d) has no properties", ErrOperandType, kind)
	}
}

func typedArrayElementSize(kind memory.ElementKind) uint64 {
	switch kind {
	case memory.ElementInt8, memory.ElementUint8, memory.ElementUint8Clamped:
		return 1
	case memory.ElementInt16, memory.ElementUint16:
		return 2
	case memory.ElementInt32, memory.ElementUint32, memory.ElementFloat32:
		return 4
	case memory.ElementFloat64:
		return 8
	default:
		return 0
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
		if kind == memory.HeapString || kind == memory.HeapSymbol {
			return primitive.Ref(), nil
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
	text, stringKey, err := execution.stringPropertyName(name)
	if err != nil {
		return 0, false, false, memory.Ref{}, err
	}
	if !stringKey {
		return 0, false, false, name, nil
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

func (execution *execution) stringPropertyName(name memory.Ref) (string, bool, error) {
	kind, err := execution.context.HeapKind(name)
	if err != nil {
		return "", false, err
	}
	switch kind {
	case memory.HeapString:
		text, err := execution.context.DerefString(name)
		return text, true, err
	case memory.HeapSymbol:
		return "", false, nil
	default:
		return "", false, fmt.Errorf("%w: property name is a %s", ErrOperandType, kind)
	}
}
