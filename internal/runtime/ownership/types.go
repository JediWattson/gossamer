// Package ownership models semantic object lifetimes independently from Go's
// physical heap. It is the shadow runtime used to prove Gossamer's task,
// queue, and realm ownership rules before a JavaScript engine is connected.
package ownership

import (
	"errors"
	"fmt"
	"time"
)

type ObjectID uint64
type RegionID uint64

// OwnerKind orders Gossamer's lifetime domains from shortest to longest.
type OwnerKind uint8

const (
	OwnerTask OwnerKind = iota
	OwnerQueue
	OwnerRealm
	OwnerBrowser
)

// OwnerID identifies one task, queue, realm, or browser owner.
type OwnerID struct {
	Kind  OwnerKind
	Value uint64
}

func (owner OwnerID) String() string {
	var kind string
	switch owner.Kind {
	case OwnerTask:
		kind = "task"
	case OwnerQueue:
		kind = "queue"
	case OwnerRealm:
		kind = "realm"
	case OwnerBrowser:
		kind = "browser"
	default:
		kind = fmt.Sprintf("owner(%d)", owner.Kind)
	}
	return fmt.Sprintf("%s:%d", kind, owner.Value)
}

// EventKind identifies one state transition in the ownership ledger.
type EventKind uint8

const (
	RegionCreated EventKind = iota
	ObjectCreated
	ObjectPublished
	ObjectTransferred
	ObjectReleased
	ObjectDestroyed
	RegionReleased
	ObjectLinked
	ObjectUnlinked
	ObjectBarrierRetained
)

// Event is an immutable telemetry record ordered by Sequence.
type Event struct {
	Sequence uint64
	At       time.Time
	Kind     EventKind

	Object ObjectID
	Target ObjectID
	Region RegionID
	Owner  OwnerID
	From   OwnerID
	To     OwnerID

	References int
}

// Stats summarizes semantic lifetime activity since the ledger was created.
type Stats struct {
	TaskLocalAllocations int
	ObjectsCreated       int
	ObjectsDestroyed     int
	LiveObjects          int
	BulkRegionReleases   int
	PublishOperations    int
	TransferOperations   int
	RetainOperations     int
	ReleaseOperations    int
	LocalReferences      int
	BarrierRetains       int
	PersistentObjects    int
}

// ObjectSnapshot describes the current or final state of one shadow object.
type ObjectSnapshot struct {
	ID         ObjectID
	Region     RegionID
	Owners     map[OwnerID]int
	Claims     []RegionID
	Edges      []ObjectID
	References int
	Alive      bool
}

// RegionSnapshot describes one logical allocation region.
type RegionSnapshot struct {
	ID      RegionID
	Owner   OwnerID
	Objects []ObjectID
	Claims  []ObjectID
	Closed  bool
}

var (
	ErrInvalidOwner       = errors.New("ownership: invalid owner")
	ErrOwnerRegistered    = errors.New("ownership: owner already has a region")
	ErrOwnerNotRegistered = errors.New("ownership: owner has no active region")
	ErrUnknownRegion      = errors.New("ownership: unknown region")
	ErrRegionClosed       = errors.New("ownership: region is closed")
	ErrUnknownObject      = errors.New("ownership: unknown object")
	ErrObjectDestroyed    = errors.New("ownership: object is destroyed")
	ErrNotOwned           = errors.New("ownership: owner does not retain object")
)
