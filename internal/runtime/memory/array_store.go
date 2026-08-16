package memory

import (
	"fmt"
	"math"
	"sort"

	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

func (store *Store) AllocArray(owner ownership.OwnerID, regionID RegionID, length uint32) (Ref, error) {
	if store == nil {
		return Ref{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	ref, err := store.allocKindLocked(owner, regionID, HeapArray, false)
	if err != nil {
		return Ref{}, err
	}
	_, slot, _ := store.slotLocked(ref)
	slot.Array.Length = length
	return ref, nil
}

func (store *Store) DerefArray(owner ownership.OwnerID, ref Ref) (Array, error) {
	if store == nil {
		return Array{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	_, slot, err := store.readSlotLocked(owner, ref)
	if err != nil {
		return Array{}, err
	}
	if slot.Kind != HeapArray {
		return Array{}, typeError(ref, slot.Kind, HeapArray)
	}
	return cloneArray(slot.Array), nil
}

func (store *Store) ArrayElement(owner ownership.OwnerID, array Ref, index uint32) (Value, bool, error) {
	if store == nil {
		return Value{}, false, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	_, slot, err := store.readSlotLocked(owner, array)
	if err != nil {
		return Value{}, false, err
	}
	if slot.Kind != HeapArray {
		return Value{}, false, typeError(array, slot.Kind, HeapArray)
	}
	position, present := arrayElementPosition(slot.Array.Elements, index)
	if !present {
		return Value{}, false, nil
	}
	return slot.Array.Elements[position].Value, true, nil
}

func (store *Store) SetArrayElement(owner ownership.OwnerID, array Ref, index uint32, value Value) error {
	if store == nil {
		return fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return store.setArrayElementLocked(owner, array, index, value, false)
}

func (store *Store) setArrayElementLocked(owner ownership.OwnerID, array Ref, index uint32, value Value, internal bool) error {
	if index == math.MaxUint32 {
		return fmt.Errorf("%w: %d cannot advance uint32 length", ErrInvalidIndex, index)
	}
	region, slot, err := store.writeSlotLocked(owner, array, internal)
	if err != nil {
		return err
	}
	if slot.Kind != HeapArray {
		return typeError(array, slot.Kind, HeapArray)
	}
	position, present := arrayElementPosition(slot.Array.Elements, index)
	old := Value{}
	if present {
		old = slot.Array.Elements[position].Value
	}
	value, err = store.replaceValueLocked(owner, region, slot, old, value, internal)
	if err != nil {
		return err
	}
	if present {
		slot.Array.Elements[position].Value = value
	} else {
		slot.Array.Elements = append(slot.Array.Elements, ArrayElement{})
		copy(slot.Array.Elements[position+1:], slot.Array.Elements[position:])
		slot.Array.Elements[position] = ArrayElement{Index: index, Value: value}
	}
	if next := index + 1; next > slot.Array.Length {
		slot.Array.Length = next
	}
	return nil
}

func (store *Store) DeleteArrayElement(owner ownership.OwnerID, array Ref, index uint32) (bool, error) {
	if store == nil {
		return false, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return store.deleteArrayElementLocked(owner, array, index, false)
}

func (store *Store) deleteArrayElementLocked(owner ownership.OwnerID, array Ref, index uint32, internal bool) (bool, error) {
	region, slot, err := store.writeSlotLocked(owner, array, internal)
	if err != nil {
		return false, err
	}
	if slot.Kind != HeapArray {
		return false, typeError(array, slot.Kind, HeapArray)
	}
	position, present := arrayElementPosition(slot.Array.Elements, index)
	if !present {
		return false, nil
	}
	if _, err := store.replaceValueLocked(owner, region, slot, slot.Array.Elements[position].Value, Value{}, internal); err != nil {
		return false, err
	}
	copy(slot.Array.Elements[position:], slot.Array.Elements[position+1:])
	slot.Array.Elements[len(slot.Array.Elements)-1] = ArrayElement{}
	slot.Array.Elements = slot.Array.Elements[:len(slot.Array.Elements)-1]
	return true, nil
}

func (store *Store) SetArrayLength(owner ownership.OwnerID, array Ref, length uint32) error {
	if store == nil {
		return fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return store.setArrayLengthLocked(owner, array, length, false)
}

func (store *Store) setArrayLengthLocked(owner ownership.OwnerID, array Ref, length uint32, internal bool) error {
	region, slot, err := store.writeSlotLocked(owner, array, internal)
	if err != nil {
		return err
	}
	if slot.Kind != HeapArray {
		return typeError(array, slot.Kind, HeapArray)
	}
	if length >= slot.Array.Length {
		slot.Array.Length = length
		return nil
	}
	firstRemoved := sort.Search(len(slot.Array.Elements), func(index int) bool {
		return slot.Array.Elements[index].Index >= length
	})
	unlinked := make([]Value, 0, len(slot.Array.Elements)-firstRemoved)
	for _, element := range slot.Array.Elements[firstRemoved:] {
		if _, err := store.replaceValueLocked(owner, region, slot, element.Value, Value{}, internal); err != nil {
			for _, value := range unlinked {
				_, _ = store.replaceValueLocked(owner, region, slot, Value{}, value, true)
			}
			return err
		}
		unlinked = append(unlinked, element.Value)
	}
	for index := firstRemoved; index < len(slot.Array.Elements); index++ {
		slot.Array.Elements[index] = ArrayElement{}
	}
	slot.Array.Elements = slot.Array.Elements[:firstRemoved]
	slot.Array.Length = length
	return nil
}

func arrayElementPosition(elements []ArrayElement, index uint32) (int, bool) {
	position := sort.Search(len(elements), func(position int) bool {
		return elements[position].Index >= index
	})
	return position, position < len(elements) && elements[position].Index == index
}
