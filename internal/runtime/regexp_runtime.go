package runtime

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
	"github.com/dlclark/regexp2"
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
	startByte, validStart := regexpRuneIndexToByteOffset(text, start)
	if !validStart {
		if stateful {
			_ = execution.context.SetRegExpLastIndex(expressionRef, 0)
		}
		return memory.NullValue(), nil
	}
	match, err := compiled.FindStringSubmatchIndex(text[startByte:])
	if err != nil {
		return memory.Value{}, err
	}
	if match == nil || expression.Flags&memory.RegExpSticky != 0 && match[0] != 0 {
		if stateful {
			_ = execution.context.SetRegExpLastIndex(expressionRef, 0)
		}
		return memory.NullValue(), nil
	}
	for index := range match {
		if match[index] >= 0 {
			match[index] += startByte
		}
	}
	if stateful {
		if err := execution.context.SetRegExpLastIndex(expressionRef, regexpByteOffsetToRuneIndex(text, match[1])); err != nil {
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
	matchIndex := regexpByteOffsetToRuneIndex(text, match[0])
	if err := defineData(execution.context, result, "index", memory.NumberValue(float64(matchIndex)), true, false, true); err != nil {
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
	startByte, validStart := regexpRuneIndexToByteOffset(text, start)
	if !validStart {
		if stateful {
			_ = execution.context.SetRegExpLastIndex(expressionRef, 0)
		}
		return memory.BoolValue(false), nil
	}
	location, err := compiled.FindStringIndex(text[startByte:])
	if err != nil {
		return memory.Value{}, err
	}
	matched := location != nil && (expression.Flags&memory.RegExpSticky == 0 || location[0] == 0)
	if stateful {
		next := uint64(0)
		if matched {
			next = regexpByteOffsetToRuneIndex(text, startByte+location[1])
		}
		if err := execution.context.SetRegExpLastIndex(expressionRef, next); err != nil {
			return memory.Value{}, err
		}
	}
	return memory.BoolValue(matched), nil
}

func regexpRuneIndexToByteOffset(text string, index uint64) (int, bool) {
	if index == 0 {
		return 0, true
	}
	position := uint64(0)
	for byteOffset := range text {
		if position == index {
			return byteOffset, true
		}
		position++
	}
	if position == index {
		return len(text), true
	}
	return 0, false
}

func regexpByteOffsetToRuneIndex(text string, offset int) uint64 {
	if offset <= 0 {
		return 0
	}
	if offset >= len(text) {
		return uint64(utf8.RuneCountInString(text))
	}
	return uint64(utf8.RuneCountInString(text[:offset]))
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

type compiledRegExp struct {
	standard *regexp.Regexp
	fallback *regexp2.Regexp
}

func (compiled *compiledRegExp) FindStringSubmatchIndex(text string) ([]int, error) {
	if compiled == nil {
		return nil, nil
	}
	if compiled.standard != nil {
		return compiled.standard.FindStringSubmatchIndex(text), nil
	}
	match, err := compiled.fallback.FindStringMatch(text)
	if err != nil || match == nil {
		return nil, regexpMatchError(err)
	}
	return regexp2MatchIndices(text, match), nil
}

func (compiled *compiledRegExp) FindStringIndex(text string) ([]int, error) {
	match, err := compiled.FindStringSubmatchIndex(text)
	if err != nil || match == nil {
		return nil, err
	}
	return match[:2], nil
}

func (compiled *compiledRegExp) FindAllStringSubmatchIndex(text string, limit int) ([][]int, error) {
	if compiled == nil {
		return nil, nil
	}
	if compiled.standard != nil {
		return compiled.standard.FindAllStringSubmatchIndex(text, limit), nil
	}
	var matches [][]int
	match, err := compiled.fallback.FindStringMatch(text)
	for match != nil && (limit < 0 || len(matches) < limit) {
		matches = append(matches, regexp2MatchIndices(text, match))
		match, err = compiled.fallback.FindNextMatch(match)
		if err != nil {
			return nil, regexpMatchError(err)
		}
	}
	return matches, regexpMatchError(err)
}

func (compiled *compiledRegExp) FindAllStringIndex(text string, limit int) ([][]int, error) {
	matches, err := compiled.FindAllStringSubmatchIndex(text, limit)
	if err != nil || matches == nil {
		return nil, err
	}
	indices := make([][]int, len(matches))
	for index, match := range matches {
		indices[index] = match[:2]
	}
	return indices, nil
}

func regexp2MatchIndices(text string, match *regexp2.Match) []int {
	runes := []rune(text)
	byteOffsets := make([]int, len(runes)+1)
	byteOffset := 0
	for index, character := range runes {
		byteOffsets[index] = byteOffset
		byteOffset += len(string(character))
	}
	byteOffsets[len(runes)] = len(text)
	groups := match.Groups()
	indices := make([]int, len(groups)*2)
	for index := range indices {
		indices[index] = -1
	}
	for index, group := range groups {
		if len(group.Captures) == 0 {
			continue
		}
		start := group.Index
		end := group.Index + group.Length
		if start < 0 || end < start || end >= len(byteOffsets) {
			continue
		}
		indices[index*2] = byteOffsets[start]
		indices[index*2+1] = byteOffsets[end]
	}
	return indices
}

func regexpMatchError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("runtime: RegExp match: %w", err)
}

func compileRegExp(context *TaskContext, expression memory.RegExp) (*compiledRegExp, error) {
	javascriptPattern, err := context.DerefString(expression.Pattern)
	if err != nil {
		return nil, err
	}
	pattern := translateJavaScriptRegExpPattern(javascriptPattern)
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
	compiled, compileErr := regexp.Compile(pattern)
	if compileErr == nil {
		return &compiledRegExp{standard: compiled}, nil
	}
	var options regexp2.RegexOptions = regexp2.ECMAScript
	if expression.Flags&memory.RegExpIgnoreCase != 0 {
		options |= regexp2.IgnoreCase
	}
	if expression.Flags&memory.RegExpMultiline != 0 {
		options |= regexp2.Multiline
	}
	if expression.Flags&memory.RegExpDotAll != 0 {
		options |= regexp2.Singleline
	}
	if expression.Flags&(memory.RegExpUnicode|memory.RegExpUnicodeSets) != 0 {
		options |= regexp2.Unicode
	}
	fallback, fallbackErr := regexp2.Compile(javascriptPattern, options)
	if fallbackErr != nil {
		return nil, fmt.Errorf("%w: pattern %q: RE2: %v; ECMAScript fallback: %v", memory.ErrInvalidRegExp, javascriptPattern, compileErr, fallbackErr)
	}
	fallback.MatchTimeout = 250 * time.Millisecond
	return &compiledRegExp{fallback: fallback}, nil
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
