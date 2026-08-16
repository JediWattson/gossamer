package memory

import (
	"fmt"

	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

func (store *Store) AllocSymbol(owner ownership.OwnerID, regionID RegionID, description Value) (Ref, error) {
	if store == nil {
		return Ref{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	id := SymbolID(nextSymbolID.Add(1))
	if id == 0 {
		return Ref{}, fmt.Errorf("%w: exhausted Symbol IDs", ErrInvalidSymbol)
	}
	ref, err := store.allocKindLocked(owner, regionID, HeapSymbol, false)
	if err != nil {
		return Ref{}, err
	}
	if err := store.initializeSymbolLocked(owner, ref, Symbol{ID: id, Description: description}, false); err != nil {
		_ = store.freeLocked(owner, ref, true)
		return Ref{}, err
	}
	return ref, nil
}

func (store *Store) DerefSymbol(owner ownership.OwnerID, ref Ref) (Symbol, error) {
	if store == nil {
		return Symbol{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	_, slot, err := store.readSlotLocked(owner, ref)
	if err != nil {
		return Symbol{}, err
	}
	if slot.Kind != HeapSymbol {
		return Symbol{}, typeError(ref, slot.Kind, HeapSymbol)
	}
	return cloneSymbol(slot.Symbol), nil
}

func (store *Store) SameSymbol(owner ownership.OwnerID, left, right Ref) (bool, error) {
	if store == nil {
		return false, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	_, leftSlot, err := store.readSlotLocked(owner, left)
	if err != nil {
		return false, err
	}
	if leftSlot.Kind != HeapSymbol {
		return false, typeError(left, leftSlot.Kind, HeapSymbol)
	}
	_, rightSlot, err := store.readSlotLocked(owner, right)
	if err != nil {
		return false, err
	}
	if rightSlot.Kind != HeapSymbol {
		return false, typeError(right, rightSlot.Kind, HeapSymbol)
	}
	return leftSlot.Symbol.ID == rightSlot.Symbol.ID, nil
}

func (store *Store) initializeSymbolLocked(owner ownership.OwnerID, ref Ref, symbol Symbol, internal bool) error {
	region, slot, err := store.writeSlotLocked(owner, ref, internal)
	if err != nil {
		return err
	}
	if slot.Kind != HeapSymbol {
		return typeError(ref, slot.Kind, HeapSymbol)
	}
	if slot.Symbol.ID != 0 || symbol.ID == 0 {
		return fmt.Errorf("%w: zero or repeated identity", ErrInvalidSymbol)
	}
	if err := store.validateOptionalTypedValueLocked(owner, symbol.Description, HeapString, "Symbol description", internal); err != nil {
		return err
	}
	description, err := store.replaceValueLocked(owner, region, slot, Value{}, symbol.Description, internal)
	if err != nil {
		return err
	}
	symbol.Description = description
	slot.Symbol = cloneSymbol(symbol)
	return nil
}
