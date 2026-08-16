// Package program defines immutable, RegionStore-independent native program
// images and validates them before they can be loaded for execution.
package program

import (
	"errors"
	"fmt"
	"math"

	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

var ErrInvalidProgram = errors.New("js/program: invalid program")

type ConstantKind uint8

const (
	ConstantUndefined ConstantKind = iota + 1
	ConstantNull
	ConstantBool
	ConstantNumber
	ConstantString
	ConstantRegExp
	ConstantFunction
)

// Constant is a portable literal. It cannot contain a RegionStore Ref.
type Constant struct {
	kind     ConstantKind
	boolean  bool
	number   float64
	text     string
	flags    string
	function uint32
}

func Undefined() Constant           { return Constant{kind: ConstantUndefined} }
func Null() Constant                { return Constant{kind: ConstantNull} }
func Bool(value bool) Constant      { return Constant{kind: ConstantBool, boolean: value} }
func Number(value float64) Constant { return Constant{kind: ConstantNumber, number: value} }
func String(value string) Constant  { return Constant{kind: ConstantString, text: value} }
func RegExp(pattern, flags string) Constant {
	return Constant{kind: ConstantRegExp, text: pattern, flags: flags}
}
func Function(index uint32) Constant { return Constant{kind: ConstantFunction, function: index} }

func (constant Constant) Kind() ConstantKind { return constant.kind }
func (constant Constant) Bool() bool         { return constant.boolean }
func (constant Constant) Number() float64    { return constant.number }
func (constant Constant) String() string     { return constant.text }
func (constant Constant) Flags() string      { return constant.flags }
func (constant Constant) Function() uint32   { return constant.function }

// FunctionTemplate is the mutable constructor input for one immutable
// function image. New deep-copies every slice.
type FunctionTemplate struct {
	Name      string
	Arity     uint32
	Code      []byte
	Constants []Constant
	Locations []browserruntime.SourceSpan
}

// Program is an immutable graph of function templates. Accessors return deep
// copies so callers cannot alter a validated image after construction.
type Program struct {
	entry     uint32
	functions []FunctionTemplate
}

func New(functions []FunctionTemplate, entry uint32) (Program, error) {
	if len(functions) == 0 {
		return Program{}, fmt.Errorf("%w: no functions", ErrInvalidProgram)
	}
	if uint64(entry) >= uint64(len(functions)) {
		return Program{}, fmt.Errorf("%w: entry function %d", ErrInvalidProgram, entry)
	}
	cloned := make([]FunctionTemplate, len(functions))
	for index, function := range functions {
		cloned[index] = cloneFunction(function)
		if err := validateFunction(cloned[index], len(functions), index); err != nil {
			return Program{}, err
		}
	}
	if err := rejectFunctionCycles(cloned); err != nil {
		return Program{}, err
	}
	return Program{entry: entry, functions: cloned}, nil
}

func (image Program) Entry() uint32 { return image.entry }

func (image Program) FunctionCount() int { return len(image.functions) }

func (image Program) Function(index uint32) (FunctionTemplate, bool) {
	if uint64(index) >= uint64(len(image.functions)) {
		return FunctionTemplate{}, false
	}
	return cloneFunction(image.functions[index]), true
}

func cloneFunction(function FunctionTemplate) FunctionTemplate {
	return FunctionTemplate{
		Name:      function.Name,
		Arity:     function.Arity,
		Code:      append([]byte(nil), function.Code...),
		Constants: append([]Constant(nil), function.Constants...),
		Locations: append([]browserruntime.SourceSpan(nil), function.Locations...),
	}
}

func validateFunction(function FunctionTemplate, functionCount, index int) error {
	if err := browserruntime.VerifyBytecode(function.Code, len(function.Constants)); err != nil {
		return fmt.Errorf("%w: function %d: %v", ErrInvalidProgram, index, err)
	}
	instructionCount := len(function.Code) / browserruntime.InstructionWidth
	if len(function.Locations) != 0 && len(function.Locations) != instructionCount {
		return fmt.Errorf("%w: function %d has %d locations for %d instructions", ErrInvalidProgram, index, len(function.Locations), instructionCount)
	}
	for locationIndex, location := range function.Locations {
		if location.End < location.Start {
			return fmt.Errorf("%w: function %d location %d is %d..%d", ErrInvalidProgram, index, locationIndex, location.Start, location.End)
		}
	}
	for constantIndex, constant := range function.Constants {
		switch constant.kind {
		case ConstantUndefined, ConstantNull, ConstantBool, ConstantNumber, ConstantString:
		case ConstantRegExp:
			if _, err := memory.ParseRegExpFlags(constant.flags); err != nil {
				return fmt.Errorf("%w: function %d constant %d: %v", ErrInvalidProgram, index, constantIndex, err)
			}
		case ConstantFunction:
			if uint64(constant.function) >= uint64(functionCount) {
				return fmt.Errorf("%w: function %d constant %d references function %d", ErrInvalidProgram, index, constantIndex, constant.function)
			}
		default:
			return fmt.Errorf("%w: function %d constant %d has kind %d", ErrInvalidProgram, index, constantIndex, constant.kind)
		}
		if constant.kind == ConstantNumber && math.IsNaN(constant.number) {
			// NaN is valid. This explicit branch documents that portable constants
			// preserve its IEEE payload rather than using it as a sentinel.
			_ = math.Float64bits(constant.number)
		}
	}
	return nil
}

func rejectFunctionCycles(functions []FunctionTemplate) error {
	const (
		unseen uint8 = iota
		visiting
		visited
	)
	states := make([]uint8, len(functions))
	var visit func(int) error
	visit = func(index int) error {
		switch states[index] {
		case visiting:
			return fmt.Errorf("%w: function template cycle at %d", ErrInvalidProgram, index)
		case visited:
			return nil
		}
		states[index] = visiting
		for _, constant := range functions[index].Constants {
			if constant.kind == ConstantFunction {
				if err := visit(int(constant.function)); err != nil {
					return err
				}
			}
		}
		states[index] = visited
		return nil
	}
	for index := range functions {
		if err := visit(index); err != nil {
			return err
		}
	}
	return nil
}
