// Package program defines immutable, RegionStore-independent native program
// images and validates them before they can be loaded for execution.
package program

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"unsafe"

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
	ConstantBigInt
	ConstantString
	ConstantRegExp
	ConstantFunction
)

const constantInvalidRegExpFlags uint64 = 1 << 63

// Constant is a portable literal. It cannot contain a RegionStore Ref.
// text stores String, BigInt, and RegExp source text. payload stores exact
// Number bits, Bool state, Function index, or packed RegExp flags. Keeping the
// mutually exclusive variants in one tagged payload reduces each retained
// constant from 56 to 32 bytes on 64-bit targets.
type Constant struct {
	text    string
	payload uint64
	kind    ConstantKind
}

func Undefined() Constant { return Constant{kind: ConstantUndefined} }
func Null() Constant      { return Constant{kind: ConstantNull} }
func Bool(value bool) Constant {
	var payload uint64
	if value {
		payload = 1
	}
	return Constant{kind: ConstantBool, payload: payload}
}
func Number(value float64) Constant {
	return Constant{kind: ConstantNumber, payload: math.Float64bits(value)}
}
func BigInt(value string) Constant { return Constant{kind: ConstantBigInt, text: value} }
func String(value string) Constant { return Constant{kind: ConstantString, text: value} }
func RegExp(pattern, flags string) Constant {
	payload := packRegExpFlags(flags)
	return Constant{kind: ConstantRegExp, text: pattern, payload: payload}
}
func Function(index uint32) Constant { return Constant{kind: ConstantFunction, payload: uint64(index)} }

func (constant Constant) Kind() ConstantKind { return constant.kind }
func (constant Constant) Bool() bool {
	return constant.kind == ConstantBool && constant.payload != 0
}
func (constant Constant) Number() float64 {
	if constant.kind != ConstantNumber {
		return 0
	}
	return math.Float64frombits(constant.payload)
}
func (constant Constant) String() string { return constant.text }
func (constant Constant) Flags() string {
	if constant.kind != ConstantRegExp || constant.payload == constantInvalidRegExpFlags {
		return ""
	}
	var bytes [8]byte
	length := 0
	for value := constant.payload; value != 0; value >>= 8 {
		bytes[length] = byte(value)
		length++
	}
	return string(bytes[:length])
}
func (constant Constant) Function() uint32 {
	if constant.kind != ConstantFunction {
		return 0
	}
	return uint32(constant.payload)
}

func packRegExpFlags(flags string) uint64 {
	if len(flags) > 8 {
		return constantInvalidRegExpFlags
	}
	var payload uint64
	for index := range len(flags) {
		character := flags[index]
		// Valid ECMAScript flags are nonzero ASCII. Reject other encodings here
		// because they cannot be represented reversibly in this compact field;
		// validateFunction will turn the sentinel into ErrInvalidProgram.
		if character == 0 || character&0x80 != 0 {
			return constantInvalidRegExpFlags
		}
		payload |= uint64(character) << (8 * index)
	}
	return payload
}

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

// StorageStats describes backing storage retained by a portable Program. It
// counts slice capacity rather than length because an unused tail remains a
// live Go allocation until the Program is released.
type StorageStats struct {
	Functions           uint64
	FunctionTableBytes  uint64
	CodeBytes           uint64
	ConstantTableBytes  uint64
	SourceLocationBytes uint64
}

func New(functions []FunctionTemplate, entry uint32) (Program, error) {
	cloned := make([]FunctionTemplate, len(functions))
	for index, function := range functions {
		cloned[index] = cloneFunction(function)
	}
	return newOwned(cloned, entry)
}

// NewOwned validates and adopts FunctionTemplate storage without cloning it.
// The caller relinquishes every supplied slice and must not mutate them after
// the call. Compilers use this boundary when their builder storage has no
// remaining owner; general callers should use New's defensive-copy contract.
func NewOwned(functions []FunctionTemplate, entry uint32) (Program, error) {
	return newOwned(functions, entry)
}

func newOwned(functions []FunctionTemplate, entry uint32) (Program, error) {
	if len(functions) == 0 {
		return Program{}, fmt.Errorf("%w: no functions", ErrInvalidProgram)
	}
	if uint64(entry) >= uint64(len(functions)) {
		return Program{}, fmt.Errorf("%w: entry function %d", ErrInvalidProgram, entry)
	}
	for index := range functions {
		if err := validateFunction(functions[index], len(functions), index); err != nil {
			return Program{}, err
		}
	}
	if err := rejectFunctionCycles(functions); err != nil {
		return Program{}, err
	}
	return Program{entry: entry, functions: functions}, nil
}

func (image Program) Entry() uint32 { return image.entry }

func (image Program) FunctionCount() int { return len(image.functions) }

func (image Program) StorageStats() StorageStats {
	stats := StorageStats{
		Functions:          uint64(len(image.functions)),
		FunctionTableBytes: uint64(cap(image.functions)) * uint64(unsafe.Sizeof(FunctionTemplate{})),
	}
	for index := range image.functions {
		function := &image.functions[index]
		stats.CodeBytes += uint64(cap(function.Code))
		stats.ConstantTableBytes += uint64(cap(function.Constants)) * uint64(unsafe.Sizeof(Constant{}))
		stats.SourceLocationBytes += uint64(cap(function.Locations)) * uint64(unsafe.Sizeof(browserruntime.SourceSpan{}))
	}
	return stats
}

func (image Program) Function(index uint32) (FunctionTemplate, bool) {
	if uint64(index) >= uint64(len(image.functions)) {
		return FunctionTemplate{}, false
	}
	return cloneFunction(image.functions[index]), true
}

// functionView returns immutable Program-owned storage to the loader. It stays
// package-private so external callers retain the defensive-copy contract of
// Function while RegionStore Function descriptors can alias executable bytes.
func (image Program) functionView(index uint32) (FunctionTemplate, bool) {
	if uint64(index) >= uint64(len(image.functions)) {
		return FunctionTemplate{}, false
	}
	return image.functions[index], true
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
		case ConstantBigInt:
			if _, ok := new(big.Int).SetString(constant.text, 0); !ok {
				return fmt.Errorf("%w: function %d constant %d has invalid BigInt %q", ErrInvalidProgram, index, constantIndex, constant.text)
			}
		case ConstantRegExp:
			if constant.payload == constantInvalidRegExpFlags {
				return fmt.Errorf("%w: function %d constant %d has invalid RegExp flags", ErrInvalidProgram, index, constantIndex)
			}
			if _, err := memory.ParseRegExpFlags(constant.Flags()); err != nil {
				return fmt.Errorf("%w: function %d constant %d: %v", ErrInvalidProgram, index, constantIndex, err)
			}
		case ConstantFunction:
			if constant.payload > math.MaxUint32 || constant.payload >= uint64(functionCount) {
				return fmt.Errorf("%w: function %d constant %d references function %d", ErrInvalidProgram, index, constantIndex, constant.payload)
			}
		default:
			return fmt.Errorf("%w: function %d constant %d has kind %d", ErrInvalidProgram, index, constantIndex, constant.kind)
		}
		if constant.kind == ConstantNumber && math.IsNaN(constant.Number()) {
			// NaN is valid. This explicit branch documents that portable constants
			// preserve its IEEE payload rather than using it as a sentinel.
			_ = constant.payload
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
				if err := visit(int(constant.Function())); err != nil {
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
