package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

var nextBrowserID atomic.Uint64

// Scheduler owns independent realm actors and their shared ownership ledger.
type Scheduler struct {
	ledger        *ownership.Ledger
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
	return &Scheduler{ledger: ledger, browserRegion: region, realms: make(map[RealmID]*Realm)}, nil
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
	realm, err := NewRealm(scheduler.nextRealm, scheduler.ledger)
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

func (scheduler *Scheduler) Ledger() *ownership.Ledger {
	if scheduler == nil {
		return nil
	}
	return scheduler.ledger
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
	return errors.Join(result, scheduler.ledger.CloseRegion(scheduler.browserRegion))
}
