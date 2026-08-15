//go:build v8 && cgo && darwin && arm64

package v8engine

/*
#include <stdlib.h>
#include "host_callbacks.h"
*/
import "C"

import (
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
		*errorOut = C.CString(err.Error())
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
		return nil
	}, executionID)
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
		if nodesOut != nil {
			*nodesOut = nil
			if len(handles) != 0 {
				bytes := C.size_t(len(handles)) * C.size_t(unsafe.Sizeof(C.uint32_t(0)))
				allocated := C.malloc(bytes)
				if allocated == nil {
					return fmt.Errorf("V8 host could not allocate child node output")
				}
				*nodesOut = (*C.uint32_t)(allocated)
				output := unsafe.Slice(*nodesOut, len(handles))
				for index, handle := range handles {
					if handle.Document != browser.DocumentGeneration(document) {
						C.free(allocated)
						*nodesOut = nil
						return fmt.Errorf("V8 host returned a child from another document")
					}
					output[index] = C.uint32_t(handle.Node)
				}
			}
		}
		if countOut != nil {
			*countOut = C.size_t(len(handles))
		}
		return nil
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
