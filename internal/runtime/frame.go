package runtime

import (
	"errors"
	"fmt"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

var ErrStackUnderflow = errors.New("runtime: operand stack underflow")

type ExceptionHandlerKind uint32

const (
	HandlerCatch ExceptionHandlerKind = iota + 1
	HandlerFinally
)

type exceptionHandler struct {
	kind             ExceptionHandlerKind
	target           uint32
	stackDepth       int
	environmentDepth int
}

type completionKind uint8

const (
	completionReturn completionKind = iota + 1
	completionThrow
	completionBreak
	completionContinue
)

type abruptCompletion struct {
	kind             completionKind
	value            memory.Value
	target           uint32
	handlerDepth     int
	environmentDepth int
}

// RootSource exposes borrowed Refs without changing their ownership. Cycle
// reclamation checkpoints can use the same contract for frames and other
// transient runtime roots.
type RootSource interface {
	VisitRefs(func(memory.Ref) error) error
}

// Frame is one synchronous native Function invocation. Arguments and operand
// stack Values are borrowed for the frame's lifetime and add no owner claim.
type Frame struct {
	Function    memory.Ref
	Environment memory.Value
	This        memory.Value
	Arguments   []memory.Value
	Stack       []memory.Value

	function     memory.Function
	instructions []Instruction
	ip           uint32
	handlers     []exceptionHandler
	environments []memory.Value
	completion   *abruptCompletion
	current      *ThrownError
}

func (frame *Frame) VisitRefs(visit func(memory.Ref) error) error {
	if frame == nil || visit == nil {
		return nil
	}
	if frame.Function != (memory.Ref{}) {
		if err := visit(frame.Function); err != nil {
			return err
		}
	}
	visitValue := func(value memory.Value) error {
		if value.IsRef() {
			return visit(value.Ref())
		}
		return nil
	}
	if err := visitValue(frame.Environment); err != nil {
		return err
	}
	if err := visitValue(frame.This); err != nil {
		return err
	}
	for _, value := range frame.Arguments {
		if err := visitValue(value); err != nil {
			return err
		}
	}
	for _, value := range frame.Stack {
		if err := visitValue(value); err != nil {
			return err
		}
	}
	for _, value := range frame.environments {
		if err := visitValue(value); err != nil {
			return err
		}
	}
	if frame.completion != nil {
		if err := visitValue(frame.completion.value); err != nil {
			return err
		}
	}
	if frame.current != nil {
		if err := visitValue(frame.current.Value); err != nil {
			return err
		}
	}
	return nil
}

// reset releases every borrowed Ref while retaining reusable operand and
// argument buffers for the next call at this task's stack depth.
func (frame *Frame) reset() {
	if frame == nil {
		return
	}
	frame.Function = memory.Ref{}
	frame.Environment = memory.Value{}
	frame.This = memory.Value{}
	clear(frame.Arguments)
	frame.Arguments = frame.Arguments[:0]
	clear(frame.Stack)
	frame.Stack = frame.Stack[:0]
	frame.function = memory.Function{}
	frame.instructions = nil
	frame.ip = 0
	clear(frame.handlers)
	frame.handlers = frame.handlers[:0]
	clear(frame.environments)
	frame.environments = frame.environments[:0]
	frame.completion = nil
	frame.current = nil
}

func (frame *Frame) push(value memory.Value) {
	frame.Stack = append(frame.Stack, value)
}

func (frame *Frame) pop() (memory.Value, error) {
	if frame == nil || len(frame.Stack) == 0 {
		return memory.Value{}, ErrStackUnderflow
	}
	index := len(frame.Stack) - 1
	value := frame.Stack[index]
	frame.Stack[index] = memory.Value{}
	frame.Stack = frame.Stack[:index]
	return value, nil
}

func (frame *Frame) peek() (memory.Value, error) {
	if frame == nil || len(frame.Stack) == 0 {
		return memory.Value{}, ErrStackUnderflow
	}
	return frame.Stack[len(frame.Stack)-1], nil
}

func (frame *Frame) next() (Instruction, error) {
	if frame == nil || uint64(frame.ip) >= uint64(len(frame.instructions)) {
		return Instruction{}, fmt.Errorf("%w: instruction pointer %d has no Return", ErrInvalidBytecode, frame.ip)
	}
	instruction := frame.instructions[frame.ip]
	frame.ip++
	return instruction, nil
}
