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
	interfaces := make([]domInterfaceBinding, 0, 15)
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
	nodeInterface := interfaces[len(interfaces)-1]
	for _, constant := range []struct {
		name  string
		value uint8
	}{
		{"ELEMENT_NODE", 1}, {"ATTRIBUTE_NODE", 2}, {"TEXT_NODE", 3}, {"CDATA_SECTION_NODE", 4},
		{"ENTITY_REFERENCE_NODE", 5}, {"ENTITY_NODE", 6}, {"PROCESSING_INSTRUCTION_NODE", 7},
		{"COMMENT_NODE", 8}, {"DOCUMENT_NODE", 9}, {"DOCUMENT_TYPE_NODE", 10},
		{"DOCUMENT_FRAGMENT_NODE", 11}, {"NOTATION_NODE", 12},
	} {
		value := memory.NumberValue(float64(constant.value))
		if err := defineData(context, nodeInterface.constructor, constant.name, value, false, false, false); err != nil {
			return nil, err
		}
		if err := defineData(context, nodeInterface.prototype, constant.name, value, false, false, false); err != nil {
			return nil, err
		}
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
		{"HTMLDivElement", &bindings.htmlDivElementPrototype},
		{"HTMLTemplateElement", &bindings.templatePrototype},
		{"HTMLIFrameElement", &bindings.htmlIFrameElementPrototype},
		{"HTMLHeadElement", &bindings.htmlHeadElementPrototype},
		{"HTMLScriptElement", &bindings.htmlScriptElementPrototype},
		{"HTMLStyleElement", &bindings.htmlStyleElementPrototype},
		{"HTMLLinkElement", &bindings.htmlLinkElementPrototype},
		{"HTMLAnchorElement", &bindings.htmlAnchorElementPrototype},
		{"HTMLDialogElement", &bindings.htmlDialogElementPrototype},
		{"HTMLMediaElement", &bindings.htmlMediaElementPrototype},
		{"HTMLImageElement", &bindings.htmlImageElementPrototype},
		{"HTMLCanvasElement", &bindings.htmlCanvasElementPrototype},
	} {
		if err := add(definition.name, bindings.htmlElementPrototype, definition.destination); err != nil {
			return nil, err
		}
		if definition.name == "HTMLMediaElement" {
			constructor := interfaces[len(interfaces)-1].constructor
			prototype := interfaces[len(interfaces)-1].prototype
			for index, name := range []string{"HAVE_NOTHING", "HAVE_METADATA", "HAVE_CURRENT_DATA", "HAVE_FUTURE_DATA", "HAVE_ENOUGH_DATA"} {
				value := memory.NumberValue(float64(index))
				if err := defineData(context, constructor, name, value, false, false, false); err != nil {
					return nil, err
				}
				if err := defineData(context, prototype, name, value, false, false, false); err != nil {
					return nil, err
				}
			}
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
	if err := add("ShadowRoot", bindings.fragmentPrototype, &bindings.shadowRootPrototype); err != nil {
		return nil, err
	}
	if err := add("HTMLCollection", realm.active.ObjectPrototype, &bindings.htmlCollectionPrototype); err != nil {
		return nil, err
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
		if metadata.ShadowRoot {
			return bindings.shadowRootPrototype, nil
		}
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
		case "div":
			return bindings.htmlDivElementPrototype, nil
		case "template":
			return bindings.templatePrototype, nil
		case "iframe":
			return bindings.htmlIFrameElementPrototype, nil
		case "head":
			return bindings.htmlHeadElementPrototype, nil
		case "script":
			return bindings.htmlScriptElementPrototype, nil
		case "style":
			return bindings.htmlStyleElementPrototype, nil
		case "link":
			return bindings.htmlLinkElementPrototype, nil
		case "a":
			return bindings.htmlAnchorElementPrototype, nil
		case "dialog":
			return bindings.htmlDialogElementPrototype, nil
		case "audio", "video":
			return bindings.htmlMediaElementPrototype, nil
		case "img":
			return bindings.htmlImageElementPrototype, nil
		case "canvas":
			return bindings.htmlCanvasElementPrototype, nil
		default:
			return bindings.htmlElementPrototype, nil
		}
	default:
		return bindings.nodePrototype, nil
	}
}
