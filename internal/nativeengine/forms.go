package nativeengine

import (
	"fmt"

	"github.com/JediWattson/gossamer/internal/browser"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func (realm *Realm) elementFormValueGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	handle, host, err := realm.formOperands(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	value, err := host.FormValue(handle)
	if err != nil {
		return memory.Value{}, err
	}
	return newString(context, value)
}

func (realm *Realm) elementFormValueSet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, host, err := realm.formOperands(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	value, err := stringArgument(context, arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), host.SetFormValue(handle, value)
}

func (realm *Realm) elementFormCheckedGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	handle, host, err := realm.formOperands(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	checked, err := host.FormChecked(handle)
	return memory.BoolValue(checked), err
}

func (realm *Realm) elementFormCheckedSet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, host, err := realm.formOperands(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), host.SetFormChecked(handle, truthy(argument(arguments, 0)))
}

func (realm *Realm) elementFormSelectedGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	handle, host, err := realm.formOperands(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	selected, err := host.FormSelected(handle)
	return memory.BoolValue(selected), err
}

func (realm *Realm) elementFormSelectedSet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, host, err := realm.formOperands(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), host.SetFormSelected(handle, truthy(argument(arguments, 0)))
}

func (realm *Realm) elementFormSelectedIndexGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	handle, host, err := realm.formOperands(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	index, err := host.FormSelectedIndex(handle)
	return memory.NumberValue(float64(index)), err
}

func (realm *Realm) elementFormSelectedIndexSet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, host, err := realm.formOperands(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	index, err := integerArgument(arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), host.SetFormSelectedIndex(handle, index)
}

func (realm *Realm) elementSelectionStartGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	start, _, _, err := realm.formSelection(context, this)
	return memory.NumberValue(float64(start)), err
}

func (realm *Realm) elementSelectionEndGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	_, end, _, err := realm.formSelection(context, this)
	return memory.NumberValue(float64(end)), err
}

func (realm *Realm) elementSelectionDirectionGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	_, _, direction, err := realm.formSelection(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	return newString(context, direction)
}

func (realm *Realm) elementSelectionStartSet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	start, err := integerArgument(arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	_, end, direction, err := realm.formSelection(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	return realm.setFormSelection(context, this, start, end, direction)
}

func (realm *Realm) elementSelectionEndSet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	end, err := integerArgument(arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	start, _, direction, err := realm.formSelection(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	return realm.setFormSelection(context, this, start, end, direction)
}

func (realm *Realm) elementSelectionDirectionSet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	direction, err := stringArgument(context, arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	start, end, _, err := realm.formSelection(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	return realm.setFormSelection(context, this, start, end, direction)
}

func (realm *Realm) elementSetSelectionRange(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	start, err := integerArgument(arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	end, err := integerArgument(arguments, 1)
	if err != nil {
		return memory.Value{}, err
	}
	direction := "none"
	if len(arguments) > 2 {
		direction, err = stringArgument(context, arguments, 2)
		if err != nil {
			return memory.Value{}, err
		}
	}
	return realm.setFormSelection(context, this, start, end, direction)
}

func (realm *Realm) elementSelect(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	handle, host, err := realm.formOperands(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	value, err := host.FormValue(handle)
	if err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), host.SetFormSelection(handle, 0, len([]rune(value)), "none")
}

func (realm *Realm) formSelection(context *browserruntime.TaskContext, this memory.Value) (int, int, string, error) {
	handle, host, err := realm.formOperands(context, this)
	if err != nil {
		return 0, 0, "", err
	}
	return host.FormSelection(handle)
}

func (realm *Realm) setFormSelection(context *browserruntime.TaskContext, this memory.Value, start, end int, direction string) (memory.Value, error) {
	handle, host, err := realm.formOperands(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), host.SetFormSelection(handle, start, end, direction)
}

func (realm *Realm) elementFocus(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	handle, host, err := realm.formOperands(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), host.Focus(handle)
}

func (realm *Realm) elementBlur(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	handle, host, err := realm.formOperands(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), host.Blur(handle)
}

func (realm *Realm) documentActiveElement(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	if _, err := realm.unwrapNode(context, this); err != nil {
		return memory.Value{}, err
	}
	host, err := realm.elementHost()
	if err != nil {
		return memory.Value{}, err
	}
	handle, found, err := host.ActiveElement()
	if err != nil || !found {
		return memory.NullValue(), err
	}
	return realm.wrappedNodeValue(context, handle)
}

func (realm *Realm) documentScrollingElement(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	if _, err := realm.unwrapNode(context, this); err != nil {
		return memory.Value{}, err
	}
	return realm.relatedNodeValue(context, memory.RefValue(realm.bindings.document), browser.RelationDocumentElement)
}

func (realm *Realm) formOperands(context *browserruntime.TaskContext, this memory.Value) (browser.NodeHandle, browser.DOMElementHost, error) {
	handle, err := realm.unwrapNode(context, this)
	if err != nil {
		return browser.NodeHandle{}, nil, err
	}
	host, err := realm.elementHost()
	if err != nil {
		return browser.NodeHandle{}, nil, err
	}
	if host == nil {
		return browser.NodeHandle{}, nil, fmt.Errorf("nativeengine: form host is unavailable")
	}
	return handle, host, nil
}
