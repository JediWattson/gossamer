package memory

import (
	"fmt"

	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

func (store *Store) AllocError(owner ownership.OwnerID, regionID RegionID, kind ErrorKind, message Value) (Ref, error) {
	if store == nil {
		return Ref{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	ref, err := store.allocKindLocked(owner, regionID, HeapError, false)
	if err != nil {
		return Ref{}, err
	}
	value := ErrorObject{Kind: kind, Message: message, Stack: NullValue()}
	if err := store.initializeErrorLocked(owner, ref, value, false); err != nil {
		_ = store.freeLocked(owner, ref, true)
		return Ref{}, err
	}
	return ref, nil
}

func (store *Store) DerefError(owner ownership.OwnerID, ref Ref) (ErrorObject, error) {
	if store == nil {
		return ErrorObject{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	_, slot, err := store.readSlotLocked(owner, ref)
	if err != nil {
		return ErrorObject{}, err
	}
	if slot.Kind != HeapError {
		return ErrorObject{}, typeError(ref, slot.Kind, HeapError)
	}
	return cloneError(slot.Error), nil
}

func (store *Store) SetErrorMessage(owner ownership.OwnerID, ref Ref, message Value) error {
	if store == nil {
		return fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return store.setErrorStringLocked(owner, ref, message, false, false)
}

func (store *Store) SetErrorStack(owner ownership.OwnerID, ref Ref, stack Value) error {
	if store == nil {
		return fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return store.setErrorStringLocked(owner, ref, stack, true, false)
}

func (store *Store) setErrorStringLocked(owner ownership.OwnerID, ref Ref, value Value, stack bool, internal bool) error {
	region, slot, err := store.writeSlotLocked(owner, ref, internal)
	if err != nil {
		return err
	}
	if slot.Kind != HeapError {
		return typeError(ref, slot.Kind, HeapError)
	}
	label := "Error message"
	old := slot.Error.Message
	if stack {
		label = "Error stack"
		old = slot.Error.Stack
	}
	if err := store.validateOptionalTypedValueLocked(owner, value, HeapString, label, internal); err != nil {
		return err
	}
	value, err = store.replaceValueLocked(owner, region, slot, old, value, internal)
	if err != nil {
		return err
	}
	if stack {
		slot.Error.Stack = value
	} else {
		slot.Error.Message = value
	}
	return nil
}

func (store *Store) SetErrorCause(owner ownership.OwnerID, ref Ref, cause Value) error {
	if store == nil {
		return fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	region, slot, err := store.writeSlotLocked(owner, ref, false)
	if err != nil {
		return err
	}
	if slot.Kind != HeapError {
		return typeError(ref, slot.Kind, HeapError)
	}
	old := Value{}
	if slot.Error.HasCause {
		old = slot.Error.Cause
	}
	cause, err = store.replaceValueLocked(owner, region, slot, old, cause, false)
	if err != nil {
		return err
	}
	slot.Error.Cause = cause
	slot.Error.HasCause = true
	return nil
}

func (store *Store) ClearErrorCause(owner ownership.OwnerID, ref Ref) error {
	if store == nil {
		return fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	region, slot, err := store.writeSlotLocked(owner, ref, false)
	if err != nil {
		return err
	}
	if slot.Kind != HeapError {
		return typeError(ref, slot.Kind, HeapError)
	}
	if !slot.Error.HasCause {
		return nil
	}
	if _, err := store.replaceValueLocked(owner, region, slot, slot.Error.Cause, Value{}, false); err != nil {
		return err
	}
	slot.Error.Cause = Value{}
	slot.Error.HasCause = false
	return nil
}

func (store *Store) SetAggregateErrors(owner ownership.OwnerID, ref Ref, errors []Value) error {
	if store == nil {
		return fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return store.setAggregateErrorsLocked(owner, ref, errors, false)
}

func (store *Store) setAggregateErrorsLocked(owner ownership.OwnerID, ref Ref, errors []Value, internal bool) error {
	region, slot, err := store.writeSlotLocked(owner, ref, internal)
	if err != nil {
		return err
	}
	if slot.Kind != HeapError {
		return typeError(ref, slot.Kind, HeapError)
	}
	if slot.Error.Kind != ErrorAggregate {
		return fmt.Errorf("%w: %s is %s", ErrInvalidError, ref, slot.Error.Kind.Name())
	}
	linked, err := store.linkValuesLocked(owner, region, slot, errors, internal)
	if err != nil {
		return err
	}
	unlinked := make([]Value, 0, len(slot.Error.Errors))
	for _, old := range slot.Error.Errors {
		if _, err := store.replaceValueLocked(owner, region, slot, old, Value{}, internal); err != nil {
			for index := len(unlinked) - 1; index >= 0; index-- {
				_, _ = store.replaceValueLocked(owner, region, slot, Value{}, unlinked[index], true)
			}
			store.unlinkValuesLocked(region, slot, linked)
			return err
		}
		unlinked = append(unlinked, old)
	}
	slot.Error.Errors = append([]Value(nil), linked...)
	return nil
}

func (store *Store) initializeErrorLocked(owner ownership.OwnerID, ref Ref, value ErrorObject, internal bool) error {
	region, slot, err := store.writeSlotLocked(owner, ref, internal)
	if err != nil {
		return err
	}
	if slot.Kind != HeapError {
		return typeError(ref, slot.Kind, HeapError)
	}
	if slot.Error.Kind != 0 {
		return fmt.Errorf("%w: descriptor already initialized", ErrInvalidError)
	}
	if value.Kind.Name() == "" {
		return fmt.Errorf("%w: kind %d", ErrInvalidError, value.Kind)
	}
	if value.Kind != ErrorAggregate && len(value.Errors) != 0 {
		return fmt.Errorf("%w: %s cannot retain aggregate members", ErrInvalidError, value.Kind.Name())
	}
	if !value.HasCause && value.Cause != (Value{}) {
		return fmt.Errorf("%w: absent cause retains a value", ErrInvalidError)
	}
	if err := store.validateOptionalTypedValueLocked(owner, value.Message, HeapString, "Error message", internal); err != nil {
		return err
	}
	if err := store.validateOptionalTypedValueLocked(owner, value.Stack, HeapString, "Error stack", internal); err != nil {
		return err
	}
	values := []Value{value.Message, value.Stack}
	if value.HasCause {
		values = append(values, value.Cause)
	}
	values = append(values, value.Errors...)
	linked, err := store.linkValuesLocked(owner, region, slot, values, internal)
	if err != nil {
		return err
	}
	value.Message = linked[0]
	value.Stack = linked[1]
	next := 2
	if value.HasCause {
		value.Cause = linked[next]
		next++
	}
	value.Errors = append([]Value(nil), linked[next:]...)
	slot.Error = cloneError(value)
	return nil
}

func (store *Store) linkValuesLocked(owner ownership.OwnerID, region *Region, slot *Slot, values []Value, internal bool) ([]Value, error) {
	linked := make([]Value, 0, len(values))
	for _, value := range values {
		value, err := store.replaceValueLocked(owner, region, slot, Value{}, value, internal)
		if err != nil {
			store.unlinkValuesLocked(region, slot, linked)
			return nil, err
		}
		linked = append(linked, value)
	}
	return linked, nil
}

func (store *Store) unlinkValuesLocked(region *Region, slot *Slot, values []Value) {
	for index := len(values) - 1; index >= 0; index-- {
		_, _ = store.replaceValueLocked(region.Owner, region, slot, values[index], Value{}, true)
	}
}
