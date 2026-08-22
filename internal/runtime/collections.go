package runtime

import (
	"fmt"
	"math"
	"strconv"
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
		{"endsWith", 1, nativeStringEndsWith},
		{"startsWith", 1, nativeStringStartsWith}, {"replace", 2, nativeStringReplace},
		{"padStart", 1, nativeStringPadStart}, {"padEnd", 1, nativeStringPadEnd},
		{"match", 1, nativeStringMatch},
		{"at", 1, nativeStringAt},
		{"indexOf", 1, nativeStringIndexOf}, {"slice", 2, nativeStringSlice},
		{"substring", 2, nativeStringSubstring},
		{"lastIndexOf", 1, nativeStringLastIndexOf},
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
	setValuesName, err := context.NewString("values")
	if err != nil {
		return err
	}
	setValues, found, err := context.GetOwnProperty(intrinsics.SetPrototype, setValuesName)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("runtime: missing Set values iterator method")
	}
	if err := defineData(context, intrinsics.SetPrototype, "keys", setValues, true, false, true); err != nil {
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
		if arguments[0].IsRef() {
			kind, kindErr := execution.context.HeapKind(arguments[0].Ref())
			if kindErr != nil {
				return memory.Value{}, kindErr
			}
			if kind == memory.HeapSymbol {
				text, err = symbolDescriptiveString(execution.context, arguments[0])
			} else {
				text, err = execution.toString(arguments[0])
			}
		} else {
			text, err = execution.toString(arguments[0])
		}
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

func builtinStringAt(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	text, err := requireString(execution.context, this)
	if err != nil {
		return memory.Value{}, err
	}
	index, err := integerArgument(execution, arguments, 0, 0)
	if err != nil {
		return memory.Value{}, err
	}
	runes := []rune(text)
	if index < 0 {
		index += int64(len(runes))
	}
	if index < 0 || index >= int64(len(runes)) {
		return memory.UndefinedValue(), nil
	}
	return newStringValue(execution.context, string(runes[index]))
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

func builtinStringEndsWith(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	text, err := requireString(execution.context, this)
	if err != nil {
		return memory.Value{}, err
	}
	search := argument(arguments, 0)
	if search.IsRef() {
		kind, kindErr := execution.context.HeapKind(search.Ref())
		if kindErr != nil {
			return memory.Value{}, kindErr
		}
		if kind == memory.HeapRegExp {
			return memory.Value{}, fmt.Errorf("%w: String.prototype.endsWith search value cannot be a RegExp", ErrOperandType)
		}
	}
	needle, err := execution.toString(search)
	if err != nil {
		return memory.Value{}, err
	}
	runes := []rune(text)
	end, err := integerArgument(execution, arguments, 1, int64(len(runes)))
	if err != nil {
		return memory.Value{}, err
	}
	if end < 0 {
		end = 0
	}
	if end > int64(len(runes)) {
		end = int64(len(runes))
	}
	return memory.BoolValue(strings.HasSuffix(string(runes[:end]), needle)), nil
}

func builtinStringStartsWith(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
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
		start = int64(len(runes))
	}
	return memory.BoolValue(strings.HasPrefix(string(runes[start:]), needle)), nil
}

func builtinStringPadStart(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	return builtinStringPad(execution, this, arguments, true)
}

func builtinStringPadEnd(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	return builtinStringPad(execution, this, arguments, false)
}

func builtinStringPad(execution *execution, this memory.Value, arguments []memory.Value, start bool) (memory.Value, error) {
	text, err := requireString(execution.context, this)
	if err != nil {
		return memory.Value{}, err
	}
	target, err := integerArgument(execution, arguments, 0, 0)
	if err != nil {
		return memory.Value{}, err
	}
	runes := []rune(text)
	if target <= int64(len(runes)) || target <= 0 {
		return newStringValue(execution.context, text)
	}
	filler := " "
	if len(arguments) > 1 && arguments[1].Kind() != memory.ValueUndefined {
		filler, err = execution.toString(arguments[1])
		if err != nil {
			return memory.Value{}, err
		}
	}
	fillerRunes := []rune(filler)
	if len(fillerRunes) == 0 {
		return newStringValue(execution.context, text)
	}
	needed := int(target) - len(runes)
	padding := make([]rune, needed)
	for index := range padding {
		padding[index] = fillerRunes[index%len(fillerRunes)]
	}
	if start {
		return newStringValue(execution.context, string(padding)+text)
	}
	return newStringValue(execution.context, text+string(padding))
}

func builtinStringReplace(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	text, err := requireString(execution.context, this)
	if err != nil {
		return memory.Value{}, err
	}
	search := argument(arguments, 0)
	replacement := argument(arguments, 1)
	var matches [][]int
	global := false
	if search.IsRef() {
		kind, kindErr := execution.context.HeapKind(search.Ref())
		if kindErr != nil {
			return memory.Value{}, kindErr
		}
		if kind == memory.HeapRegExp {
			_, expression, expressionErr := requireRegExp(execution.context, search)
			if expressionErr != nil {
				return memory.Value{}, expressionErr
			}
			compiled, compileErr := compileRegExp(execution.context, expression)
			if compileErr != nil {
				return memory.Value{}, compileErr
			}
			global = expression.Flags&memory.RegExpGlobal != 0
			if global {
				matches = compiled.FindAllStringSubmatchIndex(text, -1)
			} else if match := compiled.FindStringSubmatchIndex(text); match != nil {
				matches = [][]int{match}
			}
		}
	}
	if matches == nil {
		needle, stringErr := execution.toString(search)
		if stringErr != nil {
			return memory.Value{}, stringErr
		}
		if index := strings.Index(text, needle); index >= 0 {
			matches = [][]int{{index, index + len(needle)}}
		}
	}
	if len(matches) == 0 {
		return newStringValue(execution.context, text)
	}
	callable := memory.Ref{}
	if replacement.IsRef() {
		if candidate, callableErr := requireCallable(execution.context, replacement); callableErr == nil {
			callable = candidate
		}
	}
	replacementText := ""
	if callable == (memory.Ref{}) {
		replacementText, err = execution.toString(replacement)
		if err != nil {
			return memory.Value{}, err
		}
	}
	var result strings.Builder
	previous := 0
	for _, match := range matches {
		result.WriteString(text[previous:match[0]])
		if callable != (memory.Ref{}) {
			callbackArguments := make([]memory.Value, 0, len(match)/2+2)
			for index := 0; index < len(match); index += 2 {
				value := memory.UndefinedValue()
				if match[index] >= 0 {
					value, err = newStringValue(execution.context, text[match[index]:match[index+1]])
					if err != nil {
						return memory.Value{}, err
					}
				}
				callbackArguments = append(callbackArguments, value)
			}
			callbackArguments = append(callbackArguments, memory.NumberValue(float64(match[0])), this)
			value, callErr := execution.call(callable, memory.UndefinedValue(), callbackArguments, callAny)
			if callErr != nil {
				return memory.Value{}, callErr
			}
			converted, stringErr := execution.toString(value)
			if stringErr != nil {
				return memory.Value{}, stringErr
			}
			result.WriteString(converted)
		} else {
			result.WriteString(expandStringReplacement(replacementText, text, match))
		}
		previous = match[1]
		if !global {
			break
		}
	}
	result.WriteString(text[previous:])
	return newStringValue(execution.context, result.String())
}

func builtinStringMatch(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	text, err := requireString(execution.context, this)
	if err != nil {
		return memory.Value{}, err
	}
	search := argument(arguments, 0)
	var expressionRef memory.Ref
	var expression memory.RegExp
	if search.IsRef() {
		kind, kindErr := execution.context.HeapKind(search.Ref())
		if kindErr != nil {
			return memory.Value{}, kindErr
		}
		if kind == memory.HeapRegExp {
			expressionRef, expression, err = requireRegExp(execution.context, search)
			if err != nil {
				return memory.Value{}, err
			}
		}
	}
	if expressionRef == (memory.Ref{}) {
		pattern, stringErr := execution.toString(search)
		if stringErr != nil {
			return memory.Value{}, stringErr
		}
		patternRef, allocationErr := execution.context.NewString(pattern)
		if allocationErr != nil {
			return memory.Value{}, allocationErr
		}
		expressionRef, err = execution.context.NewRegExp(patternRef, "")
		if err != nil {
			return memory.Value{}, err
		}
		expression, err = execution.context.DerefRegExp(expressionRef)
		if err != nil {
			return memory.Value{}, err
		}
	}
	if expression.Flags&memory.RegExpGlobal == 0 {
		return regexpExec(execution, expressionRef, expression, text)
	}
	compiled, err := compileRegExp(execution.context, expression)
	if err != nil {
		return memory.Value{}, err
	}
	matches := compiled.FindAllStringIndex(text, -1)
	if err := execution.context.SetRegExpLastIndex(expressionRef, 0); err != nil {
		return memory.Value{}, err
	}
	if len(matches) == 0 {
		return memory.NullValue(), nil
	}
	result, err := execution.context.NewArray(uint32(len(matches)))
	if err != nil {
		return memory.Value{}, err
	}
	for index, match := range matches {
		value, err := newStringValue(execution.context, text[match[0]:match[1]])
		if err != nil {
			return memory.Value{}, err
		}
		if err := execution.context.SetArrayElement(result, uint32(index), value); err != nil {
			return memory.Value{}, err
		}
	}
	return memory.RefValue(result), nil
}

func expandStringReplacement(replacement, source string, match []int) string {
	var result strings.Builder
	for index := 0; index < len(replacement); index++ {
		if replacement[index] != '$' || index+1 >= len(replacement) {
			result.WriteByte(replacement[index])
			continue
		}
		next := replacement[index+1]
		switch next {
		case '$':
			result.WriteByte('$')
			index++
		case '&':
			result.WriteString(source[match[0]:match[1]])
			index++
		case '`':
			result.WriteString(source[:match[0]])
			index++
		case '\'':
			result.WriteString(source[match[1]:])
			index++
		default:
			if next < '1' || next > '9' {
				result.WriteByte('$')
				continue
			}
			capture := int(next - '0')
			consumed := 1
			captureCount := len(match)/2 - 1
			if index+2 < len(replacement) && replacement[index+2] >= '0' && replacement[index+2] <= '9' {
				candidate := capture*10 + int(replacement[index+2]-'0')
				if candidate <= captureCount {
					capture = candidate
					consumed = 2
				}
			}
			if capture > captureCount {
				result.WriteByte('$')
				continue
			}
			// An unmatched but syntactically present capture expands to the
			// empty string. It must still consume the $n token.
			if match[capture*2] >= 0 {
				result.WriteString(source[match[capture*2]:match[capture*2+1]])
			}
			index += consumed
		}
	}
	return result.String()
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

func builtinStringLastIndexOf(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	text, err := requireString(execution.context, this)
	if err != nil {
		return memory.Value{}, err
	}
	needle, err := execution.toString(argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	haystackRunes := []rune(text)
	needleRunes := []rune(needle)
	position, err := integerArgument(execution, arguments, 1, int64(len(haystackRunes)))
	if err != nil {
		return memory.Value{}, err
	}
	position = min(max(position, 0), int64(len(haystackRunes)))
	if len(needleRunes) == 0 {
		return memory.NumberValue(float64(position)), nil
	}
	start := min(int(position), len(haystackRunes)-len(needleRunes))
	for index := start; index >= 0; index-- {
		matched := true
		for offset := range needleRunes {
			if haystackRunes[index+offset] != needleRunes[offset] {
				matched = false
				break
			}
		}
		if matched {
			return memory.NumberValue(float64(index)), nil
		}
	}
	return memory.NumberValue(-1), nil
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

func builtinStringSubstring(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	text, err := requireString(execution.context, this)
	if err != nil {
		return memory.Value{}, err
	}
	runes := []rune(text)
	start, err := integerArgument(execution, arguments, 0, 0)
	if err != nil {
		return memory.Value{}, err
	}
	end, err := integerArgument(execution, arguments, 1, int64(len(runes)))
	if err != nil {
		return memory.Value{}, err
	}
	start = min(max(start, 0), int64(len(runes)))
	end = min(max(end, 0), int64(len(runes)))
	if start > end {
		start, end = end, start
	}
	return newStringValue(execution.context, string(runes[start:end]))
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
	array, err := execution.arrayReceiver(this)
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

func builtinArrayAt(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	array, err := execution.arrayReceiver(this)
	if err != nil {
		return memory.Value{}, err
	}
	snapshot, err := execution.context.DerefArray(array)
	if err != nil {
		return memory.Value{}, err
	}
	index, err := integerArgument(execution, arguments, 0, 0)
	if err != nil {
		return memory.Value{}, err
	}
	if index < 0 {
		index += int64(snapshot.Length)
	}
	if index < 0 || index >= int64(snapshot.Length) {
		return memory.UndefinedValue(), nil
	}
	key, err := execution.context.NewString(strconv.FormatInt(index, 10))
	if err != nil {
		return memory.Value{}, err
	}
	value, found, err := execution.getProperty(this, memory.RefValue(key))
	if err != nil || found {
		return value, err
	}
	return memory.UndefinedValue(), nil
}

func builtinArraySort(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	array, err := execution.arrayReceiver(this)
	if err != nil {
		return memory.Value{}, err
	}
	snapshot, err := execution.context.DerefArray(array)
	if err != nil {
		return memory.Value{}, err
	}

	var compare memory.Ref
	if candidate := argument(arguments, 0); candidate.Kind() != memory.ValueUndefined {
		compare, err = requireCallable(execution.context, candidate)
		if err != nil {
			return memory.Value{}, err
		}
	}

	values := make([]memory.Value, len(snapshot.Elements))
	for index, element := range snapshot.Elements {
		values[index] = element.Value
	}
	less := func(left, right memory.Value) (bool, error) {
		// Array.prototype.sort always places undefined values after every
		// defined value and does not invoke compareFn for them.
		if left.Kind() == memory.ValueUndefined || right.Kind() == memory.ValueUndefined {
			return left.Kind() != memory.ValueUndefined && right.Kind() == memory.ValueUndefined, nil
		}
		if compare != (memory.Ref{}) {
			result, callErr := execution.call(compare, memory.UndefinedValue(), []memory.Value{left, right}, callAny)
			if callErr != nil {
				return false, callErr
			}
			number, numberErr := execution.toNumber(result)
			return numberErr == nil && number < 0, numberErr
		}
		leftText, leftErr := execution.toString(left)
		if leftErr != nil {
			return false, leftErr
		}
		rightText, rightErr := execution.toString(right)
		return leftText < rightText, rightErr
	}
	if err := stableSortValues(values, less); err != nil {
		return memory.Value{}, err
	}

	for index, value := range values {
		key, keyErr := execution.context.NewString(strconv.Itoa(index))
		if keyErr != nil {
			return memory.Value{}, keyErr
		}
		if err := execution.setPropertyValue(this, memory.RefValue(key), value); err != nil {
			return memory.Value{}, err
		}
	}
	// Holes sort after present values. Delete any former elements which now
	// occupy the sparse tail while preserving the original array length.
	for index := uint32(len(values)); index < snapshot.Length; index++ {
		key, keyErr := execution.context.NewString(strconv.FormatUint(uint64(index), 10))
		if keyErr != nil {
			return memory.Value{}, keyErr
		}
		if _, err := execution.deletePropertyValue(this, memory.RefValue(key)); err != nil {
			return memory.Value{}, err
		}
	}
	return this, nil
}

func builtinArrayFrom(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	source := argument(arguments, 0)
	if source.Kind() == memory.ValueUndefined || source.Kind() == memory.ValueNull {
		return memory.Value{}, fmt.Errorf("%w: Array.from source is null or undefined", ErrOperandType)
	}
	var mapper memory.Ref
	if candidate := argument(arguments, 1); candidate.Kind() != memory.ValueUndefined {
		var err error
		mapper, err = requireCallable(execution.context, candidate)
		if err != nil {
			return memory.Value{}, err
		}
	}
	thisArg := argument(arguments, 2)
	result, err := execution.context.NewArray(0)
	if err != nil {
		return memory.Value{}, err
	}
	appendValue := func(value memory.Value, index uint32) error {
		if mapper != (memory.Ref{}) {
			value, err = execution.call(mapper, thisArg, []memory.Value{value, memory.NumberValue(float64(index))}, callAny)
			if err != nil {
				return err
			}
		}
		return execution.context.SetArrayElement(result, index, value)
	}

	iteratorMethod, iterable, err := execution.getProperty(source, memory.RefValue(execution.context.intrinsics.SymbolIterator))
	if err != nil {
		return memory.Value{}, err
	}
	iterable = iterable && iteratorMethod.Kind() != memory.ValueUndefined && iteratorMethod.Kind() != memory.ValueNull
	if iterable {
		iterator, next, err := execution.getIteratorRecord(source)
		if err != nil {
			return memory.Value{}, err
		}
		doneName, err := execution.context.NewString("done")
		if err != nil {
			return memory.Value{}, err
		}
		valueName, err := execution.context.NewString("value")
		if err != nil {
			return memory.Value{}, err
		}
		for index := uint32(0); ; index++ {
			step, err := execution.iteratorNext(iterator, next)
			if err != nil {
				return memory.Value{}, err
			}
			done, found, err := execution.getProperty(step, memory.RefValue(doneName))
			if err != nil {
				return memory.Value{}, err
			}
			if !found {
				done = memory.UndefinedValue()
			}
			finished, err := valueTruthy(execution.context, done)
			if err != nil {
				return memory.Value{}, err
			}
			if finished {
				return memory.RefValue(result), nil
			}
			value, found, err := execution.getProperty(step, memory.RefValue(valueName))
			if err != nil {
				return memory.Value{}, err
			}
			if !found {
				value = memory.UndefinedValue()
			}
			if err := appendValue(value, index); err != nil {
				_ = execution.closeIterator(iterator)
				return memory.Value{}, err
			}
			if index == math.MaxUint32-1 {
				_ = execution.closeIterator(iterator)
				return memory.Value{}, fmt.Errorf("%w: Array.from result exceeds uint32 length", memory.ErrInvalidIndex)
			}
		}
	}

	lengthName, err := execution.context.NewString("length")
	if err != nil {
		return memory.Value{}, err
	}
	lengthValue, found, err := execution.getProperty(source, memory.RefValue(lengthName))
	if err != nil {
		return memory.Value{}, err
	}
	if !found {
		lengthValue = memory.UndefinedValue()
	}
	lengthNumber, err := execution.toNumber(lengthValue)
	if err != nil {
		return memory.Value{}, err
	}
	length := uint32(0)
	if !math.IsNaN(lengthNumber) && lengthNumber > 0 {
		if math.IsInf(lengthNumber, 1) || lengthNumber > math.MaxUint32 {
			return memory.Value{}, fmt.Errorf("%w: Array.from length exceeds uint32", memory.ErrInvalidIndex)
		}
		length = uint32(math.Trunc(lengthNumber))
	}
	if err := execution.context.SetArrayLength(result, length); err != nil {
		return memory.Value{}, err
	}
	for index := uint32(0); index < length; index++ {
		key, err := execution.context.NewString(strconv.FormatUint(uint64(index), 10))
		if err != nil {
			return memory.Value{}, err
		}
		value, found, err := execution.getProperty(source, memory.RefValue(key))
		if err != nil {
			return memory.Value{}, err
		}
		if !found {
			value = memory.UndefinedValue()
		}
		if err := appendValue(value, index); err != nil {
			return memory.Value{}, err
		}
	}
	return memory.RefValue(result), nil
}

func stableSortValues(values []memory.Value, less func(memory.Value, memory.Value) (bool, error)) error {
	if len(values) < 2 {
		return nil
	}
	buffer := make([]memory.Value, len(values))
	for width := 1; width < len(values); width *= 2 {
		for start := 0; start < len(values); start += 2 * width {
			middle := min(start+width, len(values))
			end := min(start+2*width, len(values))
			left, right, output := start, middle, start
			for left < middle && right < end {
				rightBeforeLeft, err := less(values[right], values[left])
				if err != nil {
					return err
				}
				if rightBeforeLeft {
					buffer[output] = values[right]
					right++
				} else {
					buffer[output] = values[left]
					left++
				}
				output++
			}
			output += copy(buffer[output:end], values[left:middle])
			copy(buffer[output:end], values[right:end])
		}
		copy(values, buffer)
	}
	return nil
}

func builtinArrayFilter(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	array, err := execution.arrayReceiver(this)
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
	array, err := execution.arrayReceiver(this)
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

func builtinArrayReduce(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	array, err := execution.arrayReceiver(this)
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
	start := 0
	accumulator := memory.UndefinedValue()
	if len(arguments) > 1 {
		accumulator = arguments[1]
	} else {
		if len(snapshot.Elements) == 0 {
			return memory.Value{}, fmt.Errorf("%w: reduce of empty Array with no initial value", ErrOperandType)
		}
		accumulator = snapshot.Elements[0].Value
		start = 1
	}
	for _, element := range snapshot.Elements[start:] {
		accumulator, err = execution.call(callback, memory.UndefinedValue(), []memory.Value{
			accumulator, element.Value, memory.NumberValue(float64(element.Index)), this,
		}, callAny)
		if err != nil {
			return memory.Value{}, err
		}
	}
	return accumulator, nil
}

func builtinArraySome(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	return execution.arrayPredicate(this, arguments, false, true)
}

func builtinArrayEvery(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	return execution.arrayPredicate(this, arguments, true, false)
}

func (execution *execution) arrayPredicate(this memory.Value, arguments []memory.Value, emptyResult, matchedResult bool) (memory.Value, error) {
	array, err := execution.arrayReceiver(this)
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
		selected, err := execution.call(callback, thisArg, []memory.Value{element.Value, memory.NumberValue(float64(element.Index)), this}, callAny)
		if err != nil {
			return memory.Value{}, err
		}
		truthy, err := valueTruthy(execution.context, selected)
		if err != nil {
			return memory.Value{}, err
		}
		if truthy == matchedResult {
			return memory.BoolValue(matchedResult), nil
		}
	}
	return memory.BoolValue(emptyResult), nil
}

func builtinArrayFind(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	array, err := execution.arrayReceiver(this)
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
	for index := uint32(0); index < snapshot.Length; index++ {
		value, found, err := execution.context.ArrayElement(array, index)
		if err != nil {
			return memory.Value{}, err
		}
		if !found {
			value = memory.UndefinedValue()
		}
		selected, err := execution.call(callback, thisArg, []memory.Value{value, memory.NumberValue(float64(index)), this}, callAny)
		if err != nil {
			return memory.Value{}, err
		}
		truthy, err := valueTruthy(execution.context, selected)
		if err != nil {
			return memory.Value{}, err
		}
		if truthy {
			return value, nil
		}
	}
	return memory.UndefinedValue(), nil
}

func builtinArrayFindIndex(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	array, err := execution.arrayReceiver(this)
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
	for index := uint32(0); index < snapshot.Length; index++ {
		key, err := execution.context.NewString(strconv.FormatUint(uint64(index), 10))
		if err != nil {
			return memory.Value{}, err
		}
		value, found, err := execution.getProperty(this, memory.RefValue(key))
		if err != nil {
			return memory.Value{}, err
		}
		if !found {
			value = memory.UndefinedValue()
		}
		selected, err := execution.call(callback, thisArg, []memory.Value{value, memory.NumberValue(float64(index)), this}, callAny)
		if err != nil {
			return memory.Value{}, err
		}
		truthy, err := valueTruthy(execution.context, selected)
		if err != nil {
			return memory.Value{}, err
		}
		if truthy {
			return memory.NumberValue(float64(index)), nil
		}
	}
	return memory.NumberValue(-1), nil
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
