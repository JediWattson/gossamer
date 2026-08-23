package memory_test

import (
	"math"
	"testing"
	"unsafe"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func TestCompactValuePreservesPrimitiveAndReferenceIdentity(t *testing.T) {
	if size := unsafe.Sizeof(memory.Value{}); size != 24 {
		t.Fatalf("Value size = %d bytes, want 24", size)
	}
	if value := (memory.Value{}); value.Kind() != memory.ValueUndefined || value != memory.UndefinedValue() {
		t.Fatalf("zero Value = %#v", value)
	}
	if value := memory.NullValue(); value.Kind() != memory.ValueNull {
		t.Fatalf("Null Kind = %d", value.Kind())
	}
	for label, value := range map[string]memory.Value{
		"undefined": memory.UndefinedValue(),
		"null":      memory.NullValue(),
		"false":     memory.BoolValue(false),
		"true":      memory.BoolValue(true),
		"reference": memory.RefValue(memory.Ref{Region: 1, Slot: 2, Gen: 3}),
	} {
		if number := value.Number(); number != 0 {
			t.Fatalf("%s Number = %v, want legacy zero value", label, number)
		}
	}
	for _, boolean := range []bool{false, true} {
		value := memory.BoolValue(boolean)
		if value.Kind() != memory.ValueBool || value.Bool() != boolean {
			t.Fatalf("BoolValue(%t) = kind %d, value %t", boolean, value.Kind(), value.Bool())
		}
	}
	for _, number := range []float64{0, math.Copysign(0, -1), 42.5, math.Inf(1), math.Inf(-1), math.NaN()} {
		value := memory.NumberValue(number)
		if value.Kind() != memory.ValueNumber {
			t.Fatalf("NumberValue(%v) Kind = %d", number, value.Kind())
		}
		got := value.Number()
		if math.IsNaN(number) {
			if !math.IsNaN(got) {
				t.Fatalf("NaN round trip = %v", got)
			}
			continue
		}
		if math.Float64bits(got) != math.Float64bits(number) {
			t.Fatalf("NumberValue(%v) bits = %016x, want %016x", number, math.Float64bits(got), math.Float64bits(number))
		}
	}
	for _, ref := range []memory.Ref{{}, {Region: 7, Slot: 42, Gen: 3}} {
		value := memory.RefValue(ref)
		if !value.IsRef() || value.Kind() != memory.ValueReference || value.Ref() != ref {
			t.Fatalf("RefValue(%s) = %#v", ref, value)
		}
	}
}

func TestCompactValueKeepsSameValueNumberDistinctions(t *testing.T) {
	positiveZero := memory.NumberValue(0)
	negativeZero := memory.NumberValue(math.Copysign(0, -1))
	if positiveZero == negativeZero {
		t.Fatal("positive and negative zero collapsed in physical Value identity")
	}
	if memory.NumberValue(math.NaN()) != memory.NumberValue(math.NaN()) {
		t.Fatal("numeric NaN was not canonicalized")
	}
}
