package memory

// FunctionKind distinguishes region-owned bytecode from an opaque host
// callback handle. Native IDs are numeric and never carry a Go pointer.
type FunctionKind uint8

const (
	FunctionBytecode FunctionKind = iota + 1
	FunctionNative
)

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
	Code          []byte
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
		Code:          append([]byte(nil), function.Code...),
		Constants:     append([]Value(nil), function.Constants...),
		Captures:      append([]Value(nil), function.Captures...),
		NativeID:      function.NativeID,
	}
}
