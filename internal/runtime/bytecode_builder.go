package runtime

import (
	"fmt"
	"math"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

// Label is a symbolic instruction target owned by one BytecodeBuilder. The
// zero value is invalid so an accidentally omitted label cannot target the
// first instruction.
type Label uint32

// SourceSpan identifies a half-open byte range in the source that emitted one
// instruction. A zero span means that no source location was supplied.
type SourceSpan struct {
	Start uint32
	End   uint32
}

// BytecodeChunk is the checked output of BytecodeBuilder. Locations are
// parallel to decoded instructions and are intentionally kept outside the
// runtime Function payload for now.
type BytecodeChunk struct {
	Code      []byte
	Constants []memory.Value
	Locations []SourceSpan
}

type labelFixup struct {
	instruction uint32
	label       Label
}

// BytecodeBuilder owns symbolic labels, constants, and source locations for a
// single Function. It is not safe for concurrent use.
type BytecodeBuilder struct {
	instructions []Instruction
	locations    []SourceSpan
	constants    []memory.Value
	constantIDs  map[constantKey]uint32
	labels       map[Label]uint32
	fixups       []labelFixup
	nextLabel    Label
}

type constantKey struct {
	kind   memory.ValueKind
	bits   uint64
	ref    memory.Ref
	truthy bool
}

func NewBytecodeBuilder() *BytecodeBuilder {
	return &BytecodeBuilder{
		constantIDs: make(map[constantKey]uint32),
		labels:      make(map[Label]uint32),
	}
}

func (builder *BytecodeBuilder) NewLabel() Label {
	if builder == nil {
		return 0
	}
	builder.nextLabel++
	return builder.nextLabel
}

func (builder *BytecodeBuilder) Mark(label Label) error {
	if builder == nil {
		return fmt.Errorf("%w: nil bytecode builder", ErrInvalidBytecode)
	}
	if label == 0 || label > builder.nextLabel {
		return fmt.Errorf("%w: unknown label %d", ErrInvalidBytecode, label)
	}
	if _, exists := builder.labels[label]; exists {
		return fmt.Errorf("%w: label %d marked twice", ErrInvalidBytecode, label)
	}
	if uint64(len(builder.instructions)) > math.MaxUint32 {
		return fmt.Errorf("%w: too many instructions", ErrInvalidBytecode)
	}
	builder.labels[label] = uint32(len(builder.instructions))
	return nil
}

func (builder *BytecodeBuilder) AddConstant(value memory.Value) (uint32, error) {
	if builder == nil {
		return 0, fmt.Errorf("%w: nil bytecode builder", ErrInvalidBytecode)
	}
	key := keyForConstant(value)
	if index, exists := builder.constantIDs[key]; exists {
		return index, nil
	}
	if uint64(len(builder.constants)) > math.MaxUint32 {
		return 0, fmt.Errorf("%w: too many constants", ErrInvalidBytecode)
	}
	index := uint32(len(builder.constants))
	builder.constants = append(builder.constants, value)
	builder.constantIDs[key] = index
	return index, nil
}

func keyForConstant(value memory.Value) constantKey {
	key := constantKey{kind: value.Kind()}
	switch value.Kind() {
	case memory.ValueBool:
		key.truthy = value.Bool()
	case memory.ValueNumber:
		key.bits = math.Float64bits(value.Number())
	case memory.ValueReference:
		key.ref = value.Ref()
	}
	return key
}

func (builder *BytecodeBuilder) Emit(instruction Instruction) (uint32, error) {
	return builder.EmitAt(instruction, SourceSpan{})
}

func (builder *BytecodeBuilder) EmitAt(instruction Instruction, location SourceSpan) (uint32, error) {
	if builder == nil {
		return 0, fmt.Errorf("%w: nil bytecode builder", ErrInvalidBytecode)
	}
	if !instruction.Op.valid() {
		return 0, fmt.Errorf("%w: opcode %d", ErrInvalidBytecode, instruction.Op)
	}
	if location.End < location.Start {
		return 0, fmt.Errorf("%w: source span %d..%d", ErrInvalidBytecode, location.Start, location.End)
	}
	if uint64(len(builder.instructions)) > math.MaxUint32 {
		return 0, fmt.Errorf("%w: too many instructions", ErrInvalidBytecode)
	}
	index := uint32(len(builder.instructions))
	builder.instructions = append(builder.instructions, instruction)
	builder.locations = append(builder.locations, location)
	return index, nil
}

func (builder *BytecodeBuilder) EmitJump(opcode Opcode, label Label, location SourceSpan) (uint32, error) {
	if opcode != OpJump && opcode != OpJumpIfTrue && opcode != OpJumpIfFalse && opcode != OpJumpIfNullish {
		return 0, fmt.Errorf("%w: %s is not a jump opcode", ErrInvalidBytecode, opcode)
	}
	return builder.emitLabelReference(Instruction{Op: opcode}, label, location)
}

func (builder *BytecodeBuilder) EmitHandler(kind ExceptionHandlerKind, label Label, location SourceSpan) (uint32, error) {
	if kind != HandlerCatch && kind != HandlerFinally {
		return 0, fmt.Errorf("%w: handler kind %d", ErrInvalidBytecode, kind)
	}
	return builder.emitLabelReference(Instruction{Op: OpEnterTry, B: uint32(kind)}, label, location)
}

func (builder *BytecodeBuilder) emitLabelReference(instruction Instruction, label Label, location SourceSpan) (uint32, error) {
	if builder == nil {
		return 0, fmt.Errorf("%w: nil bytecode builder", ErrInvalidBytecode)
	}
	if label == 0 || label > builder.nextLabel {
		return 0, fmt.Errorf("%w: unknown label %d", ErrInvalidBytecode, label)
	}
	index, err := builder.EmitAt(instruction, location)
	if err != nil {
		return 0, err
	}
	builder.fixups = append(builder.fixups, labelFixup{instruction: index, label: label})
	return index, nil
}

func (builder *BytecodeBuilder) Build() (BytecodeChunk, error) {
	if builder == nil {
		return BytecodeChunk{}, fmt.Errorf("%w: nil bytecode builder", ErrInvalidBytecode)
	}
	instructions := append([]Instruction(nil), builder.instructions...)
	for _, fixup := range builder.fixups {
		target, exists := builder.labels[fixup.label]
		if !exists {
			return BytecodeChunk{}, fmt.Errorf("%w: label %d was never marked", ErrInvalidBytecode, fixup.label)
		}
		instructions[fixup.instruction].A = target
	}
	if err := verifyInstructions(instructions, len(builder.constants)); err != nil {
		return BytecodeChunk{}, err
	}
	return BytecodeChunk{
		Code:      Assemble(instructions...),
		Constants: append([]memory.Value(nil), builder.constants...),
		Locations: append([]SourceSpan(nil), builder.locations...),
	}, nil
}
