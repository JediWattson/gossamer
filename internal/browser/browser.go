// Package browser ties Gossamer's execution runtime to the existing document
// and rendering pipeline without introducing a JavaScript engine.
package browser

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sync"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/loader"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

type PageID uint64

// DocumentLoader is the navigation boundary used by Browser.LoadPage.
type DocumentLoader interface {
	Load(context.Context, string) (*loader.Response, error)
}

// Browser owns the realm scheduler and every Page created through it.
type Browser struct {
	scheduler *browserruntime.Scheduler
	engine    Engine

	mutex        sync.Mutex
	nextPage     PageID
	nextDocument DocumentGeneration
	pages        map[PageID]*Page
	documents    map[DocumentGeneration]*Page
	closed       bool
}

func New() (*Browser, error) {
	return NewWithEngine(nil)
}

// NewWithEngine makes engine the script-realm factory owned by the Browser.
// A nil engine creates pages with scripting disabled.
func NewWithEngine(engine Engine) (*Browser, error) {
	scheduler, err := browserruntime.NewScheduler(nil)
	if err != nil {
		return nil, err
	}
	return &Browser{
		scheduler: scheduler,
		engine:    engine,
		pages:     make(map[PageID]*Page),
		documents: make(map[DocumentGeneration]*Page),
	}, nil
}

// NewPage establishes the Page/Realm/Document ownership boundary around an
// already parsed DOM. Existing pointer-based rendering remains behind the
// Document's NodeID resolution seam.
func (browser *Browser) NewPage(root *dom.Node, location *url.URL) (*Page, error) {
	if browser == nil {
		return nil, fmt.Errorf("browser: nil browser")
	}
	document, err := dom.IndexDocument(root)
	if err != nil {
		return nil, err
	}
	realm, err := browser.scheduler.NewRealm()
	if err != nil {
		return nil, err
	}
	var script JSRealm
	if browser.engine != nil {
		script, err = browser.engine.NewRealm()
		if err != nil {
			_ = realm.Close()
			return nil, err
		}
	}

	browser.mutex.Lock()
	defer browser.mutex.Unlock()
	if browser.closed {
		if script != nil {
			_ = script.Close()
		}
		_ = realm.Close()
		return nil, ErrBrowserClosed
	}
	browser.nextPage++
	browser.nextDocument++
	if browser.nextDocument == 0 {
		if script != nil {
			_ = script.Close()
		}
		_ = realm.Close()
		return nil, fmt.Errorf("browser: exhausted document generations")
	}
	generation := browser.nextDocument
	page, err := newPage(browser, browser.nextPage, realm, script, document, generation, location)
	if err != nil {
		if script != nil {
			_ = script.Close()
		}
		_ = realm.Close()
		return nil, err
	}
	browser.pages[page.ID] = page
	browser.documents[generation] = page
	return page, nil
}

func (browser *Browser) reserveDocumentGeneration() (DocumentGeneration, error) {
	if browser == nil {
		return 0, fmt.Errorf("browser: nil browser")
	}
	browser.mutex.Lock()
	defer browser.mutex.Unlock()
	if browser.closed {
		return 0, ErrBrowserClosed
	}
	browser.nextDocument++
	if browser.nextDocument == 0 {
		return 0, fmt.Errorf("browser: exhausted document generations")
	}
	return browser.nextDocument, nil
}

func (browser *Browser) replaceDocument(page *Page, oldGeneration, newGeneration DocumentGeneration) {
	if browser == nil || page == nil {
		return
	}
	browser.mutex.Lock()
	if browser.documents[oldGeneration] == page {
		delete(browser.documents, oldGeneration)
	}
	browser.documents[newGeneration] = page
	browser.mutex.Unlock()
}

func (browser *Browser) pageForDocument(generation DocumentGeneration) (*Page, bool) {
	if browser == nil || generation == 0 {
		return nil, false
	}
	browser.mutex.Lock()
	page := browser.documents[generation]
	browser.mutex.Unlock()
	return page, page != nil
}

func (browser *Browser) unregisterPage(page *Page, generation DocumentGeneration) {
	if browser == nil || page == nil {
		return
	}
	browser.mutex.Lock()
	if browser.pages[page.ID] == page {
		delete(browser.pages, page.ID)
	}
	if browser.documents[generation] == page {
		delete(browser.documents, generation)
	}
	browser.mutex.Unlock()
}

// LoadPage creates a Page, begins task-driven navigation, and drives its Realm
// until the final resource-aware frame is ready.
func (browser *Browser) LoadPage(ctx context.Context, rawURL string, client DocumentLoader) (*Page, error) {
	requestedURL, err := loader.ParseHTTPURL(rawURL)
	if err != nil {
		return nil, err
	}
	page, err := browser.NewPage(dom.NewDocument(), requestedURL)
	if err != nil {
		return nil, err
	}
	navigation, err := page.Navigate(ctx, rawURL, client)
	if err != nil {
		_ = page.Close()
		return nil, err
	}
	if err := page.WaitNavigation(ctx, navigation); err != nil {
		_ = page.Close()
		return nil, err
	}
	return page, nil
}

func (browser *Browser) Ledger() *ownership.Ledger {
	if browser == nil || browser.scheduler == nil {
		return nil
	}
	return browser.scheduler.Ledger()
}

func (browser *Browser) Close() error {
	if browser == nil {
		return nil
	}
	browser.mutex.Lock()
	if browser.closed {
		browser.mutex.Unlock()
		return nil
	}
	browser.closed = true
	pages := make([]*Page, 0, len(browser.pages))
	for _, page := range browser.pages {
		pages = append(pages, page)
	}
	browser.mutex.Unlock()

	var result error
	for _, page := range pages {
		result = errors.Join(result, page.Close())
	}
	result = errors.Join(result, browser.scheduler.Close())
	if browser.engine != nil {
		result = errors.Join(result, browser.engine.Close())
	}
	return result
}

func loadedURL(response *loader.Response, requested string) (*url.URL, error) {
	if response.URL != nil {
		clone := *response.URL
		return &clone, nil
	}
	parsed, err := loader.ParseHTTPURL(requested)
	if err != nil {
		return nil, fmt.Errorf("browser: resolve document URL: %w", err)
	}
	return parsed, nil
}

var (
	ErrBrowserClosed = errors.New("browser: browser is closed")
	ErrPageClosed    = errors.New("browser: page is closed")
)
