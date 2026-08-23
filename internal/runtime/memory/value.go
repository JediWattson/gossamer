package memory

import "math"

// ValueKind identifies the small set of synthetic values supported by the
// first native heap. It is intentionally not a JavaScript value model.
type ValueKind uint8

const (
	ValueUndefined ValueKind = iota
	ValueNull
	ValueBool
	ValueNumber
	ValueReference
)

const (
	valueNullBits         uint64 = 0x7ff8000000000001
	valueFalseBits        uint64 = 0x7ff8000000000002
	valueTrueBits         uint64 = 0x7ff8000000000003
	valueReferenceBits    uint64 = 0x7ff8000000000004
	valueNaNBits          uint64 = 0x7ff8000000000005
	valuePositiveZeroBits uint64 = 0x7ff8000000000006
)

// Value keeps Ref explicit while using otherwise-unobservable NaN payloads as
// primitive tags. The zero struct remains Undefined. Numeric NaNs are
// canonicalized and positive zero gets its own tag; every other numeric value
// retains its exact IEEE bits, including negative zero. This reduces Value
// from 32 to 24 bytes without packing or weakening Region/Slot/Generation.
type Value struct {
	ref  Ref
	bits uint64
}

func UndefinedValue() Value { return Value{} }
func NullValue() Value      { return Value{bits: valueNullBits} }
func BoolValue(value bool) Value {
	if value {
		return Value{bits: valueTrueBits}
	}
	return Value{bits: valueFalseBits}
}
func NumberValue(value float64) Value {
	if value == 0 && !math.Signbit(value) {
		return Value{bits: valuePositiveZeroBits}
	}
	if math.IsNaN(value) {
		return Value{bits: valueNaNBits}
	}
	return Value{bits: math.Float64bits(value)}
}
func RefValue(ref Ref) Value { return Value{ref: ref, bits: valueReferenceBits} }

func (value Value) Kind() ValueKind {
	switch value.bits {
	case 0:
		return ValueUndefined
	case valueNullBits:
		return ValueNull
	case valueFalseBits, valueTrueBits:
		return ValueBool
	case valueReferenceBits:
		return ValueReference
	default:
		return ValueNumber
	}
}

func (value Value) IsRef() bool { return value.bits == valueReferenceBits }
func (value Value) Ref() Ref    { return value.ref }
func (value Value) Bool() bool  { return value.bits == valueTrueBits }
func (value Value) Number() float64 {
	switch value.bits {
	case 0, valueNullBits, valueFalseBits, valueTrueBits, valueReferenceBits:
		return 0
	case valuePositiveZeroBits:
		return 0
	case valueNaNBits:
		return math.NaN()
	default:
		return math.Float64frombits(value.bits)
	}
}
