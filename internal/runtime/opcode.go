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
	default:
		return fmt.Sprintf("Opcode(%d)", opcode)
	}
}

func (opcode Opcode) valid() bool {
	return opcode >= OpConstant && opcode <= OpReturn
}
