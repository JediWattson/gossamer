package browser

import (
	"errors"

	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
)

// dispatchDepartureLifecycle runs while the old document is still current.
// Returning allowed=false leaves that document, its Realm, and its history
// position intact.
func (page *Page) dispatchDepartureLifecycle(
	task *browserruntime.TaskContext,
	navigation NavigationID,
	persisted bool,
) (allowed bool, err error) {
	page.mutex.RLock()
	if !page.matchesNavigationLocked(navigation, 0) {
		page.mutex.RUnlock()
		return false, ErrNavigationSuperseded
	}
	generation := page.documentGeneration
	script := page.script
	page.mutex.RUnlock()
	if script == nil {
		return true, nil
	}
	host := &taskHost{page: page, task: task, generation: generation, autoRender: false}
	dispatch := func(eventType InputEventType, eventPersisted bool) (EventDispatchResult, error) {
		result, dispatchErr := script.DispatchEvent(host, InputEvent{
			Type: eventType, Target: NodeHandle{Document: generation}, Persisted: eventPersisted,
		})
		dispatchErr = errors.Join(dispatchErr, script.DrainMicrotasks(host), host.finish())
		return result, dispatchErr
	}
	result, err := dispatch(InputBeforeUnload, false)
	if err != nil {
		return false, err
	}
	if result.DefaultPrevented {
		return false, ErrNavigationCanceled
	}
	page.mutex.RLock()
	stillCurrent := page.matchesNavigationLocked(navigation, generation)
	page.mutex.RUnlock()
	if !stillCurrent {
		return false, ErrNavigationSuperseded
	}
	_, pageHideErr := dispatch(InputPageHide, persisted)
	var unloadErr error
	if !persisted {
		_, unloadErr = dispatch(InputUnload, false)
	}
	return true, errors.Join(pageHideErr, unloadErr)
}

func (page *Page) dispatchWindowNavigationLifecycleEvent(
	task *browserruntime.TaskContext,
	id NavigationID,
	generation DocumentGeneration,
	eventType InputEventType,
	persisted bool,
) error {
	page.mutex.RLock()
	if !page.matchesNavigationLocked(id, generation) || page.navigation.state != NavigationLoadingScripts {
		page.mutex.RUnlock()
		return nil
	}
	script := page.script
	page.mutex.RUnlock()
	if script == nil {
		return nil
	}
	host := &taskHost{page: page, task: task, generation: generation, autoRender: false}
	_, dispatchErr := script.DispatchEvent(host, InputEvent{
		Type: eventType, Target: NodeHandle{Document: generation}, Persisted: persisted,
	})
	return errors.Join(dispatchErr, script.DrainMicrotasks(host), host.finish())
}
