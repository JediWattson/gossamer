package memory

import (
	"fmt"
	"unicode/utf8"

	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

func (store *Store) AllocIterator(owner ownership.OwnerID, regionID RegionID, target Ref, kind IteratorKind) (Ref, error) {
	if store == nil {
		return Ref{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	_, targetSlot, err := store.readSlotLocked(owner, target)
	if err != nil {
		return Ref{}, err
	}
	if !iteratorTargetMatches(kind, targetSlot.Kind) {
		return Ref{}, fmt.Errorf("%w: %s cannot use iterator kind %d", ErrTypeMismatch, target, kind)
	}
	ref, err := store.allocKindLocked(owner, regionID, HeapIterator, false)
	if err != nil {
		return Ref{}, err
	}
	region, slot, _ := store.slotLocked(ref)
	linked, err := store.replaceValueLocked(owner, region, slot, Value{}, RefValue(target), false)
	if err != nil {
		_ = store.freeLocked(owner, ref, true)
		return Ref{}, err
	}
	slot.Iterator.Target = linked.Ref()
	slot.Iterator.Kind = kind
	return ref, nil
}

func (store *Store) DerefIterator(owner ownership.OwnerID, ref Ref) (Iterator, error) {
	if store == nil {
		return Iterator{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	_, slot, err := store.readSlotLocked(owner, ref)
	if err != nil {
		return Iterator{}, err
	}
	if slot.Kind != HeapIterator {
		return Iterator{}, typeError(ref, slot.Kind, HeapIterator)
	}
	return cloneIterator(*slot.Iterator), nil
}

func (store *Store) AdvanceIterator(owner ownership.OwnerID, ref Ref) (IteratorStep, error) {
	if store == nil {
		return IteratorStep{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	_, slot, err := store.writeSlotLocked(owner, ref, false)
	if err != nil {
		return IteratorStep{}, err
	}
	if slot.Kind != HeapIterator {
		return IteratorStep{}, typeError(ref, slot.Kind, HeapIterator)
	}
	_, target, err := store.readSlotLocked(owner, slot.Iterator.Target)
	if err != nil {
		return IteratorStep{}, err
	}
	step := IteratorStep{}
	position := slot.Iterator.Next
	switch slot.Iterator.Kind {
	case IteratorArrayKeys, IteratorArrayValues, IteratorArrayEntries:
		if position >= target.Array.Length {
			step.Done = true
			break
		}
		value := UndefinedValue()
		if index, present := arrayElementPosition(target.Array.Elements, position); present {
			value = target.Array.Elements[index].Value
		}
		if slot.Iterator.Kind == IteratorArrayKeys {
			step.Value = NumberValue(float64(position))
		} else if slot.Iterator.Kind == IteratorArrayEntries {
			step.Pair = true
			step.Key = NumberValue(float64(position))
			step.Value = value
		} else {
			step.Value = value
		}
		slot.Iterator.Next++
	case IteratorStringValues:
		text := target.String.Text
		if uint64(position) >= uint64(len(text)) {
			step.Done = true
			break
		}
		_, size := utf8.DecodeRuneInString(text[position:])
		if size == 0 {
			step.Done = true
			break
		}
		step.Textual = true
		step.Text = text[position : position+uint32(size)]
		slot.Iterator.Next += uint32(size)
	case IteratorMapKeys, IteratorMapValues, IteratorMapEntries:
		if uint64(position) >= uint64(len(target.Map.Entries)) {
			step.Done = true
			break
		}
		entry := target.Map.Entries[position]
		if slot.Iterator.Kind == IteratorMapKeys {
			step.Value = entry.Key
		} else if slot.Iterator.Kind == IteratorMapEntries {
			step.Pair = true
			step.Key = entry.Key
			step.Value = entry.Value
		} else {
			step.Value = entry.Value
		}
		slot.Iterator.Next++
	case IteratorSetValues, IteratorSetEntries:
		if uint64(position) >= uint64(len(target.Set.Values)) {
			step.Done = true
			break
		}
		value := target.Set.Values[position]
		if slot.Iterator.Kind == IteratorSetEntries {
			step.Pair = true
			step.Key = value
		}
		step.Value = value
		slot.Iterator.Next++
	default:
		return IteratorStep{}, fmt.Errorf("%w: invalid iterator kind %d", ErrTypeMismatch, slot.Iterator.Kind)
	}
	return step, nil
}

func iteratorTargetMatches(iterator IteratorKind, target HeapKind) bool {
	switch iterator {
	case IteratorArrayKeys, IteratorArrayValues, IteratorArrayEntries:
		return target == HeapArray
	case IteratorStringValues:
		return target == HeapString
	case IteratorMapKeys, IteratorMapValues, IteratorMapEntries:
		return target == HeapMap
	case IteratorSetValues, IteratorSetEntries:
		return target == HeapSet
	default:
		return false
	}
}
