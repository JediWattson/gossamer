package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/JediWattson/gossamer/internal/dom"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
)

const maximumHistoryStateBytes = 1 << 20

type SessionHistorySnapshot struct {
	Length    int
	Index     int
	StateJSON string
	URL       *url.URL
}

type LocationNavigationAction uint8

const (
	LocationAssign LocationNavigationAction = iota + 1
	LocationReplace
	LocationReload
)

type LocationComponent uint8

const (
	LocationHref LocationComponent = iota + 1
	LocationOrigin
	LocationProtocol
	LocationHost
	LocationHostname
	LocationPort
	LocationPathname
	LocationSearch
	LocationHash
)

func (page *Page) SetNavigationLoader(client DocumentLoader) {
	if page == nil {
		return
	}
	page.mutex.Lock()
	page.navigationLoader = client
	page.mutex.Unlock()
}

func (page *Page) SessionHistorySnapshot() SessionHistorySnapshot {
	if page == nil {
		return SessionHistorySnapshot{Index: -1, StateJSON: "null"}
	}
	page.mutex.RLock()
	snapshot := page.sessionHistorySnapshotLocked()
	page.mutex.RUnlock()
	return snapshot
}

func (page *Page) LocationComponent(component LocationComponent) (string, error) {
	if page == nil {
		return "", fmt.Errorf("browser: nil page")
	}
	page.mutex.RLock()
	location := cloneURL(page.location)
	closed := page.closed
	page.mutex.RUnlock()
	if closed {
		return "", ErrPageClosed
	}
	if location == nil {
		return "", ErrNoNavigation
	}
	return locationComponent(location, component)
}

func locationComponent(location *url.URL, component LocationComponent) (string, error) {
	switch component {
	case LocationHref:
		return location.String(), nil
	case LocationOrigin:
		return location.Scheme + "://" + location.Host, nil
	case LocationProtocol:
		return location.Scheme + ":", nil
	case LocationHost:
		return location.Host, nil
	case LocationHostname:
		return location.Hostname(), nil
	case LocationPort:
		return location.Port(), nil
	case LocationPathname:
		if location.EscapedPath() == "" {
			return "/", nil
		}
		return location.EscapedPath(), nil
	case LocationSearch:
		if location.RawQuery == "" {
			return "", nil
		}
		return "?" + location.RawQuery, nil
	case LocationHash:
		if location.Fragment == "" {
			return "", nil
		}
		return "#" + location.EscapedFragment(), nil
	default:
		return "", fmt.Errorf("browser: invalid location component %d", component)
	}
}

func (page *Page) SetLocationComponent(component LocationComponent, value string) error {
	if component == LocationHref {
		_, err := page.NavigateLocation(context.Background(), value, LocationAssign)
		return err
	}
	if component == LocationOrigin {
		return nil
	}
	page.mutex.RLock()
	destination := cloneURL(page.location)
	page.mutex.RUnlock()
	if destination == nil {
		return ErrNoNavigation
	}
	switch component {
	case LocationProtocol:
		destination.Scheme = strings.TrimSuffix(value, ":")
	case LocationHost:
		parsed, err := url.Parse(destination.Scheme + "://" + value)
		if err != nil || parsed.Host == "" {
			return dom.NewException(dom.SyntaxError, err, "invalid location host %q", value)
		}
		destination.Host = parsed.Host
	case LocationHostname:
		port := destination.Port()
		destination.Host = value
		if port != "" {
			destination.Host += ":" + port
		}
	case LocationPort:
		destination.Host = destination.Hostname()
		if value != "" {
			destination.Host += ":" + value
		}
	case LocationPathname:
		if !strings.HasPrefix(value, "/") {
			value = "/" + value
		}
		destination.Path, destination.RawPath = value, ""
	case LocationSearch:
		destination.RawQuery = strings.TrimPrefix(value, "?")
	case LocationHash:
		destination.Fragment = strings.TrimPrefix(value, "#")
		destination.RawFragment = ""
	default:
		return fmt.Errorf("browser: invalid location component %d", component)
	}
	_, err := page.NavigateLocation(context.Background(), destination.String(), LocationAssign)
	return err
}

func (page *Page) sessionHistorySnapshotLocked() SessionHistorySnapshot {
	state := "null"
	if page.historyIndex >= 0 && page.historyIndex < len(page.history) && page.history[page.historyIndex].StateJSON != "" {
		state = page.history[page.historyIndex].StateJSON
	}
	return SessionHistorySnapshot{
		Length: len(page.history), Index: page.historyIndex, StateJSON: state,
		URL: cloneURL(page.location),
	}
}

// PushState updates the current same-document history group. State is JSON
// encoded by the active engine before it crosses into the browser kernel.
func (page *Page) PushState(stateJSON, rawURL string, replace bool) (bool, error) {
	if page == nil {
		return false, fmt.Errorf("browser: nil page")
	}
	if stateJSON == "" {
		stateJSON = "null"
	}
	if len(stateJSON) > maximumHistoryStateBytes || !json.Valid([]byte(stateJSON)) {
		return false, dom.NewException(dom.DataCloneError, ErrInvalidHistoryState, "history state is not bounded valid JSON")
	}
	page.mutex.Lock()
	defer page.mutex.Unlock()
	if page.closed {
		return false, ErrPageClosed
	}
	if page.location == nil || page.historyIndex < 0 || page.historyIndex >= len(page.history) {
		return false, dom.NewException(dom.InvalidStateError, ErrNoNavigation, "history has no active document URL")
	}
	destination, err := resolveHistoryURL(page.location, rawURL)
	if err != nil {
		return false, err
	}
	if !sameOriginURL(page.location, destination) {
		return false, dom.NewException(dom.SecurityError, ErrCrossOriginHistoryURL, "history URL %q is not same-origin", destination.String())
	}
	oldURL := page.location.String()
	entry := HistoryEntry{
		URL: cloneURL(destination), StateJSON: stateJSON,
		DocumentSequence:   page.historyDocument,
		DocumentGeneration: page.documentGeneration,
	}
	if replace {
		entry.Navigation = page.history[page.historyIndex].Navigation
		page.history[page.historyIndex] = entry
	} else {
		if page.historyIndex+1 < len(page.history) {
			page.history = page.history[:page.historyIndex+1]
		}
		page.history = append(page.history, entry)
		page.historyIndex = len(page.history) - 1
	}
	page.location = cloneURL(destination)
	changed := oldURL != destination.String()
	if changed {
		page.invalidateStyleLocked()
	}
	return changed, nil
}

func resolveHistoryURL(base *url.URL, raw string) (*url.URL, error) {
	if base == nil {
		return nil, ErrNoNavigation
	}
	if strings.TrimSpace(raw) == "" {
		return cloneURL(base), nil
	}
	reference, err := url.Parse(raw)
	if err != nil {
		return nil, dom.NewException(dom.SecurityError, err, "invalid history URL %q", raw)
	}
	destination := base.ResolveReference(reference)
	if destination.Scheme != "http" && destination.Scheme != "https" {
		return nil, dom.NewException(dom.SecurityError, ErrCrossOriginHistoryURL, "unsupported history URL scheme %q", destination.Scheme)
	}
	return destination, nil
}

func (page *Page) NavigateLocation(ctx context.Context, raw string, action LocationNavigationAction) (NavigationID, error) {
	if page == nil {
		return 0, fmt.Errorf("browser: nil page")
	}
	if ctx == nil {
		return 0, fmt.Errorf("browser: nil navigation context")
	}
	page.mutex.RLock()
	base := cloneURL(page.location)
	client := page.navigationLoader
	page.mutex.RUnlock()
	if action == LocationReload {
		return page.Reload(ctx, client)
	}
	destination, err := resolveHistoryURL(base, raw)
	if err != nil {
		return 0, err
	}
	if sameURLExceptFragment(base, destination) && base.Fragment != destination.Fragment {
		return page.beginFragmentNavigation(ctx, destination, action == LocationReplace)
	}
	return page.beginNavigation(ctx, destination.String(), client, -1, -1, action == LocationReplace)
}

func sameURLExceptFragment(left, right *url.URL) bool {
	if left == nil || right == nil {
		return false
	}
	leftCopy := *left
	rightCopy := *right
	leftCopy.Fragment, leftCopy.RawFragment = "", ""
	rightCopy.Fragment, rightCopy.RawFragment = "", ""
	return leftCopy.String() == rightCopy.String()
}

func (page *Page) beginFragmentNavigation(ctx context.Context, destination *url.URL, replace bool) (NavigationID, error) {
	navigationContext, cancel := context.WithCancel(ctx)
	page.mutex.Lock()
	if page.closed {
		page.mutex.Unlock()
		cancel()
		return 0, ErrPageClosed
	}
	if page.historyIndex < 0 {
		page.mutex.Unlock()
		cancel()
		return 0, ErrNoNavigation
	}
	if page.navigation.cancel != nil {
		page.navigation.cancel()
	}
	oldURL := cloneURL(page.location)
	page.nextNavigation++
	id := page.nextNavigation
	page.navigation = navigationRecord{
		id: id, requestedURL: cloneURL(destination), state: NavigationRendering,
		documentGeneration: page.documentGeneration,
		context:            navigationContext, cancel: cancel,
	}
	entry := HistoryEntry{
		URL: cloneURL(destination), Navigation: id, StateJSON: "null",
		DocumentSequence:   page.historyDocument,
		DocumentGeneration: page.documentGeneration,
	}
	if replace {
		entry.StateJSON = page.history[page.historyIndex].StateJSON
		page.history[page.historyIndex] = entry
	} else {
		if page.historyIndex+1 < len(page.history) {
			page.history = page.history[:page.historyIndex+1]
		}
		page.history = append(page.history, entry)
		page.historyIndex = len(page.history) - 1
	}
	page.location = cloneURL(destination)
	page.invalidateStyleLocked()
	generation := page.documentGeneration
	page.mutex.Unlock()

	_, _, err := page.browser.scheduler.EnqueueExternalTask(page.Realm, func(task *browserruntime.TaskContext) error {
		return page.finishSameDocumentNavigation(task, id, generation, oldURL, destination, false)
	})
	if err != nil {
		page.mutex.Lock()
		if page.navigation.id == id {
			page.failNavigationLocked(err)
		}
		page.mutex.Unlock()
	}
	return id, err
}

func (page *Page) beginSameDocumentTraversal(ctx context.Context, source, target int) (NavigationID, error) {
	navigationContext, cancel := context.WithCancel(ctx)
	page.mutex.Lock()
	if page.closed {
		page.mutex.Unlock()
		cancel()
		return 0, ErrPageClosed
	}
	if page.historyIndex != source || target < 0 || target >= len(page.history) ||
		page.history[target].DocumentSequence != page.historyDocument {
		page.mutex.Unlock()
		cancel()
		return 0, ErrHistoryChanged
	}
	if page.navigation.cancel != nil {
		page.navigation.cancel()
	}
	oldURL := cloneURL(page.location)
	destination := cloneHistoryEntry(page.history[target])
	page.nextNavigation++
	id := page.nextNavigation
	page.navigation = navigationRecord{
		id: id, requestedURL: cloneURL(destination.URL), state: NavigationRendering,
		documentGeneration: page.documentGeneration, context: navigationContext,
		cancel: cancel, historySource: source, historyTarget: target,
	}
	generation := page.documentGeneration
	page.mutex.Unlock()

	_, _, err := page.browser.scheduler.EnqueueExternalTask(page.Realm, func(task *browserruntime.TaskContext) error {
		page.mutex.Lock()
		if page.closed || page.documentGeneration != generation || page.historyIndex != source ||
			target >= len(page.history) || page.history[target].DocumentSequence != page.historyDocument {
			page.mutex.Unlock()
			return ErrHistoryChanged
		}
		page.historyIndex = target
		page.location = cloneURL(destination.URL)
		page.invalidateStyleLocked()
		page.mutex.Unlock()
		return page.finishSameDocumentNavigation(task, id, generation, oldURL, destination.URL, true)
	})
	if err != nil {
		cancel()
	}
	return id, err
}

func (page *Page) finishSameDocumentNavigation(
	task *browserruntime.TaskContext,
	id NavigationID,
	generation DocumentGeneration,
	oldURL, newURL *url.URL,
	popState bool,
) error {
	page.mutex.RLock()
	if page.closed || page.documentGeneration != generation || page.navigation.id != id {
		page.mutex.RUnlock()
		return nil
	}
	script := page.script
	state := page.sessionHistorySnapshotLocked().StateJSON
	page.mutex.RUnlock()
	host := &taskHost{page: page, task: task, generation: generation, autoRender: true, styleChanged: true}
	var dispatchErr error
	windowTarget := NodeHandle{Document: generation}
	if script != nil && popState {
		_, dispatchErr = script.DispatchEvent(host, InputEvent{Type: InputPopState, Target: windowTarget, Data: state})
	}
	if script != nil && oldURL != nil && newURL != nil && oldURL.Fragment != newURL.Fragment {
		_, hashErr := script.DispatchEvent(host, InputEvent{
			Type: InputHashChange, Target: windowTarget, Key: oldURL.String(), Code: newURL.String(),
		})
		dispatchErr = errors.Join(dispatchErr, hashErr)
	}
	if script != nil {
		dispatchErr = errors.Join(dispatchErr, script.DrainMicrotasks(host))
	}
	finishErr := host.finish()
	page.mutex.Lock()
	if page.navigation.id == id && !page.navigation.state.terminal() {
		page.navigation.state = NavigationComplete
		page.navigation.err = nil
		if page.navigation.cancel != nil {
			page.navigation.cancel()
			page.navigation.cancel = nil
		}
	}
	page.mutex.Unlock()
	return errors.Join(dispatchErr, finishErr)
}

func (page *Page) replaceCurrentWithNewDocumentLocked(location *url.URL, navigation NavigationID, generation DocumentGeneration) {
	if page.historyIndex < 0 || page.historyIndex >= len(page.history) {
		page.pushHistoryLocked(location, navigation)
		return
	}
	page.nextHistoryDocument++
	if page.nextHistoryDocument == 0 {
		page.nextHistoryDocument++
	}
	page.historyDocument = page.nextHistoryDocument
	page.history[page.historyIndex] = HistoryEntry{
		URL: cloneURL(location), Navigation: navigation, StateJSON: "null",
		DocumentSequence: page.historyDocument, DocumentGeneration: generation,
	}
	page.invalidateStyleLocked()
}

var (
	ErrInvalidHistoryState   = errors.New("browser: invalid history state")
	ErrCrossOriginHistoryURL = errors.New("browser: cross-origin history URL")
)
