package memory

import (
	"fmt"

	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

func (store *Store) AllocBytecodeFunction(owner ownership.OwnerID, regionID RegionID, name, environment Value, arity uint32, code []byte, constants []Value) (Ref, error) {
	return store.allocFunction(owner, regionID, Function{
		Kind:          FunctionBytecode,
		Name:          name,
		Environment:   environment,
		Arity:         arity,
		Constructible: true,
		Code:          append([]byte(nil), code...),
		Constants:     append([]Value(nil), constants...),
	})
}

func (store *Store) AllocNativeFunction(owner ownership.OwnerID, regionID RegionID, name, environment Value, arity uint32, nativeID uint64) (Ref, error) {
	return store.allocNativeFunction(owner, regionID, name, environment, arity, nativeID, false)
}

func (store *Store) AllocNativeConstructor(owner ownership.OwnerID, regionID RegionID, name, environment Value, arity uint32, nativeID uint64) (Ref, error) {
	return store.allocNativeFunction(owner, regionID, name, environment, arity, nativeID, true)
}

func (store *Store) AllocBoundNativeFunction(owner ownership.OwnerID, regionID RegionID, name, environment Value, arity uint32, nativeID uint64, captures []Value) (Ref, error) {
	return store.allocFunction(owner, regionID, Function{
		Kind:        FunctionNative,
		Name:        name,
		Environment: environment,
		Arity:       arity,
		NativeID:    nativeID,
		Captures:    append([]Value(nil), captures...),
	})
}

func (store *Store) allocNativeFunction(owner ownership.OwnerID, regionID RegionID, name, environment Value, arity uint32, nativeID uint64, constructible bool) (Ref, error) {
	return store.allocFunction(owner, regionID, Function{
		Kind:          FunctionNative,
		Name:          name,
		Environment:   environment,
		Arity:         arity,
		Constructible: constructible,
		NativeID:      nativeID,
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
	if function.ObjectHeader.Prototype == (Value{}) {
		function.ObjectHeader.Prototype = NullValue()
	}
	if err := store.validateOptionalTypedValueLocked(owner, function.Name, HeapString, "Function name", internal); err != nil {
		return err
	}
	if err := store.validateOptionalTypedValueLocked(owner, function.Environment, HeapContext, "Function environment", internal); err != nil {
		return err
	}
	values := make([]Value, 0, 2+len(function.Constants)+len(function.Captures))
	values = append(values, function.Name, function.Environment)
	values = append(values, function.Constants...)
	values = append(values, function.Captures...)
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
	function.Name = linked[0]
	function.Environment = linked[1]
	constantEnd := 2 + len(function.Constants)
	function.Constants = append([]Value(nil), linked[2:constantEnd]...)
	function.Captures = append([]Value(nil), linked[constantEnd:]...)
	slot.Function = cloneFunction(function)
	store.stats.LiveBytes += uint64(len(slot.Function.Code))
	return nil
}
