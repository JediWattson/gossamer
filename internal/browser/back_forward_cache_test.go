package browser_test

import (
	"context"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
)

func TestBackForwardCacheRestoresGenerationsAndEvictsOldestRegion(t *testing.T) {
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
	aGeneration := page.DocumentGeneration()
	aNode, _ := page.Document().ElementByID("current")
	aHandle := browser.NodeHandle{Document: aGeneration, Node: aNode}

	navigateAndWait(t, page, "https://history.gossamer.test/b", client)
	bGeneration := page.DocumentGeneration()
	if page.BackForwardCacheSize() != 1 {
		t.Fatalf("cache size after A -> B = %d", page.BackForwardCacheSize())
	}
	if _, ok := page.Resolve(aHandle); ok {
		t.Fatal("cached A handle resolved while B was current")
	}

	back, err := page.Back(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := page.WaitNavigation(context.Background(), back); err != nil {
		t.Fatal(err)
	}
	if page.DocumentGeneration() != aGeneration {
		t.Fatalf("restored A generation = %d, want %d", page.DocumentGeneration(), aGeneration)
	}
	if _, ok := page.Resolve(aHandle); !ok {
		t.Fatal("restored A did not reactivate its original stable handle")
	}
	if got := client.loadCount("https://history.gossamer.test/a"); got != 1 {
		t.Fatalf("restored A loads = %d", got)
	}

	forward, err := page.Forward(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := page.WaitNavigation(context.Background(), forward); err != nil {
		t.Fatal(err)
	}
	if page.DocumentGeneration() != bGeneration {
		t.Fatalf("restored B generation = %d, want %d", page.DocumentGeneration(), bGeneration)
	}
	navigateAndWait(t, page, "https://history.gossamer.test/c", client)
	navigateAndWait(t, page, "https://history.gossamer.test/d", client)
	if page.BackForwardCacheSize() != 2 {
		t.Fatalf("bounded cache size = %d, want 2", page.BackForwardCacheSize())
	}
	for _, want := range []string{"https://history.gossamer.test/c", "https://history.gossamer.test/b"} {
		navigation, err := page.Back(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := page.WaitNavigation(context.Background(), navigation); err != nil {
			t.Fatal(err)
		}
		if page.URL().String() != want {
			t.Fatalf("restored URL = %q, want %q", page.URL(), want)
		}
	}

	back, err = page.Back(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := page.WaitNavigation(context.Background(), back); err != nil {
		t.Fatal(err)
	}
	if got := client.loadCount("https://history.gossamer.test/a"); got != 2 {
		t.Fatalf("evicted A loads = %d, want one reload", got)
	}
	if page.DocumentGeneration() == aGeneration {
		t.Fatal("evicted A unexpectedly retained its original generation")
	}
	if _, ok := page.Resolve(aHandle); ok {
		t.Fatal("evicted A handle resolved after network reconstruction")
	}
	if err := page.Close(); err != nil {
		t.Fatal(err)
	}
	if stats := browserRuntime.Ledger().Stats(); stats.LiveObjects != 0 || stats.PersistentObjects != 0 {
		t.Fatalf("BFCache teardown ownership = %#v", stats)
	}
}
