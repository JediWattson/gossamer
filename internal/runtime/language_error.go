package runtime

import (
	"errors"
	"fmt"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func (execution *execution) routeFrameError(frame *Frame, err error) (bool, error) {
	if err == nil {
		return false, nil
	}
	value, thrown := ThrownValue(err)
	if !thrown {
		kind, languageError := classifyLanguageError(err)
		if !languageError {
			return false, err
		}
		instruction := uint32(0)
		if frame != nil && frame.ip > 0 {
			instruction = frame.ip - 1
		}
		name := "<anonymous>"
		if frame != nil && frame.function.Name.IsRef() {
			if text, nameErr := execution.context.DerefString(frame.function.Name.Ref()); nameErr == nil && text != "" {
				name = text
			}
		}
		location := ""
		if frame != nil && uint64(instruction) < uint64(len(frame.function.Locations)) {
			span := frame.function.Locations[instruction]
			location = fmt.Sprintf(" source %d..%d", span.Start, span.End)
		}
		err = fmt.Errorf("%w (in %s at bytecode %d%s)", err, name, instruction, location)
		message, allocErr := execution.context.NewString(err.Error())
		if allocErr != nil {
			return false, allocErr
		}
		ref, allocErr := execution.context.NewError(kind, memory.RefValue(message))
		if allocErr != nil {
			return false, allocErr
		}
		value = memory.RefValue(ref)
	}
	frame.completion = nil
	completed, _, routeErr := routeCompletion(frame, abruptCompletion{kind: completionThrow, value: value})
	if routeErr != nil {
		return false, routeErr
	}
	if completed {
		return false, Throw(value)
	}
	return true, nil
}

func classifyLanguageError(err error) (memory.ErrorKind, bool) {
	switch {
	case errors.Is(err, memory.ErrBindingNotFound), errors.Is(err, memory.ErrBindingUninitialized):
		return memory.ErrorReference, true
	case errors.Is(err, memory.ErrInvalidIndex):
		return memory.ErrorRange, true
	case errors.Is(err, ErrOperandType), errors.Is(err, ErrNotCallable), errors.Is(err, ErrNotConstructor),
		errors.Is(err, memory.ErrTypeMismatch), errors.Is(err, memory.ErrImmutableBinding),
		errors.Is(err, memory.ErrReadOnlyProperty), errors.Is(err, memory.ErrNonConfigurable),
		errors.Is(err, memory.ErrAccessorProperty):
		return memory.ErrorType, true
	default:
		return 0, false
	}
}
