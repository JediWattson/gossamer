package runtime

import (
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func (intrinsics *Intrinsics) installCollectionBuiltins(context *TaskContext) error {
	stringConstructor, err := intrinsics.newBuiltinMethod(context, "String", 1, nativeStringConstructor)
	if err != nil {
		return err
	}
	intrinsics.StringConstructor = stringConstructor
	if err := defineData(context, stringConstructor, "prototype", memory.RefValue(intrinsics.StringPrototype), false, false, false); err != nil {
		return err
	}
	if err := defineData(context, intrinsics.StringPrototype, "constructor", memory.RefValue(stringConstructor), true, false, true); err != nil {
		return err
	}

	for _, constructor := range []struct {
		name        string
		id          uint64
		prototype   memory.Ref
		destination *memory.Ref
	}{
		{"Map", nativeMapConstructor, intrinsics.MapPrototype, &intrinsics.MapConstructor},
		{"Set", nativeSetConstructor, intrinsics.SetPrototype, &intrinsics.SetConstructor},
	} {
		name, err := context.NewString(constructor.name)
		if err != nil {
			return err
		}
		ref, err := context.Realm.store.AllocNativeConstructor(context.Owner, context.MemoryRegion, memory.RefValue(name), memory.NullValue(), 0, constructor.id)
		if err != nil {
			return err
		}
		if err := intrinsics.initializeFunctionWithPrototype(context, ref, memory.RefValue(name), 0, constructor.prototype); err != nil {
			return err
		}
		*constructor.destination = ref
	}

	if err := installMethods(intrinsics, context, intrinsics.StringPrototype, []builtinMethod{
		{"toString", 0, nativeStringToString}, {"valueOf", 0, nativeStringValueOf},
		{"charAt", 1, nativeStringCharAt}, {"includes", 1, nativeStringIncludes},
		{"indexOf", 1, nativeStringIndexOf}, {"slice", 2, nativeStringSlice},
		{"toUpperCase", 0, nativeStringToUpperCase}, {"toLowerCase", 0, nativeStringToLowerCase},
		{"trim", 0, nativeStringTrim}, {"split", 2, nativeStringSplit}, {"values", 0, nativeStringValues},
	}); err != nil {
		return err
	}
	if err := installMethods(intrinsics, context, intrinsics.IteratorPrototype, []builtinMethod{{"next", 0, nativeIteratorNext}}); err != nil {
		return err
	}
	if err := installMethods(intrinsics, context, intrinsics.ArrayPrototype, []builtinMethod{
		{"map", 1, nativeArrayMap}, {"filter", 1, nativeArrayFilter}, {"forEach", 1, nativeArrayForEach},
		{"includes", 1, nativeArrayIncludes}, {"indexOf", 1, nativeArrayIndexOf},
		{"keys", 0, nativeArrayKeys}, {"values", 0, nativeArrayValues}, {"entries", 0, nativeArrayEntries},
	}); err != nil {
		return err
	}
	if err := installMethods(intrinsics, context, intrinsics.MapPrototype, []builtinMethod{
		{"get", 1, nativeMapGet}, {"set", 2, nativeMapSet}, {"has", 1, nativeMapHas},
		{"delete", 1, nativeMapDelete}, {"clear", 0, nativeMapClear},
		{"keys", 0, nativeMapKeys}, {"values", 0, nativeMapValues}, {"entries", 0, nativeMapEntries},
		{"forEach", 1, nativeMapForEach},
	}); err != nil {
		return err
	}
	if err := installMethods(intrinsics, context, intrinsics.SetPrototype, []builtinMethod{
		{"add", 1, nativeSetAdd}, {"has", 1, nativeSetHas}, {"delete", 1, nativeSetDelete},
		{"clear", 0, nativeSetClear}, {"values", 0, nativeSetValues}, {"entries", 0, nativeSetEntries},
		{"forEach", 1, nativeSetForEach},
	}); err != nil {
		return err
	}
	mapSize, err := intrinsics.newBuiltinMethod(context, "get size", 0, nativeMapSize)
	if err != nil {
		return err
	}
	setSize, err := intrinsics.newBuiltinMethod(context, "get size", 0, nativeSetSize)
	if err != nil {
		return err
	}
	if err := defineAccessor(context, intrinsics.MapPrototype, "size", memory.RefValue(mapSize), memory.UndefinedValue(), false, true); err != nil {
		return err
	}
	return defineAccessor(context, intrinsics.SetPrototype, "size", memory.RefValue(setSize), memory.UndefinedValue(), false, true)
}

type builtinMethod struct {
	name  string
	arity uint32
	id    uint64
}

func installMethods(intrinsics *Intrinsics, context *TaskContext, target memory.Ref, methods []builtinMethod) error {
	for _, method := range methods {
		function, err := intrinsics.newBuiltinMethod(context, method.name, method.arity, method.id)
		if err != nil {
			return err
		}
		if err := defineData(context, target, method.name, memory.RefValue(function), true, false, true); err != nil {
			return err
		}
	}
	return nil
}

func defineAccessor(context *TaskContext, object memory.Ref, name string, getter, setter memory.Value, enumerable, configurable bool) error {
	nameRef, err := context.NewString(name)
	if err != nil {
		return err
	}
	return context.DefineProperty(object, nameRef, memory.AccessorProperty(getter, setter, enumerable, configurable))
}

func builtinStringConstructor(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	text := ""
	var err error
	if len(arguments) != 0 {
		text, err = execution.toString(arguments[0])
		if err != nil {
			return memory.Value{}, err
		}
	}
	ref, err := execution.context.NewString(text)
	return memory.RefValue(ref), err
}

func builtinStringToString(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, _ []memory.Value) (memory.Value, error) {
	if _, err := requireString(execution.context, this); err != nil {
		return memory.Value{}, err
	}
	return this, nil
}

func builtinStringCharAt(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	text, err := requireString(execution.context, this)
	if err != nil {
		return memory.Value{}, err
	}
	index, err := integerArgument(execution, arguments, 0, 0)
	if err != nil {
		return memory.Value{}, err
	}
	runes := []rune(text)
	result := ""
	if index >= 0 && index < int64(len(runes)) {
		result = string(runes[index])
	}
	ref, err := execution.context.NewString(result)
	return memory.RefValue(ref), err
}

func builtinStringIncludes(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	text, err := requireString(execution.context, this)
	if err != nil {
		return memory.Value{}, err
	}
	needle, err := execution.toString(argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	start, err := integerArgument(execution, arguments, 1, 0)
	if err != nil {
		return memory.Value{}, err
	}
	runes := []rune(text)
	if start < 0 {
		start = 0
	}
	if start > int64(len(runes)) {
		return memory.BoolValue(false), nil
	}
	return memory.BoolValue(strings.Contains(string(runes[start:]), needle)), nil
}

func builtinStringIndexOf(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	text, err := requireString(execution.context, this)
	if err != nil {
		return memory.Value{}, err
	}
	needle, err := execution.toString(argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	start, err := integerArgument(execution, arguments, 1, 0)
	if err != nil {
		return memory.Value{}, err
	}
	if start < 0 {
		start = 0
	}
	runes := []rune(text)
	if start > int64(len(runes)) {
		return memory.NumberValue(-1), nil
	}
	tail := string(runes[start:])
	position := strings.Index(tail, needle)
	if position < 0 {
		return memory.NumberValue(-1), nil
	}
	return memory.NumberValue(float64(start + int64(utf8.RuneCountInString(tail[:position])))), nil
}

func builtinStringSlice(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	text, err := requireString(execution.context, this)
	if err != nil {
		return memory.Value{}, err
	}
	runes := []rune(text)
	start, err := relativeRuneIndex(execution, argument(arguments, 0), len(runes), 0)
	if err != nil {
		return memory.Value{}, err
	}
	endValue := memory.UndefinedValue()
	if len(arguments) > 1 {
		endValue = arguments[1]
	}
	end, err := relativeRuneIndex(execution, endValue, len(runes), len(runes))
	if err != nil {
		return memory.Value{}, err
	}
	if end < start {
		end = start
	}
	ref, err := execution.context.NewString(string(runes[start:end]))
	return memory.RefValue(ref), err
}

func builtinStringToUpperCase(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, _ []memory.Value) (memory.Value, error) {
	text, err := requireString(execution.context, this)
	if err != nil {
		return memory.Value{}, err
	}
	ref, err := execution.context.NewString(strings.ToUpper(text))
	return memory.RefValue(ref), err
}

func builtinStringToLowerCase(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, _ []memory.Value) (memory.Value, error) {
	text, err := requireString(execution.context, this)
	if err != nil {
		return memory.Value{}, err
	}
	ref, err := execution.context.NewString(strings.ToLower(text))
	return memory.RefValue(ref), err
}

func builtinStringTrim(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, _ []memory.Value) (memory.Value, error) {
	text, err := requireString(execution.context, this)
	if err != nil {
		return memory.Value{}, err
	}
	ref, err := execution.context.NewString(strings.TrimSpace(text))
	return memory.RefValue(ref), err
}

func builtinStringSplit(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	text, err := requireString(execution.context, this)
	if err != nil {
		return memory.Value{}, err
	}
	parts := []string{text}
	if len(arguments) != 0 && arguments[0].Kind() != memory.ValueUndefined {
		separator, err := execution.toString(arguments[0])
		if err != nil {
			return memory.Value{}, err
		}
		if separator == "" {
			parts = make([]string, 0, utf8.RuneCountInString(text))
			for _, character := range text {
				parts = append(parts, string(character))
			}
		} else {
			parts = strings.Split(text, separator)
		}
	}
	array, err := execution.context.NewArray(uint32(len(parts)))
	if err != nil {
		return memory.Value{}, err
	}
	for index, part := range parts {
		ref, err := execution.context.NewString(part)
		if err != nil {
			return memory.Value{}, err
		}
		if err := execution.context.SetArrayElement(array, uint32(index), memory.RefValue(ref)); err != nil {
			return memory.Value{}, err
		}
	}
	return memory.RefValue(array), nil
}

func builtinStringValues(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, _ []memory.Value) (memory.Value, error) {
	_, err := requireString(execution.context, this)
	if err != nil {
		return memory.Value{}, err
	}
	iterator, err := execution.context.NewIterator(this.Ref(), memory.IteratorStringValues)
	return memory.RefValue(iterator), err
}

func builtinIteratorNext(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, _ []memory.Value) (memory.Value, error) {
	iterator, err := requireKind(execution.context, this, memory.HeapIterator, "Iterator receiver")
	if err != nil {
		return memory.Value{}, err
	}
	step, err := execution.context.AdvanceIterator(iterator)
	if err != nil {
		return memory.Value{}, err
	}
	value := memory.UndefinedValue()
	if !step.Done {
		value = step.Value
		if step.Textual {
			ref, err := execution.context.NewString(step.Text)
			if err != nil {
				return memory.Value{}, err
			}
			value = memory.RefValue(ref)
		} else if step.Pair {
			pair, err := execution.context.NewArray(2)
			if err != nil {
				return memory.Value{}, err
			}
			if err := execution.context.SetArrayElement(pair, 0, step.Key); err != nil {
				return memory.Value{}, err
			}
			if err := execution.context.SetArrayElement(pair, 1, step.Value); err != nil {
				return memory.Value{}, err
			}
			value = memory.RefValue(pair)
		}
	}
	result, err := execution.context.NewHeapObject()
	if err != nil {
		return memory.Value{}, err
	}
	if err := defineData(execution.context, result, "value", value, true, true, true); err != nil {
		return memory.Value{}, err
	}
	if err := defineData(execution.context, result, "done", memory.BoolValue(step.Done), true, true, true); err != nil {
		return memory.Value{}, err
	}
	return memory.RefValue(result), nil
}

func builtinArrayMap(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	array, err := requireArray(execution.context, this)
	if err != nil {
		return memory.Value{}, err
	}
	callback, err := requireCallable(execution.context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	snapshot, err := execution.context.DerefArray(array)
	if err != nil {
		return memory.Value{}, err
	}
	result, err := execution.context.NewArray(snapshot.Length)
	if err != nil {
		return memory.Value{}, err
	}
	thisArg := argument(arguments, 1)
	for _, element := range snapshot.Elements {
		mapped, err := execution.call(callback, thisArg, []memory.Value{element.Value, memory.NumberValue(float64(element.Index)), this}, callAny)
		if err != nil {
			return memory.Value{}, err
		}
		if err := execution.context.SetArrayElement(result, element.Index, mapped); err != nil {
			return memory.Value{}, err
		}
	}
	return memory.RefValue(result), nil
}

func builtinArrayFilter(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	array, err := requireArray(execution.context, this)
	if err != nil {
		return memory.Value{}, err
	}
	callback, err := requireCallable(execution.context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	snapshot, err := execution.context.DerefArray(array)
	if err != nil {
		return memory.Value{}, err
	}
	result, err := execution.context.NewArray(0)
	if err != nil {
		return memory.Value{}, err
	}
	thisArg := argument(arguments, 1)
	next := uint32(0)
	for _, element := range snapshot.Elements {
		selected, err := execution.call(callback, thisArg, []memory.Value{element.Value, memory.NumberValue(float64(element.Index)), this}, callAny)
		if err != nil {
			return memory.Value{}, err
		}
		truthy, err := valueTruthy(execution.context, selected)
		if err != nil {
			return memory.Value{}, err
		}
		if truthy {
			if err := execution.context.SetArrayElement(result, next, element.Value); err != nil {
				return memory.Value{}, err
			}
			next++
		}
	}
	return memory.RefValue(result), nil
}

func builtinArrayForEach(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	array, err := requireArray(execution.context, this)
	if err != nil {
		return memory.Value{}, err
	}
	callback, err := requireCallable(execution.context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	snapshot, err := execution.context.DerefArray(array)
	if err != nil {
		return memory.Value{}, err
	}
	thisArg := argument(arguments, 1)
	for _, element := range snapshot.Elements {
		if _, err := execution.call(callback, thisArg, []memory.Value{element.Value, memory.NumberValue(float64(element.Index)), this}, callAny); err != nil {
			return memory.Value{}, err
		}
	}
	return memory.UndefinedValue(), nil
}

func builtinArrayIncludes(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	array, err := requireArray(execution.context, this)
	if err != nil {
		return memory.Value{}, err
	}
	snapshot, err := execution.context.DerefArray(array)
	if err != nil {
		return memory.Value{}, err
	}
	start, err := integerArgument(execution, arguments, 1, 0)
	if err != nil {
		return memory.Value{}, err
	}
	if start < 0 {
		start += int64(snapshot.Length)
		if start < 0 {
			start = 0
		}
	}
	if start >= int64(snapshot.Length) {
		return memory.BoolValue(false), nil
	}
	needle := argument(arguments, 0)
	for index := uint32(start); index < snapshot.Length; index++ {
		value, found, err := execution.context.ArrayElement(array, index)
		if err != nil {
			return memory.Value{}, err
		}
		if !found {
			value = memory.UndefinedValue()
		}
		equal, err := sameValueZero(execution.context, value, needle)
		if err != nil {
			return memory.Value{}, err
		}
		if equal {
			return memory.BoolValue(true), nil
		}
	}
	return memory.BoolValue(false), nil
}

func builtinArrayIndexOf(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	array, err := requireArray(execution.context, this)
	if err != nil {
		return memory.Value{}, err
	}
	snapshot, err := execution.context.DerefArray(array)
	if err != nil {
		return memory.Value{}, err
	}
	needle := argument(arguments, 0)
	start, err := integerArgument(execution, arguments, 1, 0)
	if err != nil {
		return memory.Value{}, err
	}
	if start < 0 {
		start += int64(snapshot.Length)
		if start < 0 {
			start = 0
		}
	}
	if start >= int64(snapshot.Length) {
		return memory.NumberValue(-1), nil
	}
	for _, element := range snapshot.Elements {
		if int64(element.Index) < start {
			continue
		}
		equal, err := strictEqual(execution.context, element.Value, needle)
		if err != nil {
			return memory.Value{}, err
		}
		if equal {
			return memory.NumberValue(float64(element.Index)), nil
		}
	}
	return memory.NumberValue(-1), nil
}

func builtinArrayKeys(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, _ []memory.Value) (memory.Value, error) {
	return newIterator(execution, this, memory.HeapArray, memory.IteratorArrayKeys)
}
func builtinArrayValues(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, _ []memory.Value) (memory.Value, error) {
	return newIterator(execution, this, memory.HeapArray, memory.IteratorArrayValues)
}
func builtinArrayEntries(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, _ []memory.Value) (memory.Value, error) {
	return newIterator(execution, this, memory.HeapArray, memory.IteratorArrayEntries)
}

func builtinMapConstructor(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	result, err := execution.context.NewMap()
	if err != nil {
		return memory.Value{}, err
	}
	if len(arguments) != 0 && arguments[0].Kind() != memory.ValueUndefined && arguments[0].Kind() != memory.ValueNull {
		iterable, err := requireArray(execution.context, arguments[0])
		if err != nil {
			return memory.Value{}, err
		}
		snapshot, err := execution.context.DerefArray(iterable)
		if err != nil {
			return memory.Value{}, err
		}
		for _, element := range snapshot.Elements {
			entry, err := requireArray(execution.context, element.Value)
			if err != nil {
				return memory.Value{}, err
			}
			key, keyFound, err := execution.context.ArrayElement(entry, 0)
			if err != nil {
				return memory.Value{}, err
			}
			if !keyFound {
				key = memory.UndefinedValue()
			}
			value, valueFound, err := execution.context.ArrayElement(entry, 1)
			if err != nil {
				return memory.Value{}, err
			}
			if !valueFound {
				value = memory.UndefinedValue()
			}
			if err := execution.context.MapSet(result, key, value); err != nil {
				return memory.Value{}, err
			}
		}
	}
	return memory.RefValue(result), nil
}

func builtinMapGet(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	ref, err := requireKind(execution.context, this, memory.HeapMap, "Map receiver")
	if err != nil {
		return memory.Value{}, err
	}
	value, found, err := execution.context.MapGet(ref, argument(arguments, 0))
	if err != nil || !found {
		return memory.UndefinedValue(), err
	}
	return value, nil
}

func builtinMapSet(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	ref, err := requireKind(execution.context, this, memory.HeapMap, "Map receiver")
	if err != nil {
		return memory.Value{}, err
	}
	if err := execution.context.MapSet(ref, argument(arguments, 0), argument(arguments, 1)); err != nil {
		return memory.Value{}, err
	}
	return this, nil
}

func builtinMapHas(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	ref, err := requireKind(execution.context, this, memory.HeapMap, "Map receiver")
	if err != nil {
		return memory.Value{}, err
	}
	_, found, err := execution.context.MapGet(ref, argument(arguments, 0))
	return memory.BoolValue(found), err
}

func builtinMapDelete(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	ref, err := requireKind(execution.context, this, memory.HeapMap, "Map receiver")
	if err != nil {
		return memory.Value{}, err
	}
	deleted, err := execution.context.MapDelete(ref, argument(arguments, 0))
	return memory.BoolValue(deleted), err
}

func builtinMapClear(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, _ []memory.Value) (memory.Value, error) {
	ref, err := requireKind(execution.context, this, memory.HeapMap, "Map receiver")
	if err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), execution.context.MapClear(ref)
}

func builtinMapSize(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, _ []memory.Value) (memory.Value, error) {
	ref, err := requireKind(execution.context, this, memory.HeapMap, "Map receiver")
	if err != nil {
		return memory.Value{}, err
	}
	value, err := execution.context.DerefMap(ref)
	return memory.NumberValue(float64(len(value.Entries))), err
}

func builtinMapKeys(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, _ []memory.Value) (memory.Value, error) {
	return newIterator(execution, this, memory.HeapMap, memory.IteratorMapKeys)
}
func builtinMapValues(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, _ []memory.Value) (memory.Value, error) {
	return newIterator(execution, this, memory.HeapMap, memory.IteratorMapValues)
}
func builtinMapEntries(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, _ []memory.Value) (memory.Value, error) {
	return newIterator(execution, this, memory.HeapMap, memory.IteratorMapEntries)
}

func builtinMapForEach(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	ref, err := requireKind(execution.context, this, memory.HeapMap, "Map receiver")
	if err != nil {
		return memory.Value{}, err
	}
	callback, err := requireCallable(execution.context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	value, err := execution.context.DerefMap(ref)
	if err != nil {
		return memory.Value{}, err
	}
	thisArg := argument(arguments, 1)
	for _, entry := range value.Entries {
		if _, err := execution.call(callback, thisArg, []memory.Value{entry.Value, entry.Key, this}, callAny); err != nil {
			return memory.Value{}, err
		}
	}
	return memory.UndefinedValue(), nil
}

func builtinSetConstructor(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	result, err := execution.context.NewSet()
	if err != nil {
		return memory.Value{}, err
	}
	if len(arguments) != 0 && arguments[0].Kind() != memory.ValueUndefined && arguments[0].Kind() != memory.ValueNull {
		iterable, err := requireArray(execution.context, arguments[0])
		if err != nil {
			return memory.Value{}, err
		}
		snapshot, err := execution.context.DerefArray(iterable)
		if err != nil {
			return memory.Value{}, err
		}
		for index := uint32(0); index < snapshot.Length; index++ {
			value, found, err := execution.context.ArrayElement(iterable, index)
			if err != nil {
				return memory.Value{}, err
			}
			if !found {
				value = memory.UndefinedValue()
			}
			if err := execution.context.SetAdd(result, value); err != nil {
				return memory.Value{}, err
			}
		}
	}
	return memory.RefValue(result), nil
}

func builtinSetAdd(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	ref, err := requireKind(execution.context, this, memory.HeapSet, "Set receiver")
	if err != nil {
		return memory.Value{}, err
	}
	if err := execution.context.SetAdd(ref, argument(arguments, 0)); err != nil {
		return memory.Value{}, err
	}
	return this, nil
}

func builtinSetHas(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	ref, err := requireKind(execution.context, this, memory.HeapSet, "Set receiver")
	if err != nil {
		return memory.Value{}, err
	}
	found, err := execution.context.SetHas(ref, argument(arguments, 0))
	return memory.BoolValue(found), err
}

func builtinSetDelete(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	ref, err := requireKind(execution.context, this, memory.HeapSet, "Set receiver")
	if err != nil {
		return memory.Value{}, err
	}
	deleted, err := execution.context.SetDelete(ref, argument(arguments, 0))
	return memory.BoolValue(deleted), err
}

func builtinSetClear(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, _ []memory.Value) (memory.Value, error) {
	ref, err := requireKind(execution.context, this, memory.HeapSet, "Set receiver")
	if err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), execution.context.SetClear(ref)
}

func builtinSetSize(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, _ []memory.Value) (memory.Value, error) {
	ref, err := requireKind(execution.context, this, memory.HeapSet, "Set receiver")
	if err != nil {
		return memory.Value{}, err
	}
	value, err := execution.context.DerefSet(ref)
	return memory.NumberValue(float64(len(value.Values))), err
}

func builtinSetValues(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, _ []memory.Value) (memory.Value, error) {
	return newIterator(execution, this, memory.HeapSet, memory.IteratorSetValues)
}
func builtinSetEntries(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, _ []memory.Value) (memory.Value, error) {
	return newIterator(execution, this, memory.HeapSet, memory.IteratorSetEntries)
}

func builtinSetForEach(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	ref, err := requireKind(execution.context, this, memory.HeapSet, "Set receiver")
	if err != nil {
		return memory.Value{}, err
	}
	callback, err := requireCallable(execution.context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	value, err := execution.context.DerefSet(ref)
	if err != nil {
		return memory.Value{}, err
	}
	thisArg := argument(arguments, 1)
	for _, member := range value.Values {
		if _, err := execution.call(callback, thisArg, []memory.Value{member, member, this}, callAny); err != nil {
			return memory.Value{}, err
		}
	}
	return memory.UndefinedValue(), nil
}

func newIterator(execution *execution, this memory.Value, targetKind memory.HeapKind, iteratorKind memory.IteratorKind) (memory.Value, error) {
	target, err := requireKind(execution.context, this, targetKind, "iterator receiver")
	if err != nil {
		return memory.Value{}, err
	}
	iterator, err := execution.context.NewIterator(target, iteratorKind)
	return memory.RefValue(iterator), err
}

func requireKind(context *TaskContext, value memory.Value, kind memory.HeapKind, label string) (memory.Ref, error) {
	ref, err := requireRef(value, label)
	if err != nil {
		return memory.Ref{}, err
	}
	actual, err := context.HeapKind(ref)
	if err != nil {
		return memory.Ref{}, err
	}
	if actual != kind {
		return memory.Ref{}, fmt.Errorf("%w: %s must be %s", ErrOperandType, label, kind)
	}
	return ref, nil
}

func requireCallable(context *TaskContext, value memory.Value) (memory.Ref, error) {
	return requireKind(context, value, memory.HeapFunction, "callback")
}

func requireString(context *TaskContext, value memory.Value) (string, error) {
	ref, err := requireKind(context, value, memory.HeapString, "String receiver")
	if err != nil {
		return "", err
	}
	return context.DerefString(ref)
}

func integerArgument(execution *execution, arguments []memory.Value, index int, fallback int64) (int64, error) {
	if index >= len(arguments) || arguments[index].Kind() == memory.ValueUndefined {
		return fallback, nil
	}
	number, err := execution.toNumber(arguments[index])
	if err != nil {
		return 0, err
	}
	if math.IsNaN(number) || number == 0 {
		return 0, nil
	}
	if math.IsInf(number, 1) {
		return math.MaxInt64, nil
	}
	if math.IsInf(number, -1) {
		return math.MinInt64, nil
	}
	return int64(math.Trunc(number)), nil
}

func relativeRuneIndex(execution *execution, value memory.Value, length, fallback int) (int, error) {
	if value.Kind() == memory.ValueUndefined {
		return fallback, nil
	}
	number, err := execution.toNumber(value)
	if err != nil {
		return 0, err
	}
	if math.IsNaN(number) {
		return 0, nil
	}
	index := int(math.Trunc(number))
	if index < 0 {
		index += length
		if index < 0 {
			return 0, nil
		}
	}
	if index > length {
		return length, nil
	}
	return index, nil
}

func sameValueZero(context *TaskContext, left, right memory.Value) (bool, error) {
	if left.Kind() == memory.ValueNumber && right.Kind() == memory.ValueNumber && math.IsNaN(left.Number()) && math.IsNaN(right.Number()) {
		return true, nil
	}
	return strictEqual(context, left, right)
}
