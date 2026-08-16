package browser_test

import (
	"context"
	"errors"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
)

func TestBrowserNewBlankPageHasRenderedDocumentWithoutHistory(t *testing.T) {
	t.Parallel()

	browserRuntime, err := browser.New()
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()

	page, err := browserRuntime.NewBlankPage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if page.Frame() == nil || page.Document() == nil {
		t.Fatal("blank page did not render an indexed document")
	}
	if page.URL() != nil {
		t.Fatalf("blank page URL = %q, want nil", page.URL())
	}
	entries, current := page.History()
	if len(entries) != 0 || current != -1 {
		t.Fatalf("blank page history = %d entries at %d, want 0 at -1", len(entries), current)
	}
	if page.CanGoBack() || page.CanGoForward() {
		t.Fatal("blank page unexpectedly enabled history traversal")
	}

	if err := page.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := page.Navigate(context.Background(), "https://closed.gossamer.test/", nil); !errors.Is(err, browser.ErrPageClosed) {
		t.Fatalf("blank page navigation after close = %v, want ErrPageClosed", err)
	}
	if stats := browserRuntime.Ledger().Stats(); stats.LiveObjects != 0 || stats.PersistentObjects != 0 {
		t.Fatalf("blank page teardown ownership = %#v", stats)
	}
}
