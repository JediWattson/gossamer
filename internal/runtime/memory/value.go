package memory

// ValueKind identifies the small set of synthetic values supported by the
// first native heap. It is intentionally not a JavaScript value model.
type ValueKind uint8

const (
	ValueUndefined ValueKind = iota
	ValueBool
	ValueNumber
	ValueString
	ValueReference
)

// Value is the eventual socket for JavaScript values. Phase 0 only needs a few
// primitives and explicit Ref values to exercise storage and barriers.
type Value struct {
	kind   ValueKind
	bool   bool
	number float64
	text   string
	ref    Ref
}

func UndefinedValue() Value      { return Value{} }
func BoolValue(value bool) Value { return Value{kind: ValueBool, bool: value} }
func NumberValue(value float64) Value {
	return Value{kind: ValueNumber, number: value}
}
func StringValue(value string) Value {
	return Value{kind: ValueString, text: value}
}
func RefValue(ref Ref) Value { return Value{kind: ValueReference, ref: ref} }

func (value Value) Kind() ValueKind { return value.kind }
func (value Value) IsRef() bool     { return value.kind == ValueReference }
func (value Value) Ref() Ref        { return value.ref }
func (value Value) Bool() bool      { return value.bool }
func (value Value) Number() float64 { return value.number }
func (value Value) String() string  { return value.text }
