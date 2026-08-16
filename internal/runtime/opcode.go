package runtime

import "fmt"

// Opcode identifies one explicit native interpreter operation. Bytecode is a
// testable execution format, not a JavaScript source representation.
type Opcode uint8

const (
	OpConstant Opcode = iota + 1
	OpArgument
	OpUndefined
	OpNull
	OpTrue
	OpFalse
	OpPop
	OpDup
	OpReturn
	OpNewObject
	OpGetOwnProperty
	OpSetOwnProperty
	OpDeleteOwnProperty
	OpNewArray
	OpGetElement
	OpSetElement
	OpDeleteElement
	OpGetLength
	OpSetLength
	OpLoadBinding
	OpDeclareBinding
	OpInitializeBinding
	OpStoreBinding
	OpLoadThis
	OpAdd
	OpSubtract
	OpMultiply
	OpDivide
	OpRemainder
	OpNegate
	OpIncrement
	OpDecrement
	OpBitwiseAnd
	OpBitwiseOr
	OpBitwiseXor
	OpShiftLeft
	OpShiftRight
	OpUnsignedShiftRight
	OpLogicalNot
	OpTypeOf
	OpStrictEqual
	OpStrictNotEqual
	OpLessThan
	OpLessThanOrEqual
	OpGreaterThan
	OpGreaterThanOrEqual
	OpJump
	OpJumpIfTrue
	OpJumpIfFalse
	OpJumpIfNullish
)

func (opcode Opcode) String() string {
	switch opcode {
	case OpConstant:
		return "Constant"
	case OpArgument:
		return "Argument"
	case OpUndefined:
		return "Undefined"
	case OpNull:
		return "Null"
	case OpTrue:
		return "True"
	case OpFalse:
		return "False"
	case OpPop:
		return "Pop"
	case OpDup:
		return "Dup"
	case OpReturn:
		return "Return"
	case OpNewObject:
		return "NewObject"
	case OpGetOwnProperty:
		return "GetOwnProperty"
	case OpSetOwnProperty:
		return "SetOwnProperty"
	case OpDeleteOwnProperty:
		return "DeleteOwnProperty"
	case OpNewArray:
		return "NewArray"
	case OpGetElement:
		return "GetElement"
	case OpSetElement:
		return "SetElement"
	case OpDeleteElement:
		return "DeleteElement"
	case OpGetLength:
		return "GetLength"
	case OpSetLength:
		return "SetLength"
	case OpLoadBinding:
		return "LoadBinding"
	case OpDeclareBinding:
		return "DeclareBinding"
	case OpInitializeBinding:
		return "InitializeBinding"
	case OpStoreBinding:
		return "StoreBinding"
	case OpLoadThis:
		return "LoadThis"
	case OpAdd:
		return "Add"
	case OpSubtract:
		return "Subtract"
	case OpMultiply:
		return "Multiply"
	case OpDivide:
		return "Divide"
	case OpRemainder:
		return "Remainder"
	case OpNegate:
		return "Negate"
	case OpIncrement:
		return "Increment"
	case OpDecrement:
		return "Decrement"
	case OpBitwiseAnd:
		return "BitwiseAnd"
	case OpBitwiseOr:
		return "BitwiseOr"
	case OpBitwiseXor:
		return "BitwiseXor"
	case OpShiftLeft:
		return "ShiftLeft"
	case OpShiftRight:
		return "ShiftRight"
	case OpUnsignedShiftRight:
		return "UnsignedShiftRight"
	case OpLogicalNot:
		return "LogicalNot"
	case OpTypeOf:
		return "TypeOf"
	case OpStrictEqual:
		return "StrictEqual"
	case OpStrictNotEqual:
		return "StrictNotEqual"
	case OpLessThan:
		return "LessThan"
	case OpLessThanOrEqual:
		return "LessThanOrEqual"
	case OpGreaterThan:
		return "GreaterThan"
	case OpGreaterThanOrEqual:
		return "GreaterThanOrEqual"
	case OpJump:
		return "Jump"
	case OpJumpIfTrue:
		return "JumpIfTrue"
	case OpJumpIfFalse:
		return "JumpIfFalse"
	case OpJumpIfNullish:
		return "JumpIfNullish"
	default:
		return fmt.Sprintf("Opcode(%d)", opcode)
	}
}

func (opcode Opcode) valid() bool {
	return opcode >= OpConstant && opcode <= OpJumpIfNullish
}
