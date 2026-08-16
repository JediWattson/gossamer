package runtime

import (
	"fmt"
	"math"
	"strconv"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func (context *TaskContext) getProperty(base, key memory.Value) (memory.Value, bool, error) {
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
		name, err := context.propertyName(key)
		if err != nil {
			return memory.Value{}, false, err
		}
		return context.GetOwnProperty(ref, name)
	case memory.HeapArray:
		index, length, err := context.arrayPropertyKey(key)
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
	default:
		return memory.Value{}, false, fmt.Errorf("%w: HeapKind(%d) has no properties", ErrOperandType, kind)
	}
}

func (context *TaskContext) setPropertyValue(base, key, value memory.Value) error {
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
		name, err := context.propertyName(key)
		if err != nil {
			return err
		}
		return context.SetProperty(ref, name, value)
	case memory.HeapArray:
		index, length, err := context.arrayPropertyKey(key)
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

func (context *TaskContext) deletePropertyValue(base, key memory.Value) (bool, error) {
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
		name, err := context.propertyName(key)
		if err != nil {
			return false, err
		}
		return context.DeleteProperty(ref, name)
	case memory.HeapArray:
		index, length, err := context.arrayPropertyKey(key)
		if err != nil {
			return false, err
		}
		if length {
			return false, nil
		}
		return context.DeleteArrayElement(ref, index)
	default:
		return false, fmt.Errorf("%w: HeapKind(%d) has no properties", ErrOperandType, kind)
	}
}

func (context *TaskContext) propertyName(key memory.Value) (memory.Ref, error) {
	if key.IsRef() {
		if kind, err := context.HeapKind(key.Ref()); err != nil {
			return memory.Ref{}, err
		} else if kind == memory.HeapString {
			return key.Ref(), nil
		}
	}
	if key.Kind() == memory.ValueNumber {
		index, err := requireUint32(key, "property key", true)
		if err != nil {
			return memory.Ref{}, err
		}
		return context.NewString(strconv.FormatUint(uint64(index), 10))
	}
	return memory.Ref{}, fmt.Errorf("%w: property key must be a String or uint32", ErrOperandType)
}

func (context *TaskContext) arrayPropertyKey(key memory.Value) (index uint32, length bool, err error) {
	if key.Kind() == memory.ValueNumber {
		index, err = requireUint32(key, "Array index", false)
		return index, false, err
	}
	if !key.IsRef() {
		return 0, false, fmt.Errorf("%w: Array property key must be a String or uint32", ErrOperandType)
	}
	if kind, kindErr := context.HeapKind(key.Ref()); kindErr != nil {
		return 0, false, kindErr
	} else if kind != memory.HeapString {
		return 0, false, fmt.Errorf("%w: Array property key must be a String or uint32", ErrOperandType)
	}
	text, err := context.DerefString(key.Ref())
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
