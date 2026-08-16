package browser

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
	"github.com/JediWattson/gossamer/internal/resource"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
	computed "github.com/JediWattson/gossamer/internal/style"
)

// Page is the owner that ties one Realm to its stable-identity Document,
// current URL, render resources, invalidation state, and current Frame.
type Page struct {
	ID    PageID
	Realm *browserruntime.Realm

	browser *Browser
	script  JSRealm

	mutex              sync.RWMutex
	document           *dom.Document
	nodeLifetimes      *nodeLifetimeState
	documentGeneration DocumentGeneration
	documentLanguage   string
	parent             *Page
	frameOwner         NodeHandle
	children           map[dom.NodeID]*Page
	location           *url.URL
	readyState         string
	formLoader         DocumentLoader
	history            []HistoryEntry
	historyIndex       int
	resources          pageResources
	resourceFetcher    resource.Fetcher
	documentContext    context.Context
	documentCancel     context.CancelFunc
	viewport           render.Viewport
	scrollX            float64
	scrollY            float64
	elementScroll      map[dom.NodeID]scrollOffset
	timeOrigin         time.Time
	lastFrameTime      float64
	nextAnimationFrame AnimationFrameID
	animationFrames    map[AnimationFrameID]*pageAnimationFrame
	frame              *render.Frame
	frameGeneration    DocumentGeneration
	computedStyle      computedStyleState
	styleRevision      uint64
	layout             layoutState
	layoutRevision     uint64
	dirty              bool
	renderedVersion    uint64
	activeElement      dom.NodeID
	hoveredElement     dom.NodeID
	pressedElement     dom.NodeID
	focusVisible       bool
	nextNavigation     NavigationID
	navigation         navigationRecord
	nextTimer          TimerID
	timers             map[TimerID]*pageTimer
	closed             bool
}

// computedStyleState records the browser-owned inputs that make one immutable
// style snapshot current. Document generations remain a browser concern; the
// style package deliberately owns neither navigation nor invalidation state.
type computedStyleState struct {
	snapshot        *computed.Snapshot
	document        DocumentGeneration
	documentVersion uint64
	styleRevision   uint64
}

// layoutState records the browser inputs that make one immutable layout
// snapshot current. Layout has its own revision because decoded image arrivals
// can change used geometry without changing computed style.
type layoutState struct {
	snapshot        *render.LayoutSnapshot
	document        DocumentGeneration
	documentVersion uint64
	styleRevision   uint64
	layoutRevision  uint64
}

func newPage(
	browser *Browser,
	id PageID,
	realm *browserruntime.Realm,
	script JSRealm,
	document *dom.Document,
	generation DocumentGeneration,
	location *url.URL,
) (*Page, error) {
	lifetimes, err := newNodeLifetimeState(realm, document, generation)
	if err != nil {
		return nil, err
	}
	documentContext, documentCancel := context.WithCancel(context.Background())
	page := &Page{
		ID:                 id,
		Realm:              realm,
		browser:            browser,
		script:             script,
		document:           document,
		nodeLifetimes:      lifetimes,
		documentGeneration: generation,
		children:           make(map[dom.NodeID]*Page),
		location:           cloneURL(location),
		readyState:         "complete",
		resources:          newPageResources(),
		documentContext:    documentContext,
		documentCancel:     documentCancel,
		viewport:           render.DefaultViewport,
		elementScroll:      make(map[dom.NodeID]scrollOffset),
		timeOrigin:         time.Now(),
		animationFrames:    make(map[AnimationFrameID]*pageAnimationFrame),
		styleRevision:      1,
		layoutRevision:     1,
		timers:             make(map[TimerID]*pageTimer),
		dirty:              true,
		historyIndex:       -1,
	}
	if location != nil {
		page.history = append(page.history, HistoryEntry{URL: cloneURL(location)})
		page.historyIndex = 0
	}
	return page, nil
}

func (page *Page) Document() *dom.Document {
	if page == nil {
		return nil
	}
	page.mutex.RLock()
	document := page.document
	page.mutex.RUnlock()
	return document
}

func (page *Page) URL() *url.URL {
	if page == nil {
		return nil
	}
	page.mutex.RLock()
	location := cloneURL(page.location)
	page.mutex.RUnlock()
	return location
}

func (page *Page) SetResources(resources render.Resources) error {
	if page == nil {
		return fmt.Errorf("browser: nil page")
	}
	page.mutex.Lock()
	defer page.mutex.Unlock()
	if page.closed {
		return ErrPageClosed
	}
	stable, err := pageResourcesFromRenderer(page.document, resources)
	if err != nil {
		return err
	}
	page.resources = stable
	page.invalidateStyleLocked()
	return nil
}

func (page *Page) SetViewport(viewport render.Viewport) error {
	if page == nil {
		return fmt.Errorf("browser: nil page")
	}
	if viewport.Width <= 0 || viewport.Height <= 0 {
		return fmt.Errorf("browser: invalid viewport %dx%d", viewport.Width, viewport.Height)
	}
	page.mutex.Lock()
	defer page.mutex.Unlock()
	if page.closed {
		return ErrPageClosed
	}
	page.setViewportLocked(viewport)
	return nil
}

func (page *Page) setViewportLocked(viewport render.Viewport) bool {
	if page.viewport != viewport {
		page.viewport = viewport
		page.invalidateStyleLocked()
		return true
	}
	return false
}

func (page *Page) Frame() *render.Frame {
	if page == nil {
		return nil
	}
	page.mutex.RLock()
	frame := page.frame
	page.mutex.RUnlock()
	return frame
}

func (page *Page) Dirty() bool {
	if page == nil {
		return false
	}
	page.mutex.RLock()
	dirty := page.dirty || page.renderedVersion != page.document.Version()
	page.mutex.RUnlock()
	return dirty
}

// NodeFacadeRef returns the canonical document-region HostObject for one
// generation-safe native node. JavaScript engines keep wrapper identity in
// their own heaps; this Ref is the Go-owned half of that wrapper boundary.
func (page *Page) NodeFacadeRef(handle NodeHandle) (memory.Ref, error) {
	if page == nil {
		return memory.Ref{}, fmt.Errorf("browser: nil page")
	}
	page.mutex.RLock()
	defer page.mutex.RUnlock()
	if page.closed {
		return memory.Ref{}, ErrPageClosed
	}
	if page.nodeLifetimes == nil || handle.Document != page.documentGeneration {
		return memory.Ref{}, ErrStaleNodeHandle
	}
	return page.nodeLifetimes.facade(handle)
}

// RetainNodeWrapper adds one numeric wrapper root to the current document's
// wrapper region. Repeated retention is idempotent because the engine cache
// creates at most one weak wrapper per NodeHandle.
func (page *Page) RetainNodeWrapper(handle NodeHandle) error {
	if page == nil {
		return fmt.Errorf("browser: nil page")
	}
	page.mutex.Lock()
	defer page.mutex.Unlock()
	if page.closed {
		return ErrPageClosed
	}
	if page.nodeLifetimes == nil || handle.Document != page.documentGeneration {
		return ErrStaleNodeHandle
	}
	if _, ok := page.document.Resolve(handle.Node); !ok {
		return dom.ErrUnknownNode
	}
	return page.nodeLifetimes.retainWrapper(handle)
}

// ReleaseNodeWrappers removes weak wrappers reported by an engine GC and
// reclaims detached Go nodes whose wrapper claim was their final owner. A live
// listener claim keeps the target rooted independently.
func (page *Page) ReleaseNodeWrappers(handles []NodeHandle) error {
	if page == nil {
		return fmt.Errorf("browser: nil page")
	}
	page.mutex.Lock()
	defer page.mutex.Unlock()
	if page.closed {
		return ErrPageClosed
	}
	if page.nodeLifetimes == nil {
		return nil
	}
	return page.nodeLifetimes.releaseWrappers(handles)
}

// RetainNodeEventTarget adds one listener claim for a native EventTarget.
// Repeated registrations are counted so removing one event type cannot release
// a target that still owns other listeners.
func (page *Page) RetainNodeEventTarget(handle NodeHandle) error {
	if page == nil {
		return fmt.Errorf("browser: nil page")
	}
	page.mutex.Lock()
	defer page.mutex.Unlock()
	if page.closed {
		return ErrPageClosed
	}
	if page.nodeLifetimes == nil || handle.Document != page.documentGeneration {
		return ErrStaleNodeHandle
	}
	if _, ok := page.document.Resolve(handle.Node); !ok {
		return dom.ErrUnknownNode
	}
	return page.nodeLifetimes.retainEventTarget(handle)
}

// ReleaseNodeEventTarget removes one listener claim and reconciles detached
// reachability when the target's final listener disappears.
func (page *Page) ReleaseNodeEventTarget(handle NodeHandle) error {
	if page == nil {
		return fmt.Errorf("browser: nil page")
	}
	page.mutex.Lock()
	defer page.mutex.Unlock()
	if page.closed {
		return ErrPageClosed
	}
	if page.nodeLifetimes == nil || handle.Document != page.documentGeneration {
		return ErrStaleNodeHandle
	}
	return page.nodeLifetimes.releaseEventTarget(handle)
}

// Render synchronously renders the current document through the existing
// style/layout/display-list pipeline.
func (page *Page) Render() error {
	if page == nil {
		return fmt.Errorf("browser: nil page")
	}
	page.mutex.Lock()
	defer page.mutex.Unlock()
	return page.renderLocked(false)
}

// QueueTextMutation schedules a stable-ID DOM mutation followed by a separate
// render task. A fake invalidation object is carried across the queue boundary
// so the ownership trace covers the complete mutation-to-render lifecycle.
func (page *Page) QueueTextMutation(node dom.NodeID, data string) (browserruntime.TaskID, error) {
	if page == nil {
		return 0, fmt.Errorf("browser: nil page")
	}
	page.mutex.RLock()
	handle := NodeHandle{Document: page.documentGeneration, Node: node}
	page.mutex.RUnlock()
	return page.QueueTextMutationHandle(handle, data)
}

// QueueTextMutationHandle preserves document identity across the delay between
// enqueue and execution, preventing a reused NodeID from targeting a later
// navigation's document.
func (page *Page) QueueTextMutationHandle(handle NodeHandle, data string) (browserruntime.TaskID, error) {
	if page == nil {
		return 0, fmt.Errorf("browser: nil page")
	}
	return page.Realm.EnqueueTask(func(context *browserruntime.TaskContext) error {
		page.mutex.Lock()
		if page.closed {
			page.mutex.Unlock()
			return ErrPageClosed
		}
		if page.documentGeneration != handle.Document {
			page.mutex.Unlock()
			return ErrStaleNodeHandle
		}
		if err := page.document.SetText(handle.Node, data); err != nil {
			page.mutex.Unlock()
			return err
		}
		page.dirty = true
		page.mutex.Unlock()

		return page.queueRenderFromTask(context)
	})
}

func (page *Page) QueueRender() (browserruntime.TaskID, error) {
	if page == nil {
		return 0, fmt.Errorf("browser: nil page")
	}
	return page.Realm.EnqueueTask(func(*browserruntime.TaskContext) error {
		page.mutex.Lock()
		defer page.mutex.Unlock()
		return page.renderLocked(true)
	})
}

func (page *Page) queueRenderFromTask(context *browserruntime.TaskContext) error {
	page.mutex.RLock()
	generation := page.documentGeneration
	page.mutex.RUnlock()
	invalidation, err := context.NewObject()
	if err != nil {
		return err
	}
	_, err = context.QueueTask(func(*browserruntime.TaskContext) error {
		page.mutex.Lock()
		defer page.mutex.Unlock()
		if page.documentGeneration != generation {
			return nil
		}
		return page.renderLocked(true)
	}, invalidation)
	return err
}

func (page *Page) Close() error {
	if page == nil {
		return nil
	}
	page.mutex.Lock()
	if page.closed {
		page.mutex.Unlock()
		return nil
	}
	page.closed = true
	if page.navigation.cancel != nil {
		page.navigation.cancel()
		page.navigation.cancel = nil
	}
	documentCancel := page.documentCancel
	page.documentCancel = nil
	page.documentContext = nil
	page.frame = nil
	page.scrollX = 0
	page.scrollY = 0
	page.elementScroll = nil
	page.computedStyle = computedStyleState{}
	page.layout = layoutState{}
	timers := page.takeTimersLocked()
	animationRefs := page.takeAnimationFramesLocked()
	script := page.script
	page.script = nil
	lifetimes := page.nodeLifetimes
	page.nodeLifetimes = nil
	children := page.takeChildFramesLocked()
	parent := page.parent
	page.parent = nil
	generation := page.documentGeneration
	page.mutex.Unlock()
	if parent != nil {
		parent.removeChildFrame(page)
	}
	if page.browser != nil {
		page.browser.unregisterPage(page, generation)
	}
	if documentCancel != nil {
		documentCancel()
	}
	timerErr := page.releaseTimers(timers)
	animationErr := page.releaseAnimationFrames(animationRefs)
	var childErr error
	for _, child := range children {
		childErr = errors.Join(childErr, child.Close())
	}
	var scriptErr error
	if script != nil {
		scriptErr = script.Close()
	}
	var lifetimeErr error
	if lifetimes != nil {
		lifetimeErr = lifetimes.close()
	}
	return errors.Join(timerErr, animationErr, childErr, scriptErr, lifetimeErr, page.Realm.Close())
}

func (page *Page) renderLocked(onlyIfDirty bool) error {
	if page.closed {
		return ErrPageClosed
	}
	if onlyIfDirty && !page.dirty && page.renderedVersion == page.document.Version() {
		return nil
	}
	if _, err := page.syncStylesheetsLocked(); err != nil {
		return err
	}
	resources := page.resources.rendererResources(page.document)
	var frame *render.Frame
	var renderedVersion uint64
	err := page.document.WithReadView(func(view dom.ReadView) error {
		snapshot, snapshotErr := page.styleSnapshotForViewLocked(view, resources)
		if snapshotErr != nil {
			return snapshotErr
		}
		layout, layoutErr := page.layoutSnapshotForViewLocked(view, resources, snapshot)
		if layoutErr != nil {
			return layoutErr
		}
		var renderErr error
		frame, renderErr = render.RenderReadViewWithLayoutSnapshot(view, layout)
		if renderErr == nil {
			renderedVersion = view.Version()
		}
		return renderErr
	})
	if err != nil {
		return err
	}
	page.frame = render.TransformDisplayList(frame, page.visualTransformsLocked(frame))
	page.frameGeneration = page.documentGeneration
	page.renderedVersion = renderedVersion
	page.dirty = false
	return nil
}

// ComputedStyle synchronously flushes style computation for handle without
// running layout or publishing a new Frame. The Page remains render-dirty so a
// JavaScript task can observe its own style mutations immediately while the
// normal task-boundary render stays coalesced.
func (page *Page) ComputedStyle(handle NodeHandle) (computed.ComputedStyle, error) {
	if page == nil {
		return computed.ComputedStyle{}, fmt.Errorf("browser: nil page")
	}
	page.mutex.Lock()
	defer page.mutex.Unlock()
	if page.closed {
		return computed.ComputedStyle{}, ErrPageClosed
	}
	if handle.Document == 0 || handle.Node == dom.InvalidNodeID || handle.Document != page.documentGeneration {
		return computed.ComputedStyle{}, ErrStaleNodeHandle
	}

	if _, err := page.syncStylesheetsLocked(); err != nil {
		return computed.ComputedStyle{}, err
	}
	resources := page.resources.rendererResources(page.document)
	var result computed.ComputedStyle
	err := page.document.WithReadView(func(view dom.ReadView) error {
		node, ok := view.Resolve(handle.Node)
		if !ok {
			return fmt.Errorf("%w: %d", dom.ErrUnknownNode, handle.Node)
		}
		if node.Type != dom.ElementNode {
			return fmt.Errorf("%w: node %d is %d, want element", dom.ErrWrongNodeKind, handle.Node, node.Type)
		}
		snapshot, snapshotErr := page.styleSnapshotForViewLocked(view, resources)
		if snapshotErr != nil {
			return snapshotErr
		}
		var found bool
		result, found = snapshot.LookupID(handle.Node)
		if !found {
			return fmt.Errorf("%w: node %d", ErrComputedStyleUnavailable, handle.Node)
		}
		return nil
	})
	return result, err
}

// ComputedPseudoStyle synchronously returns the layout-independent style of a
// supported generated pseudo-element. It shares the element style snapshot
// cache and does not publish a frame.
func (page *Page) ComputedPseudoStyle(handle NodeHandle, pseudo computed.PseudoElement) (computed.ComputedStyle, error) {
	if page == nil {
		return computed.ComputedStyle{}, fmt.Errorf("browser: nil page")
	}
	if pseudo == computed.PseudoElementNone {
		return page.ComputedStyle(handle)
	}
	page.mutex.Lock()
	defer page.mutex.Unlock()
	if page.closed {
		return computed.ComputedStyle{}, ErrPageClosed
	}
	if handle.Document == 0 || handle.Node == dom.InvalidNodeID || handle.Document != page.documentGeneration {
		return computed.ComputedStyle{}, ErrStaleNodeHandle
	}
	if _, err := page.syncStylesheetsLocked(); err != nil {
		return computed.ComputedStyle{}, err
	}
	resources := page.resources.rendererResources(page.document)
	var result computed.ComputedStyle
	err := page.document.WithReadView(func(view dom.ReadView) error {
		node, ok := view.Resolve(handle.Node)
		if !ok {
			return fmt.Errorf("%w: %d", dom.ErrUnknownNode, handle.Node)
		}
		if node.Type != dom.ElementNode {
			return fmt.Errorf("%w: node %d is %d, want element", dom.ErrWrongNodeKind, handle.Node, node.Type)
		}
		snapshot, snapshotErr := page.styleSnapshotForViewLocked(view, resources)
		if snapshotErr != nil {
			return snapshotErr
		}
		var found bool
		result, found = snapshot.LookupPseudoID(handle.Node, pseudo)
		if !found {
			return fmt.Errorf("%w: node %d", ErrComputedStyleUnavailable, handle.Node)
		}
		return nil
	})
	return result, err
}

// ComputedStyleProperty synchronously returns one computed-style property and
// overlays layout-backed used width/height values when the element has a
// principal box. It does not publish a Frame or clear Page dirtiness.
func (page *Page) ComputedStyleProperty(handle NodeHandle, property string) (string, bool, error) {
	if page == nil {
		return "", false, fmt.Errorf("browser: nil page")
	}
	page.mutex.Lock()
	defer page.mutex.Unlock()
	if page.closed {
		return "", false, ErrPageClosed
	}
	if handle.Document == 0 || handle.Node == dom.InvalidNodeID || handle.Document != page.documentGeneration {
		return "", false, ErrStaleNodeHandle
	}

	if _, err := page.syncStylesheetsLocked(); err != nil {
		return "", false, err
	}
	resources := page.resources.rendererResources(page.document)
	var value string
	var found bool
	err := page.document.WithReadView(func(view dom.ReadView) error {
		node, ok := view.Resolve(handle.Node)
		if !ok {
			return fmt.Errorf("%w: %d", dom.ErrUnknownNode, handle.Node)
		}
		if node.Type != dom.ElementNode {
			return fmt.Errorf("%w: node %d is %d, want element", dom.ErrWrongNodeKind, handle.Node, node.Type)
		}
		styleSnapshot, snapshotErr := page.styleSnapshotForViewLocked(view, resources)
		if snapshotErr != nil {
			return snapshotErr
		}
		computedStyle, available := styleSnapshot.LookupID(handle.Node)
		if !available {
			return fmt.Errorf("%w: node %d", ErrComputedStyleUnavailable, handle.Node)
		}
		value, found = computed.ComputedPropertyValue(computedStyle, property)
		if !found {
			return nil
		}

		canonical := lowerASCIIProperty(property)
		if canonical != "width" && canonical != "height" {
			return nil
		}
		layout, layoutErr := page.layoutSnapshotForViewLocked(view, resources, styleSnapshot)
		if layoutErr != nil {
			return layoutErr
		}
		geometry, hasGeometry := layout.GeometryID(handle.Node)
		if !hasGeometry {
			return nil
		}
		if canonical == "height" && computedStyle.Height().DependsOnPercent() && !geometry.PercentHeightResolved {
			// A percentage height in an indefinite-height containing block
			// computes as auto for layout, so CSSOM retains the computed token.
			return nil
		}
		used := geometry.ContentBounds.Width
		if canonical == "height" {
			used = geometry.ContentBounds.Height
		}
		if computedStyle.BoxSizing() == computed.BoxSizingBorderBox {
			if canonical == "width" {
				used = geometry.Bounds.Width
			} else {
				used = geometry.Bounds.Height
			}
		}
		value = serializeUsedPixels(used)
		return nil
	})
	return value, found, err
}

// ComputedPseudoStyleProperty returns a supported generated pseudo-element's
// property, overlaying width/height from its retained block box when present.
// Inline generated content retains its layout-independent computed values.
func (page *Page) ComputedPseudoStyleProperty(handle NodeHandle, pseudo computed.PseudoElement, property string) (string, bool, error) {
	if pseudo == computed.PseudoElementNone {
		return page.ComputedStyleProperty(handle, property)
	}
	if page == nil {
		return "", false, fmt.Errorf("browser: nil page")
	}
	page.mutex.Lock()
	defer page.mutex.Unlock()
	if page.closed {
		return "", false, ErrPageClosed
	}
	if handle.Document == 0 || handle.Node == dom.InvalidNodeID || handle.Document != page.documentGeneration {
		return "", false, ErrStaleNodeHandle
	}
	if _, err := page.syncStylesheetsLocked(); err != nil {
		return "", false, err
	}
	resources := page.resources.rendererResources(page.document)
	var value string
	var found bool
	err := page.document.WithReadView(func(view dom.ReadView) error {
		node, ok := view.Resolve(handle.Node)
		if !ok {
			return fmt.Errorf("%w: %d", dom.ErrUnknownNode, handle.Node)
		}
		if node.Type != dom.ElementNode {
			return fmt.Errorf("%w: node %d is %d, want element", dom.ErrWrongNodeKind, handle.Node, node.Type)
		}
		styleSnapshot, snapshotErr := page.styleSnapshotForViewLocked(view, resources)
		if snapshotErr != nil {
			return snapshotErr
		}
		pseudoStyle, available := styleSnapshot.LookupPseudoID(handle.Node, pseudo)
		if !available {
			return fmt.Errorf("%w: node %d", ErrComputedStyleUnavailable, handle.Node)
		}
		value, found = computed.ComputedPropertyValue(pseudoStyle, property)
		if !found {
			return nil
		}
		canonical := lowerASCIIProperty(property)
		if canonical != "width" && canonical != "height" {
			return nil
		}
		layout, layoutErr := page.layoutSnapshotForViewLocked(view, resources, styleSnapshot)
		if layoutErr != nil {
			return layoutErr
		}
		geometry, hasGeometry := layout.PseudoGeometryID(handle.Node, pseudo)
		if !hasGeometry {
			return nil
		}
		if canonical == "height" && pseudoStyle.Height().DependsOnPercent() && !geometry.PercentHeightResolved {
			return nil
		}
		used := geometry.ContentBounds.Width
		if canonical == "height" {
			used = geometry.ContentBounds.Height
		}
		if pseudoStyle.BoxSizing() == computed.BoxSizingBorderBox {
			if canonical == "width" {
				used = geometry.Bounds.Width
			} else {
				used = geometry.Bounds.Height
			}
		}
		value = serializeUsedPixels(used)
		return nil
	})
	return value, found, err
}

// styleSnapshotForViewLocked returns the snapshot current for one coherent DOM
// read. The caller must hold Page.mutex and invoke it from Document.WithReadView.
func (page *Page) styleSnapshotForViewLocked(view dom.ReadView, resources render.Resources) (*computed.Snapshot, error) {
	state := page.computedStyle
	if state.snapshot != nil &&
		state.document == page.documentGeneration &&
		state.documentVersion == view.Version() &&
		state.styleRevision == page.styleRevision {
		return state.snapshot, nil
	}
	inlineDeclarations, err := page.resources.inlineStyles.declarationsForView(view)
	if err != nil {
		return nil, err
	}
	resources.InlineDeclarations = inlineDeclarations

	selectorState, err := page.selectorStateForViewLocked(view)
	if err != nil {
		return nil, err
	}
	resources.SelectorState = selectorState
	snapshot, err := render.ComputeStyleSnapshotFromReadView(view, page.viewport, resources)
	if err != nil {
		return nil, err
	}
	page.computedStyle = computedStyleState{
		snapshot:        snapshot,
		document:        page.documentGeneration,
		documentVersion: view.Version(),
		styleRevision:   page.styleRevision,
	}
	return snapshot, nil
}

func (page *Page) selectorStateForViewLocked(view dom.ReadView) (computed.SelectorState, error) {
	state := computed.SelectorState{
		Hovered:         page.hoveredElement,
		Active:          page.pressedElement,
		Focused:         page.activeElement,
		FocusVisible:    page.focusVisible,
		DefaultLanguage: page.documentLanguage,
	}
	if page.location != nil && page.location.Fragment != "" {
		state.TargetID = page.location.Fragment
	}
	visited, err := page.visitedLinkIDsForViewLocked(view)
	if err != nil {
		return computed.SelectorState{}, err
	}
	state.Visited = visited
	return state, nil
}

// layoutSnapshotForViewLocked returns the current non-published layout result
// for one coherent DOM read. The caller must hold Page.mutex and invoke it from
// Document.WithReadView.
func (page *Page) layoutSnapshotForViewLocked(view dom.ReadView, resources render.Resources, styles *computed.Snapshot) (*render.LayoutSnapshot, error) {
	state := page.layout
	if state.snapshot != nil &&
		state.document == page.documentGeneration &&
		state.documentVersion == view.Version() &&
		state.styleRevision == page.styleRevision &&
		state.layoutRevision == page.layoutRevision &&
		state.snapshot.ComputedStyles() == styles {
		return state.snapshot, nil
	}

	snapshot, err := render.ComputeLayoutSnapshotFromReadView(view, page.viewport, resources, styles)
	if err != nil {
		return nil, err
	}
	page.layout = layoutState{
		snapshot:        snapshot,
		document:        page.documentGeneration,
		documentVersion: view.Version(),
		styleRevision:   page.styleRevision,
		layoutRevision:  page.layoutRevision,
	}
	return snapshot, nil
}

func (page *Page) invalidateStyleLocked() {
	page.styleRevision++
	page.layoutRevision++
	page.dirty = true
}

func (page *Page) invalidateLayoutLocked() {
	page.layoutRevision++
	page.dirty = true
}

func lowerASCIIProperty(source string) string {
	result := []byte(source)
	for index, value := range result {
		if value >= 'A' && value <= 'Z' {
			result[index] = value + ('a' - 'A')
		}
	}
	return string(result)
}

func serializeUsedPixels(value float64) string {
	if value == 0 {
		return "0px"
	}
	return strconv.FormatFloat(value, 'f', -1, 64) + "px"
}

var ErrStaleNodeHandle = errors.New("browser: node handle belongs to a previous document")

var ErrComputedStyleUnavailable = errors.New("browser: computed style is unavailable for node")

func cloneURL(source *url.URL) *url.URL {
	if source == nil {
		return nil
	}
	clone := *source
	return &clone
}
