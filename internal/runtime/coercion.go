package runtime

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

type primitiveHint uint8

const (
	hintDefault primitiveHint = iota
	hintNumber
	hintString
)

func (execution *execution) toPrimitive(value memory.Value, hint primitiveHint) (memory.Value, error) {
	primitive, err := execution.isPrimitive(value)
	if err != nil || primitive {
		return value, err
	}
	methods := []string{"valueOf", "toString"}
	if hint == hintString {
		methods[0], methods[1] = methods[1], methods[0]
	}
	for _, methodName := range methods {
		name, err := execution.context.NewString(methodName)
		if err != nil {
			return memory.Value{}, err
		}
		method, present, err := execution.getProperty(value, memory.RefValue(name))
		if err != nil {
			return memory.Value{}, err
		}
		if !present || !method.IsRef() {
			continue
		}
		kind, err := execution.context.HeapKind(method.Ref())
		if err != nil {
			return memory.Value{}, err
		}
		if kind != memory.HeapFunction {
			continue
		}
		result, err := execution.call(method.Ref(), value, nil, callAny)
		if err != nil {
			return memory.Value{}, err
		}
		primitive, err := execution.isPrimitive(result)
		if err != nil {
			return memory.Value{}, err
		}
		if primitive {
			return result, nil
		}
	}
	return memory.Value{}, fmt.Errorf("%w: object cannot be converted to a primitive", ErrOperandType)
}

func (execution *execution) isPrimitive(value memory.Value) (bool, error) {
	if !value.IsRef() {
		return true, nil
	}
	kind, err := execution.context.HeapKind(value.Ref())
	if err != nil {
		return false, err
	}
	switch kind {
	case memory.HeapString, memory.HeapBigInt, memory.HeapSymbol:
		return true, nil
	default:
		return false, nil
	}
}

func (execution *execution) toNumber(value memory.Value) (float64, error) {
	switch value.Kind() {
	case memory.ValueUndefined:
		return math.NaN(), nil
	case memory.ValueNull:
		return 0, nil
	case memory.ValueBool:
		if value.Bool() {
			return 1, nil
		}
		return 0, nil
	case memory.ValueNumber:
		return value.Number(), nil
	case memory.ValueReference:
		kind, err := execution.context.HeapKind(value.Ref())
		if err != nil {
			return 0, err
		}
		switch kind {
		case memory.HeapString:
			text, err := execution.context.DerefString(value.Ref())
			if err != nil {
				return 0, err
			}
			return parseStringNumber(text), nil
		case memory.HeapBigInt:
			return 0, fmt.Errorf("%w: BigInt cannot be implicitly converted to Number", ErrOperandType)
		case memory.HeapSymbol:
			return 0, fmt.Errorf("%w: Symbol cannot be converted to Number", ErrOperandType)
		default:
			primitive, err := execution.toPrimitive(value, hintNumber)
			if err != nil {
				return 0, err
			}
			return execution.toNumber(primitive)
		}
	default:
		return 0, fmt.Errorf("%w: unknown Value kind %d", ErrOperandType, value.Kind())
	}
}

func parseStringNumber(text string) float64 {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	switch text {
	case "Infinity", "+Infinity":
		return math.Inf(1)
	case "-Infinity":
		return math.Inf(-1)
	}
	if len(text) > 2 && text[0] == '0' {
		base := 0
		switch text[1] {
		case 'x', 'X':
			base = 16
		case 'b', 'B':
			base = 2
		case 'o', 'O':
			base = 8
		}
		if base != 0 {
			if parsed, err := strconv.ParseUint(text[2:], base, 64); err == nil {
				return float64(parsed)
			}
			return math.NaN()
		}
	}
	parsed, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return math.NaN()
	}
	return parsed
}

func (execution *execution) toString(value memory.Value) (string, error) {
	switch value.Kind() {
	case memory.ValueUndefined:
		return "undefined", nil
	case memory.ValueNull:
		return "null", nil
	case memory.ValueBool:
		return strconv.FormatBool(value.Bool()), nil
	case memory.ValueNumber:
		return numberString(value.Number()), nil
	case memory.ValueReference:
		kind, err := execution.context.HeapKind(value.Ref())
		if err != nil {
			return "", err
		}
		switch kind {
		case memory.HeapString:
			return execution.context.DerefString(value.Ref())
		case memory.HeapBigInt:
			integer, err := execution.context.DerefBigInt(value.Ref())
			if err != nil {
				return "", err
			}
			result := new(big.Int).SetBytes(integer.Magnitude)
			if integer.Negative {
				result.Neg(result)
			}
			return result.Text(10), nil
		case memory.HeapSymbol:
			return "", fmt.Errorf("%w: Symbol cannot be converted to String", ErrOperandType)
		default:
			primitive, err := execution.toPrimitive(value, hintString)
			if err != nil {
				return "", err
			}
			return execution.toString(primitive)
		}
	default:
		return "", fmt.Errorf("%w: unknown Value kind %d", ErrOperandType, value.Kind())
	}
}

func numberString(number float64) string {
	if math.IsNaN(number) {
		return "NaN"
	}
	if math.IsInf(number, 1) {
		return "Infinity"
	}
	if math.IsInf(number, -1) {
		return "-Infinity"
	}
	if number == 0 {
		return "0"
	}
	return strconv.FormatFloat(number, 'g', -1, 64)
}

func (execution *execution) abstractEqual(left, right memory.Value) (bool, error) {
	return execution.abstractEqualDepth(left, right, 0)
}

func (execution *execution) abstractEqualDepth(left, right memory.Value, depth int) (bool, error) {
	if depth > 8 {
		return false, fmt.Errorf("%w: equality coercion did not converge", ErrOperandType)
	}
	leftType, err := execution.semanticType(left)
	if err != nil {
		return false, err
	}
	rightType, err := execution.semanticType(right)
	if err != nil {
		return false, err
	}
	if leftType == rightType {
		return strictEqual(execution.context, left, right)
	}
	if leftType == semanticNull && rightType == semanticUndefined || leftType == semanticUndefined && rightType == semanticNull {
		return true, nil
	}
	if leftType == semanticBool {
		return execution.abstractEqualDepth(memory.NumberValue(boolNumber(left.Bool())), right, depth+1)
	}
	if rightType == semanticBool {
		return execution.abstractEqualDepth(left, memory.NumberValue(boolNumber(right.Bool())), depth+1)
	}
	if leftType == semanticString && rightType == semanticNumber {
		number, err := execution.toNumber(left)
		return !math.IsNaN(number) && number == right.Number(), err
	}
	if leftType == semanticNumber && rightType == semanticString {
		number, err := execution.toNumber(right)
		return !math.IsNaN(number) && left.Number() == number, err
	}
	if leftType == semanticObject && rightType != semanticObject {
		primitive, err := execution.toPrimitive(left, hintDefault)
		if err != nil {
			return false, err
		}
		return execution.abstractEqualDepth(primitive, right, depth+1)
	}
	if leftType != semanticObject && rightType == semanticObject {
		primitive, err := execution.toPrimitive(right, hintDefault)
		if err != nil {
			return false, err
		}
		return execution.abstractEqualDepth(left, primitive, depth+1)
	}
	return false, nil
}

type semanticValueType uint8

const (
	semanticUndefined semanticValueType = iota + 1
	semanticNull
	semanticBool
	semanticNumber
	semanticString
	semanticBigInt
	semanticSymbol
	semanticObject
)

func (execution *execution) semanticType(value memory.Value) (semanticValueType, error) {
	switch value.Kind() {
	case memory.ValueUndefined:
		return semanticUndefined, nil
	case memory.ValueNull:
		return semanticNull, nil
	case memory.ValueBool:
		return semanticBool, nil
	case memory.ValueNumber:
		return semanticNumber, nil
	case memory.ValueReference:
		kind, err := execution.context.HeapKind(value.Ref())
		if err != nil {
			return 0, err
		}
		switch kind {
		case memory.HeapString:
			return semanticString, nil
		case memory.HeapBigInt:
			return semanticBigInt, nil
		case memory.HeapSymbol:
			return semanticSymbol, nil
		default:
			return semanticObject, nil
		}
	default:
		return 0, fmt.Errorf("%w: unknown Value kind %d", ErrOperandType, value.Kind())
	}
}

func boolNumber(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

func (execution *execution) isString(value memory.Value) (bool, error) {
	if !value.IsRef() {
		return false, nil
	}
	kind, err := execution.context.HeapKind(value.Ref())
	return kind == memory.HeapString, err
}

func (execution *execution) comparePrimitives(left, right memory.Value) (comparison int, unordered bool, err error) {
	left, err = execution.toPrimitive(left, hintNumber)
	if err != nil {
		return 0, false, err
	}
	right, err = execution.toPrimitive(right, hintNumber)
	if err != nil {
		return 0, false, err
	}
	leftString, err := execution.isString(left)
	if err != nil {
		return 0, false, err
	}
	rightString, err := execution.isString(right)
	if err != nil {
		return 0, false, err
	}
	if leftString && rightString {
		leftText, err := execution.toString(left)
		if err != nil {
			return 0, false, err
		}
		rightText, err := execution.toString(right)
		if err != nil {
			return 0, false, err
		}
		return strings.Compare(leftText, rightText), false, nil
	}
	leftNumber, err := execution.toNumber(left)
	if err != nil {
		return 0, false, err
	}
	rightNumber, err := execution.toNumber(right)
	if err != nil {
		return 0, false, err
	}
	if math.IsNaN(leftNumber) || math.IsNaN(rightNumber) {
		return 0, true, nil
	}
	switch {
	case leftNumber < rightNumber:
		return -1, false, nil
	case leftNumber > rightNumber:
		return 1, false, nil
	default:
		return 0, false, nil
	}
}
