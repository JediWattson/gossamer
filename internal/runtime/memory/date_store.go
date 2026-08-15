package memory

import (
	"fmt"

	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

func (store *Store) AllocDate(owner ownership.OwnerID, regionID RegionID, milliseconds float64) (Ref, error) {
	if store == nil {
		return Ref{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	ref, err := store.allocKindLocked(owner, regionID, HeapDate, false)
	if err != nil {
		return Ref{}, err
	}
	_, slot, _ := store.slotLocked(ref)
	slot.Date.Milliseconds = timeClip(milliseconds)
	return ref, nil
}

func (store *Store) DerefDate(owner ownership.OwnerID, ref Ref) (Date, error) {
	if store == nil {
		return Date{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	_, slot, err := store.readSlotLocked(owner, ref)
	if err != nil {
		return Date{}, err
	}
	if slot.Kind != HeapDate {
		return Date{}, typeError(ref, slot.Kind, HeapDate)
	}
	return slot.Date, nil
}

func (store *Store) SetDateTime(owner ownership.OwnerID, ref Ref, milliseconds float64) error {
	if store == nil {
		return fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return store.setDateTimeLocked(owner, ref, milliseconds, false)
}

func (store *Store) setDateTimeLocked(owner ownership.OwnerID, ref Ref, milliseconds float64, internal bool) error {
	_, slot, err := store.writeSlotLocked(owner, ref, internal)
	if err != nil {
		return err
	}
	if slot.Kind != HeapDate {
		return typeError(ref, slot.Kind, HeapDate)
	}
	slot.Date.Milliseconds = timeClip(milliseconds)
	return nil
}
