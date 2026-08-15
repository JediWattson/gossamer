package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

var nextBrowserID atomic.Uint64

// Scheduler owns independent realm actors and their shared ownership ledger.
type Scheduler struct {
	ledger        *ownership.Ledger
	store         *memory.Store
	browserOwner  ownership.OwnerID
	browserRegion ownership.RegionID

	mutex     sync.Mutex
	nextRealm RealmID
	realms    map[RealmID]*Realm
	closed    bool
}

func NewScheduler(ledger *ownership.Ledger) (*Scheduler, error) {
	if ledger == nil {
		ledger = ownership.NewLedger()
	}
	browser := ownership.OwnerID{Kind: ownership.OwnerBrowser, Value: nextBrowserID.Add(1)}
	region, err := ledger.CreateRegion(browser)
	if err != nil {
		return nil, err
	}
	store := memory.NewStore(ledger)
	if err := store.RegisterOwner(browser); err != nil {
		_ = ledger.CloseRegion(region)
		return nil, err
	}
	return &Scheduler{
		ledger:        ledger,
		store:         store,
		browserOwner:  browser,
		browserRegion: region,
		realms:        make(map[RealmID]*Realm),
	}, nil
}

func (scheduler *Scheduler) NewRealm() (*Realm, error) {
	if scheduler == nil {
		return nil, fmt.Errorf("runtime: nil scheduler")
	}
	scheduler.mutex.Lock()
	defer scheduler.mutex.Unlock()
	if scheduler.closed {
		return nil, ErrRealmClosed
	}
	scheduler.nextRealm++
	realm, err := newRealm(scheduler.nextRealm, scheduler.ledger, scheduler.store, false)
	if err != nil {
		return nil, err
	}
	scheduler.realms[realm.ID] = realm
	return realm, nil
}

// Start runs one realm actor and reports its terminal result once.
func (scheduler *Scheduler) Start(ctx context.Context, realm *Realm) (<-chan error, error) {
	if scheduler == nil || realm == nil {
		return nil, fmt.Errorf("runtime: nil scheduler or realm")
	}
	scheduler.mutex.Lock()
	owned := scheduler.realms[realm.ID] == realm
	closed := scheduler.closed
	scheduler.mutex.Unlock()
	if closed {
		return nil, ErrRealmClosed
	}
	if !owned {
		return nil, fmt.Errorf("runtime: realm %d is not owned by scheduler", realm.ID)
	}
	result := make(chan error, 1)
	go func() {
		result <- realm.Run(ctx)
		close(result)
	}()
	return result, nil
}

// EnqueueExternalTask publishes one browser-owned completion envelope through
// realm's task queue. The browser releases its claim after publication, so the
// queue-to-task handoff and task-region close describe the completion's full
// semantic lifetime.
func (scheduler *Scheduler) EnqueueExternalTask(realm *Realm, run TaskFunc) (TaskID, ownership.ObjectID, error) {
	if scheduler == nil || realm == nil {
		return 0, 0, fmt.Errorf("runtime: nil scheduler or realm")
	}
	if run == nil {
		return 0, 0, ErrNilTask
	}

	scheduler.mutex.Lock()
	defer scheduler.mutex.Unlock()
	if scheduler.closed {
		return 0, 0, ErrRealmClosed
	}
	if scheduler.realms[realm.ID] != realm {
		return 0, 0, fmt.Errorf("runtime: realm %d is not owned by scheduler", realm.ID)
	}

	envelope, err := scheduler.ledger.CreateObject(scheduler.browserRegion)
	if err != nil {
		return 0, 0, err
	}
	task, enqueueErr := realm.enqueue(realm.Tasks, run, scheduler.browserOwner, []ownership.ObjectID{envelope})
	releaseErr := scheduler.ledger.Release(envelope, scheduler.browserOwner)
	if enqueueErr != nil {
		return 0, envelope, errors.Join(enqueueErr, releaseErr)
	}
	return task, envelope, releaseErr
}

func (scheduler *Scheduler) Ledger() *ownership.Ledger {
	if scheduler == nil {
		return nil
	}
	return scheduler.ledger
}

func (scheduler *Scheduler) Store() *memory.Store {
	if scheduler == nil {
		return nil
	}
	return scheduler.store
}

func (scheduler *Scheduler) Close() error {
	if scheduler == nil {
		return nil
	}
	scheduler.mutex.Lock()
	if scheduler.closed {
		scheduler.mutex.Unlock()
		return nil
	}
	scheduler.closed = true
	realms := make([]*Realm, 0, len(scheduler.realms))
	for _, realm := range scheduler.realms {
		realms = append(realms, realm)
	}
	scheduler.mutex.Unlock()

	var result error
	for _, realm := range realms {
		result = errors.Join(result, realm.Close())
	}
	return errors.Join(result, scheduler.store.ReleaseOwner(scheduler.browserOwner), scheduler.store.Close())
}
