package runtime

import (
	"fmt"
	"math"
	"math/big"
	"strings"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

const maximumComputedBigIntBytes = 1 << 20

func bigIntOperand(context *TaskContext, value memory.Value) (*big.Int, bool, error) {
	if !value.IsRef() {
		return nil, false, nil
	}
	kind, err := context.HeapKind(value.Ref())
	if err != nil {
		return nil, false, err
	}
	if kind != memory.HeapBigInt {
		return nil, false, nil
	}
	stored, err := context.DerefBigInt(value.Ref())
	if err != nil {
		return nil, false, err
	}
	integer := new(big.Int).SetBytes(stored.Magnitude)
	if stored.Negative {
		integer.Neg(integer)
	}
	return integer, true, nil
}

func newComputedBigInt(context *TaskContext, integer *big.Int) (memory.Value, error) {
	if integer == nil {
		return memory.Value{}, fmt.Errorf("%w: nil BigInt result", ErrOperandType)
	}
	if (integer.BitLen()+7)/8 > maximumComputedBigIntBytes {
		return memory.Value{}, fmt.Errorf("%w: BigInt result exceeds %d bytes", ErrOperandType, maximumComputedBigIntBytes)
	}
	magnitude := new(big.Int).Abs(integer).Bytes()
	ref, err := context.NewBigInt(integer.Sign() < 0, magnitude)
	return memory.RefValue(ref), err
}

func executeBigIntUnary(context *TaskContext, opcode Opcode, operand memory.Value) (memory.Value, bool, error) {
	integer, ok, err := bigIntOperand(context, operand)
	if err != nil || !ok {
		return memory.Value{}, ok, err
	}
	switch opcode {
	case OpNegate:
		integer.Neg(integer)
	case OpIncrement:
		integer.Add(integer, big.NewInt(1))
	case OpDecrement:
		integer.Sub(integer, big.NewInt(1))
	case OpToNumber:
		return memory.Value{}, true, fmt.Errorf("%w: unary plus cannot convert BigInt", ErrOperandType)
	default:
		return memory.Value{}, false, nil
	}
	result, err := newComputedBigInt(context, integer)
	return result, true, err
}

func executeBigIntBinary(context *TaskContext, opcode Opcode, leftValue, rightValue memory.Value) (memory.Value, bool, error) {
	left, leftBigInt, err := bigIntOperand(context, leftValue)
	if err != nil {
		return memory.Value{}, false, err
	}
	right, rightBigInt, err := bigIntOperand(context, rightValue)
	if err != nil {
		return memory.Value{}, false, err
	}
	if !leftBigInt && !rightBigInt {
		return memory.Value{}, false, nil
	}
	if !leftBigInt || !rightBigInt {
		return memory.Value{}, true, fmt.Errorf("%w: cannot mix BigInt and Number operands", ErrOperandType)
	}

	result := new(big.Int)
	switch opcode {
	case OpAdd:
		result.Add(left, right)
	case OpSubtract:
		result.Sub(left, right)
	case OpMultiply:
		if left.Sign() != 0 && right.Sign() != 0 && left.BitLen()+right.BitLen() > maximumComputedBigIntBytes*8+1 {
			return memory.Value{}, true, fmt.Errorf("%w: BigInt product exceeds %d bytes", ErrOperandType, maximumComputedBigIntBytes)
		}
		result.Mul(left, right)
	case OpExponentiate:
		if right.Sign() < 0 {
			return memory.Value{}, true, fmt.Errorf("%w: BigInt exponent must be non-negative", ErrOperandType)
		}
		if !right.IsUint64() {
			return memory.Value{}, true, fmt.Errorf("%w: BigInt exponent is too large", ErrOperandType)
		}
		exponent := right.Uint64()
		if left.BitLen() > 1 && exponent > uint64(maximumComputedBigIntBytes*8)/uint64(left.BitLen()-1) {
			return memory.Value{}, true, fmt.Errorf("%w: BigInt power exceeds %d bytes", ErrOperandType, maximumComputedBigIntBytes)
		}
		result.Exp(left, right, nil)
	case OpDivide:
		if right.Sign() == 0 {
			return memory.Value{}, true, fmt.Errorf("%w: BigInt division by zero", ErrOperandType)
		}
		result.Quo(left, right)
	case OpRemainder:
		if right.Sign() == 0 {
			return memory.Value{}, true, fmt.Errorf("%w: BigInt division by zero", ErrOperandType)
		}
		result.Rem(left, right)
	case OpBitwiseAnd:
		result.And(left, right)
	case OpBitwiseOr:
		result.Or(left, right)
	case OpBitwiseXor:
		result.Xor(left, right)
	case OpShiftLeft, OpShiftRight:
		if !right.IsInt64() {
			return memory.Value{}, true, fmt.Errorf("%w: BigInt shift count is too large", ErrOperandType)
		}
		shift := right.Int64()
		if opcode == OpShiftRight {
			shift = -shift
		}
		if shift >= 0 {
			if uint64(shift) > uint64(maximumComputedBigIntBytes*8) || left.BitLen()+int(shift) > maximumComputedBigIntBytes*8 {
				return memory.Value{}, true, fmt.Errorf("%w: BigInt shift exceeds %d bytes", ErrOperandType, maximumComputedBigIntBytes)
			}
			result.Lsh(left, uint(shift))
		} else {
			result.Rsh(left, uint(-shift))
		}
	case OpUnsignedShiftRight:
		return memory.Value{}, true, fmt.Errorf("%w: BigInt has no unsigned right shift", ErrOperandType)
	default:
		return memory.Value{}, false, nil
	}
	value, err := newComputedBigInt(context, result)
	return value, true, err
}

func compareBigIntPrimitives(execution *execution, leftValue, rightValue memory.Value) (int, bool, bool, error) {
	left, leftBigInt, err := bigIntOperand(execution.context, leftValue)
	if err != nil {
		return 0, false, false, err
	}
	right, rightBigInt, err := bigIntOperand(execution.context, rightValue)
	if err != nil {
		return 0, false, false, err
	}
	if !leftBigInt && !rightBigInt {
		return 0, false, false, nil
	}
	if leftBigInt && rightBigInt {
		return left.Cmp(right), false, true, nil
	}
	if leftBigInt {
		comparison, unordered, err := compareBigIntAndNumber(execution, left, rightValue)
		return comparison, unordered, true, err
	}
	comparison, unordered, err := compareBigIntAndNumber(execution, right, leftValue)
	return -comparison, unordered, true, err
}

func compareBigIntAndNumber(execution *execution, integer *big.Int, value memory.Value) (int, bool, error) {
	number, err := execution.toNumber(value)
	if err != nil {
		return 0, false, err
	}
	if math.IsNaN(number) {
		return 0, true, nil
	}
	if math.IsInf(number, 1) {
		return -1, false, nil
	}
	if math.IsInf(number, -1) {
		return 1, false, nil
	}
	numberRat := new(big.Rat).SetFloat64(number)
	return new(big.Rat).SetInt(integer).Cmp(numberRat), false, nil
}

func equalBigIntPrimitives(execution *execution, leftValue, rightValue memory.Value, leftType, rightType semanticValueType) (bool, bool, error) {
	if leftType != semanticBigInt && rightType != semanticBigInt {
		return false, false, nil
	}
	if leftType == semanticBigInt && rightType == semanticBigInt {
		equal, err := strictEqual(execution.context, leftValue, rightValue)
		return equal, true, err
	}
	if rightType == semanticBigInt {
		equal, handled, err := equalBigIntPrimitives(execution, rightValue, leftValue, rightType, leftType)
		return equal, handled, err
	}
	left, _, err := bigIntOperand(execution.context, leftValue)
	if err != nil {
		return false, true, err
	}
	switch rightType {
	case semanticString:
		text, err := execution.context.DerefString(rightValue.Ref())
		if err != nil {
			return false, true, err
		}
		right, ok := parseBigIntString(text)
		return ok && left.Cmp(right) == 0, true, nil
	case semanticNumber:
		number := rightValue.Number()
		if math.IsNaN(number) || math.IsInf(number, 0) {
			return false, true, nil
		}
		numberRat := new(big.Rat).SetFloat64(number)
		return new(big.Rat).SetInt(left).Cmp(numberRat) == 0, true, nil
	default:
		return false, true, nil
	}
}

func parseBigIntString(text string) (*big.Int, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return new(big.Int), true
	}
	base := 10
	unsigned := strings.TrimPrefix(strings.TrimPrefix(text, "+"), "-")
	lower := strings.ToLower(unsigned)
	if strings.HasPrefix(lower, "0x") || strings.HasPrefix(lower, "0o") || strings.HasPrefix(lower, "0b") {
		base = 0
	}
	integer, ok := new(big.Int).SetString(text, base)
	return integer, ok
}
