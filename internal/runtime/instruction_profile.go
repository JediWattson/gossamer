package runtime

import (
	"fmt"
	"sort"
	"strings"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

const instructionProfileTop = 12

type instructionProgramStats struct {
	ref          memory.Ref
	name         string
	key          bytecodeCacheKey
	instructions int
	calls        uint64
	steps        uint64
}

type instructionProfile struct {
	opcodes  [256]uint64
	programs map[*cachedBytecode]*instructionProgramStats
}

type instructionLimitProfileError struct {
	summary string
}

func (err *instructionLimitProfileError) Error() string {
	return ErrInstructionLimit.Error() + "; " + err.summary
}

func (err *instructionLimitProfileError) Unwrap() error {
	return ErrInstructionLimit
}

func (interpreter *Interpreter) newExecution(context *TaskContext) *execution {
	execution := &execution{interpreter: interpreter, context: context}
	if interpreter != nil && interpreter.config.ProfileInstructions {
		execution.profile = &instructionProfile{}
	}
	return execution
}

func (execution *execution) instructionLimitError() error {
	if execution == nil || execution.profile == nil {
		return ErrInstructionLimit
	}
	return &instructionLimitProfileError{summary: execution.profile.summary()}
}

func (profile *instructionProfile) reset() {
	if profile == nil {
		return
	}
	clear(profile.opcodes[:])
	clear(profile.programs)
}

func (profile *instructionProfile) bind(frame *Frame, program *cachedBytecode, context *TaskContext) {
	if profile == nil || frame == nil || program == nil {
		return
	}
	if profile.programs == nil {
		profile.programs = make(map[*cachedBytecode]*instructionProgramStats)
	}
	stats := profile.programs[program]
	if stats == nil {
		stats = &instructionProgramStats{
			ref:          frame.Function,
			name:         instructionFunctionName(context, frame.Function, frame.function),
			key:          program.key,
			instructions: len(program.instructions),
		}
		profile.programs[program] = stats
	}
	stats.calls++
	frame.profile = stats
}

func (profile *instructionProfile) record(frame *Frame, opcode Opcode) {
	if profile == nil {
		return
	}
	profile.opcodes[uint8(opcode)]++
	if frame != nil && frame.profile != nil {
		frame.profile.steps++
	}
}

func instructionFunctionName(context *TaskContext, ref memory.Ref, function memory.Function) string {
	if context != nil && function.Name.IsRef() {
		if name, err := context.DerefString(function.Name.Ref()); err == nil && name != "" {
			return name
		}
	}
	return ref.String()
}

func (profile *instructionProfile) summary() string {
	if profile == nil {
		return "instruction profile unavailable"
	}
	type opcodeCount struct {
		opcode Opcode
		count  uint64
	}
	opcodes := make([]opcodeCount, 0, len(profile.opcodes))
	for raw, count := range profile.opcodes {
		if count != 0 {
			opcodes = append(opcodes, opcodeCount{opcode: Opcode(raw), count: count})
		}
	}
	sort.Slice(opcodes, func(left, right int) bool {
		if opcodes[left].count == opcodes[right].count {
			return opcodes[left].opcode < opcodes[right].opcode
		}
		return opcodes[left].count > opcodes[right].count
	})
	programs := make([]*instructionProgramStats, 0, len(profile.programs))
	for _, stats := range profile.programs {
		programs = append(programs, stats)
	}
	sort.Slice(programs, func(left, right int) bool {
		if programs[left].steps == programs[right].steps {
			return programs[left].name < programs[right].name
		}
		return programs[left].steps > programs[right].steps
	})

	var output strings.Builder
	output.WriteString("top opcodes [")
	for index, entry := range opcodes {
		if index >= instructionProfileTop {
			break
		}
		if index != 0 {
			output.WriteString(", ")
		}
		fmt.Fprintf(&output, "%s=%d", entry.opcode, entry.count)
	}
	output.WriteString("]; top bytecode [")
	for index, stats := range programs {
		if index >= instructionProfileTop {
			break
		}
		if index != 0 {
			output.WriteString(", ")
		}
		fmt.Fprintf(
			&output,
			"%s(%s){%016x,%d ins,%d const}=%d/%d calls",
			stats.name, stats.ref, stats.key.hash, stats.instructions, stats.key.constantCount, stats.steps, stats.calls,
		)
	}
	output.WriteByte(']')
	return output.String()
}
