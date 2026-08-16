package memory

import (
	"errors"
	"fmt"

	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

var ErrWeakKeyLifetime = errors.New("memory: weak key has a shorter lifetime than its table")

func (store *Store) AllocWeakMap(owner ownership.OwnerID, regionID RegionID) (Ref, error) {
	if store == nil {
		return Ref{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return store.allocKindLocked(owner, regionID, HeapWeakMap, false)
}

func (store *Store) DerefWeakMap(owner ownership.OwnerID, ref Ref) (WeakMap, error) {
	if store == nil {
		return WeakMap{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	_, slot, err := store.readSlotLocked(owner, ref)
	if err != nil {
		return WeakMap{}, err
	}
	if slot.Kind != HeapWeakMap {
		return WeakMap{}, typeError(ref, slot.Kind, HeapWeakMap)
	}
	return cloneWeakMap(slot.WeakMap), nil
}

func (store *Store) WeakMapSet(owner ownership.OwnerID, ref, key Ref, value Value) error {
	if store == nil {
		return fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return store.weakMapSetLocked(owner, ref, key, value, false)
}

func (store *Store) weakMapSetLocked(owner ownership.OwnerID, ref, key Ref, value Value, internal bool) error {
	region, slot, err := store.writeSlotLocked(owner, ref, internal)
	if err != nil {
		return err
	}
	if slot.Kind != HeapWeakMap {
		return typeError(ref, slot.Kind, HeapWeakMap)
	}
	if err := store.validateWeakKeyLocked(region, key, internal); err != nil {
		return err
	}
	value, err = store.prepareEscapingValueLocked(region, value, internal)
	if err != nil {
		return err
	}
	if err := store.validateValueAccessLocked(owner, value, internal); err != nil {
		return err
	}
	for index := range slot.WeakMap.Entries {
		if slot.WeakMap.Entries[index].Key == key {
			slot.WeakMap.Entries[index].Value = value
			return nil
		}
	}
	slot.WeakMap.Entries = append(slot.WeakMap.Entries, WeakMapEntry{Key: key, Value: value})
	return nil
}

func (store *Store) WeakMapGet(owner ownership.OwnerID, ref, key Ref) (Value, bool, error) {
	if store == nil {
		return Value{}, false, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	_, slot, err := store.readSlotLocked(owner, ref)
	if err != nil {
		return Value{}, false, err
	}
	if slot.Kind != HeapWeakMap {
		return Value{}, false, typeError(ref, slot.Kind, HeapWeakMap)
	}
	if err := store.validateWeakKeyAccessLocked(owner, key); err != nil {
		return Value{}, false, err
	}
	for _, entry := range slot.WeakMap.Entries {
		if entry.Key == key {
			return entry.Value, true, nil
		}
	}
	return Value{}, false, nil
}

func (store *Store) WeakMapHas(owner ownership.OwnerID, ref, key Ref) (bool, error) {
	_, found, err := store.WeakMapGet(owner, ref, key)
	return found, err
}

func (store *Store) WeakMapDelete(owner ownership.OwnerID, ref, key Ref) (bool, error) {
	if store == nil {
		return false, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	_, slot, err := store.writeSlotLocked(owner, ref, false)
	if err != nil {
		return false, err
	}
	if slot.Kind != HeapWeakMap {
		return false, typeError(ref, slot.Kind, HeapWeakMap)
	}
	if err := store.validateWeakKeyAccessLocked(owner, key); err != nil {
		return false, err
	}
	for index, entry := range slot.WeakMap.Entries {
		if entry.Key != key {
			continue
		}
		copy(slot.WeakMap.Entries[index:], slot.WeakMap.Entries[index+1:])
		slot.WeakMap.Entries[len(slot.WeakMap.Entries)-1] = WeakMapEntry{}
		slot.WeakMap.Entries = slot.WeakMap.Entries[:len(slot.WeakMap.Entries)-1]
		return true, nil
	}
	return false, nil
}

func (store *Store) WeakMapClear(owner ownership.OwnerID, ref Ref) error {
	if store == nil {
		return fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	_, slot, err := store.writeSlotLocked(owner, ref, false)
	if err != nil {
		return err
	}
	if slot.Kind != HeapWeakMap {
		return typeError(ref, slot.Kind, HeapWeakMap)
	}
	slot.WeakMap.Entries = nil
	return nil
}

func (store *Store) AllocWeakSet(owner ownership.OwnerID, regionID RegionID) (Ref, error) {
	if store == nil {
		return Ref{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return store.allocKindLocked(owner, regionID, HeapWeakSet, false)
}

func (store *Store) DerefWeakSet(owner ownership.OwnerID, ref Ref) (WeakSet, error) {
	if store == nil {
		return WeakSet{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	_, slot, err := store.readSlotLocked(owner, ref)
	if err != nil {
		return WeakSet{}, err
	}
	if slot.Kind != HeapWeakSet {
		return WeakSet{}, typeError(ref, slot.Kind, HeapWeakSet)
	}
	return cloneWeakSet(slot.WeakSet), nil
}

func (store *Store) WeakSetAdd(owner ownership.OwnerID, ref, key Ref) error {
	if store == nil {
		return fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return store.weakSetAddLocked(owner, ref, key, false)
}

func (store *Store) weakSetAddLocked(owner ownership.OwnerID, ref, key Ref, internal bool) error {
	region, slot, err := store.writeSlotLocked(owner, ref, internal)
	if err != nil {
		return err
	}
	if slot.Kind != HeapWeakSet {
		return typeError(ref, slot.Kind, HeapWeakSet)
	}
	if err := store.validateWeakKeyLocked(region, key, internal); err != nil {
		return err
	}
	for _, existing := range slot.WeakSet.Keys {
		if existing == key {
			return nil
		}
	}
	slot.WeakSet.Keys = append(slot.WeakSet.Keys, key)
	return nil
}

func (store *Store) WeakSetHas(owner ownership.OwnerID, ref, key Ref) (bool, error) {
	if store == nil {
		return false, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	_, slot, err := store.readSlotLocked(owner, ref)
	if err != nil {
		return false, err
	}
	if slot.Kind != HeapWeakSet {
		return false, typeError(ref, slot.Kind, HeapWeakSet)
	}
	if err := store.validateWeakKeyAccessLocked(owner, key); err != nil {
		return false, err
	}
	for _, existing := range slot.WeakSet.Keys {
		if existing == key {
			return true, nil
		}
	}
	return false, nil
}

func (store *Store) WeakSetDelete(owner ownership.OwnerID, ref, key Ref) (bool, error) {
	if store == nil {
		return false, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	_, slot, err := store.writeSlotLocked(owner, ref, false)
	if err != nil {
		return false, err
	}
	if slot.Kind != HeapWeakSet {
		return false, typeError(ref, slot.Kind, HeapWeakSet)
	}
	if err := store.validateWeakKeyAccessLocked(owner, key); err != nil {
		return false, err
	}
	for index, existing := range slot.WeakSet.Keys {
		if existing != key {
			continue
		}
		copy(slot.WeakSet.Keys[index:], slot.WeakSet.Keys[index+1:])
		slot.WeakSet.Keys[len(slot.WeakSet.Keys)-1] = Ref{}
		slot.WeakSet.Keys = slot.WeakSet.Keys[:len(slot.WeakSet.Keys)-1]
		return true, nil
	}
	return false, nil
}

func (store *Store) WeakSetClear(owner ownership.OwnerID, ref Ref) error {
	if store == nil {
		return fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	_, slot, err := store.writeSlotLocked(owner, ref, false)
	if err != nil {
		return err
	}
	if slot.Kind != HeapWeakSet {
		return typeError(ref, slot.Kind, HeapWeakSet)
	}
	slot.WeakSet.Keys = nil
	return nil
}

func (store *Store) validateWeakKeyLocked(table *Region, key Ref, internal bool) error {
	if key == (Ref{}) {
		return ErrStaleRef
	}
	keyRegion, _, err := store.slotLocked(key)
	if err != nil {
		return err
	}
	if keyRegion.State == RegionInTransit {
		return fmt.Errorf("%w: R%d", ErrRegionInTransit, keyRegion.ID)
	}
	if !internal && keyRegion.State == RegionPrivate && table.Owner.Kind > keyRegion.Owner.Kind {
		return fmt.Errorf("%w: %s key cannot enter %s table", ErrWeakKeyLifetime, keyRegion.Owner, table.Owner)
	}
	return nil
}

func (store *Store) validateWeakKeyAccessLocked(owner ownership.OwnerID, key Ref) error {
	if key == (Ref{}) {
		return ErrStaleRef
	}
	_, _, err := store.readSlotLocked(owner, key)
	return err
}
