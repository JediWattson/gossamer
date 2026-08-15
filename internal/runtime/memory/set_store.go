package memory

import (
	"fmt"

	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

func (store *Store) AllocSet(owner ownership.OwnerID, regionID RegionID) (Ref, error) {
	if store == nil {
		return Ref{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return store.allocKindLocked(owner, regionID, HeapSet, false)
}

func (store *Store) DerefSet(owner ownership.OwnerID, ref Ref) (Set, error) {
	if store == nil {
		return Set{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	_, slot, err := store.readSlotLocked(owner, ref)
	if err != nil {
		return Set{}, err
	}
	if slot.Kind != HeapSet {
		return Set{}, typeError(ref, slot.Kind, HeapSet)
	}
	return cloneSet(slot.Set), nil
}

func (store *Store) SetAdd(owner ownership.OwnerID, ref Ref, value Value) error {
	if store == nil {
		return fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return store.setAddLocked(owner, ref, value, false)
}

func (store *Store) setAddLocked(owner ownership.OwnerID, ref Ref, value Value, internal bool) error {
	region, slot, err := store.writeSlotLocked(owner, ref, internal)
	if err != nil {
		return err
	}
	if slot.Kind != HeapSet {
		return typeError(ref, slot.Kind, HeapSet)
	}
	if err := store.validateValueAccessLocked(owner, value, internal); err != nil {
		return err
	}
	index, err := store.findSetValueLocked(slot, value)
	if err != nil || index >= 0 {
		return err
	}
	if err := store.replaceValueLocked(owner, region, slot, Value{}, value, internal); err != nil {
		return err
	}
	slot.Set.Values = append(slot.Set.Values, value)
	return nil
}

func (store *Store) SetHas(owner ownership.OwnerID, ref Ref, value Value) (bool, error) {
	if store == nil {
		return false, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	_, slot, err := store.readSlotLocked(owner, ref)
	if err != nil {
		return false, err
	}
	if slot.Kind != HeapSet {
		return false, typeError(ref, slot.Kind, HeapSet)
	}
	if err := store.validateValueAccessLocked(owner, value, false); err != nil {
		return false, err
	}
	index, err := store.findSetValueLocked(slot, value)
	return index >= 0, err
}

func (store *Store) SetDelete(owner ownership.OwnerID, ref Ref, value Value) (bool, error) {
	if store == nil {
		return false, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	region, slot, err := store.writeSlotLocked(owner, ref, false)
	if err != nil {
		return false, err
	}
	if slot.Kind != HeapSet {
		return false, typeError(ref, slot.Kind, HeapSet)
	}
	if err := store.validateValueAccessLocked(owner, value, false); err != nil {
		return false, err
	}
	index, err := store.findSetValueLocked(slot, value)
	if err != nil || index < 0 {
		return false, err
	}
	stored := slot.Set.Values[index]
	if err := store.replaceValueLocked(owner, region, slot, stored, Value{}, false); err != nil {
		return false, err
	}
	copy(slot.Set.Values[index:], slot.Set.Values[index+1:])
	slot.Set.Values[len(slot.Set.Values)-1] = Value{}
	slot.Set.Values = slot.Set.Values[:len(slot.Set.Values)-1]
	return true, nil
}

func (store *Store) SetClear(owner ownership.OwnerID, ref Ref) error {
	if store == nil {
		return fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	region, slot, err := store.writeSlotLocked(owner, ref, false)
	if err != nil {
		return err
	}
	if slot.Kind != HeapSet {
		return typeError(ref, slot.Kind, HeapSet)
	}
	unlinked := make([]Value, 0, len(slot.Set.Values))
	for _, value := range slot.Set.Values {
		if err := store.replaceValueLocked(owner, region, slot, value, Value{}, false); err != nil {
			for index := len(unlinked) - 1; index >= 0; index-- {
				_ = store.replaceValueLocked(owner, region, slot, Value{}, unlinked[index], true)
			}
			return err
		}
		unlinked = append(unlinked, value)
	}
	slot.Set.Values = nil
	return nil
}

func (store *Store) findSetValueLocked(slot *Slot, value Value) (int, error) {
	for index, stored := range slot.Set.Values {
		equal, err := store.sameValueZeroLocked(stored, value)
		if err != nil {
			return -1, err
		}
		if equal {
			return index, nil
		}
	}
	return -1, nil
}
