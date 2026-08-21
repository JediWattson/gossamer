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
	nativeDOMInterfaceConstructor
	nativeDocumentImportNode
	nativeNodeRemove
	nativeTemplateContent
	nativeElementHiddenGet
	nativeElementHiddenSet
	nativeModuleDynamicImport
	nativeDocumentGetElementsByTagName
	nativeModuleImportMetaResolve
	nativeDocumentCreateRange
	nativeDocumentGetSelection
	nativeRangeStartContainer
	nativeRangeStartOffset
	nativeRangeEndContainer
	nativeRangeEndOffset
	nativeRangeCollapsed
	nativeRangeCommonAncestor
	nativeRangeSetStart
	nativeRangeSetEnd
	nativeRangeSelectNode
	nativeRangeSelectNodeContents
	nativeRangeCollapse
	nativeRangeCloneRange
	nativeRangeCloneContents
	nativeRangeExtractContents
	nativeRangeDeleteContents
	nativeRangeInsertNode
	nativeRangeDetach
	nativeSelectionAnchorNode
	nativeSelectionAnchorOffset
	nativeSelectionFocusNode
	nativeSelectionFocusOffset
	nativeSelectionIsCollapsed
	nativeSelectionRangeCount
	nativeSelectionType
	nativeSelectionGetRangeAt
	nativeSelectionAddRange
	nativeSelectionRemoveAllRanges
	nativeSelectionCollapse
	nativeSelectionCollapseToStart
	nativeSelectionCollapseToEnd
	nativeSelectionSelectAllChildren
	nativeSelectionDeleteFromDocument
	nativeSelectionToString
	nativeNodeFirstElementChild
	nativeNodeLastElementChild
	nativeNodePreviousElementSibling
	nativeNodeNextElementSibling
)

const (
	hostClassWindow memory.HostClass = iota + 1
	hostClassNode
	hostClassClassList
	hostClassDataset
	hostClassStyle
	hostClassComputedStyle
	hostClassMutationObserver
	hostClassFormElements
	hostClassSelectOptions
)

const (
	bindingWindowPrototype              = "\x00gossamer.window.prototype"
	bindingDocumentPrototype            = "\x00gossamer.document.prototype"
	bindingNodePrototype                = "\x00gossamer.node.prototype"
	bindingElementPrototype             = "\x00gossamer.element.prototype"
	bindingHTMLElementPrototype         = "\x00gossamer.html-element.prototype"
	bindingHTMLFormElementPrototype     = "\x00gossamer.html-form-element.prototype"
	bindingHTMLInputElementPrototype    = "\x00gossamer.html-input-element.prototype"
	bindingHTMLTextAreaElementPrototype = "\x00gossamer.html-text-area-element.prototype"
	bindingHTMLSelectElementPrototype   = "\x00gossamer.html-select-element.prototype"
	bindingHTMLOptionElementPrototype   = "\x00gossamer.html-option-element.prototype"
	bindingHTMLButtonElementPrototype   = "\x00gossamer.html-button-element.prototype"
	bindingHTMLIFrameElementPrototype   = "\x00gossamer.html-iframe-element.prototype"
	bindingHTMLHeadElementPrototype     = "\x00gossamer.html-head-element.prototype"
	bindingHTMLScriptElementPrototype   = "\x00gossamer.html-script-element.prototype"
	bindingHTMLMediaElementPrototype    = "\x00gossamer.html-media-element.prototype"
	bindingHTMLImageElementPrototype    = "\x00gossamer.html-image-element.prototype"
	bindingHTMLCollectionPrototype      = "\x00gossamer.html-collection.prototype"
	bindingTemplatePrototype            = "\x00gossamer.template.prototype"
	bindingTextPrototype                = "\x00gossamer.text.prototype"
	bindingFragmentPrototype            = "\x00gossamer.fragment.prototype"
	bindingEventPrototype               = "\x00gossamer.event.prototype"
	bindingClassListPrototype           = "\x00gossamer.class-list.prototype"
	bindingDatasetPrototype             = "\x00gossamer.dataset.prototype"
	bindingStylePrototype               = "\x00gossamer.style.prototype"
	bindingComputedStylePrototype       = "\x00gossamer.computed-style.prototype"
	bindingDOMRectPrototype             = "\x00gossamer.dom-rect.prototype"
	bindingMutationObserverPrototype    = "\x00gossamer.mutation-observer.prototype"
	bindingMutationObserverConstructor  = "\x00gossamer.mutation-observer.constructor"
	bindingDOMException                 = "DOMException"
	bindingWrapperCache                 = "\x00gossamer.wrapper.cache"
	bindingCallbackCache                = "\x00gossamer.callback.cache"
	bindingFacadeCache                  = "\x00gossamer.facade.cache"
	bindingCollectionCache              = "\x00gossamer.collection.cache"
	bindingObserverCache                = "\x00gossamer.observer.cache"
	bindingModuleCache                  = "\x00gossamer.module.cache"
	bindingWindow                       = "window"
	bindingSelf                         = "self"
	bindingDocument                     = "document"
	bindingPerformance                  = "performance"
	bindingMutationObserver             = "MutationObserver"
	bindingRangePrototype               = "\x00gossamer.range.prototype"
	bindingSelectionPrototype           = "\x00gossamer.selection.prototype"
	bindingSelection                    = "\x00gossamer.selection"
	bindingHeadersPrototype             = "\x00gossamer.headers.prototype"
	bindingHeadersConstructor           = "\x00gossamer.headers.constructor"
	bindingRequestPrototype             = "\x00gossamer.request.prototype"
	bindingRequestConstructor           = "\x00gossamer.request.constructor"
	bindingResponsePrototype            = "\x00gossamer.response.prototype"
	bindingResponseConstructor          = "\x00gossamer.response.constructor"
	bindingStoragePrototype             = "\x00gossamer.storage.prototype"
	bindingStorageConstructor           = "\x00gossamer.storage.constructor"
	bindingLocalStorage                 = "\x00gossamer.local-storage"
	bindingSessionStorage               = "\x00gossamer.session-storage"
	hostRecordProperty                  = "\x00gossamer.host.record"
)

type browserBindings struct {
	windowPrototype              memory.Ref
	documentPrototype            memory.Ref
	nodePrototype                memory.Ref
	elementPrototype             memory.Ref
	htmlElementPrototype         memory.Ref
	htmlFormElementPrototype     memory.Ref
	htmlInputElementPrototype    memory.Ref
	htmlTextAreaElementPrototype memory.Ref
	htmlSelectElementPrototype   memory.Ref
	htmlOptionElementPrototype   memory.Ref
	htmlButtonElementPrototype   memory.Ref
	htmlIFrameElementPrototype   memory.Ref
	htmlHeadElementPrototype     memory.Ref
	htmlScriptElementPrototype   memory.Ref
	htmlMediaElementPrototype    memory.Ref
	htmlImageElementPrototype    memory.Ref
	htmlCollectionPrototype      memory.Ref
	templatePrototype            memory.Ref
	textPrototype                memory.Ref
	fragmentPrototype            memory.Ref
	eventPrototype               memory.Ref
	classListPrototype           memory.Ref
	datasetPrototype             memory.Ref
	stylePrototype               memory.Ref
	computedStylePrototype       memory.Ref
	domRectPrototype             memory.Ref
	mutationObserverPrototype    memory.Ref
	mutationObserverConstructor  memory.Ref
	domExceptionPrototype        memory.Ref
	domExceptionConstructor      memory.Ref
	urlSearchParamsPrototype     memory.Ref
	urlSearchParamsConstructor   memory.Ref
	urlPrototype                 memory.Ref
	urlConstructor               memory.Ref
	textEncoderPrototype         memory.Ref
	textEncoderConstructor       memory.Ref
	textDecoderPrototype         memory.Ref
	textDecoderConstructor       memory.Ref
	uint8ArrayPrototype          memory.Ref
	uint8ArrayConstructor        memory.Ref
	headersPrototype             memory.Ref
	headersConstructor           memory.Ref
	requestPrototype             memory.Ref
	requestConstructor           memory.Ref
	responsePrototype            memory.Ref
	responseConstructor          memory.Ref
	storagePrototype             memory.Ref
	storageConstructor           memory.Ref
	localStorage                 memory.Ref
	sessionStorage               memory.Ref
	rangePrototype               memory.Ref
	selectionPrototype           memory.Ref
	selection                    memory.Ref
	wrapperCache                 memory.Ref
	callbackCache                memory.Ref
	facadeCache                  memory.Ref
	collectionCache              memory.Ref
	observerCache                memory.Ref
	moduleCache                  memory.Ref
	window                       memory.Ref
	document                     memory.Ref
	performance                  memory.Ref
}

type nativeRegistration struct {
	id       uint64
	callback browserruntime.NativeFunction
}

func (realm *Realm) installBrowserNatives() error {
	registrations := []nativeRegistration{
		{nativeDocumentGetElementByID, realm.documentGetElementByID},
		{nativeDocumentGetElementsByTagName, realm.documentGetElementsByTagName},
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
		{nativeElementHiddenGet, realm.elementHiddenGet},
		{nativeElementHiddenSet, realm.elementHiddenSet},
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
		{nativeDOMInterfaceConstructor, realm.domInterfaceConstructor},
		{nativeDocumentImportNode, realm.documentImportNode},
		{nativeNodeRemove, realm.nodeRemove},
		{nativeTemplateContent, realm.templateContent},
		{nativeGlobalSetTimeout, realm.globalSetTimeout},
		{nativeGlobalClearTimeout, realm.globalClearTimeout},
		{nativeGlobalSetInterval, realm.globalSetInterval},
		{nativeGlobalClearInterval, realm.globalClearInterval},
		{nativeGlobalRequestAnimationFrame, realm.globalRequestAnimationFrame},
		{nativeGlobalCancelAnimationFrame, realm.globalCancelAnimationFrame},
		{nativePerformanceNow, realm.performanceNow},
		{nativeMutationObserverConstructor, realm.mutationObserverConstructor},
		{nativeMutationObserverObserve, realm.mutationObserverObserve},
		{nativeMutationObserverDisconnect, realm.mutationObserverDisconnect},
		{nativeMutationObserverTakeRecords, realm.mutationObserverTakeRecords},
		{nativeEventTargetAdd, realm.eventTargetAdd},
		{nativeEventTargetRemove, realm.eventTargetRemove},
		{nativeEventTargetDispatch, realm.eventTargetDispatch},
		{nativeEventConstructor, realm.eventConstructor},
		{nativeCustomEventConstructor, realm.customEventConstructor},
		{nativeEventPreventDefault, realm.eventPreventDefault},
		{nativeEventStopPropagation, realm.eventStopPropagation},
		{nativeEventStopImmediatePropagation, realm.eventStopImmediatePropagation},
		{nativeModuleDynamicImport, realm.moduleDynamicImport},
		{nativeModuleImportMetaResolve, realm.moduleImportMetaResolve},
		{nativeDocumentCreateRange, realm.documentCreateRange},
		{nativeDocumentGetSelection, realm.documentGetSelection},
		{nativeRangeStartContainer, realm.rangeStartContainer},
		{nativeRangeStartOffset, realm.rangeStartOffset},
		{nativeRangeEndContainer, realm.rangeEndContainer},
		{nativeRangeEndOffset, realm.rangeEndOffset},
		{nativeRangeCollapsed, realm.rangeCollapsed},
		{nativeRangeCommonAncestor, realm.rangeCommonAncestor},
		{nativeRangeSetStart, realm.rangeSetStart},
		{nativeRangeSetEnd, realm.rangeSetEnd},
		{nativeRangeSelectNode, realm.rangeSelectNode},
		{nativeRangeSelectNodeContents, realm.rangeSelectNodeContents},
		{nativeRangeCollapse, realm.rangeCollapse},
		{nativeRangeCloneRange, realm.rangeCloneRange},
		{nativeRangeCloneContents, realm.rangeCloneContents},
		{nativeRangeExtractContents, realm.rangeExtractContents},
		{nativeRangeDeleteContents, realm.rangeDeleteContents},
		{nativeRangeInsertNode, realm.rangeInsertNode},
		{nativeRangeDetach, realm.rangeDetach},
		{nativeSelectionAnchorNode, realm.selectionAnchorNode},
		{nativeSelectionAnchorOffset, realm.selectionAnchorOffset},
		{nativeSelectionFocusNode, realm.selectionFocusNode},
		{nativeSelectionFocusOffset, realm.selectionFocusOffset},
		{nativeSelectionIsCollapsed, realm.selectionIsCollapsed},
		{nativeSelectionRangeCount, realm.selectionRangeCount},
		{nativeSelectionType, realm.selectionType},
		{nativeSelectionGetRangeAt, realm.selectionGetRangeAt},
		{nativeSelectionAddRange, realm.selectionAddRange},
		{nativeSelectionRemoveAllRanges, realm.selectionRemoveAllRanges},
		{nativeSelectionCollapse, realm.selectionCollapse},
		{nativeSelectionCollapseToStart, realm.selectionCollapseToStart},
		{nativeSelectionCollapseToEnd, realm.selectionCollapseToEnd},
		{nativeSelectionSelectAllChildren, realm.selectionSelectAllChildren},
		{nativeSelectionDeleteFromDocument, realm.selectionDeleteFromDocument},
		{nativeSelectionToString, realm.selectionToString},
		{nativeNodeFirstElementChild, realm.nodeRelation(browser.RelationFirstElementChild)},
		{nativeNodeLastElementChild, realm.nodeRelation(browser.RelationLastElementChild)},
		{nativeNodePreviousElementSibling, realm.nodeRelation(browser.RelationPreviousElementSibling)},
		{nativeNodeNextElementSibling, realm.nodeRelation(browser.RelationNextElementSibling)},
		{nativeDOMExceptionConstructor, realm.domExceptionConstructor},
		{nativeDOMExceptionToString, realm.domExceptionToString},
		{nativeURLSearchParamsConstructor, realm.urlSearchParamsConstructor},
		{nativeURLSearchParamsAppend, realm.urlSearchParamsAppend},
		{nativeURLSearchParamsDelete, realm.urlSearchParamsDelete},
		{nativeURLSearchParamsGet, realm.urlSearchParamsGet},
		{nativeURLSearchParamsGetAll, realm.urlSearchParamsGetAll},
		{nativeURLSearchParamsHas, realm.urlSearchParamsHas},
		{nativeURLSearchParamsSet, realm.urlSearchParamsSet},
		{nativeURLSearchParamsSort, realm.urlSearchParamsSort},
		{nativeURLSearchParamsToString, realm.urlSearchParamsToString},
		{nativeURLSearchParamsKeys, realm.urlSearchParamsKeys},
		{nativeURLSearchParamsValues, realm.urlSearchParamsValues},
		{nativeURLSearchParamsEntries, realm.urlSearchParamsEntries},
		{nativeURLSearchParamsForEach, realm.urlSearchParamsForEach},
		{nativeURLSearchParamsSize, realm.urlSearchParamsSize},
		{nativeURLConstructor, realm.urlConstructor},
		{nativeURLCanParse, realm.urlCanParse},
		{nativeURLToString, realm.urlToString},
		{nativeURLToJSON, realm.urlToJSON},
		{nativeURLHrefGet, realm.urlHrefGet},
		{nativeURLHrefSet, realm.urlHrefSet},
		{nativeURLOrigin, realm.urlOrigin},
		{nativeURLProtocolGet, realm.urlProtocolGet},
		{nativeURLProtocolSet, realm.urlProtocolSet},
		{nativeURLUsernameGet, realm.urlUsernameGet},
		{nativeURLUsernameSet, realm.urlUsernameSet},
		{nativeURLPasswordGet, realm.urlPasswordGet},
		{nativeURLPasswordSet, realm.urlPasswordSet},
		{nativeURLHostGet, realm.urlHostGet},
		{nativeURLHostSet, realm.urlHostSet},
		{nativeURLHostnameGet, realm.urlHostnameGet},
		{nativeURLHostnameSet, realm.urlHostnameSet},
		{nativeURLPortGet, realm.urlPortGet},
		{nativeURLPortSet, realm.urlPortSet},
		{nativeURLPathnameGet, realm.urlPathnameGet},
		{nativeURLPathnameSet, realm.urlPathnameSet},
		{nativeURLSearchGet, realm.urlSearchGet},
		{nativeURLSearchSet, realm.urlSearchSet},
		{nativeURLSearchParams, realm.urlSearchParams},
		{nativeURLHashGet, realm.urlHashGet},
		{nativeURLHashSet, realm.urlHashSet},
		{nativeTextEncoderConstructor, realm.textEncoderConstructor},
		{nativeTextEncoderEncode, realm.textEncoderEncode},
		{nativeTextEncoderEncodeInto, realm.textEncoderEncodeInto},
		{nativeTextEncoderEncoding, realm.textEncoderEncoding},
		{nativeTextDecoderConstructor, realm.textDecoderConstructor},
		{nativeTextDecoderDecode, realm.textDecoderDecode},
		{nativeTextDecoderEncoding, realm.textDecoderEncoding},
		{nativeTextDecoderFatal, realm.textDecoderFatal},
		{nativeTextDecoderIgnoreBOM, realm.textDecoderIgnoreBOM},
		{nativeUint8ArrayConstructor, realm.uint8ArrayConstructor},
		{nativeUint8ArrayFrom, realm.uint8ArrayFrom},
		{nativeUint8ArraySet, realm.uint8ArraySet},
		{nativeUint8ArraySlice, realm.uint8ArraySlice},
		{nativeUint8ArraySubarray, realm.uint8ArraySubarray},
		{nativeUint8ArrayFill, realm.uint8ArrayFill},
		{nativeMatchMedia, realm.matchMedia},
		{nativeMediaQueryNoop, realm.mediaQueryNoop},
		{nativeMediaQueryDispatch, realm.mediaQueryDispatch},
		{nativeImageConstructor, realm.imageConstructor},
		{nativeGlobalFetch, realm.globalFetch},
		{nativeHeadersConstructor, realm.headersConstructor},
		{nativeHeadersAppend, realm.headersAppend},
		{nativeHeadersDelete, realm.headersDelete},
		{nativeHeadersGet, realm.headersGet},
		{nativeHeadersHas, realm.headersHas},
		{nativeHeadersSet, realm.headersSet},
		{nativeHeadersForEach, realm.headersForEach},
		{nativeRequestConstructor, realm.requestConstructor},
		{nativeResponseConstructor, realm.responseConstructor},
		{nativeResponseText, realm.responseText},
		{nativeResponseJSON, realm.responseJSON},
		{nativeResponseArrayBuffer, realm.responseArrayBuffer},
		{nativeResponseClone, realm.responseClone},
		{nativeStorageGetItem, realm.storageGetItem},
		{nativeStorageSetItem, realm.storageSetItem},
		{nativeStorageRemoveItem, realm.storageRemoveItem},
		{nativeStorageClear, realm.storageClear},
		{nativeStorageKey, realm.storageKey},
		{nativeStorageLength, realm.storageLength},
		{nativeDocumentCookieGet, realm.documentCookieGet},
		{nativeDocumentCookieSet, realm.documentCookieSet},
		{nativeNodeAppend, realm.nodeConvenienceMutation(dom.MutationAppend)},
		{nativeNodePrepend, realm.nodeConvenienceMutation(dom.MutationPrepend)},
		{nativeNodeBefore, realm.nodeConvenienceMutation(dom.MutationBefore)},
		{nativeNodeAfter, realm.nodeConvenienceMutation(dom.MutationAfter)},
		{nativeNodeReplaceWith, realm.nodeConvenienceMutation(dom.MutationReplaceWith)},
		{nativeNodeReplaceChildren, realm.nodeConvenienceMutation(dom.MutationReplaceChildren)},
		{nativeElementFormOwner, realm.elementFormOwner},
		{nativeElementFormElements, realm.elementFormCollection(dom.FormElementCollection)},
		{nativeElementSelectOptions, realm.elementFormCollection(dom.SelectOptionCollection)},
		{nativeHTMLCollectionItem, realm.htmlCollectionItem},
		{nativeHTMLCollectionNamedItem, realm.htmlCollectionNamedItem},
		{nativeHTMLFormElementReset, realm.htmlFormReset},
		{nativeElementDefaultCheckedGet, realm.elementReflectedBoolean("checked", false)},
		{nativeElementDefaultCheckedSet, realm.elementReflectedBoolean("checked", true)},
		{nativeElementDefaultSelectedGet, realm.elementReflectedBoolean("selected", false)},
		{nativeElementDefaultSelectedSet, realm.elementReflectedBoolean("selected", true)},
		{nativeElementFormIndeterminateGet, realm.elementFormIndeterminateGet},
		{nativeElementFormIndeterminateSet, realm.elementFormIndeterminateSet},
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
			{bindingHTMLElementPrototype, &bindings.htmlElementPrototype},
			{bindingHTMLFormElementPrototype, &bindings.htmlFormElementPrototype},
			{bindingHTMLInputElementPrototype, &bindings.htmlInputElementPrototype},
			{bindingHTMLTextAreaElementPrototype, &bindings.htmlTextAreaElementPrototype},
			{bindingHTMLSelectElementPrototype, &bindings.htmlSelectElementPrototype},
			{bindingHTMLOptionElementPrototype, &bindings.htmlOptionElementPrototype},
			{bindingHTMLButtonElementPrototype, &bindings.htmlButtonElementPrototype},
			{bindingHTMLIFrameElementPrototype, &bindings.htmlIFrameElementPrototype},
			{bindingHTMLHeadElementPrototype, &bindings.htmlHeadElementPrototype},
			{bindingHTMLScriptElementPrototype, &bindings.htmlScriptElementPrototype},
			{bindingHTMLMediaElementPrototype, &bindings.htmlMediaElementPrototype},
			{bindingHTMLImageElementPrototype, &bindings.htmlImageElementPrototype},
			{bindingHTMLCollectionPrototype, &bindings.htmlCollectionPrototype},
			{bindingTemplatePrototype, &bindings.templatePrototype},
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
			{bindingDOMExceptionPrototype, &bindings.domExceptionPrototype},
			{bindingDOMExceptionConstructor, &bindings.domExceptionConstructor},
			{bindingURLSearchParamsPrototype, &bindings.urlSearchParamsPrototype},
			{bindingURLSearchParamsConstructor, &bindings.urlSearchParamsConstructor},
			{bindingURLPrototype, &bindings.urlPrototype},
			{bindingURLConstructor, &bindings.urlConstructor},
			{bindingTextEncoderPrototype, &bindings.textEncoderPrototype},
			{bindingTextEncoderConstructor, &bindings.textEncoderConstructor},
			{bindingTextDecoderPrototype, &bindings.textDecoderPrototype},
			{bindingTextDecoderConstructor, &bindings.textDecoderConstructor},
			{bindingUint8ArrayPrototype, &bindings.uint8ArrayPrototype},
			{bindingUint8ArrayConstructor, &bindings.uint8ArrayConstructor},
			{bindingHeadersPrototype, &bindings.headersPrototype},
			{bindingHeadersConstructor, &bindings.headersConstructor},
			{bindingRequestPrototype, &bindings.requestPrototype},
			{bindingRequestConstructor, &bindings.requestConstructor},
			{bindingResponsePrototype, &bindings.responsePrototype},
			{bindingResponseConstructor, &bindings.responseConstructor},
			{bindingStoragePrototype, &bindings.storagePrototype},
			{bindingStorageConstructor, &bindings.storageConstructor},
			{bindingLocalStorage, &bindings.localStorage},
			{bindingSessionStorage, &bindings.sessionStorage},
			{bindingRangePrototype, &bindings.rangePrototype},
			{bindingSelectionPrototype, &bindings.selectionPrototype},
			{bindingSelection, &bindings.selection},
			{bindingWrapperCache, &bindings.wrapperCache},
			{bindingCallbackCache, &bindings.callbackCache},
			{bindingFacadeCache, &bindings.facadeCache},
			{bindingCollectionCache, &bindings.collectionCache},
			{bindingObserverCache, &bindings.observerCache},
			{bindingModuleCache, &bindings.moduleCache},
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
	domInterfaces, err := realm.newDOMInterfaces(context, bindings)
	if err != nil {
		return err
	}
	eventConstructor, eventPrototype, err := realm.newEventConstructor(context, "Event", nativeEventConstructor, memory.Ref{})
	if err != nil {
		return err
	}
	bindings.eventPrototype = eventPrototype
	customEventConstructor, _, err := realm.newEventConstructor(context, "CustomEvent", nativeCustomEventConstructor, eventPrototype)
	if err != nil {
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
	bindings.moduleCache, err = context.NewMap()
	if err != nil {
		return err
	}
	bindings.mutationObserverConstructor, bindings.mutationObserverPrototype, err = realm.newMutationObserverConstructor(context)
	if err != nil {
		return err
	}
	bindings.domExceptionConstructor, bindings.domExceptionPrototype, err = realm.newDOMExceptionConstructor(context)
	if err != nil {
		return err
	}
	bindings.urlSearchParamsConstructor, bindings.urlSearchParamsPrototype, err = realm.newURLSearchParamsConstructor(context)
	if err != nil {
		return err
	}
	bindings.urlConstructor, bindings.urlPrototype, err = realm.newURLConstructor(context)
	if err != nil {
		return err
	}
	bindings.textEncoderConstructor, bindings.textEncoderPrototype,
		bindings.textDecoderConstructor, bindings.textDecoderPrototype, err = realm.newTextCodecConstructors(context)
	if err != nil {
		return err
	}
	bindings.uint8ArrayConstructor, bindings.uint8ArrayPrototype, err = realm.newUint8ArrayConstructor(context)
	if err != nil {
		return err
	}
	bindings.headersConstructor, bindings.headersPrototype,
		bindings.requestConstructor, bindings.requestPrototype,
		bindings.responseConstructor, bindings.responsePrototype, err = realm.newFetchConstructors(context)
	if err != nil {
		return err
	}
	bindings.storageConstructor, bindings.storagePrototype, bindings.localStorage, bindings.sessionStorage, err = realm.newStorageBindings(context)
	if err != nil {
		return err
	}
	rangeConstructor, err := realm.newDOMInterfaceConstructor(context, "Range", realm.active.ObjectPrototype)
	if err != nil {
		return err
	}
	bindings.rangePrototype, err = constructorPrototype(context, rangeConstructor, "Range")
	if err != nil {
		return err
	}
	selectionConstructor, err := realm.newDOMInterfaceConstructor(context, "Selection", realm.active.ObjectPrototype)
	if err != nil {
		return err
	}
	bindings.selectionPrototype, err = constructorPrototype(context, selectionConstructor, "Selection")
	if err != nil {
		return err
	}
	realm.bindings = bindings
	if err := realm.installDOMPrototypeProperties(context); err != nil {
		return err
	}
	if err := realm.installStorageDocumentCookie(context); err != nil {
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
	bindings.selection, err = realm.newSelectionLocked(context)
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
	setInterval, err := realm.newNativeFunction(context, "setInterval", 2, nativeGlobalSetInterval)
	if err != nil {
		return err
	}
	clearInterval, err := realm.newNativeFunction(context, "clearInterval", 1, nativeGlobalClearInterval)
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
	getSelection, err := realm.newNativeFunction(context, "getSelection", 0, nativeDocumentGetSelection)
	if err != nil {
		return err
	}
	imageConstructor, err := realm.newImageConstructor(context)
	if err != nil {
		return err
	}
	navigator, err := realm.newNavigator(context)
	if err != nil {
		return err
	}
	matchMedia, err := realm.newNativeFunction(context, "matchMedia", 1, nativeMatchMedia)
	if err != nil {
		return err
	}
	fetch, err := realm.newNativeFunction(context, "fetch", 1, nativeGlobalFetch)
	if err != nil {
		return err
	}
	windowProperties := []struct {
		name  string
		value memory.Value
	}{
		{"window", memory.RefValue(bindings.window)},
		{"self", memory.RefValue(bindings.window)},
		{"globalThis", memory.RefValue(bindings.window)},
		{"document", memory.RefValue(bindings.document)},
		{"queueMicrotask", memory.RefValue(realm.active.QueueMicrotask)},
		{"setTimeout", memory.RefValue(setTimeout)},
		{"clearTimeout", memory.RefValue(clearTimeout)},
		{"setInterval", memory.RefValue(setInterval)},
		{"clearInterval", memory.RefValue(clearInterval)},
		{"requestAnimationFrame", memory.RefValue(requestAnimationFrame)},
		{"cancelAnimationFrame", memory.RefValue(cancelAnimationFrame)},
		{"performance", memory.RefValue(bindings.performance)},
		{"navigator", memory.RefValue(navigator)},
		{"matchMedia", memory.RefValue(matchMedia)},
		{"Image", memory.RefValue(imageConstructor)},
		{"fetch", memory.RefValue(fetch)},
		{"Headers", memory.RefValue(bindings.headersConstructor)},
		{"Request", memory.RefValue(bindings.requestConstructor)},
		{"Response", memory.RefValue(bindings.responseConstructor)},
		{"Storage", memory.RefValue(bindings.storageConstructor)},
		{"localStorage", memory.RefValue(bindings.localStorage)},
		{"sessionStorage", memory.RefValue(bindings.sessionStorage)},
		{"MutationObserver", memory.RefValue(bindings.mutationObserverConstructor)},
		{bindingDOMException, memory.RefValue(bindings.domExceptionConstructor)},
		{"URLSearchParams", memory.RefValue(bindings.urlSearchParamsConstructor)},
		{"URL", memory.RefValue(bindings.urlConstructor)},
		{"TextEncoder", memory.RefValue(bindings.textEncoderConstructor)},
		{"TextDecoder", memory.RefValue(bindings.textDecoderConstructor)},
		{"Uint8Array", memory.RefValue(bindings.uint8ArrayConstructor)},
		{"Event", memory.RefValue(eventConstructor)},
		{"CustomEvent", memory.RefValue(customEventConstructor)},
		{"getComputedStyle", memory.RefValue(getComputedStyle)},
		{"getSelection", memory.RefValue(getSelection)},
		{"Range", memory.RefValue(rangeConstructor)},
		{"Selection", memory.RefValue(selectionConstructor)},
	}
	for _, domInterface := range domInterfaces {
		windowProperties = append(windowProperties, struct {
			name  string
			value memory.Value
		}{domInterface.name, memory.RefValue(domInterface.constructor)})
	}
	for _, property := range windowProperties {
		if err := defineData(context, bindings.window, property.name, property.value, true, false, true); err != nil {
			return err
		}
	}
	globalBindings := []struct {
		name    string
		value   memory.Ref
		mutable bool
	}{
		{bindingWindowPrototype, bindings.windowPrototype, false},
		{bindingDocumentPrototype, bindings.documentPrototype, false},
		{bindingNodePrototype, bindings.nodePrototype, false},
		{bindingElementPrototype, bindings.elementPrototype, false},
		{bindingHTMLElementPrototype, bindings.htmlElementPrototype, false},
		{bindingHTMLFormElementPrototype, bindings.htmlFormElementPrototype, false},
		{bindingHTMLInputElementPrototype, bindings.htmlInputElementPrototype, false},
		{bindingHTMLTextAreaElementPrototype, bindings.htmlTextAreaElementPrototype, false},
		{bindingHTMLSelectElementPrototype, bindings.htmlSelectElementPrototype, false},
		{bindingHTMLOptionElementPrototype, bindings.htmlOptionElementPrototype, false},
		{bindingHTMLButtonElementPrototype, bindings.htmlButtonElementPrototype, false},
		{bindingHTMLIFrameElementPrototype, bindings.htmlIFrameElementPrototype, false},
		{bindingHTMLHeadElementPrototype, bindings.htmlHeadElementPrototype, false},
		{bindingHTMLScriptElementPrototype, bindings.htmlScriptElementPrototype, false},
		{bindingHTMLMediaElementPrototype, bindings.htmlMediaElementPrototype, false},
		{bindingHTMLImageElementPrototype, bindings.htmlImageElementPrototype, false},
		{bindingHTMLCollectionPrototype, bindings.htmlCollectionPrototype, false},
		{bindingTemplatePrototype, bindings.templatePrototype, false},
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
		{bindingDOMExceptionPrototype, bindings.domExceptionPrototype, false},
		{bindingDOMExceptionConstructor, bindings.domExceptionConstructor, false},
		{bindingURLSearchParamsPrototype, bindings.urlSearchParamsPrototype, false},
		{bindingURLSearchParamsConstructor, bindings.urlSearchParamsConstructor, false},
		{bindingURLPrototype, bindings.urlPrototype, false},
		{bindingURLConstructor, bindings.urlConstructor, false},
		{bindingTextEncoderPrototype, bindings.textEncoderPrototype, false},
		{bindingTextEncoderConstructor, bindings.textEncoderConstructor, false},
		{bindingTextDecoderPrototype, bindings.textDecoderPrototype, false},
		{bindingTextDecoderConstructor, bindings.textDecoderConstructor, false},
		{bindingUint8ArrayPrototype, bindings.uint8ArrayPrototype, false},
		{bindingUint8ArrayConstructor, bindings.uint8ArrayConstructor, false},
		{bindingHeadersPrototype, bindings.headersPrototype, false},
		{bindingHeadersConstructor, bindings.headersConstructor, false},
		{bindingRequestPrototype, bindings.requestPrototype, false},
		{bindingRequestConstructor, bindings.requestConstructor, false},
		{bindingResponsePrototype, bindings.responsePrototype, false},
		{bindingResponseConstructor, bindings.responseConstructor, false},
		{bindingStoragePrototype, bindings.storagePrototype, false},
		{bindingStorageConstructor, bindings.storageConstructor, false},
		{bindingLocalStorage, bindings.localStorage, false},
		{bindingSessionStorage, bindings.sessionStorage, false},
		{bindingRangePrototype, bindings.rangePrototype, false},
		{bindingSelectionPrototype, bindings.selectionPrototype, false},
		{bindingSelection, bindings.selection, false},
		{bindingWrapperCache, bindings.wrapperCache, false},
		{bindingCallbackCache, bindings.callbackCache, false},
		{bindingFacadeCache, bindings.facadeCache, false},
		{bindingCollectionCache, bindings.collectionCache, false},
		{bindingObserverCache, bindings.observerCache, false},
		{bindingModuleCache, bindings.moduleCache, false},
		{bindingWindow, bindings.window, false},
		{bindingSelf, bindings.window, false},
		{"globalThis", bindings.window, false},
		{bindingDocument, bindings.document, false},
		{bindingPerformance, bindings.performance, false},
		{"navigator", navigator, false},
		{"matchMedia", matchMedia, true},
		{"Image", imageConstructor, true},
		{"fetch", fetch, true},
		{bindingMutationObserver, bindings.mutationObserverConstructor, false},
		{bindingDOMException, bindings.domExceptionConstructor, true},
		{"URLSearchParams", bindings.urlSearchParamsConstructor, true},
		{"URL", bindings.urlConstructor, true},
		{"TextEncoder", bindings.textEncoderConstructor, true},
		{"TextDecoder", bindings.textDecoderConstructor, true},
		{"Uint8Array", bindings.uint8ArrayConstructor, true},
		{"Headers", bindings.headersConstructor, true},
		{"Request", bindings.requestConstructor, true},
		{"Response", bindings.responseConstructor, true},
		{"Storage", bindings.storageConstructor, true},
		{"localStorage", bindings.localStorage, false},
		{"sessionStorage", bindings.sessionStorage, false},
		{"Event", eventConstructor, true},
		{"CustomEvent", customEventConstructor, true},
		{"setTimeout", setTimeout, true},
		{"clearTimeout", clearTimeout, true},
		{"setInterval", setInterval, true},
		{"clearInterval", clearInterval, true},
		{"requestAnimationFrame", requestAnimationFrame, true},
		{"cancelAnimationFrame", cancelAnimationFrame, true},
		{"getComputedStyle", getComputedStyle, true},
		{"getSelection", getSelection, true},
		{"Range", rangeConstructor, true},
		{"Selection", selectionConstructor, true},
	}
	for _, domInterface := range domInterfaces {
		globalBindings = append(globalBindings, struct {
			name    string
			value   memory.Ref
			mutable bool
		}{domInterface.name, domInterface.constructor, true})
	}
	for _, binding := range globalBindings {
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
		{realm.bindings.documentPrototype, "getElementsByTagName", 1, nativeDocumentGetElementsByTagName},
		{realm.bindings.documentPrototype, "createElement", 1, nativeDocumentCreateElement},
		{realm.bindings.documentPrototype, "createTextNode", 1, nativeDocumentCreateTextNode},
		{realm.bindings.documentPrototype, "createElementNS", 2, nativeDocumentCreateElementNS},
		{realm.bindings.documentPrototype, "createDocumentFragment", 0, nativeDocumentCreateDocumentFragment},
		{realm.bindings.documentPrototype, "importNode", 2, nativeDocumentImportNode},
		{realm.bindings.documentPrototype, "querySelector", 1, nativeElementQuerySelector},
		{realm.bindings.documentPrototype, "querySelectorAll", 1, nativeElementQuerySelectorAll},
		{realm.bindings.fragmentPrototype, "querySelector", 1, nativeElementQuerySelector},
		{realm.bindings.fragmentPrototype, "querySelectorAll", 1, nativeElementQuerySelectorAll},
		{realm.bindings.documentPrototype, "createRange", 0, nativeDocumentCreateRange},
		{realm.bindings.documentPrototype, "getSelection", 0, nativeDocumentGetSelection},
		{realm.bindings.nodePrototype, "appendChild", 1, nativeNodeAppendChild},
		{realm.bindings.nodePrototype, "insertBefore", 2, nativeNodeInsertBefore},
		{realm.bindings.nodePrototype, "removeChild", 1, nativeNodeRemoveChild},
		{realm.bindings.nodePrototype, "contains", 1, nativeNodeContains},
		{realm.bindings.nodePrototype, "cloneNode", 1, nativeNodeCloneNode},
		{realm.bindings.nodePrototype, "replaceChild", 2, nativeNodeReplaceChild},
		{realm.bindings.nodePrototype, "normalize", 0, nativeNodeNormalize},
		{realm.bindings.nodePrototype, "remove", 0, nativeNodeRemove},
		{realm.bindings.elementPrototype, "append", 0, nativeNodeAppend},
		{realm.bindings.documentPrototype, "append", 0, nativeNodeAppend},
		{realm.bindings.fragmentPrototype, "append", 0, nativeNodeAppend},
		{realm.bindings.elementPrototype, "prepend", 0, nativeNodePrepend},
		{realm.bindings.documentPrototype, "prepend", 0, nativeNodePrepend},
		{realm.bindings.fragmentPrototype, "prepend", 0, nativeNodePrepend},
		{realm.bindings.elementPrototype, "replaceChildren", 0, nativeNodeReplaceChildren},
		{realm.bindings.documentPrototype, "replaceChildren", 0, nativeNodeReplaceChildren},
		{realm.bindings.fragmentPrototype, "replaceChildren", 0, nativeNodeReplaceChildren},
		{realm.bindings.elementPrototype, "before", 0, nativeNodeBefore},
		{realm.bindings.textPrototype, "before", 0, nativeNodeBefore},
		{realm.bindings.elementPrototype, "after", 0, nativeNodeAfter},
		{realm.bindings.textPrototype, "after", 0, nativeNodeAfter},
		{realm.bindings.elementPrototype, "replaceWith", 0, nativeNodeReplaceWith},
		{realm.bindings.textPrototype, "replaceWith", 0, nativeNodeReplaceWith},
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
		{realm.bindings.htmlElementPrototype, "focus", 0, nativeElementFocus},
		{realm.bindings.htmlElementPrototype, "blur", 0, nativeElementBlur},
		{realm.bindings.htmlInputElementPrototype, "setSelectionRange", 2, nativeElementSetSelectionRange},
		{realm.bindings.htmlInputElementPrototype, "select", 0, nativeElementSelect},
		{realm.bindings.htmlTextAreaElementPrototype, "setSelectionRange", 2, nativeElementSetSelectionRange},
		{realm.bindings.htmlTextAreaElementPrototype, "select", 0, nativeElementSelect},
		{realm.bindings.htmlFormElementPrototype, "reset", 0, nativeHTMLFormElementReset},
		{realm.bindings.htmlCollectionPrototype, "item", 1, nativeHTMLCollectionItem},
		{realm.bindings.htmlCollectionPrototype, "namedItem", 1, nativeHTMLCollectionNamedItem},
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
		{realm.bindings.nodePrototype, "dispatchEvent", 1, nativeEventTargetDispatch},
		{realm.bindings.windowPrototype, "addEventListener", 2, nativeEventTargetAdd},
		{realm.bindings.windowPrototype, "removeEventListener", 2, nativeEventTargetRemove},
		{realm.bindings.windowPrototype, "dispatchEvent", 1, nativeEventTargetDispatch},
		{realm.bindings.eventPrototype, "preventDefault", 0, nativeEventPreventDefault},
		{realm.bindings.eventPrototype, "stopPropagation", 0, nativeEventStopPropagation},
		{realm.bindings.eventPrototype, "stopImmediatePropagation", 0, nativeEventStopImmediatePropagation},
		{realm.bindings.windowPrototype, "getSelection", 0, nativeDocumentGetSelection},
		{realm.bindings.rangePrototype, "setStart", 2, nativeRangeSetStart},
		{realm.bindings.rangePrototype, "setEnd", 2, nativeRangeSetEnd},
		{realm.bindings.rangePrototype, "selectNode", 1, nativeRangeSelectNode},
		{realm.bindings.rangePrototype, "selectNodeContents", 1, nativeRangeSelectNodeContents},
		{realm.bindings.rangePrototype, "collapse", 1, nativeRangeCollapse},
		{realm.bindings.rangePrototype, "cloneRange", 0, nativeRangeCloneRange},
		{realm.bindings.rangePrototype, "cloneContents", 0, nativeRangeCloneContents},
		{realm.bindings.rangePrototype, "extractContents", 0, nativeRangeExtractContents},
		{realm.bindings.rangePrototype, "deleteContents", 0, nativeRangeDeleteContents},
		{realm.bindings.rangePrototype, "insertNode", 1, nativeRangeInsertNode},
		{realm.bindings.rangePrototype, "detach", 0, nativeRangeDetach},
		{realm.bindings.selectionPrototype, "getRangeAt", 1, nativeSelectionGetRangeAt},
		{realm.bindings.selectionPrototype, "addRange", 1, nativeSelectionAddRange},
		{realm.bindings.selectionPrototype, "removeAllRanges", 0, nativeSelectionRemoveAllRanges},
		{realm.bindings.selectionPrototype, "empty", 0, nativeSelectionRemoveAllRanges},
		{realm.bindings.selectionPrototype, "collapse", 2, nativeSelectionCollapse},
		{realm.bindings.selectionPrototype, "setPosition", 2, nativeSelectionCollapse},
		{realm.bindings.selectionPrototype, "collapseToStart", 0, nativeSelectionCollapseToStart},
		{realm.bindings.selectionPrototype, "collapseToEnd", 0, nativeSelectionCollapseToEnd},
		{realm.bindings.selectionPrototype, "selectAllChildren", 1, nativeSelectionSelectAllChildren},
		{realm.bindings.selectionPrototype, "deleteFromDocument", 0, nativeSelectionDeleteFromDocument},
		{realm.bindings.selectionPrototype, "toString", 0, nativeSelectionToString},
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
	if err := realm.installHTMLCollectionIteration(context); err != nil {
		return err
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
		{realm.bindings.documentPrototype, "children", nativeElementChildren, 0},
		{realm.bindings.fragmentPrototype, "children", nativeElementChildren, 0},
		{realm.bindings.templatePrototype, "content", nativeTemplateContent, 0},
		{realm.bindings.textPrototype, "data", nativeNodeValueGet, nativeNodeValueSet},
		{realm.bindings.elementPrototype, "localName", nativeElementLocalName, 0},
		{realm.bindings.elementPrototype, "children", nativeElementChildren, 0},
		{realm.bindings.elementPrototype, "firstElementChild", nativeNodeFirstElementChild, 0},
		{realm.bindings.elementPrototype, "lastElementChild", nativeNodeLastElementChild, 0},
		{realm.bindings.elementPrototype, "previousElementSibling", nativeNodePreviousElementSibling, 0},
		{realm.bindings.elementPrototype, "nextElementSibling", nativeNodeNextElementSibling, 0},
		{realm.bindings.documentPrototype, "firstElementChild", nativeNodeFirstElementChild, 0},
		{realm.bindings.documentPrototype, "lastElementChild", nativeNodeLastElementChild, 0},
		{realm.bindings.fragmentPrototype, "firstElementChild", nativeNodeFirstElementChild, 0},
		{realm.bindings.fragmentPrototype, "lastElementChild", nativeNodeLastElementChild, 0},
		{realm.bindings.elementPrototype, "id", nativeElementIDGet, nativeElementIDSet},
		{realm.bindings.elementPrototype, "className", nativeElementClassNameGet, nativeElementClassNameSet},
		{realm.bindings.elementPrototype, "classList", nativeElementClassList, 0},
		{realm.bindings.elementPrototype, "dataset", nativeElementDataset, 0},
		{realm.bindings.elementPrototype, "innerHTML", nativeElementInnerHTMLGet, nativeElementInnerHTMLSet},
		{realm.bindings.elementPrototype, "style", nativeElementStyle, 0},
		{realm.bindings.htmlElementPrototype, "hidden", nativeElementHiddenGet, nativeElementHiddenSet},
		{realm.bindings.htmlInputElementPrototype, "value", nativeElementFormValueGet, nativeElementFormValueSet},
		{realm.bindings.htmlTextAreaElementPrototype, "value", nativeElementFormValueGet, nativeElementFormValueSet},
		{realm.bindings.htmlSelectElementPrototype, "value", nativeElementFormValueGet, nativeElementFormValueSet},
		{realm.bindings.htmlOptionElementPrototype, "value", nativeElementFormValueGet, nativeElementFormValueSet},
		{realm.bindings.htmlButtonElementPrototype, "value", nativeElementFormValueGet, nativeElementFormValueSet},
		{realm.bindings.htmlInputElementPrototype, "checked", nativeElementFormCheckedGet, nativeElementFormCheckedSet},
		{realm.bindings.htmlInputElementPrototype, "indeterminate", nativeElementFormIndeterminateGet, nativeElementFormIndeterminateSet},
		{realm.bindings.htmlInputElementPrototype, "defaultChecked", nativeElementDefaultCheckedGet, nativeElementDefaultCheckedSet},
		{realm.bindings.htmlOptionElementPrototype, "selected", nativeElementFormSelectedGet, nativeElementFormSelectedSet},
		{realm.bindings.htmlOptionElementPrototype, "defaultSelected", nativeElementDefaultSelectedGet, nativeElementDefaultSelectedSet},
		{realm.bindings.htmlSelectElementPrototype, "selectedIndex", nativeElementFormSelectedIndexGet, nativeElementFormSelectedIndexSet},
		{realm.bindings.htmlFormElementPrototype, "elements", nativeElementFormElements, 0},
		{realm.bindings.htmlSelectElementPrototype, "options", nativeElementSelectOptions, 0},
		{realm.bindings.htmlInputElementPrototype, "form", nativeElementFormOwner, 0},
		{realm.bindings.htmlTextAreaElementPrototype, "form", nativeElementFormOwner, 0},
		{realm.bindings.htmlSelectElementPrototype, "form", nativeElementFormOwner, 0},
		{realm.bindings.htmlButtonElementPrototype, "form", nativeElementFormOwner, 0},
		{realm.bindings.htmlInputElementPrototype, "selectionStart", nativeElementSelectionStartGet, nativeElementSelectionStartSet},
		{realm.bindings.htmlInputElementPrototype, "selectionEnd", nativeElementSelectionEndGet, nativeElementSelectionEndSet},
		{realm.bindings.htmlInputElementPrototype, "selectionDirection", nativeElementSelectionDirectionGet, nativeElementSelectionDirectionSet},
		{realm.bindings.htmlTextAreaElementPrototype, "selectionStart", nativeElementSelectionStartGet, nativeElementSelectionStartSet},
		{realm.bindings.htmlTextAreaElementPrototype, "selectionEnd", nativeElementSelectionEndGet, nativeElementSelectionEndSet},
		{realm.bindings.htmlTextAreaElementPrototype, "selectionDirection", nativeElementSelectionDirectionGet, nativeElementSelectionDirectionSet},
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
		{realm.bindings.rangePrototype, "startContainer", nativeRangeStartContainer, 0},
		{realm.bindings.rangePrototype, "startOffset", nativeRangeStartOffset, 0},
		{realm.bindings.rangePrototype, "endContainer", nativeRangeEndContainer, 0},
		{realm.bindings.rangePrototype, "endOffset", nativeRangeEndOffset, 0},
		{realm.bindings.rangePrototype, "collapsed", nativeRangeCollapsed, 0},
		{realm.bindings.rangePrototype, "commonAncestorContainer", nativeRangeCommonAncestor, 0},
		{realm.bindings.selectionPrototype, "anchorNode", nativeSelectionAnchorNode, 0},
		{realm.bindings.selectionPrototype, "anchorOffset", nativeSelectionAnchorOffset, 0},
		{realm.bindings.selectionPrototype, "focusNode", nativeSelectionFocusNode, 0},
		{realm.bindings.selectionPrototype, "focusOffset", nativeSelectionFocusOffset, 0},
		{realm.bindings.selectionPrototype, "isCollapsed", nativeSelectionIsCollapsed, 0},
		{realm.bindings.selectionPrototype, "rangeCount", nativeSelectionRangeCount, 0},
		{realm.bindings.selectionPrototype, "type", nativeSelectionType, 0},
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

func (realm *Realm) installHTMLCollectionIteration(context *browserruntime.TaskContext) error {
	var values memory.Value
	for _, name := range []string{"keys", "values", "entries"} {
		nameRef, err := context.NewString(name)
		if err != nil {
			return err
		}
		method, found, err := context.GetOwnProperty(realm.active.ArrayPrototype, nameRef)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("nativeengine: Array.prototype.%s is unavailable", name)
		}
		if err := defineData(context, realm.bindings.htmlCollectionPrototype, name, method, true, false, true); err != nil {
			return err
		}
		if name == "values" {
			values = method
		}
	}
	return context.DefineProperty(
		realm.bindings.htmlCollectionPrototype,
		realm.active.SymbolIterator,
		memory.DataProperty(values, true, false, true),
	)
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

func (realm *Realm) newDOMInterfaceConstructor(context *browserruntime.TaskContext, name string, parentPrototype memory.Ref) (memory.Ref, error) {
	nameValue, err := newString(context, name)
	if err != nil {
		return memory.Ref{}, err
	}
	constructor, err := context.NewNativeConstructor(nameValue, memory.RefValue(realm.active.Global), 0, nativeDOMInterfaceConstructor)
	if err != nil {
		return memory.Ref{}, err
	}
	prototypeName, err := context.NewString("prototype")
	if err != nil {
		return memory.Ref{}, err
	}
	prototype, found, err := context.GetOwnProperty(constructor, prototypeName)
	if err != nil || !found || !prototype.IsRef() {
		return memory.Ref{}, fmt.Errorf("nativeengine: %s constructor lost its prototype", name)
	}
	if err := context.SetPrototype(prototype.Ref(), memory.RefValue(parentPrototype)); err != nil {
		return memory.Ref{}, err
	}
	return constructor, nil
}

func (realm *Realm) domInterfaceConstructor(_ *browserruntime.TaskContext, _ memory.Value, _ []memory.Value) (memory.Value, error) {
	return memory.Value{}, fmt.Errorf("%w: illegal DOM constructor", browserruntime.ErrOperandType)
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
	prototype, err := realm.bindings.prototypeForNode(metadata)
	if err != nil {
		return memory.Ref{}, err
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
		return memory.Value{}, realm.throwDOMException(context, err)
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
		return memory.Value{}, realm.throwDOMException(context, err)
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

func (realm *Realm) documentImportNode(context *browserruntime.TaskContext, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapNode(context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	host, ok := realm.host.(browser.DOMElementHost)
	if !ok {
		return memory.Value{}, fmt.Errorf("nativeengine: browser host does not expose node importing")
	}
	clone, err := host.CloneNode(handle, truthy(argument(arguments, 1)))
	if err != nil {
		return memory.Value{}, err
	}
	return realm.wrappedNodeValue(context, clone)
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
		return memory.Value{}, realm.throwDOMException(context, err)
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
		return memory.Value{}, realm.throwDOMException(context, err)
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
		return memory.Value{}, realm.throwDOMException(context, err)
	}
	return childValue, realm.refreshCollections(context, parent)
}

func (realm *Realm) nodeRemove(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapNode(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	host, ok := realm.host.(browser.DOMElementHost)
	if !ok {
		return memory.Value{}, fmt.Errorf("nativeengine: browser host does not expose node removal")
	}
	parent, found, err := host.RelatedNode(handle, browser.RelationParentNode)
	if err != nil || !found {
		return memory.UndefinedValue(), err
	}
	if err := realm.host.RemoveChild(parent, handle); err != nil {
		return memory.Value{}, realm.throwDOMException(context, err)
	}
	return memory.UndefinedValue(), realm.refreshCollections(context, parent)
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

func (realm *Realm) templateContent(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapNode(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	host, ok := realm.host.(browser.DOMElementHost)
	if !ok {
		return memory.Value{}, fmt.Errorf("nativeengine: browser host does not expose template contents")
	}
	content, err := host.TemplateContent(handle)
	if err != nil {
		return memory.Value{}, err
	}
	return realm.wrappedNodeValue(context, content)
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
		return memory.Value{}, realm.throwDOMException(context, err)
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
	if err := realm.host.SetAttribute(handle, name, value); err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), realm.refreshFormCollections(context)
}

func (realm *Realm) elementRemoveAttribute(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, name, err := realm.attributeOperands(context, this, arguments)
	if err != nil {
		return memory.Value{}, err
	}
	if err := realm.host.RemoveAttribute(handle, name); err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), realm.refreshFormCollections(context)
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
	if err := realm.host.SetAttribute(handle, "id", value); err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), realm.refreshFormCollections(context)
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
			return memory.Value{}, realm.throwDOMException(context, err)
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

func (realm *Realm) documentGetElementsByTagName(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapNode(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	tag, err := stringArgument(context, arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	host, ok := realm.host.(browser.DOMElementHost)
	if !ok {
		return memory.Value{}, fmt.Errorf("nativeengine: browser host does not expose selectors")
	}
	handles, err := host.QuerySelector(handle, tag, true)
	if err != nil {
		return memory.Value{}, err
	}
	return realm.nodeArray(context, handles)
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
