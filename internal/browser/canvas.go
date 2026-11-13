package browser

import "image"

func (host *taskHost) CanvasSize(handle NodeHandle) (width, height int, revision uint64, err error) {
	host.page.mutex.RLock()
	defer host.page.mutex.RUnlock()
	if err := host.validateHandleLocked(handle); err != nil {
		return 0, 0, 0, err
	}
	return host.page.document.CanvasSize(handle.Node)
}

func (host *taskHost) SetCanvasDimension(handle NodeHandle, name, value string) (width, height int, revision uint64, err error) {
	host.page.mutex.Lock()
	defer host.page.mutex.Unlock()
	if err := host.validateHandleLocked(handle); err != nil {
		return 0, 0, 0, err
	}
	width, height, revision, err = host.page.document.SetCanvasDimension(handle.Node, name, value)
	if err != nil {
		return 0, 0, 0, err
	}
	host.page.dirty = true
	host.mutated = true
	host.page.invalidateLayoutLocked()
	if syncErr := host.page.nodeLifetimes.sync(host.task); syncErr != nil {
		return 0, 0, 0, syncErr
	}
	return width, height, revision, nil
}

func (host *taskHost) ReadCanvas(handle NodeHandle, fn func(width, height int, revision uint64, bitmap *image.RGBA)) error {
	host.page.mutex.RLock()
	defer host.page.mutex.RUnlock()
	if err := host.validateHandleLocked(handle); err != nil {
		return err
	}
	return host.page.document.ReadCanvas(handle.Node, fn)
}

func (host *taskHost) MutateCanvas(handle NodeHandle, fn func(width, height int, bitmap *image.RGBA) bool) (uint64, error) {
	host.page.mutex.Lock()
	defer host.page.mutex.Unlock()
	if err := host.validateHandleLocked(handle); err != nil {
		return 0, err
	}
	revision, mutated, err := host.page.document.MutateCanvas(handle.Node, fn)
	if err != nil {
		return 0, err
	}
	if mutated {
		host.page.dirty = true
		host.canvasChanged = true
		host.page.invalidateLayoutLocked()
	}
	return revision, nil
}
