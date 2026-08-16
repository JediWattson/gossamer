package memory

import (
	"fmt"

	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

func (store *Store) AllocPromise(owner ownership.OwnerID, regionID RegionID) (Ref, error) {
	if store == nil {
		return Ref{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return store.allocKindLocked(owner, regionID, HeapPromise, false)
}

func (store *Store) DerefPromise(owner ownership.OwnerID, ref Ref) (Promise, error) {
	if store == nil {
		return Promise{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	_, slot, err := store.readSlotLocked(owner, ref)
	if err != nil {
		return Promise{}, err
	}
	if slot.Kind != HeapPromise {
		return Promise{}, typeError(ref, slot.Kind, HeapPromise)
	}
	return clonePromise(slot.Promise), nil
}

func (store *Store) AddPromiseReaction(owner ownership.OwnerID, promise Ref, reaction PromiseReaction) error {
	if store == nil {
		return fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return store.addPromiseReactionLocked(owner, promise, reaction, false)
}

func (store *Store) addPromiseReactionLocked(owner ownership.OwnerID, promise Ref, reaction PromiseReaction, internal bool) error {
	region, slot, err := store.writeSlotLocked(owner, promise, internal)
	if err != nil {
		return err
	}
	if slot.Kind != HeapPromise {
		return typeError(promise, slot.Kind, HeapPromise)
	}
	if err := store.validateOptionalTypedValueLocked(owner, reaction.OnFulfilled, HeapFunction, "Promise fulfillment handler", internal); err != nil {
		return err
	}
	if err := store.validateOptionalTypedValueLocked(owner, reaction.OnRejected, HeapFunction, "Promise rejection handler", internal); err != nil {
		return err
	}
	if err := store.validateOptionalTypedValueLocked(owner, reaction.Downstream, HeapPromise, "Promise downstream", internal); err != nil {
		return err
	}
	values := []Value{reaction.OnFulfilled, reaction.OnRejected, reaction.Downstream}
	linked := make([]Value, 0, len(values))
	for _, value := range values {
		value, err = store.replaceValueLocked(owner, region, slot, Value{}, value, internal)
		if err != nil {
			for index := len(linked) - 1; index >= 0; index-- {
				_, _ = store.replaceValueLocked(owner, region, slot, linked[index], Value{}, true)
			}
			return err
		}
		linked = append(linked, value)
	}
	reaction.OnFulfilled = linked[0]
	reaction.OnRejected = linked[1]
	reaction.Downstream = linked[2]
	slot.Promise.Reactions = append(slot.Promise.Reactions, reaction)
	return nil
}

func (store *Store) ResolvePromise(owner ownership.OwnerID, promise Ref, result Value) error {
	return store.settlePromise(owner, promise, PromiseFulfilled, result)
}

func (store *Store) RejectPromise(owner ownership.OwnerID, promise Ref, reason Value) error {
	return store.settlePromise(owner, promise, PromiseRejected, reason)
}

func (store *Store) settlePromise(owner ownership.OwnerID, promise Ref, state PromiseState, result Value) error {
	if store == nil {
		return fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return store.settlePromiseLocked(owner, promise, state, result, false)
}

func (store *Store) settlePromiseLocked(owner ownership.OwnerID, promise Ref, state PromiseState, result Value, internal bool) error {
	if state != PromiseFulfilled && state != PromiseRejected {
		return fmt.Errorf("%w: invalid settlement state %d", ErrPromisePending, state)
	}
	region, slot, err := store.writeSlotLocked(owner, promise, internal)
	if err != nil {
		return err
	}
	if slot.Kind != HeapPromise {
		return typeError(promise, slot.Kind, HeapPromise)
	}
	if slot.Promise.State != PromisePending {
		return ErrPromiseSettled
	}
	if state == PromiseFulfilled && result.IsRef() && result.Ref() == promise {
		return ErrPromiseSelfResolution
	}
	result, err = store.replaceValueLocked(owner, region, slot, Value{}, result, internal)
	if err != nil {
		return err
	}
	slot.Promise.State = state
	slot.Promise.Result = result
	return nil
}

func (store *Store) MarkPromiseHandled(owner ownership.OwnerID, promise Ref) error {
	if store == nil {
		return fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return store.markPromiseHandledLocked(owner, promise, false)
}

func (store *Store) markPromiseHandledLocked(owner ownership.OwnerID, promise Ref, internal bool) error {
	_, slot, err := store.writeSlotLocked(owner, promise, internal)
	if err != nil {
		return err
	}
	if slot.Kind != HeapPromise {
		return typeError(promise, slot.Kind, HeapPromise)
	}
	slot.Promise.Handled = true
	return nil
}

// DrainPromiseReactions returns one immutable settlement snapshot and removes
// the retained reaction edges. RegionStore does not invoke or enqueue them.
func (store *Store) DrainPromiseReactions(owner ownership.OwnerID, promise Ref) (PromiseSettlement, error) {
	if store == nil {
		return PromiseSettlement{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	region, slot, err := store.writeSlotLocked(owner, promise, false)
	if err != nil {
		return PromiseSettlement{}, err
	}
	if slot.Kind != HeapPromise {
		return PromiseSettlement{}, typeError(promise, slot.Kind, HeapPromise)
	}
	if slot.Promise.State == PromisePending {
		return PromiseSettlement{}, ErrPromisePending
	}
	values := make([]Value, 0, len(slot.Promise.Reactions)*3)
	for _, reaction := range slot.Promise.Reactions {
		values = append(values, reaction.OnFulfilled, reaction.OnRejected, reaction.Downstream)
	}
	unlinked := make([]Value, 0, len(values))
	for _, value := range values {
		if _, err := store.replaceValueLocked(owner, region, slot, value, Value{}, false); err != nil {
			for index := len(unlinked) - 1; index >= 0; index-- {
				_, _ = store.replaceValueLocked(owner, region, slot, Value{}, unlinked[index], true)
			}
			return PromiseSettlement{}, err
		}
		unlinked = append(unlinked, value)
	}
	settlement := PromiseSettlement{
		State:     slot.Promise.State,
		Result:    slot.Promise.Result,
		Reactions: append([]PromiseReaction(nil), slot.Promise.Reactions...),
	}
	slot.Promise.Reactions = nil
	return settlement, nil
}

func (store *Store) validateOptionalTypedValueLocked(owner ownership.OwnerID, value Value, kind HeapKind, label string, internal bool) error {
	if value.Kind() == ValueNull {
		return nil
	}
	if !value.IsRef() {
		return fmt.Errorf("%w: %s must be null or a %s Ref", ErrTypeMismatch, label, kind)
	}
	_, slot, err := store.slotLocked(value.Ref())
	if err != nil {
		return err
	}
	if slot.Kind != kind {
		return typeError(value.Ref(), slot.Kind, kind)
	}
	return nil
}
