package memory

import "fmt"

// RegionID identifies physical RegionStore storage. It deliberately remains
// separate from ownership.RegionID: ownership claims move, physical refs do
// not.
type RegionID uint64

// Ref is an unpacked, generation-checked reference to one Cell.
type Ref struct {
	Region RegionID
	Slot   uint32
	Gen    uint32
}

func (ref Ref) String() string {
	return fmt.Sprintf("R%d:%d:g%d", ref.Region, ref.Slot, ref.Gen)
}
