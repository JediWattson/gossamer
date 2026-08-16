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

// EmitCompletion emits a loop completion whose target is patched from label.
// The lexical-environment and handler depths are carried with the completion
// so the interpreter can cross any intervening finally handlers before it
// resumes at the loop target.
func (builder *BytecodeBuilder) EmitCompletion(opcode Opcode, label Label, environmentDepth, handlerDepth int, location SourceSpan) (uint32, error) {
	if opcode != OpBreak && opcode != OpContinue {
		return 0, fmt.Errorf("%w: %s is not a loop completion opcode", ErrInvalidBytecode, opcode)
	}
	depths, err := packCompletionDepths(environmentDepth, handlerDepth)
	if err != nil {
		return 0, err
	}
	return builder.emitLabelReference(Instruction{Op: opcode, B: depths}, label, location)
}

func packCompletionDepths(environmentDepth, handlerDepth int) (uint32, error) {
	if environmentDepth < 0 || environmentDepth > math.MaxUint16 {
		return 0, fmt.Errorf("%w: completion environment depth %d", ErrInvalidBytecode, environmentDepth)
	}
	if handlerDepth < 0 || handlerDepth > math.MaxUint16 {
		return 0, fmt.Errorf("%w: completion handler depth %d", ErrInvalidBytecode, handlerDepth)
	}
	return uint32(environmentDepth)<<16 | uint32(handlerDepth), nil
}

func unpackCompletionDepths(depths uint32) (environmentDepth, handlerDepth int) {
	return int(depths >> 16), int(depths & math.MaxUint16)
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
	code, locations, err := builder.BuildCode(len(builder.constants))
	if err != nil {
		return BytecodeChunk{}, err
	}
	return BytecodeChunk{
		Code:      code,
		Constants: append([]memory.Value(nil), builder.constants...),
		Locations: locations,
	}, nil
}

// BuildCode patches labels and verifies instructions against an external
// constant table. Portable compilers use this without manufacturing temporary
// RegionStore Values during compilation.
func (builder *BytecodeBuilder) BuildCode(constantCount int) ([]byte, []SourceSpan, error) {
	if builder == nil {
		return nil, nil, fmt.Errorf("%w: nil bytecode builder", ErrInvalidBytecode)
	}
	instructions := append([]Instruction(nil), builder.instructions...)
	for _, fixup := range builder.fixups {
		target, exists := builder.labels[fixup.label]
		if !exists {
			return nil, nil, fmt.Errorf("%w: label %d was never marked", ErrInvalidBytecode, fixup.label)
		}
		instructions[fixup.instruction].A = target
	}
	if err := verifyInstructions(instructions, constantCount); err != nil {
		return nil, nil, err
	}
	return Assemble(instructions...), append([]SourceSpan(nil), builder.locations...), nil
}
