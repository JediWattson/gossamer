package nativeengine

import (
	"fmt"
	"unicode/utf16"

	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
	"github.com/JediWattson/gossamer/internal/webapi"
)

const (
	nativeTextEncoderConstructor uint64 = 17_000 + iota
	nativeTextEncoderEncode
	nativeTextEncoderEncodeInto
	nativeTextEncoderEncoding
	nativeTextDecoderConstructor
	nativeTextDecoderDecode
	nativeTextDecoderEncoding
	nativeTextDecoderFatal
	nativeTextDecoderIgnoreBOM
)

const (
	bindingTextEncoderPrototype   = "\x00gossamer.text-encoder.prototype"
	bindingTextEncoderConstructor = "\x00gossamer.text-encoder.constructor"
	bindingTextDecoderPrototype   = "\x00gossamer.text-decoder.prototype"
	bindingTextDecoderConstructor = "\x00gossamer.text-decoder.constructor"
	textDecoderFatalProperty      = "\x00gossamer.text-decoder.fatal"
	textDecoderIgnoreBOMProperty  = "\x00gossamer.text-decoder.ignore-bom"
)

func (realm *Realm) newTextCodecConstructors(context *browserruntime.TaskContext) (memory.Ref, memory.Ref, memory.Ref, memory.Ref, error) {
	encoder, encoderPrototype, err := realm.newTextCodecConstructor(context, "TextEncoder", nativeTextEncoderConstructor)
	if err != nil {
		return memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, err
	}
	decoder, decoderPrototype, err := realm.newTextCodecConstructor(context, "TextDecoder", nativeTextDecoderConstructor)
	if err != nil {
		return memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, err
	}
	encoderMethods := []struct {
		name  string
		arity uint32
		id    uint64
	}{{"encode", 1, nativeTextEncoderEncode}, {"encodeInto", 2, nativeTextEncoderEncodeInto}}
	for _, method := range encoderMethods {
		function, err := realm.newNativeFunction(context, method.name, method.arity, method.id)
		if err != nil {
			return memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, err
		}
		if err := defineData(context, encoderPrototype, method.name, memory.RefValue(function), true, false, true); err != nil {
			return memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, err
		}
	}
	decode, err := realm.newNativeFunction(context, "decode", 1, nativeTextDecoderDecode)
	if err != nil {
		return memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, err
	}
	if err := defineData(context, decoderPrototype, "decode", memory.RefValue(decode), true, false, true); err != nil {
		return memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, err
	}
	for _, accessor := range []struct {
		target memory.Ref
		name   string
		id     uint64
	}{
		{encoderPrototype, "encoding", nativeTextEncoderEncoding},
		{decoderPrototype, "encoding", nativeTextDecoderEncoding},
		{decoderPrototype, "fatal", nativeTextDecoderFatal},
		{decoderPrototype, "ignoreBOM", nativeTextDecoderIgnoreBOM},
	} {
		getter, err := realm.newAccessorFunction(context, "get "+accessor.name, accessor.id, 0)
		if err != nil {
			return memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, err
		}
		if err := defineAccessor(context, accessor.target, accessor.name, memory.RefValue(getter), memory.UndefinedValue()); err != nil {
			return memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, err
		}
	}
	return encoder, encoderPrototype, decoder, decoderPrototype, nil
}

func (realm *Realm) newTextCodecConstructor(context *browserruntime.TaskContext, name string, id uint64) (memory.Ref, memory.Ref, error) {
	nameValue, err := newString(context, name)
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	constructor, err := context.NewNativeConstructor(nameValue, memory.RefValue(realm.active.Global), 0, id)
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	prototype, err := constructorPrototype(context, constructor, name)
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	if err := context.SetPrototype(prototype, memory.RefValue(realm.active.ObjectPrototype)); err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	tag, err := newString(context, name)
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	if err := context.DefineProperty(prototype, realm.active.SymbolToStringTag, memory.DataProperty(tag, false, false, true)); err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	return constructor, prototype, nil
}

func (realm *Realm) textEncoderConstructor(_ *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	if !this.IsRef() {
		return memory.Value{}, fmt.Errorf("%w: TextEncoder constructor requires new", browserruntime.ErrOperandType)
	}
	return memory.UndefinedValue(), nil
}

func (realm *Realm) textDecoderConstructor(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	if !this.IsRef() {
		return memory.Value{}, fmt.Errorf("%w: TextDecoder constructor requires new", browserruntime.ErrOperandType)
	}
	label := "utf-8"
	if argument(arguments, 0).Kind() != memory.ValueUndefined {
		var err error
		label, err = valueString(context, argument(arguments, 0))
		if err != nil {
			return memory.Value{}, err
		}
	}
	if _, ok := webapi.NormalizeUTF8Label(label); !ok {
		return memory.Value{}, fmt.Errorf("%w: unsupported TextDecoder label %q", browserruntime.ErrOperandType, label)
	}
	options := argument(arguments, 1)
	fatal, ignoreBOM, err := textDecoderOptions(context, options)
	if err != nil {
		return memory.Value{}, err
	}
	if err := defineData(context, this.Ref(), textDecoderFatalProperty, memory.BoolValue(fatal), false, false, false); err != nil {
		return memory.Value{}, err
	}
	if err := defineData(context, this.Ref(), textDecoderIgnoreBOMProperty, memory.BoolValue(ignoreBOM), false, false, false); err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), nil
}

func textDecoderOptions(context *browserruntime.TaskContext, options memory.Value) (bool, bool, error) {
	if !options.IsRef() {
		return false, false, nil
	}
	values := [2]bool{}
	for index, name := range []string{"fatal", "ignoreBOM"} {
		key, err := context.NewString(name)
		if err != nil {
			return false, false, err
		}
		if value, found, err := context.GetOwnProperty(options.Ref(), key); err != nil {
			return false, false, err
		} else if found {
			values[index] = truthy(value)
		}
	}
	return values[0], values[1], nil
}

func requireTextCodecReceiver(context *browserruntime.TaskContext, this memory.Value, prototype memory.Ref, name string) (memory.Ref, error) {
	if !this.IsRef() {
		return memory.Ref{}, fmt.Errorf("%w: incompatible %s receiver", browserruntime.ErrOperandType, name)
	}
	header, err := context.DerefObjectHeader(this.Ref())
	if err != nil {
		return memory.Ref{}, err
	}
	if !header.Prototype.IsRef() || header.Prototype.Ref() != prototype {
		return memory.Ref{}, fmt.Errorf("%w: incompatible %s receiver", browserruntime.ErrOperandType, name)
	}
	return this.Ref(), nil
}

func (realm *Realm) textEncoderEncode(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	if _, err := requireTextCodecReceiver(context, this, realm.bindings.textEncoderPrototype, "TextEncoder"); err != nil {
		return memory.Value{}, err
	}
	text := ""
	var err error
	if argument(arguments, 0).Kind() != memory.ValueUndefined {
		text, err = valueString(context, argument(arguments, 0))
		if err != nil {
			return memory.Value{}, err
		}
	}
	return newUint8Array(context, webapi.EncodeUTF8(text))
}

func newUint8Array(context *browserruntime.TaskContext, bytes []byte) (memory.Value, error) {
	buffer, err := context.NewArrayBuffer(bytes)
	if err != nil {
		return memory.Value{}, err
	}
	view, err := context.NewTypedArray(buffer, memory.ElementUint8, 0, uint64(len(bytes)))
	return memory.RefValue(view), err
}

func (realm *Realm) textEncoderEncodeInto(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	if _, err := requireTextCodecReceiver(context, this, realm.bindings.textEncoderPrototype, "TextEncoder"); err != nil {
		return memory.Value{}, err
	}
	text, err := valueString(context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	destination := argument(arguments, 1)
	if !destination.IsRef() {
		return memory.Value{}, fmt.Errorf("%w: TextEncoder.encodeInto destination", browserruntime.ErrOperandType)
	}
	view, err := context.DerefTypedArray(destination.Ref())
	if err != nil || view.Element != memory.ElementUint8 {
		return memory.Value{}, fmt.Errorf("%w: TextEncoder.encodeInto requires Uint8Array", browserruntime.ErrOperandType)
	}
	written := uint64(0)
	read := 0
	for _, character := range text {
		encoded := webapi.EncodeUTF8(string(character))
		if written+uint64(len(encoded)) > view.Length {
			break
		}
		if err := context.WriteArrayBuffer(view.Buffer, view.ByteOffset+written, encoded); err != nil {
			return memory.Value{}, err
		}
		written += uint64(len(encoded))
		read += utf16.RuneLen(character)
	}
	result, err := context.NewHeapObject()
	if err != nil {
		return memory.Value{}, err
	}
	if err := defineData(context, result, "read", memory.NumberValue(float64(read)), true, true, true); err != nil {
		return memory.Value{}, err
	}
	if err := defineData(context, result, "written", memory.NumberValue(float64(written)), true, true, true); err != nil {
		return memory.Value{}, err
	}
	return memory.RefValue(result), nil
}

func (realm *Realm) textDecoderDecode(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	object, err := requireTextCodecReceiver(context, this, realm.bindings.textDecoderPrototype, "TextDecoder")
	if err != nil {
		return memory.Value{}, err
	}
	bytes, err := textDecoderBytes(context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	fatal, err := hiddenBool(context, object, textDecoderFatalProperty)
	if err != nil {
		return memory.Value{}, err
	}
	decoded, err := webapi.DecodeUTF8(bytes, fatal)
	if err != nil {
		return memory.Value{}, fmt.Errorf("%w: %v", browserruntime.ErrOperandType, err)
	}
	return newString(context, decoded)
}

func textDecoderBytes(context *browserruntime.TaskContext, input memory.Value) ([]byte, error) {
	if input.Kind() == memory.ValueUndefined {
		return nil, nil
	}
	if !input.IsRef() {
		return nil, fmt.Errorf("%w: TextDecoder input", browserruntime.ErrOperandType)
	}
	kind, err := context.HeapKind(input.Ref())
	if err != nil {
		return nil, err
	}
	switch kind {
	case memory.HeapArrayBuffer:
		buffer, err := context.DerefArrayBuffer(input.Ref())
		if err != nil {
			return nil, err
		}
		return context.ReadArrayBuffer(input.Ref(), 0, uint64(len(buffer.Bytes)))
	case memory.HeapTypedArray:
		view, err := context.DerefTypedArray(input.Ref())
		if err != nil {
			return nil, err
		}
		return context.ReadArrayBuffer(view.Buffer, view.ByteOffset, view.Length*textCodecElementSize(view.Element))
	default:
		return nil, fmt.Errorf("%w: TextDecoder input", browserruntime.ErrOperandType)
	}
}

func textCodecElementSize(kind memory.ElementKind) uint64 {
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

func hiddenBool(context *browserruntime.TaskContext, object memory.Ref, property string) (bool, error) {
	name, err := context.NewString(property)
	if err != nil {
		return false, err
	}
	value, found, err := context.GetOwnProperty(object, name)
	if err != nil || !found || value.Kind() != memory.ValueBool {
		return false, err
	}
	return value.Bool(), nil
}

func (realm *Realm) textEncoderEncoding(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	if _, err := requireTextCodecReceiver(context, this, realm.bindings.textEncoderPrototype, "TextEncoder"); err != nil {
		return memory.Value{}, err
	}
	return newString(context, "utf-8")
}

func (realm *Realm) textDecoderEncoding(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	if _, err := requireTextCodecReceiver(context, this, realm.bindings.textDecoderPrototype, "TextDecoder"); err != nil {
		return memory.Value{}, err
	}
	return newString(context, "utf-8")
}

func (realm *Realm) textDecoderFatal(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	object, err := requireTextCodecReceiver(context, this, realm.bindings.textDecoderPrototype, "TextDecoder")
	if err != nil {
		return memory.Value{}, err
	}
	value, err := hiddenBool(context, object, textDecoderFatalProperty)
	return memory.BoolValue(value), err
}

func (realm *Realm) textDecoderIgnoreBOM(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	object, err := requireTextCodecReceiver(context, this, realm.bindings.textDecoderPrototype, "TextDecoder")
	if err != nil {
		return memory.Value{}, err
	}
	value, err := hiddenBool(context, object, textDecoderIgnoreBOMProperty)
	return memory.BoolValue(value), err
}
