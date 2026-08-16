package browser

import (
	"context"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/html"
	"github.com/JediWattson/gossamer/internal/render"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
)

func TestAnimationFramesBatchCancelAndShareMonotonicTimestamp(t *testing.T) {
	script := &timerTestRealm{}
	engine, err := NewWithEngine(&timerTestEngine{realm: script})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	root, err := html.Parse(strings.NewReader(`<!doctype html><html><body></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://animation.test/")
	page, err := engine.NewPage(root, location)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := page.Realm.EnqueueTask(func(task *browserruntime.TaskContext) error {
		first, err := page.requestAnimationFrameFromTask(task, 7)
		if err != nil {
			return err
		}
		canceled, err := page.requestAnimationFrameFromTask(task, 8)
		if err != nil {
			return err
		}
		if err := page.cancelAnimationFrame(canceled); err != nil {
			return err
		}
		if _, err := page.requestAnimationFrameFromTask(task, 9); err != nil {
			return err
		}
		if first == canceled {
			t.Fatal("animation frame IDs were reused")
		}
		return page.queueAnimationFrameFromTask()
	}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := page.Realm.Tasks.Len(); got != 1 {
		t.Fatalf("frame tasks = %d, want one coalesced task", got)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(script.animationInvoked, []ValueHandle{7, 9}) {
		t.Fatalf("animation callbacks = %v, want [7 9]", script.animationInvoked)
	}
	if len(script.animationTimes) != 2 || script.animationTimes[0] <= 0 || script.animationTimes[0] != script.animationTimes[1] {
		t.Fatalf("animation timestamps = %v, want one positive shared timestamp", script.animationTimes)
	}
	page.mutex.Lock()
	now := page.performanceNowLocked()
	page.mutex.Unlock()
	if now < script.animationTimes[0] {
		t.Fatalf("performance.now = %g, before frame timestamp %g", now, script.animationTimes[0])
	}
}

func TestQueueViewportResizeDispatchesThenPublishesOneFrame(t *testing.T) {
	script := &timerTestRealm{}
	engine, err := NewWithEngine(&timerTestEngine{realm: script})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	root, err := html.Parse(strings.NewReader(`<!doctype html><html><body><div style="width:50vw"></div></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://resize.test/")
	page, err := engine.NewPage(root, location)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.QueueViewportResize(render.Viewport{Width: 640, Height: 360}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(script.dispatched) != 1 || script.dispatched[0].Type != InputResize {
		t.Fatalf("resize events = %#v", script.dispatched)
	}
	if page.viewport != (render.Viewport{Width: 640, Height: 360}) || page.Realm.Tasks.Len() != 1 {
		t.Fatalf("viewport/tasks = %#v/%d", page.viewport, page.Realm.Tasks.Len())
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if page.Frame() == nil || page.Frame().Viewport != page.viewport {
		t.Fatalf("resized frame = %#v", page.Frame())
	}
}
