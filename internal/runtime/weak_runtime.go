package runtime

import (
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func (intrinsics *Intrinsics) installWeakCollectionBuiltins(context *TaskContext) error {
	if err := installMethods(intrinsics, context, intrinsics.WeakMapPrototype, []builtinMethod{
		{"get", 1, nativeWeakMapGet},
		{"set", 2, nativeWeakMapSet},
		{"has", 1, nativeWeakMapHas},
		{"delete", 1, nativeWeakMapDelete},
	}); err != nil {
		return err
	}
	if err := installMethods(intrinsics, context, intrinsics.WeakSetPrototype, []builtinMethod{
		{"add", 1, nativeWeakSetAdd},
		{"has", 1, nativeWeakSetHas},
		{"delete", 1, nativeWeakSetDelete},
	}); err != nil {
		return err
	}
	name, err := context.NewString("FinalizationRegistry")
	if err != nil {
		return err
	}
	constructor, err := context.NewNativeConstructor(memory.RefValue(name), memory.NullValue(), 1, nativeFinalizationRegistryConstructor)
	if err != nil {
		return err
	}
	prototypeName, err := context.NewString("prototype")
	if err != nil {
		return err
	}
	prototype, found, err := context.GetOwnProperty(constructor, prototypeName)
	if err != nil || !found || !prototype.IsRef() {
		if err != nil {
			return err
		}
		return ErrOperandType
	}
	if err := installMethods(intrinsics, context, prototype.Ref(), []builtinMethod{
		{"register", 2, nativeFinalizationRegistryRegister},
		{"unregister", 1, nativeFinalizationRegistryUnregister},
	}); err != nil {
		return err
	}
	return intrinsics.defineGlobal(context, "FinalizationRegistry", memory.RefValue(constructor))
}

func builtinFinalizationRegistryConstructor(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	if !this.IsRef() {
		return memory.Value{}, ErrNotConstructor
	}
	if _, err := requireCallable(execution.context, argument(arguments, 0)); err != nil {
		return memory.Value{}, err
	}
	// RegionStore lifetimes are deterministic. The compatibility constructor
	// deliberately retains no cleanup callback and schedules no nondeterministic
	// finalizer jobs.
	return this, nil
}

func builtinFinalizationRegistryRegister(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	if _, err := requireObjectLike(execution.context, this, "FinalizationRegistry receiver"); err != nil {
		return memory.Value{}, err
	}
	if _, object, err := weakKey(execution.context, argument(arguments, 0)); err != nil || !object {
		if err != nil {
			return memory.Value{}, err
		}
		return memory.Value{}, ErrOperandType
	}
	return memory.UndefinedValue(), nil
}

func builtinFinalizationRegistryUnregister(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, _ []memory.Value) (memory.Value, error) {
	if _, err := requireObjectLike(execution.context, this, "FinalizationRegistry receiver"); err != nil {
		return memory.Value{}, err
	}
	return memory.BoolValue(false), nil
}

func builtinWeakMapConstructor(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	if !this.IsRef() {
		return memory.Value{}, ErrNotConstructor
	}
	if len(arguments) != 0 && arguments[0].Kind() != memory.ValueUndefined && arguments[0].Kind() != memory.ValueNull {
		return memory.Value{}, ErrOperandType
	}
	table, err := execution.context.NewWeakMap()
	return memory.RefValue(table), err
}

func builtinWeakMapGet(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	table, err := requireKind(execution.context, this, memory.HeapWeakMap, "WeakMap receiver")
	if err != nil {
		return memory.Value{}, err
	}
	key, object, err := weakKey(execution.context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	if !object {
		return memory.UndefinedValue(), nil
	}
	value, found, err := execution.context.WeakMapGet(table, key)
	if err != nil || !found {
		return memory.UndefinedValue(), err
	}
	return value, nil
}

func builtinWeakMapSet(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	table, err := requireKind(execution.context, this, memory.HeapWeakMap, "WeakMap receiver")
	if err != nil {
		return memory.Value{}, err
	}
	key, object, err := weakKey(execution.context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	if !object {
		return memory.Value{}, ErrOperandType
	}
	if err := execution.context.WeakMapSet(table, key, argument(arguments, 1)); err != nil {
		return memory.Value{}, err
	}
	return this, nil
}

func builtinWeakMapHas(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	table, err := requireKind(execution.context, this, memory.HeapWeakMap, "WeakMap receiver")
	if err != nil {
		return memory.Value{}, err
	}
	key, object, err := weakKey(execution.context, argument(arguments, 0))
	if err != nil || !object {
		return memory.BoolValue(false), err
	}
	_, found, err := execution.context.WeakMapGet(table, key)
	return memory.BoolValue(found), err
}

func builtinWeakMapDelete(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	table, err := requireKind(execution.context, this, memory.HeapWeakMap, "WeakMap receiver")
	if err != nil {
		return memory.Value{}, err
	}
	key, object, err := weakKey(execution.context, argument(arguments, 0))
	if err != nil || !object {
		return memory.BoolValue(false), err
	}
	deleted, err := execution.context.WeakMapDelete(table, key)
	return memory.BoolValue(deleted), err
}

func builtinWeakSetConstructor(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	if !this.IsRef() {
		return memory.Value{}, ErrNotConstructor
	}
	if len(arguments) != 0 && arguments[0].Kind() != memory.ValueUndefined && arguments[0].Kind() != memory.ValueNull {
		return memory.Value{}, ErrOperandType
	}
	table, err := execution.context.NewWeakSet()
	return memory.RefValue(table), err
}

func builtinWeakSetAdd(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	table, err := requireKind(execution.context, this, memory.HeapWeakSet, "WeakSet receiver")
	if err != nil {
		return memory.Value{}, err
	}
	key, object, err := weakKey(execution.context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	if !object {
		return memory.Value{}, ErrOperandType
	}
	if err := execution.context.WeakSetAdd(table, key); err != nil {
		return memory.Value{}, err
	}
	return this, nil
}

func builtinWeakSetHas(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	table, err := requireKind(execution.context, this, memory.HeapWeakSet, "WeakSet receiver")
	if err != nil {
		return memory.Value{}, err
	}
	key, object, err := weakKey(execution.context, argument(arguments, 0))
	if err != nil || !object {
		return memory.BoolValue(false), err
	}
	found, err := execution.context.WeakSetHas(table, key)
	return memory.BoolValue(found), err
}

func builtinWeakSetDelete(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	table, err := requireKind(execution.context, this, memory.HeapWeakSet, "WeakSet receiver")
	if err != nil {
		return memory.Value{}, err
	}
	key, object, err := weakKey(execution.context, argument(arguments, 0))
	if err != nil || !object {
		return memory.BoolValue(false), err
	}
	deleted, err := execution.context.WeakSetDelete(table, key)
	return memory.BoolValue(deleted), err
}

func weakKey(context *TaskContext, value memory.Value) (memory.Ref, bool, error) {
	object, err := isObjectValue(context, value)
	if err != nil || !object || !value.IsRef() {
		return memory.Ref{}, false, err
	}
	return value.Ref(), true, nil
}
