package browser_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/loader"
)

func TestHistoryTraversalRebuildsDocumentsAndPreservesSessionIndex(t *testing.T) {
	t.Parallel()

	browserRuntime, err := browser.New()
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	client := &historyDocumentLoader{}
	page, err := browserRuntime.LoadPage(context.Background(), "https://history.gossamer.test/a", client)
	if err != nil {
		t.Fatal(err)
	}
	assertHistoryPosition(t, page, []string{
		"https://history.gossamer.test/a",
	}, 0)
	if page.CanGoBack() || page.CanGoForward() {
		t.Fatal("initial history entry exposed back or forward traversal")
	}

	navigateAndWait(t, page, "https://history.gossamer.test/b", client)
	navigateAndWait(t, page, "https://history.gossamer.test/c", client)
	assertHistoryPosition(t, page, []string{
		"https://history.gossamer.test/a",
		"https://history.gossamer.test/b",
		"https://history.gossamer.test/c",
	}, 2)
	if !page.CanGoBack() || page.CanGoForward() {
		t.Fatal("history tail did not expose only backward traversal")
	}
	cGeneration := page.DocumentGeneration()
	cID, found := page.Document().ElementByID("current")
	if !found {
		t.Fatal("current C element has no stable identity")
	}
	cHandle := browser.NodeHandle{Document: cGeneration, Node: cID}

	back, err := page.Back(context.Background(), client)
	if err != nil {
		t.Fatal(err)
	}
	if err := page.WaitNavigation(context.Background(), back); err != nil {
		t.Fatal(err)
	}
	if got := page.URL().String(); got != "https://history.gossamer.test/b" {
		t.Fatalf("back URL = %q", got)
	}
	if page.DocumentGeneration() == cGeneration {
		t.Fatal("back traversal reused the departed document generation")
	}
	if _, ok := page.Resolve(cHandle); ok {
		t.Fatal("back traversal retained a wrapper handle from the departed document")
	}
	if !frameContainsText(page.Frame(), "document /b") {
		t.Fatal("back traversal did not render the rebuilt B document")
	}
	assertHistoryPosition(t, page, []string{
		"https://history.gossamer.test/a",
		"https://history.gossamer.test/b",
		"https://history.gossamer.test/c",
	}, 1)
	if !page.CanGoBack() || !page.CanGoForward() {
		t.Fatal("middle history entry did not expose both traversal directions")
	}

	back, err = page.Back(context.Background(), client)
	if err != nil {
		t.Fatal(err)
	}
	if err := page.WaitNavigation(context.Background(), back); err != nil {
		t.Fatal(err)
	}
	forward, err := page.Forward(context.Background(), client)
	if err != nil {
		t.Fatal(err)
	}
	if err := page.WaitNavigation(context.Background(), forward); err != nil {
		t.Fatal(err)
	}
	if got := page.URL().String(); got != "https://history.gossamer.test/b" {
		t.Fatalf("forward URL = %q", got)
	}

	entriesBeforeReload, indexBeforeReload := page.History()
	reload, err := page.Reload(context.Background(), client)
	if err != nil {
		t.Fatal(err)
	}
	if err := page.WaitNavigation(context.Background(), reload); err != nil {
		t.Fatal(err)
	}
	entriesAfterReload, indexAfterReload := page.History()
	if len(entriesAfterReload) != len(entriesBeforeReload) || indexAfterReload != indexBeforeReload {
		t.Fatalf("reload changed history length/index from %d/%d to %d/%d", len(entriesBeforeReload), indexBeforeReload, len(entriesAfterReload), indexAfterReload)
	}

	navigateAndWait(t, page, "https://history.gossamer.test/d", client)
	assertHistoryPosition(t, page, []string{
		"https://history.gossamer.test/a",
		"https://history.gossamer.test/b",
		"https://history.gossamer.test/d",
	}, 2)
	if page.CanGoForward() {
		t.Fatal("new navigation from the middle retained the old forward entry")
	}
	if _, err := page.Go(context.Background(), 1, client); !errors.Is(err, browser.ErrHistoryTraversalOutOfRange) {
		t.Fatalf("out-of-range traversal error = %v", err)
	}
	if got := client.loadCount("https://history.gossamer.test/b"); got != 4 {
		t.Fatalf("B document loads = %d, want navigation, back, forward, and reload", got)
	}

	if err := page.Close(); err != nil {
		t.Fatal(err)
	}
	if stats := browserRuntime.Ledger().Stats(); stats.LiveObjects != 0 || stats.PersistentObjects != 0 {
		t.Fatalf("history teardown ownership = %#v", stats)
	}
}

func TestFailedHistoryTraversalLeavesCurrentEntryUntouched(t *testing.T) {
	t.Parallel()

	browserRuntime, err := browser.New()
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	client := &historyDocumentLoader{}
	page, err := browserRuntime.LoadPage(context.Background(), "https://history.gossamer.test/a", client)
	if err != nil {
		t.Fatal(err)
	}
	navigateAndWait(t, page, "https://history.gossamer.test/b", client)
	back, err := page.Back(context.Background(), failingHistoryLoader{})
	if err != nil {
		t.Fatal(err)
	}
	if err := page.WaitNavigation(context.Background(), back); err == nil {
		t.Fatal("failed history traversal unexpectedly completed")
	}
	assertHistoryPosition(t, page, []string{
		"https://history.gossamer.test/a",
		"https://history.gossamer.test/b",
	}, 1)
	if got := page.URL().String(); got != "https://history.gossamer.test/b" {
		t.Fatalf("failed traversal changed current URL to %q", got)
	}
	if !page.CanGoBack() || page.CanGoForward() {
		t.Fatal("failed traversal changed history capabilities")
	}
}

func navigateAndWait(t *testing.T, page *browser.Page, rawURL string, client browser.DocumentLoader) {
	t.Helper()
	navigation, err := page.Navigate(context.Background(), rawURL, client)
	if err != nil {
		t.Fatal(err)
	}
	if err := page.WaitNavigation(context.Background(), navigation); err != nil {
		t.Fatal(err)
	}
}

func assertHistoryPosition(t *testing.T, page *browser.Page, wantURLs []string, wantIndex int) {
	t.Helper()
	entries, index := page.History()
	if index != wantIndex || len(entries) != len(wantURLs) {
		t.Fatalf("history length/index = %d/%d, want %d/%d", len(entries), index, len(wantURLs), wantIndex)
	}
	for entryIndex, want := range wantURLs {
		if entries[entryIndex].URL == nil || entries[entryIndex].URL.String() != want {
			t.Fatalf("history[%d] = %#v, want %q", entryIndex, entries[entryIndex], want)
		}
	}
}

type historyDocumentLoader struct {
	mutex sync.Mutex
	loads []string
}

type failingHistoryLoader struct{}

func (failingHistoryLoader) Load(context.Context, string) (*loader.Response, error) {
	return nil, errors.New("history transport failed")
}

func (client *historyDocumentLoader) Load(_ context.Context, rawURL string) (*loader.Response, error) {
	location, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	client.mutex.Lock()
	client.loads = append(client.loads, rawURL)
	client.mutex.Unlock()
	return &loader.Response{
		URL: location, StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(`<html><body><p id="current">document ` + location.Path + `</p></body></html>`)),
	}, nil
}

func (client *historyDocumentLoader) loadCount(rawURL string) int {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	count := 0
	for _, loaded := range client.loads {
		if loaded == rawURL {
			count++
		}
	}
	return count
}
