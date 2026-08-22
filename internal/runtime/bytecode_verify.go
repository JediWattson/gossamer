package runtime

import (
	"fmt"
	"strings"
)

// VerifyBytecode validates operands, branch targets, handler targets, stack
// requirements, join depths, and reachable fallthrough without executing the
// Function.
func VerifyBytecode(code []byte, constantCount int) error {
	instructions, err := DecodeBytecode(code)
	if err != nil {
		return err
	}
	return verifyInstructions(instructions, constantCount)
}

func verifyInstructions(instructions []Instruction, constantCount int) error {
	if constantCount < 0 {
		return fmt.Errorf("%w: negative constant count", ErrInvalidBytecode)
	}
	if len(instructions) == 0 {
		return fmt.Errorf("%w: empty Function", ErrInvalidBytecode)
	}
	for index, instruction := range instructions {
		switch instruction.Op {
		case OpConstant, OpLoadBinding, OpTypeOfBinding, OpDeclareBinding, OpInitializeBinding, OpStoreBinding, OpCreateClosure, OpCreateArrowClosure,
			OpGetPropertyConstant, OpLoadThisProperty, OpBindArgument, OpUpdateBinding, OpSetOwnPropertyConstant:
			if uint64(instruction.A) >= uint64(constantCount) {
				return fmt.Errorf("%w: instruction %d uses constant %d", ErrConstantBounds, index, instruction.A)
			}
		case OpLoadBindingProperty:
			if uint64(instruction.A) >= uint64(constantCount) || uint64(instruction.B) >= uint64(constantCount) {
				return fmt.Errorf("%w: instruction %d uses constants %d/%d", ErrConstantBounds, index, instruction.A, instruction.B)
			}
		case OpJumpIfBindingTrue, OpJumpIfBindingFalse:
			if uint64(instruction.B) >= uint64(constantCount) {
				return fmt.Errorf("%w: instruction %d uses constant %d", ErrConstantBounds, index, instruction.B)
			}
			if uint64(instruction.A) >= uint64(len(instructions)) {
				return fmt.Errorf("%w: instruction %d jumps to %d", ErrInvalidBytecode, index, instruction.A)
			}
		case OpJumpIfBindingPropertyTrue, OpJumpIfBindingPropertyFalse:
			binding, property := unpackConstantPair(instruction.B)
			if uint64(binding) >= uint64(constantCount) || uint64(property) >= uint64(constantCount) {
				return fmt.Errorf("%w: instruction %d uses constants %d/%d", ErrConstantBounds, index, binding, property)
			}
			if uint64(instruction.A) >= uint64(len(instructions)) {
				return fmt.Errorf("%w: instruction %d jumps to %d", ErrInvalidBytecode, index, instruction.A)
			}
		case OpJumpIfThisPropertyTrue, OpJumpIfThisPropertyFalse, OpJumpIfPropertyConstantTrue, OpJumpIfPropertyConstantFalse:
			if uint64(instruction.B) >= uint64(constantCount) {
				return fmt.Errorf("%w: instruction %d uses constant %d", ErrConstantBounds, index, instruction.B)
			}
			if uint64(instruction.A) >= uint64(len(instructions)) {
				return fmt.Errorf("%w: instruction %d jumps to %d", ErrInvalidBytecode, index, instruction.A)
			}
		case OpEnterFunction:
			if uint64(instruction.A)+uint64(instruction.B) > uint64(constantCount) {
				return fmt.Errorf("%w: instruction %d uses constant range %d..%d", ErrConstantBounds, index, instruction.A, uint64(instruction.A)+uint64(instruction.B))
			}
		case OpDeclareBindings:
			count := instruction.B &^ bindingBatchMutable
			if count == 0 || uint64(instruction.A)+uint64(count) > uint64(constantCount) {
				return fmt.Errorf("%w: instruction %d uses declaration range %d..%d", ErrConstantBounds, index, instruction.A, uint64(instruction.A)+uint64(count))
			}
		case OpJump, OpJumpIfTrue, OpJumpIfFalse, OpJumpIfNullish,
			OpLogicalAndValue, OpLogicalOrValue, OpLogicalNullishValue,
			OpEnterTry, OpBreak, OpContinue:
			if uint64(instruction.A) >= uint64(len(instructions)) {
				return fmt.Errorf("%w: instruction %d jumps to %d", ErrInvalidBytecode, index, instruction.A)
			}
		}
		if instruction.Op == OpDeclareBinding && instruction.B > 1 {
			return fmt.Errorf("%w: DeclareBinding mutability %d", ErrInvalidBytecode, instruction.B)
		}
		if instruction.Op == OpEnterTry && instruction.B != uint32(HandlerCatch) && instruction.B != uint32(HandlerFinally) {
			return fmt.Errorf("%w: EnterTry handler kind %d", ErrInvalidBytecode, instruction.B)
		}
		if instruction.Op == OpUpdateProperty && (instruction.A > 1 || instruction.B > 1) {
			return fmt.Errorf("%w: UpdateProperty mode %d/%d", ErrInvalidBytecode, instruction.A, instruction.B)
		}
		if (instruction.Op == OpInitializeBinding || instruction.Op == OpStoreBinding) && instruction.B > 1 {
			return fmt.Errorf("%w: %s mode %d", ErrInvalidBytecode, instruction.Op, instruction.B)
		}
		if instruction.Op == OpUpdateBinding && instruction.B > 7 {
			return fmt.Errorf("%w: UpdateBinding mode %d", ErrInvalidBytecode, instruction.B)
		}
		if instruction.Op == OpDefineAccessor && instruction.A > 1 {
			return fmt.Errorf("%w: DefineAccessor mode %d", ErrInvalidBytecode, instruction.A)
		}
	}

	depths := map[int]int{0: 0}
	queue := []int{0}
	for len(queue) != 0 {
		index := queue[0]
		queue = queue[1:]
		depth := depths[index]
		instruction := instructions[index]
		required, delta, terminal, err := instructionStackEffect(instruction)
		if err != nil {
			return fmt.Errorf("%w: instruction %d: %v", ErrInvalidBytecode, index, err)
		}
		if depth < required {
			return fmt.Errorf("%w: instruction %d %s needs %d values, has %d", ErrStackUnderflow, index, instruction.Op, required, depth)
		}
		nextDepth := depth + delta
		if nextDepth < 0 {
			return fmt.Errorf("%w: instruction %d %s", ErrStackUnderflow, index, instruction.Op)
		}

		if instruction.Op == OpEnterTry {
			if err := mergeStackDepth(depths, &queue, int(instruction.A), depth); err != nil {
				return fmt.Errorf("instruction %d %s: %w", index, instruction.Op, err)
			}
		}
		if terminal {
			continue
		}
		switch instruction.Op {
		case OpJump, OpBreak, OpContinue:
			if err := mergeStackDepth(depths, &queue, int(instruction.A), nextDepth); err != nil {
				return fmt.Errorf("instruction %d %s: %w", index, instruction.Op, err)
			}
		case OpJumpIfTrue, OpJumpIfFalse, OpJumpIfNullish,
			OpJumpIfBindingTrue, OpJumpIfBindingFalse,
			OpJumpIfBindingPropertyTrue, OpJumpIfBindingPropertyFalse,
			OpJumpIfThisPropertyTrue, OpJumpIfThisPropertyFalse,
			OpJumpIfPropertyConstantTrue, OpJumpIfPropertyConstantFalse:
			if err := mergeStackDepth(depths, &queue, int(instruction.A), nextDepth); err != nil {
				return fmt.Errorf("instruction %d %s: %w", index, instruction.Op, err)
			}
			if index+1 >= len(instructions) {
				return fmt.Errorf("%w: instruction %d can fall off Function", ErrInvalidBytecode, index)
			}
			if err := mergeStackDepth(depths, &queue, index+1, nextDepth); err != nil {
				return fmt.Errorf("instruction %d %s: %w", index, instruction.Op, err)
			}
		case OpLogicalAndValue, OpLogicalOrValue, OpLogicalNullishValue:
			if err := mergeStackDepth(depths, &queue, int(instruction.A), depth); err != nil {
				return fmt.Errorf("instruction %d %s: %w", index, instruction.Op, err)
			}
			if index+1 >= len(instructions) {
				return fmt.Errorf("%w: instruction %d can fall off Function", ErrInvalidBytecode, index)
			}
			if err := mergeStackDepth(depths, &queue, index+1, nextDepth); err != nil {
				return fmt.Errorf("instruction %d %s: %w", index, instruction.Op, err)
			}
		default:
			if index+1 >= len(instructions) {
				return fmt.Errorf("%w: instruction %d can fall off Function", ErrInvalidBytecode, index)
			}
			if err := mergeStackDepth(depths, &queue, index+1, nextDepth); err != nil {
				return fmt.Errorf("instruction %d %s: %w", index, instruction.Op, err)
			}
		}
	}
	return nil
}

func mergeStackDepth(depths map[int]int, queue *[]int, target, depth int) error {
	if current, exists := depths[target]; exists {
		if current != depth {
			return fmt.Errorf("%w: instruction %d joins stack depths %d and %d", ErrInvalidBytecode, target, current, depth)
		}
		return nil
	}
	depths[target] = depth
	*queue = append(*queue, target)
	return nil
}

func instructionStackEffect(instruction Instruction) (required, delta int, terminal bool, err error) {
	switch instruction.Op {
	case OpConstant, OpArgument, OpUndefined, OpNull, OpTrue, OpFalse,
		OpNewObject, OpNewArray, OpLoadBinding, OpTypeOfBinding, OpArguments, OpLoadThis, OpCreateClosure, OpCreateArrowClosure,
		OpLoadBindingProperty, OpLoadThisProperty:
		return 0, 1, false, nil
	case OpPop:
		return 1, -1, false, nil
	case OpDup:
		return 1, 1, false, nil
	case OpDupPair:
		return 2, 2, false, nil
	case OpReturn:
		return 0, 0, true, nil
	case OpGetOwnProperty, OpDeleteOwnProperty, OpGetElement, OpDeleteElement,
		OpGetProperty, OpDeleteProperty, OpUpdateProperty:
		return 2, -1, false, nil
	case OpGetPropertyConstant:
		return 1, 0, false, nil
	case OpSetOwnProperty, OpSetElement, OpSetProperty, OpDefineAccessor:
		return 3, -2, false, nil
	case OpSetOwnPropertyConstant, OpSetElementConstant:
		return 2, -1, false, nil
	case OpGetLength:
		return 1, 0, false, nil
	case OpOwnKeys:
		return 1, 0, false, nil
	case OpGetIterator:
		return 1, 1, false, nil
	case OpIteratorNext:
		return 2, -1, false, nil
	case OpIteratorClose:
		return 1, -1, false, nil
	case OpAppendSpread:
		return 2, -1, false, nil
	case OpCopyDataProperties:
		return 2, -1, false, nil
	case OpSetLength:
		return 2, -1, false, nil
	case OpDeclareBinding, OpDeclareBindings, OpEnterTry, OpLeaveTry, OpEnterFinally, OpEndFinally, OpEnterScope, OpEnterFunction, OpLeaveScope:
		return 0, 0, false, nil
	case OpBindArgument:
		return 0, 0, false, nil
	case OpUpdateBinding:
		if instruction.B&4 != 0 {
			return 0, 0, false, nil
		}
		return 0, 1, false, nil
	case OpJumpIfBindingTrue, OpJumpIfBindingFalse,
		OpJumpIfBindingPropertyTrue, OpJumpIfBindingPropertyFalse,
		OpJumpIfThisPropertyTrue, OpJumpIfThisPropertyFalse:
		return 0, 0, false, nil
	case OpJumpIfPropertyConstantTrue, OpJumpIfPropertyConstantFalse:
		return 1, -1, false, nil
	case OpLogicalAndValue, OpLogicalOrValue, OpLogicalNullishValue:
		return 1, -1, false, nil
	case OpInitializeBinding, OpStoreBinding:
		if instruction.B == 1 {
			return 1, -1, false, nil
		}
		return 1, 0, false, nil
	case OpNegate, OpIncrement, OpDecrement, OpLogicalNot, OpTypeOf, OpToNumber:
		return 1, 0, false, nil
	case OpAdd, OpSubtract, OpMultiply, OpExponentiate, OpDivide, OpRemainder,
		OpBitwiseAnd, OpBitwiseOr, OpBitwiseXor,
		OpShiftLeft, OpShiftRight, OpUnsignedShiftRight,
		OpStrictEqual, OpStrictNotEqual, OpEqual, OpNotEqual,
		OpLessThan, OpLessThanOrEqual, OpGreaterThan, OpGreaterThanOrEqual,
		OpIn, OpInstanceOf:
		return 2, -1, false, nil
	case OpJump:
		return 0, 0, false, nil
	case OpBreak, OpContinue:
		return 0, 0, false, nil
	case OpJumpIfTrue, OpJumpIfFalse, OpJumpIfNullish:
		return 1, -1, false, nil
	case OpCall, OpCallNative, OpConstruct:
		if uint64(instruction.A) > uint64(^uint(0)>>1)-1 {
			return 0, 0, false, fmt.Errorf("call argument count %d is too large", instruction.A)
		}
		count := int(instruction.A)
		return count + 1, -count, false, nil
	case OpCallMethod:
		if uint64(instruction.A) > uint64(^uint(0)>>1)-2 {
			return 0, 0, false, fmt.Errorf("method argument count %d is too large", instruction.A)
		}
		count := int(instruction.A)
		return count + 2, -(count + 1), false, nil
	case OpCallSpread, OpConstructSpread:
		return 2, -1, false, nil
	case OpCallMethodSpread:
		return 3, -2, false, nil
	case OpThrow:
		return 1, -1, true, nil
	case OpEnterCatch:
		return 0, 1, false, nil
	case OpRethrow:
		return 0, 0, true, nil
	default:
		return 0, 0, false, fmt.Errorf("unknown opcode %d", instruction.Op)
	}
}

// Disassemble returns a stable, line-oriented representation suitable for
// compiler snapshots and diagnostics.
func Disassemble(code []byte) (string, error) {
	instructions, err := DecodeBytecode(code)
	if err != nil {
		return "", err
	}
	var output strings.Builder
	for index, instruction := range instructions {
		fmt.Fprintf(&output, "%04d  %-20s %d %d\n", index, instruction.Op, instruction.A, instruction.B)
	}
	return output.String(), nil
}
