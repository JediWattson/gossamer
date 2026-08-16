package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

var (
	nextTaskID  atomic.Uint64
	nextQueueID atomic.Uint64
)

// Realm is Gossamer's unit of ordered page execution. Tasks and microtasks run
// on exactly one logical executor, while independent realms may run in
// separate goroutines.
type Realm struct {
	ID RealmID

	Tasks      *TaskQueue
	Microtasks *TaskQueue

	ledger    *ownership.Ledger
	store     *memory.Store
	owner     ownership.OwnerID
	region    ownership.RegionID
	ownsStore bool

	executing atomic.Bool
	closed    atomic.Bool
}

func NewRealm(id RealmID, ledger *ownership.Ledger) (*Realm, error) {
	if id == 0 {
		return nil, fmt.Errorf("runtime: invalid realm id 0")
	}
	if ledger == nil {
		ledger = ownership.NewLedger()
	}
	store := memory.NewStore(ledger)
	return newRealm(id, ledger, store, true)
}

func newRealm(id RealmID, ledger *ownership.Ledger, store *memory.Store, ownsStore bool) (*Realm, error) {
	if id == 0 {
		return nil, fmt.Errorf("runtime: invalid realm id 0")
	}
	owner := ownership.OwnerID{Kind: ownership.OwnerRealm, Value: uint64(id)}
	region, err := ledger.CreateRegion(owner)
	if err != nil {
		return nil, err
	}
	if err := store.RegisterOwner(owner); err != nil {
		_ = ledger.CloseRegion(region)
		return nil, err
	}
	tasks, err := newTaskQueue(QueueID(nextQueueID.Add(1)), ledger, store)
	if err != nil {
		_ = ledger.CloseRegion(region)
		return nil, err
	}
	microtasks, err := newTaskQueue(QueueID(nextQueueID.Add(1)), ledger, store)
	if err != nil {
		_ = tasks.close()
		_ = ledger.CloseRegion(region)
		return nil, err
	}
	return &Realm{
		ID:         id,
		Tasks:      tasks,
		Microtasks: microtasks,
		ledger:     ledger,
		store:      store,
		owner:      owner,
		region:     region,
		ownsStore:  ownsStore,
	}, nil
}

func (realm *Realm) Store() *memory.Store {
	if realm == nil {
		return nil
	}
	return realm.store
}

func (realm *Realm) Owner() ownership.OwnerID {
	if realm == nil {
		return ownership.OwnerID{}
	}
	return realm.owner
}

func (realm *Realm) Ledger() *ownership.Ledger {
	if realm == nil {
		return nil
	}
	return realm.ledger
}

// CollectNative runs an owner-local native cycle collection at a between-task
// Realm checkpoint. Roots are the Realm's current semantic native roots.
func (realm *Realm) CollectNative(roots ...memory.Ref) (memory.Collection, error) {
	if realm == nil {
		return memory.Collection{}, fmt.Errorf("runtime: nil realm")
	}
	if realm.closed.Load() {
		return memory.Collection{}, ErrRealmClosed
	}
	if realm.executing.Load() {
		return memory.Collection{}, ErrRealmRunning
	}
	return realm.store.Collect(realm.owner, roots...)
}

// EnqueueTask adds externally initiated work that carries no shadow objects.
func (realm *Realm) EnqueueTask(run TaskFunc) (TaskID, error) {
	return realm.enqueue(realm.Tasks, run, ownership.OwnerID{}, nil)
}

// EnqueueMicrotask adds externally initiated microtask work that carries no
// shadow objects. Task-owned values should use TaskContext.QueueMicrotask.
func (realm *Realm) EnqueueMicrotask(run TaskFunc) (TaskID, error) {
	return realm.enqueue(realm.Microtasks, run, ownership.OwnerID{}, nil)
}

// EnqueueRealmTask transfers Realm-owned objects into a newly runnable task.
// It is the firing boundary for callbacks retained persistently by the Realm.
func (realm *Realm) EnqueueRealmTask(run TaskFunc, objects ...ownership.ObjectID) (TaskID, error) {
	if realm == nil {
		return 0, fmt.Errorf("runtime: nil realm")
	}
	if realm.closed.Load() {
		return 0, ErrRealmClosed
	}
	if run == nil {
		return 0, ErrNilTask
	}
	id := TaskID(nextTaskID.Add(1))
	owner := ownership.OwnerID{Kind: ownership.OwnerTask, Value: uint64(id)}
	task, err := realm.newTask(id, owner, run, objects, nil)
	if err != nil {
		return 0, err
	}
	if err := realm.Tasks.enqueueTransfer(task, realm.owner); err != nil {
		_ = realm.store.ReleaseOwner(owner)
		return 0, err
	}
	return id, nil
}

func (realm *Realm) enqueue(queue *TaskQueue, run TaskFunc, publisher ownership.OwnerID, objects []ownership.ObjectID) (TaskID, error) {
	if realm == nil {
		return 0, fmt.Errorf("runtime: nil realm")
	}
	if realm.closed.Load() {
		return 0, ErrRealmClosed
	}
	if run == nil {
		return 0, ErrNilTask
	}
	id := TaskID(nextTaskID.Add(1))
	owner := ownership.OwnerID{Kind: ownership.OwnerTask, Value: uint64(id)}
	task, err := realm.newTask(id, owner, run, objects, nil)
	if err != nil {
		return 0, err
	}
	if err := queue.enqueue(task, publisher); err != nil {
		_ = realm.store.ReleaseOwner(owner)
		return 0, err
	}
	return id, nil
}

func (realm *Realm) enqueueMemory(queue *TaskQueue, run TaskFunc, publisher ownership.OwnerID, mode memorySendMode, refs []memory.Ref) (TaskID, error) {
	if realm == nil {
		return 0, fmt.Errorf("runtime: nil realm")
	}
	if realm.closed.Load() {
		return 0, ErrRealmClosed
	}
	if run == nil {
		return 0, ErrNilTask
	}
	id := TaskID(nextTaskID.Add(1))
	owner := ownership.OwnerID{Kind: ownership.OwnerTask, Value: uint64(id)}
	task, err := realm.newTask(id, owner, run, nil, refs)
	if err != nil {
		return 0, err
	}
	if err := queue.enqueueMemory(task, publisher, mode); err != nil {
		_ = realm.store.ReleaseOwner(owner)
		return 0, err
	}
	return id, nil
}

func (realm *Realm) newTask(id TaskID, owner ownership.OwnerID, run TaskFunc, objects []ownership.ObjectID, refs []memory.Ref) (Task, error) {
	region, err := realm.ledger.CreateRegion(owner)
	if err != nil {
		return Task{}, err
	}
	if err := realm.store.RegisterOwner(owner); err != nil {
		_ = realm.ledger.CloseRegion(region)
		return Task{}, err
	}
	memoryRegion, err := realm.store.NewRegion(owner)
	if err != nil {
		_ = realm.store.ReleaseOwner(owner)
		return Task{}, err
	}
	return Task{
		ID:           id,
		Run:          run,
		owner:        owner,
		region:       region,
		memoryRegion: memoryRegion,
		objects:      uniqueObjectIDs(objects),
		refs:         append([]memory.Ref(nil), refs...),
	}, nil
}

func uniqueObjectIDs(objects []ownership.ObjectID) []ownership.ObjectID {
	seen := make(map[ownership.ObjectID]struct{}, len(objects))
	result := make([]ownership.ObjectID, 0, len(objects))
	for _, object := range objects {
		if _, exists := seen[object]; exists {
			continue
		}
		seen[object] = struct{}{}
		result = append(result, object)
	}
	return result
}

// Run executes tasks until ctx is canceled or the realm is closed.
func (realm *Realm) Run(ctx context.Context) error {
	if err := realm.beginExecution(); err != nil {
		return err
	}
	defer realm.executing.Store(false)
	for {
		if err := realm.runOne(ctx); err != nil {
			if realm.closed.Load() && errors.Is(err, ErrQueueClosed) {
				return ErrRealmClosed
			}
			return err
		}
	}
}

// RunOne executes one task and then drains the realm's microtask queue.
func (realm *Realm) RunOne(ctx context.Context) error {
	if err := realm.beginExecution(); err != nil {
		return err
	}
	defer realm.executing.Store(false)
	return realm.runOne(ctx)
}

func (realm *Realm) runOne(ctx context.Context) error {
	task, err := realm.Tasks.pop(ctx)
	if err != nil {
		return err
	}
	taskErr := realm.execute(task)
	microtaskErr := realm.drainMicrotasks()
	return errors.Join(taskErr, microtaskErr)
}

func (realm *Realm) drainMicrotasks() error {
	var result error
	for {
		task, ok, err := realm.Microtasks.tryPop()
		if err != nil {
			return errors.Join(result, err)
		}
		if !ok {
			return result
		}
		result = errors.Join(result, realm.execute(task))
	}
}

func (realm *Realm) execute(task Task) (result error) {
	context := &TaskContext{
		Realm:        realm,
		TaskID:       task.ID,
		Owner:        task.owner,
		Region:       task.region,
		MemoryRegion: task.memoryRegion,
		Refs:         append([]memory.Ref(nil), task.refs...),
	}
	defer func() {
		result = errors.Join(result, realm.store.ReleaseOwner(task.owner))
	}()
	return task.Run(context)
}

func (realm *Realm) beginExecution() error {
	if realm == nil {
		return fmt.Errorf("runtime: nil realm")
	}
	if realm.closed.Load() {
		return ErrRealmClosed
	}
	if !realm.executing.CompareAndSwap(false, true) {
		return ErrRealmRunning
	}
	return nil
}

func (realm *Realm) Close() error {
	if realm == nil || realm.closed.Swap(true) {
		return nil
	}
	result := errors.Join(
		realm.Tasks.close(),
		realm.Microtasks.close(),
		realm.store.ReleaseOwner(realm.owner),
	)
	if realm.ownsStore {
		result = errors.Join(result, realm.store.Close())
	}
	return result
}
