package nativeengine

import (
	"fmt"

	"github.com/JediWattson/gossamer/internal/js/compiler"
	"github.com/JediWattson/gossamer/internal/js/program"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

const (
	maximumEvalSourceBytes = 1 << 20
	maximumEvalDepth       = 32
)

// globalEval implements indirect eval against the Realm's persistent global
// environment. Direct eval's caller-local lexical semantics remain an explicit
// compatibility boundary; the compiler does not silently pretend to provide
// them.
func (realm *Realm) globalEval(context *browserruntime.TaskContext, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	value := argument(arguments, 0)
	if !value.IsRef() {
		return value, nil
	}
	kind, err := context.HeapKind(value.Ref())
	if err != nil {
		return memory.Value{}, err
	}
	if kind != memory.HeapString {
		return value, nil
	}
	source, err := context.DerefString(value.Ref())
	if err != nil {
		return memory.Value{}, err
	}
	if len(source) > maximumEvalSourceBytes {
		return memory.Value{}, fmt.Errorf("nativeengine: eval source exceeds %d bytes", maximumEvalSourceBytes)
	}
	if realm.evalDepth >= maximumEvalDepth {
		return memory.Value{}, fmt.Errorf("%w: eval nesting exceeds %d", browserruntime.ErrCallDepth, maximumEvalDepth)
	}
	realm.evalDepth++
	defer func() { realm.evalDepth-- }()
	realm.evaluations++
	realm.sourceBytes += uint64(len(source))

	image, err := compiler.CompileWithOptions(source, compiler.Options{AllowUnresolvedGlobals: true})
	if err != nil {
		return memory.Value{}, fmt.Errorf("nativeengine: compile eval source: %w", err)
	}
	loaded, err := program.Load(context, image, memory.RefValue(realm.active.Global))
	if err != nil {
		return memory.Value{}, fmt.Errorf("nativeengine: load eval source: %w", err)
	}
	return realm.interpreter.CallWithoutCheckpoint(context, loaded.Entry, memory.RefValue(realm.bindings.window))
}
