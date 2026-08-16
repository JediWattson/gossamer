package runtime

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const InstructionWidth = 9

var (
	ErrInvalidBytecode = errors.New("runtime: invalid bytecode")
	ErrConstantBounds  = errors.New("runtime: constant index out of bounds")
)

// Instruction remains deliberately unpacked in Go. Its byte representation is
// fixed-width: one opcode byte followed by two little-endian uint32 operands.
type Instruction struct {
	Op Opcode
	A  uint32
	B  uint32
}

func Assemble(instructions ...Instruction) []byte {
	code := make([]byte, len(instructions)*InstructionWidth)
	for index, instruction := range instructions {
		offset := index * InstructionWidth
		code[offset] = byte(instruction.Op)
		binary.LittleEndian.PutUint32(code[offset+1:offset+5], instruction.A)
		binary.LittleEndian.PutUint32(code[offset+5:offset+9], instruction.B)
	}
	return code
}

func DecodeBytecode(code []byte) ([]Instruction, error) {
	if len(code)%InstructionWidth != 0 {
		return nil, fmt.Errorf("%w: %d bytes is not a multiple of %d", ErrInvalidBytecode, len(code), InstructionWidth)
	}
	instructions := make([]Instruction, len(code)/InstructionWidth)
	for index := range instructions {
		offset := index * InstructionWidth
		instruction := Instruction{
			Op: Opcode(code[offset]),
			A:  binary.LittleEndian.Uint32(code[offset+1 : offset+5]),
			B:  binary.LittleEndian.Uint32(code[offset+5 : offset+9]),
		}
		if !instruction.Op.valid() {
			return nil, fmt.Errorf("%w: instruction %d has opcode %d", ErrInvalidBytecode, index, instruction.Op)
		}
		instructions[index] = instruction
	}
	return instructions, nil
}
