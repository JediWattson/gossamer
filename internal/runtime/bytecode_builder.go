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
// parallel to decoded instructions and may be carried into Function payloads
// as diagnostic metadata.
type BytecodeChunk struct {
	Code      []byte
	Constants []memory.Value
	Locations []SourceSpan
}

type labelFixup struct {
	instruction uint32
	label       Label
}

const bindingBatchMutable = uint32(1 << 31)

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

// ReplaceOpcode rewrites one already-emitted instruction without changing its
// operands, source location, or any label positions. The compiler uses this to
// select a stack-local function prologue after it has seen the complete body
// and proved that the function creates no closures.
func (builder *BytecodeBuilder) ReplaceOpcode(index uint32, from, to Opcode) error {
	if builder == nil {
		return fmt.Errorf("%w: nil bytecode builder", ErrInvalidBytecode)
	}
	if uint64(index) >= uint64(len(builder.instructions)) {
		return fmt.Errorf("%w: instruction %d", ErrInvalidBytecode, index)
	}
	if builder.instructions[index].Op != from {
		return fmt.Errorf("%w: instruction %d is %s, want %s", ErrInvalidBytecode, index, builder.instructions[index].Op, from)
	}
	if !to.valid() {
		return fmt.Errorf("%w: opcode %d", ErrInvalidBytecode, to)
	}
	builder.instructions[index].Op = to
	return nil
}

// EmitDiscardAt emits Pop, or marks the immediately preceding binding write as
// discard-only when no branch can enter between the write and the Pop. This
// preserves the observable result of expression bytecode while avoiding a
// redundant stack round trip for statement-level writes.
func (builder *BytecodeBuilder) EmitDiscardAt(location SourceSpan) (uint32, error) {
	if builder == nil {
		return 0, fmt.Errorf("%w: nil bytecode builder", ErrInvalidBytecode)
	}
	position := uint32(len(builder.instructions))
	for _, target := range builder.labels {
		if target == position {
			return builder.EmitAt(Instruction{Op: OpPop}, location)
		}
	}
	if len(builder.instructions) != 0 {
		index := len(builder.instructions) - 1
		switch builder.instructions[index].Op {
		case OpInitializeBinding, OpStoreBinding:
			if builder.instructions[index].B == 0 {
				builder.instructions[index].B = 1
				return uint32(index), nil
			}
		case OpUpdateBinding:
			if builder.instructions[index].B&4 == 0 {
				builder.instructions[index].B |= 4
				return uint32(index), nil
			}
		}
	}
	return builder.EmitAt(Instruction{Op: OpPop}, location)
}

// EmitBindingDeclaration combines adjacent, same-mutability declaration runs
// whose interned names occupy a contiguous constant range.
func (builder *BytecodeBuilder) EmitBindingDeclaration(name uint32, mutable bool, location SourceSpan) (uint32, error) {
	if builder == nil {
		return 0, fmt.Errorf("%w: nil bytecode builder", ErrInvalidBytecode)
	}
	flag := uint32(0)
	if mutable {
		flag = 1
	}
	if len(builder.instructions) != 0 && !builder.labelAtCurrentPosition() {
		index := len(builder.instructions) - 1
		previous := builder.instructions[index]
		switch previous.Op {
		case OpDeclareBinding:
			if previous.B == flag && previous.A+1 == name {
				batchFlag := uint32(0)
				if mutable {
					batchFlag = bindingBatchMutable
				}
				builder.instructions[index] = Instruction{Op: OpDeclareBindings, A: previous.A, B: batchFlag | 2}
				return uint32(index), nil
			}
		case OpDeclareBindings:
			count := previous.B &^ bindingBatchMutable
			previousMutable := previous.B&bindingBatchMutable != 0
			if previousMutable == mutable && count < bindingBatchMutable-1 && previous.A+count == name {
				builder.instructions[index].B++
				return uint32(index), nil
			}
		}
	}
	return builder.EmitAt(Instruction{Op: OpDeclareBinding, A: name, B: flag}, location)
}

func (builder *BytecodeBuilder) EmitJump(opcode Opcode, label Label, location SourceSpan) (uint32, error) {
	if opcode != OpJump && opcode != OpJumpIfTrue && opcode != OpJumpIfFalse && opcode != OpJumpIfNullish {
		return 0, fmt.Errorf("%w: %s is not a jump opcode", ErrInvalidBytecode, opcode)
	}
	return builder.emitLabelReference(Instruction{Op: opcode}, label, location)
}

// EmitConditionalJump fuses a direct binding load into its truthiness branch
// when no label targets the boundary between them.
func (builder *BytecodeBuilder) EmitConditionalJump(opcode Opcode, label Label, location SourceSpan) (uint32, error) {
	if opcode != OpJumpIfTrue && opcode != OpJumpIfFalse {
		return builder.EmitJump(opcode, label, location)
	}
	if builder == nil {
		return 0, fmt.Errorf("%w: nil bytecode builder", ErrInvalidBytecode)
	}
	if label == 0 || label > builder.nextLabel {
		return 0, fmt.Errorf("%w: unknown label %d", ErrInvalidBytecode, label)
	}
	if len(builder.instructions) != 0 && !builder.labelAtCurrentPosition() {
		index := len(builder.instructions) - 1
		candidateIndex := index
		jumpOnTrue := opcode == OpJumpIfTrue
		if builder.instructions[index].Op == OpLogicalNot && index != 0 && !builder.labelAtPosition(uint32(index)) {
			candidateIndex--
			jumpOnTrue = !jumpOnTrue
		}
		candidate := builder.instructions[candidateIndex]
		fused := Opcode(0)
		operand := uint32(0)
		switch candidate.Op {
		case OpLoadBinding:
			operand = candidate.A
			fused = OpJumpIfBindingFalse
			if jumpOnTrue {
				fused = OpJumpIfBindingTrue
			}
		case OpLoadBindingProperty:
			var ok bool
			operand, ok = packConstantPair(candidate.A, candidate.B)
			if !ok {
				break
			}
			fused = OpJumpIfBindingPropertyFalse
			if jumpOnTrue {
				fused = OpJumpIfBindingPropertyTrue
			}
		case OpLoadThisProperty:
			operand = candidate.A
			fused = OpJumpIfThisPropertyFalse
			if jumpOnTrue {
				fused = OpJumpIfThisPropertyTrue
			}
		case OpGetPropertyConstant:
			operand = candidate.A
			fused = OpJumpIfPropertyConstantFalse
			if jumpOnTrue {
				fused = OpJumpIfPropertyConstantTrue
			}
		}
		if fused != 0 {
			builder.instructions[candidateIndex] = Instruction{Op: fused, B: operand}
			builder.locations[candidateIndex] = location
			if candidateIndex != index {
				builder.instructions[index] = Instruction{}
				builder.instructions = builder.instructions[:index]
				builder.locations[index] = SourceSpan{}
				builder.locations = builder.locations[:index]
			}
			builder.fixups = append(builder.fixups, labelFixup{instruction: uint32(candidateIndex), label: label})
			return uint32(candidateIndex), nil
		}
	}
	return builder.EmitJump(opcode, label, location)
}

func (builder *BytecodeBuilder) EmitLogicalJump(opcode Opcode, label Label, location SourceSpan) (uint32, error) {
	if opcode != OpLogicalAndValue && opcode != OpLogicalOrValue && opcode != OpLogicalNullishValue {
		return 0, fmt.Errorf("%w: %s is not a logical jump opcode", ErrInvalidBytecode, opcode)
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

func (builder *BytecodeBuilder) labelAtCurrentPosition() bool {
	return builder.labelAtPosition(uint32(len(builder.instructions)))
}

func (builder *BytecodeBuilder) labelAtPosition(position uint32) bool {
	for _, target := range builder.labels {
		if target == position {
			return true
		}
	}
	return false
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
