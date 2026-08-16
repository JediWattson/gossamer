package memory

import (
	"bytes"
	"fmt"
	"math"

	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

func (store *Store) AllocMap(owner ownership.OwnerID, regionID RegionID) (Ref, error) {
	if store == nil {
		return Ref{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return store.allocKindLocked(owner, regionID, HeapMap, false)
}

func (store *Store) DerefMap(owner ownership.OwnerID, ref Ref) (Map, error) {
	if store == nil {
		return Map{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	_, slot, err := store.readSlotLocked(owner, ref)
	if err != nil {
		return Map{}, err
	}
	if slot.Kind != HeapMap {
		return Map{}, typeError(ref, slot.Kind, HeapMap)
	}
	return cloneMap(slot.Map), nil
}

func (store *Store) MapSet(owner ownership.OwnerID, ref Ref, key, value Value) error {
	if store == nil {
		return fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return store.mapSetLocked(owner, ref, key, value, false)
}

func (store *Store) mapSetLocked(owner ownership.OwnerID, ref Ref, key, value Value, internal bool) error {
	region, slot, err := store.writeSlotLocked(owner, ref, internal)
	if err != nil {
		return err
	}
	if slot.Kind != HeapMap {
		return typeError(ref, slot.Kind, HeapMap)
	}
	key, err = store.prepareEscapingValueLocked(region, key, internal)
	if err != nil {
		return err
	}
	value, err = store.prepareEscapingValueLocked(region, value, internal)
	if err != nil {
		return err
	}
	if err := store.validateValueAccessLocked(owner, key, internal); err != nil {
		return err
	}
	index, err := store.findMapEntryLocked(slot, key)
	if err != nil {
		return err
	}
	if index >= 0 {
		old := slot.Map.Entries[index].Value
		value, err = store.replaceValueLocked(owner, region, slot, old, value, internal)
		if err != nil {
			return err
		}
		slot.Map.Entries[index].Value = value
		return nil
	}
	key, err = store.replaceValueLocked(owner, region, slot, Value{}, key, internal)
	if err != nil {
		return err
	}
	value, err = store.replaceValueLocked(owner, region, slot, Value{}, value, internal)
	if err != nil {
		_, _ = store.replaceValueLocked(owner, region, slot, key, Value{}, true)
		return err
	}
	slot.Map.Entries = append(slot.Map.Entries, MapEntry{Key: key, Value: value})
	return nil
}

func (store *Store) MapGet(owner ownership.OwnerID, ref Ref, key Value) (Value, bool, error) {
	if store == nil {
		return Value{}, false, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	_, slot, err := store.readSlotLocked(owner, ref)
	if err != nil {
		return Value{}, false, err
	}
	if slot.Kind != HeapMap {
		return Value{}, false, typeError(ref, slot.Kind, HeapMap)
	}
	if err := store.validateValueAccessLocked(owner, key, false); err != nil {
		return Value{}, false, err
	}
	index, err := store.findMapEntryLocked(slot, key)
	if err != nil {
		return Value{}, false, err
	}
	if index < 0 {
		return Value{}, false, nil
	}
	return slot.Map.Entries[index].Value, true, nil
}

func (store *Store) MapDelete(owner ownership.OwnerID, ref Ref, key Value) (bool, error) {
	if store == nil {
		return false, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	region, slot, err := store.writeSlotLocked(owner, ref, false)
	if err != nil {
		return false, err
	}
	if slot.Kind != HeapMap {
		return false, typeError(ref, slot.Kind, HeapMap)
	}
	if err := store.validateValueAccessLocked(owner, key, false); err != nil {
		return false, err
	}
	index, err := store.findMapEntryLocked(slot, key)
	if err != nil || index < 0 {
		return false, err
	}
	entry := slot.Map.Entries[index]
	if _, err := store.replaceValueLocked(owner, region, slot, entry.Key, Value{}, false); err != nil {
		return false, err
	}
	if _, err := store.replaceValueLocked(owner, region, slot, entry.Value, Value{}, false); err != nil {
		_, _ = store.replaceValueLocked(owner, region, slot, Value{}, entry.Key, true)
		return false, err
	}
	copy(slot.Map.Entries[index:], slot.Map.Entries[index+1:])
	slot.Map.Entries[len(slot.Map.Entries)-1] = MapEntry{}
	slot.Map.Entries = slot.Map.Entries[:len(slot.Map.Entries)-1]
	return true, nil
}

func (store *Store) MapClear(owner ownership.OwnerID, ref Ref) error {
	if store == nil {
		return fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	region, slot, err := store.writeSlotLocked(owner, ref, false)
	if err != nil {
		return err
	}
	if slot.Kind != HeapMap {
		return typeError(ref, slot.Kind, HeapMap)
	}
	values := slotReferences(slot)
	unlinked := make([]Value, 0, len(values))
	for _, value := range values {
		if _, err := store.replaceValueLocked(owner, region, slot, value, Value{}, false); err != nil {
			for index := len(unlinked) - 1; index >= 0; index-- {
				_, _ = store.replaceValueLocked(owner, region, slot, Value{}, unlinked[index], true)
			}
			return err
		}
		unlinked = append(unlinked, value)
	}
	slot.Map.Entries = nil
	return nil
}

func (store *Store) validateValueAccessLocked(owner ownership.OwnerID, value Value, internal bool) error {
	if !value.IsRef() {
		return nil
	}
	_, _, err := store.readSlotLocked(owner, value.Ref())
	if err != nil && internal {
		_, _, err = store.slotLocked(value.Ref())
	}
	return err
}

func (store *Store) findMapEntryLocked(slot *Slot, key Value) (int, error) {
	for index, entry := range slot.Map.Entries {
		equal, err := store.sameValueZeroLocked(entry.Key, key)
		if err != nil {
			return -1, err
		}
		if equal {
			return index, nil
		}
	}
	return -1, nil
}

func (store *Store) sameValueZeroLocked(left, right Value) (bool, error) {
	if left.Kind() != right.Kind() {
		return false, nil
	}
	switch left.Kind() {
	case ValueUndefined, ValueNull:
		return true, nil
	case ValueBool:
		return left.Bool() == right.Bool(), nil
	case ValueNumber:
		return left.Number() == right.Number() || (math.IsNaN(left.Number()) && math.IsNaN(right.Number())), nil
	case ValueReference:
		if left.Ref() == right.Ref() {
			return true, nil
		}
		_, leftSlot, err := store.slotLocked(left.Ref())
		if err != nil {
			return false, err
		}
		_, rightSlot, err := store.slotLocked(right.Ref())
		if err != nil {
			return false, err
		}
		if leftSlot.Kind != rightSlot.Kind {
			return false, nil
		}
		switch leftSlot.Kind {
		case HeapString:
			return leftSlot.String.Text == rightSlot.String.Text, nil
		case HeapBigInt:
			return leftSlot.BigInt.Negative == rightSlot.BigInt.Negative && bytes.Equal(leftSlot.BigInt.Magnitude, rightSlot.BigInt.Magnitude), nil
		case HeapSymbol:
			return leftSlot.Symbol.ID == rightSlot.Symbol.ID, nil
		default:
			return false, nil
		}
	default:
		return false, nil
	}
}
