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
)

// StringObject is the first real typed native-heap payload. Text is immutable;
// cloning a graph clones its backing bytes into the destination region.
type StringObject struct {
	Text string
}

// Slot holds one generation-checked typed heap payload.
type Slot struct {
	Generation  uint32
	Kind        HeapKind
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
	Occupied    bool

	object ownership.ObjectID
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

func cloneCell(cell Cell) Cell {
	return Cell{Fields: append([]Value(nil), cell.Fields...)}
}

func cloneString(value StringObject) StringObject {
	return StringObject{Text: strings.Clone(value.Text)}
}

func cloneSlot(slot Slot) Slot {
	return Slot{
		Generation:  slot.Generation,
		Kind:        slot.Kind,
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
		Occupied:    slot.Occupied,
	}
}

func slotReferences(slot *Slot) []Value {
	if slot == nil || !slot.Occupied {
		return nil
	}
	if slot.Kind == HeapCell {
		return slot.Cell.Fields
	}
	if slot.Kind == HeapObject {
		values := make([]Value, 0, 1+len(slot.Object.Properties)*2)
		values = append(values, slot.Object.Prototype)
		for _, property := range slot.Object.Properties {
			values = append(values, RefValue(property.Name), property.Value)
		}
		return values
	}
	if slot.Kind == HeapArray {
		values := make([]Value, len(slot.Array.Elements))
		for index, element := range slot.Array.Elements {
			values[index] = element.Value
		}
		return values
	}
	if slot.Kind == HeapContext {
		values := make([]Value, 0, 1+len(slot.Context.Bindings)*2)
		values = append(values, slot.Context.Parent)
		for _, binding := range slot.Context.Bindings {
			values = append(values, RefValue(binding.Name))
			if binding.Initialized {
				values = append(values, binding.Value)
			}
		}
		return values
	}
	if slot.Kind == HeapFunction {
		values := make([]Value, 0, 2+len(slot.Function.Constants))
		values = append(values, slot.Function.Name, slot.Function.Environment)
		values = append(values, slot.Function.Constants...)
		return values
	}
	if slot.Kind == HeapPromise {
		values := make([]Value, 0, 1+len(slot.Promise.Reactions)*3)
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
		values := make([]Value, 0, len(slot.Map.Entries)*2)
		for _, entry := range slot.Map.Entries {
			values = append(values, entry.Key, entry.Value)
		}
		return values
	}
	if slot.Kind == HeapSet {
		return slot.Set.Values
	}
	return nil
}

func slotStorageEmpty(slot *Slot) bool {
	return slot != nil && slot.Kind == HeapInvalid && len(slot.Cell.Fields) == 0 && slot.String.Text == "" && slot.Object.Prototype == (Value{}) && len(slot.Object.Properties) == 0 && slot.Array.Length == 0 && len(slot.Array.Elements) == 0 && slot.Context.Parent == (Value{}) && len(slot.Context.Bindings) == 0 && slot.Function.Kind == 0 && slot.Function.Name == (Value{}) && slot.Function.Environment == (Value{}) && slot.Function.Arity == 0 && len(slot.Function.Code) == 0 && len(slot.Function.Constants) == 0 && slot.Function.NativeID == 0 && slot.Promise.State == PromisePending && slot.Promise.Result == (Value{}) && len(slot.Promise.Reactions) == 0 && !slot.Promise.Handled && !slot.BigInt.Negative && len(slot.BigInt.Magnitude) == 0 && slot.Symbol.ID == 0 && slot.Symbol.Description == (Value{}) && len(slot.ArrayBuffer.Bytes) == 0 && !slot.ArrayBuffer.Detached && slot.TypedArray == (TypedArray{}) && len(slot.Map.Entries) == 0 && len(slot.Set.Values) == 0 && slot.Date.Milliseconds == 0
}

func clearSlotPayload(slot *Slot) {
	slot.Kind = HeapInvalid
	slot.Cell = Cell{}
	slot.String = StringObject{}
	slot.Object = Object{}
	slot.Array = Array{}
	slot.Context = Context{}
	slot.Function = Function{}
	slot.Promise = Promise{}
	slot.BigInt = BigInt{}
	slot.Symbol = Symbol{}
	slot.ArrayBuffer = ArrayBuffer{}
	slot.TypedArray = TypedArray{}
	slot.Map = Map{}
	slot.Set = Set{}
	slot.Date = Date{}
}

func initializeSlotPayload(slot *Slot, kind HeapKind) {
	clearSlotPayload(slot)
	slot.Kind = kind
	switch kind {
	case HeapObject:
		slot.Object.Prototype = NullValue()
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
