package runtime

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func (intrinsics *Intrinsics) installURIBuiltins(context *TaskContext) error {
	for _, global := range []struct {
		name string
		id   uint64
	}{
		{"encodeURIComponent", nativeEncodeURIComponent},
		{"decodeURIComponent", nativeDecodeURIComponent},
	} {
		callable, err := intrinsics.newBuiltinMethod(context, global.name, 1, global.id)
		if err != nil {
			return err
		}
		if err := intrinsics.defineGlobal(context, global.name, memory.RefValue(callable)); err != nil {
			return err
		}
	}
	boolean, err := intrinsics.newBuiltinMethod(context, "Boolean", 1, nativeBooleanConstructor)
	if err != nil {
		return err
	}
	if err := intrinsics.defineGlobal(context, "Boolean", memory.RefValue(boolean)); err != nil {
		return err
	}
	return nil
}

func builtinBooleanConstructor(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	truthy, err := valueTruthy(execution.context, argument(arguments, 0))
	return memory.BoolValue(truthy), err
}

func builtinEncodeURIComponent(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	text, err := execution.toString(argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	const hexadecimal = "0123456789ABCDEF"
	var encoded strings.Builder
	for _, octet := range []byte(text) {
		if octet >= 'a' && octet <= 'z' || octet >= 'A' && octet <= 'Z' || octet >= '0' && octet <= '9' || strings.ContainsRune("-_.!~*'()", rune(octet)) {
			encoded.WriteByte(octet)
			continue
		}
		encoded.WriteByte('%')
		encoded.WriteByte(hexadecimal[octet>>4])
		encoded.WriteByte(hexadecimal[octet&15])
	}
	return newStringValue(execution.context, encoded.String())
}

func builtinDecodeURIComponent(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	text, err := execution.toString(argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	decoded, err := url.PathUnescape(text)
	if err != nil {
		return memory.Value{}, fmt.Errorf("%w: malformed URI component", ErrOperandType)
	}
	return newStringValue(execution.context, decoded)
}
