package memory

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

func (store *Store) AllocTypedArray(owner ownership.OwnerID, regionID RegionID, buffer Ref, element ElementKind, byteOffset, length uint64) (Ref, error) {
	if store == nil {
		return Ref{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	ref, err := store.allocKindLocked(owner, regionID, HeapTypedArray, false)
	if err != nil {
		return Ref{}, err
	}
	view := TypedArray{Buffer: buffer, Element: element, ByteOffset: byteOffset, Length: length}
	if err := store.initializeTypedArrayLocked(owner, ref, view, false); err != nil {
		_ = store.freeLocked(owner, ref, true)
		return Ref{}, err
	}
	return ref, nil
}

func (store *Store) DerefTypedArray(owner ownership.OwnerID, ref Ref) (TypedArray, error) {
	if store == nil {
		return TypedArray{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	_, slot, err := store.readSlotLocked(owner, ref)
	if err != nil {
		return TypedArray{}, err
	}
	if slot.Kind != HeapTypedArray {
		return TypedArray{}, typeError(ref, slot.Kind, HeapTypedArray)
	}
	return cloneTypedArray(slot.TypedArray), nil
}

func (store *Store) ReadTypedArrayElement(owner ownership.OwnerID, ref Ref, index uint64) (float64, error) {
	if store == nil {
		return 0, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	_, viewSlot, err := store.readSlotLocked(owner, ref)
	if err != nil {
		return 0, err
	}
	if viewSlot.Kind != HeapTypedArray {
		return 0, typeError(ref, viewSlot.Kind, HeapTypedArray)
	}
	_, bufferSlot, err := store.readSlotLocked(owner, viewSlot.TypedArray.Buffer)
	if err != nil {
		return 0, err
	}
	if bufferSlot.Kind != HeapArrayBuffer {
		return 0, typeError(viewSlot.TypedArray.Buffer, bufferSlot.Kind, HeapArrayBuffer)
	}
	if bufferSlot.ArrayBuffer.Detached {
		return 0, ErrDetachedBuffer
	}
	start, end, err := typedArrayElementRange(viewSlot.TypedArray, index)
	if err != nil {
		return 0, err
	}
	return decodeTypedNumber(viewSlot.TypedArray.Element, bufferSlot.ArrayBuffer.Bytes[start:end]), nil
}

func (store *Store) WriteTypedArrayElement(owner ownership.OwnerID, ref Ref, index uint64, number float64) error {
	if store == nil {
		return fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	_, viewSlot, err := store.writeSlotLocked(owner, ref, false)
	if err != nil {
		return err
	}
	if viewSlot.Kind != HeapTypedArray {
		return typeError(ref, viewSlot.Kind, HeapTypedArray)
	}
	_, bufferSlot, err := store.writeSlotLocked(owner, viewSlot.TypedArray.Buffer, false)
	if err != nil {
		return err
	}
	if bufferSlot.Kind != HeapArrayBuffer {
		return typeError(viewSlot.TypedArray.Buffer, bufferSlot.Kind, HeapArrayBuffer)
	}
	if bufferSlot.ArrayBuffer.Detached {
		return ErrDetachedBuffer
	}
	start, end, err := typedArrayElementRange(viewSlot.TypedArray, index)
	if err != nil {
		return err
	}
	encodeTypedNumber(viewSlot.TypedArray.Element, bufferSlot.ArrayBuffer.Bytes[start:end], number)
	return nil
}

func (store *Store) initializeTypedArrayLocked(owner ownership.OwnerID, ref Ref, view TypedArray, internal bool) error {
	region, slot, err := store.writeSlotLocked(owner, ref, internal)
	if err != nil {
		return err
	}
	if slot.Kind != HeapTypedArray {
		return typeError(ref, slot.Kind, HeapTypedArray)
	}
	if slot.TypedArray != (TypedArray{}) {
		return fmt.Errorf("%w: view is already initialized", ErrInvalidTypedArray)
	}
	size, valid := elementSize(view.Element)
	if !valid || view.ByteOffset%size != 0 {
		return fmt.Errorf("%w: element=%d offset=%d", ErrInvalidTypedArray, view.Element, view.ByteOffset)
	}
	_, bufferSlot, err := store.slotLocked(view.Buffer)
	if err != nil {
		return err
	}
	if bufferSlot.Kind != HeapArrayBuffer {
		return typeError(view.Buffer, bufferSlot.Kind, HeapArrayBuffer)
	}
	if bufferSlot.ArrayBuffer.Detached {
		return ErrDetachedBuffer
	}
	byteLength := view.Length * size
	if view.Length != 0 && byteLength/view.Length != size {
		return fmt.Errorf("%w: byte length overflow", ErrInvalidTypedArray)
	}
	if _, _, err := bufferRange(len(bufferSlot.ArrayBuffer.Bytes), view.ByteOffset, byteLength); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidTypedArray, err)
	}
	buffer, err := store.replaceValueLocked(owner, region, slot, Value{}, RefValue(view.Buffer), internal)
	if err != nil {
		return err
	}
	view.Buffer = buffer.Ref()
	slot.TypedArray = view
	return nil
}

func typedArrayElementRange(view TypedArray, index uint64) (int, int, error) {
	if index >= view.Length {
		return 0, 0, fmt.Errorf("%w: index=%d length=%d", ErrInvalidIndex, index, view.Length)
	}
	size, _ := elementSize(view.Element)
	start := view.ByteOffset + index*size
	return int(start), int(start + size), nil
}

func decodeTypedNumber(kind ElementKind, bytes []byte) float64 {
	switch kind {
	case ElementInt8:
		return float64(int8(bytes[0]))
	case ElementUint8, ElementUint8Clamped:
		return float64(bytes[0])
	case ElementInt16:
		return float64(int16(binary.LittleEndian.Uint16(bytes)))
	case ElementUint16:
		return float64(binary.LittleEndian.Uint16(bytes))
	case ElementInt32:
		return float64(int32(binary.LittleEndian.Uint32(bytes)))
	case ElementUint32:
		return float64(binary.LittleEndian.Uint32(bytes))
	case ElementFloat32:
		return float64(math.Float32frombits(binary.LittleEndian.Uint32(bytes)))
	case ElementFloat64:
		return math.Float64frombits(binary.LittleEndian.Uint64(bytes))
	default:
		return math.NaN()
	}
}

func encodeTypedNumber(kind ElementKind, bytes []byte, number float64) {
	switch kind {
	case ElementInt8, ElementUint8:
		bytes[0] = byte(toUintN(number, 8))
	case ElementUint8Clamped:
		if math.IsNaN(number) || number <= 0 {
			bytes[0] = 0
		} else if number >= 255 {
			bytes[0] = 255
		} else {
			bytes[0] = byte(math.RoundToEven(number))
		}
	case ElementInt16, ElementUint16:
		binary.LittleEndian.PutUint16(bytes, uint16(toUintN(number, 16)))
	case ElementInt32, ElementUint32:
		binary.LittleEndian.PutUint32(bytes, uint32(toUintN(number, 32)))
	case ElementFloat32:
		binary.LittleEndian.PutUint32(bytes, math.Float32bits(float32(number)))
	case ElementFloat64:
		binary.LittleEndian.PutUint64(bytes, math.Float64bits(number))
	}
}

func toUintN(number float64, bits uint) uint64 {
	if math.IsNaN(number) || math.IsInf(number, 0) || number == 0 {
		return 0
	}
	modulus := math.Ldexp(1, int(bits))
	value := math.Mod(math.Trunc(number), modulus)
	if value < 0 {
		value += modulus
	}
	return uint64(value)
}
