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
