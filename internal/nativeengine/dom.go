package nativeengine

import (
	"fmt"
	"math"
	"strconv"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

const (
	nativeDocumentGetElementByID uint64 = 10_000 + iota
	nativeDocumentCreateElement
	nativeDocumentCreateTextNode
	nativeDocumentElement
	nativeDocumentHead
	nativeDocumentBody
	nativeNodeType
	nativeNodeName
	nativeNodeParent
	nativeNodeFirstChild
	nativeNodeLastChild
	nativeNodePreviousSibling
	nativeNodeNextSibling
	nativeNodeTextContentGet
	nativeNodeTextContentSet
	nativeNodeChildNodes
	nativeNodeAppendChild
	nativeNodeInsertBefore
	nativeNodeRemoveChild
	nativeNodeContains
	nativeNodeCloneNode
	nativeElementLocalName
	nativeElementChildren
	nativeElementGetAttribute
	nativeElementSetAttribute
	nativeElementRemoveAttribute
	nativeElementHasAttribute
	nativeElementIDGet
	nativeElementIDSet
	nativeElementQuerySelector
	nativeElementQuerySelectorAll
	nativeElementMatches
	nativeElementClosest
	nativeDocumentCreateElementNS
	nativeDocumentCreateDocumentFragment
	nativeDocumentDefaultView
	nativeDocumentBaseURI
	nativeDocumentReadyState
	nativeNodeParentElement
	nativeNodeOwnerDocument
	nativeNodeNamespaceURI
	nativeNodePrefix
	nativeNodeIsConnected
	nativeNodeValueGet
	nativeNodeValueSet
	nativeNodeReplaceChild
	nativeNodeNormalize
	nativeTextSplitText
	nativeElementGetAttributeNames
	nativeElementClassNameGet
	nativeElementClassNameSet
	nativeElementClassList
	nativeElementDataset
	nativeElementInnerHTMLGet
	nativeElementInnerHTMLSet
	nativeElementInsertAdjacentHTML
	nativeElementStyle
	nativeClassListValue
	nativeClassListLength
	nativeClassListAdd
	nativeClassListRemove
	nativeClassListContains
	nativeClassListToggle
	nativeClassListItem
	nativeClassListToString
	nativeStyleCSSTextGet
	nativeStyleCSSTextSet
	nativeStyleLength
	nativeStyleGetPropertyValue
	nativeStyleGetPropertyPriority
	nativeStyleSetProperty
	nativeStyleRemoveProperty
	nativeStyleItem
	nativeElementFormValueGet
	nativeElementFormValueSet
	nativeElementFormCheckedGet
	nativeElementFormCheckedSet
	nativeElementFormSelectedGet
	nativeElementFormSelectedSet
	nativeElementFormSelectedIndexGet
	nativeElementFormSelectedIndexSet
	nativeElementSelectionStartGet
	nativeElementSelectionStartSet
	nativeElementSelectionEndGet
	nativeElementSelectionEndSet
	nativeElementSelectionDirectionGet
	nativeElementSelectionDirectionSet
	nativeElementSetSelectionRange
	nativeElementSelect
	nativeElementFocus
	nativeElementBlur
	nativeDocumentActiveElement
	nativeDocumentScrollingElement
	nativeGlobalGetComputedStyle
	nativeComputedStyleCSSText
	nativeComputedStyleLength
	nativeComputedStyleGetPropertyValue
	nativeComputedStyleGetPropertyPriority
	nativeComputedStyleItem
	nativeElementGetBoundingClientRect
	nativeElementGetClientRects
	nativeElementClientWidth
	nativeElementClientHeight
	nativeElementOffsetWidth
	nativeElementOffsetHeight
	nativeElementScrollWidth
	nativeElementScrollHeight
	nativeElementScrollLeftGet
	nativeElementScrollLeftSet
	nativeElementScrollTopGet
	nativeElementScrollTopSet
	nativeWindowInnerWidth
	nativeWindowInnerHeight
	nativeWindowScrollX
	nativeWindowScrollY
	nativeDOMRectToJSON
)

const (
	hostClassWindow memory.HostClass = iota + 1
	hostClassNode
	hostClassClassList
	hostClassDataset
	hostClassStyle
	hostClassComputedStyle
	hostClassMutationObserver
)

const (
	bindingWindowPrototype             = "\x00gossamer.window.prototype"
	bindingDocumentPrototype           = "\x00gossamer.document.prototype"
	bindingNodePrototype               = "\x00gossamer.node.prototype"
	bindingElementPrototype            = "\x00gossamer.element.prototype"
	bindingTextPrototype               = "\x00gossamer.text.prototype"
	bindingFragmentPrototype           = "\x00gossamer.fragment.prototype"
	bindingEventPrototype              = "\x00gossamer.event.prototype"
	bindingClassListPrototype          = "\x00gossamer.class-list.prototype"
	bindingDatasetPrototype            = "\x00gossamer.dataset.prototype"
	bindingStylePrototype              = "\x00gossamer.style.prototype"
	bindingComputedStylePrototype      = "\x00gossamer.computed-style.prototype"
	bindingDOMRectPrototype            = "\x00gossamer.dom-rect.prototype"
	bindingMutationObserverPrototype   = "\x00gossamer.mutation-observer.prototype"
	bindingMutationObserverConstructor = "\x00gossamer.mutation-observer.constructor"
	bindingWrapperCache                = "\x00gossamer.wrapper.cache"
	bindingCallbackCache               = "\x00gossamer.callback.cache"
	bindingFacadeCache                 = "\x00gossamer.facade.cache"
	bindingCollectionCache             = "\x00gossamer.collection.cache"
	bindingObserverCache               = "\x00gossamer.observer.cache"
	bindingWindow                      = "window"
	bindingSelf                        = "self"
	bindingDocument                    = "document"
	bindingPerformance                 = "performance"
	bindingMutationObserver            = "MutationObserver"
	hostRecordProperty                 = "\x00gossamer.host.record"
)

type browserBindings struct {
	windowPrototype             memory.Ref
	documentPrototype           memory.Ref
	nodePrototype               memory.Ref
	elementPrototype            memory.Ref
	textPrototype               memory.Ref
	fragmentPrototype           memory.Ref
	eventPrototype              memory.Ref
	classListPrototype          memory.Ref
	datasetPrototype            memory.Ref
	stylePrototype              memory.Ref
	computedStylePrototype      memory.Ref
	domRectPrototype            memory.Ref
	mutationObserverPrototype   memory.Ref
	mutationObserverConstructor memory.Ref
	wrapperCache                memory.Ref
	callbackCache               memory.Ref
	facadeCache                 memory.Ref
	collectionCache             memory.Ref
	observerCache               memory.Ref
	window                      memory.Ref
	document                    memory.Ref
	performance                 memory.Ref
}

type nativeRegistration struct {
	id       uint64
	callback browserruntime.NativeFunction
}

func (realm *Realm) installBrowserNatives() error {
	registrations := []nativeRegistration{
		{nativeDocumentGetElementByID, realm.documentGetElementByID},
		{nativeDocumentCreateElement, realm.documentCreateElement},
		{nativeDocumentCreateTextNode, realm.documentCreateTextNode},
		{nativeDocumentElement, realm.documentRelation(browser.RelationDocumentElement)},
		{nativeDocumentHead, realm.documentRelation(browser.RelationDocumentHead)},
		{nativeDocumentBody, realm.documentRelation(browser.RelationDocumentBody)},
		{nativeNodeType, realm.nodeType},
		{nativeNodeName, realm.nodeName},
		{nativeNodeParent, realm.nodeRelation(browser.RelationParentNode)},
		{nativeNodeFirstChild, realm.nodeRelation(browser.RelationFirstChild)},
		{nativeNodeLastChild, realm.nodeRelation(browser.RelationLastChild)},
		{nativeNodePreviousSibling, realm.nodeRelation(browser.RelationPreviousSibling)},
		{nativeNodeNextSibling, realm.nodeRelation(browser.RelationNextSibling)},
		{nativeNodeTextContentGet, realm.nodeTextContentGet},
		{nativeNodeTextContentSet, realm.nodeTextContentSet},
		{nativeNodeChildNodes, realm.nodeChildren(false)},
		{nativeNodeAppendChild, realm.nodeAppendChild},
		{nativeNodeInsertBefore, realm.nodeInsertBefore},
		{nativeNodeRemoveChild, realm.nodeRemoveChild},
		{nativeNodeContains, realm.nodeContains},
		{nativeNodeCloneNode, realm.nodeCloneNode},
		{nativeElementLocalName, realm.elementLocalName},
		{nativeElementChildren, realm.nodeChildren(true)},
		{nativeElementGetAttribute, realm.elementGetAttribute},
		{nativeElementSetAttribute, realm.elementSetAttribute},
		{nativeElementRemoveAttribute, realm.elementRemoveAttribute},
		{nativeElementHasAttribute, realm.elementHasAttribute},
		{nativeElementIDGet, realm.elementIDGet},
		{nativeElementIDSet, realm.elementIDSet},
		{nativeElementQuerySelector, realm.elementQuerySelector(false)},
		{nativeElementQuerySelectorAll, realm.elementQuerySelector(true)},
		{nativeElementMatches, realm.elementMatches},
		{nativeElementClosest, realm.elementClosest},
		{nativeDocumentCreateElementNS, realm.documentCreateElementNS},
		{nativeDocumentCreateDocumentFragment, realm.documentCreateDocumentFragment},
		{nativeDocumentDefaultView, realm.documentDefaultView},
		{nativeDocumentBaseURI, realm.documentBaseURI},
		{nativeDocumentReadyState, realm.documentReadyState},
		{nativeNodeParentElement, realm.nodeRelation(browser.RelationParentElement)},
		{nativeNodeOwnerDocument, realm.nodeOwnerDocument},
		{nativeNodeNamespaceURI, realm.nodeNamespaceURI},
		{nativeNodePrefix, realm.nodePrefix},
		{nativeNodeIsConnected, realm.nodeIsConnected},
		{nativeNodeValueGet, realm.nodeValueGet},
		{nativeNodeValueSet, realm.nodeValueSet},
		{nativeNodeReplaceChild, realm.nodeReplaceChild},
		{nativeNodeNormalize, realm.nodeNormalize},
		{nativeTextSplitText, realm.textSplitText},
		{nativeElementGetAttributeNames, realm.elementGetAttributeNames},
		{nativeElementClassNameGet, realm.elementClassNameGet},
		{nativeElementClassNameSet, realm.elementClassNameSet},
		{nativeElementClassList, realm.elementClassList},
		{nativeElementDataset, realm.elementDataset},
		{nativeElementInnerHTMLGet, realm.elementInnerHTMLGet},
		{nativeElementInnerHTMLSet, realm.elementInnerHTMLSet},
		{nativeElementInsertAdjacentHTML, realm.elementInsertAdjacentHTML},
		{nativeElementStyle, realm.elementStyle},
		{nativeClassListValue, realm.classListValue},
		{nativeClassListLength, realm.classListLength},
		{nativeClassListAdd, realm.classListAdd},
		{nativeClassListRemove, realm.classListRemove},
		{nativeClassListContains, realm.classListContains},
		{nativeClassListToggle, realm.classListToggle},
		{nativeClassListItem, realm.classListItem},
		{nativeClassListToString, realm.classListToString},
		{nativeStyleCSSTextGet, realm.styleCSSTextGet},
		{nativeStyleCSSTextSet, realm.styleCSSTextSet},
		{nativeStyleLength, realm.styleLength},
		{nativeStyleGetPropertyValue, realm.styleGetPropertyValue},
		{nativeStyleGetPropertyPriority, realm.styleGetPropertyPriority},
		{nativeStyleSetProperty, realm.styleSetProperty},
		{nativeStyleRemoveProperty, realm.styleRemoveProperty},
		{nativeStyleItem, realm.styleItem},
		{nativeElementFormValueGet, realm.elementFormValueGet},
		{nativeElementFormValueSet, realm.elementFormValueSet},
		{nativeElementFormCheckedGet, realm.elementFormCheckedGet},
		{nativeElementFormCheckedSet, realm.elementFormCheckedSet},
		{nativeElementFormSelectedGet, realm.elementFormSelectedGet},
		{nativeElementFormSelectedSet, realm.elementFormSelectedSet},
		{nativeElementFormSelectedIndexGet, realm.elementFormSelectedIndexGet},
		{nativeElementFormSelectedIndexSet, realm.elementFormSelectedIndexSet},
		{nativeElementSelectionStartGet, realm.elementSelectionStartGet},
		{nativeElementSelectionStartSet, realm.elementSelectionStartSet},
		{nativeElementSelectionEndGet, realm.elementSelectionEndGet},
		{nativeElementSelectionEndSet, realm.elementSelectionEndSet},
		{nativeElementSelectionDirectionGet, realm.elementSelectionDirectionGet},
		{nativeElementSelectionDirectionSet, realm.elementSelectionDirectionSet},
		{nativeElementSetSelectionRange, realm.elementSetSelectionRange},
		{nativeElementSelect, realm.elementSelect},
		{nativeElementFocus, realm.elementFocus},
		{nativeElementBlur, realm.elementBlur},
		{nativeDocumentActiveElement, realm.documentActiveElement},
		{nativeDocumentScrollingElement, realm.documentScrollingElement},
		{nativeGlobalGetComputedStyle, realm.globalGetComputedStyle},
		{nativeComputedStyleCSSText, realm.computedStyleCSSText},
		{nativeComputedStyleLength, realm.computedStyleLength},
		{nativeComputedStyleGetPropertyValue, realm.computedStyleGetPropertyValue},
		{nativeComputedStyleGetPropertyPriority, realm.computedStyleGetPropertyPriority},
		{nativeComputedStyleItem, realm.computedStyleItem},
		{nativeElementGetBoundingClientRect, realm.elementGetBoundingClientRect},
		{nativeElementGetClientRects, realm.elementGetClientRects},
		{nativeElementClientWidth, realm.elementGeometryValue("clientWidth")},
		{nativeElementClientHeight, realm.elementGeometryValue("clientHeight")},
		{nativeElementOffsetWidth, realm.elementGeometryValue("offsetWidth")},
		{nativeElementOffsetHeight, realm.elementGeometryValue("offsetHeight")},
		{nativeElementScrollWidth, realm.elementGeometryValue("scrollWidth")},
		{nativeElementScrollHeight, realm.elementGeometryValue("scrollHeight")},
		{nativeElementScrollLeftGet, realm.elementGeometryValue("scrollLeft")},
		{nativeElementScrollLeftSet, realm.elementScrollSet(false)},
		{nativeElementScrollTopGet, realm.elementGeometryValue("scrollTop")},
		{nativeElementScrollTopSet, realm.elementScrollSet(true)},
		{nativeWindowInnerWidth, realm.windowGeometryValue("innerWidth")},
		{nativeWindowInnerHeight, realm.windowGeometryValue("innerHeight")},
		{nativeWindowScrollX, realm.windowGeometryValue("scrollX")},
		{nativeWindowScrollY, realm.windowGeometryValue("scrollY")},
		{nativeDOMRectToJSON, realm.domRectToJSON},
		{nativeGlobalSetTimeout, realm.globalSetTimeout},
		{nativeGlobalClearTimeout, realm.globalClearTimeout},
		{nativeGlobalRequestAnimationFrame, realm.globalRequestAnimationFrame},
		{nativeGlobalCancelAnimationFrame, realm.globalCancelAnimationFrame},
		{nativePerformanceNow, realm.performanceNow},
		{nativeMutationObserverConstructor, realm.mutationObserverConstructor},
		{nativeMutationObserverObserve, realm.mutationObserverObserve},
		{nativeMutationObserverDisconnect, realm.mutationObserverDisconnect},
		{nativeMutationObserverTakeRecords, realm.mutationObserverTakeRecords},
		{nativeEventTargetAdd, realm.eventTargetAdd},
		{nativeEventTargetRemove, realm.eventTargetRemove},
		{nativeEventPreventDefault, realm.eventPreventDefault},
		{nativeEventStopPropagation, realm.eventStopPropagation},
		{nativeEventStopImmediatePropagation, realm.eventStopImmediatePropagation},
	}
	for _, registration := range registrations {
		if err := realm.interpreter.RegisterNative(registration.id, registration.callback); err != nil {
			return err
		}
	}
	return realm.interpreter.RegisterPropertyInterceptor(browserruntime.PropertyInterceptor{
		Get: realm.facadePropertyGet, Set: realm.facadePropertySet, Delete: realm.facadePropertyDelete,
	})
}

func (realm *Realm) prepareBrowserBindingsLocked(context *browserruntime.TaskContext) error {
	window, found, err := globalRef(context, realm.active.Global, bindingWindow)
	if err != nil {
		return err
	}
	if found {
		bindings := &browserBindings{window: window}
		for _, item := range []struct {
			name        string
			destination *memory.Ref
		}{
			{bindingDocument, &bindings.document},
			{bindingPerformance, &bindings.performance},
			{bindingWindowPrototype, &bindings.windowPrototype},
			{bindingDocumentPrototype, &bindings.documentPrototype},
			{bindingNodePrototype, &bindings.nodePrototype},
			{bindingElementPrototype, &bindings.elementPrototype},
			{bindingTextPrototype, &bindings.textPrototype},
			{bindingFragmentPrototype, &bindings.fragmentPrototype},
			{bindingEventPrototype, &bindings.eventPrototype},
			{bindingClassListPrototype, &bindings.classListPrototype},
			{bindingDatasetPrototype, &bindings.datasetPrototype},
			{bindingStylePrototype, &bindings.stylePrototype},
			{bindingComputedStylePrototype, &bindings.computedStylePrototype},
			{bindingDOMRectPrototype, &bindings.domRectPrototype},
			{bindingMutationObserverPrototype, &bindings.mutationObserverPrototype},
			{bindingMutationObserverConstructor, &bindings.mutationObserverConstructor},
			{bindingWrapperCache, &bindings.wrapperCache},
			{bindingCallbackCache, &bindings.callbackCache},
			{bindingFacadeCache, &bindings.facadeCache},
			{bindingCollectionCache, &bindings.collectionCache},
			{bindingObserverCache, &bindings.observerCache},
		} {
			ref, exists, lookupErr := globalRef(context, realm.active.Global, item.name)
			if lookupErr != nil {
				return lookupErr
			}
			if !exists {
				return fmt.Errorf("nativeengine: missing retained browser binding %q", item.name)
			}
			*item.destination = ref
		}
		realm.bindings = bindings
		return nil
	}
	return realm.installBrowserBindingsLocked(context)
}

func (realm *Realm) installBrowserBindingsLocked(context *browserruntime.TaskContext) error {
	documentHost, ok := realm.host.(browser.DOMDocumentHost)
	if !ok {
		return fmt.Errorf("nativeengine: browser host does not expose document metadata")
	}
	metadata, err := documentHost.DocumentMetadata()
	if err != nil {
		return err
	}
	bindings := &browserBindings{}
	for _, destination := range []*memory.Ref{
		&bindings.windowPrototype,
		&bindings.nodePrototype,
		&bindings.documentPrototype,
		&bindings.elementPrototype,
		&bindings.textPrototype,
		&bindings.fragmentPrototype,
		&bindings.eventPrototype,
		&bindings.classListPrototype,
		&bindings.datasetPrototype,
		&bindings.stylePrototype,
		&bindings.computedStylePrototype,
		&bindings.domRectPrototype,
	} {
		*destination, err = context.NewHeapObject()
		if err != nil {
			return err
		}
	}
	if err := context.SetPrototype(bindings.documentPrototype, memory.RefValue(bindings.nodePrototype)); err != nil {
		return err
	}
	if err := context.SetPrototype(bindings.elementPrototype, memory.RefValue(bindings.nodePrototype)); err != nil {
		return err
	}
	if err := context.SetPrototype(bindings.textPrototype, memory.RefValue(bindings.nodePrototype)); err != nil {
		return err
	}
	if err := context.SetPrototype(bindings.fragmentPrototype, memory.RefValue(bindings.nodePrototype)); err != nil {
		return err
	}
	bindings.wrapperCache, err = context.NewMap()
	if err != nil {
		return err
	}
	bindings.callbackCache, err = context.NewMap()
	if err != nil {
		return err
	}
	bindings.facadeCache, err = context.NewMap()
	if err != nil {
		return err
	}
	bindings.collectionCache, err = context.NewMap()
	if err != nil {
		return err
	}
	bindings.observerCache, err = context.NewMap()
	if err != nil {
		return err
	}
	bindings.mutationObserverConstructor, bindings.mutationObserverPrototype, err = realm.newMutationObserverConstructor(context)
	if err != nil {
		return err
	}
	realm.bindings = bindings
	if err := realm.installDOMPrototypeProperties(context); err != nil {
		return err
	}

	bindings.document, err = realm.wrapNodeLocked(context, metadata.Root)
	if err != nil {
		return err
	}
	bindings.window, err = realm.newHostWrapperLocked(
		context,
		memory.HostObject{Class: hostClassWindow, Scope: uint64(metadata.Root.Document), Identity: 1},
		bindings.windowPrototype,
	)
	if err != nil {
		return err
	}
	setTimeout, err := realm.newNativeFunction(context, "setTimeout", 2, nativeGlobalSetTimeout)
	if err != nil {
		return err
	}
	clearTimeout, err := realm.newNativeFunction(context, "clearTimeout", 1, nativeGlobalClearTimeout)
	if err != nil {
		return err
	}
	requestAnimationFrame, err := realm.newNativeFunction(context, "requestAnimationFrame", 1, nativeGlobalRequestAnimationFrame)
	if err != nil {
		return err
	}
	cancelAnimationFrame, err := realm.newNativeFunction(context, "cancelAnimationFrame", 1, nativeGlobalCancelAnimationFrame)
	if err != nil {
		return err
	}
	performanceNow, err := realm.newNativeFunction(context, "now", 0, nativePerformanceNow)
	if err != nil {
		return err
	}
	bindings.performance, err = context.NewHeapObject()
	if err != nil {
		return err
	}
	if err := defineData(context, bindings.performance, "now", memory.RefValue(performanceNow), true, false, true); err != nil {
		return err
	}
	getComputedStyle, err := realm.newNativeFunction(context, "getComputedStyle", 1, nativeGlobalGetComputedStyle)
	if err != nil {
		return err
	}
	for _, property := range []struct {
		name  string
		value memory.Value
	}{
		{"window", memory.RefValue(bindings.window)},
		{"self", memory.RefValue(bindings.window)},
		{"document", memory.RefValue(bindings.document)},
		{"queueMicrotask", memory.RefValue(realm.active.QueueMicrotask)},
		{"setTimeout", memory.RefValue(setTimeout)},
		{"clearTimeout", memory.RefValue(clearTimeout)},
		{"requestAnimationFrame", memory.RefValue(requestAnimationFrame)},
		{"cancelAnimationFrame", memory.RefValue(cancelAnimationFrame)},
		{"performance", memory.RefValue(bindings.performance)},
		{"MutationObserver", memory.RefValue(bindings.mutationObserverConstructor)},
		{"getComputedStyle", memory.RefValue(getComputedStyle)},
	} {
		if err := defineData(context, bindings.window, property.name, property.value, true, false, true); err != nil {
			return err
		}
	}
	for _, binding := range []struct {
		name    string
		value   memory.Ref
		mutable bool
	}{
		{bindingWindowPrototype, bindings.windowPrototype, false},
		{bindingDocumentPrototype, bindings.documentPrototype, false},
		{bindingNodePrototype, bindings.nodePrototype, false},
		{bindingElementPrototype, bindings.elementPrototype, false},
		{bindingTextPrototype, bindings.textPrototype, false},
		{bindingFragmentPrototype, bindings.fragmentPrototype, false},
		{bindingEventPrototype, bindings.eventPrototype, false},
		{bindingClassListPrototype, bindings.classListPrototype, false},
		{bindingDatasetPrototype, bindings.datasetPrototype, false},
		{bindingStylePrototype, bindings.stylePrototype, false},
		{bindingComputedStylePrototype, bindings.computedStylePrototype, false},
		{bindingDOMRectPrototype, bindings.domRectPrototype, false},
		{bindingMutationObserverPrototype, bindings.mutationObserverPrototype, false},
		{bindingMutationObserverConstructor, bindings.mutationObserverConstructor, false},
		{bindingWrapperCache, bindings.wrapperCache, false},
		{bindingCallbackCache, bindings.callbackCache, false},
		{bindingFacadeCache, bindings.facadeCache, false},
		{bindingCollectionCache, bindings.collectionCache, false},
		{bindingObserverCache, bindings.observerCache, false},
		{bindingWindow, bindings.window, false},
		{bindingSelf, bindings.window, false},
		{bindingDocument, bindings.document, false},
		{bindingPerformance, bindings.performance, false},
		{bindingMutationObserver, bindings.mutationObserverConstructor, false},
		{"setTimeout", setTimeout, true},
		{"clearTimeout", clearTimeout, true},
		{"requestAnimationFrame", requestAnimationFrame, true},
		{"cancelAnimationFrame", cancelAnimationFrame, true},
		{"getComputedStyle", getComputedStyle, true},
	} {
		if err := declareGlobal(context, realm.active.Global, binding.name, memory.RefValue(binding.value), binding.mutable); err != nil {
			return err
		}
	}
	return nil
}

func (realm *Realm) installDOMPrototypeProperties(context *browserruntime.TaskContext) error {
	methods := []struct {
		target memory.Ref
		name   string
		arity  uint32
		id     uint64
	}{
		{realm.bindings.documentPrototype, "getElementById", 1, nativeDocumentGetElementByID},
		{realm.bindings.documentPrototype, "createElement", 1, nativeDocumentCreateElement},
		{realm.bindings.documentPrototype, "createTextNode", 1, nativeDocumentCreateTextNode},
		{realm.bindings.documentPrototype, "createElementNS", 2, nativeDocumentCreateElementNS},
		{realm.bindings.documentPrototype, "createDocumentFragment", 0, nativeDocumentCreateDocumentFragment},
		{realm.bindings.documentPrototype, "querySelector", 1, nativeElementQuerySelector},
		{realm.bindings.documentPrototype, "querySelectorAll", 1, nativeElementQuerySelectorAll},
		{realm.bindings.nodePrototype, "appendChild", 1, nativeNodeAppendChild},
		{realm.bindings.nodePrototype, "insertBefore", 2, nativeNodeInsertBefore},
		{realm.bindings.nodePrototype, "removeChild", 1, nativeNodeRemoveChild},
		{realm.bindings.nodePrototype, "contains", 1, nativeNodeContains},
		{realm.bindings.nodePrototype, "cloneNode", 1, nativeNodeCloneNode},
		{realm.bindings.nodePrototype, "replaceChild", 2, nativeNodeReplaceChild},
		{realm.bindings.nodePrototype, "normalize", 0, nativeNodeNormalize},
		{realm.bindings.textPrototype, "splitText", 1, nativeTextSplitText},
		{realm.bindings.elementPrototype, "getAttribute", 1, nativeElementGetAttribute},
		{realm.bindings.elementPrototype, "setAttribute", 2, nativeElementSetAttribute},
		{realm.bindings.elementPrototype, "removeAttribute", 1, nativeElementRemoveAttribute},
		{realm.bindings.elementPrototype, "hasAttribute", 1, nativeElementHasAttribute},
		{realm.bindings.elementPrototype, "querySelector", 1, nativeElementQuerySelector},
		{realm.bindings.elementPrototype, "querySelectorAll", 1, nativeElementQuerySelectorAll},
		{realm.bindings.elementPrototype, "matches", 1, nativeElementMatches},
		{realm.bindings.elementPrototype, "closest", 1, nativeElementClosest},
		{realm.bindings.elementPrototype, "getAttributeNames", 0, nativeElementGetAttributeNames},
		{realm.bindings.elementPrototype, "insertAdjacentHTML", 2, nativeElementInsertAdjacentHTML},
		{realm.bindings.elementPrototype, "setSelectionRange", 2, nativeElementSetSelectionRange},
		{realm.bindings.elementPrototype, "select", 0, nativeElementSelect},
		{realm.bindings.elementPrototype, "focus", 0, nativeElementFocus},
		{realm.bindings.elementPrototype, "blur", 0, nativeElementBlur},
		{realm.bindings.elementPrototype, "getBoundingClientRect", 0, nativeElementGetBoundingClientRect},
		{realm.bindings.elementPrototype, "getClientRects", 0, nativeElementGetClientRects},
		{realm.bindings.classListPrototype, "add", 1, nativeClassListAdd},
		{realm.bindings.classListPrototype, "remove", 1, nativeClassListRemove},
		{realm.bindings.classListPrototype, "contains", 1, nativeClassListContains},
		{realm.bindings.classListPrototype, "toggle", 1, nativeClassListToggle},
		{realm.bindings.classListPrototype, "item", 1, nativeClassListItem},
		{realm.bindings.classListPrototype, "toString", 0, nativeClassListToString},
		{realm.bindings.stylePrototype, "getPropertyValue", 1, nativeStyleGetPropertyValue},
		{realm.bindings.stylePrototype, "getPropertyPriority", 1, nativeStyleGetPropertyPriority},
		{realm.bindings.stylePrototype, "setProperty", 2, nativeStyleSetProperty},
		{realm.bindings.stylePrototype, "removeProperty", 1, nativeStyleRemoveProperty},
		{realm.bindings.stylePrototype, "item", 1, nativeStyleItem},
		{realm.bindings.computedStylePrototype, "getPropertyValue", 1, nativeComputedStyleGetPropertyValue},
		{realm.bindings.computedStylePrototype, "getPropertyPriority", 1, nativeComputedStyleGetPropertyPriority},
		{realm.bindings.computedStylePrototype, "item", 1, nativeComputedStyleItem},
		{realm.bindings.domRectPrototype, "toJSON", 0, nativeDOMRectToJSON},
		{realm.bindings.mutationObserverPrototype, "observe", 2, nativeMutationObserverObserve},
		{realm.bindings.mutationObserverPrototype, "disconnect", 0, nativeMutationObserverDisconnect},
		{realm.bindings.mutationObserverPrototype, "takeRecords", 0, nativeMutationObserverTakeRecords},
		{realm.bindings.nodePrototype, "addEventListener", 2, nativeEventTargetAdd},
		{realm.bindings.nodePrototype, "removeEventListener", 2, nativeEventTargetRemove},
		{realm.bindings.windowPrototype, "addEventListener", 2, nativeEventTargetAdd},
		{realm.bindings.windowPrototype, "removeEventListener", 2, nativeEventTargetRemove},
		{realm.bindings.eventPrototype, "preventDefault", 0, nativeEventPreventDefault},
		{realm.bindings.eventPrototype, "stopPropagation", 0, nativeEventStopPropagation},
		{realm.bindings.eventPrototype, "stopImmediatePropagation", 0, nativeEventStopImmediatePropagation},
	}
	for _, method := range methods {
		name, err := newString(context, method.name)
		if err != nil {
			return err
		}
		function, err := context.NewNativeFunction(
			name, memory.RefValue(realm.active.Global), method.arity, method.id,
		)
		if err != nil {
			return err
		}
		if err := defineData(context, method.target, method.name, memory.RefValue(function), true, false, true); err != nil {
			return err
		}
	}
	accessors := []struct {
		target memory.Ref
		name   string
		getter uint64
		setter uint64
	}{
		{realm.bindings.documentPrototype, "documentElement", nativeDocumentElement, 0},
		{realm.bindings.documentPrototype, "head", nativeDocumentHead, 0},
		{realm.bindings.documentPrototype, "body", nativeDocumentBody, 0},
		{realm.bindings.documentPrototype, "defaultView", nativeDocumentDefaultView, 0},
		{realm.bindings.documentPrototype, "baseURI", nativeDocumentBaseURI, 0},
		{realm.bindings.documentPrototype, "readyState", nativeDocumentReadyState, 0},
		{realm.bindings.documentPrototype, "activeElement", nativeDocumentActiveElement, 0},
		{realm.bindings.documentPrototype, "scrollingElement", nativeDocumentScrollingElement, 0},
		{realm.bindings.nodePrototype, "nodeType", nativeNodeType, 0},
		{realm.bindings.nodePrototype, "nodeName", nativeNodeName, 0},
		{realm.bindings.nodePrototype, "parentNode", nativeNodeParent, 0},
		{realm.bindings.nodePrototype, "parentElement", nativeNodeParentElement, 0},
		{realm.bindings.nodePrototype, "ownerDocument", nativeNodeOwnerDocument, 0},
		{realm.bindings.nodePrototype, "namespaceURI", nativeNodeNamespaceURI, 0},
		{realm.bindings.nodePrototype, "prefix", nativeNodePrefix, 0},
		{realm.bindings.nodePrototype, "isConnected", nativeNodeIsConnected, 0},
		{realm.bindings.nodePrototype, "nodeValue", nativeNodeValueGet, nativeNodeValueSet},
		{realm.bindings.nodePrototype, "firstChild", nativeNodeFirstChild, 0},
		{realm.bindings.nodePrototype, "lastChild", nativeNodeLastChild, 0},
		{realm.bindings.nodePrototype, "previousSibling", nativeNodePreviousSibling, 0},
		{realm.bindings.nodePrototype, "nextSibling", nativeNodeNextSibling, 0},
		{realm.bindings.nodePrototype, "textContent", nativeNodeTextContentGet, nativeNodeTextContentSet},
		{realm.bindings.nodePrototype, "childNodes", nativeNodeChildNodes, 0},
		{realm.bindings.elementPrototype, "localName", nativeElementLocalName, 0},
		{realm.bindings.elementPrototype, "children", nativeElementChildren, 0},
		{realm.bindings.elementPrototype, "id", nativeElementIDGet, nativeElementIDSet},
		{realm.bindings.elementPrototype, "className", nativeElementClassNameGet, nativeElementClassNameSet},
		{realm.bindings.elementPrototype, "classList", nativeElementClassList, 0},
		{realm.bindings.elementPrototype, "dataset", nativeElementDataset, 0},
		{realm.bindings.elementPrototype, "innerHTML", nativeElementInnerHTMLGet, nativeElementInnerHTMLSet},
		{realm.bindings.elementPrototype, "style", nativeElementStyle, 0},
		{realm.bindings.elementPrototype, "value", nativeElementFormValueGet, nativeElementFormValueSet},
		{realm.bindings.elementPrototype, "checked", nativeElementFormCheckedGet, nativeElementFormCheckedSet},
		{realm.bindings.elementPrototype, "selected", nativeElementFormSelectedGet, nativeElementFormSelectedSet},
		{realm.bindings.elementPrototype, "selectedIndex", nativeElementFormSelectedIndexGet, nativeElementFormSelectedIndexSet},
		{realm.bindings.elementPrototype, "selectionStart", nativeElementSelectionStartGet, nativeElementSelectionStartSet},
		{realm.bindings.elementPrototype, "selectionEnd", nativeElementSelectionEndGet, nativeElementSelectionEndSet},
		{realm.bindings.elementPrototype, "selectionDirection", nativeElementSelectionDirectionGet, nativeElementSelectionDirectionSet},
		{realm.bindings.elementPrototype, "clientWidth", nativeElementClientWidth, 0},
		{realm.bindings.elementPrototype, "clientHeight", nativeElementClientHeight, 0},
		{realm.bindings.elementPrototype, "offsetWidth", nativeElementOffsetWidth, 0},
		{realm.bindings.elementPrototype, "offsetHeight", nativeElementOffsetHeight, 0},
		{realm.bindings.elementPrototype, "scrollWidth", nativeElementScrollWidth, 0},
		{realm.bindings.elementPrototype, "scrollHeight", nativeElementScrollHeight, 0},
		{realm.bindings.elementPrototype, "scrollLeft", nativeElementScrollLeftGet, nativeElementScrollLeftSet},
		{realm.bindings.elementPrototype, "scrollTop", nativeElementScrollTopGet, nativeElementScrollTopSet},
		{realm.bindings.classListPrototype, "value", nativeClassListValue, nativeElementClassNameSet},
		{realm.bindings.classListPrototype, "length", nativeClassListLength, 0},
		{realm.bindings.stylePrototype, "cssText", nativeStyleCSSTextGet, nativeStyleCSSTextSet},
		{realm.bindings.stylePrototype, "length", nativeStyleLength, 0},
		{realm.bindings.computedStylePrototype, "cssText", nativeComputedStyleCSSText, 0},
		{realm.bindings.computedStylePrototype, "length", nativeComputedStyleLength, 0},
		{realm.bindings.windowPrototype, "innerWidth", nativeWindowInnerWidth, 0},
		{realm.bindings.windowPrototype, "innerHeight", nativeWindowInnerHeight, 0},
		{realm.bindings.windowPrototype, "scrollX", nativeWindowScrollX, 0},
		{realm.bindings.windowPrototype, "scrollY", nativeWindowScrollY, 0},
		{realm.bindings.windowPrototype, "pageXOffset", nativeWindowScrollX, 0},
		{realm.bindings.windowPrototype, "pageYOffset", nativeWindowScrollY, 0},
	}
	for _, accessor := range accessors {
		getter, err := realm.newAccessorFunction(context, "get "+accessor.name, accessor.getter, 0)
		if err != nil {
			return err
		}
		setter := memory.UndefinedValue()
		if accessor.setter != 0 {
			ref, setterErr := realm.newAccessorFunction(context, "set "+accessor.name, accessor.setter, 1)
			if setterErr != nil {
				return setterErr
			}
			setter = memory.RefValue(ref)
		}
		if err := defineAccessor(context, accessor.target, accessor.name, memory.RefValue(getter), setter); err != nil {
			return err
		}
	}
	return nil
}

func (realm *Realm) newAccessorFunction(context *browserruntime.TaskContext, name string, id uint64, arity uint32) (memory.Ref, error) {
	return realm.newNativeFunction(context, name, arity, id)
}

func (realm *Realm) newNativeFunction(context *browserruntime.TaskContext, name string, arity uint32, id uint64) (memory.Ref, error) {
	nameValue, err := newString(context, name)
	if err != nil {
		return memory.Ref{}, err
	}
	return context.NewNativeFunction(nameValue, memory.RefValue(realm.active.Global), arity, id)
}

func (realm *Realm) newHostWrapperLocked(context *browserruntime.TaskContext, record memory.HostObject, prototype memory.Ref) (memory.Ref, error) {
	wrapper, err := context.NewHeapObject()
	if err != nil {
		return memory.Ref{}, err
	}
	if err := context.SetPrototype(wrapper, memory.RefValue(prototype)); err != nil {
		return memory.Ref{}, err
	}
	hostRecord, err := context.NewHostObject(record)
	if err != nil {
		return memory.Ref{}, err
	}
	if err := defineData(context, wrapper, hostRecordProperty, memory.RefValue(hostRecord), false, false, false); err != nil {
		return memory.Ref{}, err
	}
	return wrapper, nil
}

func (realm *Realm) wrapNodeLocked(context *browserruntime.TaskContext, handle browser.NodeHandle) (memory.Ref, error) {
	if realm.bindings == nil || realm.bindings.wrapperCache == (memory.Ref{}) {
		return memory.Ref{}, fmt.Errorf("nativeengine: wrapper cache is unavailable")
	}
	key, err := newString(context, nodeCacheKey(handle))
	if err != nil {
		return memory.Ref{}, err
	}
	if cached, found, err := context.MapGet(realm.bindings.wrapperCache, key); err != nil {
		return memory.Ref{}, err
	} else if found && cached.IsRef() {
		return cached.Ref(), nil
	}
	elementHost, ok := realm.host.(browser.DOMElementHost)
	if !ok {
		return memory.Ref{}, fmt.Errorf("nativeengine: browser host does not expose node metadata")
	}
	metadata, err := elementHost.NodeMetadata(handle)
	if err != nil {
		return memory.Ref{}, err
	}
	prototype := realm.bindings.nodePrototype
	switch metadata.Type {
	case browser.DOMDocumentNode:
		prototype = realm.bindings.documentPrototype
	case browser.DOMElementNode:
		prototype = realm.bindings.elementPrototype
	case browser.DOMTextNode:
		prototype = realm.bindings.textPrototype
	case browser.DOMDocumentFragmentNode:
		prototype = realm.bindings.fragmentPrototype
	}
	wrapper, err := realm.newHostWrapperLocked(context, memory.HostObject{
		Class: hostClassNode, Scope: uint64(handle.Document), Identity: uint64(handle.Node),
	}, prototype)
	if err != nil {
		return memory.Ref{}, err
	}
	if lifetime, ok := realm.host.(browser.NodeWrapperLifetimeHost); ok {
		if err := lifetime.RetainNodeWrapper(handle); err != nil {
			return memory.Ref{}, err
		}
	}
	if err := context.MapSet(realm.bindings.wrapperCache, key, memory.RefValue(wrapper)); err != nil {
		if lifetime, ok := realm.host.(browser.NodeWrapperLifetimeHost); ok {
			_ = lifetime.ReleaseNodeWrappers([]browser.NodeHandle{handle})
		}
		return memory.Ref{}, err
	}
	return wrapper, nil
}

func (realm *Realm) unwrapNode(context *browserruntime.TaskContext, value memory.Value) (browser.NodeHandle, error) {
	if !value.IsRef() {
		return browser.NodeHandle{}, fmt.Errorf("%w: DOM receiver is not an object", browserruntime.ErrOperandType)
	}
	name, err := context.NewString(hostRecordProperty)
	if err != nil {
		return browser.NodeHandle{}, err
	}
	recordValue, found, err := context.GetOwnProperty(value.Ref(), name)
	if err != nil || !found || !recordValue.IsRef() {
		return browser.NodeHandle{}, fmt.Errorf("%w: value is not a DOM wrapper", browserruntime.ErrOperandType)
	}
	record, err := context.DerefHostObject(recordValue.Ref())
	if err != nil {
		return browser.NodeHandle{}, err
	}
	if record.Class != hostClassNode {
		return browser.NodeHandle{}, fmt.Errorf("%w: value is not a Node wrapper", browserruntime.ErrOperandType)
	}
	return browser.NodeHandle{Document: browser.DocumentGeneration(record.Scope), Node: dom.NodeID(record.Identity)}, nil
}

func (realm *Realm) documentGetElementByID(context *browserruntime.TaskContext, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	id, err := stringArgument(context, arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	handle, found, err := realm.host.GetElementByID(id)
	if err != nil || !found {
		return memory.NullValue(), err
	}
	return realm.wrappedNodeValue(context, handle)
}

func (realm *Realm) documentCreateElement(context *browserruntime.TaskContext, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	name, err := stringArgument(context, arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	handle, err := realm.host.CreateElement(name)
	if err != nil {
		return memory.Value{}, err
	}
	return realm.wrappedNodeValue(context, handle)
}

func (realm *Realm) documentCreateTextNode(context *browserruntime.TaskContext, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	data, err := stringArgument(context, arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	handle, err := realm.host.CreateTextNode(data)
	if err != nil {
		return memory.Value{}, err
	}
	return realm.wrappedNodeValue(context, handle)
}

func (realm *Realm) documentCreateElementNS(context *browserruntime.TaskContext, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	namespace, err := namespaceArgument(context, arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	name, err := stringArgument(context, arguments, 1)
	if err != nil {
		return memory.Value{}, err
	}
	host, ok := realm.host.(browser.DOMDocumentHost)
	if !ok {
		return memory.Value{}, fmt.Errorf("nativeengine: browser host does not expose namespace-aware construction")
	}
	handle, err := host.CreateElementNS(namespace, name)
	if err != nil {
		return memory.Value{}, err
	}
	return realm.wrappedNodeValue(context, handle)
}

func (realm *Realm) documentCreateDocumentFragment(context *browserruntime.TaskContext, _ memory.Value, _ []memory.Value) (memory.Value, error) {
	host, ok := realm.host.(browser.DOMDocumentHost)
	if !ok {
		return memory.Value{}, fmt.Errorf("nativeengine: browser host does not expose document fragments")
	}
	handle, err := host.CreateDocumentFragment()
	if err != nil {
		return memory.Value{}, err
	}
	return realm.wrappedNodeValue(context, handle)
}

func (realm *Realm) documentDefaultView(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	if _, err := realm.unwrapNode(context, this); err != nil {
		return memory.Value{}, err
	}
	return memory.RefValue(realm.bindings.window), nil
}

func (realm *Realm) documentBaseURI(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	if _, err := realm.unwrapNode(context, this); err != nil {
		return memory.Value{}, err
	}
	metadata, err := realm.documentMetadata()
	if err != nil {
		return memory.Value{}, err
	}
	return newString(context, metadata.BaseURI)
}

func (realm *Realm) documentReadyState(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	if _, err := realm.unwrapNode(context, this); err != nil {
		return memory.Value{}, err
	}
	host, ok := realm.host.(browser.DocumentLifecycleHost)
	if !ok {
		return memory.Value{}, fmt.Errorf("nativeengine: browser host does not expose document lifecycle state")
	}
	state, err := host.DocumentReadyState()
	if err != nil {
		return memory.Value{}, err
	}
	return newString(context, state)
}

func (realm *Realm) documentMetadata() (browser.DocumentMetadata, error) {
	host, ok := realm.host.(browser.DOMDocumentHost)
	if !ok {
		return browser.DocumentMetadata{}, fmt.Errorf("nativeengine: browser host does not expose document metadata")
	}
	return host.DocumentMetadata()
}

func (realm *Realm) documentRelation(relation browser.NodeRelation) browserruntime.NativeFunction {
	return func(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
		return realm.relatedNodeValue(context, this, relation)
	}
}

func (realm *Realm) nodeRelation(relation browser.NodeRelation) browserruntime.NativeFunction {
	return func(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
		return realm.relatedNodeValue(context, this, relation)
	}
}

func (realm *Realm) relatedNodeValue(context *browserruntime.TaskContext, receiver memory.Value, relation browser.NodeRelation) (memory.Value, error) {
	handle, err := realm.unwrapNode(context, receiver)
	if err != nil {
		return memory.Value{}, err
	}
	host, ok := realm.host.(browser.DOMElementHost)
	if !ok {
		return memory.Value{}, fmt.Errorf("nativeengine: browser host does not expose node traversal")
	}
	related, found, err := host.RelatedNode(handle, relation)
	if err != nil || !found {
		return memory.NullValue(), err
	}
	return realm.wrappedNodeValue(context, related)
}

func (realm *Realm) nodeType(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	metadata, err := realm.nodeMetadata(context, this)
	return memory.NumberValue(float64(metadata.Type)), err
}

func (realm *Realm) nodeName(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	metadata, err := realm.nodeMetadata(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	return newString(context, metadata.NodeName)
}

func (realm *Realm) nodeOwnerDocument(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapNode(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	metadata, err := realm.documentMetadata()
	if err != nil {
		return memory.Value{}, err
	}
	if handle == metadata.Root {
		return memory.NullValue(), nil
	}
	return memory.RefValue(realm.bindings.document), nil
}

func (realm *Realm) nodeNamespaceURI(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	metadata, err := realm.nodeMetadata(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	if metadata.NamespaceURI == "" {
		return memory.NullValue(), nil
	}
	return newString(context, metadata.NamespaceURI)
}

func (realm *Realm) nodePrefix(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	metadata, err := realm.nodeMetadata(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	if metadata.Prefix == "" {
		return memory.NullValue(), nil
	}
	return newString(context, metadata.Prefix)
}

func (realm *Realm) nodeIsConnected(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	metadata, err := realm.nodeMetadata(context, this)
	return memory.BoolValue(metadata.Connected), err
}

func (realm *Realm) nodeValueGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapNode(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	host, ok := realm.host.(browser.DOMElementHost)
	if !ok {
		return memory.Value{}, fmt.Errorf("nativeengine: browser host does not expose node values")
	}
	value, present, err := host.NodeValue(handle)
	if err != nil || !present {
		return memory.NullValue(), err
	}
	return newString(context, value)
}

func (realm *Realm) nodeValueSet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapNode(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	value, err := stringArgument(context, arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	host, ok := realm.host.(browser.DOMElementHost)
	if !ok {
		return memory.Value{}, fmt.Errorf("nativeengine: browser host does not expose node values")
	}
	return memory.UndefinedValue(), host.SetNodeValue(handle, value)
}

func (realm *Realm) elementLocalName(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	metadata, err := realm.nodeMetadata(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	return newString(context, metadata.LocalName)
}

func (realm *Realm) nodeMetadata(context *browserruntime.TaskContext, receiver memory.Value) (browser.NodeMetadata, error) {
	handle, err := realm.unwrapNode(context, receiver)
	if err != nil {
		return browser.NodeMetadata{}, err
	}
	host, ok := realm.host.(browser.DOMElementHost)
	if !ok {
		return browser.NodeMetadata{}, fmt.Errorf("nativeengine: browser host does not expose node metadata")
	}
	return host.NodeMetadata(handle)
}

func (realm *Realm) nodeTextContentGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapNode(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	text, err := realm.host.TextContent(handle)
	if err != nil {
		return memory.Value{}, err
	}
	return newString(context, text)
}

func (realm *Realm) nodeTextContentSet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapNode(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	text, err := stringArgument(context, arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	if err := realm.host.SetTextContent(handle, text); err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), realm.refreshCollections(context, handle)
}

func (realm *Realm) nodeChildren(elementsOnly bool) browserruntime.NativeFunction {
	return func(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
		handle, err := realm.unwrapNode(context, this)
		if err != nil {
			return memory.Value{}, err
		}
		return realm.liveNodeArray(context, handle, elementsOnly)
	}
}

func (realm *Realm) nodeAppendChild(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	parent, err := realm.unwrapNode(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	childValue := argument(arguments, 0)
	child, err := realm.unwrapNode(context, childValue)
	if err != nil {
		return memory.Value{}, err
	}
	oldParent := realm.parentHandle(child)
	if err := realm.host.AppendChild(parent, child); err != nil {
		return memory.Value{}, err
	}
	return childValue, realm.refreshCollections(context, parent, oldParent, child)
}

func (realm *Realm) nodeInsertBefore(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	parent, err := realm.unwrapNode(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	childValue := argument(arguments, 0)
	child, err := realm.unwrapNode(context, childValue)
	if err != nil {
		return memory.Value{}, err
	}
	reference := browser.NodeHandle{}
	if value := argument(arguments, 1); value.Kind() != memory.ValueNull && value.Kind() != memory.ValueUndefined {
		reference, err = realm.unwrapNode(context, value)
		if err != nil {
			return memory.Value{}, err
		}
	}
	oldParent := realm.parentHandle(child)
	if err := realm.host.InsertBefore(parent, child, reference); err != nil {
		return memory.Value{}, err
	}
	return childValue, realm.refreshCollections(context, parent, oldParent, child)
}

func (realm *Realm) nodeRemoveChild(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	parent, err := realm.unwrapNode(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	childValue := argument(arguments, 0)
	child, err := realm.unwrapNode(context, childValue)
	if err != nil {
		return memory.Value{}, err
	}
	if err := realm.host.RemoveChild(parent, child); err != nil {
		return memory.Value{}, err
	}
	return childValue, realm.refreshCollections(context, parent)
}

func (realm *Realm) nodeContains(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapNode(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	other, err := realm.unwrapNode(context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	host, ok := realm.host.(browser.DOMElementHost)
	if !ok {
		return memory.Value{}, fmt.Errorf("nativeengine: browser host does not expose containment")
	}
	contains, err := host.Contains(handle, other)
	return memory.BoolValue(contains), err
}

func (realm *Realm) nodeCloneNode(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapNode(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	host, ok := realm.host.(browser.DOMElementHost)
	if !ok {
		return memory.Value{}, fmt.Errorf("nativeengine: browser host does not expose cloning")
	}
	clone, err := host.CloneNode(handle, truthy(argument(arguments, 0)))
	if err != nil {
		return memory.Value{}, err
	}
	return realm.wrappedNodeValue(context, clone)
}

func (realm *Realm) nodeReplaceChild(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	parent, err := realm.unwrapNode(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	child, err := realm.unwrapNode(context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	replacedValue := argument(arguments, 1)
	replaced, err := realm.unwrapNode(context, replacedValue)
	if err != nil {
		return memory.Value{}, err
	}
	host, ok := realm.host.(browser.DOMElementHost)
	if !ok {
		return memory.Value{}, fmt.Errorf("nativeengine: browser host does not expose child replacement")
	}
	oldParent := realm.parentHandle(child)
	if err := host.ReplaceChild(parent, child, replaced); err != nil {
		return memory.Value{}, err
	}
	return replacedValue, realm.refreshCollections(context, parent, oldParent, child)
}

func (realm *Realm) nodeNormalize(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapNode(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	host, ok := realm.host.(browser.DOMElementHost)
	if !ok {
		return memory.Value{}, fmt.Errorf("nativeengine: browser host does not expose node normalization")
	}
	if err := host.Normalize(handle); err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), realm.refreshCollections(context, handle)
}

func (realm *Realm) textSplitText(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapNode(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	offset, err := integerArgument(arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	host, ok := realm.host.(browser.DOMElementHost)
	if !ok {
		return memory.Value{}, fmt.Errorf("nativeengine: browser host does not expose text splitting")
	}
	result, err := host.SplitText(handle, offset)
	if err != nil {
		return memory.Value{}, err
	}
	parent := realm.parentHandle(handle)
	if err := realm.refreshCollections(context, parent); err != nil {
		return memory.Value{}, err
	}
	return realm.wrappedNodeValue(context, result)
}

func (realm *Realm) parentHandle(handle browser.NodeHandle) browser.NodeHandle {
	host, ok := realm.host.(browser.DOMElementHost)
	if !ok {
		return browser.NodeHandle{}
	}
	parent, found, err := host.RelatedNode(handle, browser.RelationParentNode)
	if err != nil || !found {
		return browser.NodeHandle{}
	}
	return parent
}

func (realm *Realm) elementGetAttribute(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, name, err := realm.attributeOperands(context, this, arguments)
	if err != nil {
		return memory.Value{}, err
	}
	value, found, err := realm.host.GetAttribute(handle, name)
	if err != nil || !found {
		return memory.NullValue(), err
	}
	return newString(context, value)
}

func (realm *Realm) elementSetAttribute(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, name, err := realm.attributeOperands(context, this, arguments)
	if err != nil {
		return memory.Value{}, err
	}
	value, err := stringArgument(context, arguments, 1)
	if err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), realm.host.SetAttribute(handle, name, value)
}

func (realm *Realm) elementRemoveAttribute(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, name, err := realm.attributeOperands(context, this, arguments)
	if err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), realm.host.RemoveAttribute(handle, name)
}

func (realm *Realm) elementHasAttribute(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, name, err := realm.attributeOperands(context, this, arguments)
	if err != nil {
		return memory.Value{}, err
	}
	host, ok := realm.host.(browser.DOMElementHost)
	if !ok {
		_, found, lookupErr := realm.host.GetAttribute(handle, name)
		return memory.BoolValue(found), lookupErr
	}
	found, err := host.HasAttribute(handle, name)
	return memory.BoolValue(found), err
}

func (realm *Realm) elementIDGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapNode(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	value, _, err := realm.host.GetAttribute(handle, "id")
	if err != nil {
		return memory.Value{}, err
	}
	return newString(context, value)
}

func (realm *Realm) elementIDSet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapNode(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	value, err := stringArgument(context, arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), realm.host.SetAttribute(handle, "id", value)
}

func (realm *Realm) elementQuerySelector(all bool) browserruntime.NativeFunction {
	return func(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
		handle, err := realm.unwrapNode(context, this)
		if err != nil {
			return memory.Value{}, err
		}
		selector, err := stringArgument(context, arguments, 0)
		if err != nil {
			return memory.Value{}, err
		}
		host, ok := realm.host.(browser.DOMElementHost)
		if !ok {
			return memory.Value{}, fmt.Errorf("nativeengine: browser host does not expose selectors")
		}
		handles, err := host.QuerySelector(handle, selector, all)
		if err != nil {
			return memory.Value{}, err
		}
		if all {
			return realm.nodeArray(context, handles)
		}
		if len(handles) == 0 {
			return memory.NullValue(), nil
		}
		return realm.wrappedNodeValue(context, handles[0])
	}
}

func (realm *Realm) elementMatches(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapNode(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	selector, err := stringArgument(context, arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	host, ok := realm.host.(browser.DOMElementHost)
	if !ok {
		return memory.Value{}, fmt.Errorf("nativeengine: browser host does not expose selectors")
	}
	matches, err := host.MatchesSelector(handle, selector)
	return memory.BoolValue(matches), err
}

func (realm *Realm) elementClosest(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapNode(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	selector, err := stringArgument(context, arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	host, ok := realm.host.(browser.DOMElementHost)
	if !ok {
		return memory.Value{}, fmt.Errorf("nativeengine: browser host does not expose selectors")
	}
	closest, found, err := host.ClosestSelector(handle, selector)
	if err != nil || !found {
		return memory.NullValue(), err
	}
	return realm.wrappedNodeValue(context, closest)
}

func (realm *Realm) attributeOperands(context *browserruntime.TaskContext, receiver memory.Value, arguments []memory.Value) (browser.NodeHandle, string, error) {
	handle, err := realm.unwrapNode(context, receiver)
	if err != nil {
		return browser.NodeHandle{}, "", err
	}
	name, err := stringArgument(context, arguments, 0)
	return handle, name, err
}

func (realm *Realm) nodeArray(context *browserruntime.TaskContext, handles []browser.NodeHandle) (memory.Value, error) {
	array, err := context.NewArray(uint32(len(handles)))
	if err != nil {
		return memory.Value{}, err
	}
	for index, handle := range handles {
		value, err := realm.wrappedNodeValue(context, handle)
		if err != nil {
			return memory.Value{}, err
		}
		if err := context.SetArrayElement(array, uint32(index), value); err != nil {
			return memory.Value{}, err
		}
	}
	return memory.RefValue(array), nil
}

func (realm *Realm) wrappedNodeValue(context *browserruntime.TaskContext, handle browser.NodeHandle) (memory.Value, error) {
	wrapper, err := realm.wrapNodeLocked(context, handle)
	return memory.RefValue(wrapper), err
}

func globalRef(context *browserruntime.TaskContext, global memory.Ref, name string) (memory.Ref, bool, error) {
	nameRef, err := context.NewString(name)
	if err != nil {
		return memory.Ref{}, false, err
	}
	value, found, err := context.ResolveBinding(global, nameRef)
	if err != nil || !found {
		return memory.Ref{}, found, err
	}
	if !value.IsRef() {
		return memory.Ref{}, false, fmt.Errorf("nativeengine: browser binding %q is not a Ref", name)
	}
	return value.Ref(), true, nil
}

func declareGlobal(context *browserruntime.TaskContext, global memory.Ref, name string, value memory.Value, mutable bool) error {
	nameRef, err := context.NewString(name)
	if err != nil {
		return err
	}
	if err := context.DeclareBinding(global, nameRef, mutable); err != nil {
		return err
	}
	return context.InitializeBinding(global, nameRef, value)
}

func defineData(context *browserruntime.TaskContext, object memory.Ref, name string, value memory.Value, writable, enumerable, configurable bool) error {
	nameRef, err := context.NewString(name)
	if err != nil {
		return err
	}
	return context.DefineProperty(object, nameRef, memory.DataProperty(value, writable, enumerable, configurable))
}

func defineAccessor(context *browserruntime.TaskContext, object memory.Ref, name string, getter, setter memory.Value) error {
	nameRef, err := context.NewString(name)
	if err != nil {
		return err
	}
	return context.DefineProperty(object, nameRef, memory.AccessorProperty(getter, setter, false, true))
}

func newString(context *browserruntime.TaskContext, text string) (memory.Value, error) {
	ref, err := context.NewString(text)
	return memory.RefValue(ref), err
}

func stringArgument(context *browserruntime.TaskContext, arguments []memory.Value, index int) (string, error) {
	return valueString(context, argument(arguments, index))
}

func namespaceArgument(context *browserruntime.TaskContext, arguments []memory.Value, index int) (string, error) {
	value := argument(arguments, index)
	if value.Kind() == memory.ValueNull || value.Kind() == memory.ValueUndefined {
		return "", nil
	}
	return valueString(context, value)
}

func integerArgument(arguments []memory.Value, index int) (int, error) {
	value := argument(arguments, index)
	if value.Kind() != memory.ValueNumber || math.IsNaN(value.Number()) || math.IsInf(value.Number(), 0) {
		return 0, fmt.Errorf("%w: argument is not a finite integer", browserruntime.ErrOperandType)
	}
	number := math.Trunc(value.Number())
	if number > float64(math.MaxInt) || number < float64(math.MinInt) {
		return 0, fmt.Errorf("%w: integer argument is out of range", browserruntime.ErrOperandType)
	}
	return int(number), nil
}

func valueString(context *browserruntime.TaskContext, value memory.Value) (string, error) {
	switch value.Kind() {
	case memory.ValueUndefined:
		return "undefined", nil
	case memory.ValueNull:
		return "null", nil
	case memory.ValueBool:
		return strconv.FormatBool(value.Bool()), nil
	case memory.ValueNumber:
		return strconv.FormatFloat(value.Number(), 'g', -1, 64), nil
	case memory.ValueReference:
		if kind, err := context.HeapKind(value.Ref()); err != nil {
			return "", err
		} else if kind == memory.HeapString {
			return context.DerefString(value.Ref())
		}
	}
	return "", fmt.Errorf("%w: value cannot be converted to a DOMString", browserruntime.ErrOperandType)
}

func argument(arguments []memory.Value, index int) memory.Value {
	if index < 0 || index >= len(arguments) {
		return memory.UndefinedValue()
	}
	return arguments[index]
}

func truthy(value memory.Value) bool {
	switch value.Kind() {
	case memory.ValueUndefined, memory.ValueNull:
		return false
	case memory.ValueBool:
		return value.Bool()
	case memory.ValueNumber:
		return value.Number() != 0
	default:
		return true
	}
}

func nodeCacheKey(handle browser.NodeHandle) string {
	return fmt.Sprintf("%d:%d", handle.Document, handle.Node)
}
