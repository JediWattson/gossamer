package browser

import (
	"time"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

// ValueHandle is opaque engine-owned identity for a JavaScript value or
// callback. Browser code may transport it but never inspect engine storage.
type ValueHandle uint64

type InputEventType uint8

const (
	InputClick InputEventType = iota + 1
	InputPointerDown
	InputPointerUp
	InputPointerMove
	InputPointerCancel
	InputPointerOver
	InputPointerOut
	InputPointerEnter
	InputPointerLeave
	InputKeyDown
	InputKeyUp
	InputBeforeInput
	InputInput
	InputFocus
	InputBlur
	InputFocusIn
	InputFocusOut
	InputChange
	InputCompositionStart
	InputCompositionUpdate
	InputCompositionEnd
	InputSubmit
	InputInvalid
	InputScroll
	InputResize
	InputDOMContentLoaded
	InputLoad
)

// InputEvent contains browser-normalized input and a generation-safe target.
type InputEvent struct {
	Type          InputEventType
	Target        NodeHandle
	RelatedTarget NodeHandle
	X             float64
	Y             float64
	Button        int
	Buttons       uint
	PointerID     int
	PointerType   string
	IsPrimary     bool
	Key           string
	Code          string
	Data          string
	InputType     string
	Repeat        bool
	IsComposing   bool
	AltKey        bool
	CtrlKey       bool
	MetaKey       bool
	ShiftKey      bool
}

func (eventType InputEventType) String() string {
	switch eventType {
	case InputClick:
		return "click"
	case InputPointerDown:
		return "pointerdown"
	case InputPointerUp:
		return "pointerup"
	case InputPointerMove:
		return "pointermove"
	case InputPointerCancel:
		return "pointercancel"
	case InputPointerOver:
		return "pointerover"
	case InputPointerOut:
		return "pointerout"
	case InputPointerEnter:
		return "pointerenter"
	case InputPointerLeave:
		return "pointerleave"
	case InputKeyDown:
		return "keydown"
	case InputKeyUp:
		return "keyup"
	case InputBeforeInput:
		return "beforeinput"
	case InputInput:
		return "input"
	case InputFocus:
		return "focus"
	case InputBlur:
		return "blur"
	case InputFocusIn:
		return "focusin"
	case InputFocusOut:
		return "focusout"
	case InputChange:
		return "change"
	case InputCompositionStart:
		return "compositionstart"
	case InputCompositionUpdate:
		return "compositionupdate"
	case InputCompositionEnd:
		return "compositionend"
	case InputSubmit:
		return "submit"
	case InputInvalid:
		return "invalid"
	case InputScroll:
		return "scroll"
	case InputResize:
		return "resize"
	case InputDOMContentLoaded:
		return "DOMContentLoaded"
	case InputLoad:
		return "load"
	default:
		return ""
	}
}

func (eventType InputEventType) pointerTargeted() bool {
	return eventType >= InputClick && eventType <= InputPointerLeave
}

// ScriptSource is an engine-neutral unit of source evaluation. URL is the
// resolved source identity used for diagnostics; Source contains UTF-8 code.
type ScriptSource struct {
	URL    string
	Source string
}

// ModuleResolution is one browser-resolved static import edge. Engines receive
// canonical URLs and never perform network access or URL resolution while an
// isolate is entered.
type ModuleResolution struct {
	Referrer  string
	Specifier string
	URL       string
}

// ModuleGraph is a complete, pointer-free source graph rooted at RootURL.
// Sources may be supplied again across root evaluations; an engine-owned
// per-Realm module cache preserves JavaScript module identity and one-time
// evaluation.
type ModuleGraph struct {
	RootURL     string
	Sources     []ScriptSource
	Resolutions []ModuleResolution
}

// Engine is the engine-neutral factory boundary. A future V8 adapter owns its
// isolates and creates one JSRealm per browser Page through this interface.
type Engine interface {
	NewRealm() (JSRealm, error)
	Close() error
}

// JSRealm is the only script surface called by Page tasks. It receives an
// execution-scoped Host and may not retain it after the call returns.
type JSRealm interface {
	Evaluate(Host, ScriptSource) error
	DispatchEvent(Host, InputEvent) (EventDispatchResult, error)
	Invoke(Host, ValueHandle) error
	DrainMicrotasks(Host) error
	Close() error
}

// JSModuleRealm is the optional stock-engine extension for browser-fetched ES
// module graphs. Fetching and resolution stay in Go outside the Realm; compile,
// instantiate, and evaluation stay inside the engine entry.
type JSModuleRealm interface {
	EvaluateModule(Host, ModuleGraph) error
}

type EventDispatchResult struct {
	DefaultPrevented bool
}

// Host is the execution-scoped browser API visible to an engine. Methods that
// schedule work preserve the current task as the publication boundary.
type Host interface {
	GetElementByID(string) (NodeHandle, bool, error)
	CreateElement(string) (NodeHandle, error)
	CreateTextNode(string) (NodeHandle, error)
	TextContent(NodeHandle) (string, error)
	SetTextContent(NodeHandle, string) error
	AppendChild(NodeHandle, NodeHandle) error
	InsertBefore(NodeHandle, NodeHandle, NodeHandle) error
	RemoveChild(NodeHandle, NodeHandle) error
	GetAttribute(NodeHandle, string) (string, bool, error)
	SetAttribute(NodeHandle, string, string) error
	RemoveAttribute(NodeHandle, string) error
	Text(NodeHandle) (string, error)
	SetText(NodeHandle, string) error
	QueueCallback(ValueHandle) error
	QueueMicrotask(ValueHandle) error
	SetTimeout(ValueHandle, time.Duration) (TimerID, error)
	ClearTimeout(TimerID) error
}

type AnimationFrameID uint64

// AnimationFrameHost is the optional frame-clock seam exposed by browser
// hosts. Callback identity stays engine-owned; the Page owns scheduling,
// cancellation, time origin, and queue publication.
type AnimationFrameHost interface {
	RequestAnimationFrame(ValueHandle) (AnimationFrameID, error)
	CancelAnimationFrame(AnimationFrameID) error
	PerformanceNow() float64
}

// AnimationFrameRealm receives the shared monotonic timestamp for every
// callback in one frame batch. Engines without this extension can fall back to
// ordinary no-argument callback invocation.
type AnimationFrameRealm interface {
	InvokeAnimationFrame(Host, ValueHandle, float64) error
}

// NodeWrapperLifetimeHost is the optional lifetime seam used by engines with
// garbage-collected numeric DOM wrappers. One detached weak wrapper is one
// semantic root; connected wrappers rely on the document claim, and ordinary
// aliases to either wrapper remain entirely inside the engine.
type NodeWrapperLifetimeHost interface {
	RetainNodeWrapper(NodeHandle) error
	ReleaseNodeWrappers([]NodeHandle) error
}

// NodeFacadeHost exposes the Go-owned half of a JavaScript DOM wrapper. The
// Ref resolves to an immutable HostObject in the current document region.
type NodeFacadeHost interface {
	NodeFacadeRef(NodeHandle) (memory.Ref, error)
}

// NodeEventListenerLifetimeHost lets an engine publish native listener roots
// independently from weak wrapper roots. A detached target with a registered
// listener remains alive until its final listener is removed or its document
// Realm is torn down.
type NodeEventListenerLifetimeHost interface {
	RetainNodeEventTarget(NodeHandle) error
	ReleaseNodeEventTarget(NodeHandle) error
}

// NodeRelation identifies one traversal property on a JavaScript Node or
// Element wrapper.
type NodeRelation uint8

const (
	RelationParentNode NodeRelation = iota + 1
	RelationParentElement
	RelationFirstChild
	RelationLastChild
	RelationPreviousSibling
	RelationNextSibling
	RelationFirstElementChild
	RelationLastElementChild
	RelationPreviousElementSibling
	RelationNextElementSibling
	RelationDocumentElement
	RelationDocumentHead
	RelationDocumentBody
)

// DOMNodeType uses the numeric values exposed by the web Node interface.
type DOMNodeType uint8

const (
	DOMElementNode               DOMNodeType = 1
	DOMTextNode                  DOMNodeType = 3
	DOMProcessingInstructionNode DOMNodeType = 7
	DOMCommentNode               DOMNodeType = 8
	DOMDocumentNode              DOMNodeType = 9
	DOMDocumentTypeNode          DOMNodeType = 10
	DOMDocumentFragmentNode      DOMNodeType = 11
)

// NodeMetadata is the scalar node state exposed through JavaScript traversal.
type NodeMetadata struct {
	Type         DOMNodeType
	NodeName     string
	LocalName    string
	NamespaceURI string
	Prefix       string
	Connected    bool
}

type DocumentMetadata struct {
	Root    NodeHandle
	BaseURI string
}

// DocumentLifecycleHost exposes the Page-owned loading state. Engines surface
// it as a live document.readyState read instead of caching it in their heap.
type DocumentLifecycleHost interface {
	DocumentReadyState() (string, error)
}

// DOMElementHost is an optional extension implemented by browser hosts that
// expose the practical Node, Element, and inline-style surface used by
// framework renderers. Keeping it separate lets smaller engines implement the
// base Host without pretending to support these APIs.
type DOMElementHost interface {
	NodeMetadata(NodeHandle) (NodeMetadata, error)
	RelatedNode(NodeHandle, NodeRelation) (NodeHandle, bool, error)
	ChildNodes(NodeHandle, bool) ([]NodeHandle, error)
	Contains(NodeHandle, NodeHandle) (bool, error)
	ReplaceChild(NodeHandle, NodeHandle, NodeHandle) error
	MutateNodes(NodeHandle, dom.MutationOperation, []NodeHandle) error
	NodeValue(NodeHandle) (string, bool, error)
	SetNodeValue(NodeHandle, string) error
	HasAttribute(NodeHandle, string) (bool, error)
	AttributeNames(NodeHandle) ([]string, error)
	QuerySelector(NodeHandle, string, bool) ([]NodeHandle, error)
	MatchesSelector(NodeHandle, string) (bool, error)
	ClosestSelector(NodeHandle, string) (NodeHandle, bool, error)
	CloneNode(NodeHandle, bool) (NodeHandle, error)
	TemplateContent(NodeHandle) (NodeHandle, error)
	SplitText(NodeHandle, int) (NodeHandle, error)
	Normalize(NodeHandle) error
	AdoptNode(NodeHandle) (NodeHandle, error)
	RangeContents(NodeHandle, int, NodeHandle, int, dom.RangeContentOperation) (NodeHandle, error)
	InnerHTML(NodeHandle) (string, error)
	SetInnerHTML(NodeHandle, string) error
	InsertAdjacentHTML(NodeHandle, string, string) error
	FormValue(NodeHandle) (string, error)
	SetFormValue(NodeHandle, string) error
	FormSelection(NodeHandle) (int, int, string, error)
	SetFormSelection(NodeHandle, int, int, string) error
	ReplaceFormSelection(NodeHandle, string, string) error
	FormChecked(NodeHandle) (bool, error)
	SetFormChecked(NodeHandle, bool) error
	FormIndeterminate(NodeHandle) (bool, error)
	SetFormIndeterminate(NodeHandle, bool) error
	FormSelected(NodeHandle) (bool, error)
	SetFormSelected(NodeHandle, bool) error
	FormSelectedIndex(NodeHandle) (int, error)
	SetFormSelectedIndex(NodeHandle, int) error
	FormControlNodes(NodeHandle, dom.FormCollectionKind) ([]NodeHandle, error)
	FormOwner(NodeHandle) (NodeHandle, bool, error)
	ResetForm(NodeHandle) error
	FormValidity(NodeHandle) (bool, []NodeHandle, error)
	MarkFormUserValidityForSubmission(NodeHandle) error
	FormData(NodeHandle, NodeHandle) ([]dom.FormEntry, error)
	SubmitForm(NodeHandle, NodeHandle) error
	Focus(NodeHandle) error
	Blur(NodeHandle) error
	ActiveElement() (NodeHandle, bool, error)
	StyleCSSText(NodeHandle) (string, error)
	SetStyleCSSText(NodeHandle, string) error
	StyleProperty(NodeHandle, string) (string, string, bool, error)
	SetStyleProperty(NodeHandle, string, string, string) error
	RemoveStyleProperty(NodeHandle, string) (string, error)
	StylePropertyNames(NodeHandle) ([]string, error)
}

// DOMComputedStyleHost is an optional extension implemented by browser hosts
// that expose layout-independent computed style. Computed declarations are
// read-only and live: each property or name read asks the Page for its current
// style snapshot instead of retaining a stale declaration value.
type DOMComputedStyleHost interface {
	ComputedStyleProperty(handle NodeHandle, pseudo, property string) (string, bool, error)
	ComputedStylePropertyNames(handle NodeHandle, pseudo string) ([]string, error)
}

// DOMRect is one immutable CSS-pixel rectangle returned across the engine
// boundary. Browser geometry stays in Go; JavaScript engines materialize only
// value facades from these scalars.
type DOMRect struct {
	X      float64
	Y      float64
	Width  float64
	Height float64
}

// DOMElementGeometry is the current CSSOM View projection for one Element.
// Rect is viewport-relative while all dimensions and scroll offsets are CSS
// pixels owned by the Page.
type DOMElementGeometry struct {
	Rect         DOMRect
	ClientRects  []DOMRect
	ClientWidth  float64
	ClientHeight float64
	OffsetWidth  float64
	OffsetHeight float64
	ScrollWidth  float64
	ScrollHeight float64
	ScrollLeft   float64
	ScrollTop    float64
}

// DOMViewportGeometry is the window viewport and root scrolling state.
type DOMViewportGeometry struct {
	InnerWidth   float64
	InnerHeight  float64
	ScrollX      float64
	ScrollY      float64
	ScrollWidth  float64
	ScrollHeight float64
}

// DOMGeometryHost exposes synchronous layout reads and root scrolling without
// allowing an engine to retain a Page pointer or a layout snapshot.
type DOMGeometryHost interface {
	ElementGeometry(NodeHandle) (DOMElementGeometry, error)
	ViewportGeometry() (DOMViewportGeometry, error)
	ScrollElement(NodeHandle, float64, float64) (bool, error)
	ScrollViewport(float64, float64) (bool, error)
	ScrollIntoView(NodeHandle) (bool, error)
}

// DOMMutationObserverHost exposes the document-owned mutation journal to a
// script Realm without moving observer policy into the native DOM.
type DOMMutationObserverHost interface {
	MutationSequence() (uint64, error)
	MutationRecordsSince(uint64) ([]dom.MutationRecord, uint64, error)
}

// DOMDocumentHost is the optional document interface needed to bind one
// canonical JavaScript Document wrapper to a Page's native document root.
type DOMDocumentHost interface {
	DocumentMetadata() (DocumentMetadata, error)
	CreateElementNS(string, string) (NodeHandle, error)
	CreateDocumentFragment() (NodeHandle, error)
}

func domNodeMetadata(snapshot dom.NodeSnapshot) NodeMetadata {
	metadata := NodeMetadata{Connected: snapshot.Connected}
	switch snapshot.Type {
	case dom.ElementNode:
		metadata.Type = DOMElementNode
		metadata.LocalName = snapshot.Data
		metadata.NamespaceURI = snapshot.NamespaceURI
		metadata.Prefix = snapshot.Prefix
		metadata.NodeName = snapshot.Data
		if snapshot.Prefix != "" {
			metadata.NodeName = snapshot.Prefix + ":" + snapshot.Data
		}
		if snapshot.NamespaceURI == dom.HTMLNamespace {
			metadata.NodeName = asciiUpper(metadata.NodeName)
		}
	case dom.TextNode:
		metadata.Type = DOMTextNode
		metadata.NodeName = "#text"
	case dom.CommentNode:
		metadata.Type = DOMCommentNode
		metadata.NodeName = "#comment"
	case dom.DocumentNode:
		metadata.Type = DOMDocumentNode
		metadata.NodeName = "#document"
	case dom.DoctypeNode:
		metadata.Type = DOMDocumentTypeNode
		metadata.NodeName = snapshot.Data
	case dom.ProcessingInstructionNode:
		metadata.Type = DOMProcessingInstructionNode
		metadata.NodeName = snapshot.Target
	case dom.DocumentFragmentNode:
		metadata.Type = DOMDocumentFragmentNode
		metadata.NodeName = "#document-fragment"
	}
	return metadata
}

func asciiUpper(value string) string {
	bytes := []byte(value)
	for index, character := range bytes {
		if character >= 'a' && character <= 'z' {
			bytes[index] = character - ('a' - 'A')
		}
	}
	return string(bytes)
}
