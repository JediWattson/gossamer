package runtime

import (
	"context"
	"fmt"
	"sync"

	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

// TaskQueue is a first-class publication boundary. Enqueue publishes carried
// objects to the queue owner; pop transfers those references to the task.
type TaskQueue struct {
	ID QueueID

	ledger *ownership.Ledger
	owner  ownership.OwnerID
	region ownership.RegionID

	mutex  sync.Mutex
	items  []Task
	head   int
	closed bool
	notify chan struct{}
}

func newTaskQueue(id QueueID, ledger *ownership.Ledger) (*TaskQueue, error) {
	owner := ownership.OwnerID{Kind: ownership.OwnerQueue, Value: uint64(id)}
	region, err := ledger.CreateRegion(owner)
	if err != nil {
		return nil, err
	}
	return &TaskQueue{
		ID:     id,
		ledger: ledger,
		owner:  owner,
		region: region,
		notify: make(chan struct{}, 1),
	}, nil
}

func (queue *TaskQueue) Owner() ownership.OwnerID {
	if queue == nil {
		return ownership.OwnerID{}
	}
	return queue.owner
}

func (queue *TaskQueue) Len() int {
	if queue == nil {
		return 0
	}
	queue.mutex.Lock()
	length := len(queue.items) - queue.head
	queue.mutex.Unlock()
	return length
}

func (queue *TaskQueue) enqueue(task Task, publisher ownership.OwnerID) error {
	if queue == nil {
		return fmt.Errorf("runtime: nil task queue")
	}
	if task.Run == nil {
		return ErrNilTask
	}
	queue.mutex.Lock()
	defer queue.mutex.Unlock()
	if queue.closed {
		return ErrQueueClosed
	}
	if err := queue.ledger.PublishAll(task.objects, publisher, queue.owner); err != nil {
		return err
	}
	queue.items = append(queue.items, task)
	queue.signal()
	return nil
}

// enqueueTransfer moves a persistent publisher claim into the queue instead
// of retaining both owners. Timers use this when a Realm-owned callback becomes
// runnable exactly once.
func (queue *TaskQueue) enqueueTransfer(task Task, publisher ownership.OwnerID) error {
	if queue == nil {
		return fmt.Errorf("runtime: nil task queue")
	}
	if task.Run == nil {
		return ErrNilTask
	}
	queue.mutex.Lock()
	defer queue.mutex.Unlock()
	if queue.closed {
		return ErrQueueClosed
	}
	if err := queue.ledger.TransferAll(task.objects, publisher, queue.owner); err != nil {
		return err
	}
	queue.items = append(queue.items, task)
	queue.signal()
	return nil
}

func (queue *TaskQueue) pop(ctx context.Context) (Task, error) {
	for {
		queue.mutex.Lock()
		task, ok, err := queue.takeLocked()
		closed := queue.closed
		queue.mutex.Unlock()
		if err != nil {
			return Task{}, err
		}
		if ok {
			return task, nil
		}
		if closed {
			return Task{}, ErrQueueClosed
		}

		select {
		case <-ctx.Done():
			return Task{}, ctx.Err()
		case <-queue.notify:
		}
	}
}

func (queue *TaskQueue) tryPop() (Task, bool, error) {
	if queue == nil {
		return Task{}, false, fmt.Errorf("runtime: nil task queue")
	}
	queue.mutex.Lock()
	defer queue.mutex.Unlock()
	return queue.takeLocked()
}

func (queue *TaskQueue) takeLocked() (Task, bool, error) {
	if queue.head == len(queue.items) {
		return Task{}, false, nil
	}
	task := queue.items[queue.head]
	poppedGraph, err := queue.ledger.Reachable(task.objects)
	if err != nil {
		return Task{}, false, err
	}
	remainingRoots := make([]ownership.ObjectID, 0)
	for index := queue.head + 1; index < len(queue.items); index++ {
		remainingRoots = append(remainingRoots, queue.items[index].objects...)
	}
	remainingGraph, err := queue.ledger.Reachable(remainingRoots)
	if err != nil {
		return Task{}, false, err
	}
	shared, exclusive := partitionReachability(poppedGraph, remainingGraph)
	if err := queue.ledger.Handoff(shared, exclusive, queue.owner, task.owner); err != nil {
		return Task{}, false, err
	}
	queue.items[queue.head] = Task{}
	queue.head++
	queue.compactLocked()
	if queue.head < len(queue.items) {
		queue.signal()
	}
	return task, true, nil
}

func partitionReachability(popped, remaining []ownership.ObjectID) (shared, exclusive []ownership.ObjectID) {
	stillQueued := make(map[ownership.ObjectID]struct{}, len(remaining))
	for _, object := range remaining {
		stillQueued[object] = struct{}{}
	}
	for _, object := range popped {
		if _, exists := stillQueued[object]; exists {
			shared = append(shared, object)
		} else {
			exclusive = append(exclusive, object)
		}
	}
	return shared, exclusive
}

func (queue *TaskQueue) compactLocked() {
	remaining := len(queue.items) - queue.head
	if remaining == 0 {
		queue.items = nil
		queue.head = 0
		return
	}
	if queue.head < 64 || queue.head*2 < len(queue.items) {
		return
	}
	copy(queue.items, queue.items[queue.head:])
	clear(queue.items[remaining:])
	queue.items = queue.items[:remaining]
	queue.head = 0
}

func (queue *TaskQueue) close() error {
	if queue == nil {
		return nil
	}
	queue.mutex.Lock()
	if queue.closed {
		queue.mutex.Unlock()
		return nil
	}
	queue.closed = true
	pending := append([]Task(nil), queue.items[queue.head:]...)
	clear(queue.items)
	queue.items = nil
	queue.head = 0
	queue.signal()
	queue.mutex.Unlock()

	closeErr := queue.ledger.CloseRegion(queue.region)
	for _, task := range pending {
		if err := queue.ledger.CloseRegion(task.region); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	return closeErr
}

func (queue *TaskQueue) signal() {
	select {
	case queue.notify <- struct{}{}:
	default:
	}
}
