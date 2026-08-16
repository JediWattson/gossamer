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
		return err
	}
	want := memory.HostObject{
		Class:    class,
		Scope:    uint64(generation),
		Identity: identity,
	}
	if record != want {
		return fmt.Errorf("browser: invalid async host record: got %#v, want %#v", record, want)
	}
	return page.invokeScript(context, generation, callback, autoRender)
}
