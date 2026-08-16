package browser_test

import (
	"context"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
)

func TestSameDocumentHistoryRetainsRealmAndTruncatesForwardEntries(t *testing.T) {
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
	generation := page.DocumentGeneration()

	if changed, err := page.PushState(`{"step":1}`, "?route=one#one", false); err != nil || !changed {
		t.Fatalf("PushState(step 1) changed=%v error=%v", changed, err)
	}
	if changed, err := page.PushState(`{"step":2}`, "#two", false); err != nil || !changed {
		t.Fatalf("PushState(step 2) changed=%v error=%v", changed, err)
	}
	if page.DocumentGeneration() != generation {
		t.Fatal("pushState replaced the active document generation")
	}
	if snapshot := page.SessionHistorySnapshot(); snapshot.Length != 3 || snapshot.Index != 2 || snapshot.StateJSON != `{"step":2}` {
		t.Fatalf("history snapshot after pushes = %#v", snapshot)
	}

	back, err := page.Back(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := page.WaitNavigation(context.Background(), back); err != nil {
		t.Fatal(err)
	}
	if page.DocumentGeneration() != generation || page.URL().String() != "https://history.gossamer.test/a?route=one#one" {
		t.Fatalf("same-document back generation=%d URL=%q", page.DocumentGeneration(), page.URL())
	}
	if snapshot := page.SessionHistorySnapshot(); snapshot.StateJSON != `{"step":1}` {
		t.Fatalf("history.state after back = %s", snapshot.StateJSON)
	}

	if _, err := page.PushState(`{"step":3}`, "#three", false); err != nil {
		t.Fatal(err)
	}
	if page.CanGoForward() {
		t.Fatal("pushState from the middle retained a forward entry")
	}
	entries, index := page.History()
	if len(entries) != 3 || index != 2 || entries[2].URL.String() != "https://history.gossamer.test/a?route=one#three" {
		t.Fatalf("history after forward truncation = %#v index=%d", entries, index)
	}
	if got := client.loadCount("https://history.gossamer.test/a"); got != 1 {
		t.Fatalf("same-document history reloaded the document %d times", got)
	}
}

func TestHistoryURLValidationAndLiveLocationComponents(t *testing.T) {
	t.Parallel()

	browserRuntime, err := browser.New()
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	client := &historyDocumentLoader{}
	page, err := browserRuntime.LoadPage(context.Background(), "https://history.gossamer.test:8443/a?q=one#old", client)
	if err != nil {
		t.Fatal(err)
	}

	for component, want := range map[browser.LocationComponent]string{
		browser.LocationHref:     "https://history.gossamer.test:8443/a?q=one#old",
		browser.LocationOrigin:   "https://history.gossamer.test:8443",
		browser.LocationProtocol: "https:",
		browser.LocationHost:     "history.gossamer.test:8443",
		browser.LocationHostname: "history.gossamer.test",
		browser.LocationPort:     "8443",
		browser.LocationPathname: "/a",
		browser.LocationSearch:   "?q=one",
		browser.LocationHash:     "#old",
	} {
		got, err := page.LocationComponent(component)
		if err != nil || got != want {
			t.Fatalf("LocationComponent(%d) = %q, %v, want %q", component, got, err, want)
		}
	}

	if _, err := page.PushState("not-json", "", false); domExceptionName(err) != dom.DataCloneError {
		t.Fatalf("invalid state error = %v", err)
	}
	if _, err := page.PushState("null", "https://other.test/", false); domExceptionName(err) != dom.SecurityError {
		t.Fatalf("cross-origin state URL error = %v", err)
	}
	if err := page.SetLocationComponent(browser.LocationHash, "#new"); err != nil {
		t.Fatal(err)
	}
	navigation := page.Navigation().ID
	if navigation == 0 {
		t.Fatal("location.hash did not schedule a same-document navigation")
	}
	if err := page.WaitNavigation(context.Background(), navigation); err != nil {
		t.Fatal(err)
	}
	if page.URL().String() != "https://history.gossamer.test:8443/a?q=one#new" {
		t.Fatalf("location.hash URL = %q", page.URL())
	}
	if got := client.loadCount("https://history.gossamer.test:8443/a?q=one#old"); got != 1 {
		t.Fatalf("location.hash reloaded the document %d times", got)
	}
}

func domExceptionName(err error) dom.ExceptionName {
	name, _ := dom.ErrorExceptionName(err)
	return name
}
