package memory

import (
	"errors"
	"fmt"
	"math"

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
	return cloneWeakMap(*slot.WeakMap), nil
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
			old := slot.WeakMap.Entries[index].Value
			if old == value {
				return nil
			}
			if value.IsRef() {
				if err := store.ensureWeakUseCapacityLocked(value.Ref(), 1); err != nil {
					return err
				}
			}
			if old.IsRef() {
				entry := &slot.WeakMap.Entries[index]
				if err := store.removeWeakUseLocked(old.Ref(), entry.valueUse, weakUse{table: ref, entry: uint32(index), role: weakMapValueUse}); err != nil {
					return err
				}
			}
			var valueUse uint32
			if value.IsRef() {
				valueUse, err = store.addWeakUseLocked(value.Ref(), weakUse{table: ref, entry: uint32(index), role: weakMapValueUse})
				if err != nil {
					return err
				}
			}
			slot.WeakMap.Entries[index].Value = value
			slot.WeakMap.Entries[index].valueUse = valueUse
			return nil
		}
	}
	if uint64(len(slot.WeakMap.Entries)) > math.MaxUint32 {
		return fmt.Errorf("memory: WeakMap %s exhausted entries", ref)
	}
	keyUses := uint64(1)
	if value.IsRef() && value.Ref() == key {
		keyUses++
	}
	if err := store.ensureWeakUseCapacityLocked(key, keyUses); err != nil {
		return err
	}
	if value.IsRef() && value.Ref() != key {
		if err := store.ensureWeakUseCapacityLocked(value.Ref(), 1); err != nil {
			return err
		}
	}
	entryIndex := uint32(len(slot.WeakMap.Entries))
	slot.WeakMap.Entries = append(slot.WeakMap.Entries, WeakMapEntry{Key: key, Value: value})
	entry := &slot.WeakMap.Entries[entryIndex]
	entry.keyUse, err = store.addWeakUseLocked(key, weakUse{table: ref, entry: entryIndex, role: weakMapKeyUse})
	if err != nil {
		slot.WeakMap.Entries = slot.WeakMap.Entries[:entryIndex]
		return err
	}
	if value.IsRef() {
		entry.valueUse, err = store.addWeakUseLocked(value.Ref(), weakUse{table: ref, entry: entryIndex, role: weakMapValueUse})
		if err != nil {
			_ = store.removeWeakUseLocked(key, entry.keyUse, weakUse{table: ref, entry: entryIndex, role: weakMapKeyUse})
			slot.WeakMap.Entries = slot.WeakMap.Entries[:entryIndex]
			return err
		}
	}
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
		if err := store.removeWeakMapEntryLocked(ref, slot, uint32(index)); err != nil {
			return false, err
		}
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
	for len(slot.WeakMap.Entries) != 0 {
		if err := store.removeWeakMapEntryLocked(ref, slot, uint32(len(slot.WeakMap.Entries)-1)); err != nil {
			return err
		}
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
	return cloneWeakSet(*slot.WeakSet), nil
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
	if uint64(len(slot.WeakSet.Keys)) > math.MaxUint32 {
		return fmt.Errorf("memory: WeakSet %s exhausted entries", ref)
	}
	if err := store.ensureWeakUseCapacityLocked(key, 1); err != nil {
		return err
	}
	entryIndex := uint32(len(slot.WeakSet.Keys))
	slot.WeakSet.Keys = append(slot.WeakSet.Keys, key)
	use, err := store.addWeakUseLocked(key, weakUse{table: ref, entry: entryIndex, role: weakSetKeyUse})
	if err != nil {
		slot.WeakSet.Keys = slot.WeakSet.Keys[:entryIndex]
		return err
	}
	slot.WeakSet.uses = append(slot.WeakSet.uses, use)
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
		if err := store.removeWeakSetEntryLocked(ref, slot, uint32(index)); err != nil {
			return false, err
		}
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
	for len(slot.WeakSet.Keys) != 0 {
		if err := store.removeWeakSetEntryLocked(ref, slot, uint32(len(slot.WeakSet.Keys)-1)); err != nil {
			return err
		}
	}
	slot.WeakSet.Keys = nil
	slot.WeakSet.uses = nil
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
	if keyRegion.State == RegionInTransit && (!internal || table.State != RegionInTransit || table.Owner != keyRegion.Owner) {
		return fmt.Errorf("%w: R%d", ErrRegionInTransit, keyRegion.ID)
	}
	if !internal && keyRegion.State == RegionPrivate && table.Owner.Kind > keyRegion.Owner.Kind {
		return fmt.Errorf("%w: %s key cannot enter %s table", ErrWeakKeyLifetime, keyRegion.Owner, table.Owner)
	}
	if !internal && keyRegion.State == RegionPrivate && keyRegion.Owner != table.Owner {
		return store.accessError(keyRegion, table.Owner)
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

func (store *Store) ensureWeakUseCapacityLocked(target Ref, additional uint64) error {
	if uint64(len(store.weakTargets[target]))+additional > math.MaxUint32 {
		return fmt.Errorf("memory: weak target %s reference count overflow", target)
	}
	return nil
}

func (store *Store) addWeakUseLocked(target Ref, use weakUse) (uint32, error) {
	if err := store.ensureWeakUseCapacityLocked(target, 1); err != nil {
		return 0, err
	}
	uses := store.weakTargets[target]
	index := uint32(len(uses))
	store.weakTargets[target] = append(uses, use)
	return index, nil
}

func (store *Store) setWeakUseBackLocked(target Ref, use weakUse, back uint32) error {
	_, slot, err := store.slotLocked(use.table)
	if err != nil {
		return fmt.Errorf("%w: weak use for %s has stale table %s: %v", ErrInvariantViolation, target, use.table, err)
	}
	switch use.role {
	case weakMapKeyUse:
		if slot.Kind != HeapWeakMap || uint64(use.entry) >= uint64(len(slot.WeakMap.Entries)) || slot.WeakMap.Entries[use.entry].Key != target {
			return fmt.Errorf("%w: weak key use for %s does not resolve to %s entry %d", ErrInvariantViolation, target, use.table, use.entry)
		}
		slot.WeakMap.Entries[use.entry].keyUse = back
	case weakMapValueUse:
		if slot.Kind != HeapWeakMap || uint64(use.entry) >= uint64(len(slot.WeakMap.Entries)) {
			return fmt.Errorf("%w: weak value use for %s does not resolve to %s entry %d", ErrInvariantViolation, target, use.table, use.entry)
		}
		entry := &slot.WeakMap.Entries[use.entry]
		if !entry.Value.IsRef() || entry.Value.Ref() != target {
			return fmt.Errorf("%w: weak value use for %s does not match %s entry %d", ErrInvariantViolation, target, use.table, use.entry)
		}
		entry.valueUse = back
	case weakSetKeyUse:
		if slot.Kind != HeapWeakSet || uint64(use.entry) >= uint64(len(slot.WeakSet.Keys)) || len(slot.WeakSet.uses) != len(slot.WeakSet.Keys) || slot.WeakSet.Keys[use.entry] != target {
			return fmt.Errorf("%w: weak set use for %s does not resolve to %s entry %d", ErrInvariantViolation, target, use.table, use.entry)
		}
		slot.WeakSet.uses[use.entry] = back
	default:
		return fmt.Errorf("%w: weak use for %s has invalid role %d", ErrInvariantViolation, target, use.role)
	}
	return nil
}

func (store *Store) removeWeakUseLocked(target Ref, back uint32, expected weakUse) error {
	uses := store.weakTargets[target]
	if uint64(back) >= uint64(len(uses)) || uses[back] != expected {
		return fmt.Errorf("%w: weak target %s use %d does not match %#v", ErrInvariantViolation, target, back, expected)
	}
	last := len(uses) - 1
	if int(back) != last {
		moved := uses[last]
		uses[back] = moved
		if err := store.setWeakUseBackLocked(target, moved, back); err != nil {
			return err
		}
	}
	uses[last] = weakUse{}
	if last == 0 {
		delete(store.weakTargets, target)
	} else {
		store.weakTargets[target] = uses[:last]
	}
	return nil
}

func (store *Store) removeWeakMapEntryLocked(table Ref, slot *Slot, index uint32) error {
	if slot == nil || slot.Kind != HeapWeakMap || uint64(index) >= uint64(len(slot.WeakMap.Entries)) {
		return fmt.Errorf("%w: invalid WeakMap %s entry %d", ErrInvariantViolation, table, index)
	}
	entry := &slot.WeakMap.Entries[index]
	if err := store.removeWeakUseLocked(entry.Key, entry.keyUse, weakUse{table: table, entry: index, role: weakMapKeyUse}); err != nil {
		return err
	}
	// Read valueUse after removing the key use. When key and value are the same
	// Ref, the reverse-index swap may have rewritten this back pointer.
	if entry.Value.IsRef() {
		if err := store.removeWeakUseLocked(entry.Value.Ref(), entry.valueUse, weakUse{table: table, entry: index, role: weakMapValueUse}); err != nil {
			return err
		}
	}
	last := len(slot.WeakMap.Entries) - 1
	if int(index) != last {
		moved := slot.WeakMap.Entries[last]
		slot.WeakMap.Entries[index] = moved
		keyUses := store.weakTargets[moved.Key]
		if uint64(moved.keyUse) >= uint64(len(keyUses)) {
			return fmt.Errorf("%w: moved WeakMap key %s has invalid use %d", ErrInvariantViolation, moved.Key, moved.keyUse)
		}
		keyUses[moved.keyUse].entry = index
		store.weakTargets[moved.Key] = keyUses
		if moved.Value.IsRef() {
			valueUses := store.weakTargets[moved.Value.Ref()]
			if uint64(moved.valueUse) >= uint64(len(valueUses)) {
				return fmt.Errorf("%w: moved WeakMap value %s has invalid use %d", ErrInvariantViolation, moved.Value.Ref(), moved.valueUse)
			}
			valueUses[moved.valueUse].entry = index
			store.weakTargets[moved.Value.Ref()] = valueUses
		}
	}
	slot.WeakMap.Entries[last] = WeakMapEntry{}
	slot.WeakMap.Entries = slot.WeakMap.Entries[:last]
	return nil
}

func (store *Store) removeWeakSetEntryLocked(table Ref, slot *Slot, index uint32) error {
	if slot == nil || slot.Kind != HeapWeakSet || uint64(index) >= uint64(len(slot.WeakSet.Keys)) || len(slot.WeakSet.uses) != len(slot.WeakSet.Keys) {
		return fmt.Errorf("%w: invalid WeakSet %s entry %d", ErrInvariantViolation, table, index)
	}
	target := slot.WeakSet.Keys[index]
	if err := store.removeWeakUseLocked(target, slot.WeakSet.uses[index], weakUse{table: table, entry: index, role: weakSetKeyUse}); err != nil {
		return err
	}
	last := len(slot.WeakSet.Keys) - 1
	if int(index) != last {
		// Read the moved back pointer after removal because a reverse-index swap
		// may have updated it.
		movedTarget := slot.WeakSet.Keys[last]
		movedBack := slot.WeakSet.uses[last]
		slot.WeakSet.Keys[index] = movedTarget
		slot.WeakSet.uses[index] = movedBack
		uses := store.weakTargets[movedTarget]
		if uint64(movedBack) >= uint64(len(uses)) {
			return fmt.Errorf("%w: moved WeakSet key %s has invalid use %d", ErrInvariantViolation, movedTarget, movedBack)
		}
		uses[movedBack].entry = index
		store.weakTargets[movedTarget] = uses
	}
	slot.WeakSet.Keys[last] = Ref{}
	slot.WeakSet.uses[last] = 0
	slot.WeakSet.Keys = slot.WeakSet.Keys[:last]
	slot.WeakSet.uses = slot.WeakSet.uses[:last]
	return nil
}

func (store *Store) forgetWeakTableLocked(table Ref, slot *Slot) error {
	if slot == nil {
		return nil
	}
	switch slot.Kind {
	case HeapWeakMap:
		for len(slot.WeakMap.Entries) != 0 {
			if err := store.removeWeakMapEntryLocked(table, slot, uint32(len(slot.WeakMap.Entries)-1)); err != nil {
				return err
			}
		}
	case HeapWeakSet:
		for len(slot.WeakSet.Keys) != 0 {
			if err := store.removeWeakSetEntryLocked(table, slot, uint32(len(slot.WeakSet.Keys)-1)); err != nil {
				return err
			}
		}
	default:
		return nil
	}
	delete(store.weakTables, table)
	return nil
}

// dropWeakTargetUsesLocked removes selected entries that reference target. Its
// work is proportional to the target's exact reverse uses, independent of
// unrelated weak tables and entries. count reports whether an entry belongs to
// a surviving table and should contribute to WeakEntriesCleared.
func (store *Store) dropWeakTargetUsesLocked(target Ref, drop func(table, target Ref) bool, count func(table Ref) bool) (uint64, error) {
	var cleared uint64
	for index := 0; index < len(store.weakTargets[target]); {
		uses := store.weakTargets[target]
		use := uses[index]
		if drop != nil && !drop(use.table, target) {
			index++
			continue
		}
		_, slot, err := store.slotLocked(use.table)
		if err != nil {
			return cleared, fmt.Errorf("%w: weak use for %s has stale table %s: %v", ErrInvariantViolation, target, use.table, err)
		}
		switch use.role {
		case weakMapKeyUse, weakMapValueUse:
			if err := store.removeWeakMapEntryLocked(use.table, slot, use.entry); err != nil {
				return cleared, err
			}
		case weakSetKeyUse:
			if err := store.removeWeakSetEntryLocked(use.table, slot, use.entry); err != nil {
				return cleared, err
			}
		default:
			return cleared, fmt.Errorf("%w: weak use for %s has invalid role %d", ErrInvariantViolation, target, use.role)
		}
		if count == nil || count(use.table) {
			cleared++
		}
	}
	return cleared, nil
}

func (store *Store) validateWeakUseLocked(target Ref, back uint32, expected weakUse) error {
	uses := store.weakTargets[target]
	if uint64(back) >= uint64(len(uses)) || uses[back] != expected {
		return fmt.Errorf("%w: weak target %s use %d does not match %#v", ErrInvariantViolation, target, back, expected)
	}
	return nil
}
