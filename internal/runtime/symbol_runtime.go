package runtime

import (
	"fmt"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func (intrinsics *Intrinsics) installSymbolBuiltins(context *TaskContext) error {
	name, err := context.NewString("Symbol")
	if err != nil {
		return err
	}
	constructor, err := context.Realm.store.AllocNativeFunction(
		context.Owner,
		context.MemoryRegion,
		memory.RefValue(name),
		memory.NullValue(),
		0,
		nativeSymbolConstructor,
	)
	if err != nil {
		return err
	}
	if err := intrinsics.initializeFunctionWithPrototype(context, constructor, memory.RefValue(name), 0, intrinsics.SymbolPrototype); err != nil {
		return err
	}
	intrinsics.SymbolConstructor = constructor

	registry, err := context.NewMap()
	if err != nil {
		return err
	}
	intrinsics.SymbolRegistry = registry

	iteratorDescription, err := newStringValue(context, "Symbol.iterator")
	if err != nil {
		return err
	}
	iterator, err := context.NewSymbol(iteratorDescription)
	if err != nil {
		return err
	}
	intrinsics.SymbolIterator = iterator

	forMethod, err := intrinsics.newBuiltinMethod(context, "for", 1, nativeSymbolFor)
	if err != nil {
		return err
	}
	if err := defineData(context, constructor, "for", memory.RefValue(forMethod), true, false, true); err != nil {
		return err
	}
	if err := defineData(context, constructor, "iterator", memory.RefValue(iterator), false, false, false); err != nil {
		return err
	}
	if err := installMethods(intrinsics, context, intrinsics.SymbolPrototype, []builtinMethod{
		{"toString", 0, nativeSymbolToString},
		{"valueOf", 0, nativeSymbolValueOf},
	}); err != nil {
		return err
	}
	description, err := intrinsics.newBuiltinMethod(context, "get description", 0, nativeSymbolDescription)
	if err != nil {
		return err
	}
	return defineAccessor(context, intrinsics.SymbolPrototype, "description", memory.RefValue(description), memory.UndefinedValue(), false, true)
}

func (intrinsics *Intrinsics) installSymbolIteratorAliases(context *TaskContext) error {
	for _, alias := range []struct {
		target memory.Ref
		name   string
	}{
		{intrinsics.ArrayPrototype, "values"},
		{intrinsics.StringPrototype, "values"},
		{intrinsics.MapPrototype, "entries"},
		{intrinsics.SetPrototype, "values"},
	} {
		name, err := context.NewString(alias.name)
		if err != nil {
			return err
		}
		method, found, err := context.GetOwnProperty(alias.target, name)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("runtime: missing %s iterator method", alias.name)
		}
		if err := context.DefineProperty(alias.target, intrinsics.SymbolIterator, memory.DataProperty(method, true, false, true)); err != nil {
			return err
		}
	}
	identity, err := intrinsics.newBuiltinMethod(context, "[Symbol.iterator]", 0, nativeIteratorIdentity)
	if err != nil {
		return err
	}
	return context.DefineProperty(intrinsics.IteratorPrototype, intrinsics.SymbolIterator, memory.DataProperty(memory.RefValue(identity), true, false, true))
}

func builtinSymbolConstructor(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	description := memory.NullValue()
	if len(arguments) != 0 && arguments[0].Kind() != memory.ValueUndefined {
		text, err := execution.toString(arguments[0])
		if err != nil {
			return memory.Value{}, err
		}
		value, err := newStringValue(execution.context, text)
		if err != nil {
			return memory.Value{}, err
		}
		description = value
	}
	symbol, err := execution.context.NewSymbol(description)
	return memory.RefValue(symbol), err
}

func builtinSymbolFor(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	if execution.context.intrinsics == nil {
		return memory.Value{}, fmt.Errorf("runtime: Symbol registry is not initialized")
	}
	keyText, err := execution.toString(argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	key, err := newStringValue(execution.context, keyText)
	if err != nil {
		return memory.Value{}, err
	}
	if symbol, found, err := execution.context.MapGet(execution.context.intrinsics.SymbolRegistry, key); err != nil || found {
		return symbol, err
	}
	symbol, err := execution.context.NewSymbol(key)
	if err != nil {
		return memory.Value{}, err
	}
	value := memory.RefValue(symbol)
	if err := execution.context.MapSet(execution.context.intrinsics.SymbolRegistry, key, value); err != nil {
		return memory.Value{}, err
	}
	return value, nil
}

func builtinSymbolToString(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, _ []memory.Value) (memory.Value, error) {
	text, err := symbolDescriptiveString(execution.context, this)
	if err != nil {
		return memory.Value{}, err
	}
	return newStringValue(execution.context, text)
}

func builtinSymbolValueOf(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, _ []memory.Value) (memory.Value, error) {
	if _, err := requireSymbol(execution.context, this); err != nil {
		return memory.Value{}, err
	}
	return this, nil
}

func builtinSymbolDescription(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, _ []memory.Value) (memory.Value, error) {
	symbol, err := requireSymbol(execution.context, this)
	if err != nil {
		return memory.Value{}, err
	}
	if symbol.Description.Kind() == memory.ValueNull {
		return memory.UndefinedValue(), nil
	}
	return symbol.Description, nil
}

func builtinIteratorIdentity(_ *execution, _ memory.Ref, _ memory.Function, this memory.Value, _ []memory.Value) (memory.Value, error) {
	return this, nil
}

func requireSymbol(context *TaskContext, value memory.Value) (memory.Symbol, error) {
	if !value.IsRef() {
		return memory.Symbol{}, fmt.Errorf("%w: receiver is not a Symbol", ErrOperandType)
	}
	kind, err := context.HeapKind(value.Ref())
	if err != nil {
		return memory.Symbol{}, err
	}
	if kind != memory.HeapSymbol {
		return memory.Symbol{}, fmt.Errorf("%w: receiver is a %s, want Symbol", ErrOperandType, kind)
	}
	return context.DerefSymbol(value.Ref())
}

func symbolDescriptiveString(context *TaskContext, value memory.Value) (string, error) {
	symbol, err := requireSymbol(context, value)
	if err != nil {
		return "", err
	}
	if symbol.Description.Kind() == memory.ValueNull {
		return "Symbol()", nil
	}
	description, err := context.DerefString(symbol.Description.Ref())
	if err != nil {
		return "", err
	}
	return "Symbol(" + description + ")", nil
}
