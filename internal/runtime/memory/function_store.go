package memory

import (
	"fmt"

	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

func (store *Store) AllocBytecodeFunction(owner ownership.OwnerID, regionID RegionID, name, environment Value, arity uint32, code []byte, constants []Value) (Ref, error) {
	return store.allocFunction(owner, regionID, Function{
		Kind:        FunctionBytecode,
		Name:        name,
		Environment: environment,
		Arity:       arity,
		Code:        append([]byte(nil), code...),
		Constants:   append([]Value(nil), constants...),
	})
}

func (store *Store) AllocNativeFunction(owner ownership.OwnerID, regionID RegionID, name, environment Value, arity uint32, nativeID uint64) (Ref, error) {
	return store.allocFunction(owner, regionID, Function{
		Kind:        FunctionNative,
		Name:        name,
		Environment: environment,
		Arity:       arity,
		NativeID:    nativeID,
	})
}

func (store *Store) allocFunction(owner ownership.OwnerID, regionID RegionID, function Function) (Ref, error) {
	if store == nil {
		return Ref{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	ref, err := store.allocKindLocked(owner, regionID, HeapFunction, false)
	if err != nil {
		return Ref{}, err
	}
	if err := store.initializeFunctionLocked(owner, ref, function, false); err != nil {
		_ = store.freeLocked(owner, ref, true)
		return Ref{}, err
	}
	return ref, nil
}

func (store *Store) DerefFunction(owner ownership.OwnerID, ref Ref) (Function, error) {
	if store == nil {
		return Function{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	_, slot, err := store.readSlotLocked(owner, ref)
	if err != nil {
		return Function{}, err
	}
	if slot.Kind != HeapFunction {
		return Function{}, typeError(ref, slot.Kind, HeapFunction)
	}
	return cloneFunction(slot.Function), nil
}

func (store *Store) initializeFunctionLocked(owner ownership.OwnerID, ref Ref, function Function, internal bool) error {
	region, slot, err := store.writeSlotLocked(owner, ref, internal)
	if err != nil {
		return err
	}
	if slot.Kind != HeapFunction {
		return typeError(ref, slot.Kind, HeapFunction)
	}
	if slot.Function.Kind != 0 {
		return fmt.Errorf("%w: Function slot is already initialized", ErrInvalidFunction)
	}
	if function.Kind != FunctionBytecode && function.Kind != FunctionNative {
		return fmt.Errorf("%w: unknown kind %d", ErrInvalidFunction, function.Kind)
	}
	if function.Kind == FunctionBytecode && function.NativeID != 0 {
		return fmt.Errorf("%w: bytecode Function has native ID", ErrInvalidFunction)
	}
	if function.Kind == FunctionNative && (function.NativeID == 0 || len(function.Code) != 0 || len(function.Constants) != 0) {
		return fmt.Errorf("%w: native Function requires only a nonzero native ID", ErrInvalidFunction)
	}
	if err := store.validateFunctionTypedValueLocked(owner, function.Name, HeapString, "name", internal); err != nil {
		return err
	}
	if err := store.validateFunctionTypedValueLocked(owner, function.Environment, HeapContext, "environment", internal); err != nil {
		return err
	}
	values := make([]Value, 0, 2+len(function.Constants))
	values = append(values, function.Name, function.Environment)
	values = append(values, function.Constants...)
	linked := make([]Value, 0, len(values))
	for _, value := range values {
		if err := store.replaceValueLocked(owner, region, slot, Value{}, value, internal); err != nil {
			for index := len(linked) - 1; index >= 0; index-- {
				_ = store.replaceValueLocked(owner, region, slot, linked[index], Value{}, true)
			}
			return err
		}
		linked = append(linked, value)
	}
	slot.Function = cloneFunction(function)
	store.stats.LiveBytes += uint64(len(slot.Function.Code))
	return nil
}

func (store *Store) validateFunctionTypedValueLocked(owner ownership.OwnerID, value Value, kind HeapKind, label string, internal bool) error {
	if value.Kind() == ValueNull {
		return nil
	}
	if !value.IsRef() {
		return fmt.Errorf("%w: Function %s must be null or a %s Ref", ErrInvalidFunction, label, kind)
	}
	_, slot, err := store.readSlotLocked(owner, value.Ref())
	if err != nil && internal {
		_, slot, err = store.slotLocked(value.Ref())
	}
	if err != nil {
		return err
	}
	if slot.Kind != kind {
		return typeError(value.Ref(), slot.Kind, kind)
	}
	return nil
}
