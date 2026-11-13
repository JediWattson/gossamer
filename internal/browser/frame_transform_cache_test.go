package browser

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"
	"testing"

	htmlparser "github.com/JediWattson/gossamer/internal/html"
	"github.com/JediWattson/gossamer/internal/loader"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestHitTestUsesTransformsPublishedWithExactFrame(t *testing.T) {
	engine, page, initialID := computedStyleTestPage(t, `<!doctype html><html><body style="margin:0">
		<button id="target" style="display:block;width:100px;height:40px">initial</button>
		<div style="height:140px"></div>
		<button id="scrolled" style="display:block;width:100px;height:40px">scrolled</button>
		<div style="height:180px"></div>
	</body></html>`)
	defer engine.Close()
	if err := page.SetViewport(render.Viewport{Width: 100, Height: 100}); err != nil {
		t.Fatal(err)
	}
	if err := page.Render(); err != nil {
		t.Fatal(err)
	}

	generation := page.DocumentGeneration()
	initial := NodeHandle{Document: generation, Node: initialID}
	scrolledID := mustPageElementID(t, page, "scrolled")
	scrolled := NodeHandle{Document: generation, Node: scrolledID}
	initialNode, ok := page.document.Resolve(initialID)
	if !ok {
		t.Fatal("initial node is not live")
	}
	page.mutex.RLock()
	publishedFrame := page.frame
	publishedTransform, indexed := page.frameTransforms[initialNode]
	page.mutex.RUnlock()
	if publishedFrame == nil || !indexed || publishedTransform.OffsetY != 0 {
		t.Fatalf("initial frame/transform = %p, %#v, indexed=%t", publishedFrame, publishedTransform, indexed)
	}
	if hit, ok := page.HitTest(10, 10); !ok || hit != initial {
		t.Fatalf("initial HitTest = %#v, %t; want %#v", hit, ok, initial)
	}

	changed, err := page.ScrollViewport(0, 180)
	if err != nil || !changed {
		t.Fatalf("ScrollViewport = %t, %v; want true, nil", changed, err)
	}
	// Scroll state is newer, but the displayed frame has not been replaced.
	// Hit testing must remain coupled to the old pixels until Render publishes
	// the new frame and transform snapshot together.
	page.mutex.RLock()
	if page.frame != publishedFrame || page.frameTransforms[initialNode] != publishedTransform {
		page.mutex.RUnlock()
		t.Fatal("pending scroll changed the published frame or transform snapshot")
	}
	page.mutex.RUnlock()
	if hit, ok := page.HitTest(10, 10); !ok || hit != initial {
		t.Fatalf("pre-render HitTest = %#v, %t; want still-published %#v", hit, ok, initial)
	}

	if err := page.Render(); err != nil {
		t.Fatal(err)
	}
	page.mutex.RLock()
	updatedFrame := page.frame
	updatedTransform := page.frameTransforms[initialNode]
	page.mutex.RUnlock()
	if updatedFrame == publishedFrame || updatedTransform.OffsetY != 180 {
		t.Fatalf("updated frame/transform = %p/%#v; want replacement with y offset 180", updatedFrame, updatedTransform)
	}
	if hit, ok := page.HitTest(10, 10); !ok || hit != scrolled {
		t.Fatalf("post-render HitTest = %#v, %t; want %#v", hit, ok, scrolled)
	}
}

func TestBackForwardCacheRestoresFrameAndTransformSnapshotTogether(t *testing.T) {
	browserRuntime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	client := frameTransformDocumentLoader{}
	page, err := browserRuntime.LoadPage(context.Background(), "https://frame-transform.test/a", client)
	if err != nil {
		t.Fatal(err)
	}
	if err := page.SetViewport(render.Viewport{Width: 100, Height: 100}); err != nil {
		t.Fatal(err)
	}
	targetID := mustPageElementID(t, page, "current")
	targetNode, ok := page.document.Resolve(targetID)
	if !ok {
		t.Fatal("target node is not live")
	}
	if changed, scrollErr := page.ScrollViewport(0, 120); scrollErr != nil || !changed {
		t.Fatalf("ScrollViewport = %t, %v; want true, nil", changed, scrollErr)
	}
	if err := page.Render(); err != nil {
		t.Fatal(err)
	}
	originalGeneration := page.DocumentGeneration()
	originalHandle := NodeHandle{Document: originalGeneration, Node: targetID}
	page.mutex.RLock()
	originalFrame := page.frame
	originalTransform, indexed := page.frameTransforms[targetNode]
	page.mutex.RUnlock()
	if originalFrame == nil || !indexed || originalTransform.OffsetY != 120 {
		t.Fatalf("cached candidate frame/transform = %p, %#v, indexed=%t", originalFrame, originalTransform, indexed)
	}
	if hit, ok := page.HitTest(10, 10); !ok || hit != originalHandle {
		t.Fatalf("candidate HitTest = %#v, %t; want %#v", hit, ok, originalHandle)
	}

	navigation, err := page.Navigate(context.Background(), "https://frame-transform.test/b", client)
	if err != nil {
		t.Fatal(err)
	}
	if err := page.WaitNavigation(context.Background(), navigation); err != nil {
		t.Fatal(err)
	}
	back, err := page.Back(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := page.WaitNavigation(context.Background(), back); err != nil {
		t.Fatal(err)
	}

	page.mutex.RLock()
	restoredFrame := page.frame
	restoredTransform, indexed := page.frameTransforms[targetNode]
	restoredGeneration := page.frameGeneration
	page.mutex.RUnlock()
	if restoredFrame != originalFrame || !indexed || restoredTransform != originalTransform || restoredGeneration != originalGeneration {
		t.Fatalf("restored frame/transform/generation = %p, %#v, %d; want %p, %#v, %d", restoredFrame, restoredTransform, restoredGeneration, originalFrame, originalTransform, originalGeneration)
	}
	if hit, ok := page.HitTest(10, 10); !ok || hit != originalHandle {
		t.Fatalf("restored HitTest = %#v, %t; want %#v", hit, ok, originalHandle)
	}
}

func TestPageCloseClearsFrameAndTransformSnapshotTogether(t *testing.T) {
	engine, page, _ := computedStyleTestPage(t, `<!doctype html><html><body><button id="target">target</button></body></html>`)
	defer engine.Close()
	if err := page.Render(); err != nil {
		t.Fatal(err)
	}
	page.mutex.RLock()
	frame, transforms := page.frame, page.frameTransforms
	page.mutex.RUnlock()
	if frame == nil || transforms == nil {
		t.Fatal("render did not publish a frame and transform snapshot")
	}
	if err := page.Close(); err != nil {
		t.Fatal(err)
	}
	page.mutex.RLock()
	defer page.mutex.RUnlock()
	if page.frame != nil || page.frameTransforms != nil || page.frameGeneration != 0 {
		t.Fatalf("closed frame state = %p, %#v, %d; want fully cleared", page.frame, page.frameTransforms, page.frameGeneration)
	}
}

func BenchmarkPageHitTestVisualTransforms(b *testing.B) {
	var source strings.Builder
	source.WriteString(`<!doctype html><html><body style="margin:0">`)
	for index := 0; index < 512; index++ {
		fmt.Fprintf(&source, `<button id="node-%d" style="display:block;width:100px;height:20px">%d</button>`, index, index)
	}
	source.WriteString(`</body></html>`)
	root, err := htmlparser.Parse(strings.NewReader(source.String()))
	if err != nil {
		b.Fatal(err)
	}
	engine, err := New()
	if err != nil {
		b.Fatal(err)
	}
	defer engine.Close()
	location, _ := url.Parse("https://frame-transform-benchmark.test/")
	page, err := engine.NewPage(root, location)
	if err != nil {
		b.Fatal(err)
	}
	if err := page.SetViewport(render.Viewport{Width: 100, Height: 10_240}); err != nil {
		b.Fatal(err)
	}
	if err := page.Render(); err != nil {
		b.Fatal(err)
	}

	b.Run("retained", func(b *testing.B) {
		b.ReportAllocs()
		for iteration := 0; iteration < b.N; iteration++ {
			if _, ok := page.HitTest(10, 10_230); !ok {
				b.Fatal("retained hit test missed")
			}
		}
	})
	b.Run("rebuild", func(b *testing.B) {
		b.ReportAllocs()
		page.mutex.RLock()
		defer page.mutex.RUnlock()
		for iteration := 0; iteration < b.N; iteration++ {
			transforms := page.visualTransformsLocked(page.frame)
			if render.HitTestVisual(page.frame, 10, 10_230, transforms) == nil {
				b.Fatal("rebuilt hit test missed")
			}
		}
	})
}

type frameTransformDocumentLoader struct{}

func (frameTransformDocumentLoader) Load(_ context.Context, rawURL string) (*loader.Response, error) {
	location, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	return &loader.Response{
		URL:        location,
		StatusCode: 200,
		Body: io.NopCloser(strings.NewReader(`<!doctype html><html><body style="margin:0">
			<div style="height:120px"></div>
			<button id="current" style="display:block;width:100px;height:40px">` + location.Path + `</button>
			<div style="height:180px"></div>
		</body></html>`)),
	}, nil
}
