package nativeengine

import (
	"fmt"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

const (
	nativeElementFormOwner uint64 = 15_000 + iota
	nativeElementFormElements
	nativeElementSelectOptions
	nativeHTMLCollectionItem
	nativeHTMLCollectionNamedItem
	nativeHTMLFormElementReset
	nativeElementDefaultCheckedGet
	nativeElementDefaultCheckedSet
	nativeElementDefaultSelectedGet
	nativeElementDefaultSelectedSet
	nativeElementFormIndeterminateGet
	nativeElementFormIndeterminateSet
)

const formCollectionOwnerProperty = "\x00gossamer.form-collection.owner"

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

func (realm *Realm) elementHiddenGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapNode(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	_, found, err := realm.host.GetAttribute(handle, "hidden")
	return memory.BoolValue(found), err
}

func (realm *Realm) elementHiddenSet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapNode(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	if truthy(argument(arguments, 0)) {
		return memory.UndefinedValue(), realm.host.SetAttribute(handle, "hidden", "")
	}
	return memory.UndefinedValue(), realm.host.RemoveAttribute(handle, "hidden")
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

func (realm *Realm) elementFormIndeterminateGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	handle, host, err := realm.formOperands(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	indeterminate, err := host.FormIndeterminate(handle)
	return memory.BoolValue(indeterminate), err
}

func (realm *Realm) elementFormIndeterminateSet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, host, err := realm.formOperands(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), host.SetFormIndeterminate(handle, truthy(argument(arguments, 0)))
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

func (realm *Realm) elementFormOwner(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	handle, host, err := realm.formOperands(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	owner, found, err := host.FormOwner(handle)
	if err != nil || !found {
		return memory.NullValue(), err
	}
	return realm.wrappedNodeValue(context, owner)
}

func (realm *Realm) htmlFormReset(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	handle, host, err := realm.formOperands(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), host.ResetForm(handle)
}

func (realm *Realm) elementReflectedBoolean(attribute string, setter bool) browserruntime.NativeFunction {
	return func(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
		handle, err := realm.unwrapNode(context, this)
		if err != nil {
			return memory.Value{}, err
		}
		if !setter {
			_, found, err := realm.host.GetAttribute(handle, attribute)
			return memory.BoolValue(found), err
		}
		if truthy(argument(arguments, 0)) {
			return memory.UndefinedValue(), realm.host.SetAttribute(handle, attribute, "")
		}
		return memory.UndefinedValue(), realm.host.RemoveAttribute(handle, attribute)
	}
}

func (realm *Realm) elementFormCollection(kind dom.FormCollectionKind) browserruntime.NativeFunction {
	return func(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
		handle, host, err := realm.formOperands(context, this)
		if err != nil {
			return memory.Value{}, err
		}
		key, err := newString(context, formCollectionCacheKey(handle, kind))
		if err != nil {
			return memory.Value{}, err
		}
		var collection memory.Ref
		if cached, found, cacheErr := context.MapGet(realm.bindings.collectionCache, key); cacheErr != nil {
			return memory.Value{}, cacheErr
		} else if found && cached.IsRef() {
			collection = cached.Ref()
		} else {
			collection, err = context.NewArray(0)
			if err != nil {
				return memory.Value{}, err
			}
			if err := context.SetPrototype(collection, memory.RefValue(realm.bindings.htmlCollectionPrototype)); err != nil {
				return memory.Value{}, err
			}
			class := hostClassFormElements
			if kind == dom.SelectOptionCollection {
				class = hostClassSelectOptions
			}
			record, err := context.NewHostObject(memory.HostObject{Class: class, Scope: uint64(handle.Document), Identity: uint64(handle.Node)})
			if err != nil {
				return memory.Value{}, err
			}
			if err := defineData(context, collection, hostRecordProperty, memory.RefValue(record), false, false, false); err != nil {
				return memory.Value{}, err
			}
			if err := defineData(context, collection, formCollectionOwnerProperty, this, false, false, false); err != nil {
				return memory.Value{}, err
			}
			if err := context.MapSet(realm.bindings.collectionCache, key, memory.RefValue(collection)); err != nil {
				return memory.Value{}, err
			}
		}
		handles, err := host.FormControlNodes(handle, kind)
		if err != nil {
			return memory.Value{}, err
		}
		if err := realm.replaceNodeArrayContents(context, collection, handles); err != nil {
			return memory.Value{}, err
		}
		return memory.RefValue(collection), nil
	}
}

func (realm *Realm) htmlCollectionNamedItem(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	if !this.IsRef() {
		return memory.Value{}, fmt.Errorf("%w: invalid HTMLCollection receiver", browserruntime.ErrOperandType)
	}
	name, err := stringArgument(context, arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	value, found, err := realm.formCollectionNamedProperty(context, this.Ref(), name)
	if err != nil || found {
		return value, err
	}
	return memory.NullValue(), nil
}

func (realm *Realm) htmlCollectionItem(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	if !this.IsRef() {
		return memory.Value{}, fmt.Errorf("%w: invalid HTMLCollection receiver", browserruntime.ErrOperandType)
	}
	if _, found, err := realm.formCollectionRecord(context, this.Ref()); err != nil {
		return memory.Value{}, err
	} else if !found {
		return memory.Value{}, fmt.Errorf("%w: invalid HTMLCollection receiver", browserruntime.ErrOperandType)
	}
	index := 0
	var err error
	if len(arguments) != 0 {
		index, err = integerArgument(arguments, 0)
		if err != nil {
			return memory.Value{}, err
		}
	}
	if index < 0 || uint64(index) > uint64(^uint32(0)) {
		return memory.NullValue(), nil
	}
	value, found, err := context.ArrayElement(this.Ref(), uint32(index))
	if err != nil || found {
		return value, err
	}
	return memory.NullValue(), nil
}

func (realm *Realm) formCollectionNamedProperty(context *browserruntime.TaskContext, collection memory.Ref, name string) (memory.Value, bool, error) {
	if name == "" {
		return memory.Value{}, false, nil
	}
	record, found, err := realm.formCollectionRecord(context, collection)
	if err != nil || !found {
		return memory.Value{}, false, err
	}
	handle, kind, err := formCollectionIdentity(record)
	if err != nil {
		return memory.Value{}, false, err
	}
	host, err := realm.elementHost()
	if err != nil {
		return memory.Value{}, false, err
	}
	handles, err := host.FormControlNodes(handle, kind)
	if err != nil {
		return memory.Value{}, false, err
	}
	var named browser.NodeHandle
	for _, candidate := range handles {
		id, idFound, attrErr := realm.host.GetAttribute(candidate, "id")
		if attrErr != nil {
			return memory.Value{}, false, attrErr
		}
		if idFound && id == name {
			return realm.namedCollectionNodeValue(context, candidate)
		}
		if named == (browser.NodeHandle{}) {
			candidateName, nameFound, attrErr := realm.host.GetAttribute(candidate, "name")
			if attrErr != nil {
				return memory.Value{}, false, attrErr
			}
			if nameFound && candidateName == name {
				named = candidate
			}
		}
	}
	if named != (browser.NodeHandle{}) {
		return realm.namedCollectionNodeValue(context, named)
	}
	return memory.Value{}, false, nil
}

func (realm *Realm) namedCollectionNodeValue(context *browserruntime.TaskContext, handle browser.NodeHandle) (memory.Value, bool, error) {
	value, err := realm.wrappedNodeValue(context, handle)
	return value, err == nil, err
}

func (realm *Realm) formCollectionRecord(context *browserruntime.TaskContext, collection memory.Ref) (memory.HostObject, bool, error) {
	record, facade, err := realm.facadeRecord(context, collection)
	if err != nil || !facade {
		return memory.HostObject{}, false, err
	}
	if record.Class != hostClassFormElements && record.Class != hostClassSelectOptions {
		return memory.HostObject{}, false, nil
	}
	return record, true, nil
}

func formCollectionIdentity(record memory.HostObject) (browser.NodeHandle, dom.FormCollectionKind, error) {
	kind := dom.FormElementCollection
	if record.Class == hostClassSelectOptions {
		kind = dom.SelectOptionCollection
	} else if record.Class != hostClassFormElements {
		return browser.NodeHandle{}, 0, fmt.Errorf("nativeengine: invalid form collection class %d", record.Class)
	}
	return browser.NodeHandle{Document: browser.DocumentGeneration(record.Scope), Node: dom.NodeID(record.Identity)}, kind, nil
}

func formCollectionCacheKey(handle browser.NodeHandle, kind dom.FormCollectionKind) string {
	return fmt.Sprintf("form:%d:%s", kind, nodeCacheKey(handle))
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
