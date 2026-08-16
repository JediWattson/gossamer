package memory

import (
	"fmt"

	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

// AllocObject creates an empty native object with a null prototype.
func (store *Store) AllocObject(owner ownership.OwnerID, regionID RegionID) (Ref, error) {
	if store == nil {
		return Ref{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	ref, err := store.allocKindLocked(owner, regionID, HeapObject, false)
	if err != nil {
		return Ref{}, err
	}
	_, slot, _ := store.slotLocked(ref)
	slot.Object.Prototype = NullValue()
	return ref, nil
}

func (store *Store) DerefObject(owner ownership.OwnerID, ref Ref) (Object, error) {
	if store == nil {
		return Object{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	_, slot, err := store.readSlotLocked(owner, ref)
	if err != nil {
		return Object{}, err
	}
	if slot.Kind != HeapObject {
		return Object{}, typeError(ref, slot.Kind, HeapObject)
	}
	return cloneObject(slot.Object), nil
}

// SetPrototype replaces object's direct prototype. The value must be null or
// reference another native Object.
func (store *Store) SetPrototype(owner ownership.OwnerID, object Ref, prototype Value) error {
	if store == nil {
		return fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return store.setPrototypeLocked(owner, object, prototype, false)
}

func (store *Store) setPrototypeLocked(owner ownership.OwnerID, object Ref, prototype Value, internal bool) error {
	region, slot, err := store.writeSlotLocked(owner, object, internal)
	if err != nil {
		return err
	}
	if slot.Kind != HeapObject {
		return typeError(object, slot.Kind, HeapObject)
	}
	if prototype.Kind() != ValueNull {
		if !prototype.IsRef() {
			return fmt.Errorf("%w: Object prototype must be null or an Object Ref", ErrTypeMismatch)
		}
		_, prototypeSlot, lookupErr := store.slotLocked(prototype.Ref())
		if lookupErr != nil {
			return lookupErr
		}
		if prototypeSlot.Kind != HeapObject {
			return typeError(prototype.Ref(), prototypeSlot.Kind, HeapObject)
		}
	}
	prototype, err = store.replaceValueLocked(owner, region, slot, slot.Object.Prototype, prototype, internal)
	if err != nil {
		return err
	}
	slot.Object.Prototype = prototype
	return nil
}

// SetProperty inserts or replaces an own property. Name must reference a
// native String. Equal String contents address the same property even when the
// caller supplies a different String Ref.
func (store *Store) SetProperty(owner ownership.OwnerID, object, name Ref, value Value) error {
	if store == nil {
		return fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return store.setPropertyLocked(owner, object, name, value, false)
}

func (store *Store) setPropertyLocked(owner ownership.OwnerID, object, name Ref, value Value, internal bool) error {
	region, slot, err := store.writeSlotLocked(owner, object, internal)
	if err != nil {
		return err
	}
	if slot.Kind != HeapObject {
		return typeError(object, slot.Kind, HeapObject)
	}
	nameText, err := store.stringTextLocked(owner, name, internal)
	if err != nil {
		return err
	}
	index, err := store.findPropertyLocked(slot, nameText)
	if err != nil {
		return err
	}
	if index >= 0 {
		old := slot.Object.Properties[index].Value
		value, err = store.replaceValueLocked(owner, region, slot, old, value, internal)
		if err != nil {
			return err
		}
		slot.Object.Properties[index].Value = value
		return nil
	}
	preparedName, err := store.replaceValueLocked(owner, region, slot, Value{}, RefValue(name), internal)
	if err != nil {
		return err
	}
	value, err = store.replaceValueLocked(owner, region, slot, Value{}, value, internal)
	if err != nil {
		_, _ = store.replaceValueLocked(owner, region, slot, preparedName, Value{}, true)
		return err
	}
	slot.Object.Properties = append(slot.Object.Properties, Property{Name: preparedName.Ref(), Value: value})
	return nil
}

func (store *Store) GetOwnProperty(owner ownership.OwnerID, object, name Ref) (Value, bool, error) {
	if store == nil {
		return Value{}, false, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	_, slot, err := store.readSlotLocked(owner, object)
	if err != nil {
		return Value{}, false, err
	}
	if slot.Kind != HeapObject {
		return Value{}, false, typeError(object, slot.Kind, HeapObject)
	}
	nameText, err := store.stringTextLocked(owner, name, false)
	if err != nil {
		return Value{}, false, err
	}
	index, err := store.findPropertyLocked(slot, nameText)
	if err != nil {
		return Value{}, false, err
	}
	if index < 0 {
		return Value{}, false, nil
	}
	return slot.Object.Properties[index].Value, true, nil
}

func (store *Store) DeleteProperty(owner ownership.OwnerID, object, name Ref) (bool, error) {
	if store == nil {
		return false, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	region, slot, err := store.writeSlotLocked(owner, object, false)
	if err != nil {
		return false, err
	}
	if slot.Kind != HeapObject {
		return false, typeError(object, slot.Kind, HeapObject)
	}
	nameText, err := store.stringTextLocked(owner, name, false)
	if err != nil {
		return false, err
	}
	index, err := store.findPropertyLocked(slot, nameText)
	if err != nil || index < 0 {
		return false, err
	}
	property := slot.Object.Properties[index]
	if _, err := store.replaceValueLocked(owner, region, slot, RefValue(property.Name), Value{}, false); err != nil {
		return false, err
	}
	if _, err := store.replaceValueLocked(owner, region, slot, property.Value, Value{}, false); err != nil {
		_, _ = store.replaceValueLocked(owner, region, slot, Value{}, RefValue(property.Name), true)
		return false, err
	}
	copy(slot.Object.Properties[index:], slot.Object.Properties[index+1:])
	slot.Object.Properties[len(slot.Object.Properties)-1] = Property{}
	slot.Object.Properties = slot.Object.Properties[:len(slot.Object.Properties)-1]
	return true, nil
}

func (store *Store) stringTextLocked(owner ownership.OwnerID, ref Ref, internal bool) (string, error) {
	_, slot, err := store.slotLocked(ref)
	if err != nil {
		return "", err
	}
	if slot.Kind != HeapString {
		return "", typeError(ref, slot.Kind, HeapString)
	}
	return slot.String.Text, nil
}

func (store *Store) findPropertyLocked(slot *Slot, name string) (int, error) {
	for index, property := range slot.Object.Properties {
		_, nameSlot, err := store.slotLocked(property.Name)
		if err != nil {
			return -1, err
		}
		if nameSlot.Kind != HeapString {
			return -1, typeError(property.Name, nameSlot.Kind, HeapString)
		}
		if nameSlot.String.Text == name {
			return index, nil
		}
	}
	return -1, nil
}
