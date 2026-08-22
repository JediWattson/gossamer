package memory

import (
	"fmt"
	"math/bits"
	"unsafe"
)

const payloadSlabCapacity = 256

type payloadHandle struct {
	slab  uint32
	index uint16
}

type payloadSlab[T any] struct {
	values            [payloadSlabCapacity]T
	used              [payloadSlabCapacity / 64]uint64
	live              uint16
	availablePosition int32
}

type typedPayloadArena[T any] struct {
	slabs     []*payloadSlab[T]
	available []uint32
	freeSlabs []uint32
	live      uint64
}

type payloadArenaPhysical struct {
	slabs         uint64
	live          uint64
	reservedBytes uint64
	occupiedBytes uint64
}

func (arena *typedPayloadArena[T]) allocate() (*T, payloadHandle) {
	if len(arena.available) == 0 {
		arena.addSlab()
	}
	slabIndex := arena.available[len(arena.available)-1]
	slab := arena.slabs[slabIndex]
	valueIndex := -1
	for wordIndex, word := range slab.used {
		if word == ^uint64(0) {
			continue
		}
		valueIndex = wordIndex*64 + bits.TrailingZeros64(^word)
		break
	}
	if valueIndex < 0 || valueIndex >= payloadSlabCapacity {
		panic("memory: typed payload slab marked available without a free value")
	}
	wordIndex := valueIndex / 64
	bit := uint(valueIndex % 64)
	slab.used[wordIndex] |= uint64(1) << bit
	slab.live++
	arena.live++
	if int(slab.live) == payloadSlabCapacity {
		arena.removeAvailable(slabIndex)
	}
	value := &slab.values[valueIndex]
	var zero T
	*value = zero
	return value, payloadHandle{slab: slabIndex, index: uint16(valueIndex)}
}

func (arena *typedPayloadArena[T]) release(handle payloadHandle, value *T) error {
	if uint64(handle.slab) >= uint64(len(arena.slabs)) {
		return fmt.Errorf("memory: payload slab %d is out of range", handle.slab)
	}
	slab := arena.slabs[handle.slab]
	if slab == nil || int(handle.index) >= payloadSlabCapacity {
		return fmt.Errorf("memory: payload handle %d:%d is stale", handle.slab, handle.index)
	}
	wordIndex := int(handle.index) / 64
	bit := uint(handle.index % 64)
	mask := uint64(1) << bit
	if slab.used[wordIndex]&mask == 0 || value != &slab.values[handle.index] {
		return fmt.Errorf("memory: payload handle %d:%d does not own its value", handle.slab, handle.index)
	}
	wasFull := int(slab.live) == payloadSlabCapacity
	var zero T
	*value = zero
	slab.used[wordIndex] &^= mask
	slab.live--
	arena.live--
	if slab.live == 0 {
		if slab.availablePosition >= 0 {
			arena.removeAvailable(handle.slab)
		}
		arena.slabs[handle.slab] = nil
		arena.freeSlabs = append(arena.freeSlabs, handle.slab)
		if arena.live == 0 {
			arena.slabs = nil
			arena.available = nil
			arena.freeSlabs = nil
		}
		return nil
	}
	if wasFull {
		arena.appendAvailable(handle.slab)
	}
	return nil
}

func (arena *typedPayloadArena[T]) addSlab() {
	slab := &payloadSlab[T]{availablePosition: -1}
	var index uint32
	if len(arena.freeSlabs) != 0 {
		index = arena.freeSlabs[len(arena.freeSlabs)-1]
		arena.freeSlabs = arena.freeSlabs[:len(arena.freeSlabs)-1]
		arena.slabs[index] = slab
	} else {
		index = uint32(len(arena.slabs))
		arena.slabs = append(arena.slabs, slab)
	}
	arena.appendAvailable(index)
}

func (arena *typedPayloadArena[T]) appendAvailable(slabIndex uint32) {
	slab := arena.slabs[slabIndex]
	if slab == nil || slab.availablePosition >= 0 {
		panic("memory: invalid typed payload available insertion")
	}
	slab.availablePosition = int32(len(arena.available))
	arena.available = append(arena.available, slabIndex)
}

func (arena *typedPayloadArena[T]) removeAvailable(slabIndex uint32) {
	slab := arena.slabs[slabIndex]
	position := int(slab.availablePosition)
	if position < 0 || position >= len(arena.available) || arena.available[position] != slabIndex {
		panic("memory: invalid typed payload available removal")
	}
	lastPosition := len(arena.available) - 1
	lastIndex := arena.available[lastPosition]
	arena.available[position] = lastIndex
	arena.available = arena.available[:lastPosition]
	slab.availablePosition = -1
	if position != lastPosition {
		arena.slabs[lastIndex].availablePosition = int32(position)
	}
}

func (arena *typedPayloadArena[T]) pointer(handle payloadHandle) *T {
	if uint64(handle.slab) >= uint64(len(arena.slabs)) || int(handle.index) >= payloadSlabCapacity {
		return nil
	}
	slab := arena.slabs[handle.slab]
	if slab == nil {
		return nil
	}
	wordIndex := int(handle.index) / 64
	bit := uint(handle.index % 64)
	if slab.used[wordIndex]&(uint64(1)<<bit) == 0 {
		return nil
	}
	return &slab.values[handle.index]
}

func (arena *typedPayloadArena[T]) physical() payloadArenaPhysical {
	result := payloadArenaPhysical{live: arena.live}
	var value T
	for _, slab := range arena.slabs {
		if slab != nil {
			result.slabs++
		}
	}
	result.reservedBytes = result.slabs * uint64(unsafe.Sizeof(payloadSlab[T]{}))
	result.reservedBytes += uint64(cap(arena.slabs)) * uint64(unsafe.Sizeof((*payloadSlab[T])(nil)))
	result.reservedBytes += uint64(cap(arena.available)+cap(arena.freeSlabs)) * uint64(unsafe.Sizeof(uint32(0)))
	result.occupiedBytes = result.live * uint64(unsafe.Sizeof(value))
	return result
}

func (arena *typedPayloadArena[T]) check(expected uint64) error {
	derived := uint64(0)
	available := make(map[uint32]struct{}, len(arena.available))
	for position, slabIndex := range arena.available {
		if uint64(slabIndex) >= uint64(len(arena.slabs)) || arena.slabs[slabIndex] == nil {
			return fmt.Errorf("available payload slab %d is stale", slabIndex)
		}
		if _, duplicate := available[slabIndex]; duplicate {
			return fmt.Errorf("available payload slab %d is duplicated", slabIndex)
		}
		available[slabIndex] = struct{}{}
		if arena.slabs[slabIndex].availablePosition != int32(position) {
			return fmt.Errorf("available payload slab %d position is %d, want %d", slabIndex, arena.slabs[slabIndex].availablePosition, position)
		}
	}
	free := make(map[uint32]struct{}, len(arena.freeSlabs))
	for _, slabIndex := range arena.freeSlabs {
		if uint64(slabIndex) >= uint64(len(arena.slabs)) || arena.slabs[slabIndex] != nil {
			return fmt.Errorf("free payload slab %d is not empty", slabIndex)
		}
		if _, duplicate := free[slabIndex]; duplicate {
			return fmt.Errorf("free payload slab %d is duplicated", slabIndex)
		}
		free[slabIndex] = struct{}{}
	}
	for slabIndex, slab := range arena.slabs {
		if slab == nil {
			if _, exists := free[uint32(slabIndex)]; !exists {
				return fmt.Errorf("released payload slab %d is missing from free list", slabIndex)
			}
			continue
		}
		used := 0
		for _, word := range slab.used {
			used += bits.OnesCount64(word)
		}
		if used == 0 || used != int(slab.live) {
			return fmt.Errorf("payload slab %d live bits are %d, counter is %d", slabIndex, used, slab.live)
		}
		_, isAvailable := available[uint32(slabIndex)]
		if isAvailable != (used < payloadSlabCapacity) {
			return fmt.Errorf("payload slab %d available=%t with %d live values", slabIndex, isAvailable, used)
		}
		if !isAvailable && slab.availablePosition != -1 {
			return fmt.Errorf("full payload slab %d retains available position %d", slabIndex, slab.availablePosition)
		}
		derived += uint64(used)
	}
	if derived != arena.live || derived != expected {
		return fmt.Errorf("typed payload live count is %d/%d, want %d", derived, arena.live, expected)
	}
	return nil
}

type payloadAllocator struct {
	cells        typedPayloadArena[Cell]
	strings      typedPayloadArena[StringObject]
	objects      typedPayloadArena[Object]
	arrays       typedPayloadArena[Array]
	contexts     typedPayloadArena[Context]
	functions    typedPayloadArena[Function]
	promises     typedPayloadArena[Promise]
	bigInts      typedPayloadArena[BigInt]
	symbols      typedPayloadArena[Symbol]
	arrayBuffers typedPayloadArena[ArrayBuffer]
	typedArrays  typedPayloadArena[TypedArray]
	maps         typedPayloadArena[Map]
	sets         typedPayloadArena[Set]
	dates        typedPayloadArena[Date]
	regExps      typedPayloadArena[RegExp]
	errors       typedPayloadArena[ErrorObject]
	weakMaps     typedPayloadArena[WeakMap]
	weakSets     typedPayloadArena[WeakSet]
	iterators    typedPayloadArena[Iterator]
	hostObjects  typedPayloadArena[HostObject]
}

func (allocator *payloadAllocator) allocate(kind HeapKind) *slotPayload {
	payload := &slotPayload{}
	switch kind {
	case HeapCell:
		payload.Cell, payload.handle = allocator.cells.allocate()
	case HeapString:
		payload.String, payload.handle = allocator.strings.allocate()
	case HeapObject:
		payload.Object, payload.handle = allocator.objects.allocate()
	case HeapArray:
		payload.Array, payload.handle = allocator.arrays.allocate()
	case HeapContext:
		payload.Context, payload.handle = allocator.contexts.allocate()
	case HeapFunction:
		payload.Function, payload.handle = allocator.functions.allocate()
	case HeapPromise:
		payload.Promise, payload.handle = allocator.promises.allocate()
	case HeapBigInt:
		payload.BigInt, payload.handle = allocator.bigInts.allocate()
	case HeapSymbol:
		payload.Symbol, payload.handle = allocator.symbols.allocate()
	case HeapArrayBuffer:
		payload.ArrayBuffer, payload.handle = allocator.arrayBuffers.allocate()
	case HeapTypedArray:
		payload.TypedArray, payload.handle = allocator.typedArrays.allocate()
	case HeapMap:
		payload.Map, payload.handle = allocator.maps.allocate()
	case HeapSet:
		payload.Set, payload.handle = allocator.sets.allocate()
	case HeapDate:
		payload.Date, payload.handle = allocator.dates.allocate()
	case HeapRegExp:
		payload.RegExp, payload.handle = allocator.regExps.allocate()
	case HeapError:
		payload.Error, payload.handle = allocator.errors.allocate()
	case HeapWeakMap:
		payload.WeakMap, payload.handle = allocator.weakMaps.allocate()
	case HeapWeakSet:
		payload.WeakSet, payload.handle = allocator.weakSets.allocate()
	case HeapIterator:
		payload.Iterator, payload.handle = allocator.iterators.allocate()
	case HeapHostObject:
		payload.HostObject, payload.handle = allocator.hostObjects.allocate()
	default:
		panic(fmt.Sprintf("memory: cannot allocate invalid heap kind %d", kind))
	}
	return payload
}

func (allocator *payloadAllocator) release(kind HeapKind, payload *slotPayload) error {
	if payload == nil {
		return fmt.Errorf("memory: %s slot has no payload", kind)
	}
	switch kind {
	case HeapCell:
		return allocator.cells.release(payload.handle, payload.Cell)
	case HeapString:
		return allocator.strings.release(payload.handle, payload.String)
	case HeapObject:
		return allocator.objects.release(payload.handle, payload.Object)
	case HeapArray:
		return allocator.arrays.release(payload.handle, payload.Array)
	case HeapContext:
		return allocator.contexts.release(payload.handle, payload.Context)
	case HeapFunction:
		return allocator.functions.release(payload.handle, payload.Function)
	case HeapPromise:
		return allocator.promises.release(payload.handle, payload.Promise)
	case HeapBigInt:
		return allocator.bigInts.release(payload.handle, payload.BigInt)
	case HeapSymbol:
		return allocator.symbols.release(payload.handle, payload.Symbol)
	case HeapArrayBuffer:
		return allocator.arrayBuffers.release(payload.handle, payload.ArrayBuffer)
	case HeapTypedArray:
		return allocator.typedArrays.release(payload.handle, payload.TypedArray)
	case HeapMap:
		return allocator.maps.release(payload.handle, payload.Map)
	case HeapSet:
		return allocator.sets.release(payload.handle, payload.Set)
	case HeapDate:
		return allocator.dates.release(payload.handle, payload.Date)
	case HeapRegExp:
		return allocator.regExps.release(payload.handle, payload.RegExp)
	case HeapError:
		return allocator.errors.release(payload.handle, payload.Error)
	case HeapWeakMap:
		return allocator.weakMaps.release(payload.handle, payload.WeakMap)
	case HeapWeakSet:
		return allocator.weakSets.release(payload.handle, payload.WeakSet)
	case HeapIterator:
		return allocator.iterators.release(payload.handle, payload.Iterator)
	case HeapHostObject:
		return allocator.hostObjects.release(payload.handle, payload.HostObject)
	default:
		return fmt.Errorf("memory: cannot release invalid heap kind %d", kind)
	}
}

func combinePayloadPhysical(values ...payloadArenaPhysical) payloadArenaPhysical {
	var result payloadArenaPhysical
	for _, value := range values {
		result.slabs += value.slabs
		result.live += value.live
		result.reservedBytes += value.reservedBytes
		result.occupiedBytes += value.occupiedBytes
	}
	return result
}

func (allocator *payloadAllocator) physical() payloadArenaPhysical {
	return combinePayloadPhysical(
		allocator.cells.physical(), allocator.strings.physical(), allocator.objects.physical(), allocator.arrays.physical(),
		allocator.contexts.physical(), allocator.functions.physical(), allocator.promises.physical(), allocator.bigInts.physical(),
		allocator.symbols.physical(), allocator.arrayBuffers.physical(), allocator.typedArrays.physical(), allocator.maps.physical(),
		allocator.sets.physical(), allocator.dates.physical(), allocator.regExps.physical(), allocator.errors.physical(),
		allocator.weakMaps.physical(), allocator.weakSets.physical(), allocator.iterators.physical(), allocator.hostObjects.physical(),
	)
}

func checkPayloadPointer[T any](arena *typedPayloadArena[T], payload *slotPayload, value *T) error {
	if want := arena.pointer(payload.handle); want == nil || want != value {
		return fmt.Errorf("typed payload handle %d:%d resolves to %p, slot has %p", payload.handle.slab, payload.handle.index, want, value)
	}
	return nil
}

func (allocator *payloadAllocator) checkSlot(kind HeapKind, payload *slotPayload) error {
	if payload == nil {
		return fmt.Errorf("%s slot has no payload facade", kind)
	}
	switch kind {
	case HeapCell:
		return checkPayloadPointer(&allocator.cells, payload, payload.Cell)
	case HeapString:
		return checkPayloadPointer(&allocator.strings, payload, payload.String)
	case HeapObject:
		return checkPayloadPointer(&allocator.objects, payload, payload.Object)
	case HeapArray:
		return checkPayloadPointer(&allocator.arrays, payload, payload.Array)
	case HeapContext:
		return checkPayloadPointer(&allocator.contexts, payload, payload.Context)
	case HeapFunction:
		return checkPayloadPointer(&allocator.functions, payload, payload.Function)
	case HeapPromise:
		return checkPayloadPointer(&allocator.promises, payload, payload.Promise)
	case HeapBigInt:
		return checkPayloadPointer(&allocator.bigInts, payload, payload.BigInt)
	case HeapSymbol:
		return checkPayloadPointer(&allocator.symbols, payload, payload.Symbol)
	case HeapArrayBuffer:
		return checkPayloadPointer(&allocator.arrayBuffers, payload, payload.ArrayBuffer)
	case HeapTypedArray:
		return checkPayloadPointer(&allocator.typedArrays, payload, payload.TypedArray)
	case HeapMap:
		return checkPayloadPointer(&allocator.maps, payload, payload.Map)
	case HeapSet:
		return checkPayloadPointer(&allocator.sets, payload, payload.Set)
	case HeapDate:
		return checkPayloadPointer(&allocator.dates, payload, payload.Date)
	case HeapRegExp:
		return checkPayloadPointer(&allocator.regExps, payload, payload.RegExp)
	case HeapError:
		return checkPayloadPointer(&allocator.errors, payload, payload.Error)
	case HeapWeakMap:
		return checkPayloadPointer(&allocator.weakMaps, payload, payload.WeakMap)
	case HeapWeakSet:
		return checkPayloadPointer(&allocator.weakSets, payload, payload.WeakSet)
	case HeapIterator:
		return checkPayloadPointer(&allocator.iterators, payload, payload.Iterator)
	case HeapHostObject:
		return checkPayloadPointer(&allocator.hostObjects, payload, payload.HostObject)
	default:
		return fmt.Errorf("invalid heap kind %d", kind)
	}
}

func (allocator *payloadAllocator) check(stats Stats) error {
	checks := []struct {
		kind HeapKind
		err  error
	}{
		{HeapCell, allocator.cells.check(stats.LiveCells)},
		{HeapString, allocator.strings.check(stats.LiveStrings)},
		{HeapObject, allocator.objects.check(stats.LiveObjects)},
		{HeapArray, allocator.arrays.check(stats.LiveArrays)},
		{HeapContext, allocator.contexts.check(stats.LiveContexts)},
		{HeapFunction, allocator.functions.check(stats.LiveFunctions)},
		{HeapPromise, allocator.promises.check(stats.LivePromises)},
		{HeapBigInt, allocator.bigInts.check(stats.LiveBigInts)},
		{HeapSymbol, allocator.symbols.check(stats.LiveSymbols)},
		{HeapArrayBuffer, allocator.arrayBuffers.check(stats.LiveArrayBuffers)},
		{HeapTypedArray, allocator.typedArrays.check(stats.LiveTypedArrays)},
		{HeapMap, allocator.maps.check(stats.LiveMaps)},
		{HeapSet, allocator.sets.check(stats.LiveSets)},
		{HeapDate, allocator.dates.check(stats.LiveDates)},
		{HeapRegExp, allocator.regExps.check(stats.LiveRegExps)},
		{HeapError, allocator.errors.check(stats.LiveErrors)},
		{HeapWeakMap, allocator.weakMaps.check(stats.LiveWeakMaps)},
		{HeapWeakSet, allocator.weakSets.check(stats.LiveWeakSets)},
		{HeapIterator, allocator.iterators.check(stats.LiveIterators)},
		{HeapHostObject, allocator.hostObjects.check(stats.LiveHostObjects)},
	}
	for _, check := range checks {
		if check.err != nil {
			return fmt.Errorf("%s arena: %w", check.kind, check.err)
		}
	}
	return nil
}
