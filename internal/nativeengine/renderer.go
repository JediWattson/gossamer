package nativeengine

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func (realm *Realm) elementGetAttributeNames(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapNode(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	host, err := realm.elementHost()
	if err != nil {
		return memory.Value{}, err
	}
	names, err := host.AttributeNames(handle)
	if err != nil {
		return memory.Value{}, err
	}
	array, err := context.NewArray(uint32(len(names)))
	if err != nil {
		return memory.Value{}, err
	}
	for index, name := range names {
		value, err := newString(context, name)
		if err != nil {
			return memory.Value{}, err
		}
		if err := context.SetArrayElement(array, uint32(index), value); err != nil {
			return memory.Value{}, err
		}
	}
	return memory.RefValue(array), nil
}

func (realm *Realm) elementClassNameGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapNode(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	value, _, err := realm.host.GetAttribute(handle, "class")
	if err != nil {
		return memory.Value{}, err
	}
	return newString(context, value)
}

func (realm *Realm) elementClassNameSet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapFacadeOrNode(context, this, hostClassClassList)
	if err != nil {
		return memory.Value{}, err
	}
	value, err := stringArgument(context, arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), realm.host.SetAttribute(handle, "class", value)
}

func (realm *Realm) elementClassList(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	return realm.elementFacade(context, this, hostClassClassList, realm.bindings.classListPrototype, "class-list")
}

func (realm *Realm) elementDataset(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	return realm.elementFacade(context, this, hostClassDataset, realm.bindings.datasetPrototype, "dataset")
}

func (realm *Realm) elementStyle(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	return realm.elementFacade(context, this, hostClassStyle, realm.bindings.stylePrototype, "style")
}

func (realm *Realm) elementFacade(context *browserruntime.TaskContext, this memory.Value, class memory.HostClass, prototype memory.Ref, prefix string) (memory.Value, error) {
	handle, err := realm.unwrapNode(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	key, err := newString(context, prefix+":"+nodeCacheKey(handle))
	if err != nil {
		return memory.Value{}, err
	}
	if cached, found, err := context.MapGet(realm.bindings.facadeCache, key); err != nil {
		return memory.Value{}, err
	} else if found && cached.IsRef() {
		return cached, nil
	}
	ref, err := realm.newHostWrapperLocked(context, memory.HostObject{
		Class: class, Scope: uint64(handle.Document), Identity: uint64(handle.Node),
	}, prototype)
	if err != nil {
		return memory.Value{}, err
	}
	if err := context.MapSet(realm.bindings.facadeCache, key, memory.RefValue(ref)); err != nil {
		return memory.Value{}, err
	}
	return memory.RefValue(ref), nil
}

func (realm *Realm) facadeRecord(context *browserruntime.TaskContext, object memory.Ref) (memory.HostObject, bool, error) {
	name, err := context.NewString(hostRecordProperty)
	if err != nil {
		return memory.HostObject{}, false, err
	}
	value, found, err := context.GetOwnProperty(object, name)
	if err != nil || !found || !value.IsRef() {
		return memory.HostObject{}, false, err
	}
	record, err := context.DerefHostObject(value.Ref())
	if err != nil {
		return memory.HostObject{}, false, err
	}
	switch record.Class {
	case hostClassClassList, hostClassDataset, hostClassStyle, hostClassComputedStyle, hostClassFormElements, hostClassSelectOptions:
		return record, true, nil
	default:
		return memory.HostObject{}, false, nil
	}
}

func (realm *Realm) facadePropertyGet(context *browserruntime.TaskContext, object memory.Ref, name string) (memory.Value, bool, bool, error) {
	if kind, err := context.HeapKind(object); err != nil {
		return memory.Value{}, false, false, err
	} else if kind == memory.HeapTypedArray {
		key, err := context.NewString(name)
		if err != nil {
			return memory.Value{}, false, true, err
		}
		value, found, err := context.GetOwnProperty(realm.bindings.uint8ArrayPrototype, key)
		return value, found, true, err
	}
	record, facade, err := realm.facadeRecord(context, object)
	if err != nil || !facade {
		return memory.Value{}, false, facade, err
	}
	handle := browser.NodeHandle{Document: browser.DocumentGeneration(record.Scope), Node: dom.NodeID(record.Identity)}
	switch record.Class {
	case hostClassFormElements, hostClassSelectOptions:
		value, found, err := realm.formCollectionNamedProperty(context, object, name)
		return value, found, true, err
	case hostClassDataset:
		value, found, err := realm.host.GetAttribute(handle, datasetAttribute(name))
		if err != nil || !found {
			return memory.Value{}, false, true, err
		}
		valueRef, err := newString(context, value)
		return valueRef, true, true, err
	case hostClassStyle:
		host, err := realm.elementHost()
		if err != nil {
			return memory.Value{}, false, true, err
		}
		if index, indexErr := strconv.ParseUint(name, 10, 32); indexErr == nil {
			names, namesErr := host.StylePropertyNames(handle)
			if namesErr != nil {
				return memory.Value{}, false, true, namesErr
			}
			if index >= uint64(len(names)) {
				return memory.Value{}, false, true, nil
			}
			valueRef, valueErr := newString(context, names[index])
			return valueRef, true, true, valueErr
		}
		value, _, _, err := host.StyleProperty(handle, cssPropertyName(name))
		if err != nil {
			return memory.Value{}, false, true, err
		}
		valueRef, err := newString(context, value)
		return valueRef, true, true, err
	case hostClassComputedStyle:
		host, ok := realm.host.(browser.DOMComputedStyleHost)
		if !ok {
			return memory.Value{}, false, true, fmt.Errorf("nativeengine: browser host does not expose computed style")
		}
		if index, indexErr := strconv.ParseUint(name, 10, 32); indexErr == nil {
			names, namesErr := host.ComputedStylePropertyNames(handle, "")
			if namesErr != nil {
				return memory.Value{}, false, true, namesErr
			}
			if index >= uint64(len(names)) {
				return memory.Value{}, false, true, nil
			}
			valueRef, valueErr := newString(context, names[index])
			return valueRef, true, true, valueErr
		}
		value, _, valueErr := host.ComputedStyleProperty(handle, "", cssPropertyName(name))
		if valueErr != nil {
			return memory.Value{}, false, true, valueErr
		}
		valueRef, valueErr := newString(context, value)
		return valueRef, true, true, valueErr
	default:
		return memory.Value{}, false, false, nil
	}
}

func (realm *Realm) facadePropertySet(context *browserruntime.TaskContext, object memory.Ref, name string, value memory.Value) (bool, error) {
	record, facade, err := realm.facadeRecord(context, object)
	if err != nil || !facade {
		return facade, err
	}
	handle := browser.NodeHandle{Document: browser.DocumentGeneration(record.Scope), Node: dom.NodeID(record.Identity)}
	if record.Class == hostClassFormElements || record.Class == hostClassSelectOptions {
		return true, memory.ErrReadOnlyProperty
	}
	text, err := valueString(context, value)
	if err != nil {
		return true, err
	}
	switch record.Class {
	case hostClassDataset:
		return true, realm.host.SetAttribute(handle, datasetAttribute(name), text)
	case hostClassStyle:
		host, err := realm.elementHost()
		if err != nil {
			return true, err
		}
		return true, host.SetStyleProperty(handle, cssPropertyName(name), text, "")
	case hostClassComputedStyle:
		return true, memory.ErrReadOnlyProperty
	default:
		return false, nil
	}
}

func (realm *Realm) facadePropertyDelete(context *browserruntime.TaskContext, object memory.Ref, name string) (bool, bool, error) {
	record, facade, err := realm.facadeRecord(context, object)
	if err != nil || !facade {
		return false, facade, err
	}
	handle := browser.NodeHandle{Document: browser.DocumentGeneration(record.Scope), Node: dom.NodeID(record.Identity)}
	switch record.Class {
	case hostClassFormElements, hostClassSelectOptions:
		return false, true, nil
	case hostClassDataset:
		return true, true, realm.host.RemoveAttribute(handle, datasetAttribute(name))
	case hostClassStyle:
		host, err := realm.elementHost()
		if err != nil {
			return false, true, err
		}
		_, err = host.RemoveStyleProperty(handle, cssPropertyName(name))
		return true, true, err
	case hostClassComputedStyle:
		return false, true, memory.ErrReadOnlyProperty
	default:
		return false, false, nil
	}
}

func (realm *Realm) unwrapFacadeOrNode(context *browserruntime.TaskContext, value memory.Value, facadeClass memory.HostClass) (browser.NodeHandle, error) {
	if handle, err := realm.unwrapNode(context, value); err == nil {
		return handle, nil
	}
	if !value.IsRef() {
		return browser.NodeHandle{}, fmt.Errorf("%w: facade receiver is not an object", browserruntime.ErrOperandType)
	}
	record, facade, err := realm.facadeRecord(context, value.Ref())
	if err != nil || !facade || record.Class != facadeClass {
		return browser.NodeHandle{}, fmt.Errorf("%w: invalid facade receiver", browserruntime.ErrOperandType)
	}
	return browser.NodeHandle{Document: browser.DocumentGeneration(record.Scope), Node: dom.NodeID(record.Identity)}, nil
}

func (realm *Realm) elementInnerHTMLGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapNode(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	host, err := realm.elementHost()
	if err != nil {
		return memory.Value{}, err
	}
	source, err := host.InnerHTML(handle)
	if err != nil {
		return memory.Value{}, err
	}
	return newString(context, source)
}

func (realm *Realm) elementInnerHTMLSet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapNode(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	source, err := stringArgument(context, arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	host, err := realm.elementHost()
	if err != nil {
		return memory.Value{}, err
	}
	if err := host.SetInnerHTML(handle, source); err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), realm.refreshCollections(context, handle)
}

func (realm *Realm) elementInsertAdjacentHTML(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapNode(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	position, err := stringArgument(context, arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	source, err := stringArgument(context, arguments, 1)
	if err != nil {
		return memory.Value{}, err
	}
	host, err := realm.elementHost()
	if err != nil {
		return memory.Value{}, err
	}
	if err := host.InsertAdjacentHTML(handle, position, source); err != nil {
		return memory.Value{}, err
	}
	parent, _, _ := host.RelatedNode(handle, browser.RelationParentNode)
	return memory.UndefinedValue(), realm.refreshCollections(context, handle, parent)
}

func (realm *Realm) classTokens(handle browser.NodeHandle) ([]string, error) {
	value, _, err := realm.host.GetAttribute(handle, "class")
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	tokens := make([]string, 0)
	for _, token := range strings.Fields(value) {
		if _, exists := seen[token]; !exists {
			seen[token] = struct{}{}
			tokens = append(tokens, token)
		}
	}
	return tokens, nil
}

func (realm *Realm) setClassTokens(handle browser.NodeHandle, tokens []string) error {
	if len(tokens) == 0 {
		return realm.host.RemoveAttribute(handle, "class")
	}
	return realm.host.SetAttribute(handle, "class", strings.Join(tokens, " "))
}

func (realm *Realm) classListValue(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapFacadeOrNode(context, this, hostClassClassList)
	if err != nil {
		return memory.Value{}, err
	}
	tokens, err := realm.classTokens(handle)
	if err != nil {
		return memory.Value{}, err
	}
	return newString(context, strings.Join(tokens, " "))
}

func (realm *Realm) classListLength(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapFacadeOrNode(context, this, hostClassClassList)
	if err != nil {
		return memory.Value{}, err
	}
	tokens, err := realm.classTokens(handle)
	return memory.NumberValue(float64(len(tokens))), err
}

func (realm *Realm) classListAdd(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapFacadeOrNode(context, this, hostClassClassList)
	if err != nil {
		return memory.Value{}, err
	}
	tokens, err := realm.classTokens(handle)
	if err != nil {
		return memory.Value{}, err
	}
	seen := make(map[string]struct{}, len(tokens))
	for _, token := range tokens {
		seen[token] = struct{}{}
	}
	for index := range arguments {
		token, err := classTokenArgument(context, arguments, index)
		if err != nil {
			return memory.Value{}, err
		}
		if _, exists := seen[token]; !exists {
			seen[token] = struct{}{}
			tokens = append(tokens, token)
		}
	}
	return memory.UndefinedValue(), realm.setClassTokens(handle, tokens)
}

func (realm *Realm) classListRemove(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapFacadeOrNode(context, this, hostClassClassList)
	if err != nil {
		return memory.Value{}, err
	}
	remove := make(map[string]struct{}, len(arguments))
	for index := range arguments {
		token, err := classTokenArgument(context, arguments, index)
		if err != nil {
			return memory.Value{}, err
		}
		remove[token] = struct{}{}
	}
	tokens, err := realm.classTokens(handle)
	if err != nil {
		return memory.Value{}, err
	}
	kept := tokens[:0]
	for _, token := range tokens {
		if _, exists := remove[token]; !exists {
			kept = append(kept, token)
		}
	}
	return memory.UndefinedValue(), realm.setClassTokens(handle, kept)
}

func (realm *Realm) classListContains(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapFacadeOrNode(context, this, hostClassClassList)
	if err != nil {
		return memory.Value{}, err
	}
	token, err := classTokenArgument(context, arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	tokens, err := realm.classTokens(handle)
	if err != nil {
		return memory.Value{}, err
	}
	for _, current := range tokens {
		if current == token {
			return memory.BoolValue(true), nil
		}
	}
	return memory.BoolValue(false), nil
}

func (realm *Realm) classListToggle(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapFacadeOrNode(context, this, hostClassClassList)
	if err != nil {
		return memory.Value{}, err
	}
	token, err := classTokenArgument(context, arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	tokens, err := realm.classTokens(handle)
	if err != nil {
		return memory.Value{}, err
	}
	found := -1
	for index, current := range tokens {
		if current == token {
			found = index
			break
		}
	}
	want := found < 0
	if len(arguments) > 1 {
		want = truthy(arguments[1])
	}
	if want && found < 0 {
		tokens = append(tokens, token)
	} else if !want && found >= 0 {
		tokens = append(tokens[:found], tokens[found+1:]...)
	}
	if err := realm.setClassTokens(handle, tokens); err != nil {
		return memory.Value{}, err
	}
	return memory.BoolValue(want), nil
}

func (realm *Realm) classListItem(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapFacadeOrNode(context, this, hostClassClassList)
	if err != nil {
		return memory.Value{}, err
	}
	index, err := integerArgument(arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	tokens, err := realm.classTokens(handle)
	if err != nil {
		return memory.Value{}, err
	}
	if index < 0 || index >= len(tokens) {
		return memory.NullValue(), nil
	}
	return newString(context, tokens[index])
}

func (realm *Realm) classListToString(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	return realm.classListValue(context, this, arguments)
}

func classTokenArgument(context *browserruntime.TaskContext, arguments []memory.Value, index int) (string, error) {
	token, err := stringArgument(context, arguments, index)
	if err != nil {
		return "", err
	}
	if token == "" || strings.IndexFunc(token, unicode.IsSpace) >= 0 {
		return "", fmt.Errorf("%w: class token must be non-empty and contain no whitespace", browserruntime.ErrOperandType)
	}
	return token, nil
}

func (realm *Realm) styleCSSTextGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapFacadeOrNode(context, this, hostClassStyle)
	if err != nil {
		return memory.Value{}, err
	}
	host, err := realm.elementHost()
	if err != nil {
		return memory.Value{}, err
	}
	value, err := host.StyleCSSText(handle)
	if err != nil {
		return memory.Value{}, err
	}
	return newString(context, value)
}

func (realm *Realm) styleCSSTextSet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapFacadeOrNode(context, this, hostClassStyle)
	if err != nil {
		return memory.Value{}, err
	}
	value, err := stringArgument(context, arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	host, err := realm.elementHost()
	if err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), host.SetStyleCSSText(handle, value)
}

func (realm *Realm) styleLength(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapFacadeOrNode(context, this, hostClassStyle)
	if err != nil {
		return memory.Value{}, err
	}
	host, err := realm.elementHost()
	if err != nil {
		return memory.Value{}, err
	}
	names, err := host.StylePropertyNames(handle)
	return memory.NumberValue(float64(len(names))), err
}

func (realm *Realm) styleGetPropertyValue(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapFacadeOrNode(context, this, hostClassStyle)
	if err != nil {
		return memory.Value{}, err
	}
	name, err := stringArgument(context, arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	host, err := realm.elementHost()
	if err != nil {
		return memory.Value{}, err
	}
	value, _, _, err := host.StyleProperty(handle, name)
	if err != nil {
		return memory.Value{}, err
	}
	return newString(context, value)
}

func (realm *Realm) styleGetPropertyPriority(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapFacadeOrNode(context, this, hostClassStyle)
	if err != nil {
		return memory.Value{}, err
	}
	name, err := stringArgument(context, arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	host, err := realm.elementHost()
	if err != nil {
		return memory.Value{}, err
	}
	_, priority, _, err := host.StyleProperty(handle, name)
	if err != nil {
		return memory.Value{}, err
	}
	return newString(context, priority)
}

func (realm *Realm) styleSetProperty(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapFacadeOrNode(context, this, hostClassStyle)
	if err != nil {
		return memory.Value{}, err
	}
	name, err := stringArgument(context, arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	value, err := stringArgument(context, arguments, 1)
	if err != nil {
		return memory.Value{}, err
	}
	priority := ""
	if len(arguments) > 2 {
		priority, err = stringArgument(context, arguments, 2)
		if err != nil {
			return memory.Value{}, err
		}
	}
	host, err := realm.elementHost()
	if err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), host.SetStyleProperty(handle, name, value, priority)
}

func (realm *Realm) styleRemoveProperty(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapFacadeOrNode(context, this, hostClassStyle)
	if err != nil {
		return memory.Value{}, err
	}
	name, err := stringArgument(context, arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	host, err := realm.elementHost()
	if err != nil {
		return memory.Value{}, err
	}
	value, err := host.RemoveStyleProperty(handle, name)
	if err != nil {
		return memory.Value{}, err
	}
	return newString(context, value)
}

func (realm *Realm) styleItem(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapFacadeOrNode(context, this, hostClassStyle)
	if err != nil {
		return memory.Value{}, err
	}
	index, err := integerArgument(arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	host, err := realm.elementHost()
	if err != nil {
		return memory.Value{}, err
	}
	names, err := host.StylePropertyNames(handle)
	if err != nil {
		return memory.Value{}, err
	}
	if index < 0 || index >= len(names) {
		return newString(context, "")
	}
	return newString(context, names[index])
}

func (realm *Realm) elementHost() (browser.DOMElementHost, error) {
	host, ok := realm.host.(browser.DOMElementHost)
	if !ok {
		return nil, fmt.Errorf("nativeengine: browser host does not expose renderer DOM APIs")
	}
	return host, nil
}

func (realm *Realm) liveNodeArray(context *browserruntime.TaskContext, handle browser.NodeHandle, elementsOnly bool) (memory.Value, error) {
	host, err := realm.elementHost()
	if err != nil {
		return memory.Value{}, err
	}
	handles, err := host.ChildNodes(handle, elementsOnly)
	if err != nil {
		return memory.Value{}, err
	}
	key, err := newString(context, collectionCacheKey(handle, elementsOnly))
	if err != nil {
		return memory.Value{}, err
	}
	var array memory.Ref
	if cached, found, err := context.MapGet(realm.bindings.collectionCache, key); err != nil {
		return memory.Value{}, err
	} else if found && cached.IsRef() {
		array = cached.Ref()
	} else {
		array, err = context.NewArray(0)
		if err != nil {
			return memory.Value{}, err
		}
		if err := context.MapSet(realm.bindings.collectionCache, key, memory.RefValue(array)); err != nil {
			return memory.Value{}, err
		}
	}
	if err := realm.replaceNodeArrayContents(context, array, handles); err != nil {
		return memory.Value{}, err
	}
	return memory.RefValue(array), nil
}

func (realm *Realm) replaceNodeArrayContents(context *browserruntime.TaskContext, array memory.Ref, handles []browser.NodeHandle) error {
	if err := context.SetArrayLength(array, uint32(len(handles))); err != nil {
		return err
	}
	for index, handle := range handles {
		value, err := realm.wrappedNodeValue(context, handle)
		if err != nil {
			return err
		}
		if err := context.SetArrayElement(array, uint32(index), value); err != nil {
			return err
		}
	}
	return nil
}

func (realm *Realm) refreshCollections(context *browserruntime.TaskContext, handles ...browser.NodeHandle) error {
	host, err := realm.elementHost()
	if err != nil {
		return err
	}
	seen := make(map[browser.NodeHandle]struct{}, len(handles))
	for _, handle := range handles {
		if handle == (browser.NodeHandle{}) {
			continue
		}
		if _, duplicate := seen[handle]; duplicate {
			continue
		}
		seen[handle] = struct{}{}
		for _, elementsOnly := range []bool{false, true} {
			key, err := newString(context, collectionCacheKey(handle, elementsOnly))
			if err != nil {
				return err
			}
			cached, found, err := context.MapGet(realm.bindings.collectionCache, key)
			if err != nil {
				return err
			}
			if !found || !cached.IsRef() {
				continue
			}
			children, err := host.ChildNodes(handle, elementsOnly)
			if err != nil {
				return err
			}
			if err := realm.replaceNodeArrayContents(context, cached.Ref(), children); err != nil {
				return err
			}
		}
	}
	return realm.refreshFormCollections(context)
}

func (realm *Realm) refreshFormCollections(context *browserruntime.TaskContext) error {
	if realm.bindings == nil || realm.bindings.collectionCache == (memory.Ref{}) {
		return nil
	}
	cache, err := context.DerefMap(realm.bindings.collectionCache)
	if err != nil {
		return err
	}
	host, err := realm.elementHost()
	if err != nil {
		return err
	}
	for _, entry := range cache.Entries {
		if !entry.Value.IsRef() {
			continue
		}
		record, found, err := realm.formCollectionRecord(context, entry.Value.Ref())
		if err != nil {
			return err
		}
		if !found {
			continue
		}
		handle, kind, err := formCollectionIdentity(record)
		if err != nil {
			return err
		}
		handles, err := host.FormControlNodes(handle, kind)
		if err != nil {
			return err
		}
		if err := realm.replaceNodeArrayContents(context, entry.Value.Ref(), handles); err != nil {
			return err
		}
	}
	return nil
}

func collectionCacheKey(handle browser.NodeHandle, elementsOnly bool) string {
	kind := "nodes"
	if elementsOnly {
		kind = "elements"
	}
	return kind + ":" + nodeCacheKey(handle)
}

func datasetAttribute(name string) string {
	var builder strings.Builder
	builder.WriteString("data-")
	for _, character := range name {
		if unicode.IsUpper(character) {
			builder.WriteByte('-')
			builder.WriteRune(unicode.ToLower(character))
		} else {
			builder.WriteRune(character)
		}
	}
	return builder.String()
}

func cssPropertyName(name string) string {
	if strings.HasPrefix(name, "--") || strings.ContainsRune(name, '-') {
		return strings.ToLower(name)
	}
	var builder strings.Builder
	for _, character := range name {
		if unicode.IsUpper(character) {
			builder.WriteByte('-')
			builder.WriteRune(unicode.ToLower(character))
		} else {
			builder.WriteRune(character)
		}
	}
	return builder.String()
}
