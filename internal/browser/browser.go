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
	htmlparser "github.com/JediWattson/gossamer/internal/html"
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

	mutex    sync.Mutex
	nextPage PageID
	pages    map[PageID]*Page
	closed   bool
}

func New() (*Browser, error) {
	scheduler, err := browserruntime.NewScheduler(nil)
	if err != nil {
		return nil, err
	}
	return &Browser{scheduler: scheduler, pages: make(map[PageID]*Page)}, nil
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

	browser.mutex.Lock()
	defer browser.mutex.Unlock()
	if browser.closed {
		_ = realm.Close()
		return nil, ErrBrowserClosed
	}
	browser.nextPage++
	page := newPage(browser.nextPage, realm, document, location)
	browser.pages[page.ID] = page
	return page, nil
}

// LoadPage loads and parses one document. Subresources can be attached through
// Page.SetResources before rendering; the existing CLI resource path remains
// unchanged during this boundary migration.
func (browser *Browser) LoadPage(ctx context.Context, rawURL string, client DocumentLoader) (*Page, error) {
	if client == nil {
		client = loader.New(nil)
	}
	response, err := client.Load(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	if response == nil || response.Body == nil {
		return nil, fmt.Errorf("browser: empty document response")
	}
	defer response.Body.Close()
	root, err := htmlparser.Parse(response.Body)
	if err != nil {
		return nil, fmt.Errorf("browser: parse document: %w", err)
	}
	location, err := loadedURL(response, rawURL)
	if err != nil {
		return nil, err
	}
	return browser.NewPage(root, location)
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
	return errors.Join(result, browser.scheduler.Close())
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
