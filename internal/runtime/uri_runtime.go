package runtime

import (
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

var errMalformedURI = errors.New("runtime: malformed URI sequence")

const (
	uriUnescaped       = "-_.!~*'()"
	uriReserved        = ";/?:@&=+$,#"
	uriEncodePreserved = uriUnescaped + uriReserved
)

func (intrinsics *Intrinsics) installURIBuiltins(context *TaskContext) error {
	for _, global := range []struct {
		name string
		id   uint64
	}{
		{"encodeURI", nativeEncodeURI},
		{"decodeURI", nativeDecodeURI},
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
	encoded, err := encodeURIText(text, uriUnescaped)
	if err != nil {
		return memory.Value{}, err
	}
	return newStringValue(execution.context, encoded)
}

func builtinEncodeURI(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	text, err := execution.toString(argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	encoded, err := encodeURIText(text, uriEncodePreserved)
	if err != nil {
		return memory.Value{}, err
	}
	return newStringValue(execution.context, encoded)
}

func encodeURIText(text, preserved string) (string, error) {
	if !utf8.ValidString(text) {
		return "", errMalformedURI
	}
	const hexadecimal = "0123456789ABCDEF"
	var encoded strings.Builder
	for _, octet := range []byte(text) {
		if asciiAlphaNumeric(octet) || strings.IndexByte(preserved, octet) >= 0 {
			encoded.WriteByte(octet)
			continue
		}
		encoded.WriteByte('%')
		encoded.WriteByte(hexadecimal[octet>>4])
		encoded.WriteByte(hexadecimal[octet&15])
	}
	return encoded.String(), nil
}

func builtinDecodeURIComponent(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	text, err := execution.toString(argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	decoded, err := decodeURIText(text, false)
	if err != nil {
		return memory.Value{}, err
	}
	return newStringValue(execution.context, decoded)
}

func builtinDecodeURI(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	text, err := execution.toString(argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	decoded, err := decodeURIText(text, true)
	if err != nil {
		return memory.Value{}, err
	}
	return newStringValue(execution.context, decoded)
}

func decodeURIText(text string, preserveReserved bool) (string, error) {
	if !utf8.ValidString(text) {
		return "", errMalformedURI
	}
	var decoded strings.Builder
	decoded.Grow(len(text))
	for index := 0; index < len(text); {
		if text[index] != '%' {
			decoded.WriteByte(text[index])
			index++
			continue
		}
		first, ok := percentOctet(text, index)
		if !ok {
			return "", errMalformedURI
		}
		if first < utf8.RuneSelf {
			if preserveReserved && strings.IndexByte(uriReserved, first) >= 0 {
				decoded.WriteString(text[index : index+3])
			} else {
				decoded.WriteByte(first)
			}
			index += 3
			continue
		}
		width := utf8SequenceWidth(first)
		if width == 0 || index+width*3 > len(text) {
			return "", errMalformedURI
		}
		sequence := make([]byte, width)
		for offset := range width {
			octet, valid := percentOctet(text, index+offset*3)
			if !valid {
				return "", errMalformedURI
			}
			sequence[offset] = octet
		}
		if !utf8.Valid(sequence) {
			return "", errMalformedURI
		}
		decoded.Write(sequence)
		index += width * 3
	}
	return decoded.String(), nil
}

func asciiAlphaNumeric(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9'
}

func percentOctet(text string, index int) (byte, bool) {
	if index < 0 || index+2 >= len(text) || text[index] != '%' {
		return 0, false
	}
	high, highOK := hexadecimalValue(text[index+1])
	low, lowOK := hexadecimalValue(text[index+2])
	return high<<4 | low, highOK && lowOK
}

func hexadecimalValue(value byte) (byte, bool) {
	switch {
	case value >= '0' && value <= '9':
		return value - '0', true
	case value >= 'a' && value <= 'f':
		return value - 'a' + 10, true
	case value >= 'A' && value <= 'F':
		return value - 'A' + 10, true
	default:
		return 0, false
	}
}

func utf8SequenceWidth(first byte) int {
	switch {
	case first >= 0xc2 && first <= 0xdf:
		return 2
	case first >= 0xe0 && first <= 0xef:
		return 3
	case first >= 0xf0 && first <= 0xf4:
		return 4
	default:
		return 0
	}
}
