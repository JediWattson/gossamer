package memory

import (
	"strings"

	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

// RegionState describes whether storage is mutable, moving through a queue,
// globally readable, or gone.
type RegionState uint8

const (
	RegionPrivate RegionState = iota
	RegionInTransit
	RegionPublished
	RegionDestroyed
)

// Cell is the deliberately small object used to prove the heap semantics.
type Cell struct {
	Fields []Value
}

// HeapKind identifies the concrete payload stored in a generation-checked
// slot. Refs stay untyped so the Store can reject type confusion at deref.
type HeapKind uint8

const (
	HeapInvalid HeapKind = iota
	HeapCell
	HeapString
	HeapObject
	HeapArray
	HeapContext
	HeapFunction
	HeapPromise
	HeapBigInt
	HeapSymbol
	HeapArrayBuffer
	HeapTypedArray
	HeapMap
	HeapSet
	HeapDate
	HeapRegExp
	HeapError
	HeapWeakMap
	HeapWeakSet
	HeapIterator
	HeapHostObject
)

// StringObject is the first real typed native-heap payload. Text is immutable;
// cloning a graph clones its backing bytes into the destination region.
type StringObject struct {
	Text string
}

// slotPayload is allocated only while a slot is occupied. Keeping the stable
// Ref metadata outside this union lets regions retain generation tombstones
// without pinning every possible heap representation for every vacant slot.
type slotPayload struct {
	Cell        Cell
	String      StringObject
	Object      Object
	Array       Array
	Context     Context
	Function    Function
	Promise     Promise
	BigInt      BigInt
	Symbol      Symbol
	ArrayBuffer ArrayBuffer
	TypedArray  TypedArray
	Map         Map
	Set         Set
	Date        Date
	RegExp      RegExp
	Error       ErrorObject
	WeakMap     WeakMap
	WeakSet     WeakSet
	Iterator    Iterator
	HostObject  HostObject
}

// Slot holds one generation-checked typed heap payload. The payload pointer is
// deliberately embedded so existing typed access remains direct while vacant
// slots retain only the stable header needed to reject stale Refs.
type Slot struct {
	object ownership.ObjectID
	*slotPayload
	Generation uint32
	Kind       HeapKind
	Occupied   bool
}

// Region is a snapshot-compatible physical allocation region. Store methods
// return copies so callers cannot mutate slots around the write barrier.
type Region struct {
	ID    RegionID
	Owner ownership.OwnerID
	State RegionState
	Slots []Slot

	claim ownership.RegionID
	free  []uint32
}

// RegionMetadata is the allocation-free ownership/state view used by hot
// execution paths. Region remains the deep snapshot API for diagnostics and
// tests that explicitly need slot payloads.
type RegionMetadata struct {
	ID           RegionID
	Owner        ownership.OwnerID
	State        RegionState
	Slots        int
	SlotCapacity int
}

func cloneCell(cell Cell) Cell {
	return Cell{Fields: append([]Value(nil), cell.Fields...)}
}

func cloneString(value StringObject) StringObject {
	return StringObject{Text: strings.Clone(value.Text)}
}

func cloneSlot(slot Slot) Slot {
	result := Slot{
		Generation: slot.Generation,
		Kind:       slot.Kind,
		Occupied:   slot.Occupied,
	}
	if slot.slotPayload == nil {
		return result
	}
	result.slotPayload = &slotPayload{
		Cell:        cloneCell(slot.Cell),
		String:      cloneString(slot.String),
		Object:      cloneObject(slot.Object),
		Array:       cloneArray(slot.Array),
		Context:     cloneContext(slot.Context),
		Function:    cloneFunction(slot.Function),
		Promise:     clonePromise(slot.Promise),
		BigInt:      cloneBigInt(slot.BigInt),
		Symbol:      cloneSymbol(slot.Symbol),
		ArrayBuffer: cloneArrayBuffer(slot.ArrayBuffer),
		TypedArray:  cloneTypedArray(slot.TypedArray),
		Map:         cloneMap(slot.Map),
		Set:         cloneSet(slot.Set),
		Date:        slot.Date,
		RegExp:      cloneRegExp(slot.RegExp),
		Error:       cloneError(slot.Error),
		WeakMap:     cloneWeakMap(slot.WeakMap),
		WeakSet:     cloneWeakSet(slot.WeakSet),
		Iterator:    cloneIterator(slot.Iterator),
		HostObject:  slot.HostObject,
	}
	return result
}

func slotReferences(slot *Slot) []Value {
	if slot == nil || !slot.Occupied {
		return nil
	}
	if slot.Kind == HeapCell {
		return slot.Cell.Fields
	}
	values := objectHeaderReferences(slot)
	if slot.Kind == HeapObject {
		return values
	}
	if slot.Kind == HeapArray {
		for _, element := range slot.Array.Elements {
			values = append(values, element.Value)
		}
		return values
	}
	if slot.Kind == HeapContext {
		values := make([]Value, 0, 1+len(slot.Context.Bindings)*3)
		values = append(values, slot.Context.Parent)
		for _, binding := range slot.Context.Bindings {
			values = append(values, RefValue(binding.Name))
			if binding.Indirect {
				values = append(values, RefValue(binding.Target), RefValue(binding.TargetName))
			} else if binding.Initialized {
				values = append(values, binding.Value)
			}
		}
		return values
	}
	if slot.Kind == HeapFunction {
		values = append(values, slot.Function.Name, slot.Function.Environment)
		values = append(values, slot.Function.Constants...)
		values = append(values, slot.Function.Captures...)
		return values
	}
	if slot.Kind == HeapPromise {
		if slot.Promise.State != PromisePending {
			values = append(values, slot.Promise.Result)
		}
		for _, reaction := range slot.Promise.Reactions {
			values = append(values, reaction.OnFulfilled, reaction.OnRejected, reaction.Downstream)
		}
		return values
	}
	if slot.Kind == HeapSymbol {
		return []Value{slot.Symbol.Description}
	}
	if slot.Kind == HeapTypedArray {
		if slot.TypedArray.Buffer == (Ref{}) {
			return nil
		}
		return []Value{RefValue(slot.TypedArray.Buffer)}
	}
	if slot.Kind == HeapMap {
		for _, entry := range slot.Map.Entries {
			values = append(values, entry.Key, entry.Value)
		}
		return values
	}
	if slot.Kind == HeapSet {
		return append(values, slot.Set.Values...)
	}
	if slot.Kind == HeapRegExp {
		if slot.RegExp.Pattern == (Ref{}) {
			return nil
		}
		return []Value{RefValue(slot.RegExp.Pattern)}
	}
	if slot.Kind == HeapError {
		values = append(values, slot.Error.Message, slot.Error.Stack)
		if slot.Error.HasCause {
			values = append(values, slot.Error.Cause)
		}
		values = append(values, slot.Error.Errors...)
		return values
	}
	if slot.Kind == HeapIterator {
		return append(values, RefValue(slot.Iterator.Target))
	}
	return nil
}

func objectHeaderReferences(slot *Slot) []Value {
	header, ok := objectHeaderForSlot(slot)
	if !ok {
		return nil
	}
	values := make([]Value, 0, 1+len(header.Properties)*4)
	values = append(values, header.Prototype)
	for _, property := range header.Properties {
		values = append(values, RefValue(property.Name), property.Value, property.Getter, property.Setter)
	}
	return values
}

func slotStorageEmpty(slot *Slot) bool {
	return slot != nil && slot.Kind == HeapInvalid && slot.slotPayload == nil
}

func objectHeaderStorageEmpty(header ObjectHeader) bool {
	return header.Prototype == (Value{}) && len(header.Properties) == 0 && !header.NonExtensible && !header.ImmutablePrototype
}

func clearSlotPayload(slot *Slot) {
	slot.Kind = HeapInvalid
	slot.slotPayload = nil
}

func initializeSlotPayload(slot *Slot, kind HeapKind) {
	clearSlotPayload(slot)
	slot.slotPayload = &slotPayload{}
	slot.Kind = kind
	if header, ok := objectHeaderForSlot(slot); ok {
		header.Prototype = NullValue()
	}
	switch kind {
	case HeapContext:
		slot.Context.Parent = NullValue()
	}
}

func cloneRegion(region *Region) Region {
	result := Region{
		ID:    region.ID,
		Owner: region.Owner,
		State: region.State,
		Slots: make([]Slot, len(region.Slots)),
	}
	for index, slot := range region.Slots {
		result.Slots[index] = cloneSlot(slot)
	}
	return result
}
