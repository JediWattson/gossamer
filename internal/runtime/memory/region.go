package memory

import "github.com/JediWattson/gossamer/internal/runtime/ownership"

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

// Slot holds one generation-checked Cell.
type Slot struct {
	Generation uint32
	Cell       Cell
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

func cloneRegion(region *Region) Region {
	result := Region{
		ID:    region.ID,
		Owner: region.Owner,
		State: region.State,
		Slots: make([]Slot, len(region.Slots)),
	}
	for index, slot := range region.Slots {
		result.Slots[index] = Slot{
			Generation: slot.Generation,
			Cell:       cloneCell(slot.Cell),
			Occupied:   slot.Occupied,
		}
	}
	return result
}
