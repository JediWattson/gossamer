package nativeengine

import (
	"fmt"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

type domInterfaceBinding struct {
	name        string
	constructor memory.Ref
	prototype   memory.Ref
}

func (realm *Realm) newDOMInterfaces(context *browserruntime.TaskContext, bindings *browserBindings) ([]domInterfaceBinding, error) {
	interfaces := make([]domInterfaceBinding, 0, 14)
	add := func(name string, parent memory.Ref, destination *memory.Ref) error {
		constructor, err := realm.newDOMInterfaceConstructor(context, name, parent)
		if err != nil {
			return err
		}
		prototype, err := constructorPrototype(context, constructor, name)
		if err != nil {
			return err
		}
		*destination = prototype
		interfaces = append(interfaces, domInterfaceBinding{name: name, constructor: constructor, prototype: prototype})
		return nil
	}

	if err := add("Node", realm.active.ObjectPrototype, &bindings.nodePrototype); err != nil {
		return nil, err
	}
	if err := add("Element", bindings.nodePrototype, &bindings.elementPrototype); err != nil {
		return nil, err
	}
	if err := add("HTMLElement", bindings.elementPrototype, &bindings.htmlElementPrototype); err != nil {
		return nil, err
	}
	for _, definition := range []struct {
		name        string
		destination *memory.Ref
	}{
		{"HTMLFormElement", &bindings.htmlFormElementPrototype},
		{"HTMLInputElement", &bindings.htmlInputElementPrototype},
		{"HTMLTextAreaElement", &bindings.htmlTextAreaElementPrototype},
		{"HTMLSelectElement", &bindings.htmlSelectElementPrototype},
		{"HTMLOptionElement", &bindings.htmlOptionElementPrototype},
		{"HTMLButtonElement", &bindings.htmlButtonElementPrototype},
		{"HTMLTemplateElement", &bindings.templatePrototype},
		{"HTMLIFrameElement", &bindings.htmlIFrameElementPrototype},
	} {
		if err := add(definition.name, bindings.htmlElementPrototype, definition.destination); err != nil {
			return nil, err
		}
	}
	for _, definition := range []struct {
		name        string
		destination *memory.Ref
	}{
		{"Text", &bindings.textPrototype},
		{"Document", &bindings.documentPrototype},
		{"DocumentFragment", &bindings.fragmentPrototype},
	} {
		if err := add(definition.name, bindings.nodePrototype, definition.destination); err != nil {
			return nil, err
		}
	}
	return interfaces, nil
}

func (bindings *browserBindings) prototypeForNode(metadata browser.NodeMetadata) (memory.Ref, error) {
	if bindings == nil {
		return memory.Ref{}, fmt.Errorf("nativeengine: browser bindings are unavailable")
	}
	switch metadata.Type {
	case browser.DOMDocumentNode:
		return bindings.documentPrototype, nil
	case browser.DOMTextNode:
		return bindings.textPrototype, nil
	case browser.DOMDocumentFragmentNode:
		return bindings.fragmentPrototype, nil
	case browser.DOMElementNode:
		if metadata.NamespaceURI != dom.HTMLNamespace {
			return bindings.elementPrototype, nil
		}
		switch metadata.LocalName {
		case "form":
			return bindings.htmlFormElementPrototype, nil
		case "input":
			return bindings.htmlInputElementPrototype, nil
		case "textarea":
			return bindings.htmlTextAreaElementPrototype, nil
		case "select":
			return bindings.htmlSelectElementPrototype, nil
		case "option":
			return bindings.htmlOptionElementPrototype, nil
		case "button":
			return bindings.htmlButtonElementPrototype, nil
		case "template":
			return bindings.templatePrototype, nil
		case "iframe":
			return bindings.htmlIFrameElementPrototype, nil
		default:
			return bindings.htmlElementPrototype, nil
		}
	default:
		return bindings.nodePrototype, nil
	}
}
