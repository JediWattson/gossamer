package runtime

import (
	"errors"
	"fmt"
	"math"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

var (
	ErrInstructionLimit = errors.New("runtime: instruction limit exceeded")
	ErrNotCallable      = errors.New("runtime: value is not a callable bytecode Function")
	ErrOperandType      = errors.New("runtime: invalid operand type")
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
	return interpreter.runFrame(context, frame)
}

func (interpreter *Interpreter) runFrame(context *TaskContext, frame *Frame) (memory.Value, error) {
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
		case OpNewObject:
			ref, err := context.NewHeapObject()
			if err != nil {
				return memory.Value{}, err
			}
			frame.push(memory.RefValue(ref))
		case OpGetOwnProperty:
			name, object, err := popRefPair(frame, "Object", "property name")
			if err != nil {
				return memory.Value{}, err
			}
			value, present, err := context.GetOwnProperty(object, name)
			if err != nil {
				return memory.Value{}, err
			}
			if !present {
				value = memory.UndefinedValue()
			}
			frame.push(value)
		case OpSetOwnProperty:
			value, err := frame.pop()
			if err != nil {
				return memory.Value{}, err
			}
			name, object, err := popRefPair(frame, "Object", "property name")
			if err != nil {
				return memory.Value{}, err
			}
			if err := context.SetProperty(object, name, value); err != nil {
				return memory.Value{}, err
			}
			frame.push(value)
		case OpDeleteOwnProperty:
			name, object, err := popRefPair(frame, "Object", "property name")
			if err != nil {
				return memory.Value{}, err
			}
			deleted, err := context.DeleteProperty(object, name)
			if err != nil {
				return memory.Value{}, err
			}
			frame.push(memory.BoolValue(deleted))
		case OpNewArray:
			ref, err := context.NewArray(instruction.A)
			if err != nil {
				return memory.Value{}, err
			}
			frame.push(memory.RefValue(ref))
		case OpGetElement:
			index, array, err := popArrayIndex(frame)
			if err != nil {
				return memory.Value{}, err
			}
			value, present, err := context.ArrayElement(array, index)
			if err != nil {
				return memory.Value{}, err
			}
			if !present {
				value = memory.UndefinedValue()
			}
			frame.push(value)
		case OpSetElement:
			value, err := frame.pop()
			if err != nil {
				return memory.Value{}, err
			}
			index, array, err := popArrayIndex(frame)
			if err != nil {
				return memory.Value{}, err
			}
			if err := context.SetArrayElement(array, index, value); err != nil {
				return memory.Value{}, err
			}
			frame.push(value)
		case OpDeleteElement:
			index, array, err := popArrayIndex(frame)
			if err != nil {
				return memory.Value{}, err
			}
			deleted, err := context.DeleteArrayElement(array, index)
			if err != nil {
				return memory.Value{}, err
			}
			frame.push(memory.BoolValue(deleted))
		case OpGetLength:
			arrayValue, err := frame.pop()
			if err != nil {
				return memory.Value{}, err
			}
			array, err := requireRef(arrayValue, "Array")
			if err != nil {
				return memory.Value{}, err
			}
			snapshot, err := context.DerefArray(array)
			if err != nil {
				return memory.Value{}, err
			}
			frame.push(memory.NumberValue(float64(snapshot.Length)))
		case OpSetLength:
			lengthValue, err := frame.pop()
			if err != nil {
				return memory.Value{}, err
			}
			arrayValue, err := frame.pop()
			if err != nil {
				return memory.Value{}, err
			}
			array, err := requireRef(arrayValue, "Array")
			if err != nil {
				return memory.Value{}, err
			}
			length, err := requireUint32(lengthValue, "Array length", true)
			if err != nil {
				return memory.Value{}, err
			}
			if err := context.SetArrayLength(array, length); err != nil {
				return memory.Value{}, err
			}
			frame.push(lengthValue)
		default:
			return memory.Value{}, fmt.Errorf("%w: unimplemented %s", ErrInvalidBytecode, instruction.Op)
		}
	}
}

func requireRef(value memory.Value, label string) (memory.Ref, error) {
	if !value.IsRef() {
		return memory.Ref{}, fmt.Errorf("%w: %s must be a Ref", ErrOperandType, label)
	}
	return value.Ref(), nil
}

// popRefPair consumes [... first, second] and returns second, first. Property
// operations use this to return the name before the containing Object.
func popRefPair(frame *Frame, firstLabel, secondLabel string) (memory.Ref, memory.Ref, error) {
	secondValue, err := frame.pop()
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	firstValue, err := frame.pop()
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	second, err := requireRef(secondValue, secondLabel)
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	first, err := requireRef(firstValue, firstLabel)
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	return second, first, nil
}

func popArrayIndex(frame *Frame) (uint32, memory.Ref, error) {
	indexValue, err := frame.pop()
	if err != nil {
		return 0, memory.Ref{}, err
	}
	arrayValue, err := frame.pop()
	if err != nil {
		return 0, memory.Ref{}, err
	}
	array, err := requireRef(arrayValue, "Array")
	if err != nil {
		return 0, memory.Ref{}, err
	}
	index, err := requireUint32(indexValue, "Array index", false)
	if err != nil {
		return 0, memory.Ref{}, err
	}
	return index, array, nil
}

func requireUint32(value memory.Value, label string, allowMaximum bool) (uint32, error) {
	if value.Kind() != memory.ValueNumber {
		return 0, fmt.Errorf("%w: %s must be a number", ErrOperandType, label)
	}
	number := value.Number()
	maximum := float64(math.MaxUint32)
	if math.IsNaN(number) || math.IsInf(number, 0) || number < 0 || number != math.Trunc(number) || number > maximum || !allowMaximum && number == maximum {
		return 0, fmt.Errorf("%w: %s %v is outside uint32 range", ErrOperandType, label, number)
	}
	return uint32(number), nil
}
