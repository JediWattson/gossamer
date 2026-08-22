package nativeengine

import (
	"fmt"
	"strconv"

	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

const nativeConsoleMethod uint64 = 28_000

var consoleMethodNames = []string{
	"assert", "clear", "count", "countReset", "debug", "dir", "error",
	"group", "groupCollapsed", "groupEnd", "info", "log", "table", "time",
	"timeEnd", "trace", "warn",
}

type ConsoleMessage struct {
	Method    string   `json:"method"`
	Arguments []string `json:"arguments,omitempty"`
}

func (realm *Realm) newConsole(context *browserruntime.TaskContext) (memory.Ref, error) {
	console, err := context.NewHeapObject()
	if err != nil {
		return memory.Ref{}, err
	}
	if err := context.SetPrototype(console, memory.RefValue(realm.active.ObjectPrototype)); err != nil {
		return memory.Ref{}, err
	}
	for index, name := range consoleMethodNames {
		method, err := realm.newNativeFunction(context, name, 0, nativeConsoleMethod+uint64(index))
		if err != nil {
			return memory.Ref{}, err
		}
		if err := defineData(context, console, name, memory.RefValue(method), true, false, true); err != nil {
			return memory.Ref{}, err
		}
	}
	return console, nil
}

// Console output is an observability side effect, not JavaScript state. The
// compatibility console deliberately retains neither arguments nor callbacks.
// A configured sink receives copied strings before the task checkpoint.
func (realm *Realm) consoleMethod(method string) browserruntime.NativeFunction {
	return func(context *browserruntime.TaskContext, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
		if realm.consoleSink != nil {
			copied := make([]string, len(arguments))
			for index, argument := range arguments {
				copied[index] = consoleValue(context, argument)
			}
			realm.consoleSink(ConsoleMessage{Method: method, Arguments: copied})
		}
		return memory.UndefinedValue(), nil
	}
}

func consoleValue(context *browserruntime.TaskContext, value memory.Value) string {
	switch value.Kind() {
	case memory.ValueUndefined:
		return "undefined"
	case memory.ValueNull:
		return "null"
	case memory.ValueBool:
		return strconv.FormatBool(value.Bool())
	case memory.ValueNumber:
		return strconv.FormatFloat(value.Number(), 'g', -1, 64)
	case memory.ValueReference:
		kind, err := context.HeapKind(value.Ref())
		if err != nil {
			return "[unavailable]"
		}
		switch kind {
		case memory.HeapString:
			text, err := context.DerefString(value.Ref())
			if err == nil {
				return text
			}
		case memory.HeapError:
			object, err := context.DerefError(value.Ref())
			if err == nil {
				message := ""
				if object.Message.IsRef() {
					message, _ = context.DerefString(object.Message.Ref())
				}
				if message != "" {
					return object.Kind.Name() + ": " + message
				}
				return object.Kind.Name()
			}
		}
		return fmt.Sprintf("[%v]", kind)
	default:
		return "[unknown]"
	}
}
