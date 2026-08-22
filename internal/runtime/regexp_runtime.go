package runtime

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func (intrinsics *Intrinsics) installRegExpBuiltins(context *TaskContext) error {
	return installMethods(intrinsics, context, intrinsics.RegExpPrototype, []builtinMethod{
		{"test", 1, nativeRegExpTest},
		{"exec", 1, nativeRegExpExec},
		{"toString", 0, nativeRegExpToString},
	})
}

func builtinRegExpExec(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	expressionRef, expression, err := requireRegExp(execution.context, this)
	if err != nil {
		return memory.Value{}, err
	}
	text, err := execution.toString(argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	return regexpExec(execution, expressionRef, expression, text)
}

func regexpExec(execution *execution, expressionRef memory.Ref, expression memory.RegExp, text string) (memory.Value, error) {
	compiled, err := compileRegExp(execution.context, expression)
	if err != nil {
		return memory.Value{}, err
	}
	start := uint64(0)
	stateful := expression.Flags&(memory.RegExpGlobal|memory.RegExpSticky) != 0
	if stateful {
		start = expression.LastIndex
	}
	if start > uint64(len(text)) {
		if stateful {
			_ = execution.context.SetRegExpLastIndex(expressionRef, 0)
		}
		return memory.NullValue(), nil
	}
	match := compiled.FindStringSubmatchIndex(text[start:])
	if match == nil || expression.Flags&memory.RegExpSticky != 0 && match[0] != 0 {
		if stateful {
			_ = execution.context.SetRegExpLastIndex(expressionRef, 0)
		}
		return memory.NullValue(), nil
	}
	for index := range match {
		if match[index] >= 0 {
			match[index] += int(start)
		}
	}
	if stateful {
		if err := execution.context.SetRegExpLastIndex(expressionRef, uint64(match[1])); err != nil {
			return memory.Value{}, err
		}
	}
	result, err := execution.context.NewArray(uint32(len(match) / 2))
	if err != nil {
		return memory.Value{}, err
	}
	for index := 0; index < len(match); index += 2 {
		if match[index] < 0 {
			continue
		}
		value, err := newStringValue(execution.context, text[match[index]:match[index+1]])
		if err != nil {
			return memory.Value{}, err
		}
		if err := execution.context.SetArrayElement(result, uint32(index/2), value); err != nil {
			return memory.Value{}, err
		}
	}
	input, err := newStringValue(execution.context, text)
	if err != nil {
		return memory.Value{}, err
	}
	if err := defineData(execution.context, result, "index", memory.NumberValue(float64(match[0])), true, false, true); err != nil {
		return memory.Value{}, err
	}
	if err := defineData(execution.context, result, "input", input, true, false, true); err != nil {
		return memory.Value{}, err
	}
	return memory.RefValue(result), nil
}

func builtinRegExpConstructor(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	if len(arguments) != 0 && arguments[0].IsRef() && (len(arguments) < 2 || arguments[1].Kind() == memory.ValueUndefined) {
		kind, err := execution.context.HeapKind(arguments[0].Ref())
		if err != nil {
			return memory.Value{}, err
		}
		if kind == memory.HeapRegExp {
			return arguments[0], nil
		}
	}
	patternText := ""
	if len(arguments) != 0 && arguments[0].Kind() != memory.ValueUndefined {
		var err error
		patternText, err = execution.toString(arguments[0])
		if err != nil {
			return memory.Value{}, err
		}
	}
	flagsText := ""
	if len(arguments) > 1 && arguments[1].Kind() != memory.ValueUndefined {
		var err error
		flagsText, err = execution.toString(arguments[1])
		if err != nil {
			return memory.Value{}, err
		}
	}
	pattern, err := execution.context.NewString(patternText)
	if err != nil {
		return memory.Value{}, err
	}
	expression, err := execution.context.NewRegExp(pattern, flagsText)
	return memory.RefValue(expression), err
}

func builtinRegExpTest(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	expressionRef, expression, err := requireRegExp(execution.context, this)
	if err != nil {
		return memory.Value{}, err
	}
	text, err := execution.toString(argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	compiled, err := compileRegExp(execution.context, expression)
	if err != nil {
		return memory.Value{}, err
	}
	start := uint64(0)
	stateful := expression.Flags&(memory.RegExpGlobal|memory.RegExpSticky) != 0
	if stateful {
		start = expression.LastIndex
	}
	if start > uint64(len(text)) {
		if stateful {
			_ = execution.context.SetRegExpLastIndex(expressionRef, 0)
		}
		return memory.BoolValue(false), nil
	}
	location := compiled.FindStringIndex(text[start:])
	matched := location != nil && (expression.Flags&memory.RegExpSticky == 0 || location[0] == 0)
	if stateful {
		next := uint64(0)
		if matched {
			next = start + uint64(location[1])
		}
		if err := execution.context.SetRegExpLastIndex(expressionRef, next); err != nil {
			return memory.Value{}, err
		}
	}
	return memory.BoolValue(matched), nil
}

func builtinRegExpToString(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, _ []memory.Value) (memory.Value, error) {
	_, expression, err := requireRegExp(execution.context, this)
	if err != nil {
		return memory.Value{}, err
	}
	pattern, err := execution.context.DerefString(expression.Pattern)
	if err != nil {
		return memory.Value{}, err
	}
	return newStringValue(execution.context, "/"+pattern+"/"+expression.Flags.String())
}

func requireRegExp(context *TaskContext, value memory.Value) (memory.Ref, memory.RegExp, error) {
	if !value.IsRef() {
		return memory.Ref{}, memory.RegExp{}, ErrOperandType
	}
	kind, err := context.HeapKind(value.Ref())
	if err != nil {
		return memory.Ref{}, memory.RegExp{}, err
	}
	if kind != memory.HeapRegExp {
		return memory.Ref{}, memory.RegExp{}, ErrOperandType
	}
	expression, err := context.DerefRegExp(value.Ref())
	return value.Ref(), expression, err
}

func compileRegExp(context *TaskContext, expression memory.RegExp) (*regexp.Regexp, error) {
	pattern, err := context.DerefString(expression.Pattern)
	if err != nil {
		return nil, err
	}
	var modes strings.Builder
	if expression.Flags&memory.RegExpIgnoreCase != 0 {
		modes.WriteByte('i')
	}
	if expression.Flags&memory.RegExpMultiline != 0 {
		modes.WriteByte('m')
	}
	if expression.Flags&memory.RegExpDotAll != 0 {
		modes.WriteByte('s')
	}
	if modes.Len() != 0 {
		pattern = "(?" + modes.String() + ")" + translateJavaScriptRegExpPattern(pattern)
	} else {
		pattern = translateJavaScriptRegExpPattern(pattern)
	}
	compiled, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("%w: pattern %q: %v", memory.ErrInvalidRegExp, pattern, err)
	}
	return compiled, nil
}

func translateJavaScriptRegExpPattern(pattern string) string {
	var translated strings.Builder
	translated.Grow(len(pattern))
	for index := 0; index < len(pattern); {
		if pattern[index] != '\\' || index+1 >= len(pattern) {
			translated.WriteByte(pattern[index])
			index++
			continue
		}
		if pattern[index+1] == '\\' {
			translated.WriteString(`\\`)
			index += 2
			continue
		}
		if pattern[index+1] == 'u' && index+6 <= len(pattern) && isHexQuad(pattern[index+2:index+6]) {
			translated.WriteString(`\x{`)
			translated.WriteString(pattern[index+2 : index+6])
			translated.WriteByte('}')
			index += 6
			continue
		}
		translated.WriteByte(pattern[index])
		translated.WriteByte(pattern[index+1])
		index += 2
	}
	return translated.String()
}

func isHexQuad(value string) bool {
	if len(value) != 4 {
		return false
	}
	for index := range value {
		character := value[index]
		if !((character >= '0' && character <= '9') ||
			(character >= 'a' && character <= 'f') ||
			(character >= 'A' && character <= 'F')) {
			return false
		}
	}
	return true
}
