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
		{"toString", 0, nativeRegExpToString},
	})
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
		pattern = "(?" + modes.String() + ")" + pattern
	}
	compiled, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", memory.ErrInvalidRegExp, err)
	}
	return compiled, nil
}
