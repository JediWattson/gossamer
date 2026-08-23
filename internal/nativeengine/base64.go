package nativeengine

import (
	"encoding/base64"
	"fmt"
	"strings"

	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func (realm *Realm) globalAtob(context *browserruntime.TaskContext, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	encoded, err := stringArgument(context, arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	encoded = strings.Map(func(character rune) rune {
		switch character {
		case '\t', '\n', '\f', '\r', ' ':
			return -1
		default:
			return character
		}
	}, encoded)
	if len(encoded)%4 == 1 {
		return memory.Value{}, realm.invalidCharacterError(context, "The string to be decoded is not correctly encoded.")
	}
	if remainder := len(encoded) % 4; remainder != 0 {
		encoded += strings.Repeat("=", 4-remainder)
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return memory.Value{}, realm.invalidCharacterError(context, "The string to be decoded is not correctly encoded.")
	}
	characters := make([]rune, len(decoded))
	for index, value := range decoded {
		characters[index] = rune(value)
	}
	return newString(context, string(characters))
}

func (realm *Realm) globalBtoa(context *browserruntime.TaskContext, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	plain, err := stringArgument(context, arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	bytes := make([]byte, 0, len(plain))
	for _, character := range plain {
		if character > 0xff {
			return memory.Value{}, realm.invalidCharacterError(context, "The string to be encoded contains characters outside of the Latin1 range.")
		}
		bytes = append(bytes, byte(character))
	}
	return newString(context, base64.StdEncoding.EncodeToString(bytes))
}

func (realm *Realm) invalidCharacterError(context *browserruntime.TaskContext, message string) error {
	if realm == nil || realm.bindings == nil {
		return fmt.Errorf("nativeengine: InvalidCharacterError")
	}
	value, err := realm.newDOMExceptionValue(context, "InvalidCharacterError", message)
	if err != nil {
		return err
	}
	return browserruntime.Throw(value)
}
