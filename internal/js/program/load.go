package program

import (
	"fmt"

	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

// Loaded is one task-region instantiation of a portable Program. Function Refs
// are borrowed and remain valid only for the lifetime selected by the caller.
type Loaded struct {
	Entry     memory.Ref
	Functions []memory.Ref
}

// Load allocates all Strings and Function descriptors through TaskContext. The
// image itself remains reusable and contains no Ref identity.
func Load(context *browserruntime.TaskContext, image Program, environment memory.Value) (Loaded, error) {
	if context == nil || context.Realm == nil {
		return Loaded{}, fmt.Errorf("%w: nil task context", ErrInvalidProgram)
	}
	if image.FunctionCount() == 0 {
		return Loaded{}, fmt.Errorf("%w: empty image", ErrInvalidProgram)
	}
	loader := &programLoader{
		context:   context,
		image:     image,
		functions: make([]memory.Ref, image.FunctionCount()),
		states:    make([]uint8, image.FunctionCount()),
		strings:   make(map[string]memory.Ref),
	}
	entry, err := loader.loadFunction(image.Entry(), environment)
	if err != nil {
		return Loaded{}, err
	}
	for index := 0; index < image.FunctionCount(); index++ {
		if loader.functions[index] == (memory.Ref{}) {
			if _, err := loader.loadFunction(uint32(index), memory.NullValue()); err != nil {
				return Loaded{}, err
			}
		}
	}
	return Loaded{Entry: entry, Functions: append([]memory.Ref(nil), loader.functions...)}, nil
}

type programLoader struct {
	context   *browserruntime.TaskContext
	image     Program
	functions []memory.Ref
	states    []uint8
	strings   map[string]memory.Ref
}

func (loader *programLoader) loadFunction(index uint32, environment memory.Value) (memory.Ref, error) {
	if uint64(index) >= uint64(len(loader.functions)) {
		return memory.Ref{}, fmt.Errorf("%w: function %d", ErrInvalidProgram, index)
	}
	if loader.functions[index] != (memory.Ref{}) {
		return loader.functions[index], nil
	}
	if loader.states[index] == 1 {
		return memory.Ref{}, fmt.Errorf("%w: function template cycle at %d", ErrInvalidProgram, index)
	}
	loader.states[index] = 1
	function, ok := loader.image.Function(index)
	if !ok {
		return memory.Ref{}, fmt.Errorf("%w: function %d", ErrInvalidProgram, index)
	}
	constants := make([]memory.Value, len(function.Constants))
	for constantIndex, constant := range function.Constants {
		value, err := loader.loadConstant(constant)
		if err != nil {
			return memory.Ref{}, fmt.Errorf("%w: function %d constant %d: %v", ErrInvalidProgram, index, constantIndex, err)
		}
		constants[constantIndex] = value
	}
	name := memory.NullValue()
	if function.Name != "" {
		ref, err := loader.loadString(function.Name)
		if err != nil {
			return memory.Ref{}, err
		}
		name = memory.RefValue(ref)
	}
	ref, err := loader.context.NewBytecodeFunction(name, environment, function.Arity, function.Code, constants)
	if err != nil {
		return memory.Ref{}, err
	}
	loader.functions[index] = ref
	loader.states[index] = 2
	return ref, nil
}

func (loader *programLoader) loadConstant(constant Constant) (memory.Value, error) {
	switch constant.Kind() {
	case ConstantUndefined:
		return memory.UndefinedValue(), nil
	case ConstantNull:
		return memory.NullValue(), nil
	case ConstantBool:
		return memory.BoolValue(constant.Bool()), nil
	case ConstantNumber:
		return memory.NumberValue(constant.Number()), nil
	case ConstantString:
		ref, err := loader.loadString(constant.String())
		if err != nil {
			return memory.Value{}, err
		}
		return memory.RefValue(ref), nil
	case ConstantRegExp:
		pattern, err := loader.loadString(constant.String())
		if err != nil {
			return memory.Value{}, err
		}
		ref, err := loader.context.NewRegExp(pattern, constant.Flags())
		return memory.RefValue(ref), err
	case ConstantFunction:
		ref, err := loader.loadFunction(constant.Function(), memory.NullValue())
		if err != nil {
			return memory.Value{}, err
		}
		return memory.RefValue(ref), nil
	default:
		return memory.Value{}, fmt.Errorf("unknown constant kind %d", constant.Kind())
	}
}

func (loader *programLoader) loadString(text string) (memory.Ref, error) {
	if ref, exists := loader.strings[text]; exists {
		return ref, nil
	}
	ref, err := loader.context.NewString(text)
	if err != nil {
		return memory.Ref{}, err
	}
	loader.strings[text] = ref
	return ref, nil
}
