package runtime

import (
	"fmt"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func (intrinsics *Intrinsics) installFunctionBuiltins(context *TaskContext) error {
	return installMethods(intrinsics, context, intrinsics.FunctionPrototype, []builtinMethod{
		{"call", 1, nativeFunctionCall},
		{"apply", 2, nativeFunctionApply},
		{"bind", 1, nativeFunctionBind},
	})
}

func builtinFunctionCall(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	target, err := requireCallable(execution.context, this)
	if err != nil {
		return memory.Value{}, err
	}
	thisArgument := argument(arguments, 0)
	return execution.call(target, thisArgument, arguments[min(1, len(arguments)):], callAny)
}

func builtinFunctionApply(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	target, err := requireCallable(execution.context, this)
	if err != nil {
		return memory.Value{}, err
	}
	callArguments, err := createListFromArrayLike(execution, argument(arguments, 1))
	if err != nil {
		return memory.Value{}, err
	}
	return execution.call(target, argument(arguments, 0), callArguments, callAny)
}

func builtinFunctionBind(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	target, err := requireCallable(execution.context, this)
	if err != nil {
		return memory.Value{}, err
	}
	targetFunction, err := execution.context.DerefFunction(target)
	if err != nil {
		return memory.Value{}, err
	}
	boundArguments := arguments[min(1, len(arguments)):]
	boundArity := uint32(0)
	if uint32(len(boundArguments)) < targetFunction.Arity {
		boundArity = targetFunction.Arity - uint32(len(boundArguments))
	}
	nameText := "bound"
	if targetFunction.Name.IsRef() {
		if targetName, nameErr := execution.context.DerefString(targetFunction.Name.Ref()); nameErr == nil && targetName != "" {
			nameText += " " + targetName
		}
	}
	name, err := newStringValue(execution.context, nameText)
	if err != nil {
		return memory.Value{}, err
	}
	captures := make([]memory.Value, 0, 2+len(boundArguments))
	captures = append(captures, memory.RefValue(target), argument(arguments, 0))
	captures = append(captures, boundArguments...)
	bound, err := execution.context.NewBoundNativeFunction(name, memory.NullValue(), boundArity, nativeBoundFunction, captures...)
	return memory.RefValue(bound), err
}

func builtinBoundFunction(execution *execution, _ memory.Ref, function memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	if len(function.Captures) < 2 || !function.Captures[0].IsRef() {
		return memory.Value{}, fmt.Errorf("%w: invalid bound Function captures", ErrNotCallable)
	}
	combined := make([]memory.Value, 0, len(function.Captures)-2+len(arguments))
	combined = append(combined, function.Captures[2:]...)
	combined = append(combined, arguments...)
	return execution.call(function.Captures[0].Ref(), function.Captures[1], combined, callAny)
}

func createListFromArrayLike(execution *execution, value memory.Value) ([]memory.Value, error) {
	if value.Kind() == memory.ValueUndefined || value.Kind() == memory.ValueNull {
		return nil, nil
	}
	if !value.IsRef() {
		return nil, fmt.Errorf("%w: apply arguments must be array-like", ErrOperandType)
	}
	lengthName, err := execution.context.NewString("length")
	if err != nil {
		return nil, err
	}
	lengthValue, found, err := execution.getProperty(value, memory.RefValue(lengthName))
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	length, err := requireUint32(lengthValue, "apply argument length", true)
	if err != nil {
		return nil, err
	}
	result := make([]memory.Value, length)
	for index := uint32(0); index < length; index++ {
		key, err := execution.context.NewString(fmt.Sprintf("%d", index))
		if err != nil {
			return nil, err
		}
		item, found, err := execution.getProperty(value, memory.RefValue(key))
		if err != nil {
			return nil, err
		}
		if found {
			result[index] = item
		} else {
			result[index] = memory.UndefinedValue()
		}
	}
	return result, nil
}

func (execution *execution) popSpreadCallOperands(frame *Frame) (memory.Ref, []memory.Value, error) {
	argumentList, err := frame.pop()
	if err != nil {
		return memory.Ref{}, nil, err
	}
	arguments, err := createListFromArrayLike(execution, argumentList)
	if err != nil {
		return memory.Ref{}, nil, err
	}
	calleeValue, err := frame.pop()
	if err != nil {
		return memory.Ref{}, nil, err
	}
	callee, err := requireRef(calleeValue, "Function")
	return callee, arguments, err
}

func (execution *execution) popSpreadMethodOperands(frame *Frame) (memory.Value, memory.Value, []memory.Value, error) {
	argumentList, err := frame.pop()
	if err != nil {
		return memory.Value{}, memory.Value{}, nil, err
	}
	arguments, err := createListFromArrayLike(execution, argumentList)
	if err != nil {
		return memory.Value{}, memory.Value{}, nil, err
	}
	key, err := frame.pop()
	if err != nil {
		return memory.Value{}, memory.Value{}, nil, err
	}
	base, err := frame.pop()
	return base, key, arguments, err
}
