package browser

import (
	"fmt"

	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

const (
	browserTimerHostClass          memory.HostClass = 2
	browserCallbackHostClass       memory.HostClass = 3
	browserAnimationFrameHostClass memory.HostClass = 4
)

func (page *Page) releaseAsyncRef(ref memory.Ref) error {
	if page == nil || page.Realm == nil {
		return nil
	}
	return page.Realm.Store().DestroyRegion(page.Realm.Owner(), ref.Region)
}

// NativeMemoryRoots snapshots Realm-owned asynchronous records. Queued
// records are included as well because their ownership transfer can race a
// between-task collection; the Store atomically ignores records that have
// already moved to a queue or firing task.
func (page *Page) NativeMemoryRoots() []memory.Ref {
	if page == nil {
		return nil
	}
	page.mutex.RLock()
	defer page.mutex.RUnlock()
	refs := make([]memory.Ref, 0, len(page.timers)+len(page.animationFrames))
	for _, timer := range page.timers {
		refs = append(refs, timer.ref)
	}
	for _, frame := range page.animationFrames {
		refs = append(refs, frame.ref)
	}
	return refs
}

func (page *Page) invokeAsyncScript(
	context *browserruntime.TaskContext,
	class memory.HostClass,
	generation DocumentGeneration,
	identity uint64,
	callback ValueHandle,
	autoRender bool,
) error {
	if context == nil || len(context.Refs) != 1 {
		return fmt.Errorf("browser: async task has %d native records, want 1", len(context.Refs))
	}
	record, err := context.DerefHostObject(context.Refs[0])
	if err != nil {
		return fmt.Errorf("browser: dereference async class %d identity %d callback %d record %s: %w", class, identity, callback, context.Refs[0], err)
	}
	want := memory.HostObject{
		Class:    class,
		Scope:    uint64(generation),
		Identity: identity,
	}
	if record != want {
		return fmt.Errorf("browser: invalid async host record: got %#v, want %#v", record, want)
	}
	if err := page.invokeScript(context, generation, callback, autoRender); err != nil {
		return fmt.Errorf("browser: invoke async class %d identity %d callback %d: %w", class, identity, callback, err)
	}
	return nil
}
