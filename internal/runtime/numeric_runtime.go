package runtime

import (
	"math"
	"math/bits"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func (intrinsics *Intrinsics) installNumericBuiltins(context *TaskContext) error {
	numberConstructor, err := intrinsics.newBuiltinMethod(context, "Number", 1, nativeNumberConstructor)
	if err != nil {
		return err
	}
	intrinsics.NumberConstructor = numberConstructor
	if err := defineData(context, numberConstructor, "prototype", memory.RefValue(intrinsics.NumberPrototype), false, false, false); err != nil {
		return err
	}
	if err := defineData(context, intrinsics.NumberPrototype, "constructor", memory.RefValue(numberConstructor), true, false, true); err != nil {
		return err
	}
	if err := installMethods(intrinsics, context, intrinsics.NumberPrototype, []builtinMethod{
		{"toString", 1, nativeNumberToString},
		{"valueOf", 0, nativeNumberValueOf},
	}); err != nil {
		return err
	}

	mathObject, err := context.NewHeapObject()
	if err != nil {
		return err
	}
	if err := context.SetPrototype(mathObject, memory.RefValue(intrinsics.ObjectPrototype)); err != nil {
		return err
	}
	intrinsics.MathObject = mathObject
	if err := installMethods(intrinsics, context, mathObject, []builtinMethod{
		{"clz32", 1, nativeMathCLZ32},
		{"floor", 1, nativeMathFloor},
		{"log", 1, nativeMathLog},
		{"min", 2, nativeMathMin},
		{"random", 0, nativeMathRandom},
	}); err != nil {
		return err
	}
	if err := defineData(context, mathObject, "LN2", memory.NumberValue(math.Ln2), false, false, false); err != nil {
		return err
	}

	now, err := intrinsics.newBuiltinMethod(context, "now", 0, nativeDateNow)
	if err != nil {
		return err
	}
	if err := defineData(context, intrinsics.DateConstructor, "now", memory.RefValue(now), true, false, true); err != nil {
		return err
	}
	isNaN, err := intrinsics.newBuiltinMethod(context, "isNaN", 1, nativeGlobalIsNaN)
	if err != nil {
		return err
	}
	intrinsics.IsNaN = isNaN
	return nil
}

func builtinMathCLZ32(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	number, err := execution.toNumber(argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	return memory.NumberValue(float64(bits.LeadingZeros32(toUint32(number)))), nil
}

func builtinMathFloor(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	number, err := execution.toNumber(argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	return memory.NumberValue(math.Floor(number)), nil
}

func builtinMathLog(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	number, err := execution.toNumber(argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	return memory.NumberValue(math.Log(number)), nil
}

func builtinMathMin(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	result := math.Inf(1)
	for _, argument := range arguments {
		number, err := execution.toNumber(argument)
		if err != nil {
			return memory.Value{}, err
		}
		result = math.Min(result, number)
	}
	return memory.NumberValue(result), nil
}

func builtinMathRandom(_ *execution, _ memory.Ref, _ memory.Function, _ memory.Value, _ []memory.Value) (memory.Value, error) {
	return memory.NumberValue(rand.Float64()), nil
}

func builtinDateConstructor(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	milliseconds := float64(time.Now().UnixMilli())
	if len(arguments) != 0 {
		var err error
		milliseconds, err = execution.toNumber(arguments[0])
		if err != nil {
			return memory.Value{}, err
		}
	}
	date, err := execution.context.NewDate(milliseconds)
	return memory.RefValue(date), err
}

func builtinDateNow(_ *execution, _ memory.Ref, _ memory.Function, _ memory.Value, _ []memory.Value) (memory.Value, error) {
	return memory.NumberValue(float64(time.Now().UnixMilli())), nil
}

func builtinGlobalIsNaN(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	number, err := execution.toNumber(argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	return memory.BoolValue(math.IsNaN(number)), nil
}

func builtinNumberConstructor(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	if len(arguments) == 0 {
		return memory.NumberValue(0), nil
	}
	number, err := execution.toNumber(arguments[0])
	return memory.NumberValue(number), err
}

func builtinNumberToString(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	if this.Kind() != memory.ValueNumber {
		return memory.Value{}, ErrOperandType
	}
	radix := 10
	if len(arguments) != 0 && arguments[0].Kind() != memory.ValueUndefined {
		number, err := execution.toNumber(arguments[0])
		if err != nil {
			return memory.Value{}, err
		}
		radix = int(number)
		if radix < 2 || radix > 36 || number != float64(radix) {
			return memory.Value{}, ErrOperandType
		}
	}
	text := numberString(this.Number())
	if radix != 10 {
		text = formatNumberRadix(this.Number(), radix)
	}
	return newStringValue(execution.context, text)
}

func builtinNumberValueOf(_ *execution, _ memory.Ref, _ memory.Function, this memory.Value, _ []memory.Value) (memory.Value, error) {
	if this.Kind() != memory.ValueNumber {
		return memory.Value{}, ErrOperandType
	}
	return this, nil
}

func formatNumberRadix(number float64, radix int) string {
	if math.IsNaN(number) || math.IsInf(number, 0) || number == 0 {
		return numberString(number)
	}
	sign := ""
	if number < 0 {
		sign = "-"
		number = -number
	}
	integer, fraction := math.Modf(number)
	integerText := strconv.FormatUint(uint64(integer), radix)
	if fraction == 0 {
		return sign + integerText
	}
	const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
	var fractional strings.Builder
	for fractional.Len() < 16 && fraction != 0 {
		fraction *= float64(radix)
		digit := int(fraction)
		fraction -= float64(digit)
		fractional.WriteByte(digits[digit])
	}
	return sign + integerText + "." + fractional.String()
}
