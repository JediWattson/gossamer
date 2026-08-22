package browser

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/JediWattson/gossamer/internal/dom"
	htmlparser "github.com/JediWattson/gossamer/internal/html"
	"github.com/JediWattson/gossamer/internal/loader"
	"github.com/JediWattson/gossamer/internal/resource"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
)

type NavigationID uint64
type DocumentGeneration uint64

// NodeHandle pairs document-scoped NodeID identity with the document
// generation that issued it. Async browser work must carry both values.
type NodeHandle struct {
	Document DocumentGeneration
	Node     dom.NodeID
}

type NavigationState uint8

const (
	NavigationIdle NavigationState = iota
	NavigationLoadingDocument
	NavigationLoadingResources
	NavigationLoadingScripts
	NavigationRendering
	NavigationComplete
	NavigationFailed
	NavigationCanceled
)

func (state NavigationState) terminal() bool {
	return state == NavigationComplete || state == NavigationFailed || state == NavigationCanceled
}

// NavigationSnapshot is an immutable view of the Page's current navigation.
type NavigationSnapshot struct {
	ID                 NavigationID
	RequestedURL       *url.URL
	URL                *url.URL
	State              NavigationState
	DocumentGeneration DocumentGeneration
	ResourcesTotal     int
	ResourcesPending   int
	ResourcesFailed    int
	ScriptsTotal       int
	ScriptsPending     int
	ScriptsFailed      int
	ScriptFailures     []ScriptFailure
	Err                error
}

// ScriptFailure is one bounded diagnostic captured while loading, evaluating,
// or dispatching lifecycle events for an initial-navigation script.
type ScriptFailure struct {
	URL     string `json:"url,omitempty"`
	Phase   string `json:"phase"`
	Message string `json:"message"`
}

type navigationRecord struct {
	id                 NavigationID
	requestedURL       *url.URL
	state              NavigationState
	documentGeneration DocumentGeneration
	resourcesTotal     int
	resourcesPending   int
	resourcesFailed    int
	scriptsTotal       int
	scriptsPending     int
	scriptsFailed      int
	scriptFailures     []ScriptFailure
	scripts            []navigationScript
	fetcher            resource.Fetcher
	err                error
	context            context.Context
	cancel             context.CancelFunc
	historySource      int
	historyTarget      int
	replaceCurrent     bool
}

type preparedNavigation struct {
	document        *dom.Document
	location        *url.URL
	defaultLanguage string
	requests        []navigationResourceRequest
	scripts         []navigationScript
	fetcher         resource.Fetcher
}

// Navigate begins document I/O outside the Realm. Parsed document state is
// committed only by the browser-owned completion task published afterward.
func (page *Page) Navigate(ctx context.Context, rawURL string, client DocumentLoader) (NavigationID, error) {
	return page.beginNavigation(ctx, rawURL, client, -1, -1, false)
}

// Back begins a document-rebuilding traversal to the preceding session
// history entry. The history index advances only when the replacement document
// commits successfully.
func (page *Page) Back(ctx context.Context, client DocumentLoader) (NavigationID, error) {
	return page.Go(ctx, -1, client)
}

// Forward begins a document-rebuilding traversal to the following session
// history entry.
func (page *Page) Forward(ctx context.Context, client DocumentLoader) (NavigationID, error) {
	return page.Go(ctx, 1, client)
}

// Reload rebuilds the current session history entry without appending a new
// entry.
func (page *Page) Reload(ctx context.Context, client DocumentLoader) (NavigationID, error) {
	return page.Go(ctx, 0, client)
}

// Go begins a session-history traversal relative to the current entry.
// Same-document entries retain the active document and Realm; cross-document
// entries currently rebuild through the ordinary navigation path.
func (page *Page) Go(ctx context.Context, delta int, client DocumentLoader) (NavigationID, error) {
	if page == nil {
		return 0, fmt.Errorf("browser: nil page")
	}
	if ctx == nil {
		return 0, fmt.Errorf("browser: nil navigation context")
	}
	page.mutex.RLock()
	if page.closed {
		page.mutex.RUnlock()
		return 0, ErrPageClosed
	}
	source := page.historyIndex
	target := source + delta
	if source < 0 || target < 0 || target >= len(page.history) || page.history[target].URL == nil {
		page.mutex.RUnlock()
		return 0, ErrHistoryTraversalOutOfRange
	}
	rawURL := page.history[target].URL.String()
	sameDocument := delta != 0 && page.history[target].DocumentSequence != 0 &&
		page.history[target].DocumentSequence == page.historyDocument
	cachedDocument := delta != 0 && page.history[target].DocumentSequence != 0 &&
		page.backForwardCache[page.history[target].DocumentSequence] != nil
	page.mutex.RUnlock()
	if sameDocument {
		return page.beginSameDocumentTraversal(ctx, source, target)
	}
	if cachedDocument {
		return page.beginBackForwardCacheTraversal(ctx, source, target)
	}
	return page.beginNavigation(ctx, rawURL, client, source, target, false)
}

func (page *Page) CanGoBack() bool {
	return page.canTraverseHistory(-1)
}

func (page *Page) CanGoForward() bool {
	return page.canTraverseHistory(1)
}

func (page *Page) canTraverseHistory(delta int) bool {
	if page == nil {
		return false
	}
	page.mutex.RLock()
	target := page.historyIndex + delta
	ok := !page.closed && page.historyIndex >= 0 && target >= 0 && target < len(page.history)
	page.mutex.RUnlock()
	return ok
}

func (page *Page) beginNavigation(
	ctx context.Context,
	rawURL string,
	client DocumentLoader,
	historySource int,
	historyTarget int,
	replaceCurrent bool,
) (NavigationID, error) {
	if page == nil {
		return 0, fmt.Errorf("browser: nil page")
	}
	if ctx == nil {
		return 0, fmt.Errorf("browser: nil navigation context")
	}
	requestedURL, err := loader.ParseHTTPURL(rawURL)
	if err != nil {
		return 0, err
	}
	navigationContext, cancel := context.WithCancel(ctx)

	page.mutex.Lock()
	if page.closed {
		page.mutex.Unlock()
		cancel()
		return 0, ErrPageClosed
	}
	if client == nil {
		client = page.navigationLoader
	}
	if client == nil {
		client = loader.New(nil)
	}
	page.navigationLoader = client
	if historyTarget >= 0 {
		if page.historyIndex != historySource || historyTarget >= len(page.history) ||
			page.history[historyTarget].URL == nil || page.history[historyTarget].URL.String() != requestedURL.String() {
			page.mutex.Unlock()
			cancel()
			return 0, ErrHistoryChanged
		}
	}
	if page.navigation.cancel != nil {
		page.navigation.cancel()
	}
	page.nextNavigation++
	id := page.nextNavigation
	page.navigation = navigationRecord{
		id:             id,
		requestedURL:   cloneURL(requestedURL),
		state:          NavigationLoadingDocument,
		context:        navigationContext,
		cancel:         cancel,
		historySource:  historySource,
		historyTarget:  historyTarget,
		replaceCurrent: replaceCurrent,
	}
	page.mutex.Unlock()

	go page.loadNavigationDocument(navigationContext, id, rawURL, client)
	return id, nil
}

// WaitNavigation drives the Page's ordered Realm until navigation reaches a
// terminal state. Embedders with their own actor loop may instead inspect
// Navigation and run the Realm directly.
func (page *Page) WaitNavigation(ctx context.Context, id NavigationID) error {
	if page == nil {
		return fmt.Errorf("browser: nil page")
	}
	if ctx == nil {
		return fmt.Errorf("browser: nil navigation context")
	}
	for {
		snapshot, err := page.navigationSnapshot(id)
		if err != nil {
			return err
		}
		if snapshot.State.terminal() {
			return snapshot.Err
		}
		runErr := page.Realm.RunOne(ctx)
		snapshot, snapshotErr := page.navigationSnapshot(id)
		if snapshotErr != nil {
			return snapshotErr
		}
		if snapshot.State.terminal() {
			return snapshot.Err
		}
		if runErr != nil {
			if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
				return page.cancelNavigationWait(id, snapshot.State, runErr)
			}
			return runErr
		}
	}
}

func (page *Page) cancelNavigationWait(id NavigationID, state NavigationState, waitErr error) error {
	wrapped := waitErr
	switch state {
	case NavigationLoadingDocument:
		wrapped = fmt.Errorf("browser: load document: %w", waitErr)
	case NavigationLoadingResources:
		wrapped = fmt.Errorf("browser: load page resources: %w", waitErr)
	case NavigationLoadingScripts:
		wrapped = fmt.Errorf("browser: load page scripts: %w", waitErr)
	case NavigationRendering:
		wrapped = fmt.Errorf("browser: render page: %w", waitErr)
	}
	page.mutex.Lock()
	if page.matchesNavigationLocked(id, 0) {
		page.failNavigationLocked(wrapped)
	}
	page.mutex.Unlock()
	return wrapped
}

func (page *Page) Navigation() NavigationSnapshot {
	if page == nil {
		return NavigationSnapshot{}
	}
	page.mutex.RLock()
	snapshot := page.navigationSnapshotLocked()
	page.mutex.RUnlock()
	return snapshot
}

// CancelNavigation stops the current non-terminal navigation without closing
// the Page or changing its committed history entry. Native browser chrome uses
// this for a loading Stop control; cancellation still crosses the Page lock
// and the navigation context owned by the browser kernel.
func (page *Page) CancelNavigation(id NavigationID) error {
	if page == nil {
		return fmt.Errorf("browser: nil page")
	}
	page.mutex.Lock()
	defer page.mutex.Unlock()
	if page.closed {
		return ErrPageClosed
	}
	if id == 0 || page.navigation.id == 0 {
		return ErrNoNavigation
	}
	if page.navigation.id != id {
		return fmt.Errorf("%w: navigation %d is no longer current", ErrNavigationSuperseded, id)
	}
	if page.navigation.state.terminal() {
		return nil
	}
	page.cancelNavigationLocked(context.Canceled)
	return nil
}

func (page *Page) DocumentGeneration() DocumentGeneration {
	if page == nil {
		return 0
	}
	page.mutex.RLock()
	generation := page.documentGeneration
	page.mutex.RUnlock()
	return generation
}

func (page *Page) Resolve(handle NodeHandle) (*dom.Node, bool) {
	if page == nil || handle.Document == 0 || handle.Node == dom.InvalidNodeID {
		return nil, false
	}
	page.mutex.RLock()
	if page.documentGeneration != handle.Document {
		page.mutex.RUnlock()
		return nil, false
	}
	document := page.document
	page.mutex.RUnlock()
	return document.Resolve(handle.Node)
}

func (page *Page) navigationSnapshot(id NavigationID) (NavigationSnapshot, error) {
	page.mutex.RLock()
	defer page.mutex.RUnlock()
	if page.navigation.id == 0 {
		return NavigationSnapshot{}, ErrNoNavigation
	}
	if page.navigation.id != id {
		return NavigationSnapshot{}, fmt.Errorf("%w: navigation %d is no longer current", ErrNavigationSuperseded, id)
	}
	return page.navigationSnapshotLocked(), nil
}

func (page *Page) navigationSnapshotLocked() NavigationSnapshot {
	return NavigationSnapshot{
		ID:                 page.navigation.id,
		RequestedURL:       cloneURL(page.navigation.requestedURL),
		URL:                cloneURL(page.location),
		State:              page.navigation.state,
		DocumentGeneration: page.navigation.documentGeneration,
		ResourcesTotal:     page.navigation.resourcesTotal,
		ResourcesPending:   page.navigation.resourcesPending,
		ResourcesFailed:    page.navigation.resourcesFailed,
		ScriptsTotal:       page.navigation.scriptsTotal,
		ScriptsPending:     page.navigation.scriptsPending,
		ScriptsFailed:      page.navigation.scriptsFailed,
		ScriptFailures:     append([]ScriptFailure(nil), page.navigation.scriptFailures...),
		Err:                page.navigation.err,
	}
}

func (page *Page) loadNavigationDocument(ctx context.Context, id NavigationID, rawURL string, client DocumentLoader) {
	prepared, err := prepareNavigation(ctx, rawURL, client)
	if err != nil {
		page.enqueueNavigationFailure(id, err)
		return
	}
	_, _, err = page.browser.scheduler.EnqueueExternalTask(page.Realm, func(task *browserruntime.TaskContext) error {
		return page.commitNavigationDocument(task, id, prepared)
	})
	if err != nil && !errors.Is(err, browserruntime.ErrRealmClosed) {
		page.enqueueNavigationFailure(id, err)
	}
}

func prepareNavigation(ctx context.Context, rawURL string, client DocumentLoader) (preparedNavigation, error) {
	response, err := client.Load(ctx, rawURL)
	if err != nil {
		return preparedNavigation{}, fmt.Errorf("browser: load document: %w", err)
	}
	if response == nil || response.Body == nil {
		return preparedNavigation{}, fmt.Errorf("browser: empty document response")
	}
	defer response.Body.Close()

	root, err := htmlparser.Parse(response.Body)
	if err != nil {
		return preparedNavigation{}, fmt.Errorf("browser: parse document: %w", err)
	}
	location, err := loadedURL(response, rawURL)
	if err != nil {
		return preparedNavigation{}, err
	}
	document, err := dom.IndexDocument(root)
	if err != nil {
		return preparedNavigation{}, err
	}
	requests, err := discoverNavigationResources(document, location)
	if err != nil {
		return preparedNavigation{}, fmt.Errorf("browser: discover page resources: %w", err)
	}
	scripts, err := discoverNavigationScripts(document, location)
	if err != nil {
		return preparedNavigation{}, fmt.Errorf("browser: discover page scripts: %w", err)
	}
	fetcher, _ := client.(resource.Fetcher)
	if fetcher == nil && len(requests) != 0 {
		available := requests[:0]
		for _, request := range requests {
			if !request.optional {
				available = append(available, request)
			}
		}
		requests = available
		if len(requests) != 0 {
			return preparedNavigation{}, ErrResourceLoaderUnavailable
		}
	}
	return preparedNavigation{
		document:        document,
		location:        location,
		defaultLanguage: responseDefaultLanguage(response),
		requests:        requests,
		scripts:         scripts,
		fetcher:         fetcher,
	}, nil
}

func responseDefaultLanguage(response *loader.Response) string {
	if response == nil || response.Header == nil {
		return ""
	}
	values := response.Header.Values("Content-Language")
	if len(values) == 0 {
		return ""
	}
	joined := strings.Join(values, ",")
	if strings.Contains(joined, ",") {
		return ""
	}
	candidate := strings.Trim(joined, " \t")
	if candidate == "" || strings.ContainsAny(candidate, " \t\r\n") {
		return ""
	}
	return candidate
}

func (page *Page) commitNavigationDocument(
	task *browserruntime.TaskContext,
	id NavigationID,
	prepared preparedNavigation,
) error {
	replacementResources := newPageResources()
	stylesheetRequests, _, graphErr := replacementResources.stylesheets.sync(prepared.document, prepared.location)
	if graphErr != nil {
		page.mutex.Lock()
		if page.matchesNavigationLocked(id, 0) {
			page.failNavigationLocked(graphErr)
		}
		page.mutex.Unlock()
		return graphErr
	}
	stylesheetByOwner := make(map[dom.NodeID]navigationResourceRequest, len(stylesheetRequests))
	for _, request := range stylesheetRequests {
		stylesheetByOwner[request.node] = request
	}
	requests := make([]navigationResourceRequest, 0, len(prepared.requests))
	for _, request := range prepared.requests {
		if request.kind == resource.Stylesheet {
			if replacement, ok := stylesheetByOwner[request.node]; ok {
				requests = append(requests, replacement)
				delete(stylesheetByOwner, request.node)
			}
			continue
		}
		requests = append(requests, request)
	}
	for _, request := range stylesheetRequests {
		if _, remains := stylesheetByOwner[request.node]; remains {
			requests = append(requests, request)
		}
	}
	if err := replacementResources.markImageRequests(requests); err != nil {
		page.mutex.Lock()
		if page.matchesNavigationLocked(id, 0) {
			page.failNavigationLocked(err)
		}
		page.mutex.Unlock()
		return err
	}
	if err := replacementResources.decodeInitialInlineImages(prepared.document, prepared.location); err != nil {
		page.mutex.Lock()
		if page.matchesNavigationLocked(id, 0) {
			page.failNavigationLocked(err)
		}
		page.mutex.Unlock()
		return err
	}
	prepared.requests = requests

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

	var replacementScript JSRealm
	var err error
	if page.browser.engine != nil {
		replacementScript, err = page.browser.engine.NewRealm()
		if err != nil {
			page.mutex.Lock()
			if page.matchesNavigationLocked(id, 0) {
				page.failNavigationLocked(err)
			}
			page.mutex.Unlock()
			return err
		}
	}
	generation, generationErr := page.browser.reserveDocumentGeneration()
	if generationErr != nil {
		if replacementScript != nil {
			_ = replacementScript.Close()
		}
		return generationErr
	}

	page.mutex.Lock()
	if !page.matchesNavigationLocked(id, 0) {
		page.mutex.Unlock()
		if replacementScript != nil {
			_ = replacementScript.Close()
		}
		return nil
	}
	replacementLifetimes, lifetimeErr := newNodeLifetimeState(
		page.Realm,
		prepared.document,
		generation,
	)
	if lifetimeErr != nil {
		page.failNavigationLocked(lifetimeErr)
		page.mutex.Unlock()
		if replacementScript != nil {
			_ = replacementScript.Close()
		}
		return lifetimeErr
	}
	oldScript := page.script
	oldLifetimes := page.nodeLifetimes
	oldDocumentCancel := page.documentCancel
	var retiredCachedDocuments []*cachedDocumentState
	if cacheDeparture {
		retiredCachedDocuments = page.storeCachedDocumentLocked(page.captureCurrentDocumentLocked())
		oldScript = nil
		oldLifetimes = nil
		oldDocumentCancel = nil
	}
	documentContext, documentCancel := context.WithCancel(context.Background())
	timers := page.takeTimersLocked()
	webSockets := page.takeWebSocketsLocked()
	animationRefs := page.takeAnimationFramesLocked()
	children := page.takeChildFramesLocked()
	oldGeneration := page.documentGeneration
	page.script = replacementScript
	page.nodeLifetimes = replacementLifetimes
	page.document = prepared.document
	page.documentGeneration = generation
	page.documentLanguage = prepared.defaultLanguage
	page.activeElement = dom.InvalidNodeID
	page.hoveredElement = dom.InvalidNodeID
	page.pressedElement = dom.InvalidNodeID
	page.focusVisible = false
	page.scrollX = 0
	page.scrollY = 0
	page.elementScroll = make(map[dom.NodeID]scrollOffset)
	page.timeOrigin = time.Now()
	page.lastFrameTime = 0
	page.computedStyle = computedStyleState{}
	page.layout = layoutState{}
	page.styleRevision++
	page.layoutRevision++
	page.location = cloneURL(prepared.location)
	page.readyState = "loading"
	if page.navigation.historyTarget >= 0 {
		targetEntry := page.history[page.navigation.historyTarget]
		page.historyDocument = targetEntry.DocumentSequence
		for index := range page.history {
			if page.history[index].DocumentSequence == page.historyDocument {
				page.history[index].DocumentGeneration = generation
			}
		}
		page.replaceHistoryEntryLocked(page.navigation.historyTarget, prepared.location, id)
	} else if page.navigation.replaceCurrent && page.historyIndex >= 0 {
		page.replaceCurrentWithNewDocumentLocked(prepared.location, id, generation)
	} else {
		page.pushHistoryLocked(prepared.location, id)
	}
	page.resources = replacementResources
	page.resources.stylesheets.markRequested(prepared.requests)
	page.resourceFetcher = prepared.fetcher
	page.documentContext = documentContext
	page.documentCancel = documentCancel
	page.dirty = true
	page.navigation.documentGeneration = generation
	page.navigation.resourcesTotal = len(prepared.requests)
	page.navigation.resourcesPending = len(prepared.requests)
	page.navigation.resourcesFailed = 0
	page.navigation.scripts = nil
	if replacementScript != nil {
		page.navigation.scripts = append([]navigationScript(nil), prepared.scripts...)
	}
	page.navigation.fetcher = prepared.fetcher
	page.navigation.scriptsTotal = len(page.navigation.scripts)
	page.navigation.scriptsPending = len(page.navigation.scripts)
	page.navigation.scriptsFailed = 0
	navigationContext := page.navigation.context
	page.browser.replaceDocument(page, oldGeneration, generation)
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
		if page.matchesNavigationLocked(id, generation) {
			page.failNavigationLocked(cleanupErr)
		}
		page.mutex.Unlock()
		return cleanupErr
	}

	if len(prepared.requests) == 0 {
		page.mutex.Lock()
		if !page.matchesNavigationLocked(id, generation) {
			page.mutex.Unlock()
			return nil
		}
		page.navigation.state = NavigationLoadingScripts
		scripts := append([]navigationScript(nil), page.navigation.scripts...)
		fetcher := page.navigation.fetcher
		page.mutex.Unlock()
		go page.loadNavigationScripts(navigationContext, id, generation, fetcher, scripts)
		return nil
	}

	page.mutex.Lock()
	if !page.matchesNavigationLocked(id, generation) {
		page.mutex.Unlock()
		return nil
	}
	page.navigation.state = NavigationLoadingResources
	page.mutex.Unlock()

	go page.loadNavigationResources(
		navigationContext,
		id,
		generation,
		prepared.fetcher,
		prepared.requests,
		maxNavigationImagePixels,
	)
	return nil
}

func (page *Page) applyNavigationResource(
	task *browserruntime.TaskContext,
	id NavigationID,
	generation DocumentGeneration,
	result navigationResourceResult,
) error {
	page.mutex.Lock()
	if !page.matchesNavigationLocked(id, generation) || page.navigation.state != NavigationLoadingResources {
		page.mutex.Unlock()
		return nil
	}
	if result.err != nil {
		page.navigation.resourcesFailed++
	} else {
		if page.resources.apply(result) {
			if result.kind == resource.Stylesheet {
				page.invalidateStyleLocked()
			} else {
				page.invalidateLayoutLocked()
			}
		}
	}
	if page.navigation.resourcesPending > 0 {
		page.navigation.resourcesPending--
	}
	if page.navigation.resourcesPending != 0 {
		page.mutex.Unlock()
		return nil
	}
	page.navigation.state = NavigationLoadingScripts
	scripts := append([]navigationScript(nil), page.navigation.scripts...)
	fetcher := page.navigation.fetcher
	navigationContext := page.navigation.context
	page.mutex.Unlock()
	go page.loadNavigationScripts(navigationContext, id, generation, fetcher, scripts)
	return nil
}

func (page *Page) queueNavigationRenderLocked(
	task *browserruntime.TaskContext,
	id NavigationID,
	generation DocumentGeneration,
) error {
	invalidation, err := task.NewObject()
	if err != nil {
		page.failNavigationLocked(err)
		return err
	}
	_, err = task.QueueTask(func(*browserruntime.TaskContext) error {
		page.mutex.Lock()
		defer page.mutex.Unlock()
		if !page.matchesNavigationLocked(id, generation) || page.navigation.state != NavigationRendering {
			return nil
		}
		if renderErr := page.renderLocked(false); renderErr != nil {
			page.failNavigationLocked(renderErr)
			return renderErr
		}
		page.navigation.state = NavigationComplete
		page.navigation.err = nil
		page.navigation.scripts = nil
		page.navigation.fetcher = nil
		if page.navigation.cancel != nil {
			page.navigation.cancel()
			page.navigation.cancel = nil
		}
		return nil
	}, invalidation)
	if err != nil {
		page.failNavigationLocked(err)
	}
	return err
}

func (page *Page) enqueueNavigationFailure(id NavigationID, navigationErr error) {
	if navigationErr == nil || page == nil || page.browser == nil {
		return
	}
	_, _, _ = page.browser.scheduler.EnqueueExternalTask(page.Realm, func(*browserruntime.TaskContext) error {
		page.mutex.Lock()
		defer page.mutex.Unlock()
		if !page.matchesNavigationLocked(id, 0) {
			return nil
		}
		page.failNavigationLocked(navigationErr)
		return nil
	})
}

func (page *Page) failNavigationLocked(err error) {
	page.navigation.err = err
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		page.navigation.state = NavigationCanceled
	} else {
		page.navigation.state = NavigationFailed
	}
	if page.navigation.cancel != nil {
		page.navigation.cancel()
		page.navigation.cancel = nil
	}
}

func (page *Page) cancelNavigationLocked(err error) {
	page.navigation.err = err
	page.navigation.state = NavigationCanceled
	if page.navigation.cancel != nil {
		page.navigation.cancel()
		page.navigation.cancel = nil
	}
}

func (page *Page) matchesNavigationLocked(id NavigationID, generation DocumentGeneration) bool {
	if page.closed || page.navigation.id != id || page.navigation.state.terminal() {
		return false
	}
	return generation == 0 || page.documentGeneration == generation
}

var (
	ErrNoNavigation               = errors.New("browser: page has no navigation")
	ErrNavigationSuperseded       = errors.New("browser: navigation superseded")
	ErrNavigationCanceled         = errors.New("browser: navigation canceled by beforeunload")
	ErrHistoryChanged             = errors.New("browser: session history changed before traversal")
	ErrHistoryTraversalOutOfRange = errors.New("browser: session history traversal is out of range")
	ErrResourceLoaderUnavailable  = errors.New("browser: document loader cannot load subresources")
)
