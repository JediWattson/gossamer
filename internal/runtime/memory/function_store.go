package memory

import (
	"fmt"

	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

func (store *Store) AllocBytecodeFunction(owner ownership.OwnerID, regionID RegionID, name, environment Value, arity uint32, code []byte, constants []Value) (Ref, error) {
	return store.allocBytecodeFunction(owner, regionID, name, environment, arity, code, constants, FunctionThisDynamic, Value{})
}

func (store *Store) AllocArrowBytecodeFunction(owner ownership.OwnerID, regionID RegionID, name, environment Value, lexicalThis Value, arity uint32, code []byte, constants []Value) (Ref, error) {
	return store.allocBytecodeFunction(owner, regionID, name, environment, arity, code, constants, FunctionThisLexical, lexicalThis)
}

func (store *Store) allocBytecodeFunction(owner ownership.OwnerID, regionID RegionID, name, environment Value, arity uint32, code []byte, constants []Value, thisMode FunctionThisMode, lexicalThis Value) (Ref, error) {
	return store.allocFunction(owner, regionID, Function{
		Kind:          FunctionBytecode,
		Name:          name,
		Environment:   environment,
		Arity:         arity,
		Constructible: thisMode == FunctionThisDynamic,
		ThisMode:      thisMode,
		LexicalThis:   lexicalThis,
		Code:          code,
		Constants:     constants,
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
		Captures:    captures,
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
	if err := store.initializeFunctionLocked(owner, ref, function, false, false); err != nil {
		_ = store.freeLocked(owner, ref, true)
		return Ref{}, err
	}
	return ref, nil
}

// AllocBytecodeClosure creates a dynamic-this closure whose immutable
// executable storage aliases template. The closure still gets its own heap
// identity, environment edges, and Function object properties.
func (store *Store) AllocBytecodeClosure(owner ownership.OwnerID, regionID RegionID, template Ref, environment Value) (Ref, error) {
	return store.allocBytecodeClosure(owner, regionID, template, environment, Value{}, FunctionThisDynamic)
}

// AllocArrowBytecodeClosure creates a lexical-this closure whose immutable
// executable storage aliases template.
func (store *Store) AllocArrowBytecodeClosure(owner ownership.OwnerID, regionID RegionID, template Ref, environment, lexicalThis Value) (Ref, error) {
	return store.allocBytecodeClosure(owner, regionID, template, environment, lexicalThis, FunctionThisLexical)
}

func (store *Store) allocBytecodeClosure(owner ownership.OwnerID, regionID RegionID, template Ref, environment, lexicalThis Value, thisMode FunctionThisMode) (Ref, error) {
	if store == nil {
		return Ref{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	_, templateSlot, err := store.readSlotLocked(owner, template)
	if err != nil {
		return Ref{}, err
	}
	if templateSlot.Kind != HeapFunction || templateSlot.Function.Kind != FunctionBytecode {
		return Ref{}, fmt.Errorf("%w: template %s is not bytecode", ErrInvalidFunction, template)
	}
	templateFunction := loadFunction(*templateSlot.Function)
	function := Function{
		Kind:          FunctionBytecode,
		Name:          templateFunction.Name,
		Environment:   environment,
		Arity:         templateFunction.Arity,
		Constructible: thisMode == FunctionThisDynamic,
		ThisMode:      thisMode,
		LexicalThis:   lexicalThis,
		Code:          templateFunction.Code,
		Locations:     templateFunction.Locations,
		Constants:     templateFunction.Constants,
	}
	ref, err := store.allocKindLocked(owner, regionID, HeapFunction, false)
	if err != nil {
		return Ref{}, err
	}
	if err := store.initializeFunctionLocked(owner, ref, function, false, true); err != nil {
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
	return cloneFunction(*slot.Function), nil
}

// LoadFunction returns an immutable execution view backed by Store-owned
// slices. Callers must not mutate Code, Locations, Constants, or Captures; use
// DerefFunction when a defensive diagnostic snapshot is required.
func (store *Store) LoadFunction(owner ownership.OwnerID, ref Ref) (Function, error) {
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
	return loadFunction(*slot.Function), nil
}

func (store *Store) SetFunctionLocations(owner ownership.OwnerID, ref Ref, locations []SourceSpan) error {
	if store == nil {
		return fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	_, slot, err := store.writeSlotLocked(owner, ref, false)
	if err != nil {
		return err
	}
	if slot.Kind != HeapFunction || slot.Function.Kind != FunctionBytecode {
		return typeError(ref, slot.Kind, HeapFunction)
	}
	store.stats.LiveBytes -= uint64(len(slot.Function.Locations)) * 8
	slot.Function.Locations = append([]SourceSpan(nil), locations...)
	store.stats.LiveBytes += uint64(len(slot.Function.Locations)) * 8
	return nil
}

func (store *Store) initializeFunctionLocked(owner ownership.OwnerID, ref Ref, function Function, internal, shareExecutable bool) error {
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
	if function.Kind == FunctionNative && (function.NativeID == 0 || len(function.Code) != 0 || len(function.Locations) != 0 || len(function.Constants) != 0) {
		return fmt.Errorf("%w: native Function requires only a nonzero native ID", ErrInvalidFunction)
	}
	if function.ThisMode != FunctionThisDynamic && function.ThisMode != FunctionThisLexical {
		return fmt.Errorf("%w: unknown this mode %d", ErrInvalidFunction, function.ThisMode)
	}
	if function.ThisMode == FunctionThisLexical {
		if function.Kind != FunctionBytecode || function.Constructible {
			return fmt.Errorf("%w: lexical-this Function must be non-constructible bytecode with a captured receiver", ErrInvalidFunction)
		}
	} else if function.LexicalThis != (Value{}) {
		return fmt.Errorf("%w: dynamic-this Function retains a lexical receiver", ErrInvalidFunction)
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
	values := make([]Value, 0, 3+len(function.Constants)+len(function.Captures))
	values = append(values, function.Name, function.Environment)
	if function.ThisMode == FunctionThisLexical {
		values = append(values, function.LexicalThis)
	}
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
	valueStart := 2
	if function.ThisMode == FunctionThisLexical {
		function.LexicalThis = linked[valueStart]
		valueStart++
	}
	originalConstants := function.Constants
	constantEnd := valueStart + len(originalConstants)
	function.Constants = linked[valueStart:constantEnd]
	function.Captures = linked[constantEnd:]
	shareConstants := shareExecutable && equalValues(originalConstants, function.Constants)
	if shareConstants {
		function.Constants = originalConstants
	}
	*slot.Function = storeFunction(function, shareExecutable, shareConstants)
	store.stats.LiveBytes += uint64(len(slot.Function.Code)) + uint64(len(slot.Function.Locations))*8
	return nil
}
