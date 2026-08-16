package runtime

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"sync"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

var (
	ErrInstructionLimit = errors.New("runtime: instruction limit exceeded")
	ErrCallDepth        = errors.New("runtime: call depth limit exceeded")
	ErrNotCallable      = errors.New("runtime: value is not a callable Function")
	ErrNativeFunction   = errors.New("runtime: native Function is not registered")
	ErrOperandType      = errors.New("runtime: invalid operand type")
)

const defaultMaxInstructions uint64 = 1_000_000

type InterpreterConfig struct {
	MaxInstructions uint64
	MaxCallDepth    uint32
}

// NativeFunction is an explicitly registered host callback. Native Function
// heap payloads retain only its numeric ID, never this Go function value.
type NativeFunction func(*TaskContext, memory.Value, []memory.Value) (memory.Value, error)

// Interpreter executes native Function descriptors against one TaskContext.
// It does not parse source, schedule work, or extend value lifetimes.
type Interpreter struct {
	config InterpreterConfig

	nativeMutex sync.RWMutex
	natives     map[uint64]NativeFunction
}

func NewInterpreter(config InterpreterConfig) *Interpreter {
	if config.MaxInstructions == 0 {
		config.MaxInstructions = defaultMaxInstructions
	}
	if config.MaxCallDepth == 0 {
		config.MaxCallDepth = 256
	}
	return &Interpreter{config: config, natives: make(map[uint64]NativeFunction)}
}

func (interpreter *Interpreter) RegisterNative(id uint64, function NativeFunction) error {
	if interpreter == nil {
		return fmt.Errorf("runtime: nil interpreter")
	}
	if id == 0 || function == nil {
		return fmt.Errorf("%w: ID %d", ErrNativeFunction, id)
	}
	interpreter.nativeMutex.Lock()
	defer interpreter.nativeMutex.Unlock()
	if _, exists := interpreter.natives[id]; exists {
		return fmt.Errorf("%w: duplicate ID %d", ErrNativeFunction, id)
	}
	interpreter.natives[id] = function
	return nil
}

type execution struct {
	interpreter *Interpreter
	context     *TaskContext
	steps       uint64
	depth       uint32
}

type callMode uint8

const (
	callAny callMode = iota
	callNativeOnly
)

func (interpreter *Interpreter) Execute(context *TaskContext, function memory.Ref, arguments ...memory.Value) (memory.Value, error) {
	if interpreter == nil {
		return memory.Value{}, fmt.Errorf("runtime: nil interpreter")
	}
	if context == nil || context.Realm == nil {
		return memory.Value{}, fmt.Errorf("runtime: nil task context")
	}
	execution := &execution{interpreter: interpreter, context: context}
	return execution.call(function, memory.UndefinedValue(), arguments, callAny)
}

func (execution *execution) call(function memory.Ref, this memory.Value, arguments []memory.Value, mode callMode) (memory.Value, error) {
	if execution.depth >= execution.interpreter.config.MaxCallDepth {
		return memory.Value{}, ErrCallDepth
	}
	descriptor, err := execution.context.DerefFunction(function)
	if err != nil {
		return memory.Value{}, err
	}
	if mode == callNativeOnly && descriptor.Kind != memory.FunctionNative {
		return memory.Value{}, fmt.Errorf("%w: %s", ErrNotCallable, function)
	}
	execution.depth++
	defer func() { execution.depth-- }()
	if descriptor.Kind == memory.FunctionNative {
		execution.interpreter.nativeMutex.RLock()
		native := execution.interpreter.natives[descriptor.NativeID]
		execution.interpreter.nativeMutex.RUnlock()
		if native == nil {
			return memory.Value{}, fmt.Errorf("%w: ID %d", ErrNativeFunction, descriptor.NativeID)
		}
		return native(execution.context, this, append([]memory.Value(nil), arguments...))
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
		This:         this,
		Arguments:    append([]memory.Value(nil), arguments...),
		function:     descriptor,
		instructions: instructions,
	}
	if err := validateFrameProgram(frame); err != nil {
		return memory.Value{}, err
	}
	return execution.runFrame(frame)
}

func (execution *execution) runFrame(frame *Frame) (memory.Value, error) {
	context := execution.context
	for {
		if execution.steps >= execution.interpreter.config.MaxInstructions {
			return memory.Value{}, ErrInstructionLimit
		}
		execution.steps++
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
		case OpLoadBinding:
			environment, name, err := bindingOperands(frame, instruction.A)
			if err != nil {
				return memory.Value{}, err
			}
			value, found, err := context.ResolveBinding(environment, name)
			if err != nil {
				return memory.Value{}, err
			}
			if !found {
				return memory.Value{}, fmt.Errorf("%w: constant %d", memory.ErrBindingNotFound, instruction.A)
			}
			frame.push(value)
		case OpDeclareBinding:
			if instruction.B > 1 {
				return memory.Value{}, fmt.Errorf("%w: DeclareBinding mutability %d", ErrInvalidBytecode, instruction.B)
			}
			environment, name, err := bindingOperands(frame, instruction.A)
			if err != nil {
				return memory.Value{}, err
			}
			if err := context.DeclareBinding(environment, name, instruction.B == 1); err != nil {
				return memory.Value{}, err
			}
		case OpInitializeBinding:
			value, err := frame.pop()
			if err != nil {
				return memory.Value{}, err
			}
			environment, name, err := bindingOperands(frame, instruction.A)
			if err != nil {
				return memory.Value{}, err
			}
			if err := context.InitializeBinding(environment, name, value); err != nil {
				return memory.Value{}, err
			}
			frame.push(value)
		case OpStoreBinding:
			value, err := frame.pop()
			if err != nil {
				return memory.Value{}, err
			}
			environment, name, err := bindingOperands(frame, instruction.A)
			if err != nil {
				return memory.Value{}, err
			}
			if err := context.SetBinding(environment, name, value); err != nil {
				return memory.Value{}, err
			}
			frame.push(value)
		case OpLoadThis:
			frame.push(frame.This)
		case OpAdd, OpSubtract, OpMultiply, OpDivide, OpRemainder,
			OpNegate, OpIncrement, OpDecrement,
			OpBitwiseAnd, OpBitwiseOr, OpBitwiseXor,
			OpShiftLeft, OpShiftRight, OpUnsignedShiftRight,
			OpLogicalNot, OpTypeOf,
			OpStrictEqual, OpStrictNotEqual,
			OpLessThan, OpLessThanOrEqual, OpGreaterThan, OpGreaterThanOrEqual:
			if err := executeOperator(context, frame, instruction.Op); err != nil {
				return memory.Value{}, err
			}
		case OpJump:
			frame.ip = instruction.A
		case OpJumpIfTrue, OpJumpIfFalse, OpJumpIfNullish:
			condition, err := frame.pop()
			if err != nil {
				return memory.Value{}, err
			}
			jump := condition.Kind() == memory.ValueUndefined || condition.Kind() == memory.ValueNull
			if instruction.Op != OpJumpIfNullish {
				truthy, err := valueTruthy(context, condition)
				if err != nil {
					return memory.Value{}, err
				}
				jump = truthy == (instruction.Op == OpJumpIfTrue)
			}
			if jump {
				frame.ip = instruction.A
			}
		case OpCall, OpCallNative, OpConstruct:
			callee, arguments, err := popCallOperands(frame, instruction.A)
			if err != nil {
				return memory.Value{}, err
			}
			this := memory.UndefinedValue()
			mode := callAny
			var constructed memory.Ref
			if instruction.Op == OpCallNative {
				mode = callNativeOnly
			}
			if instruction.Op == OpConstruct {
				constructed, err = context.NewHeapObject()
				if err != nil {
					return memory.Value{}, err
				}
				this = memory.RefValue(constructed)
			}
			result, err := execution.call(callee, this, arguments, mode)
			if err != nil {
				return memory.Value{}, err
			}
			if instruction.Op == OpConstruct {
				object, err := isObjectValue(context, result)
				if err != nil {
					return memory.Value{}, err
				}
				if !object {
					result = memory.RefValue(constructed)
				}
			}
			frame.push(result)
		case OpCreateClosure:
			template, err := constantRef(frame, instruction.A, "Function template")
			if err != nil {
				return memory.Value{}, err
			}
			descriptor, err := context.DerefFunction(template)
			if err != nil {
				return memory.Value{}, err
			}
			var closure memory.Ref
			switch descriptor.Kind {
			case memory.FunctionBytecode:
				closure, err = context.NewBytecodeFunction(descriptor.Name, frame.Environment, descriptor.Arity, descriptor.Code, descriptor.Constants)
			case memory.FunctionNative:
				closure, err = context.NewNativeFunction(descriptor.Name, frame.Environment, descriptor.Arity, descriptor.NativeID)
			default:
				err = fmt.Errorf("%w: template %s", ErrNotCallable, template)
			}
			if err != nil {
				return memory.Value{}, err
			}
			frame.push(memory.RefValue(closure))
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

func bindingOperands(frame *Frame, constant uint32) (memory.Ref, memory.Ref, error) {
	environment, err := requireRef(frame.Environment, "Function environment Context")
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	name, err := constantRef(frame, constant, "binding name")
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	return environment, name, nil
}

func constantRef(frame *Frame, index uint32, label string) (memory.Ref, error) {
	if frame == nil || uint64(index) >= uint64(len(frame.function.Constants)) {
		return memory.Ref{}, fmt.Errorf("%w: %d", ErrConstantBounds, index)
	}
	return requireRef(frame.function.Constants[index], label)
}

func validateFrameProgram(frame *Frame) error {
	if frame == nil {
		return fmt.Errorf("%w: nil frame", ErrInvalidBytecode)
	}
	for index, instruction := range frame.instructions {
		switch instruction.Op {
		case OpConstant, OpLoadBinding, OpDeclareBinding, OpInitializeBinding, OpStoreBinding, OpCreateClosure:
			if uint64(instruction.A) >= uint64(len(frame.function.Constants)) {
				return fmt.Errorf("%w: instruction %d uses constant %d", ErrConstantBounds, index, instruction.A)
			}
		case OpJump, OpJumpIfTrue, OpJumpIfFalse, OpJumpIfNullish:
			if uint64(instruction.A) >= uint64(len(frame.instructions)) {
				return fmt.Errorf("%w: instruction %d jumps to %d", ErrInvalidBytecode, index, instruction.A)
			}
		}
		if instruction.Op == OpDeclareBinding && instruction.B > 1 {
			return fmt.Errorf("%w: DeclareBinding mutability %d", ErrInvalidBytecode, instruction.B)
		}
	}
	return nil
}

func popCallOperands(frame *Frame, count uint32) (memory.Ref, []memory.Value, error) {
	if uint64(count) > uint64(len(frame.Stack)) {
		return memory.Ref{}, nil, ErrStackUnderflow
	}
	arguments := make([]memory.Value, count)
	for index := int(count) - 1; index >= 0; index-- {
		value, err := frame.pop()
		if err != nil {
			return memory.Ref{}, nil, err
		}
		arguments[index] = value
	}
	calleeValue, err := frame.pop()
	if err != nil {
		return memory.Ref{}, nil, err
	}
	callee, err := requireRef(calleeValue, "callee Function")
	if err != nil {
		return memory.Ref{}, nil, err
	}
	return callee, arguments, nil
}

func isObjectValue(context *TaskContext, value memory.Value) (bool, error) {
	if !value.IsRef() {
		return false, nil
	}
	kind, err := context.HeapKind(value.Ref())
	if err != nil {
		return false, err
	}
	switch kind {
	case memory.HeapString, memory.HeapBigInt, memory.HeapSymbol:
		return false, nil
	default:
		return true, nil
	}
}

func executeOperator(context *TaskContext, frame *Frame, opcode Opcode) error {
	switch opcode {
	case OpNegate, OpIncrement, OpDecrement:
		value, err := popNumber(frame, opcode.String())
		if err != nil {
			return err
		}
		switch opcode {
		case OpNegate:
			value = -value
		case OpIncrement:
			value++
		case OpDecrement:
			value--
		}
		frame.push(memory.NumberValue(value))
		return nil
	case OpLogicalNot:
		value, err := frame.pop()
		if err != nil {
			return err
		}
		truthy, err := valueTruthy(context, value)
		if err != nil {
			return err
		}
		frame.push(memory.BoolValue(!truthy))
		return nil
	case OpTypeOf:
		value, err := frame.pop()
		if err != nil {
			return err
		}
		name, err := valueTypeName(context, value)
		if err != nil {
			return err
		}
		ref, err := context.NewString(name)
		if err != nil {
			return err
		}
		frame.push(memory.RefValue(ref))
		return nil
	case OpStrictEqual, OpStrictNotEqual:
		right, left, err := popBinary(frame)
		if err != nil {
			return err
		}
		equal, err := strictEqual(context, left, right)
		if err != nil {
			return err
		}
		if opcode == OpStrictNotEqual {
			equal = !equal
		}
		frame.push(memory.BoolValue(equal))
		return nil
	case OpAdd:
		right, left, err := popBinary(frame)
		if err != nil {
			return err
		}
		if left.Kind() == memory.ValueNumber && right.Kind() == memory.ValueNumber {
			frame.push(memory.NumberValue(left.Number() + right.Number()))
			return nil
		}
		if left.IsRef() && right.IsRef() {
			leftKind, leftErr := context.HeapKind(left.Ref())
			rightKind, rightErr := context.HeapKind(right.Ref())
			if leftErr != nil {
				return leftErr
			}
			if rightErr != nil {
				return rightErr
			}
			if leftKind == memory.HeapString && rightKind == memory.HeapString {
				leftText, err := context.DerefString(left.Ref())
				if err != nil {
					return err
				}
				rightText, err := context.DerefString(right.Ref())
				if err != nil {
					return err
				}
				ref, err := context.NewString(leftText + rightText)
				if err != nil {
					return err
				}
				frame.push(memory.RefValue(ref))
				return nil
			}
		}
		return fmt.Errorf("%w: Add requires two numbers or two Strings", ErrOperandType)
	case OpSubtract, OpMultiply, OpDivide, OpRemainder,
		OpBitwiseAnd, OpBitwiseOr, OpBitwiseXor,
		OpShiftLeft, OpShiftRight, OpUnsignedShiftRight,
		OpLessThan, OpLessThanOrEqual, OpGreaterThan, OpGreaterThanOrEqual:
		right, left, err := popNumberPair(frame, opcode.String())
		if err != nil {
			return err
		}
		switch opcode {
		case OpSubtract:
			frame.push(memory.NumberValue(left - right))
		case OpMultiply:
			frame.push(memory.NumberValue(left * right))
		case OpDivide:
			frame.push(memory.NumberValue(left / right))
		case OpRemainder:
			frame.push(memory.NumberValue(math.Mod(left, right)))
		case OpBitwiseAnd:
			frame.push(memory.NumberValue(float64(toInt32(left) & toInt32(right))))
		case OpBitwiseOr:
			frame.push(memory.NumberValue(float64(toInt32(left) | toInt32(right))))
		case OpBitwiseXor:
			frame.push(memory.NumberValue(float64(toInt32(left) ^ toInt32(right))))
		case OpShiftLeft:
			frame.push(memory.NumberValue(float64(toInt32(left) << (toUint32(right) & 31))))
		case OpShiftRight:
			frame.push(memory.NumberValue(float64(toInt32(left) >> (toUint32(right) & 31))))
		case OpUnsignedShiftRight:
			frame.push(memory.NumberValue(float64(toUint32(left) >> (toUint32(right) & 31))))
		case OpLessThan:
			frame.push(memory.BoolValue(left < right))
		case OpLessThanOrEqual:
			frame.push(memory.BoolValue(left <= right))
		case OpGreaterThan:
			frame.push(memory.BoolValue(left > right))
		case OpGreaterThanOrEqual:
			frame.push(memory.BoolValue(left >= right))
		}
		return nil
	default:
		return fmt.Errorf("%w: operator %s", ErrInvalidBytecode, opcode)
	}
}

func popBinary(frame *Frame) (memory.Value, memory.Value, error) {
	right, err := frame.pop()
	if err != nil {
		return memory.Value{}, memory.Value{}, err
	}
	left, err := frame.pop()
	if err != nil {
		return memory.Value{}, memory.Value{}, err
	}
	return right, left, nil
}

func popNumber(frame *Frame, label string) (float64, error) {
	value, err := frame.pop()
	if err != nil {
		return 0, err
	}
	if value.Kind() != memory.ValueNumber {
		return 0, fmt.Errorf("%w: %s requires a number", ErrOperandType, label)
	}
	return value.Number(), nil
}

func popNumberPair(frame *Frame, label string) (float64, float64, error) {
	right, err := popNumber(frame, label)
	if err != nil {
		return 0, 0, err
	}
	left, err := popNumber(frame, label)
	if err != nil {
		return 0, 0, err
	}
	return right, left, nil
}

func strictEqual(context *TaskContext, left, right memory.Value) (bool, error) {
	if left.Kind() != right.Kind() {
		return false, nil
	}
	switch left.Kind() {
	case memory.ValueUndefined, memory.ValueNull:
		return true, nil
	case memory.ValueBool:
		return left.Bool() == right.Bool(), nil
	case memory.ValueNumber:
		return !math.IsNaN(left.Number()) && !math.IsNaN(right.Number()) && left.Number() == right.Number(), nil
	case memory.ValueReference:
		if left.Ref() == right.Ref() {
			return true, nil
		}
		leftKind, err := context.HeapKind(left.Ref())
		if err != nil {
			return false, err
		}
		rightKind, err := context.HeapKind(right.Ref())
		if err != nil {
			return false, err
		}
		if leftKind != rightKind {
			return false, nil
		}
		switch leftKind {
		case memory.HeapString:
			leftText, err := context.DerefString(left.Ref())
			if err != nil {
				return false, err
			}
			rightText, err := context.DerefString(right.Ref())
			return leftText == rightText, err
		case memory.HeapBigInt:
			leftValue, err := context.DerefBigInt(left.Ref())
			if err != nil {
				return false, err
			}
			rightValue, err := context.DerefBigInt(right.Ref())
			return leftValue.Negative == rightValue.Negative && bytes.Equal(leftValue.Magnitude, rightValue.Magnitude), err
		case memory.HeapSymbol:
			leftValue, err := context.DerefSymbol(left.Ref())
			if err != nil {
				return false, err
			}
			rightValue, err := context.DerefSymbol(right.Ref())
			return leftValue.ID == rightValue.ID, err
		default:
			return false, nil
		}
	default:
		return false, fmt.Errorf("%w: unknown Value kind %d", ErrOperandType, left.Kind())
	}
}

func valueTruthy(context *TaskContext, value memory.Value) (bool, error) {
	switch value.Kind() {
	case memory.ValueUndefined, memory.ValueNull:
		return false, nil
	case memory.ValueBool:
		return value.Bool(), nil
	case memory.ValueNumber:
		return value.Number() != 0 && !math.IsNaN(value.Number()), nil
	case memory.ValueReference:
		kind, err := context.HeapKind(value.Ref())
		if err != nil {
			return false, err
		}
		switch kind {
		case memory.HeapString:
			text, err := context.DerefString(value.Ref())
			return text != "", err
		case memory.HeapBigInt:
			bigint, err := context.DerefBigInt(value.Ref())
			return len(bigint.Magnitude) != 0, err
		default:
			return true, nil
		}
	default:
		return false, fmt.Errorf("%w: unknown Value kind %d", ErrOperandType, value.Kind())
	}
}

func valueTypeName(context *TaskContext, value memory.Value) (string, error) {
	switch value.Kind() {
	case memory.ValueUndefined:
		return "undefined", nil
	case memory.ValueNull:
		return "object", nil
	case memory.ValueBool:
		return "boolean", nil
	case memory.ValueNumber:
		return "number", nil
	case memory.ValueReference:
		kind, err := context.HeapKind(value.Ref())
		if err != nil {
			return "", err
		}
		switch kind {
		case memory.HeapString:
			return "string", nil
		case memory.HeapBigInt:
			return "bigint", nil
		case memory.HeapSymbol:
			return "symbol", nil
		case memory.HeapFunction:
			return "function", nil
		default:
			return "object", nil
		}
	default:
		return "", fmt.Errorf("%w: unknown Value kind %d", ErrOperandType, value.Kind())
	}
}

func toUint32(number float64) uint32 {
	if number == 0 || math.IsNaN(number) || math.IsInf(number, 0) {
		return 0
	}
	integer := math.Trunc(number)
	modulo := math.Mod(integer, 4294967296)
	if modulo < 0 {
		modulo += 4294967296
	}
	return uint32(modulo)
}

func toInt32(number float64) int32 {
	return int32(toUint32(number))
}
