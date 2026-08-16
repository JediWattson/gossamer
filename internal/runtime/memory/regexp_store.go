package memory

import (
	"fmt"

	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

func (store *Store) AllocRegExp(owner ownership.OwnerID, regionID RegionID, pattern Ref, flagsText string) (Ref, error) {
	if store == nil {
		return Ref{}, fmt.Errorf("memory: nil store")
	}
	flags, err := ParseRegExpFlags(flagsText)
	if err != nil {
		return Ref{}, err
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	ref, err := store.allocKindLocked(owner, regionID, HeapRegExp, false)
	if err != nil {
		return Ref{}, err
	}
	if err := store.initializeRegExpLocked(owner, ref, RegExp{Pattern: pattern, Flags: flags}, false); err != nil {
		_ = store.freeLocked(owner, ref, true)
		return Ref{}, err
	}
	return ref, nil
}

func (store *Store) DerefRegExp(owner ownership.OwnerID, ref Ref) (RegExp, error) {
	if store == nil {
		return RegExp{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	_, slot, err := store.readSlotLocked(owner, ref)
	if err != nil {
		return RegExp{}, err
	}
	if slot.Kind != HeapRegExp {
		return RegExp{}, typeError(ref, slot.Kind, HeapRegExp)
	}
	return cloneRegExp(slot.RegExp), nil
}

func (store *Store) SetRegExpLastIndex(owner ownership.OwnerID, ref Ref, index uint64) error {
	if store == nil {
		return fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return store.setRegExpLastIndexLocked(owner, ref, index, false)
}

func (store *Store) setRegExpLastIndexLocked(owner ownership.OwnerID, ref Ref, index uint64, internal bool) error {
	_, slot, err := store.writeSlotLocked(owner, ref, internal)
	if err != nil {
		return err
	}
	if slot.Kind != HeapRegExp {
		return typeError(ref, slot.Kind, HeapRegExp)
	}
	slot.RegExp.LastIndex = index
	return nil
}

func (store *Store) initializeRegExpLocked(owner ownership.OwnerID, ref Ref, expression RegExp, internal bool) error {
	region, slot, err := store.writeSlotLocked(owner, ref, internal)
	if err != nil {
		return err
	}
	if slot.Kind != HeapRegExp {
		return typeError(ref, slot.Kind, HeapRegExp)
	}
	if slot.RegExp != (RegExp{}) {
		return fmt.Errorf("%w: descriptor already initialized", ErrInvalidRegExp)
	}
	if expression.Flags&^allRegExpFlags != 0 || expression.Flags&RegExpUnicode != 0 && expression.Flags&RegExpUnicodeSets != 0 {
		return fmt.Errorf("%w: flag mask %x", ErrInvalidRegExp, expression.Flags)
	}
	_, patternSlot, err := store.slotLocked(expression.Pattern)
	if err != nil {
		return err
	}
	if patternSlot.Kind != HeapString {
		return typeError(expression.Pattern, patternSlot.Kind, HeapString)
	}
	pattern, err := store.replaceValueLocked(owner, region, slot, Value{}, RefValue(expression.Pattern), internal)
	if err != nil {
		return err
	}
	expression.Pattern = pattern.Ref()
	slot.RegExp = cloneRegExp(expression)
	return nil
}
