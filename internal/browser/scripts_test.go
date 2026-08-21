package browser

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/loader"
	"github.com/JediWattson/gossamer/internal/resource"
)

func TestNavigationSnapshotCopiesBoundedScriptFailureDiagnostics(t *testing.T) {
	page := &Page{navigation: navigationRecord{}}
	longMessage := strings.Repeat("x", 5000)
	for index := 0; index < 65; index++ {
		page.appendScriptFailureLocked(ScriptFailure{
			URL: " https://example.test/app.js ", Phase: " evaluate ", Message: " " + longMessage + " ",
		})
	}
	snapshot := page.navigationSnapshotLocked()
	if len(snapshot.ScriptFailures) != 64 {
		t.Fatalf("script failure diagnostics = %d, want 64", len(snapshot.ScriptFailures))
	}
	first := snapshot.ScriptFailures[0]
	if first.URL != "https://example.test/app.js" || first.Phase != "evaluate" || len(first.Message) != 4099 || !strings.HasSuffix(first.Message, "...") {
		t.Fatalf("bounded script failure = %#v", first)
	}
	snapshot.ScriptFailures[0].Message = "changed"
	if page.navigation.scriptFailures[0].Message == "changed" {
		t.Fatal("NavigationSnapshot exposed mutable script failure storage")
	}
}

func TestNavigationScriptSequenceDoesNotDelayDOMContentLoadedForAsyncScript(t *testing.T) {
	t.Parallel()
	asyncURL, _ := url.Parse("https://example.test/async.js")
	releaseAsync := make(chan struct{})
	pipeline := resource.NewPipeline(blockingScriptFetcher{release: releaseAsync}, resource.PipelineOptions{})
	scripts := []navigationScript{
		{mode: navigationBlockingScript, source: ScriptSource{URL: "inline-blocking", Source: "blocking"}},
		{mode: navigationDeferredScript, source: ScriptSource{URL: "inline-deferred", Source: "deferred"}},
		{
			mode:   navigationAsyncScript,
			source: ScriptSource{URL: asyncURL.String()},
			external: &resource.Reference{
				Kind: resource.Script,
				URL:  asyncURL,
			},
		},
	}

	var mutex sync.Mutex
	var order []string
	delivered := make(chan struct{}, 8)
	done := make(chan error, 1)
	go func() {
		done <- loadNavigationScriptSequence(context.Background(), pipeline, scripts, func(result navigationScriptResult) error {
			label := result.source.Source
			switch result.kind {
			case navigationReadyInteractive:
				label = "interactive"
			case navigationDOMContentLoaded:
				label = "DOMContentLoaded"
			case navigationReadyComplete:
				label = "complete"
			}
			mutex.Lock()
			order = append(order, label)
			mutex.Unlock()
			delivered <- struct{}{}
			return nil
		})
	}()

	for index := 0; index < 4; index++ {
		select {
		case <-delivered:
		case <-time.After(time.Second):
			t.Fatal("script sequence did not reach DOMContentLoaded while async fetch was blocked")
		}
	}
	mutex.Lock()
	beforeRelease := append([]string(nil), order...)
	mutex.Unlock()
	if want := []string{"blocking", "interactive", "deferred", "DOMContentLoaded"}; !reflect.DeepEqual(beforeRelease, want) {
		t.Fatalf("order before async release = %#v, want %#v", beforeRelease, want)
	}

	close(releaseAsync)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("script sequence did not complete after async fetch release")
	}
	mutex.Lock()
	afterRelease := append([]string(nil), order...)
	mutex.Unlock()
	if want := []string{"blocking", "interactive", "deferred", "DOMContentLoaded", "async", "complete"}; !reflect.DeepEqual(afterRelease, want) {
		t.Fatalf("final script order = %#v, want %#v", afterRelease, want)
	}
}

type blockingScriptFetcher struct {
	release <-chan struct{}
}

func (fetcher blockingScriptFetcher) LoadResource(ctx context.Context, rawURL string, _ loader.Destination) (*loader.Response, error) {
	select {
	case <-fetcher.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	return &loader.Response{
		URL:        parsed,
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/javascript"}},
		Body:       io.NopCloser(strings.NewReader("async")),
	}, nil
}
