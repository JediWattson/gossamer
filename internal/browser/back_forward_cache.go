package browser

import (
	"context"
	"errors"
	"net/url"
	"time"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
	"github.com/JediWattson/gossamer/internal/resource"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
)

const maximumBackForwardCacheEntries = 2

type cachedDocumentState struct {
	group            uint64
	document         *dom.Document
	nodeLifetimes    *nodeLifetimeState
	script           JSRealm
	generation       DocumentGeneration
	documentLanguage string
	location         *url.URL
	readyState       string
	resources        pageResources
	resourceFetcher  resource.Fetcher
	documentContext  context.Context
	documentCancel   context.CancelFunc
	scrollX          float64
	scrollY          float64
	elementScroll    map[dom.NodeID]scrollOffset
	timeOrigin       time.Time
	lastFrameTime    float64
	frame            *render.Frame
	frameGeneration  DocumentGeneration
	computedStyle    computedStyleState
	styleRevision    uint64
	layout           layoutState
	layoutRevision   uint64
	dirty            bool
	renderedVersion  uint64
	activeElement    dom.NodeID
	hoveredElement   dom.NodeID
	pressedElement   dom.NodeID
	focusVisible     bool
}

func (page *Page) BackForwardCacheSize() int {
	if page == nil {
		return 0
	}
	page.mutex.RLock()
	size := len(page.backForwardCache)
	page.mutex.RUnlock()
	return size
}

// EvictBackForwardCache releases every suspended document region. Embedders
// can call it under memory pressure without disturbing the current document.
func (page *Page) EvictBackForwardCache() error {
	if page == nil {
		return nil
	}
	page.mutex.Lock()
	cached := page.takeAllCachedDocumentsLocked()
	page.backForwardCache = make(map[uint64]*cachedDocumentState)
	page.mutex.Unlock()
	return closeCachedDocuments(cached)
}

func (page *Page) canCacheCurrentDocument() bool {
	page.mutex.RLock()
	if page.closed || page.historyDocument == 0 || len(page.timers) != 0 ||
		len(page.animationFrames) != 0 || len(page.children) != 0 ||
		page.Realm.Tasks.Len() != 0 {
		page.mutex.RUnlock()
		return false
	}
	script := page.script
	page.mutex.RUnlock()
	if script == nil {
		return true
	}
	eligible, ok := script.(JSBackForwardCacheRealm)
	return ok && eligible.BFCacheEligible()
}

func (page *Page) captureCurrentDocumentLocked() *cachedDocumentState {
	return &cachedDocumentState{
		group: page.historyDocument, document: page.document,
		nodeLifetimes: page.nodeLifetimes, script: page.script,
		generation: page.documentGeneration, documentLanguage: page.documentLanguage,
		location: cloneURL(page.location), readyState: page.readyState,
		resources: page.resources, resourceFetcher: page.resourceFetcher,
		documentContext: page.documentContext, documentCancel: page.documentCancel,
		scrollX: page.scrollX, scrollY: page.scrollY,
		elementScroll: cloneElementScroll(page.elementScroll),
		timeOrigin:    page.timeOrigin, lastFrameTime: page.lastFrameTime,
		frame: page.frame, frameGeneration: page.frameGeneration,
		computedStyle: page.computedStyle, styleRevision: page.styleRevision,
		layout: page.layout, layoutRevision: page.layoutRevision,
		dirty: page.dirty, renderedVersion: page.renderedVersion,
		activeElement: page.activeElement, hoveredElement: page.hoveredElement,
		pressedElement: page.pressedElement, focusVisible: page.focusVisible,
	}
}

func cloneElementScroll(source map[dom.NodeID]scrollOffset) map[dom.NodeID]scrollOffset {
	if source == nil {
		return nil
	}
	clone := make(map[dom.NodeID]scrollOffset, len(source))
	for node, offset := range source {
		clone[node] = offset
	}
	return clone
}

func (page *Page) storeCachedDocumentLocked(cached *cachedDocumentState) []*cachedDocumentState {
	if cached == nil || cached.group == 0 {
		return nil
	}
	var retired []*cachedDocumentState
	if previous := page.backForwardCache[cached.group]; previous != nil {
		retired = append(retired, previous)
		page.removeCachedOrderLocked(cached.group)
	}
	page.backForwardCache[cached.group] = cached
	page.backForwardOrder = append(page.backForwardOrder, cached.group)
	for len(page.backForwardOrder) > maximumBackForwardCacheEntries {
		group := page.backForwardOrder[0]
		page.backForwardOrder = page.backForwardOrder[1:]
		if evicted := page.backForwardCache[group]; evicted != nil {
			delete(page.backForwardCache, group)
			retired = append(retired, evicted)
		}
	}
	return retired
}

func (page *Page) takeCachedDocumentLocked(group uint64) *cachedDocumentState {
	cached := page.backForwardCache[group]
	if cached == nil {
		return nil
	}
	delete(page.backForwardCache, group)
	page.removeCachedOrderLocked(group)
	return cached
}

func (page *Page) removeCachedOrderLocked(group uint64) {
	for index, candidate := range page.backForwardOrder {
		if candidate == group {
			page.backForwardOrder = append(page.backForwardOrder[:index], page.backForwardOrder[index+1:]...)
			return
		}
	}
}

func (page *Page) takeAllCachedDocumentsLocked() []*cachedDocumentState {
	result := make([]*cachedDocumentState, 0, len(page.backForwardCache))
	for _, group := range page.backForwardOrder {
		if cached := page.backForwardCache[group]; cached != nil {
			result = append(result, cached)
		}
	}
	page.backForwardCache = nil
	page.backForwardOrder = nil
	return result
}

func closeCachedDocuments(cached []*cachedDocumentState) error {
	var err error
	for _, state := range cached {
		if state == nil {
			continue
		}
		if state.documentCancel != nil {
			state.documentCancel()
		}
		if state.script != nil {
			err = errors.Join(err, state.script.Close())
		}
		if state.nodeLifetimes != nil {
			err = errors.Join(err, state.nodeLifetimes.close())
		}
	}
	return err
}

func (page *Page) beginBackForwardCacheTraversal(ctx context.Context, source, target int) (NavigationID, error) {
	navigationContext, cancel := context.WithCancel(ctx)
	page.mutex.Lock()
	if page.closed {
		page.mutex.Unlock()
		cancel()
		return 0, ErrPageClosed
	}
	if page.historyIndex != source || target < 0 || target >= len(page.history) {
		page.mutex.Unlock()
		cancel()
		return 0, ErrHistoryChanged
	}
	group := page.history[target].DocumentSequence
	cached := page.backForwardCache[group]
	if group == 0 || cached == nil {
		page.mutex.Unlock()
		cancel()
		return 0, ErrHistoryChanged
	}
	if page.navigation.cancel != nil {
		page.navigation.cancel()
	}
	page.nextNavigation++
	id := page.nextNavigation
	page.navigation = navigationRecord{
		id: id, requestedURL: cloneURL(page.history[target].URL), state: NavigationRendering,
		documentGeneration: page.documentGeneration, context: navigationContext,
		cancel: cancel, historySource: source, historyTarget: target,
	}
	page.mutex.Unlock()

	_, _, err := page.browser.scheduler.EnqueueExternalTask(page.Realm, func(task *browserruntime.TaskContext) error {
		return page.restoreBackForwardCacheDocument(task, id, source, target, group)
	})
	if err != nil {
		cancel()
	}
	return id, err
}

func (page *Page) restoreBackForwardCacheDocument(
	task *browserruntime.TaskContext,
	id NavigationID,
	source, target int,
	group uint64,
) error {
	cacheDeparture := page.canCacheCurrentDocument()
	allowed, departureErr := page.dispatchDepartureLifecycle(task, id, cacheDeparture)
	if !allowed {
		page.mutex.Lock()
		if page.matchesNavigationLocked(id, 0) {
			page.cancelNavigationLocked(departureErr)
		}
		page.mutex.Unlock()
		return nil
	}

	page.mutex.Lock()
	if !page.matchesNavigationLocked(id, 0) || page.historyIndex != source ||
		target < 0 || target >= len(page.history) ||
		page.history[target].DocumentSequence != group {
		page.mutex.Unlock()
		return nil
	}
	restored := page.takeCachedDocumentLocked(group)
	if restored == nil {
		page.failNavigationLocked(ErrHistoryChanged)
		page.mutex.Unlock()
		return nil
	}

	oldGeneration := page.documentGeneration
	oldScript := page.script
	oldLifetimes := page.nodeLifetimes
	oldDocumentCancel := page.documentCancel
	timers := page.takeTimersLocked()
	webSockets := page.takeWebSocketsLocked()
	animationRefs := page.takeAnimationFramesLocked()
	children := page.takeChildFramesLocked()
	var retiredCachedDocuments []*cachedDocumentState
	if cacheDeparture {
		retiredCachedDocuments = page.storeCachedDocumentLocked(page.captureCurrentDocumentLocked())
		oldScript = nil
		oldLifetimes = nil
		oldDocumentCancel = nil
	}

	oldURL := cloneURL(restored.location)
	destination := cloneHistoryEntry(page.history[target])
	page.document = restored.document
	page.nodeLifetimes = restored.nodeLifetimes
	page.script = restored.script
	page.documentGeneration = restored.generation
	page.documentLanguage = restored.documentLanguage
	page.location = cloneURL(destination.URL)
	page.readyState = restored.readyState
	page.resources = restored.resources
	page.resourceFetcher = restored.resourceFetcher
	page.documentContext = restored.documentContext
	page.documentCancel = restored.documentCancel
	page.scrollX = restored.scrollX
	page.scrollY = restored.scrollY
	page.elementScroll = cloneElementScroll(restored.elementScroll)
	page.timeOrigin = restored.timeOrigin
	page.lastFrameTime = restored.lastFrameTime
	page.frame = restored.frame
	page.frameGeneration = restored.frameGeneration
	page.computedStyle = restored.computedStyle
	page.styleRevision = restored.styleRevision
	page.layout = restored.layout
	page.layoutRevision = restored.layoutRevision
	page.dirty = restored.dirty
	page.renderedVersion = restored.renderedVersion
	page.activeElement = restored.activeElement
	page.hoveredElement = restored.hoveredElement
	page.pressedElement = restored.pressedElement
	page.focusVisible = restored.focusVisible
	page.historyDocument = group
	page.historyIndex = target
	for index := range page.history {
		if page.history[index].DocumentSequence == group {
			page.history[index].DocumentGeneration = restored.generation
		}
	}
	page.navigation.documentGeneration = restored.generation
	page.browser.replaceDocument(page, oldGeneration, restored.generation)
	page.mutex.Unlock()
	closeWebSockets(webSockets)

	if oldDocumentCancel != nil {
		oldDocumentCancel()
	}
	cleanupErr := page.releaseTimers(timers)
	cleanupErr = errors.Join(cleanupErr, page.releaseAnimationFrames(animationRefs))
	for _, child := range children {
		cleanupErr = errors.Join(cleanupErr, child.Close())
	}
	if oldScript != nil {
		cleanupErr = errors.Join(cleanupErr, oldScript.Close())
	}
	if oldLifetimes != nil {
		cleanupErr = errors.Join(cleanupErr, oldLifetimes.close())
	}
	cleanupErr = errors.Join(cleanupErr, closeCachedDocuments(retiredCachedDocuments))
	if cleanupErr != nil {
		page.mutex.Lock()
		if page.matchesNavigationLocked(id, restored.generation) {
			page.failNavigationLocked(cleanupErr)
		}
		page.mutex.Unlock()
		return cleanupErr
	}

	host := &taskHost{page: page, task: task, generation: restored.generation, autoRender: true}
	if restored.script != nil {
		_, lifecycleErr := restored.script.DispatchEvent(host, InputEvent{
			Type: InputPageShow, Target: NodeHandle{Document: restored.generation}, Persisted: true,
		})
		_, popStateErr := restored.script.DispatchEvent(host, InputEvent{
			Type: InputPopState, Target: NodeHandle{Document: restored.generation}, Data: destination.StateJSON,
		})
		cleanupErr = errors.Join(cleanupErr, lifecycleErr, popStateErr)
		if oldURL != nil && destination.URL != nil && oldURL.Fragment != destination.URL.Fragment {
			_, hashErr := restored.script.DispatchEvent(host, InputEvent{
				Type: InputHashChange, Target: NodeHandle{Document: restored.generation},
				Key: oldURL.String(), Code: destination.URL.String(),
			})
			cleanupErr = errors.Join(cleanupErr, hashErr)
		}
		cleanupErr = errors.Join(cleanupErr, restored.script.DrainMicrotasks(host), host.finish())
	}
	page.mutex.Lock()
	if page.matchesNavigationLocked(id, restored.generation) {
		if cleanupErr != nil {
			page.failNavigationLocked(cleanupErr)
		} else {
			page.navigation.state = NavigationComplete
			page.navigation.err = nil
			if page.navigation.cancel != nil {
				page.navigation.cancel()
				page.navigation.cancel = nil
			}
		}
	}
	page.mutex.Unlock()
	return cleanupErr
}
