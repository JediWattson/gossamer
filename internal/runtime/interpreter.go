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
	ErrMicrotaskLimit   = errors.New("runtime: microtask checkpoint limit exceeded")
	ErrCallDepth        = errors.New("runtime: call depth limit exceeded")
	ErrNotCallable      = errors.New("runtime: value is not a callable Function")
	ErrNotConstructor   = errors.New("runtime: Function is not a constructor")
	ErrNativeFunction   = errors.New("runtime: native Function is not registered")
	ErrOperandType      = errors.New("runtime: invalid operand type")
	ErrExceptionState   = errors.New("runtime: invalid exception state")
	errFrameCall        = errors.New("runtime: bytecode frame call")
)

const (
	defaultMaxInstructions uint64 = 10_000_000
	defaultMaxMicrotasks   uint64 = 100_000
)

type InterpreterConfig struct {
	// MaxInstructions bounds control-flow progress independently for a
	// top-level call and each microtask job. Function activations, completions,
	// and loop backedges are charged; compiler lowering details are not.
	// MaxMicrotasks bounds the complete checkpoint.
	MaxInstructions     uint64
	MaxMicrotasks       uint64
	MaxCallDepth        uint32
	ProfileInstructions bool
	// ExecutionSample is an optional sparse diagnostic hook. It observes the
	// current borrowed execution state without retaining any Ref. A zero
	// interval disables sampling.
	ExecutionSampleInterval uint64
	ExecutionSample         func(*TaskContext, ExecutionSample) error
	// PropertyMiss observes absent named-property reads without exposing the
	// receiver Ref or retaining heap ownership. It is intended for bounded,
	// opt-in compatibility diagnostics.
	PropertyMiss func(PropertyMissSample)
}

// ExecutionSample identifies one sparse point inside a running bytecode call.
// RawInstructions is deliberately a diagnostic counter rather than the
// lowering-independent runaway-script budget.
type ExecutionSample struct {
	TaskID          TaskID
	RawInstructions uint64
	ControlSteps    uint64
	StackDepth      int
	Function        memory.Ref
	FunctionName    string
	FunctionHash    uint64
	Instruction     uint32
	Opcode          Opcode
}

// PropertyMissSample describes an absent named-property read. BaseKind is the
// compact Value representation; HeapKind further classifies reference values.
type PropertyMissSample struct {
	BaseKind memory.ValueKind
	HeapKind memory.HeapKind
	Name     string
}

// NativeFunction is an explicitly registered host callback. Native Function
// heap payloads retain only its numeric ID, never this Go function value.
type NativeFunction func(*TaskContext, memory.Value, []memory.Value) (memory.Value, error)

// PropertyInterceptor supplies embedder-owned named properties for ordinary
// heap Objects after their own and inherited descriptors have been checked.
// Handled distinguishes an absent intercepted property from an Object that is
// not owned by this interceptor. Heap payloads retain no Go callback pointer.
type PropertyInterceptor struct {
	Get    func(*TaskContext, memory.Ref, string) (value memory.Value, found, handled bool, err error)
	Set    func(*TaskContext, memory.Ref, string, memory.Value) (handled bool, err error)
	Delete func(*TaskContext, memory.Ref, string) (deleted, handled bool, err error)
}

type nativeFunction func(*execution, memory.Ref, memory.Function, memory.Value, []memory.Value) (memory.Value, error)

// ThrownError carries an interpreter Value through Go call frames without
// converting it into a host-language error object.
type ThrownError struct {
	Value memory.Value
}

func (thrown *ThrownError) Error() string {
	if thrown == nil {
		return "runtime: thrown <nil>"
	}
	if thrown.Value.IsRef() {
		return fmt.Sprintf("runtime: thrown %s", thrown.Value.Ref())
	}
	return fmt.Sprintf("runtime: thrown Value(%d)", thrown.Value.Kind())
}

func Throw(value memory.Value) error {
	return &ThrownError{Value: value}
}

func ThrownValue(err error) (memory.Value, bool) {
	var thrown *ThrownError
	if !errors.As(err, &thrown) || thrown == nil {
		return memory.Value{}, false
	}
	return thrown.Value, true
}

// Interpreter executes native Function descriptors against one TaskContext.
// It does not parse source, schedule work, or extend value lifetimes.
type Interpreter struct {
	config InterpreterConfig

	nativeMutex sync.RWMutex
	natives     map[uint64]nativeFunction
	properties  []PropertyInterceptor
	builtinOnce sync.Once
	builtinErr  error

	jobMutex sync.Mutex
	jobs     map[TaskID][]microtaskJob

	programMutex sync.Mutex
	programs     map[bytecodeCacheKey][]*cachedBytecode
	programClock []*cachedBytecode
	programAt    int
}

func NewInterpreter(config InterpreterConfig) *Interpreter {
	if config.MaxInstructions == 0 {
		config.MaxInstructions = defaultMaxInstructions
	}
	if config.MaxMicrotasks == 0 {
		config.MaxMicrotasks = defaultMaxMicrotasks
	}
	if config.MaxCallDepth == 0 {
		config.MaxCallDepth = 256
	}
	return &Interpreter{
		config:   config,
		natives:  make(map[uint64]nativeFunction),
		jobs:     make(map[TaskID][]microtaskJob),
		programs: make(map[bytecodeCacheKey][]*cachedBytecode),
	}
}

func (interpreter *Interpreter) RegisterNative(id uint64, function NativeFunction) error {
	if interpreter == nil {
		return fmt.Errorf("runtime: nil interpreter")
	}
	if id == 0 || id >= nativeBuiltinBase || function == nil {
		return fmt.Errorf("%w: ID %d", ErrNativeFunction, id)
	}
	interpreter.nativeMutex.Lock()
	defer interpreter.nativeMutex.Unlock()
	if _, exists := interpreter.natives[id]; exists {
		return fmt.Errorf("%w: duplicate ID %d", ErrNativeFunction, id)
	}
	interpreter.natives[id] = func(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
		return function(execution.context, this, arguments)
	}
	return nil
}

// RegisterPropertyInterceptor adds one execution-scoped embedder property
// provider. Interceptors are queried in registration order only when ordinary
// descriptor lookup did not resolve the name.
func (interpreter *Interpreter) RegisterPropertyInterceptor(interceptor PropertyInterceptor) error {
	if interpreter == nil {
		return fmt.Errorf("runtime: nil interpreter")
	}
	if interceptor.Get == nil && interceptor.Set == nil && interceptor.Delete == nil {
		return fmt.Errorf("runtime: empty property interceptor")
	}
	interpreter.nativeMutex.Lock()
	interpreter.properties = append(interpreter.properties, interceptor)
	interpreter.nativeMutex.Unlock()
	return nil
}

func (interpreter *Interpreter) propertyInterceptors() []PropertyInterceptor {
	interpreter.nativeMutex.RLock()
	result := append([]PropertyInterceptor(nil), interpreter.properties...)
	interpreter.nativeMutex.RUnlock()
	return result
}

func (interpreter *Interpreter) registerNative(id uint64, function nativeFunction) error {
	if interpreter == nil || id == 0 || function == nil {
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
	budgetSteps uint64
	profile     *instructionProfile
}

type callMode uint8

const (
	callAny callMode = iota
	callNativeOnly
)

func (interpreter *Interpreter) Execute(context *TaskContext, function memory.Ref, arguments ...memory.Value) (memory.Value, error) {
	result, callErr := interpreter.ExecuteWithoutCheckpoint(context, function, arguments...)
	jobErr := interpreter.DrainJobs(context)
	return result, errors.Join(callErr, jobErr)
}

// ExecuteWithoutCheckpoint invokes one Function but leaves native Promise and
// queueMicrotask jobs pending. Browser embedders use this form so the existing
// JSRealm.DrainMicrotasks boundary remains the one observable checkpoint.
func (interpreter *Interpreter) ExecuteWithoutCheckpoint(context *TaskContext, function memory.Ref, arguments ...memory.Value) (memory.Value, error) {
	return interpreter.CallWithoutCheckpoint(context, function, memory.UndefinedValue(), arguments...)
}

// CallWithoutCheckpoint invokes one Function with an explicit this value and
// leaves native Promise and queueMicrotask jobs pending. Host adapters use it
// for browser callbacks whose receiver is observable, such as event listeners.
func (interpreter *Interpreter) CallWithoutCheckpoint(context *TaskContext, function memory.Ref, this memory.Value, arguments ...memory.Value) (memory.Value, error) {
	if interpreter == nil {
		return memory.Value{}, fmt.Errorf("runtime: nil interpreter")
	}
	if context == nil || context.Realm == nil {
		return memory.Value{}, fmt.Errorf("runtime: nil task context")
	}
	execution := interpreter.newExecution(context)
	return execution.call(function, this, arguments, callAny)
}

// DrainJobs runs the current task's queued native microtasks in FIFO order.
// Execute already calls it at its top-level checkpoint; embedders may call it
// after explicitly enqueueing jobs outside bytecode execution.
func (interpreter *Interpreter) DrainJobs(context *TaskContext) error {
	if interpreter == nil || context == nil || context.Realm == nil {
		return fmt.Errorf("runtime: nil interpreter or task context")
	}
	execution := interpreter.newExecution(context)
	err := execution.drainJobs()
	if err != nil {
		// A failed checkpoint cannot leave borrowed task refs behind after the
		// task owner is released. ExecuteWithoutCheckpoint remains the explicit
		// API for intentionally pending work before this boundary.
		interpreter.DiscardJobs(context.TaskID)
	}
	return err
}

// DiscardJobs forgets pending borrowed callbacks for a task that is being
// abandoned before its normal checkpoint. It never changes Ref ownership.
func (interpreter *Interpreter) DiscardJobs(task TaskID) {
	if interpreter == nil || task == 0 {
		return
	}
	interpreter.jobMutex.Lock()
	delete(interpreter.jobs, task)
	interpreter.jobMutex.Unlock()
}

func (execution *execution) call(function memory.Ref, this memory.Value, arguments []memory.Value, mode callMode) (result memory.Value, resultErr error) {
	pinning := execution.context.pinNativeAllocations
	execution.context.pinNativeAllocations = false
	defer func() {
		execution.context.pinNativeAllocations = pinning
		if pinning && resultErr == nil {
			execution.context.trackNativeResult(result)
		}
	}()
	stack := execution.context.stack
	if stack == nil {
		stack = &Stack{}
		execution.context.stack = stack
	}
	descriptor, this, err := execution.prepareCall(stack, function, this, mode)
	if err != nil {
		return memory.Value{}, err
	}
	if descriptor.Kind == memory.FunctionNative {
		return execution.invokeNative(stack, function, descriptor, this, arguments)
	}
	return execution.runBytecodeCall(stack, function, descriptor, this, arguments)
}

func (execution *execution) prepareCall(stack *Stack, function memory.Ref, this memory.Value, mode callMode) (memory.Function, memory.Value, error) {
	// Stack depth is task-shared, unlike execution, so native re-entry through
	// a public Interpreter call cannot reset and bypass the call-depth limit.
	if uint64(stack.Depth()) >= uint64(execution.interpreter.config.MaxCallDepth) {
		return memory.Function{}, memory.Value{}, ErrCallDepth
	}
	descriptor, err := execution.context.LoadFunction(function)
	if err != nil {
		return memory.Function{}, memory.Value{}, err
	}
	if mode == callNativeOnly && descriptor.Kind != memory.FunctionNative {
		return memory.Function{}, memory.Value{}, fmt.Errorf("%w: %s", ErrNotCallable, function)
	}
	if descriptor.ThisMode == memory.FunctionThisLexical {
		this = descriptor.LexicalThis
	}
	if descriptor.Kind != memory.FunctionNative && descriptor.Kind != memory.FunctionBytecode {
		return memory.Function{}, memory.Value{}, fmt.Errorf("%w: %s", ErrNotCallable, function)
	}
	return descriptor, this, nil
}

func (execution *execution) acquireFrame(stack *Stack, function memory.Ref, descriptor memory.Function, this memory.Value, arguments []memory.Value) (*Frame, error) {
	frame := stack.acquireFrame()
	frame.Function = function
	frame.Environment = descriptor.Environment
	frame.This = this
	frame.Arguments = append(frame.Arguments[:0], arguments...)
	frame.function = descriptor
	if err := stack.push(frame); err != nil {
		stack.recycleFrame(frame)
		return nil, err
	}
	return frame, nil
}

func (execution *execution) invokeNative(stack *Stack, function memory.Ref, descriptor memory.Function, this memory.Value, arguments []memory.Value) (result memory.Value, resultErr error) {
	frame, err := execution.acquireFrame(stack, function, descriptor, this, arguments)
	if err != nil {
		return memory.Value{}, err
	}
	previousPinning := execution.context.beginNativeRoots()
	defer func() {
		execution.context.endNativeRoots(previousPinning)
		if err := stack.pop(frame); err != nil {
			resultErr = errors.Join(resultErr, err)
			return
		}
		stack.recycleFrame(frame)
	}()
	execution.interpreter.nativeMutex.RLock()
	native := execution.interpreter.natives[descriptor.NativeID]
	execution.interpreter.nativeMutex.RUnlock()
	if native == nil {
		return memory.Value{}, fmt.Errorf("%w: ID %d", ErrNativeFunction, descriptor.NativeID)
	}
	result, err = native(execution, function, descriptor, this, frame.Arguments)
	if err != nil {
		return memory.Value{}, fmt.Errorf("runtime: native function %s ID %d: %w", function, descriptor.NativeID, err)
	}
	return result, nil
}

func (execution *execution) acquireBytecodeFrame(stack *Stack, function memory.Ref, descriptor memory.Function, this memory.Value, arguments []memory.Value) (*Frame, error) {
	frame, err := execution.acquireFrame(stack, function, descriptor, this, arguments)
	if err != nil {
		return nil, err
	}
	program, err := execution.interpreter.loadProgram(descriptor.Code, len(descriptor.Constants))
	if err != nil {
		_ = stack.pop(frame)
		stack.recycleFrame(frame)
		return nil, err
	}
	frame.instructions = program.instructions
	frame.programHash = program.key.hash
	if execution.profile != nil {
		execution.profile.bind(frame, program, execution.context)
	}
	return frame, nil
}

// runBytecodeCall drives bytecode-to-bytecode calls on the task's explicit
// Frame stack. Native callbacks may still re-enter call, but ordinary JS call
// chains no longer consume one Go goroutine frame per JavaScript frame.
func (execution *execution) runBytecodeCall(stack *Stack, function memory.Ref, descriptor memory.Function, this memory.Value, arguments []memory.Value) (result memory.Value, resultErr error) {
	baseDepth := stack.Depth()
	frame, err := execution.acquireBytecodeFrame(stack, function, descriptor, this, arguments)
	if err != nil {
		return memory.Value{}, err
	}
	defer func() {
		for stack.Depth() > baseDepth {
			index := stack.Depth() - 1
			active := stack.frames[index]
			if popErr := stack.pop(active); popErr != nil {
				resultErr = errors.Join(resultErr, popErr)
				break
			}
			stack.recycleFrame(active)
		}
	}()

	for {
		result, err = execution.runFrame(frame)
		if err == errFrameCall {
			request := frame.call
			if request == nil {
				return memory.Value{}, fmt.Errorf("%w: missing pending call", ErrExceptionState)
			}
			child, childThis, prepareErr := execution.prepareCall(stack, request.function, request.this, request.mode)
			if prepareErr != nil {
				frame.call = nil
				handled, terminal := execution.routeFrameError(frame, prepareErr)
				if handled {
					continue
				}
				err = terminal
			} else if child.Kind == memory.FunctionNative {
				result, err = execution.invokeNative(stack, request.function, child, childThis, request.arguments)
				frame.call = nil
				if err == nil {
					result, err = execution.finishPendingCall(request, result)
				}
				if err == nil {
					frame.push(result)
					continue
				}
				handled, terminal := execution.routeFrameError(frame, err)
				if handled {
					continue
				}
				err = terminal
			} else {
				childFrame, childErr := execution.acquireBytecodeFrame(stack, request.function, child, childThis, request.arguments)
				if childErr == nil {
					request.arguments = nil
					frame = childFrame
					continue
				}
				frame.call = nil
				handled, terminal := execution.routeFrameError(frame, childErr)
				if handled {
					continue
				}
				err = terminal
			}
		}

		for {
			if err != nil {
				err = execution.wrapFrameError(frame, err)
			}
			if popErr := stack.pop(frame); popErr != nil {
				return memory.Value{}, errors.Join(err, popErr)
			}
			stack.recycleFrame(frame)
			if stack.Depth() == baseDepth {
				return result, err
			}

			parent := stack.frames[stack.Depth()-1]
			request := parent.call
			parent.call = nil
			if request == nil {
				return memory.Value{}, fmt.Errorf("%w: caller has no pending continuation", ErrExceptionState)
			}
			if err == nil {
				result, err = execution.finishPendingCall(request, result)
				if err == nil {
					parent.push(result)
					frame = parent
					break
				}
			}
			handled, terminal := execution.routeFrameError(parent, err)
			if handled {
				frame = parent
				err = nil
				break
			}
			frame = parent
			err = terminal
		}
	}
}

func (execution *execution) finishPendingCall(call *frameCall, result memory.Value) (memory.Value, error) {
	if call == nil || !call.construct {
		return result, nil
	}
	object, err := isObjectValue(execution.context, result)
	if err != nil {
		return memory.Value{}, err
	}
	if !object {
		return memory.RefValue(call.constructed), nil
	}
	return result, nil
}

func (execution *execution) wrapFrameError(frame *Frame, err error) error {
	if frame == nil || err == nil {
		return err
	}
	detail := execution.thrownSummary(err)
	if detail != "" {
		err = fmt.Errorf("%s: %w", detail, err)
	}
	instruction := uint32(0)
	if frame.ip != 0 {
		instruction = frame.ip - 1
	}
	if location, ok := frame.function.Locations.At(instruction); ok {
		return fmt.Errorf("runtime: bytecode function %s instruction %d source %d:%d: %w", frame.Function, instruction, location.Start, location.End, err)
	}
	return fmt.Errorf("runtime: bytecode function %s instruction %d: %w", frame.Function, instruction, err)
}

func (execution *execution) thrownSummary(err error) string {
	value, ok := ThrownValue(err)
	if !ok || !value.IsRef() {
		return ""
	}
	kind, kindErr := execution.context.HeapKind(value.Ref())
	if kindErr != nil || kind != memory.HeapError {
		return ""
	}
	object, objectErr := execution.context.DerefError(value.Ref())
	if objectErr != nil {
		return ""
	}
	message := ""
	if object.Message.IsRef() {
		message, _ = execution.context.DerefString(object.Message.Ref())
	}
	if message == "" {
		return object.Kind.Name()
	}
	return object.Kind.Name() + ": " + message
}

func (execution *execution) runFrame(frame *Frame) (memory.Value, error) {
	context := execution.context
	for {
		instruction, err := frame.next()
		if err != nil {
			return memory.Value{}, err
		}
		instructionIndex := frame.ip - 1
		if chargeExecutionBudget(frame, instructionIndex, instruction) {
			if execution.budgetSteps >= execution.interpreter.config.MaxInstructions {
				return memory.Value{}, execution.instructionLimitError()
			}
			execution.budgetSteps++
		}
		execution.steps++
		if execution.profile != nil {
			execution.profile.record(frame, instruction.Op)
		}
		if interval := execution.interpreter.config.ExecutionSampleInterval; interval != 0 &&
			execution.interpreter.config.ExecutionSample != nil && execution.steps%interval == 0 {
			sample := ExecutionSample{
				TaskID:          context.TaskID,
				RawInstructions: execution.steps,
				ControlSteps:    execution.budgetSteps,
				StackDepth:      context.StackDepth(),
				Function:        frame.Function,
				FunctionName:    instructionFunctionName(context, frame.Function, frame.function),
				FunctionHash:    frame.programHash,
				Instruction:     instructionIndex,
				Opcode:          instruction.Op,
			}
			if err := execution.interpreter.config.ExecutionSample(context, sample); err != nil {
				return memory.Value{}, fmt.Errorf("runtime: execution sample: %w", err)
			}
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
		case OpArguments:
			arguments, err := context.NewArray(uint32(len(frame.Arguments)))
			if err != nil {
				return memory.Value{}, err
			}
			for index, value := range frame.Arguments {
				if err := context.SetArrayElement(arguments, uint32(index), value); err != nil {
					return memory.Value{}, err
				}
			}
			frame.push(memory.RefValue(arguments))
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
		case OpDupPair:
			if len(frame.Stack) < 2 {
				return memory.Value{}, ErrStackUnderflow
			}
			index := len(frame.Stack) - 2
			frame.push(frame.Stack[index])
			frame.push(frame.Stack[index+1])
		case OpReturn:
			value := memory.UndefinedValue()
			if len(frame.Stack) == 0 {
				value = memory.UndefinedValue()
			} else {
				value, err = frame.pop()
				if err != nil {
					return memory.Value{}, err
				}
			}
			frame.completion = nil
			frame.current = nil
			completed, result, err := routeCompletion(frame, abruptCompletion{kind: completionReturn, value: value})
			if err != nil {
				return memory.Value{}, err
			}
			if !completed {
				continue
			}
			return result, nil
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
		case OpSetOwnPropertyConstant:
			value, err := frame.pop()
			if err != nil {
				return memory.Value{}, err
			}
			objectValue, err := frame.peek()
			if err != nil {
				return memory.Value{}, err
			}
			object, err := requireRef(objectValue, "Object")
			if err != nil {
				return memory.Value{}, err
			}
			if uint64(instruction.A) >= uint64(len(frame.function.Constants)) {
				return memory.Value{}, fmt.Errorf("%w: %d", ErrConstantBounds, instruction.A)
			}
			name, err := requireRef(frame.function.Constants[instruction.A], "property name")
			if err != nil {
				return memory.Value{}, err
			}
			if err := context.SetProperty(object, name, value); err != nil {
				return memory.Value{}, err
			}
		case OpDefineAccessor:
			callable, err := frame.pop()
			if err != nil {
				return memory.Value{}, err
			}
			name, object, err := popRefPair(frame, "Object", "property name")
			if err != nil {
				return memory.Value{}, err
			}
			descriptor, found, err := context.GetOwnPropertyDescriptor(object, name)
			if err != nil {
				return memory.Value{}, err
			}
			if !found || descriptor.Kind != memory.PropertyAccessor {
				descriptor = memory.AccessorProperty(memory.UndefinedValue(), memory.UndefinedValue(), true, true)
			}
			if instruction.A == 0 {
				descriptor.SetGetter(callable)
			} else {
				descriptor.SetSetter(callable)
			}
			if err := context.DefineProperty(object, name, descriptor); err != nil {
				return memory.Value{}, err
			}
			frame.push(callable)
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
		case OpGetProperty:
			key, err := frame.pop()
			if err != nil {
				return memory.Value{}, err
			}
			base, err := frame.pop()
			if err != nil {
				return memory.Value{}, err
			}
			value, present, err := execution.getProperty(base, key)
			if err != nil {
				if handled, terminal := execution.routeFrameError(frame, err); handled {
					continue
				} else {
					return memory.Value{}, terminal
				}
			}
			if !present {
				value = memory.UndefinedValue()
			}
			frame.push(value)
		case OpGetPropertyConstant:
			base, err := frame.pop()
			if err != nil {
				return memory.Value{}, err
			}
			if uint64(instruction.A) >= uint64(len(frame.function.Constants)) {
				return memory.Value{}, fmt.Errorf("%w: %d", ErrConstantBounds, instruction.A)
			}
			value, present, err := execution.getProperty(base, frame.function.Constants[instruction.A])
			if err != nil {
				if handled, terminal := execution.routeFrameError(frame, err); handled {
					continue
				} else {
					return memory.Value{}, terminal
				}
			}
			if !present {
				value = memory.UndefinedValue()
			}
			frame.push(value)
		case OpLoadBindingProperty:
			base, err := execution.resolveBindingOperand(frame, instruction.A)
			if err != nil {
				if handled, terminal := execution.routeFrameError(frame, err); handled {
					continue
				} else {
					return memory.Value{}, terminal
				}
			}
			if uint64(instruction.B) >= uint64(len(frame.function.Constants)) {
				return memory.Value{}, fmt.Errorf("%w: %d", ErrConstantBounds, instruction.B)
			}
			value, present, err := execution.getProperty(base, frame.function.Constants[instruction.B])
			if err != nil {
				if handled, terminal := execution.routeFrameError(frame, err); handled {
					continue
				} else {
					return memory.Value{}, terminal
				}
			}
			if !present {
				value = memory.UndefinedValue()
			}
			frame.push(value)
		case OpLoadThisProperty:
			if uint64(instruction.A) >= uint64(len(frame.function.Constants)) {
				return memory.Value{}, fmt.Errorf("%w: %d", ErrConstantBounds, instruction.A)
			}
			value, present, err := execution.getProperty(frame.This, frame.function.Constants[instruction.A])
			if err != nil {
				if handled, terminal := execution.routeFrameError(frame, err); handled {
					continue
				} else {
					return memory.Value{}, terminal
				}
			}
			if !present {
				value = memory.UndefinedValue()
			}
			frame.push(value)
		case OpSetProperty:
			value, err := frame.pop()
			if err != nil {
				return memory.Value{}, err
			}
			key, err := frame.pop()
			if err != nil {
				return memory.Value{}, err
			}
			base, err := frame.pop()
			if err != nil {
				return memory.Value{}, err
			}
			if err := execution.setPropertyValue(base, key, value); err != nil {
				if handled, terminal := execution.routeFrameError(frame, err); handled {
					continue
				} else {
					return memory.Value{}, terminal
				}
			}
			frame.push(value)
		case OpDeleteProperty:
			key, err := frame.pop()
			if err != nil {
				return memory.Value{}, err
			}
			base, err := frame.pop()
			if err != nil {
				return memory.Value{}, err
			}
			deleted, err := execution.deletePropertyValue(base, key)
			if err != nil {
				if handled, terminal := execution.routeFrameError(frame, err); handled {
					continue
				} else {
					return memory.Value{}, terminal
				}
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
		case OpSetElementConstant:
			value, err := frame.pop()
			if err != nil {
				return memory.Value{}, err
			}
			arrayValue, err := frame.peek()
			if err != nil {
				return memory.Value{}, err
			}
			array, err := requireRef(arrayValue, "Array")
			if err != nil {
				return memory.Value{}, err
			}
			if err := context.SetArrayElement(array, instruction.A, value); err != nil {
				return memory.Value{}, err
			}
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
		case OpOwnKeys:
			target, err := frame.pop()
			if err != nil {
				return memory.Value{}, err
			}
			keys, err := builtinObjectKeys(execution, memory.Ref{}, memory.Function{}, memory.UndefinedValue(), []memory.Value{target})
			if err != nil {
				if handled, terminal := execution.routeFrameError(frame, err); handled {
					continue
				} else {
					return memory.Value{}, terminal
				}
			}
			frame.push(keys)
		case OpGetIterator:
			iterable, err := frame.pop()
			if err != nil {
				return memory.Value{}, err
			}
			iterator, next, err := execution.getIteratorRecord(iterable)
			if err != nil {
				if handled, terminal := execution.routeFrameError(frame, err); handled {
					continue
				} else {
					return memory.Value{}, terminal
				}
			}
			frame.push(iterator)
			frame.push(next)
		case OpIteratorNext:
			next, err := frame.pop()
			if err != nil {
				return memory.Value{}, err
			}
			iterator, err := frame.pop()
			if err != nil {
				return memory.Value{}, err
			}
			result, err := execution.iteratorNext(iterator, next)
			if err != nil {
				if handled, terminal := execution.routeFrameError(frame, err); handled {
					continue
				} else {
					return memory.Value{}, terminal
				}
			}
			frame.push(result)
		case OpIteratorClose:
			iterator, err := frame.pop()
			if err != nil {
				return memory.Value{}, err
			}
			if err := execution.closeIterator(iterator); err != nil {
				// IteratorClose preserves an already-thrown completion. For
				// break and return, a close failure replaces the completion.
				if frame.completion != nil && frame.completion.kind == completionThrow {
					continue
				}
				if handled, terminal := execution.routeFrameError(frame, err); handled {
					continue
				} else {
					return memory.Value{}, terminal
				}
			}
		case OpAppendSpread:
			iterable, err := frame.pop()
			if err != nil {
				return memory.Value{}, err
			}
			arrayValue, err := frame.pop()
			if err != nil {
				return memory.Value{}, err
			}
			array, err := requireRef(arrayValue, "Array spread target")
			if err == nil {
				err = execution.appendIterableToArray(array, iterable)
			}
			if err != nil {
				if handled, terminal := execution.routeFrameError(frame, err); handled {
					continue
				} else {
					return memory.Value{}, terminal
				}
			}
			frame.push(arrayValue)
		case OpCopyDataProperties:
			source, err := frame.pop()
			if err != nil {
				return memory.Value{}, err
			}
			target, err := frame.pop()
			if err != nil {
				return memory.Value{}, err
			}
			result, err := builtinObjectAssign(execution, memory.Ref{}, memory.Function{}, memory.UndefinedValue(), []memory.Value{target, source})
			if err != nil {
				if handled, terminal := execution.routeFrameError(frame, err); handled {
					continue
				} else {
					return memory.Value{}, terminal
				}
			}
			frame.push(result)
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
			value, err := execution.resolveBindingOperand(frame, instruction.A)
			if err != nil {
				if handled, terminal := execution.routeFrameError(frame, err); handled {
					continue
				} else {
					return memory.Value{}, terminal
				}
			}
			frame.push(value)
		case OpTypeOfBinding:
			value, found, err := execution.resolveBindingOperandFound(frame, instruction.A)
			if err != nil {
				if handled, terminal := execution.routeFrameError(frame, err); handled {
					continue
				} else {
					return memory.Value{}, terminal
				}
			}
			typeName := "undefined"
			if found {
				typeName, err = valueTypeName(context, value)
				if err != nil {
					return memory.Value{}, err
				}
			}
			result, err := context.NewString(typeName)
			if err != nil {
				return memory.Value{}, err
			}
			frame.push(memory.RefValue(result))
		case OpDeclareBinding:
			if instruction.B > 1 {
				return memory.Value{}, fmt.Errorf("%w: DeclareBinding mutability %d", ErrInvalidBytecode, instruction.B)
			}
			if err := execution.declareBindingOperand(frame, instruction.A, instruction.B == 1); err != nil {
				return memory.Value{}, err
			}
		case OpDeclareBindings:
			count := instruction.B &^ bindingBatchMutable
			mutable := instruction.B&bindingBatchMutable != 0
			for bindingIndex := uint32(0); bindingIndex < count; bindingIndex++ {
				if err := execution.declareBindingOperand(frame, instruction.A+bindingIndex, mutable); err != nil {
					return memory.Value{}, err
				}
			}
		case OpBindArgument:
			if err := execution.declareBindingOperand(frame, instruction.A, true); err != nil {
				return memory.Value{}, err
			}
			value := memory.UndefinedValue()
			if uint64(instruction.B) < uint64(len(frame.Arguments)) {
				value = frame.Arguments[instruction.B]
			}
			if err := execution.initializeBindingOperand(frame, instruction.A, value); err != nil {
				return memory.Value{}, err
			}
		case OpInitializeBinding:
			value, err := frame.pop()
			if err != nil {
				return memory.Value{}, err
			}
			if err := execution.initializeBindingOperand(frame, instruction.A, value); err != nil {
				return memory.Value{}, err
			}
			if instruction.B == 0 {
				frame.push(value)
			}
		case OpStoreBinding:
			value, err := frame.pop()
			if err != nil {
				return memory.Value{}, err
			}
			if err := execution.setBindingOperand(frame, instruction.A, value); err != nil {
				if handled, terminal := execution.routeFrameError(frame, err); handled {
					continue
				} else {
					return memory.Value{}, terminal
				}
			}
			if instruction.B == 0 {
				frame.push(value)
			}
		case OpUpdateBinding:
			old, err := execution.resolveBindingOperand(frame, instruction.A)
			if err != nil {
				if handled, terminal := execution.routeFrameError(frame, err); handled {
					continue
				} else {
					return memory.Value{}, terminal
				}
			}
			number, err := execution.toNumber(old)
			if err != nil {
				if handled, terminal := execution.routeFrameError(frame, err); handled {
					continue
				} else {
					return memory.Value{}, terminal
				}
			}
			delta := 1.0
			if instruction.B&1 != 0 {
				delta = -1
			}
			updated := memory.NumberValue(number + delta)
			if err := execution.setBindingOperand(frame, instruction.A, updated); err != nil {
				if handled, terminal := execution.routeFrameError(frame, err); handled {
					continue
				} else {
					return memory.Value{}, terminal
				}
			}
			if instruction.B&4 == 0 {
				if instruction.B&2 != 0 {
					frame.push(updated)
				} else {
					frame.push(old)
				}
			}
		case OpLoadThis:
			frame.push(frame.This)
		case OpAdd, OpSubtract, OpMultiply, OpExponentiate, OpDivide, OpRemainder,
			OpNegate, OpIncrement, OpDecrement,
			OpBitwiseAnd, OpBitwiseOr, OpBitwiseXor,
			OpShiftLeft, OpShiftRight, OpUnsignedShiftRight,
			OpLogicalNot, OpTypeOf, OpToNumber,
			OpStrictEqual, OpStrictNotEqual, OpEqual, OpNotEqual,
			OpLessThan, OpLessThanOrEqual, OpGreaterThan, OpGreaterThanOrEqual,
			OpIn, OpInstanceOf:
			if err := executeOperator(execution, frame, instruction.Op); err != nil {
				if handled, terminal := execution.routeFrameError(frame, err); handled {
					continue
				} else {
					return memory.Value{}, terminal
				}
			}
		case OpJump:
			frame.ip = instruction.A
		case OpBreak, OpContinue:
			environmentDepth, handlerDepth := unpackCompletionDepths(instruction.B)
			kind := completionBreak
			if instruction.Op == OpContinue {
				kind = completionContinue
			}
			frame.completion = nil
			completed, result, err := routeCompletion(frame, abruptCompletion{
				kind: kind, target: instruction.A,
				environmentDepth: environmentDepth, handlerDepth: handlerDepth,
			})
			if err != nil {
				return memory.Value{}, err
			}
			if completed {
				return result, nil
			}
			continue
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
		case OpJumpIfBindingTrue, OpJumpIfBindingFalse:
			condition, err := execution.resolveBindingOperand(frame, instruction.B)
			if err != nil {
				if handled, terminal := execution.routeFrameError(frame, err); handled {
					continue
				} else {
					return memory.Value{}, terminal
				}
			}
			truthy, err := valueTruthy(context, condition)
			if err != nil {
				return memory.Value{}, err
			}
			if truthy == (instruction.Op == OpJumpIfBindingTrue) {
				frame.ip = instruction.A
			}
		case OpJumpIfBindingPropertyTrue, OpJumpIfBindingPropertyFalse,
			OpJumpIfThisPropertyTrue, OpJumpIfThisPropertyFalse,
			OpJumpIfPropertyConstantTrue, OpJumpIfPropertyConstantFalse:
			var base memory.Value
			propertyIndex := instruction.B
			switch instruction.Op {
			case OpJumpIfBindingPropertyTrue, OpJumpIfBindingPropertyFalse:
				bindingIndex, unpackedProperty := unpackConstantPair(instruction.B)
				propertyIndex = unpackedProperty
				base, err = execution.resolveBindingOperand(frame, bindingIndex)
			case OpJumpIfThisPropertyTrue, OpJumpIfThisPropertyFalse:
				base = frame.This
			default:
				base, err = frame.pop()
			}
			if err != nil {
				if handled, terminal := execution.routeFrameError(frame, err); handled {
					continue
				} else {
					return memory.Value{}, terminal
				}
			}
			if uint64(propertyIndex) >= uint64(len(frame.function.Constants)) {
				return memory.Value{}, fmt.Errorf("%w: %d", ErrConstantBounds, propertyIndex)
			}
			truthy, err := execution.propertyTruthy(base, frame.function.Constants[propertyIndex])
			if err != nil {
				if handled, terminal := execution.routeFrameError(frame, err); handled {
					continue
				} else {
					return memory.Value{}, terminal
				}
			}
			jumpOnTrue := instruction.Op == OpJumpIfBindingPropertyTrue ||
				instruction.Op == OpJumpIfThisPropertyTrue || instruction.Op == OpJumpIfPropertyConstantTrue
			if truthy == jumpOnTrue {
				frame.ip = instruction.A
			}
		case OpLogicalAndValue, OpLogicalOrValue, OpLogicalNullishValue:
			value, err := frame.pop()
			if err != nil {
				return memory.Value{}, err
			}
			shortCircuit := value.Kind() != memory.ValueUndefined && value.Kind() != memory.ValueNull
			if instruction.Op != OpLogicalNullishValue {
				truthy, truthErr := valueTruthy(context, value)
				if truthErr != nil {
					return memory.Value{}, truthErr
				}
				shortCircuit = truthy == (instruction.Op == OpLogicalOrValue)
			}
			if shortCircuit {
				frame.push(value)
				frame.ip = instruction.A
			}
		case OpCall, OpCallNative, OpConstruct, OpCallMethod, OpCallSpread, OpCallMethodSpread, OpConstructSpread:
			methodCall := instruction.Op == OpCallMethod || instruction.Op == OpCallMethodSpread
			spreadCall := instruction.Op == OpCallSpread || instruction.Op == OpCallMethodSpread || instruction.Op == OpConstructSpread
			if methodCall {
				var base, key memory.Value
				var arguments []memory.Value
				var err error
				if spreadCall {
					base, key, arguments, err = execution.popSpreadMethodOperands(frame)
				} else {
					base, key, arguments, err = popMethodOperands(frame, instruction.A)
				}
				if err != nil {
					if handled, terminal := execution.routeFrameError(frame, err); handled {
						continue
					} else {
						return memory.Value{}, terminal
					}
				}
				calleeValue, present, err := execution.getProperty(base, key)
				if err != nil {
					if handled, terminal := execution.routeFrameError(frame, err); handled {
						continue
					} else {
						return memory.Value{}, terminal
					}
				}
				if !present {
					methodErr := ErrNotCallable
					if key.IsRef() {
						if kind, kindErr := context.HeapKind(key.Ref()); kindErr == nil && kind == memory.HeapString {
							if name, nameErr := context.DerefString(key.Ref()); nameErr == nil {
								methodErr = fmt.Errorf("%w: method %q", ErrNotCallable, name)
							}
						}
					}
					if handled, terminal := execution.routeFrameError(frame, methodErr); handled {
						continue
					} else {
						return memory.Value{}, terminal
					}
				}
				callee, err := requireRef(calleeValue, "method Function")
				if err != nil {
					if handled, terminal := execution.routeFrameError(frame, err); handled {
						continue
					} else {
						return memory.Value{}, terminal
					}
				}
				frame.call = &frameCall{function: callee, this: base, arguments: arguments, mode: callAny}
				return memory.Value{}, errFrameCall
			}
			var callee memory.Ref
			var arguments []memory.Value
			var err error
			if spreadCall {
				callee, arguments, err = execution.popSpreadCallOperands(frame)
			} else {
				callee, arguments, err = popCallOperands(frame, instruction.A)
			}
			if err != nil {
				if handled, terminal := execution.routeFrameError(frame, err); handled {
					continue
				} else {
					return memory.Value{}, terminal
				}
			}
			this := memory.UndefinedValue()
			mode := callAny
			var constructed memory.Ref
			if instruction.Op == OpCallNative {
				mode = callNativeOnly
			}
			construct := instruction.Op == OpConstruct || instruction.Op == OpConstructSpread
			if construct {
				descriptor, descriptorErr := context.LoadFunction(callee)
				if descriptorErr != nil {
					return memory.Value{}, descriptorErr
				}
				if !descriptor.Constructible {
					if handled, terminal := execution.routeFrameError(frame, ErrNotConstructor); handled {
						continue
					} else {
						return memory.Value{}, terminal
					}
				}
				constructed, err = context.NewHeapObject()
				if err != nil {
					return memory.Value{}, err
				}
				prototypeName, nameErr := context.NewString("prototype")
				if nameErr != nil {
					return memory.Value{}, nameErr
				}
				prototype, present, propertyErr := execution.getProperty(memory.RefValue(callee), memory.RefValue(prototypeName))
				if propertyErr != nil {
					return memory.Value{}, propertyErr
				}
				if present && prototype.IsRef() {
					if _, headerErr := context.DerefObjectHeader(prototype.Ref()); headerErr == nil {
						if err := context.SetPrototype(constructed, prototype); err != nil {
							return memory.Value{}, err
						}
					}
				}
				this = memory.RefValue(constructed)
			}
			frame.call = &frameCall{
				function: callee, this: this, arguments: arguments, mode: mode,
				construct: construct, constructed: constructed,
			}
			return memory.Value{}, errFrameCall
		case OpCreateClosure, OpCreateArrowClosure:
			if frame.stackLocals {
				if err := execution.materializeFrameEnvironment(frame); err != nil {
					return memory.Value{}, err
				}
			}
			template, err := constantRef(frame, instruction.A, "Function template")
			if err != nil {
				return memory.Value{}, err
			}
			descriptor, err := context.LoadFunction(template)
			if err != nil {
				return memory.Value{}, err
			}
			var closure memory.Ref
			switch descriptor.Kind {
			case memory.FunctionBytecode:
				if instruction.Op == OpCreateArrowClosure {
					closure, err = context.NewArrowBytecodeClosure(template, frame.Environment, frame.This)
				} else {
					closure, err = context.NewBytecodeClosure(template, frame.Environment)
				}
			case memory.FunctionNative:
				if instruction.Op == OpCreateArrowClosure {
					err = fmt.Errorf("%w: native arrow template %s", ErrNotCallable, template)
				} else {
					closure, err = context.NewNativeFunction(descriptor.Name, frame.Environment, descriptor.Arity, descriptor.NativeID)
				}
			default:
				err = fmt.Errorf("%w: template %s", ErrNotCallable, template)
			}
			if err != nil {
				return memory.Value{}, err
			}
			frame.push(memory.RefValue(closure))
		case OpUpdateProperty:
			key, err := frame.pop()
			if err != nil {
				return memory.Value{}, err
			}
			base, err := frame.pop()
			if err != nil {
				return memory.Value{}, err
			}
			old, present, err := execution.getProperty(base, key)
			if err != nil {
				if handled, terminal := execution.routeFrameError(frame, err); handled {
					continue
				} else {
					return memory.Value{}, terminal
				}
			}
			if !present {
				old = memory.UndefinedValue()
			}
			number, err := execution.toNumber(old)
			if err != nil {
				if handled, terminal := execution.routeFrameError(frame, err); handled {
					continue
				} else {
					return memory.Value{}, terminal
				}
			}
			delta := 1.0
			if instruction.A == 1 {
				delta = -1
			}
			updated := memory.NumberValue(number + delta)
			if err := execution.setPropertyValue(base, key, updated); err != nil {
				if handled, terminal := execution.routeFrameError(frame, err); handled {
					continue
				} else {
					return memory.Value{}, terminal
				}
			}
			if instruction.B == 1 {
				frame.push(updated)
			} else {
				frame.push(old)
			}
		case OpThrow:
			value, err := frame.pop()
			if err != nil {
				return memory.Value{}, err
			}
			frame.completion = nil
			completed, result, routed := routeCompletion(frame, abruptCompletion{kind: completionThrow, value: value})
			if routed != nil {
				return memory.Value{}, routed
			}
			if !completed {
				continue
			}
			return result, Throw(value)
		case OpEnterTry:
			frame.handlers = append(frame.handlers, exceptionHandler{
				kind:             ExceptionHandlerKind(instruction.B),
				target:           instruction.A,
				stackDepth:       len(frame.Stack),
				environmentDepth: len(frame.environments),
			})
		case OpLeaveTry:
			if len(frame.handlers) == 0 {
				return memory.Value{}, fmt.Errorf("%w: LeaveTry without handler", ErrExceptionState)
			}
			frame.handlers[len(frame.handlers)-1] = exceptionHandler{}
			frame.handlers = frame.handlers[:len(frame.handlers)-1]
		case OpEnterCatch:
			if frame.completion == nil || frame.completion.kind != completionThrow {
				return memory.Value{}, fmt.Errorf("%w: EnterCatch without a thrown value", ErrExceptionState)
			}
			frame.current = &ThrownError{Value: frame.completion.value}
			frame.push(frame.completion.value)
			frame.completion = nil
		case OpEnterFinally:
			// Abrupt entry retains its completion; normal entry has none.
			// EndFinally resumes the retained completion unless the finalizer
			// replaces it with a new abrupt completion.
		case OpEndFinally:
			if frame.completion != nil {
				completion := *frame.completion
				frame.completion = nil
				completed, result, err := routeCompletion(frame, completion)
				if err != nil {
					return memory.Value{}, err
				}
				if !completed {
					continue
				}
				if completion.kind == completionThrow {
					return memory.Value{}, Throw(completion.value)
				}
				return result, nil
			}
		case OpRethrow:
			if frame.current == nil {
				return memory.Value{}, fmt.Errorf("%w: Rethrow without a current exception", ErrExceptionState)
			}
			frame.completion = nil
			value := frame.current.Value
			completed, result, routed := routeCompletion(frame, abruptCompletion{kind: completionThrow, value: value})
			if routed != nil {
				return memory.Value{}, routed
			}
			if !completed {
				continue
			}
			return result, Throw(value)
		case OpEnterScope:
			if frame.stackLocals {
				frame.enterLocalEnvironment()
			} else {
				environment, err := context.NewContext(frame.Environment)
				if err != nil {
					return memory.Value{}, err
				}
				frame.environments = append(frame.environments, frame.Environment)
				frame.localEnvironments = append(frame.localEnvironments, false)
				frame.Environment = memory.RefValue(environment)
			}
		case OpEnterFunction:
			environment, err := context.NewContext(frame.Environment)
			if err != nil {
				return memory.Value{}, err
			}
			frame.environments = append(frame.environments, frame.Environment)
			frame.localEnvironments = append(frame.localEnvironments, false)
			frame.Environment = memory.RefValue(environment)
			for parameterIndex := uint32(0); parameterIndex < instruction.B; parameterIndex++ {
				_, name, operandErr := bindingOperands(frame, instruction.A+parameterIndex)
				if operandErr != nil {
					return memory.Value{}, operandErr
				}
				if err := context.DeclareBinding(environment, name, true); err != nil {
					return memory.Value{}, err
				}
				value := memory.UndefinedValue()
				if uint64(parameterIndex) < uint64(len(frame.Arguments)) {
					value = frame.Arguments[parameterIndex]
				}
				if err := context.InitializeBinding(environment, name, value); err != nil {
					return memory.Value{}, err
				}
			}
		case OpEnterStackFunction:
			frame.stackLocals = true
			frame.enterLocalEnvironment()
			for parameterIndex := uint32(0); parameterIndex < instruction.B; parameterIndex++ {
				value := memory.UndefinedValue()
				if uint64(parameterIndex) < uint64(len(frame.Arguments)) {
					value = frame.Arguments[parameterIndex]
				}
				if err := frame.declareLocal(instruction.A+parameterIndex, true, true, value); err != nil {
					return memory.Value{}, err
				}
			}
		case OpLeaveScope:
			if len(frame.environments) == 0 {
				return memory.Value{}, fmt.Errorf("%w: LeaveScope without EnterScope", ErrExceptionState)
			}
			if err := frame.leaveEnvironment(); err != nil {
				return memory.Value{}, err
			}
		default:
			return memory.Value{}, fmt.Errorf("%w: unimplemented %s", ErrInvalidBytecode, instruction.Op)
		}
	}
}

// chargeExecutionBudget keeps the runaway-script guard independent from
// bytecode lowering. A finite expression may expand into several explicit
// stack operations, while the control behavior that can execute without bound
// is function activation or a backward transfer. Completions are charged too
// so even tiny call chains consume a visible amount of the budget.
func chargeExecutionBudget(frame *Frame, instructionIndex uint32, instruction Instruction) bool {
	if frame == nil {
		return true
	}
	if !frame.budgetStarted {
		frame.budgetStarted = true
		return true
	}
	switch instruction.Op {
	case OpReturn, OpThrow, OpRethrow:
		return true
	case OpJump, OpJumpIfTrue, OpJumpIfFalse, OpJumpIfNullish,
		OpLogicalAndValue, OpLogicalOrValue, OpLogicalNullishValue,
		OpJumpIfBindingTrue, OpJumpIfBindingFalse,
		OpJumpIfBindingPropertyTrue, OpJumpIfBindingPropertyFalse,
		OpJumpIfThisPropertyTrue, OpJumpIfThisPropertyFalse,
		OpJumpIfPropertyConstantTrue, OpJumpIfPropertyConstantFalse,
		OpBreak, OpContinue:
		return instruction.A <= instructionIndex
	default:
		return false
	}
}

func requireRef(value memory.Value, label string) (memory.Ref, error) {
	if !value.IsRef() {
		return memory.Ref{}, fmt.Errorf("%w: %s must be a Ref (got %s)", ErrOperandType, label, value.Kind())
	}
	return value.Ref(), nil
}

func (execution *execution) declareBindingOperand(frame *Frame, index uint32, mutable bool) error {
	if frame.stackLocals {
		return frame.declareLocal(index, mutable, false, memory.Value{})
	}
	environment, name, err := bindingOperands(frame, index)
	if err != nil {
		return err
	}
	return execution.context.DeclareBinding(environment, name, mutable)
}

func (execution *execution) initializeBindingOperand(frame *Frame, index uint32, value memory.Value) error {
	if binding, found := frame.resolveLocal(index); found {
		if binding.initialized {
			return execution.localBindingError(memory.ErrBindingExists, frame, index, "is already initialized")
		}
		binding.value = value
		binding.initialized = true
		return nil
	}
	environment, name, err := bindingOperands(frame, index)
	if err != nil {
		return err
	}
	return execution.context.InitializeBinding(environment, name, value)
}

func (execution *execution) resolveBindingOperand(frame *Frame, index uint32) (memory.Value, error) {
	value, found, err := execution.resolveBindingOperandFound(frame, index)
	if err != nil {
		return memory.Value{}, err
	}
	if found {
		return value, nil
	}
	name, nameErr := constantRef(frame, index, "binding name")
	if nameErr != nil {
		return memory.Value{}, nameErr
	}
	nameText, nameErr := execution.context.DerefString(name)
	if nameErr != nil {
		return memory.Value{}, nameErr
	}
	return memory.Value{}, fmt.Errorf("%w: %q", memory.ErrBindingNotFound, nameText)
}

func (execution *execution) resolveBindingOperandFound(frame *Frame, index uint32) (memory.Value, bool, error) {
	if binding, found := frame.resolveLocal(index); found {
		if !binding.initialized {
			return memory.Value{}, true, execution.localBindingError(memory.ErrBindingUninitialized, frame, index, "")
		}
		return binding.value, true, nil
	}
	environment, name, err := bindingOperands(frame, index)
	if err != nil {
		return memory.Value{}, false, err
	}
	cache := frame.bindingCache(index)
	if cache.valid && cache.environment == environment {
		value, err := execution.context.ResolveBindingAt(cache.location)
		return value, true, err
	}
	location, value, found, err := execution.context.ResolveBindingLocation(environment, name)
	if err != nil {
		return memory.Value{}, false, err
	}
	if found {
		cache.environment = environment
		cache.location = location
		cache.valid = true
		return value, true, nil
	}
	// Browser globals may be properties created through globalThis rather than
	// Context bindings. Preserve TaskContext's object-backed global fallback
	// after the fast lexical lookup misses.
	return execution.context.ResolveBinding(environment, name)
}

func (execution *execution) setBindingOperand(frame *Frame, index uint32, value memory.Value) error {
	if binding, found := frame.resolveLocal(index); found {
		if !binding.initialized {
			return execution.localBindingError(memory.ErrBindingUninitialized, frame, index, "")
		}
		if !binding.mutable {
			return execution.localBindingError(memory.ErrImmutableBinding, frame, index, "")
		}
		binding.value = value
		return nil
	}
	environment, name, err := bindingOperands(frame, index)
	if err != nil {
		return err
	}
	cache := frame.bindingCache(index)
	if cache.valid && cache.environment == environment {
		return execution.context.SetBindingAt(cache.location, value)
	}
	location, _, found, err := execution.context.ResolveBindingLocation(environment, name)
	if err != nil {
		return err
	}
	if !found {
		return execution.context.SetBinding(environment, name, value)
	}
	cache.environment = environment
	cache.location = location
	cache.valid = true
	return execution.context.SetBindingAt(location, value)
}

func (execution *execution) localBindingError(base error, frame *Frame, index uint32, suffix string) error {
	name, err := constantRef(frame, index, "binding name")
	if err != nil {
		return err
	}
	text, err := execution.context.DerefString(name)
	if err != nil {
		return err
	}
	if suffix == "" {
		return fmt.Errorf("%w: %q", base, text)
	}
	return fmt.Errorf("%w: %q %s", base, text, suffix)
}

// materializeFrameEnvironment moves the active lexical scope chain from
// borrowed Frame slots into RegionStore Contexts immediately before a closure
// captures it. From this point onward the frame uses those Contexts as the
// authoritative binding storage, so later mutations remain visible to every
// closure after the frame returns.
func (execution *execution) materializeFrameEnvironment(frame *Frame) error {
	if frame == nil || !frame.stackLocals {
		return nil
	}
	if len(frame.localScopes) == 0 || len(frame.localScopes) != len(frame.environments) || len(frame.localEnvironments) != len(frame.environments) {
		return fmt.Errorf("%w: stack-local environment shape", ErrExceptionState)
	}
	for _, local := range frame.localEnvironments {
		if !local {
			return fmt.Errorf("%w: mixed local and heap environments", ErrExceptionState)
		}
	}

	parent := frame.environments[0]
	for scopeIndex, start := range frame.localScopes {
		end := len(frame.localBindings)
		if scopeIndex+1 < len(frame.localScopes) {
			end = frame.localScopes[scopeIndex+1]
		}
		if start < 0 || start > end || end > len(frame.localBindings) {
			return fmt.Errorf("%w: local scope %d bounds %d..%d", ErrExceptionState, scopeIndex, start, end)
		}
		frame.environments[scopeIndex] = parent
		environment, err := execution.context.NewContext(parent)
		if err != nil {
			return err
		}
		for bindingIndex := start; bindingIndex < end; bindingIndex++ {
			binding := frame.localBindings[bindingIndex]
			name, err := constantRef(frame, binding.name, "binding name")
			if err != nil {
				return err
			}
			if err := execution.context.DeclareBinding(environment, name, binding.mutable); err != nil {
				return err
			}
			if binding.initialized {
				if err := execution.context.InitializeBinding(environment, name, binding.value); err != nil {
					return err
				}
			}
		}
		parent = memory.RefValue(environment)
	}

	frame.Environment = parent
	clear(frame.localEnvironments)
	frame.localEnvironments = append(frame.localEnvironments[:0], make([]bool, len(frame.environments))...)
	clear(frame.localBindings)
	frame.localBindings = frame.localBindings[:0]
	clear(frame.localSlots)
	frame.localSlots = frame.localSlots[:0]
	clear(frame.localScopes)
	frame.localScopes = frame.localScopes[:0]
	frame.stackLocals = false
	return nil
}

func (frame *Frame) enterLocalEnvironment() {
	frame.environments = append(frame.environments, frame.Environment)
	frame.localEnvironments = append(frame.localEnvironments, true)
	frame.localScopes = append(frame.localScopes, len(frame.localBindings))
}

func (frame *Frame) declareLocal(name uint32, mutable, initialized bool, value memory.Value) error {
	if len(frame.localScopes) == 0 {
		return fmt.Errorf("%w: local declaration outside stack scope", ErrExceptionState)
	}
	if uint64(name)+1 > uint64(int(^uint(0)>>1)) {
		return fmt.Errorf("%w: local constant %d", ErrConstantBounds, name)
	}
	required := int(name) + 1
	if required > len(frame.localSlots) {
		frame.localSlots = append(frame.localSlots, make([]uint32, required-len(frame.localSlots))...)
	}
	previous := frame.localSlots[name]
	start := frame.localScopes[len(frame.localScopes)-1]
	if previous != 0 && int(previous-1) >= start {
		return fmt.Errorf("%w: local constant %d", memory.ErrBindingExists, name)
	}
	frame.localBindings = append(frame.localBindings, frameLocalBinding{
		name: name, value: value, mutable: mutable, initialized: initialized, previous: previous,
	})
	frame.localSlots[name] = uint32(len(frame.localBindings))
	return nil
}

func (frame *Frame) resolveLocal(name uint32) (*frameLocalBinding, bool) {
	if frame == nil || !frame.stackLocals || uint64(name) >= uint64(len(frame.localSlots)) {
		return nil, false
	}
	slot := frame.localSlots[name]
	if slot == 0 || uint64(slot) > uint64(len(frame.localBindings)) {
		return nil, false
	}
	return &frame.localBindings[slot-1], true
}

func (frame *Frame) leaveEnvironment() error {
	if len(frame.environments) == 0 || len(frame.localEnvironments) != len(frame.environments) {
		return fmt.Errorf("%w: environment stack mismatch", ErrExceptionState)
	}
	index := len(frame.environments) - 1
	if frame.localEnvironments[index] {
		if len(frame.localScopes) == 0 {
			return fmt.Errorf("%w: local scope stack underflow", ErrExceptionState)
		}
		start := frame.localScopes[len(frame.localScopes)-1]
		for bindingIndex := len(frame.localBindings) - 1; bindingIndex >= start; bindingIndex-- {
			binding := &frame.localBindings[bindingIndex]
			if uint64(binding.name) < uint64(len(frame.localSlots)) {
				frame.localSlots[binding.name] = binding.previous
			}
		}
		clear(frame.localBindings[start:])
		frame.localBindings = frame.localBindings[:start]
		frame.localScopes = frame.localScopes[:len(frame.localScopes)-1]
	}
	frame.Environment = frame.environments[index]
	frame.environments[index] = memory.Value{}
	frame.environments = frame.environments[:index]
	frame.localEnvironments[index] = false
	frame.localEnvironments = frame.localEnvironments[:index]
	return nil
}

func (frame *Frame) bindingCache(index uint32) *frameBinding {
	required := int(index) + 1
	if required > len(frame.bindings) {
		frame.bindings = append(frame.bindings, make([]frameBinding, required-len(frame.bindings))...)
	}
	return &frame.bindings[index]
}

func (execution *execution) propertyTruthy(base, key memory.Value) (bool, error) {
	value, present, err := execution.getProperty(base, key)
	if err != nil {
		return false, err
	}
	if !present {
		value = memory.UndefinedValue()
	}
	return valueTruthy(execution.context, value)
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
	return verifyInstructions(frame.instructions, len(frame.function.Constants))
}

func routeCompletion(frame *Frame, completion abruptCompletion) (bool, memory.Value, error) {
	if frame == nil {
		return false, memory.Value{}, fmt.Errorf("%w: nil completion frame", ErrExceptionState)
	}
	floor := 0
	if completion.kind == completionBreak || completion.kind == completionContinue {
		floor = completion.handlerDepth
		if floor < 0 || floor > len(frame.handlers) {
			return false, memory.Value{}, fmt.Errorf("%w: completion handler depth %d", ErrExceptionState, floor)
		}
	}

	handlerIndex := -1
	for index := len(frame.handlers) - 1; index >= floor; index-- {
		handler := frame.handlers[index]
		if completion.kind == completionThrow || handler.kind == HandlerFinally {
			handlerIndex = index
			break
		}
	}
	if handlerIndex >= 0 {
		handler := frame.handlers[handlerIndex]
		clear(frame.handlers[handlerIndex:])
		frame.handlers = frame.handlers[:handlerIndex]
		if handler.stackDepth < 0 || handler.stackDepth > len(frame.Stack) {
			return false, memory.Value{}, fmt.Errorf("%w: handler stack depth %d", ErrExceptionState, handler.stackDepth)
		}
		clear(frame.Stack[handler.stackDepth:])
		frame.Stack = frame.Stack[:handler.stackDepth]
		if err := restoreEnvironmentDepth(frame, handler.environmentDepth); err != nil {
			return false, memory.Value{}, err
		}
		retained := completion
		frame.completion = &retained
		if completion.kind != completionThrow {
			frame.current = nil
		}
		frame.ip = handler.target
		return false, memory.Value{}, nil
	}

	clear(frame.handlers[floor:])
	frame.handlers = frame.handlers[:floor]
	frame.completion = nil
	switch completion.kind {
	case completionReturn:
		return true, completion.value, nil
	case completionThrow:
		return true, memory.Value{}, nil
	case completionBreak, completionContinue:
		if uint64(completion.target) >= uint64(len(frame.instructions)) {
			return false, memory.Value{}, fmt.Errorf("%w: completion target %d", ErrExceptionState, completion.target)
		}
		if err := restoreEnvironmentDepth(frame, completion.environmentDepth); err != nil {
			return false, memory.Value{}, err
		}
		frame.ip = completion.target
		return false, memory.Value{}, nil
	default:
		return false, memory.Value{}, fmt.Errorf("%w: completion kind %d", ErrExceptionState, completion.kind)
	}
}

func restoreEnvironmentDepth(frame *Frame, depth int) error {
	if depth < 0 || depth > len(frame.environments) {
		return fmt.Errorf("%w: handler environment depth %d", ErrExceptionState, depth)
	}
	for len(frame.environments) > depth {
		if err := frame.leaveEnvironment(); err != nil {
			return err
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

func popMethodOperands(frame *Frame, count uint32) (memory.Value, memory.Value, []memory.Value, error) {
	if uint64(count)+2 > uint64(len(frame.Stack)) {
		return memory.Value{}, memory.Value{}, nil, ErrStackUnderflow
	}
	arguments := make([]memory.Value, count)
	for index := int(count) - 1; index >= 0; index-- {
		value, err := frame.pop()
		if err != nil {
			return memory.Value{}, memory.Value{}, nil, err
		}
		arguments[index] = value
	}
	key, err := frame.pop()
	if err != nil {
		return memory.Value{}, memory.Value{}, nil, err
	}
	base, err := frame.pop()
	if err != nil {
		return memory.Value{}, memory.Value{}, nil, err
	}
	return base, key, arguments, nil
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

func executeOperator(execution *execution, frame *Frame, opcode Opcode) error {
	context := execution.context
	switch opcode {
	case OpNegate, OpIncrement, OpDecrement, OpToNumber:
		operand, err := frame.pop()
		if err != nil {
			return err
		}
		if value, handled, err := executeBigIntUnary(context, opcode, operand); handled || err != nil {
			if err != nil {
				return err
			}
			frame.push(value)
			return nil
		}
		value, err := execution.toNumber(operand)
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
	case OpEqual, OpNotEqual:
		right, left, err := popBinary(frame)
		if err != nil {
			return err
		}
		equal, err := execution.abstractEqual(left, right)
		if err != nil {
			return err
		}
		if opcode == OpNotEqual {
			equal = !equal
		}
		frame.push(memory.BoolValue(equal))
		return nil
	case OpIn:
		right, left, err := popBinary(frame)
		if err != nil {
			return err
		}
		present, err := execution.hasProperty(right, left)
		if err != nil {
			return err
		}
		frame.push(memory.BoolValue(present))
		return nil
	case OpInstanceOf:
		right, left, err := popBinary(frame)
		if err != nil {
			return err
		}
		result, err := execution.instanceOf(left, right)
		if err != nil {
			return err
		}
		frame.push(memory.BoolValue(result))
		return nil
	case OpAdd:
		right, left, err := popBinary(frame)
		if err != nil {
			return err
		}
		left, err = execution.toPrimitive(left, hintDefault)
		if err != nil {
			return err
		}
		right, err = execution.toPrimitive(right, hintDefault)
		if err != nil {
			return err
		}
		leftString, err := execution.isString(left)
		if err != nil {
			return err
		}
		rightString, err := execution.isString(right)
		if err != nil {
			return err
		}
		if leftString || rightString {
			leftText, err := execution.toString(left)
			if err != nil {
				return err
			}
			rightText, err := execution.toString(right)
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
		if value, handled, err := executeBigIntBinary(context, opcode, left, right); handled || err != nil {
			if err != nil {
				return err
			}
			frame.push(value)
			return nil
		}
		leftNumber, err := execution.toNumber(left)
		if err != nil {
			return err
		}
		rightNumber, err := execution.toNumber(right)
		if err != nil {
			return err
		}
		frame.push(memory.NumberValue(leftNumber + rightNumber))
		return nil
	case OpSubtract, OpMultiply, OpExponentiate, OpDivide, OpRemainder,
		OpBitwiseAnd, OpBitwiseOr, OpBitwiseXor,
		OpShiftLeft, OpShiftRight, OpUnsignedShiftRight:
		rightValue, leftValue, err := popBinary(frame)
		if err != nil {
			return err
		}
		if value, handled, err := executeBigIntBinary(context, opcode, leftValue, rightValue); handled || err != nil {
			if err != nil {
				return err
			}
			frame.push(value)
			return nil
		}
		left, err := execution.toNumber(leftValue)
		if err != nil {
			return err
		}
		right, err := execution.toNumber(rightValue)
		if err != nil {
			return err
		}
		switch opcode {
		case OpSubtract:
			frame.push(memory.NumberValue(left - right))
		case OpMultiply:
			frame.push(memory.NumberValue(left * right))
		case OpExponentiate:
			frame.push(memory.NumberValue(math.Pow(left, right)))
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
		}
		return nil
	case OpLessThan, OpLessThanOrEqual, OpGreaterThan, OpGreaterThanOrEqual:
		right, left, err := popBinary(frame)
		if err != nil {
			return err
		}
		comparison, unordered, err := execution.comparePrimitives(left, right)
		if err != nil {
			return err
		}
		result := false
		if !unordered {
			switch opcode {
			case OpLessThan:
				result = comparison < 0
			case OpLessThanOrEqual:
				result = comparison <= 0
			case OpGreaterThan:
				result = comparison > 0
			case OpGreaterThanOrEqual:
				result = comparison >= 0
			}
		}
		frame.push(memory.BoolValue(result))
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
