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
		case OpConstant, OpLoadBinding, OpDeclareBinding, OpInitializeBinding, OpStoreBinding, OpCreateClosure:
			if uint64(instruction.A) >= uint64(constantCount) {
				return fmt.Errorf("%w: instruction %d uses constant %d", ErrConstantBounds, index, instruction.A)
			}
		case OpJump, OpJumpIfTrue, OpJumpIfFalse, OpJumpIfNullish, OpEnterTry:
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
				return err
			}
		}
		if terminal {
			continue
		}
		switch instruction.Op {
		case OpJump:
			if err := mergeStackDepth(depths, &queue, int(instruction.A), nextDepth); err != nil {
				return err
			}
		case OpJumpIfTrue, OpJumpIfFalse, OpJumpIfNullish:
			if err := mergeStackDepth(depths, &queue, int(instruction.A), nextDepth); err != nil {
				return err
			}
			if index+1 >= len(instructions) {
				return fmt.Errorf("%w: instruction %d can fall off Function", ErrInvalidBytecode, index)
			}
			if err := mergeStackDepth(depths, &queue, index+1, nextDepth); err != nil {
				return err
			}
		default:
			if index+1 >= len(instructions) {
				return fmt.Errorf("%w: instruction %d can fall off Function", ErrInvalidBytecode, index)
			}
			if err := mergeStackDepth(depths, &queue, index+1, nextDepth); err != nil {
				return err
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
		OpNewObject, OpNewArray, OpLoadBinding, OpLoadThis, OpCreateClosure:
		return 0, 1, false, nil
	case OpPop:
		return 1, -1, false, nil
	case OpDup:
		return 1, 1, false, nil
	case OpReturn:
		return 0, 0, true, nil
	case OpGetOwnProperty, OpDeleteOwnProperty, OpGetElement, OpDeleteElement:
		return 2, -1, false, nil
	case OpSetOwnProperty, OpSetElement:
		return 3, -2, false, nil
	case OpGetLength:
		return 1, 0, false, nil
	case OpSetLength:
		return 2, -1, false, nil
	case OpDeclareBinding, OpEnterTry, OpLeaveTry, OpEnterFinally, OpEndFinally:
		return 0, 0, false, nil
	case OpInitializeBinding, OpStoreBinding,
		OpNegate, OpIncrement, OpDecrement, OpLogicalNot, OpTypeOf:
		return 1, 0, false, nil
	case OpAdd, OpSubtract, OpMultiply, OpDivide, OpRemainder,
		OpBitwiseAnd, OpBitwiseOr, OpBitwiseXor,
		OpShiftLeft, OpShiftRight, OpUnsignedShiftRight,
		OpStrictEqual, OpStrictNotEqual,
		OpLessThan, OpLessThanOrEqual, OpGreaterThan, OpGreaterThanOrEqual:
		return 2, -1, false, nil
	case OpJump:
		return 0, 0, false, nil
	case OpJumpIfTrue, OpJumpIfFalse, OpJumpIfNullish:
		return 1, -1, false, nil
	case OpCall, OpCallNative, OpConstruct:
		if uint64(instruction.A) > uint64(^uint(0)>>1)-1 {
			return 0, 0, false, fmt.Errorf("call argument count %d is too large", instruction.A)
		}
		count := int(instruction.A)
		return count + 1, -count, false, nil
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
