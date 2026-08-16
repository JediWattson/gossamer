package runtime

import (
	"errors"
	"fmt"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

var (
	ErrInstructionLimit = errors.New("runtime: instruction limit exceeded")
	ErrNotCallable      = errors.New("runtime: value is not a callable bytecode Function")
)

const defaultMaxInstructions uint64 = 1_000_000

type InterpreterConfig struct {
	MaxInstructions uint64
	MaxCallDepth    uint32
}

// Interpreter executes native Function descriptors against one TaskContext.
// It does not parse source, schedule work, or extend value lifetimes.
type Interpreter struct {
	config InterpreterConfig
}

func NewInterpreter(config InterpreterConfig) *Interpreter {
	if config.MaxInstructions == 0 {
		config.MaxInstructions = defaultMaxInstructions
	}
	if config.MaxCallDepth == 0 {
		config.MaxCallDepth = 256
	}
	return &Interpreter{config: config}
}

func (interpreter *Interpreter) Execute(context *TaskContext, function memory.Ref, arguments ...memory.Value) (memory.Value, error) {
	if interpreter == nil {
		return memory.Value{}, fmt.Errorf("runtime: nil interpreter")
	}
	if context == nil || context.Realm == nil {
		return memory.Value{}, fmt.Errorf("runtime: nil task context")
	}
	descriptor, err := context.DerefFunction(function)
	if err != nil {
		return memory.Value{}, err
	}
	if descriptor.Kind != memory.FunctionBytecode {
		return memory.Value{}, fmt.Errorf("%w: %s", ErrNotCallable, function)
	}
	instructions, err := DecodeBytecode(descriptor.Code)
	if err != nil {
		return memory.Value{}, err
	}
	frame := &Frame{
		Function:     function,
		Environment:  descriptor.Environment,
		This:         memory.UndefinedValue(),
		Arguments:    append([]memory.Value(nil), arguments...),
		function:     descriptor,
		instructions: instructions,
	}
	return interpreter.runFrame(frame)
}

func (interpreter *Interpreter) runFrame(frame *Frame) (memory.Value, error) {
	for steps := uint64(0); ; steps++ {
		if steps >= interpreter.config.MaxInstructions {
			return memory.Value{}, ErrInstructionLimit
		}
		instruction, err := frame.next()
		if err != nil {
			return memory.Value{}, err
		}
		switch instruction.Op {
		case OpConstant:
			if uint64(instruction.A) >= uint64(len(frame.function.Constants)) {
				return memory.Value{}, fmt.Errorf("%w: %d", ErrConstantBounds, instruction.A)
			}
			frame.push(frame.function.Constants[instruction.A])
		case OpArgument:
			if uint64(instruction.A) >= uint64(len(frame.Arguments)) {
				frame.push(memory.UndefinedValue())
			} else {
				frame.push(frame.Arguments[instruction.A])
			}
		case OpUndefined:
			frame.push(memory.UndefinedValue())
		case OpNull:
			frame.push(memory.NullValue())
		case OpTrue:
			frame.push(memory.BoolValue(true))
		case OpFalse:
			frame.push(memory.BoolValue(false))
		case OpPop:
			if _, err := frame.pop(); err != nil {
				return memory.Value{}, err
			}
		case OpDup:
			value, err := frame.peek()
			if err != nil {
				return memory.Value{}, err
			}
			frame.push(value)
		case OpReturn:
			if len(frame.Stack) == 0 {
				return memory.UndefinedValue(), nil
			}
			return frame.pop()
		default:
			return memory.Value{}, fmt.Errorf("%w: unimplemented %s", ErrInvalidBytecode, instruction.Op)
		}
	}
}
