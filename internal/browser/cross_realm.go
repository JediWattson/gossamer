package browser

import (
	"fmt"

	"github.com/JediWattson/gossamer/internal/dom"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

type NodeTransferResult struct {
	Handle NodeHandle
	Err    error
}

// QueueImportNodeFrom snapshots a source node in its Realm and copies the
// pointer-free encoding through the target Realm's task queue.
func (page *Page) QueueImportNodeFrom(source *Page, handle NodeHandle, deep bool) (<-chan NodeTransferResult, error) {
	return page.queueNodeTransfer(source, handle, deep, false)
}

// QueueAdoptNodeFrom moves a complete pointer-free node encoding through the
// target Realm queue and retires the source document's NodeIDs.
func (page *Page) QueueAdoptNodeFrom(source *Page, handle NodeHandle) (<-chan NodeTransferResult, error) {
	return page.queueNodeTransfer(source, handle, true, true)
}

func (page *Page) queueNodeTransfer(source *Page, handle NodeHandle, deep, adopt bool) (<-chan NodeTransferResult, error) {
	if page == nil || source == nil || page.browser == nil || page.browser != source.browser {
		return nil, fmt.Errorf("browser: node transfer requires pages from one Browser")
	}
	if page == source {
		return nil, fmt.Errorf("browser: cross-document transfer requires distinct pages")
	}
	page.mutex.RLock()
	targetGeneration := page.documentGeneration
	targetClosed := page.closed
	page.mutex.RUnlock()
	if targetClosed {
		return nil, ErrPageClosed
	}
	result := make(chan NodeTransferResult, 1)
	complete := func(handle NodeHandle, err error) {
		result <- NodeTransferResult{Handle: handle, Err: err}
		close(result)
	}

	_, _, err := page.browser.scheduler.EnqueueExternalTask(source.Realm, func(task *browserruntime.TaskContext) error {
		source.mutex.Lock()
		if source.closed {
			source.mutex.Unlock()
			complete(NodeHandle{}, ErrPageClosed)
			return ErrPageClosed
		}
		if handle.Document != source.documentGeneration || handle.Node == dom.InvalidNodeID {
			source.mutex.Unlock()
			complete(NodeHandle{}, ErrStaleNodeHandle)
			return ErrStaleNodeHandle
		}
		data, transferErr := source.document.ExportNode(handle.Node, deep)
		source.mutex.Unlock()
		if transferErr != nil {
			complete(NodeHandle{}, transferErr)
			return transferErr
		}

		buffer, allocErr := task.NewArrayBuffer(data)
		if allocErr != nil {
			complete(NodeHandle{}, allocErr)
			return allocErr
		}
		dataLength := len(data)
		var adoptionCommit chan error
		if adopt {
			adoptionCommit = make(chan error, 1)
		}
		run := func(next *browserruntime.TaskContext) error {
			if adoptionCommit != nil {
				if commitErr := <-adoptionCommit; commitErr != nil {
					complete(NodeHandle{}, commitErr)
					return commitErr
				}
			}
			if len(next.Refs) != 1 {
				refErr := fmt.Errorf("browser: node transfer delivered %d buffers, want 1", len(next.Refs))
				complete(NodeHandle{}, refErr)
				return refErr
			}
			bytes, readErr := next.ReadArrayBuffer(next.Refs[0], 0, uint64(dataLength))
			if readErr != nil {
				complete(NodeHandle{}, readErr)
				return readErr
			}
			page.mutex.Lock()
			if page.closed {
				page.mutex.Unlock()
				complete(NodeHandle{}, ErrPageClosed)
				return ErrPageClosed
			}
			if page.documentGeneration != targetGeneration {
				page.mutex.Unlock()
				complete(NodeHandle{}, ErrStaleNodeHandle)
				return nil
			}
			imported, importErr := page.document.ImportNode(bytes)
			if importErr == nil {
				importErr = page.nodeLifetimes.sync(next)
			}
			page.mutex.Unlock()
			if importErr != nil {
				complete(NodeHandle{}, importErr)
				return importErr
			}
			complete(NodeHandle{Document: targetGeneration, Node: imported}, nil)
			return nil
		}
		var enqueueErr error
		if adopt {
			_, enqueueErr = task.Transfer(page.Realm, run, buffer)
		} else {
			_, enqueueErr = task.Copy(page.Realm, run, buffer)
		}
		if enqueueErr != nil {
			complete(NodeHandle{}, enqueueErr)
			if adoptionCommit != nil {
				adoptionCommit <- enqueueErr
				close(adoptionCommit)
			}
			return enqueueErr
		}
		if !adopt {
			return nil
		}

		source.mutex.Lock()
		_, _, commitErr := source.document.TakeNode(handle.Node)
		if commitErr == nil {
			commitErr = source.nodeLifetimes.sync(task)
			source.dirty = true
		}
		source.mutex.Unlock()
		adoptionCommit <- commitErr
		close(adoptionCommit)
		return commitErr
	})
	if err != nil {
		close(result)
		return nil, err
	}
	return result, nil
}

// PostMessage structured-clones native roots into target's task queue. Listed
// ArrayBuffers are detached atomically; no Go object pointer crosses Realms.
func (page *Page) PostMessage(
	task *browserruntime.TaskContext,
	target *Page,
	run browserruntime.TaskFunc,
	transfers []memory.Ref,
	refs ...memory.Ref,
) (browserruntime.TaskID, error) {
	if page == nil || target == nil || task == nil || page.browser == nil || page.browser != target.browser {
		return 0, fmt.Errorf("browser: postMessage requires pages from one Browser")
	}
	if task.Realm != page.Realm {
		return 0, fmt.Errorf("browser: postMessage task does not belong to source page")
	}
	if run == nil {
		return 0, browserruntime.ErrNilTask
	}
	target.mutex.RLock()
	generation := target.documentGeneration
	closed := target.closed
	target.mutex.RUnlock()
	if closed {
		return 0, ErrPageClosed
	}
	return task.StructuredClone(target.Realm, func(next *browserruntime.TaskContext) error {
		target.mutex.RLock()
		current := !target.closed && target.documentGeneration == generation
		target.mutex.RUnlock()
		if !current {
			return nil
		}
		return run(next)
	}, transfers, refs...)
}
