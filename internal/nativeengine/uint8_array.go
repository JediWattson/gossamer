package nativeengine

import (
	"fmt"
	"math"
	"strconv"

	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

const (
	nativeUint8ArrayConstructor uint64 = 18_000 + iota
	nativeUint8ArrayFrom
	nativeUint8ArraySet
	nativeUint8ArraySlice
	nativeUint8ArraySubarray
	nativeUint8ArrayFill
)

const (
	bindingUint8ArrayPrototype   = "\x00gossamer.uint8-array.prototype"
	bindingUint8ArrayConstructor = "\x00gossamer.uint8-array.constructor"
)

func (realm *Realm) newUint8ArrayConstructor(context *browserruntime.TaskContext) (memory.Ref, memory.Ref, error) {
	name, err := newString(context, "Uint8Array")
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	constructor, err := context.NewNativeConstructor(name, memory.RefValue(realm.active.Global), 3, nativeUint8ArrayConstructor)
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	prototype, err := constructorPrototype(context, constructor, "Uint8Array")
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	if err := context.SetPrototype(prototype, memory.RefValue(realm.active.ObjectPrototype)); err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	from, err := realm.newNativeFunction(context, "from", 1, nativeUint8ArrayFrom)
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	if err := defineData(context, constructor, "from", memory.RefValue(from), true, false, true); err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	for _, method := range []struct {
		name  string
		arity uint32
		id    uint64
	}{
		{"set", 1, nativeUint8ArraySet}, {"slice", 2, nativeUint8ArraySlice},
		{"subarray", 2, nativeUint8ArraySubarray}, {"fill", 1, nativeUint8ArrayFill},
	} {
		function, err := realm.newNativeFunction(context, method.name, method.arity, method.id)
		if err != nil {
			return memory.Ref{}, memory.Ref{}, err
		}
		if err := defineData(context, prototype, method.name, memory.RefValue(function), true, false, true); err != nil {
			return memory.Ref{}, memory.Ref{}, err
		}
	}
	return constructor, prototype, nil
}

func (realm *Realm) uint8ArrayConstructor(context *browserruntime.TaskContext, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	initial := argument(arguments, 0)
	if initial.Kind() == memory.ValueUndefined {
		return newUint8Array(context, nil)
	}
	if initial.Kind() == memory.ValueNumber {
		length, err := uint8ArrayLength(initial.Number())
		if err != nil {
			return memory.Value{}, err
		}
		return newUint8Array(context, make([]byte, length))
	}
	if !initial.IsRef() {
		return memory.Value{}, fmt.Errorf("%w: Uint8Array source", browserruntime.ErrOperandType)
	}
	kind, err := context.HeapKind(initial.Ref())
	if err != nil {
		return memory.Value{}, err
	}
	switch kind {
	case memory.HeapArray:
		return uint8ArrayFromArray(context, initial.Ref(), realm, memory.UndefinedValue())
	case memory.HeapArrayBuffer:
		buffer, err := context.DerefArrayBuffer(initial.Ref())
		if err != nil {
			return memory.Value{}, err
		}
		offset := uint64(0)
		if argument(arguments, 1).Kind() != memory.ValueUndefined {
			offset, err = uint8ArrayIndex(context, argument(arguments, 1))
			if err != nil {
				return memory.Value{}, err
			}
		}
		if offset > uint64(len(buffer.Bytes)) {
			return memory.Value{}, fmt.Errorf("%w: Uint8Array byte offset", browserruntime.ErrOperandType)
		}
		length := uint64(len(buffer.Bytes)) - offset
		if argument(arguments, 2).Kind() != memory.ValueUndefined {
			length, err = uint8ArrayIndex(context, argument(arguments, 2))
			if err != nil || offset+length > uint64(len(buffer.Bytes)) {
				return memory.Value{}, fmt.Errorf("%w: Uint8Array length", browserruntime.ErrOperandType)
			}
		}
		view, err := context.NewTypedArray(initial.Ref(), memory.ElementUint8, offset, length)
		return memory.RefValue(view), err
	case memory.HeapTypedArray:
		view, err := context.DerefTypedArray(initial.Ref())
		if err != nil {
			return memory.Value{}, err
		}
		bytes, err := context.ReadArrayBuffer(view.Buffer, view.ByteOffset, view.Length*textCodecElementSize(view.Element))
		if err != nil {
			return memory.Value{}, err
		}
		return newUint8Array(context, bytes)
	default:
		return memory.Value{}, fmt.Errorf("%w: Uint8Array source", browserruntime.ErrOperandType)
	}
}

func uint8ArrayLength(number float64) (int, error) {
	if math.IsNaN(number) || math.IsInf(number, 0) || number < 0 || number > math.MaxUint32 || number != math.Trunc(number) {
		return 0, fmt.Errorf("%w: Uint8Array length", browserruntime.ErrOperandType)
	}
	return int(number), nil
}

func uint8ArrayIndex(context *browserruntime.TaskContext, value memory.Value) (uint64, error) {
	if value.Kind() == memory.ValueNumber {
		length, err := uint8ArrayLength(value.Number())
		return uint64(length), err
	}
	text, err := valueString(context, value)
	if err != nil {
		return 0, err
	}
	parsed, err := strconv.ParseUint(text, 10, 32)
	return parsed, err
}

func uint8ArrayNumber(context *browserruntime.TaskContext, value memory.Value) (byte, error) {
	var number float64
	switch value.Kind() {
	case memory.ValueUndefined:
		return 0, nil
	case memory.ValueNull:
		return 0, nil
	case memory.ValueBool:
		if value.Bool() {
			return 1, nil
		}
		return 0, nil
	case memory.ValueNumber:
		number = value.Number()
	default:
		text, err := valueString(context, value)
		if err != nil {
			return 0, err
		}
		number, err = strconv.ParseFloat(text, 64)
		if err != nil {
			return 0, nil
		}
	}
	if math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, nil
	}
	return byte(int64(math.Trunc(number)) & 0xff), nil
}

func uint8ArrayFromArray(context *browserruntime.TaskContext, array memory.Ref, realm *Realm, mapper memory.Value) (memory.Value, error) {
	snapshot, err := context.DerefArray(array)
	if err != nil {
		return memory.Value{}, err
	}
	bytes := make([]byte, snapshot.Length)
	for index := uint32(0); index < snapshot.Length; index++ {
		value, _, err := context.ArrayElement(array, index)
		if err != nil {
			return memory.Value{}, err
		}
		if mapper.IsRef() {
			value, err = realm.interpreter.CallWithoutCheckpoint(context, mapper.Ref(), memory.UndefinedValue(), value, memory.NumberValue(float64(index)))
			if err != nil {
				return memory.Value{}, err
			}
		}
		bytes[index], err = uint8ArrayNumber(context, value)
		if err != nil {
			return memory.Value{}, err
		}
	}
	return newUint8Array(context, bytes)
}

func (realm *Realm) uint8ArrayFrom(context *browserruntime.TaskContext, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	source := argument(arguments, 0)
	mapper := argument(arguments, 1)
	if !source.IsRef() {
		return memory.Value{}, fmt.Errorf("%w: Uint8Array.from source", browserruntime.ErrOperandType)
	}
	kind, err := context.HeapKind(source.Ref())
	if err != nil {
		return memory.Value{}, err
	}
	if mapper.Kind() != memory.ValueUndefined {
		if !mapper.IsRef() {
			return memory.Value{}, fmt.Errorf("%w: Uint8Array.from mapper", browserruntime.ErrOperandType)
		}
		mapperKind, err := context.HeapKind(mapper.Ref())
		if err != nil || mapperKind != memory.HeapFunction {
			return memory.Value{}, fmt.Errorf("%w: Uint8Array.from mapper", browserruntime.ErrOperandType)
		}
	}
	if kind == memory.HeapArray {
		return uint8ArrayFromArray(context, source.Ref(), realm, mapper)
	}
	if kind == memory.HeapString {
		text, err := context.DerefString(source.Ref())
		if err != nil {
			return memory.Value{}, err
		}
		array, err := context.NewArray(uint32(len([]rune(text))))
		if err != nil {
			return memory.Value{}, err
		}
		for index, character := range []rune(text) {
			value, err := newString(context, string(character))
			if err != nil {
				return memory.Value{}, err
			}
			if err := context.SetArrayElement(array, uint32(index), value); err != nil {
				return memory.Value{}, err
			}
		}
		return uint8ArrayFromArray(context, array, realm, mapper)
	}
	return realm.uint8ArrayConstructor(context, memory.UndefinedValue(), []memory.Value{source})
}

func (realm *Realm) uint8ArraySet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	view, err := requireUint8Array(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	offset := uint64(0)
	if argument(arguments, 1).Kind() != memory.ValueUndefined {
		offset, err = uint8ArrayIndex(context, argument(arguments, 1))
		if err != nil {
			return memory.Value{}, err
		}
	}
	values, err := uint8ArraySourceBytes(context, argument(arguments, 0))
	if err != nil || offset+uint64(len(values)) > view.Length {
		return memory.Value{}, fmt.Errorf("%w: Uint8Array.set source exceeds destination", browserruntime.ErrOperandType)
	}
	return memory.UndefinedValue(), context.WriteArrayBuffer(view.Buffer, view.ByteOffset+offset, values)
}

func uint8ArraySourceBytes(context *browserruntime.TaskContext, source memory.Value) ([]byte, error) {
	if !source.IsRef() {
		return nil, fmt.Errorf("%w: Uint8Array source", browserruntime.ErrOperandType)
	}
	kind, err := context.HeapKind(source.Ref())
	if err != nil {
		return nil, err
	}
	if kind == memory.HeapTypedArray {
		view, err := context.DerefTypedArray(source.Ref())
		if err != nil {
			return nil, err
		}
		return context.ReadArrayBuffer(view.Buffer, view.ByteOffset, view.Length*textCodecElementSize(view.Element))
	}
	if kind == memory.HeapArray {
		array, err := context.DerefArray(source.Ref())
		if err != nil {
			return nil, err
		}
		bytes := make([]byte, array.Length)
		for index := uint32(0); index < array.Length; index++ {
			value, _, err := context.ArrayElement(source.Ref(), index)
			if err != nil {
				return nil, err
			}
			bytes[index], err = uint8ArrayNumber(context, value)
			if err != nil {
				return nil, err
			}
		}
		return bytes, nil
	}
	return nil, fmt.Errorf("%w: Uint8Array source", browserruntime.ErrOperandType)
}

func requireUint8Array(context *browserruntime.TaskContext, value memory.Value) (memory.TypedArray, error) {
	if !value.IsRef() {
		return memory.TypedArray{}, fmt.Errorf("%w: incompatible Uint8Array receiver", browserruntime.ErrOperandType)
	}
	view, err := context.DerefTypedArray(value.Ref())
	if err != nil || view.Element != memory.ElementUint8 {
		return memory.TypedArray{}, fmt.Errorf("%w: incompatible Uint8Array receiver", browserruntime.ErrOperandType)
	}
	return view, nil
}

func (realm *Realm) uint8ArraySlice(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	view, err := requireUint8Array(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	start, end, err := typedArrayBounds(context, view.Length, arguments)
	if err != nil {
		return memory.Value{}, err
	}
	bytes, err := context.ReadArrayBuffer(view.Buffer, view.ByteOffset+start, end-start)
	if err != nil {
		return memory.Value{}, err
	}
	return newUint8Array(context, bytes)
}

func (realm *Realm) uint8ArraySubarray(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	view, err := requireUint8Array(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	start, end, err := typedArrayBounds(context, view.Length, arguments)
	if err != nil {
		return memory.Value{}, err
	}
	result, err := context.NewTypedArray(view.Buffer, memory.ElementUint8, view.ByteOffset+start, end-start)
	return memory.RefValue(result), err
}

func typedArrayBounds(context *browserruntime.TaskContext, length uint64, arguments []memory.Value) (uint64, uint64, error) {
	start := uint64(0)
	end := length
	var err error
	if argument(arguments, 0).Kind() != memory.ValueUndefined {
		start, err = uint8ArrayIndex(context, argument(arguments, 0))
		if err != nil {
			return 0, 0, err
		}
		if start > length {
			start = length
		}
	}
	if argument(arguments, 1).Kind() != memory.ValueUndefined {
		end, err = uint8ArrayIndex(context, argument(arguments, 1))
		if err != nil {
			return 0, 0, err
		}
		if end > length {
			end = length
		}
	}
	if end < start {
		end = start
	}
	return start, end, nil
}

func (realm *Realm) uint8ArrayFill(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	view, err := requireUint8Array(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	value, err := uint8ArrayNumber(context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	start, end, err := typedArrayBounds(context, view.Length, arguments[1:])
	if err != nil {
		return memory.Value{}, err
	}
	bytes := make([]byte, end-start)
	for index := range bytes {
		bytes[index] = value
	}
	if err := context.WriteArrayBuffer(view.Buffer, view.ByteOffset+start, bytes); err != nil {
		return memory.Value{}, err
	}
	return this, nil
}
