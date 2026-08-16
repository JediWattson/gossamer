package memory

import (
	"errors"
	"fmt"

	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

var ErrInvalidHostObject = errors.New("memory: invalid host object")

func (store *Store) AllocHostObject(owner ownership.OwnerID, regionID RegionID, value HostObject) (Ref, error) {
	if store == nil {
		return Ref{}, fmt.Errorf("memory: nil store")
	}
	if value.Class == 0 || value.Scope == 0 || value.Identity == 0 {
		return Ref{}, ErrInvalidHostObject
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	ref, err := store.allocKindLocked(owner, regionID, HeapHostObject, false)
	if err != nil {
		return Ref{}, err
	}
	_, slot, _ := store.slotLocked(ref)
	slot.HostObject = value
	return ref, nil
}

func (store *Store) DerefHostObject(owner ownership.OwnerID, ref Ref) (HostObject, error) {
	if store == nil {
		return HostObject{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	_, slot, err := store.readSlotLocked(owner, ref)
	if err != nil {
		return HostObject{}, err
	}
	if slot.Kind != HeapHostObject {
		return HostObject{}, typeError(ref, slot.Kind, HeapHostObject)
	}
	return slot.HostObject, nil
}

// ObjectID returns the semantic ledger identity behind one physical Ref. It is
// the narrow bridge used when a non-Store ownership root must retain a typed
// native object without exposing Slot storage.
func (store *Store) ObjectID(owner ownership.OwnerID, ref Ref) (ownership.ObjectID, error) {
	if store == nil {
		return 0, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	_, slot, err := store.readSlotLocked(owner, ref)
	if err != nil {
		return 0, err
	}
	return slot.object, nil
}
