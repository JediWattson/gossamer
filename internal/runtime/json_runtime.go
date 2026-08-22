package runtime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

// installJSONBuiltins creates the task-local JSON namespace. Keeping it in the
// RegionStore means the namespace, its methods, and values produced by parse
// obey the same ownership and bulk-release rules as every other intrinsic.
func (intrinsics *Intrinsics) installJSONBuiltins(context *TaskContext) (memory.Ref, error) {
	object, err := context.NewHeapObject()
	if err != nil {
		return memory.Ref{}, err
	}
	for _, method := range []struct {
		name  string
		arity uint32
		id    uint64
	}{
		{"parse", 2, nativeJSONParse},
		{"stringify", 3, nativeJSONStringify},
	} {
		callable, methodErr := intrinsics.newBuiltinMethod(context, method.name, method.arity, method.id)
		if methodErr != nil {
			return memory.Ref{}, methodErr
		}
		if methodErr := defineData(context, object, method.name, memory.RefValue(callable), true, false, true); methodErr != nil {
			return memory.Ref{}, methodErr
		}
	}
	return object, nil
}

func builtinJSONParse(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	text, err := execution.toString(argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return memory.Value{}, fmt.Errorf("%w: invalid JSON: %v", ErrOperandType, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return memory.Value{}, fmt.Errorf("%w: invalid trailing JSON data", ErrOperandType)
	}
	return allocateJSONValue(execution.context, decoded)
}

func allocateJSONValue(context *TaskContext, decoded any) (memory.Value, error) {
	switch value := decoded.(type) {
	case nil:
		return memory.NullValue(), nil
	case bool:
		return memory.BoolValue(value), nil
	case json.Number:
		number, err := strconv.ParseFloat(string(value), 64)
		if err != nil {
			return memory.Value{}, fmt.Errorf("%w: invalid JSON number %q", ErrOperandType, value)
		}
		return memory.NumberValue(number), nil
	case string:
		return newStringValue(context, value)
	case []any:
		array, err := context.NewArray(uint32(len(value)))
		if err != nil {
			return memory.Value{}, err
		}
		for index, item := range value {
			itemValue, itemErr := allocateJSONValue(context, item)
			if itemErr != nil {
				return memory.Value{}, itemErr
			}
			if itemErr := context.SetArrayElement(array, uint32(index), itemValue); itemErr != nil {
				return memory.Value{}, itemErr
			}
		}
		return memory.RefValue(array), nil
	case map[string]any:
		object, err := context.NewHeapObject()
		if err != nil {
			return memory.Value{}, err
		}
		for name, item := range value {
			itemValue, itemErr := allocateJSONValue(context, item)
			if itemErr != nil {
				return memory.Value{}, itemErr
			}
			if itemErr := defineData(context, object, name, itemValue, true, true, true); itemErr != nil {
				return memory.Value{}, itemErr
			}
		}
		return memory.RefValue(object), nil
	default:
		return memory.Value{}, fmt.Errorf("%w: unsupported decoded JSON value %T", ErrOperandType, decoded)
	}
}

func builtinJSONStringify(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	encoded, included, err := encodeJSONValue(execution, argument(arguments, 0), make(map[memory.Ref]bool), false)
	if err != nil {
		return memory.Value{}, err
	}
	if !included {
		return memory.UndefinedValue(), nil
	}
	return newStringValue(execution.context, encoded)
}

func encodeJSONValue(execution *execution, value memory.Value, stack map[memory.Ref]bool, arrayElement bool) (string, bool, error) {
	switch value.Kind() {
	case memory.ValueUndefined:
		return omittedJSONValue(arrayElement)
	case memory.ValueNull:
		return "null", true, nil
	case memory.ValueBool:
		return strconv.FormatBool(value.Bool()), true, nil
	case memory.ValueNumber:
		if math.IsNaN(value.Number()) || math.IsInf(value.Number(), 0) {
			return "null", true, nil
		}
		if value.Number() == 0 {
			return "0", true, nil
		}
		return strconv.FormatFloat(value.Number(), 'g', -1, 64), true, nil
	case memory.ValueReference:
		// Continue below after giving object-like values their toJSON hook.
	default:
		return "", false, fmt.Errorf("%w: unsupported JSON value kind %d", ErrOperandType, value.Kind())
	}

	kind, err := execution.context.HeapKind(value.Ref())
	if err != nil {
		return "", false, err
	}
	switch kind {
	case memory.HeapString:
		text, err := execution.context.DerefString(value.Ref())
		if err != nil {
			return "", false, err
		}
		encoded, _ := json.Marshal(text)
		return string(encoded), true, nil
	case memory.HeapBigInt:
		return "", false, fmt.Errorf("%w: JSON.stringify cannot serialize a BigInt", ErrOperandType)
	case memory.HeapSymbol, memory.HeapFunction:
		return omittedJSONValue(arrayElement)
	}

	toJSONName, err := execution.context.NewString("toJSON")
	if err != nil {
		return "", false, err
	}
	toJSON, found, err := execution.getProperty(value, memory.RefValue(toJSONName))
	if err != nil {
		return "", false, err
	}
	if found && toJSON.Kind() != memory.ValueUndefined {
		callable, callableErr := requireCallable(execution.context, toJSON)
		if callableErr != nil {
			return "", false, callableErr
		}
		value, err = execution.call(callable, value, nil, callAny)
		if err != nil {
			return "", false, err
		}
		return encodeJSONValue(execution, value, stack, arrayElement)
	}

	if stack[value.Ref()] {
		return "", false, fmt.Errorf("%w: cyclic object value", ErrOperandType)
	}
	stack[value.Ref()] = true
	defer delete(stack, value.Ref())

	isArray, err := builtinArrayIsArray(execution, memory.Ref{}, memory.Function{}, memory.UndefinedValue(), []memory.Value{value})
	if err != nil {
		return "", false, err
	}
	if isArray.Bool() {
		arrayRef, err := execution.arrayReceiver(value)
		if err != nil {
			return "", false, err
		}
		array, err := execution.context.DerefArray(arrayRef)
		if err != nil {
			return "", false, err
		}
		parts := make([]string, array.Length)
		for index := uint32(0); index < array.Length; index++ {
			key, keyErr := execution.context.NewString(strconv.FormatUint(uint64(index), 10))
			if keyErr != nil {
				return "", false, keyErr
			}
			item, present, itemErr := execution.getProperty(value, memory.RefValue(key))
			if itemErr != nil {
				return "", false, itemErr
			}
			if !present {
				parts[index] = "null"
				continue
			}
			encoded, _, itemErr := encodeJSONValue(execution, item, stack, true)
			if itemErr != nil {
				return "", false, itemErr
			}
			parts[index] = encoded
		}
		return "[" + strings.Join(parts, ",") + "]", true, nil
	}

	keys, err := enumerableOwnPropertyKeys(execution, value)
	if err != nil {
		return "", false, err
	}
	var result bytes.Buffer
	result.WriteByte('{')
	wrote := false
	for _, key := range keys {
		if keyKind, keyErr := execution.context.HeapKind(key); keyErr != nil {
			return "", false, keyErr
		} else if keyKind != memory.HeapString {
			continue
		}
		name, err := execution.context.DerefString(key)
		if err != nil {
			return "", false, err
		}
		item, present, err := execution.getProperty(value, memory.RefValue(key))
		if err != nil {
			return "", false, err
		}
		if !present {
			continue
		}
		encoded, included, err := encodeJSONValue(execution, item, stack, false)
		if err != nil {
			return "", false, err
		}
		if !included {
			continue
		}
		if wrote {
			result.WriteByte(',')
		}
		encodedName, _ := json.Marshal(name)
		result.Write(encodedName)
		result.WriteByte(':')
		result.WriteString(encoded)
		wrote = true
	}
	result.WriteByte('}')
	return result.String(), true, nil
}

func omittedJSONValue(arrayElement bool) (string, bool, error) {
	if arrayElement {
		return "null", true, nil
	}
	return "", false, nil
}
