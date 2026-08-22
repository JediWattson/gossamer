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
	handle      payloadHandle
	Cell        *Cell
	String      *StringObject
	Object      *Object
	Array       *Array
	Context     *Context
	Function    *Function
	Promise     *Promise
	BigInt      *BigInt
	Symbol      *Symbol
	ArrayBuffer *ArrayBuffer
	TypedArray  *TypedArray
	Map         *Map
	Set         *Set
	Date        *Date
	RegExp      *RegExp
	Error       *ErrorObject
	WeakMap     *WeakMap
	WeakSet     *WeakSet
	Iterator    *Iterator
	HostObject  *HostObject
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
	result.slotPayload = &slotPayload{}
	switch slot.Kind {
	case HeapCell:
		result.Cell = payloadPointer(cloneCell(*slot.Cell))
	case HeapString:
		result.String = payloadPointer(cloneString(*slot.String))
	case HeapObject:
		result.Object = payloadPointer(cloneObject(*slot.Object))
	case HeapArray:
		result.Array = payloadPointer(cloneArray(*slot.Array))
	case HeapContext:
		result.Context = payloadPointer(cloneContext(*slot.Context))
	case HeapFunction:
		result.Function = payloadPointer(cloneFunction(*slot.Function))
	case HeapPromise:
		result.Promise = payloadPointer(clonePromise(*slot.Promise))
	case HeapBigInt:
		result.BigInt = payloadPointer(cloneBigInt(*slot.BigInt))
	case HeapSymbol:
		result.Symbol = payloadPointer(cloneSymbol(*slot.Symbol))
	case HeapArrayBuffer:
		result.ArrayBuffer = payloadPointer(cloneArrayBuffer(*slot.ArrayBuffer))
	case HeapTypedArray:
		result.TypedArray = payloadPointer(cloneTypedArray(*slot.TypedArray))
	case HeapMap:
		result.Map = payloadPointer(cloneMap(*slot.Map))
	case HeapSet:
		result.Set = payloadPointer(cloneSet(*slot.Set))
	case HeapDate:
		result.Date = payloadPointer(*slot.Date)
	case HeapRegExp:
		result.RegExp = payloadPointer(cloneRegExp(*slot.RegExp))
	case HeapError:
		result.Error = payloadPointer(cloneError(*slot.Error))
	case HeapWeakMap:
		result.WeakMap = payloadPointer(cloneWeakMap(*slot.WeakMap))
	case HeapWeakSet:
		result.WeakSet = payloadPointer(cloneWeakSet(*slot.WeakSet))
	case HeapIterator:
		result.Iterator = payloadPointer(cloneIterator(*slot.Iterator))
	case HeapHostObject:
		result.HostObject = payloadPointer(*slot.HostObject)
	}
	return result
}

func payloadPointer[T any](value T) *T {
	return &value
}

func appendSlotReferences(values []Value, slot *Slot) []Value {
	if slot == nil || !slot.Occupied {
		return values
	}
	if slot.Kind == HeapCell {
		return append(values, slot.Cell.Fields...)
	}
	values = appendObjectHeaderReferences(values, slot)
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
		if slot.Function.ThisMode == FunctionThisLexical {
			values = append(values, slot.Function.LexicalThis)
		}
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
		return append(values, slot.Symbol.Description)
	}
	if slot.Kind == HeapTypedArray {
		if slot.TypedArray.Buffer == (Ref{}) {
			return values
		}
		return append(values, RefValue(slot.TypedArray.Buffer))
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
			return values
		}
		return append(values, RefValue(slot.RegExp.Pattern))
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
	return values
}

func appendObjectHeaderReferences(values []Value, slot *Slot) []Value {
	header, ok := objectHeaderForSlot(slot)
	if !ok {
		return values
	}
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

func (store *Store) initializeSlotPayloadLocked(slot *Slot, kind HeapKind) {
	slot.slotPayload = store.payloads.allocate(kind)
	slot.Kind = kind
	if header, ok := objectHeaderForSlot(slot); ok {
		header.Prototype = NullValue()
	}
	switch kind {
	case HeapContext:
		slot.Context.Parent = NullValue()
	}
}

func (store *Store) clearSlotPayloadLocked(slot *Slot) error {
	if err := store.payloads.release(slot.Kind, slot.slotPayload); err != nil {
		return err
	}
	slot.Kind = HeapInvalid
	slot.slotPayload = nil
	return nil
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
