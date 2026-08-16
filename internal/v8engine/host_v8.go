//go:build v8 && cgo && darwin && arm64

package v8engine

/*
#include <stdlib.h>
#include "host_callbacks.h"
*/
import "C"

import (
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
)

var nextHostExecution atomic.Uint64

var hostExecutions = struct {
	sync.RWMutex
	hosts map[uint64]browser.Host
}{hosts: make(map[uint64]browser.Host)}

func registerHostExecution(host browser.Host) uint64 {
	if host == nil {
		return 0
	}
	id := nextHostExecution.Add(1)
	hostExecutions.Lock()
	hostExecutions.hosts[id] = host
	hostExecutions.Unlock()
	return id
}

func unregisterHostExecution(id uint64) {
	if id == 0 {
		return
	}
	hostExecutions.Lock()
	delete(hostExecutions.hosts, id)
	hostExecutions.Unlock()
}

func lookupHostExecution(id uint64) (browser.Host, error) {
	if id == 0 {
		return nil, fmt.Errorf("V8 host bindings are unavailable outside a Page task")
	}
	hostExecutions.RLock()
	host := hostExecutions.hosts[id]
	hostExecutions.RUnlock()
	if host == nil {
		return nil, fmt.Errorf("V8 host execution %d is no longer active", id)
	}
	return host, nil
}

func runHostCall(errorOut **C.char, callback func(browser.Host) error, executionID C.uint64_t) (result C.int) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = hostCallFailure(errorOut, fmt.Errorf("V8 host callback panic: %v", recovered))
		}
	}()
	host, err := lookupHostExecution(uint64(executionID))
	if err != nil {
		return hostCallFailure(errorOut, err)
	}
	if err := callback(host); err != nil {
		return hostCallFailure(errorOut, err)
	}
	return 1
}

func hostCallFailure(errorOut **C.char, err error) C.int {
	if errorOut != nil {
		message := err.Error()
		if name, ok := dom.ErrorExceptionName(err); ok {
			message = "__GOSSAMER_DOM_EXCEPTION__:" + string(name) + ":" + message
		}
		*errorOut = C.CString(message)
	}
	return 0
}

func goString(data *C.char, length C.size_t) string {
	if data == nil || length == 0 {
		return ""
	}
	return string(unsafe.Slice((*byte)(unsafe.Pointer(data)), int(length)))
}

func writeHostString(value string, valueOut **C.char, valueLengthOut *C.size_t) error {
	if valueOut != nil {
		*valueOut = nil
		if value != "" {
			*valueOut = (*C.char)(C.CBytes([]byte(value)))
			if *valueOut == nil {
				return fmt.Errorf("V8 host could not allocate string output")
			}
		}
	}
	if valueLengthOut != nil {
		*valueLengthOut = C.size_t(len(value))
	}
	return nil
}

func domElementHost(host browser.Host) (browser.DOMElementHost, error) {
	domHost, ok := host.(browser.DOMElementHost)
	if !ok {
		return nil, fmt.Errorf("V8 host does not support DOM element bindings")
	}
	return domHost, nil
}

func domMutationObserverHost(host browser.Host) (browser.DOMMutationObserverHost, error) {
	mutationHost, ok := host.(browser.DOMMutationObserverHost)
	if !ok {
		return nil, fmt.Errorf("V8 host does not support DOM mutation observers")
	}
	return mutationHost, nil
}

func domDocumentHost(host browser.Host) (browser.DOMDocumentHost, bool) {
	domHost, ok := host.(browser.DOMDocumentHost)
	return domHost, ok
}

func domComputedStyleHost(host browser.Host) (browser.DOMComputedStyleHost, error) {
	domHost, ok := host.(browser.DOMComputedStyleHost)
	if !ok {
		return nil, fmt.Errorf("V8 host does not support computed style bindings")
	}
	return domHost, nil
}

//export goGossamerV8HostDocumentMetadata
func goGossamerV8HostDocumentMetadata(
	executionID C.uint64_t,
	documentOut *C.uint64_t,
	nodeOut *C.uint32_t,
	baseURIOut **C.char,
	baseURILengthOut *C.size_t,
	foundOut *C.int,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, supported := domDocumentHost(host)
		if !supported {
			if foundOut != nil {
				*foundOut = 0
			}
			return writeHostString("", baseURIOut, baseURILengthOut)
		}
		metadata, err := domHost.DocumentMetadata()
		if err != nil {
			return err
		}
		writeNodeHandle(metadata.Root, documentOut, nodeOut)
		if foundOut != nil {
			*foundOut = 1
		}
		return writeHostString(metadata.BaseURI, baseURIOut, baseURILengthOut)
	}, executionID)
}

//export goGossamerV8HostGetElementByID
func goGossamerV8HostGetElementByID(
	executionID C.uint64_t,
	value *C.char,
	valueLength C.size_t,
	documentOut *C.uint64_t,
	nodeOut *C.uint32_t,
	foundOut *C.int,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		handle, found, err := host.GetElementByID(goString(value, valueLength))
		if err != nil {
			return err
		}
		if documentOut != nil {
			*documentOut = C.uint64_t(handle.Document)
		}
		if nodeOut != nil {
			*nodeOut = C.uint32_t(handle.Node)
		}
		if foundOut != nil {
			if found {
				*foundOut = 1
			} else {
				*foundOut = 0
			}
		}
		return nil
	}, executionID)
}

func writeNodeHandle(handle browser.NodeHandle, documentOut *C.uint64_t, nodeOut *C.uint32_t) {
	if documentOut != nil {
		*documentOut = C.uint64_t(handle.Document)
	}
	if nodeOut != nil {
		*nodeOut = C.uint32_t(handle.Node)
	}
}

func browserNodeHandle(document C.uint64_t, node C.uint32_t) browser.NodeHandle {
	return browser.NodeHandle{
		Document: browser.DocumentGeneration(document),
		Node:     dom.NodeID(node),
	}
}

//export goGossamerV8HostCreateElement
func goGossamerV8HostCreateElement(
	executionID C.uint64_t,
	name *C.char,
	nameLength C.size_t,
	documentOut *C.uint64_t,
	nodeOut *C.uint32_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		handle, err := host.CreateElement(goString(name, nameLength))
		if err != nil {
			return err
		}
		writeNodeHandle(handle, documentOut, nodeOut)
		return nil
	}, executionID)
}

//export goGossamerV8HostCreateElementNS
func goGossamerV8HostCreateElementNS(
	executionID C.uint64_t,
	namespaceURI *C.char,
	namespaceURILength C.size_t,
	qualifiedName *C.char,
	qualifiedNameLength C.size_t,
	documentOut *C.uint64_t,
	nodeOut *C.uint32_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, supported := domDocumentHost(host)
		if !supported {
			return fmt.Errorf("V8 host does not support DOM document bindings")
		}
		handle, err := domHost.CreateElementNS(
			goString(namespaceURI, namespaceURILength),
			goString(qualifiedName, qualifiedNameLength),
		)
		if err != nil {
			return err
		}
		writeNodeHandle(handle, documentOut, nodeOut)
		return nil
	}, executionID)
}

//export goGossamerV8HostCreateTextNode
func goGossamerV8HostCreateTextNode(
	executionID C.uint64_t,
	data *C.char,
	dataLength C.size_t,
	documentOut *C.uint64_t,
	nodeOut *C.uint32_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		handle, err := host.CreateTextNode(goString(data, dataLength))
		if err != nil {
			return err
		}
		writeNodeHandle(handle, documentOut, nodeOut)
		return nil
	}, executionID)
}

//export goGossamerV8HostCreateDocumentFragment
func goGossamerV8HostCreateDocumentFragment(
	executionID C.uint64_t,
	documentOut *C.uint64_t,
	nodeOut *C.uint32_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, supported := domDocumentHost(host)
		if !supported {
			return fmt.Errorf("V8 host does not support DOM document bindings")
		}
		handle, err := domHost.CreateDocumentFragment()
		if err != nil {
			return err
		}
		writeNodeHandle(handle, documentOut, nodeOut)
		return nil
	}, executionID)
}

//export goGossamerV8HostTextContent
func goGossamerV8HostTextContent(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	valueOut **C.char,
	valueLengthOut *C.size_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		value, err := host.TextContent(browser.NodeHandle{
			Document: browser.DocumentGeneration(document),
			Node:     dom.NodeID(node),
		})
		if err != nil {
			return err
		}
		if valueOut != nil {
			if value == "" {
				*valueOut = nil
			} else {
				*valueOut = (*C.char)(C.CBytes([]byte(value)))
			}
		}
		if valueLengthOut != nil {
			*valueLengthOut = C.size_t(len(value))
		}
		return nil
	}, executionID)
}

//export goGossamerV8HostSetTextContent
func goGossamerV8HostSetTextContent(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	value *C.char,
	valueLength C.size_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		return host.SetTextContent(browser.NodeHandle{
			Document: browser.DocumentGeneration(document),
			Node:     dom.NodeID(node),
		}, goString(value, valueLength))
	}, executionID)
}

//export goGossamerV8HostAppendChild
func goGossamerV8HostAppendChild(
	executionID C.uint64_t,
	parentDocument C.uint64_t,
	parentNode C.uint32_t,
	childDocument C.uint64_t,
	childNode C.uint32_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		return host.AppendChild(
			browserNodeHandle(parentDocument, parentNode),
			browserNodeHandle(childDocument, childNode),
		)
	}, executionID)
}

//export goGossamerV8HostInsertBefore
func goGossamerV8HostInsertBefore(
	executionID C.uint64_t,
	parentDocument C.uint64_t,
	parentNode C.uint32_t,
	childDocument C.uint64_t,
	childNode C.uint32_t,
	referenceDocument C.uint64_t,
	referenceNode C.uint32_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		return host.InsertBefore(
			browserNodeHandle(parentDocument, parentNode),
			browserNodeHandle(childDocument, childNode),
			browserNodeHandle(referenceDocument, referenceNode),
		)
	}, executionID)
}

//export goGossamerV8HostRemoveChild
func goGossamerV8HostRemoveChild(
	executionID C.uint64_t,
	parentDocument C.uint64_t,
	parentNode C.uint32_t,
	childDocument C.uint64_t,
	childNode C.uint32_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		return host.RemoveChild(
			browserNodeHandle(parentDocument, parentNode),
			browserNodeHandle(childDocument, childNode),
		)
	}, executionID)
}

//export goGossamerV8HostGetAttribute
func goGossamerV8HostGetAttribute(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	name *C.char,
	nameLength C.size_t,
	valueOut **C.char,
	valueLengthOut *C.size_t,
	foundOut *C.int,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		value, found, err := host.GetAttribute(browserNodeHandle(document, node), goString(name, nameLength))
		if err != nil {
			return err
		}
		if valueOut != nil {
			*valueOut = nil
			if value != "" {
				*valueOut = (*C.char)(C.CBytes([]byte(value)))
				if *valueOut == nil {
					return fmt.Errorf("V8 host could not allocate attribute output")
				}
			}
		}
		if valueLengthOut != nil {
			*valueLengthOut = C.size_t(len(value))
		}
		if foundOut != nil {
			if found {
				*foundOut = 1
			} else {
				*foundOut = 0
			}
		}
		return nil
	}, executionID)
}

//export goGossamerV8HostSetAttribute
func goGossamerV8HostSetAttribute(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	name *C.char,
	nameLength C.size_t,
	value *C.char,
	valueLength C.size_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		return host.SetAttribute(
			browserNodeHandle(document, node),
			goString(name, nameLength),
			goString(value, valueLength),
		)
	}, executionID)
}

//export goGossamerV8HostRemoveAttribute
func goGossamerV8HostRemoveAttribute(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	name *C.char,
	nameLength C.size_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		return host.RemoveAttribute(browserNodeHandle(document, node), goString(name, nameLength))
	}, executionID)
}

//export goGossamerV8HostNodeMetadata
func goGossamerV8HostNodeMetadata(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	typeOut *C.uint8_t,
	nodeNameOut **C.char,
	nodeNameLengthOut *C.size_t,
	localNameOut **C.char,
	localNameLengthOut *C.size_t,
	namespaceURIOut **C.char,
	namespaceURILengthOut *C.size_t,
	prefixOut **C.char,
	prefixLengthOut *C.size_t,
	connectedOut *C.int,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		metadata, err := domHost.NodeMetadata(browserNodeHandle(document, node))
		if err != nil {
			return err
		}
		if typeOut != nil {
			*typeOut = C.uint8_t(metadata.Type)
		}
		if connectedOut != nil {
			if metadata.Connected {
				*connectedOut = 1
			} else {
				*connectedOut = 0
			}
		}
		if err := writeHostString(metadata.NodeName, nodeNameOut, nodeNameLengthOut); err != nil {
			return err
		}
		if err := writeHostString(metadata.LocalName, localNameOut, localNameLengthOut); err != nil {
			if nodeNameOut != nil {
				C.free(unsafe.Pointer(*nodeNameOut))
				*nodeNameOut = nil
			}
			return err
		}
		if err := writeHostString(metadata.NamespaceURI, namespaceURIOut, namespaceURILengthOut); err != nil {
			freeHostStrings(nodeNameOut, localNameOut)
			return err
		}
		if err := writeHostString(metadata.Prefix, prefixOut, prefixLengthOut); err != nil {
			freeHostStrings(nodeNameOut, localNameOut, namespaceURIOut)
			return err
		}
		return nil
	}, executionID)
}

func freeHostStrings(outputs ...**C.char) {
	for _, output := range outputs {
		if output == nil || *output == nil {
			continue
		}
		C.free(unsafe.Pointer(*output))
		*output = nil
	}
}

func writeHostNodes(
	handles []browser.NodeHandle,
	document browser.DocumentGeneration,
	nodesOut **C.uint32_t,
	countOut *C.size_t,
) error {
	if nodesOut != nil {
		*nodesOut = nil
		if len(handles) != 0 {
			bytes := C.size_t(len(handles)) * C.size_t(unsafe.Sizeof(C.uint32_t(0)))
			allocated := C.malloc(bytes)
			if allocated == nil {
				return fmt.Errorf("V8 host could not allocate node output")
			}
			*nodesOut = (*C.uint32_t)(allocated)
			output := unsafe.Slice(*nodesOut, len(handles))
			for index, handle := range handles {
				if handle.Document != document {
					C.free(allocated)
					*nodesOut = nil
					return fmt.Errorf("V8 host returned a node from another document")
				}
				output[index] = C.uint32_t(handle.Node)
			}
		}
	}
	if countOut != nil {
		*countOut = C.size_t(len(handles))
	}
	return nil
}

//export goGossamerV8HostRelatedNode
func goGossamerV8HostRelatedNode(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	relation C.uint8_t,
	relatedNodeOut *C.uint32_t,
	foundOut *C.int,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		related, found, err := domHost.RelatedNode(
			browserNodeHandle(document, node),
			browser.NodeRelation(relation),
		)
		if err != nil {
			return err
		}
		if relatedNodeOut != nil {
			*relatedNodeOut = C.uint32_t(related.Node)
		}
		if foundOut != nil {
			if found {
				*foundOut = 1
			} else {
				*foundOut = 0
			}
		}
		return nil
	}, executionID)
}

//export goGossamerV8HostChildNodes
func goGossamerV8HostChildNodes(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	elementsOnly C.int,
	nodesOut **C.uint32_t,
	countOut *C.size_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		handles, err := domHost.ChildNodes(browserNodeHandle(document, node), elementsOnly != 0)
		if err != nil {
			return err
		}
		return writeHostNodes(handles, browser.DocumentGeneration(document), nodesOut, countOut)
	}, executionID)
}

//export goGossamerV8HostContains
func goGossamerV8HostContains(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	otherDocument C.uint64_t,
	otherNode C.uint32_t,
	containsOut *C.int,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		contains, err := domHost.Contains(
			browserNodeHandle(document, node),
			browserNodeHandle(otherDocument, otherNode),
		)
		if err != nil {
			return err
		}
		if containsOut != nil {
			if contains {
				*containsOut = 1
			} else {
				*containsOut = 0
			}
		}
		return nil
	}, executionID)
}

//export goGossamerV8HostReplaceChild
func goGossamerV8HostReplaceChild(
	executionID C.uint64_t,
	parentDocument C.uint64_t,
	parentNode C.uint32_t,
	childDocument C.uint64_t,
	childNode C.uint32_t,
	replacedDocument C.uint64_t,
	replacedNode C.uint32_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		return domHost.ReplaceChild(
			browserNodeHandle(parentDocument, parentNode),
			browserNodeHandle(childDocument, childNode),
			browserNodeHandle(replacedDocument, replacedNode),
		)
	}, executionID)
}

//export goGossamerV8HostMutateNodes
func goGossamerV8HostMutateNodes(
	executionID C.uint64_t,
	receiverDocument C.uint64_t,
	receiverNode C.uint32_t,
	operation C.uint8_t,
	documents *C.uint64_t,
	nodes *C.uint32_t,
	count C.size_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		length := int(count)
		handles := make([]browser.NodeHandle, length)
		if length != 0 {
			if documents == nil || nodes == nil {
				return fmt.Errorf("V8 host received an incomplete DOM mutation list")
			}
			documentValues := unsafe.Slice(documents, length)
			nodeValues := unsafe.Slice(nodes, length)
			for index := range handles {
				handles[index] = browserNodeHandle(documentValues[index], nodeValues[index])
			}
		}
		return domHost.MutateNodes(
			browserNodeHandle(receiverDocument, receiverNode),
			dom.MutationOperation(operation),
			handles,
		)
	}, executionID)
}

//export goGossamerV8HostNodeValue
func goGossamerV8HostNodeValue(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	valueOut **C.char,
	valueLengthOut *C.size_t,
	nonNullOut *C.int,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		value, nonNull, err := domHost.NodeValue(browserNodeHandle(document, node))
		if err != nil {
			return err
		}
		if nonNullOut != nil {
			if nonNull {
				*nonNullOut = 1
			} else {
				*nonNullOut = 0
			}
		}
		return writeHostString(value, valueOut, valueLengthOut)
	}, executionID)
}

//export goGossamerV8HostSetNodeValue
func goGossamerV8HostSetNodeValue(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	value *C.char,
	valueLength C.size_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		return domHost.SetNodeValue(browserNodeHandle(document, node), goString(value, valueLength))
	}, executionID)
}

//export goGossamerV8HostHasAttribute
func goGossamerV8HostHasAttribute(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	name *C.char,
	nameLength C.size_t,
	foundOut *C.int,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		found, err := domHost.HasAttribute(browserNodeHandle(document, node), goString(name, nameLength))
		if err != nil {
			return err
		}
		if foundOut != nil {
			if found {
				*foundOut = 1
			} else {
				*foundOut = 0
			}
		}
		return nil
	}, executionID)
}

//export goGossamerV8HostAttributeCount
func goGossamerV8HostAttributeCount(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	countOut *C.size_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		names, err := domHost.AttributeNames(browserNodeHandle(document, node))
		if err != nil {
			return err
		}
		if countOut != nil {
			*countOut = C.size_t(len(names))
		}
		return nil
	}, executionID)
}

//export goGossamerV8HostAttributeName
func goGossamerV8HostAttributeName(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	index C.size_t,
	nameOut **C.char,
	nameLengthOut *C.size_t,
	foundOut *C.int,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		names, err := domHost.AttributeNames(browserNodeHandle(document, node))
		if err != nil {
			return err
		}
		found := uint64(index) < uint64(len(names))
		if foundOut != nil {
			if found {
				*foundOut = 1
			} else {
				*foundOut = 0
			}
		}
		if !found {
			return writeHostString("", nameOut, nameLengthOut)
		}
		return writeHostString(names[int(index)], nameOut, nameLengthOut)
	}, executionID)
}

//export goGossamerV8HostQuerySelector
func goGossamerV8HostQuerySelector(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	selector *C.char,
	selectorLength C.size_t,
	all C.int,
	nodesOut **C.uint32_t,
	countOut *C.size_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		handles, err := domHost.QuerySelector(
			browserNodeHandle(document, node),
			goString(selector, selectorLength),
			all != 0,
		)
		if err != nil {
			return err
		}
		return writeHostNodes(handles, browser.DocumentGeneration(document), nodesOut, countOut)
	}, executionID)
}

//export goGossamerV8HostMatchesSelector
func goGossamerV8HostMatchesSelector(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	selector *C.char,
	selectorLength C.size_t,
	matchesOut *C.int,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		matches, err := domHost.MatchesSelector(
			browserNodeHandle(document, node),
			goString(selector, selectorLength),
		)
		if err != nil {
			return err
		}
		if matchesOut != nil {
			if matches {
				*matchesOut = 1
			} else {
				*matchesOut = 0
			}
		}
		return nil
	}, executionID)
}

//export goGossamerV8HostClosestSelector
func goGossamerV8HostClosestSelector(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	selector *C.char,
	selectorLength C.size_t,
	closestNodeOut *C.uint32_t,
	foundOut *C.int,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		closest, found, err := domHost.ClosestSelector(
			browserNodeHandle(document, node),
			goString(selector, selectorLength),
		)
		if err != nil {
			return err
		}
		if closestNodeOut != nil {
			*closestNodeOut = C.uint32_t(closest.Node)
		}
		if foundOut != nil {
			if found {
				*foundOut = 1
			} else {
				*foundOut = 0
			}
		}
		return nil
	}, executionID)
}

//export goGossamerV8HostCloneNode
func goGossamerV8HostCloneNode(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	deep C.int,
	cloneDocumentOut *C.uint64_t,
	cloneNodeOut *C.uint32_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		clone, err := domHost.CloneNode(browserNodeHandle(document, node), deep != 0)
		if err != nil {
			return err
		}
		writeNodeHandle(clone, cloneDocumentOut, cloneNodeOut)
		return nil
	}, executionID)
}

//export goGossamerV8HostTemplateContent
func goGossamerV8HostTemplateContent(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	contentDocumentOut *C.uint64_t,
	contentNodeOut *C.uint32_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		content, err := domHost.TemplateContent(browserNodeHandle(document, node))
		if err != nil {
			return err
		}
		writeNodeHandle(content, contentDocumentOut, contentNodeOut)
		return nil
	}, executionID)
}

//export goGossamerV8HostSplitText
func goGossamerV8HostSplitText(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	offset C.int32_t,
	splitDocumentOut *C.uint64_t,
	splitNodeOut *C.uint32_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		split, err := domHost.SplitText(browserNodeHandle(document, node), int(offset))
		if err != nil {
			return err
		}
		writeNodeHandle(split, splitDocumentOut, splitNodeOut)
		return nil
	}, executionID)
}

//export goGossamerV8HostNormalizeNode
func goGossamerV8HostNormalizeNode(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		return domHost.Normalize(browserNodeHandle(document, node))
	}, executionID)
}

//export goGossamerV8HostAdoptNode
func goGossamerV8HostAdoptNode(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	adoptedDocumentOut *C.uint64_t,
	adoptedNodeOut *C.uint32_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		adopted, err := domHost.AdoptNode(browserNodeHandle(document, node))
		if err != nil {
			return err
		}
		writeNodeHandle(adopted, adoptedDocumentOut, adoptedNodeOut)
		return nil
	}, executionID)
}

//export goGossamerV8HostRangeContents
func goGossamerV8HostRangeContents(
	executionID C.uint64_t,
	startDocument C.uint64_t,
	startNode C.uint32_t,
	startOffset C.int32_t,
	endDocument C.uint64_t,
	endNode C.uint32_t,
	endOffset C.int32_t,
	operation C.uint8_t,
	fragmentDocumentOut *C.uint64_t,
	fragmentNodeOut *C.uint32_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		fragment, err := domHost.RangeContents(
			browserNodeHandle(startDocument, startNode),
			int(startOffset),
			browserNodeHandle(endDocument, endNode),
			int(endOffset),
			dom.RangeContentOperation(operation),
		)
		if err != nil {
			return err
		}
		writeNodeHandle(fragment, fragmentDocumentOut, fragmentNodeOut)
		return nil
	}, executionID)
}

//export goGossamerV8HostInnerHTML
func goGossamerV8HostInnerHTML(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	valueOut **C.char,
	valueLengthOut *C.size_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		value, err := domHost.InnerHTML(browserNodeHandle(document, node))
		if err != nil {
			return err
		}
		return writeHostString(value, valueOut, valueLengthOut)
	}, executionID)
}

//export goGossamerV8HostSetInnerHTML
func goGossamerV8HostSetInnerHTML(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	value *C.char,
	valueLength C.size_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		return domHost.SetInnerHTML(
			browserNodeHandle(document, node),
			goString(value, valueLength),
		)
	}, executionID)
}

//export goGossamerV8HostInsertAdjacentHTML
func goGossamerV8HostInsertAdjacentHTML(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	position *C.char,
	positionLength C.size_t,
	value *C.char,
	valueLength C.size_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		return domHost.InsertAdjacentHTML(
			browserNodeHandle(document, node),
			goString(position, positionLength),
			goString(value, valueLength),
		)
	}, executionID)
}

//export goGossamerV8HostFormValue
func goGossamerV8HostFormValue(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	valueOut **C.char,
	valueLengthOut *C.size_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		value, err := domHost.FormValue(browserNodeHandle(document, node))
		if err != nil {
			return err
		}
		return writeHostString(value, valueOut, valueLengthOut)
	}, executionID)
}

//export goGossamerV8HostSetFormValue
func goGossamerV8HostSetFormValue(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	value *C.char,
	valueLength C.size_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		return domHost.SetFormValue(browserNodeHandle(document, node), goString(value, valueLength))
	}, executionID)
}

//export goGossamerV8HostFormSelection
func goGossamerV8HostFormSelection(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	startOut *C.int32_t,
	endOut *C.int32_t,
	directionOut **C.char,
	directionLengthOut *C.size_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		start, end, direction, err := domHost.FormSelection(browserNodeHandle(document, node))
		if err != nil {
			return err
		}
		if startOut != nil {
			*startOut = C.int32_t(start)
		}
		if endOut != nil {
			*endOut = C.int32_t(end)
		}
		return writeHostString(direction, directionOut, directionLengthOut)
	}, executionID)
}

//export goGossamerV8HostSetFormSelection
func goGossamerV8HostSetFormSelection(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	start C.int32_t,
	end C.int32_t,
	direction *C.char,
	directionLength C.size_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		return domHost.SetFormSelection(
			browserNodeHandle(document, node), int(start), int(end),
			goString(direction, directionLength),
		)
	}, executionID)
}

//export goGossamerV8HostFormChecked
func goGossamerV8HostFormChecked(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	checkedOut *C.int,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		checked, err := domHost.FormChecked(browserNodeHandle(document, node))
		if err != nil {
			return err
		}
		if checkedOut != nil {
			if checked {
				*checkedOut = 1
			} else {
				*checkedOut = 0
			}
		}
		return nil
	}, executionID)
}

//export goGossamerV8HostSetFormChecked
func goGossamerV8HostSetFormChecked(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	checked C.int,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		return domHost.SetFormChecked(browserNodeHandle(document, node), checked != 0)
	}, executionID)
}

//export goGossamerV8HostFormSelected
func goGossamerV8HostFormSelected(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	selectedOut *C.int,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		selected, err := domHost.FormSelected(browserNodeHandle(document, node))
		if err != nil {
			return err
		}
		if selectedOut != nil {
			if selected {
				*selectedOut = 1
			} else {
				*selectedOut = 0
			}
		}
		return nil
	}, executionID)
}

//export goGossamerV8HostSetFormSelected
func goGossamerV8HostSetFormSelected(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	selected C.int,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		return domHost.SetFormSelected(browserNodeHandle(document, node), selected != 0)
	}, executionID)
}

//export goGossamerV8HostFormSelectedIndex
func goGossamerV8HostFormSelectedIndex(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	indexOut *C.int32_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		index, err := domHost.FormSelectedIndex(browserNodeHandle(document, node))
		if err != nil {
			return err
		}
		if indexOut != nil {
			*indexOut = C.int32_t(index)
		}
		return nil
	}, executionID)
}

//export goGossamerV8HostSetFormSelectedIndex
func goGossamerV8HostSetFormSelectedIndex(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	index C.int32_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		return domHost.SetFormSelectedIndex(browserNodeHandle(document, node), int(index))
	}, executionID)
}

//export goGossamerV8HostFormControlNodes
func goGossamerV8HostFormControlNodes(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	kind C.uint8_t,
	nodesOut **C.uint32_t,
	countOut *C.size_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		handles, err := domHost.FormControlNodes(
			browserNodeHandle(document, node), dom.FormCollectionKind(kind))
		if err != nil {
			return err
		}
		return writeHostNodes(handles, browser.DocumentGeneration(document), nodesOut, countOut)
	}, executionID)
}

//export goGossamerV8HostFormOwner
func goGossamerV8HostFormOwner(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	ownerNodeOut *C.uint32_t,
	foundOut *C.int,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		owner, found, err := domHost.FormOwner(browserNodeHandle(document, node))
		if err != nil {
			return err
		}
		if ownerNodeOut != nil {
			*ownerNodeOut = C.uint32_t(owner.Node)
		}
		if foundOut != nil {
			if found {
				*foundOut = 1
			} else {
				*foundOut = 0
			}
		}
		return nil
	}, executionID)
}

//export goGossamerV8HostResetForm
func goGossamerV8HostResetForm(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		return domHost.ResetForm(browserNodeHandle(document, node))
	}, executionID)
}

//export goGossamerV8HostFormValidity
func goGossamerV8HostFormValidity(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	validOut *C.int,
	invalidNodesOut **C.uint32_t,
	countOut *C.size_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		valid, invalid, err := domHost.FormValidity(browserNodeHandle(document, node))
		if err != nil {
			return err
		}
		if validOut != nil {
			if valid {
				*validOut = 1
			} else {
				*validOut = 0
			}
		}
		return writeHostNodes(invalid, browser.DocumentGeneration(document), invalidNodesOut, countOut)
	}, executionID)
}

//export goGossamerV8HostFormDataJSON
func goGossamerV8HostFormDataJSON(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	submitterDocument C.uint64_t,
	submitterNode C.uint32_t,
	jsonOut **C.char,
	jsonLengthOut *C.size_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		entries, err := domHost.FormData(
			browserNodeHandle(document, node),
			browserNodeHandle(submitterDocument, submitterNode),
		)
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(entries)
		if err != nil {
			return fmt.Errorf("marshal FormData entries: %w", err)
		}
		return writeHostString(string(encoded), jsonOut, jsonLengthOut)
	}, executionID)
}

//export goGossamerV8HostSubmitForm
func goGossamerV8HostSubmitForm(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	submitterDocument C.uint64_t,
	submitterNode C.uint32_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		return domHost.SubmitForm(
			browserNodeHandle(document, node),
			browserNodeHandle(submitterDocument, submitterNode),
		)
	}, executionID)
}

//export goGossamerV8HostFocusNode
func goGossamerV8HostFocusNode(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	focused C.int,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		if focused != 0 {
			return domHost.Focus(browserNodeHandle(document, node))
		}
		return domHost.Blur(browserNodeHandle(document, node))
	}, executionID)
}

//export goGossamerV8HostActiveElement
func goGossamerV8HostActiveElement(
	executionID C.uint64_t,
	documentOut *C.uint64_t,
	nodeOut *C.uint32_t,
	foundOut *C.int,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		active, found, err := domHost.ActiveElement()
		if err != nil {
			return err
		}
		writeNodeHandle(active, documentOut, nodeOut)
		if foundOut != nil {
			if found {
				*foundOut = 1
			} else {
				*foundOut = 0
			}
		}
		return nil
	}, executionID)
}

//export goGossamerV8HostMutationSequence
func goGossamerV8HostMutationSequence(
	executionID C.uint64_t,
	sequenceOut *C.uint64_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		mutationHost, err := domMutationObserverHost(host)
		if err != nil {
			return err
		}
		sequence, err := mutationHost.MutationSequence()
		if err != nil {
			return err
		}
		if sequenceOut != nil {
			*sequenceOut = C.uint64_t(sequence)
		}
		return nil
	}, executionID)
}

//export goGossamerV8HostMutationRecords
func goGossamerV8HostMutationRecords(
	executionID C.uint64_t,
	sinceSequence C.uint64_t,
	recordsOut **C.gossamer_v8_mutation_record,
	countOut *C.size_t,
	latestSequenceOut *C.uint64_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		mutationHost, err := domMutationObserverHost(host)
		if err != nil {
			return err
		}
		records, latest, err := mutationHost.MutationRecordsSince(uint64(sinceSequence))
		if err != nil {
			return err
		}
		if countOut != nil {
			*countOut = C.size_t(len(records))
		}
		if latestSequenceOut != nil {
			*latestSequenceOut = C.uint64_t(latest)
		}
		if recordsOut == nil {
			return nil
		}
		*recordsOut = nil
		if len(records) == 0 {
			return nil
		}
		allocated := C.calloc(C.size_t(len(records)), C.size_t(C.sizeof_gossamer_v8_mutation_record))
		if allocated == nil {
			return fmt.Errorf("V8 host could not allocate mutation records")
		}
		output := unsafe.Slice((*C.gossamer_v8_mutation_record)(allocated), len(records))
		cleanup := func() {
			for index := range output {
				C.free(unsafe.Pointer(output[index].added_nodes))
				C.free(unsafe.Pointer(output[index].removed_nodes))
				C.free(unsafe.Pointer(output[index].attribute_name))
				C.free(unsafe.Pointer(output[index].old_value))
			}
			C.free(allocated)
		}
		writeNodes := func(ids []dom.NodeID, nodesOut **C.uint32_t, nodesCount *C.size_t) error {
			*nodesOut = nil
			*nodesCount = C.size_t(len(ids))
			if len(ids) == 0 {
				return nil
			}
			bytes := C.size_t(len(ids)) * C.size_t(unsafe.Sizeof(C.uint32_t(0)))
			memory := C.malloc(bytes)
			if memory == nil {
				return fmt.Errorf("V8 host could not allocate mutation node IDs")
			}
			values := unsafe.Slice((*C.uint32_t)(memory), len(ids))
			for index, id := range ids {
				values[index] = C.uint32_t(id)
			}
			*nodesOut = (*C.uint32_t)(memory)
			return nil
		}
		for index, record := range records {
			output[index].sequence = C.uint64_t(record.Sequence)
			output[index]._type = C.uint8_t(record.Type)
			output[index].target = C.uint32_t(record.Target)
			if err := writeNodes(record.AddedNodes, &output[index].added_nodes, &output[index].added_count); err != nil {
				cleanup()
				return err
			}
			if err := writeNodes(record.RemovedNodes, &output[index].removed_nodes, &output[index].removed_count); err != nil {
				cleanup()
				return err
			}
			if record.PreviousSibling != dom.InvalidNodeID {
				output[index].previous_sibling = C.uint32_t(record.PreviousSibling)
				output[index].has_previous_sibling = 1
			}
			if record.NextSibling != dom.InvalidNodeID {
				output[index].next_sibling = C.uint32_t(record.NextSibling)
				output[index].has_next_sibling = 1
			}
			if record.AttributeName != "" {
				output[index].attribute_name = (*C.char)(C.CBytes([]byte(record.AttributeName)))
				if output[index].attribute_name == nil {
					cleanup()
					return fmt.Errorf("V8 host could not allocate mutation attribute name")
				}
				output[index].attribute_name_length = C.size_t(len(record.AttributeName))
			}
			if record.OldValue != "" {
				output[index].old_value = (*C.char)(C.CBytes([]byte(record.OldValue)))
				if output[index].old_value == nil {
					cleanup()
					return fmt.Errorf("V8 host could not allocate mutation old value")
				}
				output[index].old_value_length = C.size_t(len(record.OldValue))
			}
			if record.OldValuePresent {
				output[index].old_value_present = 1
			}
		}
		*recordsOut = (*C.gossamer_v8_mutation_record)(allocated)
		return nil
	}, executionID)
}

//export goGossamerV8HostStyleCSSText
func goGossamerV8HostStyleCSSText(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	valueOut **C.char,
	valueLengthOut *C.size_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		value, err := domHost.StyleCSSText(browserNodeHandle(document, node))
		if err != nil {
			return err
		}
		return writeHostString(value, valueOut, valueLengthOut)
	}, executionID)
}

//export goGossamerV8HostSetStyleCSSText
func goGossamerV8HostSetStyleCSSText(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	value *C.char,
	valueLength C.size_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		return domHost.SetStyleCSSText(browserNodeHandle(document, node), goString(value, valueLength))
	}, executionID)
}

//export goGossamerV8HostStyleProperty
func goGossamerV8HostStyleProperty(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	name *C.char,
	nameLength C.size_t,
	valueOut **C.char,
	valueLengthOut *C.size_t,
	priorityOut **C.char,
	priorityLengthOut *C.size_t,
	foundOut *C.int,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		value, priority, found, err := domHost.StyleProperty(
			browserNodeHandle(document, node), goString(name, nameLength),
		)
		if err != nil {
			return err
		}
		if foundOut != nil {
			if found {
				*foundOut = 1
			} else {
				*foundOut = 0
			}
		}
		if err := writeHostString(value, valueOut, valueLengthOut); err != nil {
			return err
		}
		if err := writeHostString(priority, priorityOut, priorityLengthOut); err != nil {
			if valueOut != nil {
				C.free(unsafe.Pointer(*valueOut))
				*valueOut = nil
			}
			return err
		}
		return nil
	}, executionID)
}

//export goGossamerV8HostSetStyleProperty
func goGossamerV8HostSetStyleProperty(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	name *C.char,
	nameLength C.size_t,
	value *C.char,
	valueLength C.size_t,
	priority *C.char,
	priorityLength C.size_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		return domHost.SetStyleProperty(
			browserNodeHandle(document, node),
			goString(name, nameLength),
			goString(value, valueLength),
			goString(priority, priorityLength),
		)
	}, executionID)
}

//export goGossamerV8HostRemoveStyleProperty
func goGossamerV8HostRemoveStyleProperty(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	name *C.char,
	nameLength C.size_t,
	valueOut **C.char,
	valueLengthOut *C.size_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		value, err := domHost.RemoveStyleProperty(
			browserNodeHandle(document, node), goString(name, nameLength),
		)
		if err != nil {
			return err
		}
		return writeHostString(value, valueOut, valueLengthOut)
	}, executionID)
}

//export goGossamerV8HostStylePropertyCount
func goGossamerV8HostStylePropertyCount(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	countOut *C.size_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		names, err := domHost.StylePropertyNames(browserNodeHandle(document, node))
		if err != nil {
			return err
		}
		if countOut != nil {
			*countOut = C.size_t(len(names))
		}
		return nil
	}, executionID)
}

//export goGossamerV8HostStylePropertyName
func goGossamerV8HostStylePropertyName(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	index C.size_t,
	nameOut **C.char,
	nameLengthOut *C.size_t,
	foundOut *C.int,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domElementHost(host)
		if err != nil {
			return err
		}
		names, err := domHost.StylePropertyNames(browserNodeHandle(document, node))
		if err != nil {
			return err
		}
		found := uint64(index) < uint64(len(names))
		if foundOut != nil {
			if found {
				*foundOut = 1
			} else {
				*foundOut = 0
			}
		}
		if !found {
			return writeHostString("", nameOut, nameLengthOut)
		}
		return writeHostString(names[int(index)], nameOut, nameLengthOut)
	}, executionID)
}

//export goGossamerV8HostComputedStyleProperty
func goGossamerV8HostComputedStyleProperty(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	pseudo *C.char,
	pseudoLength C.size_t,
	name *C.char,
	nameLength C.size_t,
	valueOut **C.char,
	valueLengthOut *C.size_t,
	foundOut *C.int,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domComputedStyleHost(host)
		if err != nil {
			return err
		}
		value, found, err := domHost.ComputedStyleProperty(
			browserNodeHandle(document, node),
			goString(pseudo, pseudoLength),
			goString(name, nameLength),
		)
		if err != nil {
			return err
		}
		if foundOut != nil {
			if found {
				*foundOut = 1
			} else {
				*foundOut = 0
			}
		}
		return writeHostString(value, valueOut, valueLengthOut)
	}, executionID)
}

//export goGossamerV8HostComputedStylePropertyCount
func goGossamerV8HostComputedStylePropertyCount(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	pseudo *C.char,
	pseudoLength C.size_t,
	countOut *C.size_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domComputedStyleHost(host)
		if err != nil {
			return err
		}
		names, err := domHost.ComputedStylePropertyNames(
			browserNodeHandle(document, node), goString(pseudo, pseudoLength),
		)
		if err != nil {
			return err
		}
		if countOut != nil {
			*countOut = C.size_t(len(names))
		}
		return nil
	}, executionID)
}

//export goGossamerV8HostComputedStylePropertyName
func goGossamerV8HostComputedStylePropertyName(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	pseudo *C.char,
	pseudoLength C.size_t,
	index C.size_t,
	nameOut **C.char,
	nameLengthOut *C.size_t,
	foundOut *C.int,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		domHost, err := domComputedStyleHost(host)
		if err != nil {
			return err
		}
		names, err := domHost.ComputedStylePropertyNames(
			browserNodeHandle(document, node), goString(pseudo, pseudoLength),
		)
		if err != nil {
			return err
		}
		found := uint64(index) < uint64(len(names))
		if foundOut != nil {
			if found {
				*foundOut = 1
			} else {
				*foundOut = 0
			}
		}
		if !found {
			return writeHostString("", nameOut, nameLengthOut)
		}
		return writeHostString(names[int(index)], nameOut, nameLengthOut)
	}, executionID)
}

//export goGossamerV8HostRetainNodeWrapper
func goGossamerV8HostRetainNodeWrapper(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		lifetimes, ok := host.(browser.NodeWrapperLifetimeHost)
		if !ok {
			return fmt.Errorf("V8 host does not support node wrapper lifetimes")
		}
		return lifetimes.RetainNodeWrapper(browserNodeHandle(document, node))
	}, executionID)
}

//export goGossamerV8HostRetainNodeEventTarget
func goGossamerV8HostRetainNodeEventTarget(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		lifetimes, ok := host.(browser.NodeEventListenerLifetimeHost)
		if !ok {
			return fmt.Errorf("V8 host does not support event listener lifetimes")
		}
		return lifetimes.RetainNodeEventTarget(browserNodeHandle(document, node))
	}, executionID)
}

//export goGossamerV8HostReleaseNodeEventTarget
func goGossamerV8HostReleaseNodeEventTarget(
	executionID C.uint64_t,
	document C.uint64_t,
	node C.uint32_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		lifetimes, ok := host.(browser.NodeEventListenerLifetimeHost)
		if !ok {
			return fmt.Errorf("V8 host does not support event listener lifetimes")
		}
		return lifetimes.ReleaseNodeEventTarget(browserNodeHandle(document, node))
	}, executionID)
}

//export goGossamerV8HostQueueCallback
func goGossamerV8HostQueueCallback(executionID C.uint64_t, callback C.uint64_t, errorOut **C.char) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		return host.QueueCallback(browser.ValueHandle(callback))
	}, executionID)
}

//export goGossamerV8HostQueueMicrotask
func goGossamerV8HostQueueMicrotask(executionID C.uint64_t, callback C.uint64_t, errorOut **C.char) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		return host.QueueMicrotask(browser.ValueHandle(callback))
	}, executionID)
}

//export goGossamerV8HostSetTimeout
func goGossamerV8HostSetTimeout(
	executionID C.uint64_t,
	callback C.uint64_t,
	delayMilliseconds C.int64_t,
	timerOut *C.uint64_t,
	errorOut **C.char,
) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		timer, err := host.SetTimeout(
			browser.ValueHandle(callback),
			time.Duration(delayMilliseconds)*time.Millisecond,
		)
		if err != nil {
			return err
		}
		if timerOut != nil {
			*timerOut = C.uint64_t(timer)
		}
		return nil
	}, executionID)
}

//export goGossamerV8HostClearTimeout
func goGossamerV8HostClearTimeout(executionID C.uint64_t, timer C.uint64_t, errorOut **C.char) C.int {
	return runHostCall(errorOut, func(host browser.Host) error {
		return host.ClearTimeout(browser.TimerID(timer))
	}, executionID)
}
