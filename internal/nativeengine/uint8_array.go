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
	nativeInt8ArrayConstructor
	nativeInt8ArrayFrom
	nativeUint8ClampedArrayConstructor
	nativeUint8ClampedArrayFrom
	nativeInt16ArrayConstructor
	nativeInt16ArrayFrom
	nativeUint16ArrayConstructor
	nativeUint16ArrayFrom
	nativeInt32ArrayConstructor
	nativeInt32ArrayFrom
	nativeUint32ArrayConstructor
	nativeUint32ArrayFrom
	nativeFloat32ArrayConstructor
	nativeFloat32ArrayFrom
	nativeFloat64ArrayConstructor
	nativeFloat64ArrayFrom
	nativeArrayBufferConstructor
	nativeArrayBufferIsView
	nativeArrayBufferSlice
)

const (
	bindingUint8ArrayPrototype    = "\x00gossamer.uint8-array.prototype"
	bindingUint8ArrayConstructor  = "\x00gossamer.uint8-array.constructor"
	bindingArrayBufferPrototype   = "\x00gossamer.array-buffer.prototype"
	bindingArrayBufferConstructor = "\x00gossamer.array-buffer.constructor"
)

type typedArraySpec struct {
	name         string
	element      memory.ElementKind
	bytesPerElem uint64
	nativeCtor   uint64
	nativeFrom   uint64
}

var typedArraySpecs = []typedArraySpec{
	{"Int8Array", memory.ElementInt8, 1, nativeInt8ArrayConstructor, nativeInt8ArrayFrom},
	{"Uint8Array", memory.ElementUint8, 1, nativeUint8ArrayConstructor, nativeUint8ArrayFrom},
	{"Uint8ClampedArray", memory.ElementUint8Clamped, 1, nativeUint8ClampedArrayConstructor, nativeUint8ClampedArrayFrom},
	{"Int16Array", memory.ElementInt16, 2, nativeInt16ArrayConstructor, nativeInt16ArrayFrom},
	{"Uint16Array", memory.ElementUint16, 2, nativeUint16ArrayConstructor, nativeUint16ArrayFrom},
	{"Int32Array", memory.ElementInt32, 4, nativeInt32ArrayConstructor, nativeInt32ArrayFrom},
	{"Uint32Array", memory.ElementUint32, 4, nativeUint32ArrayConstructor, nativeUint32ArrayFrom},
	{"Float32Array", memory.ElementFloat32, 4, nativeFloat32ArrayConstructor, nativeFloat32ArrayFrom},
	{"Float64Array", memory.ElementFloat64, 8, nativeFloat64ArrayConstructor, nativeFloat64ArrayFrom},
}

func (realm *Realm) newArrayBufferConstructor(context *browserruntime.TaskContext) (memory.Ref, memory.Ref, error) {
	name, err := newString(context, "ArrayBuffer")
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	constructor, err := context.NewNativeConstructor(name, memory.RefValue(realm.active.Global), 1, nativeArrayBufferConstructor)
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	prototype, err := constructorPrototype(context, constructor, "ArrayBuffer")
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	if err := context.SetPrototype(prototype, memory.RefValue(realm.active.ObjectPrototype)); err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	isView, err := realm.newNativeFunction(context, "isView", 1, nativeArrayBufferIsView)
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	if err := defineData(context, constructor, "isView", memory.RefValue(isView), true, false, true); err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	slice, err := realm.newNativeFunction(context, "slice", 2, nativeArrayBufferSlice)
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	if err := defineData(context, prototype, "slice", memory.RefValue(slice), true, false, true); err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	return constructor, prototype, nil
}

func (realm *Realm) arrayBufferConstructor(context *browserruntime.TaskContext, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	length := 0
	if arg := argument(arguments, 0); arg.Kind() != memory.ValueUndefined {
		var err error
		length, err = uint8ArrayLength(arg.Number())
		if err != nil {
			return memory.Value{}, err
		}
	}
	buffer, err := context.NewArrayBuffer(make([]byte, length))
	return memory.RefValue(buffer), err
}

func (realm *Realm) arrayBufferIsView(context *browserruntime.TaskContext, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	value := argument(arguments, 0)
	if !value.IsRef() {
		return memory.BoolValue(false), nil
	}
	kind, err := context.HeapKind(value.Ref())
	if err != nil {
		return memory.Value{}, err
	}
	return memory.BoolValue(kind == memory.HeapTypedArray), nil
}

func (realm *Realm) arrayBufferSlice(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	if !this.IsRef() {
		return memory.Value{}, fmt.Errorf("%w: incompatible ArrayBuffer receiver", browserruntime.ErrOperandType)
	}
	buffer, err := context.DerefArrayBuffer(this.Ref())
	if err != nil {
		return memory.Value{}, err
	}
	start, end, err := typedArraySignedBounds(context, uint64(len(buffer.Bytes)), arguments)
	if err != nil {
		return memory.Value{}, err
	}
	bytes, err := context.ReadArrayBuffer(this.Ref(), start, end-start)
	if err != nil {
		return memory.Value{}, err
	}
	result, err := context.NewArrayBuffer(bytes)
	return memory.RefValue(result), err
}

func (realm *Realm) installTypedArrayConstructors(context *browserruntime.TaskContext) (map[string]memory.Ref, memory.Ref, memory.Ref, error) {
	sharedPrototype, uint8Constructor, err := realm.newUint8ArrayConstructor(context)
	if err != nil {
		return nil, memory.Ref{}, memory.Ref{}, err
	}
	constructors := make(map[string]memory.Ref, len(typedArraySpecs))
	constructors["Uint8Array"] = uint8Constructor
	for _, spec := range typedArraySpecs {
		var constructor memory.Ref
		var prototype memory.Ref
		if spec.element == memory.ElementUint8 {
			constructor = uint8Constructor
			prototype = sharedPrototype
		} else {
			name, err := newString(context, spec.name)
			if err != nil {
				return nil, memory.Ref{}, memory.Ref{}, err
			}
			constructor, err = context.NewNativeConstructor(name, memory.RefValue(realm.active.Global), 3, spec.nativeCtor)
			if err != nil {
				return nil, memory.Ref{}, memory.Ref{}, err
			}
			prototype, err = constructorPrototype(context, constructor, spec.name)
			if err != nil {
				return nil, memory.Ref{}, memory.Ref{}, err
			}
			if err := context.SetPrototype(prototype, memory.RefValue(sharedPrototype)); err != nil {
				return nil, memory.Ref{}, memory.Ref{}, err
			}
			from, err := realm.newNativeFunction(context, "from", 1, spec.nativeFrom)
			if err != nil {
				return nil, memory.Ref{}, memory.Ref{}, err
			}
			if err := defineData(context, constructor, "from", memory.RefValue(from), true, false, true); err != nil {
				return nil, memory.Ref{}, memory.Ref{}, err
			}
		}
		bytesPerElem := memory.NumberValue(float64(spec.bytesPerElem))
		if err := defineData(context, constructor, "BYTES_PER_ELEMENT", bytesPerElem, false, false, false); err != nil {
			return nil, memory.Ref{}, memory.Ref{}, err
		}
		if err := defineData(context, prototype, "BYTES_PER_ELEMENT", bytesPerElem, false, false, false); err != nil {
			return nil, memory.Ref{}, memory.Ref{}, err
		}
		constructors[spec.name] = constructor
	}
	return constructors, uint8Constructor, sharedPrototype, nil
}

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
	return prototype, constructor, nil
}

func (realm *Realm) typedArrayConstructorFor(spec typedArraySpec) browserruntime.NativeFunction {
	return func(context *browserruntime.TaskContext, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
		return realm.constructTypedArray(context, spec, arguments)
	}
}

func (realm *Realm) typedArrayFromFor(spec typedArraySpec) browserruntime.NativeFunction {
	return func(context *browserruntime.TaskContext, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
		return realm.typedArrayFrom(context, spec, arguments)
	}
}

func (realm *Realm) constructTypedArray(context *browserruntime.TaskContext, spec typedArraySpec, arguments []memory.Value) (memory.Value, error) {
	initial := argument(arguments, 0)
	if initial.Kind() == memory.ValueUndefined {
		return newTypedArrayZeroed(context, spec.element, spec.bytesPerElem, 0)
	}
	if initial.Kind() == memory.ValueNumber {
		length, err := uint8ArrayLength(initial.Number())
		if err != nil {
			return memory.Value{}, err
		}
		return newTypedArrayZeroed(context, spec.element, spec.bytesPerElem, uint64(length))
	}
	if !initial.IsRef() {
		return memory.Value{}, fmt.Errorf("%w: %s source", browserruntime.ErrOperandType, spec.name)
	}
	kind, err := context.HeapKind(initial.Ref())
	if err != nil {
		return memory.Value{}, err
	}
	switch kind {
	case memory.HeapArray:
		return typedArrayFromArray(context, spec, initial.Ref(), realm, memory.UndefinedValue())
	case memory.HeapArrayBuffer:
		buffer, err := context.DerefArrayBuffer(initial.Ref())
		if err != nil {
			return memory.Value{}, err
		}
		if buffer.Detached {
			return memory.Value{}, memory.ErrDetachedBuffer
		}
		offset := uint64(0)
		if argument(arguments, 1).Kind() != memory.ValueUndefined {
			offset, err = uint8ArrayIndex(context, argument(arguments, 1))
			if err != nil {
				return memory.Value{}, err
			}
		}
		if offset > uint64(len(buffer.Bytes)) || offset%spec.bytesPerElem != 0 {
			return memory.Value{}, fmt.Errorf("%w: %s byte offset", browserruntime.ErrOperandType, spec.name)
		}
		remaining := uint64(len(buffer.Bytes)) - offset
		var length uint64
		if argument(arguments, 2).Kind() != memory.ValueUndefined {
			length, err = uint8ArrayIndex(context, argument(arguments, 2))
			if err != nil || length*spec.bytesPerElem > remaining {
				return memory.Value{}, fmt.Errorf("%w: %s length", browserruntime.ErrOperandType, spec.name)
			}
		} else {
			if remaining%spec.bytesPerElem != 0 {
				return memory.Value{}, fmt.Errorf("%w: %s buffer byteLength is not aligned", browserruntime.ErrOperandType, spec.name)
			}
			length = remaining / spec.bytesPerElem
		}
		view, err := context.NewTypedArray(initial.Ref(), spec.element, offset, length)
		return memory.RefValue(view), err
	case memory.HeapTypedArray:
		sourceView, err := context.DerefTypedArray(initial.Ref())
		if err != nil {
			return memory.Value{}, err
		}
		if sourceView.Element == spec.element {
			bytes, err := context.ReadArrayBuffer(sourceView.Buffer, sourceView.ByteOffset, sourceView.Length*spec.bytesPerElem)
			if err != nil {
				return memory.Value{}, err
			}
			return newTypedArrayFromBytes(context, spec.element, spec.bytesPerElem, bytes)
		}
		dstValue, err := newTypedArrayZeroed(context, spec.element, spec.bytesPerElem, sourceView.Length)
		if err != nil {
			return memory.Value{}, err
		}
		for index := uint64(0); index < sourceView.Length; index++ {
			elementValue, err := context.ReadTypedArrayElement(initial.Ref(), index)
			if err != nil {
				return memory.Value{}, err
			}
			if err := context.WriteTypedArrayElement(dstValue.Ref(), index, elementValue); err != nil {
				return memory.Value{}, err
			}
		}
		return dstValue, nil
	default:
		if kind == memory.HeapSet || kind == memory.HeapMap || kind == memory.HeapIterator {
			converted, convErr := realm.arrayFromSource(context, initial, memory.UndefinedValue())
			if convErr != nil {
				return memory.Value{}, convErr
			}
			return typedArrayFromArray(context, spec, converted, realm, memory.UndefinedValue())
		}
		return memory.Value{}, fmt.Errorf("%w: %s source", browserruntime.ErrOperandType, spec.name)
	}
}

func newTypedArrayZeroed(context *browserruntime.TaskContext, element memory.ElementKind, bytesPerElem, length uint64) (memory.Value, error) {
	return newTypedArrayFromBytes(context, element, bytesPerElem, make([]byte, length*bytesPerElem))
}

func newTypedArrayFromBytes(context *browserruntime.TaskContext, element memory.ElementKind, bytesPerElem uint64, bytes []byte) (memory.Value, error) {
	buffer, err := context.NewArrayBuffer(bytes)
	if err != nil {
		return memory.Value{}, err
	}
	view, err := context.NewTypedArray(buffer, element, 0, uint64(len(bytes))/bytesPerElem)
	return memory.RefValue(view), err
}

func uint8ArrayLength(number float64) (int, error) {
	if math.IsNaN(number) || math.IsInf(number, 0) || number < 0 || number > math.MaxUint32 || number != math.Trunc(number) {
		return 0, fmt.Errorf("%w: TypedArray length", browserruntime.ErrOperandType)
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
	return strconv.ParseUint(text, 10, 32)
}

func typedArraySignedIndex(context *browserruntime.TaskContext, value memory.Value) (int64, error) {
	if value.Kind() == memory.ValueNumber {
		number := value.Number()
		if math.IsNaN(number) {
			return 0, nil
		}
		if math.IsInf(number, 1) {
			return math.MaxInt64, nil
		}
		if math.IsInf(number, -1) {
			return math.MinInt64, nil
		}
		return int64(math.Trunc(number)), nil
	}
	text, err := valueString(context, value)
	if err != nil {
		return 0, err
	}
	parsed, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0, nil
	}
	return parsed, nil
}

func typedArrayNumber(context *browserruntime.TaskContext, value memory.Value) (float64, error) {
	switch value.Kind() {
	case memory.ValueUndefined:
		return math.NaN(), nil
	case memory.ValueNull:
		return 0, nil
	case memory.ValueBool:
		if value.Bool() {
			return 1, nil
		}
		return 0, nil
	case memory.ValueNumber:
		return value.Number(), nil
	default:
		text, err := valueString(context, value)
		if err != nil {
			return 0, err
		}
		number, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return math.NaN(), nil
		}
		return number, nil
	}
}

func typedArrayFromArray(context *browserruntime.TaskContext, spec typedArraySpec, array memory.Ref, realm *Realm, mapper memory.Value) (memory.Value, error) {
	snapshot, err := context.DerefArray(array)
	if err != nil {
		return memory.Value{}, err
	}
	result, err := newTypedArrayZeroed(context, spec.element, spec.bytesPerElem, uint64(snapshot.Length))
	if err != nil {
		return memory.Value{}, err
	}
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
		number, err := typedArrayNumber(context, value)
		if err != nil {
			return memory.Value{}, err
		}
		if err := context.WriteTypedArrayElement(result.Ref(), uint64(index), number); err != nil {
			return memory.Value{}, err
		}
	}
	return result, nil
}

func (realm *Realm) typedArrayFrom(context *browserruntime.TaskContext, spec typedArraySpec, arguments []memory.Value) (memory.Value, error) {
	source := argument(arguments, 0)
	mapper := argument(arguments, 1)
	if !source.IsRef() {
		return memory.Value{}, fmt.Errorf("%w: %s.from source", browserruntime.ErrOperandType, spec.name)
	}
	kind, err := context.HeapKind(source.Ref())
	if err != nil {
		return memory.Value{}, err
	}
	if mapper.Kind() != memory.ValueUndefined {
		if !mapper.IsRef() {
			return memory.Value{}, fmt.Errorf("%w: %s.from mapper", browserruntime.ErrOperandType, spec.name)
		}
		mapperKind, err := context.HeapKind(mapper.Ref())
		if err != nil || mapperKind != memory.HeapFunction {
			return memory.Value{}, fmt.Errorf("%w: %s.from mapper", browserruntime.ErrOperandType, spec.name)
		}
	}
	if kind == memory.HeapArray {
		return typedArrayFromArray(context, spec, source.Ref(), realm, mapper)
	}
	if kind == memory.HeapString {
		text, err := context.DerefString(source.Ref())
		if err != nil {
			return memory.Value{}, err
		}
		runes := []rune(text)
		array, err := context.NewArray(uint32(len(runes)))
		if err != nil {
			return memory.Value{}, err
		}
		for index, character := range runes {
			value, err := newString(context, string(character))
			if err != nil {
				return memory.Value{}, err
			}
			if err := context.SetArrayElement(array, uint32(index), value); err != nil {
				return memory.Value{}, err
			}
		}
		return typedArrayFromArray(context, spec, array, realm, mapper)
	}
	if kind == memory.HeapTypedArray && mapper.IsRef() {
		srcView, err := context.DerefTypedArray(source.Ref())
		if err != nil {
			return memory.Value{}, err
		}
		result, err := newTypedArrayZeroed(context, spec.element, spec.bytesPerElem, srcView.Length)
		if err != nil {
			return memory.Value{}, err
		}
		for index := uint64(0); index < srcView.Length; index++ {
			elementValue, err := context.ReadTypedArrayElement(source.Ref(), index)
			if err != nil {
				return memory.Value{}, err
			}
			mapped, err := realm.interpreter.CallWithoutCheckpoint(context, mapper.Ref(), memory.UndefinedValue(), memory.NumberValue(elementValue), memory.NumberValue(float64(index)))
			if err != nil {
				return memory.Value{}, err
			}
			number, err := typedArrayNumber(context, mapped)
			if err != nil {
				return memory.Value{}, err
			}
			if err := context.WriteTypedArrayElement(result.Ref(), index, number); err != nil {
				return memory.Value{}, err
			}
		}
		return result, nil
	}
	if kind == memory.HeapTypedArray {
		return realm.constructTypedArray(context, spec, []memory.Value{source})
	}
	converted, err := realm.arrayFromSource(context, source, mapper)
	if err != nil {
		return memory.Value{}, err
	}
	return typedArrayFromArray(context, spec, converted, realm, memory.UndefinedValue())
}

func (realm *Realm) arrayFromSource(context *browserruntime.TaskContext, source, mapper memory.Value) (memory.Ref, error) {
	arrayName, err := newString(context, "Array")
	if err != nil {
		return memory.Ref{}, err
	}
	arrayCtor, found, err := context.ResolveBinding(realm.active.Global, arrayName.Ref())
	if err != nil || !found || !arrayCtor.IsRef() {
		return memory.Ref{}, fmt.Errorf("%w: missing global Array", browserruntime.ErrOperandType)
	}
	fromName, err := newString(context, "from")
	if err != nil {
		return memory.Ref{}, err
	}
	fromFn, found, err := context.GetOwnProperty(arrayCtor.Ref(), fromName.Ref())
	if err != nil || !found || !fromFn.IsRef() {
		return memory.Ref{}, fmt.Errorf("%w: missing Array.from", browserruntime.ErrOperandType)
	}
	args := []memory.Value{source}
	if mapper.Kind() != memory.ValueUndefined {
		args = append(args, mapper)
	}
	converted, err := realm.interpreter.CallWithoutCheckpoint(context, fromFn.Ref(), arrayCtor, args...)
	if err != nil {
		return memory.Ref{}, err
	}
	if !converted.IsRef() {
		return memory.Ref{}, fmt.Errorf("%w: Array.from did not return an Array", browserruntime.ErrOperandType)
	}
	return converted.Ref(), nil
}

func requireAnyTypedArray(context *browserruntime.TaskContext, value memory.Value) (memory.TypedArray, uint64, error) {
	if !value.IsRef() {
		return memory.TypedArray{}, 0, fmt.Errorf("%w: incompatible TypedArray receiver", browserruntime.ErrOperandType)
	}
	view, err := context.DerefTypedArray(value.Ref())
	if err != nil {
		return memory.TypedArray{}, 0, fmt.Errorf("%w: incompatible TypedArray receiver", browserruntime.ErrOperandType)
	}
	return view, textCodecElementSize(view.Element), nil
}

func (realm *Realm) uint8ArraySet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	view, _, err := requireAnyTypedArray(context, this)
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
	values, err := typedArraySourceNumbers(context, argument(arguments, 0))
	if err != nil || offset+uint64(len(values)) > view.Length {
		return memory.Value{}, fmt.Errorf("%w: TypedArray.set source exceeds destination", browserruntime.ErrOperandType)
	}
	for index, value := range values {
		if err := context.WriteTypedArrayElement(this.Ref(), offset+uint64(index), value); err != nil {
			return memory.Value{}, err
		}
	}
	return memory.UndefinedValue(), nil
}

func typedArraySourceNumbers(context *browserruntime.TaskContext, source memory.Value) ([]float64, error) {
	if !source.IsRef() {
		return nil, fmt.Errorf("%w: TypedArray source", browserruntime.ErrOperandType)
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
		values := make([]float64, view.Length)
		for index := uint64(0); index < view.Length; index++ {
			values[index], err = context.ReadTypedArrayElement(source.Ref(), index)
			if err != nil {
				return nil, err
			}
		}
		return values, nil
	}
	if kind == memory.HeapArray {
		array, err := context.DerefArray(source.Ref())
		if err != nil {
			return nil, err
		}
		values := make([]float64, array.Length)
		for index := uint32(0); index < array.Length; index++ {
			value, _, err := context.ArrayElement(source.Ref(), index)
			if err != nil {
				return nil, err
			}
			values[index], err = typedArrayNumber(context, value)
			if err != nil {
				return nil, err
			}
		}
		return values, nil
	}
	return nil, fmt.Errorf("%w: TypedArray source", browserruntime.ErrOperandType)
}

func (realm *Realm) uint8ArraySlice(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	view, elemSize, err := requireAnyTypedArray(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	start, end, err := typedArraySignedBounds(context, view.Length, arguments)
	if err != nil {
		return memory.Value{}, err
	}
	bytes, err := context.ReadArrayBuffer(view.Buffer, view.ByteOffset+start*elemSize, (end-start)*elemSize)
	if err != nil {
		return memory.Value{}, err
	}
	return newTypedArrayFromBytes(context, view.Element, elemSize, bytes)
}

func (realm *Realm) uint8ArraySubarray(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	view, elemSize, err := requireAnyTypedArray(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	start, end, err := typedArraySignedBounds(context, view.Length, arguments)
	if err != nil {
		return memory.Value{}, err
	}
	result, err := context.NewTypedArray(view.Buffer, view.Element, view.ByteOffset+start*elemSize, end-start)
	return memory.RefValue(result), err
}

func typedArraySignedBounds(context *browserruntime.TaskContext, length uint64, arguments []memory.Value) (uint64, uint64, error) {
	clamp := func(relative int64) uint64 {
		if relative < 0 {
			resolved := int64(length) + relative
			if resolved < 0 {
				return 0
			}
			return uint64(resolved)
		}
		if uint64(relative) > length {
			return length
		}
		return uint64(relative)
	}
	start := uint64(0)
	end := length
	if arg := argument(arguments, 0); arg.Kind() != memory.ValueUndefined {
		relative, err := typedArraySignedIndex(context, arg)
		if err != nil {
			return 0, 0, err
		}
		start = clamp(relative)
	}
	if arg := argument(arguments, 1); arg.Kind() != memory.ValueUndefined {
		relative, err := typedArraySignedIndex(context, arg)
		if err != nil {
			return 0, 0, err
		}
		end = clamp(relative)
	}
	if end < start {
		end = start
	}
	return start, end, nil
}

func (realm *Realm) uint8ArrayFill(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	view, _, err := requireAnyTypedArray(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	value, err := typedArrayNumber(context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	start, end, err := typedArraySignedBounds(context, view.Length, arguments[1:])
	if err != nil {
		return memory.Value{}, err
	}
	for index := start; index < end; index++ {
		if err := context.WriteTypedArrayElement(this.Ref(), index, value); err != nil {
			return memory.Value{}, err
		}
	}
	return this, nil
}
