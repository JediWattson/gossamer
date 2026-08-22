package runtime

import (
	"fmt"
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
	if err := installMethods(intrinsics, context, numberConstructor, []builtinMethod{
		{"isFinite", 1, nativeNumberIsFinite},
		{"isNaN", 1, nativeNumberIsNaN},
		{"parseInt", 2, nativeGlobalParseInt},
		{"parseFloat", 1, nativeGlobalParseFloat},
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
		{"round", 1, nativeMathRound},
		{"ceil", 1, nativeMathCeil},
		{"abs", 1, nativeMathAbs},
		{"cbrt", 1, nativeMathCbrt},
		{"sqrt", 1, nativeMathSqrt},
		{"trunc", 1, nativeMathTrunc},
		{"sign", 1, nativeMathSign},
		{"log", 1, nativeMathLog},
		{"min", 2, nativeMathMin},
		{"max", 2, nativeMathMax},
		{"pow", 2, nativeMathPow},
		{"atan2", 2, nativeMathAtan2},
		{"cos", 1, nativeMathCos},
		{"sin", 1, nativeMathSin},
		{"hypot", 2, nativeMathHypot},
		{"random", 0, nativeMathRandom},
	}); err != nil {
		return err
	}
	if err := defineData(context, mathObject, "LN2", memory.NumberValue(math.Ln2), false, false, false); err != nil {
		return err
	}
	for _, constant := range []struct {
		name  string
		value float64
	}{
		{"E", math.E}, {"LN10", math.Ln10}, {"LOG2E", math.Log2E},
		{"LOG10E", math.Log10E}, {"PI", math.Pi}, {"SQRT1_2", math.Sqrt2 / 2}, {"SQRT2", math.Sqrt2},
	} {
		if err := defineData(context, mathObject, constant.name, memory.NumberValue(constant.value), false, false, false); err != nil {
			return err
		}
	}

	now, err := intrinsics.newBuiltinMethod(context, "now", 0, nativeDateNow)
	if err != nil {
		return err
	}
	if err := defineData(context, intrinsics.DateConstructor, "now", memory.RefValue(now), true, false, true); err != nil {
		return err
	}
	if err := installMethods(intrinsics, context, intrinsics.DatePrototype, []builtinMethod{
		{"getTime", 0, nativeDateGetTime},
		{"valueOf", 0, nativeDateGetTime},
		{"toISOString", 0, nativeDateToISOString},
		{"toJSON", 1, nativeDateToISOString},
		{"toLocaleTimeString", 0, nativeDateToLocaleTimeString},
		{"getHours", 0, nativeDateGetHours},
		{"getUTCMonth", 0, nativeDateGetUTCMonth},
		{"getUTCDate", 0, nativeDateGetUTCDate},
	}); err != nil {
		return err
	}
	isNaN, err := intrinsics.newBuiltinMethod(context, "isNaN", 1, nativeGlobalIsNaN)
	if err != nil {
		return err
	}
	intrinsics.IsNaN = isNaN
	for _, global := range []struct {
		name  string
		arity uint32
		id    uint64
	}{
		{"parseInt", 2, nativeGlobalParseInt},
		{"parseFloat", 1, nativeGlobalParseFloat},
		{"isFinite", 1, nativeGlobalIsFinite},
	} {
		callable, err := intrinsics.newBuiltinMethod(context, global.name, global.arity, global.id)
		if err != nil {
			return err
		}
		if err := intrinsics.defineGlobal(context, global.name, memory.RefValue(callable)); err != nil {
			return err
		}
	}
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

func builtinMathRound(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	number, err := execution.toNumber(argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	return memory.NumberValue(math.Floor(number + 0.5)), nil
}

func builtinMathCeil(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	number, err := execution.toNumber(argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	return memory.NumberValue(math.Ceil(number)), nil
}

func builtinMathAbs(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	number, err := execution.toNumber(argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	return memory.NumberValue(math.Abs(number)), nil
}

func builtinMathUnary(operation func(float64) float64) nativeFunction {
	return func(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
		number, err := execution.toNumber(argument(arguments, 0))
		if err != nil {
			return memory.Value{}, err
		}
		return memory.NumberValue(operation(number)), nil
	}
}

func builtinMathSign(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	number, err := execution.toNumber(argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	if math.IsNaN(number) || number == 0 {
		return memory.NumberValue(number), nil
	}
	if number < 0 {
		return memory.NumberValue(-1), nil
	}
	return memory.NumberValue(1), nil
}

func builtinMathPow(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	base, err := execution.toNumber(argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	exponent, err := execution.toNumber(argument(arguments, 1))
	if err != nil {
		return memory.Value{}, err
	}
	return memory.NumberValue(math.Pow(base, exponent)), nil
}

func builtinMathAtan2(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	y, err := execution.toNumber(argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	x, err := execution.toNumber(argument(arguments, 1))
	if err != nil {
		return memory.Value{}, err
	}
	return memory.NumberValue(math.Atan2(y, x)), nil
}

func builtinMathHypot(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	result := 0.0
	for _, argument := range arguments {
		number, err := execution.toNumber(argument)
		if err != nil {
			return memory.Value{}, err
		}
		result = math.Hypot(result, number)
	}
	return memory.NumberValue(result), nil
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

func builtinMathMax(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	result := math.Inf(-1)
	for _, argument := range arguments {
		number, err := execution.toNumber(argument)
		if err != nil {
			return memory.Value{}, err
		}
		result = math.Max(result, number)
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

func requireDate(execution *execution, this memory.Value) (memory.Date, error) {
	ref, err := requireKind(execution.context, this, memory.HeapDate, "Date receiver")
	if err != nil {
		return memory.Date{}, err
	}
	return execution.context.DerefDate(ref)
}

func builtinDateGetTime(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, _ []memory.Value) (memory.Value, error) {
	date, err := requireDate(execution, this)
	if err != nil {
		return memory.Value{}, err
	}
	return memory.NumberValue(date.Milliseconds), nil
}

func builtinDateToISOString(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, _ []memory.Value) (memory.Value, error) {
	date, err := requireDate(execution, this)
	if err != nil {
		return memory.Value{}, err
	}
	if math.IsNaN(date.Milliseconds) || math.IsInf(date.Milliseconds, 0) {
		return memory.Value{}, fmt.Errorf("%w: invalid Date", memory.ErrInvalidIndex)
	}
	formatted := time.UnixMilli(int64(date.Milliseconds)).UTC().Format("2006-01-02T15:04:05.000Z")
	return newStringValue(execution.context, formatted)
}

func builtinDateToLocaleTimeString(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, _ []memory.Value) (memory.Value, error) {
	date, err := requireDate(execution, this)
	if err != nil {
		return memory.Value{}, err
	}
	if math.IsNaN(date.Milliseconds) {
		return newStringValue(execution.context, "Invalid Date")
	}
	return newStringValue(execution.context, time.UnixMilli(int64(date.Milliseconds)).Format("3:04 PM"))
}

func builtinDateGetHours(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, _ []memory.Value) (memory.Value, error) {
	date, err := requireDate(execution, this)
	if err != nil {
		return memory.Value{}, err
	}
	return memory.NumberValue(float64(time.UnixMilli(int64(date.Milliseconds)).Hour())), nil
}

func builtinDateGetUTCMonth(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, _ []memory.Value) (memory.Value, error) {
	date, err := requireDate(execution, this)
	if err != nil {
		return memory.Value{}, err
	}
	return memory.NumberValue(float64(time.UnixMilli(int64(date.Milliseconds)).UTC().Month() - 1)), nil
}

func builtinDateGetUTCDate(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, _ []memory.Value) (memory.Value, error) {
	date, err := requireDate(execution, this)
	if err != nil {
		return memory.Value{}, err
	}
	return memory.NumberValue(float64(time.UnixMilli(int64(date.Milliseconds)).UTC().Day())), nil
}

func builtinGlobalIsNaN(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	number, err := execution.toNumber(argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	return memory.BoolValue(math.IsNaN(number)), nil
}

func builtinGlobalIsFinite(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	number, err := execution.toNumber(argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	return memory.BoolValue(!math.IsNaN(number) && !math.IsInf(number, 0)), nil
}

func builtinNumberIsFinite(_ *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	value := argument(arguments, 0)
	return memory.BoolValue(value.Kind() == memory.ValueNumber && !math.IsNaN(value.Number()) && !math.IsInf(value.Number(), 0)), nil
}

func builtinNumberIsNaN(_ *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	value := argument(arguments, 0)
	return memory.BoolValue(value.Kind() == memory.ValueNumber && math.IsNaN(value.Number())), nil
}

func builtinGlobalParseInt(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	text, err := execution.toString(argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	text = strings.TrimSpace(text)
	sign := 1.0
	if strings.HasPrefix(text, "+") || strings.HasPrefix(text, "-") {
		if text[0] == '-' {
			sign = -1
		}
		text = text[1:]
	}
	radix := 0
	if len(arguments) > 1 && arguments[1].Kind() != memory.ValueUndefined {
		number, numberErr := execution.toNumber(arguments[1])
		if numberErr != nil {
			return memory.Value{}, numberErr
		}
		radix = int(toUint32(number))
		if radix != 0 && (radix < 2 || radix > 36) {
			return memory.NumberValue(math.NaN()), nil
		}
	}
	if radix == 0 {
		radix = 10
		if strings.HasPrefix(text, "0x") || strings.HasPrefix(text, "0X") {
			radix = 16
			text = text[2:]
		}
	} else if radix == 16 && (strings.HasPrefix(text, "0x") || strings.HasPrefix(text, "0X")) {
		text = text[2:]
	}
	result := 0.0
	digits := 0
	for _, character := range text {
		digit := -1
		switch {
		case character >= '0' && character <= '9':
			digit = int(character - '0')
		case character >= 'a' && character <= 'z':
			digit = int(character-'a') + 10
		case character >= 'A' && character <= 'Z':
			digit = int(character-'A') + 10
		}
		if digit < 0 || digit >= radix {
			break
		}
		result = result*float64(radix) + float64(digit)
		digits++
	}
	if digits == 0 {
		return memory.NumberValue(math.NaN()), nil
	}
	return memory.NumberValue(sign * result), nil
}

func builtinGlobalParseFloat(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	text, err := execution.toString(argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	text = strings.TrimSpace(text)
	for end := len(text); end > 0; end-- {
		if number, parseErr := strconv.ParseFloat(text[:end], 64); parseErr == nil {
			return memory.NumberValue(number), nil
		}
	}
	return memory.NumberValue(math.NaN()), nil
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
