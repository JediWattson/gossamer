package browser

import (
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
	ID       PageID
	Realm    *browserruntime.Realm
	Document *dom.Document

	mutex           sync.RWMutex
	location        *url.URL
	resources       render.Resources
	viewport        render.Viewport
	frame           *render.Frame
	dirty           bool
	renderedVersion uint64
	closed          bool
}

func newPage(id PageID, realm *browserruntime.Realm, document *dom.Document, location *url.URL) *Page {
	return &Page{
		ID:       id,
		Realm:    realm,
		Document: document,
		location: cloneURL(location),
		viewport: render.DefaultViewport,
		dirty:    true,
	}
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
	page.resources = resources
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
	dirty := page.dirty || page.renderedVersion != page.Document.Version()
	page.mutex.RUnlock()
	return dirty
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
	return page.Realm.EnqueueTask(func(context *browserruntime.TaskContext) error {
		page.mutex.Lock()
		if page.closed {
			page.mutex.Unlock()
			return ErrPageClosed
		}
		if err := page.Document.SetText(node, data); err != nil {
			page.mutex.Unlock()
			return err
		}
		page.dirty = true
		page.mutex.Unlock()

		invalidation, err := context.NewObject()
		if err != nil {
			return err
		}
		_, err = context.QueueTask(func(*browserruntime.TaskContext) error {
			page.mutex.Lock()
			defer page.mutex.Unlock()
			return page.renderLocked(true)
		}, invalidation)
		return err
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
	page.frame = nil
	page.mutex.Unlock()
	return page.Realm.Close()
}

func (page *Page) renderLocked(onlyIfDirty bool) error {
	if page.closed {
		return ErrPageClosed
	}
	version := page.Document.Version()
	if onlyIfDirty && !page.dirty && page.renderedVersion == version {
		return nil
	}
	var frame *render.Frame
	err := page.Document.ReadRoot(func(root *dom.Node) error {
		var renderErr error
		frame, renderErr = render.RenderWithResources(root, page.viewport, page.resources)
		return renderErr
	})
	if err != nil {
		return err
	}
	page.frame = frame
	page.renderedVersion = version
	page.dirty = false
	return nil
}

func cloneURL(source *url.URL) *url.URL {
	if source == nil {
		return nil
	}
	clone := *source
	return &clone
}
