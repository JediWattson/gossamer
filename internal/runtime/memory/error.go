package memory

type ErrorKind uint8

const (
	ErrorGeneric ErrorKind = iota + 1
	ErrorEval
	ErrorRange
	ErrorReference
	ErrorSyntax
	ErrorType
	ErrorURI
	ErrorAggregate
)

func (kind ErrorKind) Name() string {
	switch kind {
	case ErrorGeneric:
		return "Error"
	case ErrorEval:
		return "EvalError"
	case ErrorRange:
		return "RangeError"
	case ErrorReference:
		return "ReferenceError"
	case ErrorSyntax:
		return "SyntaxError"
	case ErrorType:
		return "TypeError"
	case ErrorURI:
		return "URIError"
	case ErrorAggregate:
		return "AggregateError"
	default:
		return ""
	}
}

type ErrorObject struct {
	ObjectHeader
	Kind     ErrorKind
	Message  Value
	Stack    Value
	Cause    Value
	HasCause bool
	Errors   []Value
}

func cloneError(value ErrorObject) ErrorObject {
	return ErrorObject{
		ObjectHeader: cloneObjectHeader(value.ObjectHeader),
		Kind:         value.Kind,
		Message:      value.Message,
		Stack:        value.Stack,
		Cause:        value.Cause,
		HasCause:     value.HasCause,
		Errors:       append([]Value(nil), value.Errors...),
	}
}
