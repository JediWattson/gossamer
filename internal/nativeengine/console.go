package nativeengine

import (
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

const nativeConsoleMethod uint64 = 28_000

func (realm *Realm) newConsole(context *browserruntime.TaskContext) (memory.Ref, error) {
	console, err := context.NewHeapObject()
	if err != nil {
		return memory.Ref{}, err
	}
	if err := context.SetPrototype(console, memory.RefValue(realm.active.ObjectPrototype)); err != nil {
		return memory.Ref{}, err
	}
	for _, name := range []string{
		"assert", "clear", "count", "countReset", "debug", "dir", "error",
		"group", "groupCollapsed", "groupEnd", "info", "log", "table", "time",
		"timeEnd", "trace", "warn",
	} {
		method, err := realm.newNativeFunction(context, name, 0, nativeConsoleMethod)
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
// compatibility console deliberately retains neither arguments nor callbacks;
// embedders can add a diagnostic sink without changing its ownership behavior.
func (realm *Realm) consoleMethod(_ *browserruntime.TaskContext, _ memory.Value, _ []memory.Value) (memory.Value, error) {
	return memory.UndefinedValue(), nil
}
