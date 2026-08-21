package nativeengine

import (
	"fmt"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

const (
	nativeDOMExceptionConstructor uint64 = 14_000 + iota
	nativeDOMExceptionToString
	nativeNodeAppend
	nativeNodePrepend
	nativeNodeBefore
	nativeNodeAfter
	nativeNodeReplaceWith
	nativeNodeReplaceChildren
)

const (
	bindingDOMExceptionPrototype   = "\x00gossamer.dom-exception.prototype"
	bindingDOMExceptionConstructor = "\x00gossamer.dom-exception.constructor"
)

var domExceptionCodes = map[string]uint32{
	"IndexSizeError":        1,
	"HierarchyRequestError": 3,
	"InvalidCharacterError": 5,
	"NotFoundError":         8,
	"NotSupportedError":     9,
	"InvalidStateError":     11,
	"SyntaxError":           12,
	"NamespaceError":        14,
	"InvalidNodeTypeError":  24,
}

func (realm *Realm) newDOMExceptionConstructor(context *browserruntime.TaskContext) (memory.Ref, memory.Ref, error) {
	name, err := newString(context, "DOMException")
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	constructor, err := context.NewNativeConstructor(name, memory.RefValue(realm.active.Global), 0, nativeDOMExceptionConstructor)
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	prototype, err := constructorPrototype(context, constructor, "DOMException")
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	if err := context.SetPrototype(prototype, memory.RefValue(realm.active.ObjectPrototype)); err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	toString, err := realm.newNativeFunction(context, "toString", 0, nativeDOMExceptionToString)
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	if err := defineData(context, prototype, "toString", memory.RefValue(toString), true, false, true); err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	tag, err := newString(context, "DOMException")
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	if err := context.DefineProperty(prototype, realm.active.SymbolToStringTag, memory.DataProperty(tag, false, false, true)); err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	for constant, code := range map[string]uint32{
		"INDEX_SIZE_ERR": 1, "HIERARCHY_REQUEST_ERR": 3, "INVALID_CHARACTER_ERR": 5,
		"NOT_FOUND_ERR": 8, "NOT_SUPPORTED_ERR": 9, "INVALID_STATE_ERR": 11,
		"SYNTAX_ERR": 12, "NAMESPACE_ERR": 14, "INVALID_NODE_TYPE_ERR": 24,
	} {
		value := memory.NumberValue(float64(code))
		if err := defineData(context, constructor, constant, value, false, false, false); err != nil {
			return memory.Ref{}, memory.Ref{}, err
		}
		if err := defineData(context, prototype, constant, value, false, false, false); err != nil {
			return memory.Ref{}, memory.Ref{}, err
		}
	}
	return constructor, prototype, nil
}

func (realm *Realm) domExceptionConstructor(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	if !this.IsRef() {
		return memory.Value{}, fmt.Errorf("%w: DOMException constructor requires new", browserruntime.ErrOperandType)
	}
	message := ""
	name := "Error"
	var err error
	if len(arguments) > 0 && arguments[0].Kind() != memory.ValueUndefined {
		message, err = valueString(context, arguments[0])
		if err != nil {
			return memory.Value{}, err
		}
	}
	if len(arguments) > 1 && arguments[1].Kind() != memory.ValueUndefined {
		name, err = valueString(context, arguments[1])
		if err != nil {
			return memory.Value{}, err
		}
	}
	return memory.UndefinedValue(), realm.initializeDOMException(context, this.Ref(), name, message)
}

func (realm *Realm) initializeDOMException(context *browserruntime.TaskContext, object memory.Ref, name, message string) error {
	nameValue, err := newString(context, name)
	if err != nil {
		return err
	}
	messageValue, err := newString(context, message)
	if err != nil {
		return err
	}
	code := domExceptionCodes[name]
	for _, property := range []struct {
		name  string
		value memory.Value
	}{
		{"name", nameValue},
		{"message", messageValue},
		{"code", memory.NumberValue(float64(code))},
	} {
		if err := defineData(context, object, property.name, property.value, false, false, true); err != nil {
			return err
		}
	}
	return nil
}

func (realm *Realm) newDOMExceptionValue(context *browserruntime.TaskContext, name, message string) (memory.Value, error) {
	object, err := context.NewHeapObject()
	if err != nil {
		return memory.Value{}, err
	}
	if err := context.SetPrototype(object, memory.RefValue(realm.bindings.domExceptionPrototype)); err != nil {
		return memory.Value{}, err
	}
	if err := realm.initializeDOMException(context, object, name, message); err != nil {
		return memory.Value{}, err
	}
	return memory.RefValue(object), nil
}

func (realm *Realm) throwDOMException(context *browserruntime.TaskContext, err error) error {
	name, ok := dom.ErrorExceptionName(err)
	if !ok {
		return err
	}
	value, createErr := realm.newDOMExceptionValue(context, string(name), err.Error())
	if createErr != nil {
		return createErr
	}
	return browserruntime.Throw(value)
}

func (realm *Realm) domExceptionToString(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	if !this.IsRef() {
		return memory.Value{}, fmt.Errorf("%w: invalid DOMException receiver", browserruntime.ErrOperandType)
	}
	name, err := ownStringProperty(context, this.Ref(), "name")
	if err != nil {
		return memory.Value{}, err
	}
	message, err := ownStringProperty(context, this.Ref(), "message")
	if err != nil {
		return memory.Value{}, err
	}
	text := name
	if text == "" {
		text = message
	} else if message != "" {
		text += ": " + message
	}
	return newString(context, text)
}

func ownStringProperty(context *browserruntime.TaskContext, object memory.Ref, name string) (string, error) {
	value, found, err := hiddenProperty(context, object, name)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("%w: missing DOMException.%s", browserruntime.ErrOperandType, name)
	}
	return valueString(context, value)
}

func (realm *Realm) nodeConvenienceMutation(operation dom.MutationOperation) browserruntime.NativeFunction {
	return func(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
		receiver, err := realm.unwrapNode(context, this)
		if err != nil {
			return memory.Value{}, err
		}
		host, err := realm.elementHost()
		if err != nil {
			return memory.Value{}, err
		}
		nodes := make([]browser.NodeHandle, 0, len(arguments))
		refresh := []browser.NodeHandle{receiver, realm.parentHandle(receiver)}
		for _, value := range arguments {
			handle, unwrapErr := realm.unwrapNode(context, value)
			if unwrapErr != nil {
				text, stringErr := valueString(context, value)
				if stringErr != nil {
					return memory.Value{}, stringErr
				}
				handle, err = realm.host.CreateTextNode(text)
				if err != nil {
					return memory.Value{}, realm.throwDOMException(context, err)
				}
			}
			nodes = append(nodes, handle)
			refresh = append(refresh, handle, realm.parentHandle(handle))
		}
		if err := host.MutateNodes(receiver, operation, nodes); err != nil {
			return memory.Value{}, realm.throwDOMException(context, err)
		}
		refresh = append(refresh, realm.parentHandle(receiver))
		for _, handle := range nodes {
			refresh = append(refresh, handle, realm.parentHandle(handle))
		}
		if err := realm.refreshCollections(context, refresh...); err != nil {
			return memory.Value{}, err
		}
		return memory.UndefinedValue(), nil
	}
}
