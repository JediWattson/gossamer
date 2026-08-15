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
)

// StringObject is the first real typed native-heap payload. Text is immutable;
// cloning a graph clones its backing bytes into the destination region.
type StringObject struct {
	Text string
}

// Slot holds one generation-checked typed heap payload.
type Slot struct {
	Generation uint32
	Kind       HeapKind
	Cell       Cell
	String     StringObject
	Object     Object
	Array      Array
	Occupied   bool

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
		Generation: slot.Generation,
		Kind:       slot.Kind,
		Cell:       cloneCell(slot.Cell),
		String:     cloneString(slot.String),
		Object:     cloneObject(slot.Object),
		Array:      cloneArray(slot.Array),
		Occupied:   slot.Occupied,
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
	return nil
}

func slotStorageEmpty(slot *Slot) bool {
	return slot != nil && slot.Kind == HeapInvalid && len(slot.Cell.Fields) == 0 && slot.String.Text == "" && slot.Object.Prototype == (Value{}) && len(slot.Object.Properties) == 0 && slot.Array.Length == 0 && len(slot.Array.Elements) == 0
}

func clearSlotPayload(slot *Slot) {
	slot.Kind = HeapInvalid
	slot.Cell = Cell{}
	slot.String = StringObject{}
	slot.Object = Object{}
	slot.Array = Array{}
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
