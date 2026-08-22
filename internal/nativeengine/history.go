package nativeengine

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/JediWattson/gossamer/internal/browser"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

const (
	nativeHistoryPushState uint64 = 20_100 + iota
	nativeHistoryReplaceState
	nativeHistoryGo
	nativeHistoryBack
	nativeHistoryForward
)

func (realm *Realm) newHistory(context *browserruntime.TaskContext) (memory.Ref, error) {
	history, err := context.NewHeapObject()
	if err != nil {
		return memory.Ref{}, err
	}
	methods := []struct {
		name  string
		arity uint32
		id    uint64
	}{
		{"pushState", 2, nativeHistoryPushState},
		{"replaceState", 2, nativeHistoryReplaceState},
		{"go", 1, nativeHistoryGo},
		{"back", 0, nativeHistoryBack},
		{"forward", 0, nativeHistoryForward},
	}
	for _, method := range methods {
		callable, methodErr := realm.newNativeFunction(context, method.name, method.arity, method.id)
		if methodErr != nil {
			return memory.Ref{}, methodErr
		}
		if methodErr := defineData(context, history, method.name, memory.RefValue(callable), true, false, true); methodErr != nil {
			return memory.Ref{}, methodErr
		}
	}
	if err := realm.refreshHistory(context, history); err != nil {
		return memory.Ref{}, err
	}
	return history, nil
}

func (realm *Realm) historyPushState(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	return realm.updateHistoryState(context, this, arguments, false)
}

func (realm *Realm) historyReplaceState(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	return realm.updateHistoryState(context, this, arguments, true)
}

func (realm *Realm) updateHistoryState(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value, replace bool) (memory.Value, error) {
	history, err := realm.historyReceiver(this)
	if err != nil {
		return memory.Value{}, err
	}
	stateJSON, err := historyStateJSON(context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	rawURL := ""
	if len(arguments) > 2 && arguments[2].Kind() != memory.ValueUndefined {
		rawURL, err = valueString(context, arguments[2])
		if err != nil {
			return memory.Value{}, err
		}
	}
	host, ok := realm.host.(browser.SessionHistoryHost)
	if !ok {
		return memory.Value{}, fmt.Errorf("nativeengine: browser host does not expose session history")
	}
	if _, err := host.UpdateHistoryState(stateJSON, rawURL, replace); err != nil {
		return memory.Value{}, err
	}
	if err := realm.refreshHistory(context, history); err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), nil
}

func (realm *Realm) historyGo(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	if _, err := realm.historyReceiver(this); err != nil {
		return memory.Value{}, err
	}
	delta := 0
	if len(arguments) != 0 && arguments[0].Kind() != memory.ValueUndefined {
		if arguments[0].Kind() != memory.ValueNumber || math.IsNaN(arguments[0].Number()) || math.IsInf(arguments[0].Number(), 0) {
			return memory.Value{}, fmt.Errorf("%w: history.go delta", browserruntime.ErrOperandType)
		}
		delta = int(math.Trunc(arguments[0].Number()))
	}
	host, ok := realm.host.(browser.SessionHistoryHost)
	if !ok {
		return memory.Value{}, fmt.Errorf("nativeengine: browser host does not expose session history")
	}
	if err := host.TraverseHistory(delta); err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), nil
}

func (realm *Realm) historyBack(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	return realm.historyGo(context, this, []memory.Value{memory.NumberValue(-1)})
}

func (realm *Realm) historyForward(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	return realm.historyGo(context, this, []memory.Value{memory.NumberValue(1)})
}

func (realm *Realm) historyReceiver(value memory.Value) (memory.Ref, error) {
	if !value.IsRef() || realm.bindings == nil || value.Ref() != realm.bindings.history {
		return memory.Ref{}, fmt.Errorf("%w: incompatible History receiver", browserruntime.ErrOperandType)
	}
	return value.Ref(), nil
}

func (realm *Realm) refreshHistory(context *browserruntime.TaskContext, history memory.Ref) error {
	host, ok := realm.host.(browser.SessionHistoryHost)
	if !ok {
		return fmt.Errorf("nativeengine: browser host does not expose session history")
	}
	snapshot, err := host.SessionHistorySnapshot()
	if err != nil {
		return err
	}
	stateJSON := snapshot.StateJSON
	if stateJSON == "" {
		stateJSON = "null"
	}
	var decoded any
	if err := json.Unmarshal([]byte(stateJSON), &decoded); err != nil {
		return fmt.Errorf("nativeengine: invalid session history state: %w", err)
	}
	state, err := jsonMemoryValue(context, decoded)
	if err != nil {
		return err
	}
	if err := defineData(context, history, "length", memory.NumberValue(float64(snapshot.Length)), false, true, true); err != nil {
		return err
	}
	return defineData(context, history, "state", state, false, true, true)
}

func historyStateJSON(context *browserruntime.TaskContext, value memory.Value) (string, error) {
	converted, err := historyStateValue(context, value, make(map[memory.Ref]bool))
	if err != nil {
		return "", err
	}
	if _, undefined := converted.(historyUndefined); undefined {
		converted = nil
	}
	encoded, err := json.Marshal(converted)
	if err != nil {
		return "", fmt.Errorf("%w: history state: %v", browserruntime.ErrOperandType, err)
	}
	return string(encoded), nil
}

type historyUndefined struct{}

func historyStateValue(context *browserruntime.TaskContext, value memory.Value, active map[memory.Ref]bool) (any, error) {
	switch value.Kind() {
	case memory.ValueUndefined:
		return historyUndefined{}, nil
	case memory.ValueNull:
		return nil, nil
	case memory.ValueBool:
		return value.Bool(), nil
	case memory.ValueNumber:
		if math.IsNaN(value.Number()) || math.IsInf(value.Number(), 0) {
			return nil, nil
		}
		return value.Number(), nil
	case memory.ValueReference:
	default:
		return nil, fmt.Errorf("%w: unsupported history state value", browserruntime.ErrOperandType)
	}
	ref := value.Ref()
	if active[ref] {
		return nil, fmt.Errorf("%w: cyclic history state", browserruntime.ErrOperandType)
	}
	kind, err := context.HeapKind(ref)
	if err != nil {
		return nil, err
	}
	switch kind {
	case memory.HeapString:
		return context.DerefString(ref)
	case memory.HeapArray:
		active[ref] = true
		defer delete(active, ref)
		array, err := context.DerefArray(ref)
		if err != nil {
			return nil, err
		}
		result := make([]any, array.Length)
		for _, element := range array.Elements {
			item, itemErr := historyStateValue(context, element.Value, active)
			if itemErr != nil {
				return nil, itemErr
			}
			if _, undefined := item.(historyUndefined); !undefined {
				result[element.Index] = item
			}
		}
		return result, nil
	case memory.HeapObject:
		active[ref] = true
		defer delete(active, ref)
		header, err := context.DerefObjectHeader(ref)
		if err != nil {
			return nil, err
		}
		result := make(map[string]any)
		for _, property := range header.Properties {
			if !property.Enumerable || property.Kind != memory.PropertyData {
				continue
			}
			name, nameErr := context.DerefString(property.Name)
			if nameErr != nil {
				continue
			}
			item, itemErr := historyStateValue(context, property.Value, active)
			if itemErr != nil {
				return nil, itemErr
			}
			if _, undefined := item.(historyUndefined); !undefined {
				result[name] = item
			}
		}
		return result, nil
	default:
		return nil, fmt.Errorf("%w: history state contains non-cloneable value", browserruntime.ErrOperandType)
	}
}
