package memory

// FunctionKind distinguishes region-owned bytecode from an opaque host
// callback handle. Native IDs are numeric and never carry a Go pointer.
type FunctionKind uint8

const (
	FunctionBytecode FunctionKind = iota + 1
	FunctionNative
)

// FunctionThisMode records whether a bytecode function receives its call-site
// receiver or the lexical receiver captured when an arrow closure is created.
type FunctionThisMode uint8

const (
	FunctionThisDynamic FunctionThisMode = iota
	FunctionThisLexical
)

// SourceSpan identifies the half-open source byte range that emitted one
// bytecode instruction. It is diagnostic metadata, not a heap reference.
type SourceSpan struct {
	Start uint32
	End   uint32
}

// Function is an immutable executable descriptor. Name is null or a String
// Ref; Environment is null or a Context Ref. Constants may contain scalars or
// arbitrary heap Refs.
type Function struct {
	ObjectHeader
	Kind          FunctionKind
	Name          Value
	Environment   Value
	Arity         uint32
	Constructible bool
	ThisMode      FunctionThisMode
	LexicalThis   Value
	Code          []byte
	Locations     []SourceSpan
	Constants     []Value
	Captures      []Value
	NativeID      uint64
}

func cloneFunction(function Function) Function {
	return Function{
		ObjectHeader:  cloneObjectHeader(function.ObjectHeader),
		Kind:          function.Kind,
		Name:          function.Name,
		Environment:   function.Environment,
		Arity:         function.Arity,
		Constructible: function.Constructible,
		ThisMode:      function.ThisMode,
		LexicalThis:   function.LexicalThis,
		Code:          append([]byte(nil), function.Code...),
		Locations:     append([]SourceSpan(nil), function.Locations...),
		Constants:     append([]Value(nil), function.Constants...),
		Captures:      append([]Value(nil), function.Captures...),
		NativeID:      function.NativeID,
	}
}

// loadFunction returns the immutable execution fields without cloning their
// backing storage. ObjectHeader is deliberately omitted because executable
// consumers must use the ordinary property APIs for mutable Function object
// state.
func loadFunction(function Function) Function {
	return Function{
		Kind:          function.Kind,
		Name:          function.Name,
		Environment:   function.Environment,
		Arity:         function.Arity,
		Constructible: function.Constructible,
		ThisMode:      function.ThisMode,
		LexicalThis:   function.LexicalThis,
		Code:          function.Code,
		Locations:     function.Locations,
		Constants:     function.Constants,
		Captures:      function.Captures,
		NativeID:      function.NativeID,
	}
}

func storeFunction(function Function, shareCode, shareConstants bool) Function {
	stored := Function{
		ObjectHeader:  cloneObjectHeader(function.ObjectHeader),
		Kind:          function.Kind,
		Name:          function.Name,
		Environment:   function.Environment,
		Arity:         function.Arity,
		Constructible: function.Constructible,
		ThisMode:      function.ThisMode,
		LexicalThis:   function.LexicalThis,
		Captures:      append([]Value(nil), function.Captures...),
		NativeID:      function.NativeID,
	}
	if shareCode {
		stored.Code = function.Code
		stored.Locations = function.Locations
	} else {
		stored.Code = append([]byte(nil), function.Code...)
		stored.Locations = append([]SourceSpan(nil), function.Locations...)
	}
	if shareConstants {
		stored.Constants = function.Constants
	} else {
		stored.Constants = append([]Value(nil), function.Constants...)
	}
	return stored
}

func equalValues(left, right []Value) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
