package browser

import (
	"errors"
	"fmt"
	"net/url"
	"sync"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
)

// Page is the owner that ties one Realm to its stable-identity Document,
// current URL, render resources, invalidation state, and current Frame.
type Page struct {
	ID    PageID
	Realm *browserruntime.Realm

	browser *Browser
	script  JSRealm

	mutex                  sync.RWMutex
	document               *dom.Document
	nodeLifetimes          *nodeLifetimeState
	documentGeneration     DocumentGeneration
	nextDocumentGeneration DocumentGeneration
	location               *url.URL
	resources              pageResources
	viewport               render.Viewport
	frame                  *render.Frame
	frameGeneration        DocumentGeneration
	dirty                  bool
	renderedVersion        uint64
	activeElement          dom.NodeID
	nextNavigation         NavigationID
	navigation             navigationRecord
	nextTimer              TimerID
	timers                 map[TimerID]*pageTimer
	closed                 bool
}

func newPage(
	browser *Browser,
	id PageID,
	realm *browserruntime.Realm,
	script JSRealm,
	document *dom.Document,
	location *url.URL,
) (*Page, error) {
	lifetimes, err := newNodeLifetimeState(realm.Ledger(), document, 1)
	if err != nil {
		return nil, err
	}
	return &Page{
		ID:                     id,
		Realm:                  realm,
		browser:                browser,
		script:                 script,
		document:               document,
		nodeLifetimes:          lifetimes,
		documentGeneration:     1,
		nextDocumentGeneration: 1,
		location:               cloneURL(location),
		resources:              newPageResources(),
		viewport:               render.DefaultViewport,
		timers:                 make(map[TimerID]*pageTimer),
		dirty:                  true,
	}, nil
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
	page.dirty = true
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
	if page.viewport != viewport {
		page.viewport = viewport
		page.dirty = true
	}
	return nil
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
	page.frame = nil
	timers := page.takeTimersLocked()
	script := page.script
	page.script = nil
	lifetimes := page.nodeLifetimes
	page.nodeLifetimes = nil
	page.mutex.Unlock()
	timerErr := page.releaseTimers(timers)
	var scriptErr error
	if script != nil {
		scriptErr = script.Close()
	}
	var lifetimeErr error
	if lifetimes != nil {
		lifetimeErr = lifetimes.close()
	}
	return errors.Join(timerErr, scriptErr, lifetimeErr, page.Realm.Close())
}

func (page *Page) renderLocked(onlyIfDirty bool) error {
	if page.closed {
		return ErrPageClosed
	}
	version := page.document.Version()
	if onlyIfDirty && !page.dirty && page.renderedVersion == version {
		return nil
	}
	var frame *render.Frame
	resources := page.resources.rendererResources(page.document)
	err := page.document.ReadRoot(func(root *dom.Node) error {
		var renderErr error
		frame, renderErr = render.RenderWithResources(root, page.viewport, resources)
		return renderErr
	})
	if err != nil {
		return err
	}
	page.frame = frame
	page.frameGeneration = page.documentGeneration
	page.renderedVersion = version
	page.dirty = false
	return nil
}

var ErrStaleNodeHandle = errors.New("browser: node handle belongs to a previous document")

func cloneURL(source *url.URL) *url.URL {
	if source == nil {
		return nil
	}
	clone := *source
	return &clone
}
