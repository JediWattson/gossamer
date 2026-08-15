package memory

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

var nextSharedOwner atomic.Uint64

var (
	ErrStoreClosed          = errors.New("memory: store is closed")
	ErrUnknownRegion        = errors.New("memory: unknown region")
	ErrRegionDestroyed      = errors.New("memory: region is destroyed")
	ErrRegionInTransit      = errors.New("memory: region is in transit")
	ErrImmutableRegion      = errors.New("memory: published region is immutable")
	ErrAccessDenied         = errors.New("memory: private region access denied")
	ErrStaleRef             = errors.New("memory: stale reference")
	ErrTypeMismatch         = errors.New("memory: heap object type mismatch")
	ErrInvalidField         = errors.New("memory: invalid field")
	ErrInvalidIndex         = errors.New("memory: invalid array index")
	ErrBindingExists        = errors.New("memory: binding already exists")
	ErrBindingNotFound      = errors.New("memory: binding not found")
	ErrBindingUninitialized = errors.New("memory: binding is uninitialized")
	ErrImmutableBinding     = errors.New("memory: binding is immutable")
	ErrContextCycle         = errors.New("memory: context parent cycle")
	ErrInvalidFunction      = errors.New("memory: invalid function descriptor")
	ErrObjectReferenced     = errors.New("memory: heap object still has incoming references")
	ErrCellReferenced       = ErrObjectReferenced
	ErrRegionReferenced     = errors.New("memory: region still has incoming references")
	ErrExplicitSendRequired = errors.New("memory: private refs require Transfer, Publish, or Copy")
	ErrInvalidTransfer      = errors.New("memory: transfer destination is not a queue")
	ErrOwnerMismatch        = errors.New("memory: ownership claim does not match")
)

// Stats describes physical heap activity, independently from ledger telemetry.
type Stats struct {
	Allocations        uint64
	Frees              uint64
	LiveSlots          uint64
	LiveCells          uint64
	LiveStrings        uint64
	LiveObjects        uint64
	LiveArrays         uint64
	LiveContexts       uint64
	LiveFunctions      uint64
	LiveBytes          uint64
	LiveRegions        uint64
	BulkRegionReleases uint64
}

type objectEdge struct {
	from ownership.ObjectID
	to   ownership.ObjectID
}

func typeError(ref Ref, got, want HeapKind) error {
	return fmt.Errorf("%w: %s contains %s, want %s", ErrTypeMismatch, ref, got, want)
}

func (kind HeapKind) String() string {
	switch kind {
	case HeapCell:
		return "Cell"
	case HeapString:
		return "String"
	case HeapObject:
		return "Object"
	case HeapArray:
		return "Array"
	case HeapContext:
		return "Context"
	case HeapFunction:
		return "Function"
	default:
		return fmt.Sprintf("HeapKind(%d)", kind)
	}
}

// Store is a concurrency-safe native heap whose Cells are mirrored by the
// Phase-0 ownership ledger.
type Store struct {
	mutex sync.Mutex

	ledger *ownership.Ledger
	closed bool

	nextRegion   RegionID
	regions      map[RegionID]*Region
	ownerClaims  map[ownership.OwnerID]ownership.RegionID
	closedOwners map[ownership.OwnerID]bool
	barrier      *Barrier
	objectEdges  map[objectEdge]uint32
	stats        Stats

	sharedOwner ownership.OwnerID
	sharedClaim ownership.RegionID
}

func NewStore(ledger *ownership.Ledger) *Store {
	if ledger == nil {
		ledger = ownership.NewLedger()
	}
	return &Store{
		ledger:       ledger,
		regions:      make(map[RegionID]*Region),
		ownerClaims:  make(map[ownership.OwnerID]ownership.RegionID),
		closedOwners: make(map[ownership.OwnerID]bool),
		barrier:      newBarrier(),
		objectEdges:  make(map[objectEdge]uint32),
	}
}

func (store *Store) Ledger() *ownership.Ledger {
	if store == nil {
		return nil
	}
	return store.ledger
}

// RegisterOwner binds a runtime owner that already exists in the ledger, or
// creates its logical claim region when the Store is used independently.
func (store *Store) RegisterOwner(owner ownership.OwnerID) error {
	if store == nil {
		return fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	_, err := store.ensureOwnerLocked(owner)
	return err
}

func (store *Store) NewRegion(owner ownership.OwnerID) (RegionID, error) {
	if store == nil {
		return 0, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if store.closed {
		return 0, ErrStoreClosed
	}
	claim, err := store.ensureOwnerLocked(owner)
	if err != nil {
		return 0, err
	}
	if store.nextRegion == RegionID(math.MaxUint64) {
		return 0, fmt.Errorf("memory: exhausted region IDs")
	}
	store.nextRegion++
	region := &Region{
		ID:    store.nextRegion,
		Owner: owner,
		State: stateForOwner(owner),
		claim: claim,
	}
	store.regions[region.ID] = region
	store.stats.LiveRegions++
	return region.ID, nil
}

func stateForOwner(owner ownership.OwnerID) RegionState {
	if owner.Kind == ownership.OwnerQueue {
		return RegionInTransit
	}
	if owner.Kind == ownership.OwnerShared {
		return RegionPublished
	}
	return RegionPrivate
}

func (store *Store) Region(id RegionID) (Region, error) {
	if store == nil {
		return Region{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	region := store.regions[id]
	if region == nil {
		return Region{}, fmt.Errorf("%w: R%d", ErrUnknownRegion, id)
	}
	return cloneRegion(region), nil
}

func (store *Store) Alloc(owner ownership.OwnerID, regionID RegionID) (Ref, error) {
	return store.AllocCell(owner, regionID)
}

// AllocCell allocates the synthetic field container used by the ownership
// tests. Alloc remains its compatibility alias.
func (store *Store) AllocCell(owner ownership.OwnerID, regionID RegionID) (Ref, error) {
	if store == nil {
		return Ref{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return store.allocKindLocked(owner, regionID, HeapCell, false)
}

func (store *Store) allocLocked(owner ownership.OwnerID, regionID RegionID, internal bool) (Ref, error) {
	return store.allocKindLocked(owner, regionID, HeapCell, internal)
}

func (store *Store) allocKindLocked(owner ownership.OwnerID, regionID RegionID, kind HeapKind, internal bool) (Ref, error) {
	if kind < HeapCell || kind > HeapFunction {
		return Ref{}, fmt.Errorf("%w: heap kind %d", ErrTypeMismatch, kind)
	}
	region, err := store.mutableRegionLocked(owner, regionID, internal)
	if err != nil {
		return Ref{}, err
	}
	object, err := store.ledger.CreateObject(region.claim)
	if err != nil {
		return Ref{}, err
	}
	var index uint32
	if len(region.free) != 0 {
		index = region.free[len(region.free)-1]
		region.free = region.free[:len(region.free)-1]
		slot := &region.Slots[index]
		slot.Occupied = true
		slot.object = object
		initializeSlotPayload(slot, kind)
	} else {
		if uint64(len(region.Slots)) > math.MaxUint32 {
			_ = store.ledger.Release(object, owner)
			return Ref{}, fmt.Errorf("memory: region R%d exhausted slots", regionID)
		}
		index = uint32(len(region.Slots))
		region.Slots = append(region.Slots, Slot{Generation: 1, Kind: kind, Occupied: true, object: object})
		initializeSlotPayload(&region.Slots[index], kind)
	}
	store.stats.Allocations++
	store.stats.LiveSlots++
	store.recordKindAllocationLocked(kind, 0)
	return Ref{Region: regionID, Slot: index, Gen: region.Slots[index].Generation}, nil
}

// AllocString clones text into an immutable String slot owned by regionID.
func (store *Store) AllocString(owner ownership.OwnerID, regionID RegionID, text string) (Ref, error) {
	if store == nil {
		return Ref{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	ref, err := store.allocKindLocked(owner, regionID, HeapString, false)
	if err != nil {
		return Ref{}, err
	}
	_, slot, _ := store.slotLocked(ref)
	slot.String = cloneString(StringObject{Text: text})
	store.stats.LiveBytes += uint64(len(slot.String.Text))
	return ref, nil
}

func (store *Store) Deref(owner ownership.OwnerID, ref Ref) (Cell, error) {
	return store.DerefCell(owner, ref)
}

func (store *Store) DerefCell(owner ownership.OwnerID, ref Ref) (Cell, error) {
	if store == nil {
		return Cell{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	_, slot, err := store.readSlotLocked(owner, ref)
	if err != nil {
		return Cell{}, err
	}
	if slot.Kind != HeapCell {
		return Cell{}, typeError(ref, slot.Kind, HeapCell)
	}
	return cloneCell(slot.Cell), nil
}

// DerefString returns the immutable native string payload for ref.
func (store *Store) DerefString(owner ownership.OwnerID, ref Ref) (string, error) {
	if store == nil {
		return "", fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	_, slot, err := store.readSlotLocked(owner, ref)
	if err != nil {
		return "", err
	}
	if slot.Kind != HeapString {
		return "", typeError(ref, slot.Kind, HeapString)
	}
	return slot.String.Text, nil
}

func (store *Store) Get(owner ownership.OwnerID, ref Ref, field int) (Value, error) {
	cell, err := store.Deref(owner, ref)
	if err != nil {
		return Value{}, err
	}
	if field < 0 || field >= len(cell.Fields) {
		return Value{}, fmt.Errorf("%w: %d", ErrInvalidField, field)
	}
	return cell.Fields[field], nil
}

func (store *Store) Set(owner ownership.OwnerID, object Ref, field int, value Value) error {
	if store == nil {
		return fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return store.setLocked(owner, object, field, value, false)
}

func (store *Store) setLocked(owner ownership.OwnerID, object Ref, field int, value Value, internal bool) error {
	if field < 0 {
		return fmt.Errorf("%w: %d", ErrInvalidField, field)
	}
	region, slot, err := store.writeSlotLocked(owner, object, internal)
	if err != nil {
		return err
	}
	if slot.Kind != HeapCell {
		return typeError(object, slot.Kind, HeapCell)
	}
	old := Value{}
	if field < len(slot.Cell.Fields) {
		old = slot.Cell.Fields[field]
	}
	if old == value {
		if field >= len(slot.Cell.Fields) {
			slot.Cell.Fields = append(slot.Cell.Fields, make([]Value, field-len(slot.Cell.Fields)+1)...)
		}
		return nil
	}
	if err := store.replaceValueLocked(owner, region, slot, old, value, internal); err != nil {
		return err
	}
	if field >= len(slot.Cell.Fields) {
		slot.Cell.Fields = append(slot.Cell.Fields, make([]Value, field-len(slot.Cell.Fields)+1)...)
	}
	slot.Cell.Fields[field] = value
	return nil
}

func (store *Store) replaceValueLocked(owner ownership.OwnerID, region *Region, slot *Slot, old, value Value, internal bool) error {
	if old == value {
		return nil
	}
	var targetRegion *Region
	var targetSlot *Slot
	var err error
	if value.IsRef() {
		targetRegion, targetSlot, err = store.readSlotLocked(owner, value.Ref())
		if err != nil && internal {
			targetRegion, targetSlot, err = store.slotLocked(value.Ref())
		}
		if err != nil {
			return err
		}
		if targetRegion.State == RegionPrivate && targetRegion.Owner != region.Owner {
			return fmt.Errorf("%w: R%d owned by %s cannot reference R%d owned by %s", ErrAccessDenied, region.ID, region.Owner, targetRegion.ID, targetRegion.Owner)
		}
		if err := store.linkLocked(region.ID, slot.object, targetRegion.ID, targetSlot.object); err != nil {
			return err
		}
	}
	if old.IsRef() {
		oldRegion, oldSlot, oldErr := store.slotLocked(old.Ref())
		if oldErr != nil {
			if value.IsRef() {
				_ = store.unlinkLocked(region.ID, slot.object, targetRegion.ID, targetSlot.object)
			}
			return oldErr
		}
		if err := store.unlinkLocked(region.ID, slot.object, oldRegion.ID, oldSlot.object); err != nil {
			if value.IsRef() {
				_ = store.unlinkLocked(region.ID, slot.object, targetRegion.ID, targetSlot.object)
			}
			return err
		}
	}
	return nil
}

func (store *Store) Free(owner ownership.OwnerID, ref Ref) error {
	if store == nil {
		return fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return store.freeLocked(owner, ref, false)
}

func (store *Store) freeLocked(owner ownership.OwnerID, ref Ref, internal bool) error {
	region, slot, err := store.writeSlotLocked(owner, ref, internal)
	if err != nil {
		return err
	}
	for edge, count := range store.objectEdges {
		if count != 0 && edge.to == slot.object && edge.from != slot.object {
			return fmt.Errorf("%w: %s", ErrCellReferenced, ref)
		}
	}
	if err := store.unlinkSlotLocked(region, slot); err != nil {
		return err
	}
	if err := store.ledger.Release(slot.object, region.Owner); err != nil {
		return err
	}
	store.recordKindFreeLocked(slot)
	clearSlotPayload(slot)
	slot.object = 0
	slot.Occupied = false
	if slot.Generation != math.MaxUint32 {
		slot.Generation++
		region.free = append(region.free, ref.Slot)
	}
	store.stats.Frees++
	store.stats.LiveSlots--
	return nil
}

func (store *Store) Edges() []RegionEdge {
	if store == nil {
		return nil
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return store.barrier.snapshot()
}

func (store *Store) EdgeCount(from, to RegionID) uint32 {
	if store == nil {
		return 0
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return store.barrier.count(from, to)
}

func (store *Store) DestroyRegion(owner ownership.OwnerID, regionID RegionID) error {
	if store == nil {
		return fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	region := store.regions[regionID]
	if region == nil {
		return fmt.Errorf("%w: R%d", ErrUnknownRegion, regionID)
	}
	if region.State == RegionDestroyed {
		return nil
	}
	if region.State == RegionPublished {
		return fmt.Errorf("%w: R%d", ErrImmutableRegion, regionID)
	}
	if region.State == RegionInTransit {
		return fmt.Errorf("%w: R%d", ErrRegionInTransit, regionID)
	}
	if region.Owner != owner {
		return store.accessError(region, owner)
	}
	return store.destroyRegionsLocked(map[RegionID]struct{}{regionID: {}})
}

// ReleaseOwner performs the task/queue/realm bulk-release boundary. It first
// proves no region outside the release set still points in, then tears down all
// physical cells and finally closes the owner's logical ledger region.
func (store *Store) ReleaseOwner(owner ownership.OwnerID) error {
	if store == nil {
		return fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if store.closedOwners[owner] {
		return nil
	}
	ids := make(map[RegionID]struct{})
	for id, region := range store.regions {
		if region.State != RegionDestroyed && region.State != RegionPublished && region.Owner == owner {
			ids[id] = struct{}{}
		}
	}
	if err := store.destroyRegionsLocked(ids); err != nil {
		return err
	}
	claim := store.ownerClaims[owner]
	if claim != 0 {
		if err := store.ledger.CloseRegion(claim); err != nil {
			return err
		}
	}
	store.closedOwners[owner] = true
	return nil
}

// ValidateSend permits an unqualified queue send only for already-published
// refs. A private ref must cross with an explicit operation.
func (store *Store) ValidateSend(from ownership.OwnerID, refs ...Ref) error {
	if store == nil {
		return fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	for _, ref := range uniqueRefs(refs) {
		region, _, err := store.slotLocked(ref)
		if err != nil {
			return err
		}
		if region.State == RegionPublished {
			continue
		}
		if region.State == RegionInTransit {
			return fmt.Errorf("%w: R%d", ErrRegionInTransit, region.ID)
		}
		if region.Owner != from {
			return store.accessError(region, from)
		}
		return fmt.Errorf("%w: private region R%d owned by %s", ErrExplicitSendRequired, region.ID, region.Owner)
	}
	return nil
}

// Transfer moves the complete connected private region component to a queue.
func (store *Store) Transfer(from, queue ownership.OwnerID, refs ...Ref) error {
	if store == nil {
		return fmt.Errorf("memory: nil store")
	}
	if queue.Kind != ownership.OwnerQueue {
		return fmt.Errorf("%w: %s", ErrInvalidTransfer, queue)
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	ids, err := store.privateComponentLocked(from, refs, true)
	if err != nil {
		return err
	}
	return store.moveRegionsLocked(ids, from, queue, RegionInTransit)
}

// Accept transfers queued regions to the receiving task or realm owner.
func (store *Store) Accept(queue, to ownership.OwnerID, refs ...Ref) error {
	if store == nil {
		return fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	ids := make(map[RegionID]struct{})
	for _, ref := range uniqueRefs(refs) {
		region, _, err := store.slotLocked(ref)
		if err != nil {
			return err
		}
		if region.State == RegionPublished {
			continue
		}
		if region.State != RegionInTransit || region.Owner != queue {
			return fmt.Errorf("%w: R%d is not held by %s", ErrAccessDenied, region.ID, queue)
		}
		ids[region.ID] = struct{}{}
	}
	ids = store.connectedRegionsLocked(ids, func(region *Region) bool {
		return region.State == RegionInTransit && region.Owner == queue
	})
	return store.moveRegionsLocked(ids, queue, to, RegionPrivate)
}

// Publish makes the root regions and their private outgoing dependencies
// immutable and globally readable. Publication never happens implicitly.
func (store *Store) Publish(from ownership.OwnerID, refs ...Ref) error {
	if store == nil {
		return fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	ids, err := store.privateComponentLocked(from, refs, false)
	if err != nil {
		return err
	}
	shared, err := store.ensureSharedLocked()
	if err != nil {
		return err
	}
	return store.moveRegionsLocked(ids, from, shared, RegionPublished)
}

// Copy clones the complete Cell graph reachable from roots into one new
// private region owned by to. Queue-owned copies begin in transit.
func (store *Store) Copy(from, to ownership.OwnerID, roots ...Ref) ([]Ref, error) {
	if store == nil {
		return nil, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return store.copyLocked(from, to, append([]Ref(nil), roots...))
}

// Promote explicitly copies the complete Cell graph reachable from roots into
// a new immutable shared region. The original refs remain private and are
// released with their current owner; callers must use the returned refs for
// the promoted graph.
func (store *Store) Promote(from ownership.OwnerID, roots ...Ref) ([]Ref, error) {
	if store == nil {
		return nil, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	shared, err := store.ensureSharedLocked()
	if err != nil {
		return nil, err
	}
	return store.copyLocked(from, shared, append([]Ref(nil), roots...))
}

func (store *Store) Stats() Stats {
	if store == nil {
		return Stats{}
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return store.stats
}

func (store *Store) recordKindAllocationLocked(kind HeapKind, bytes uint64) {
	switch kind {
	case HeapCell:
		store.stats.LiveCells++
	case HeapString:
		store.stats.LiveStrings++
	case HeapObject:
		store.stats.LiveObjects++
	case HeapArray:
		store.stats.LiveArrays++
	case HeapContext:
		store.stats.LiveContexts++
	case HeapFunction:
		store.stats.LiveFunctions++
	}
	store.stats.LiveBytes += bytes
}

func (store *Store) recordKindFreeLocked(slot *Slot) {
	if slot == nil || !slot.Occupied {
		return
	}
	switch slot.Kind {
	case HeapCell:
		store.stats.LiveCells--
	case HeapString:
		store.stats.LiveStrings--
		store.stats.LiveBytes -= uint64(len(slot.String.Text))
	case HeapObject:
		store.stats.LiveObjects--
	case HeapArray:
		store.stats.LiveArrays--
	case HeapContext:
		store.stats.LiveContexts--
	case HeapFunction:
		store.stats.LiveFunctions--
		store.stats.LiveBytes -= uint64(len(slot.Function.Code))
	}
}

func (store *Store) Close() error {
	if store == nil {
		return nil
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if store.closed {
		return nil
	}
	ids := make(map[RegionID]struct{})
	for id, region := range store.regions {
		if region.State != RegionDestroyed {
			ids[id] = struct{}{}
		}
	}
	if err := store.destroyRegionsLocked(ids); err != nil {
		return err
	}
	owners := make([]ownership.OwnerID, 0, len(store.ownerClaims))
	for owner := range store.ownerClaims {
		owners = append(owners, owner)
	}
	sort.Slice(owners, func(i, j int) bool {
		if owners[i].Kind != owners[j].Kind {
			return owners[i].Kind < owners[j].Kind
		}
		return owners[i].Value < owners[j].Value
	})
	for _, owner := range owners {
		if store.closedOwners[owner] {
			continue
		}
		if err := store.ledger.CloseRegion(store.ownerClaims[owner]); err != nil {
			return err
		}
		store.closedOwners[owner] = true
	}
	store.closed = true
	return nil
}

func (store *Store) ensureOwnerLocked(owner ownership.OwnerID) (ownership.RegionID, error) {
	if store.closed {
		return 0, ErrStoreClosed
	}
	if store.closedOwners[owner] {
		return 0, fmt.Errorf("%w: %s", ownership.ErrRegionClosed, owner)
	}
	if claim := store.ownerClaims[owner]; claim != 0 {
		snapshot, err := store.ledger.Region(claim)
		if err != nil {
			return 0, err
		}
		if snapshot.Closed {
			return 0, fmt.Errorf("%w: %d", ownership.ErrRegionClosed, claim)
		}
		return claim, nil
	}
	claim, err := store.ledger.OwnerRegion(owner)
	if errors.Is(err, ownership.ErrOwnerNotRegistered) {
		claim, err = store.ledger.CreateRegion(owner)
	}
	if err != nil {
		return 0, err
	}
	store.ownerClaims[owner] = claim
	return claim, nil
}

func (store *Store) ensureSharedLocked() (ownership.OwnerID, error) {
	if store.sharedOwner.Value != 0 {
		return store.sharedOwner, nil
	}
	value := nextSharedOwner.Add(1)
	if value == 0 {
		return ownership.OwnerID{}, fmt.Errorf("memory: exhausted shared owner IDs")
	}
	owner := ownership.OwnerID{Kind: ownership.OwnerShared, Value: value}
	claim, err := store.ensureOwnerLocked(owner)
	if err != nil {
		return ownership.OwnerID{}, err
	}
	store.sharedOwner = owner
	store.sharedClaim = claim
	return owner, nil
}

func (store *Store) slotLocked(ref Ref) (*Region, *Slot, error) {
	region := store.regions[ref.Region]
	if region == nil {
		return nil, nil, fmt.Errorf("%w: %s", ErrStaleRef, ref)
	}
	if region.State == RegionDestroyed {
		return nil, nil, fmt.Errorf("%w: %s: %w", ErrStaleRef, ref, ErrRegionDestroyed)
	}
	if uint64(ref.Slot) >= uint64(len(region.Slots)) {
		return nil, nil, fmt.Errorf("%w: %s", ErrStaleRef, ref)
	}
	slot := &region.Slots[ref.Slot]
	if !slot.Occupied || slot.Generation != ref.Gen {
		return nil, nil, fmt.Errorf("%w: %s", ErrStaleRef, ref)
	}
	return region, slot, nil
}

func (store *Store) readSlotLocked(owner ownership.OwnerID, ref Ref) (*Region, *Slot, error) {
	region, slot, err := store.slotLocked(ref)
	if err != nil {
		return nil, nil, err
	}
	switch region.State {
	case RegionPublished:
		return region, slot, nil
	case RegionInTransit:
		return nil, nil, fmt.Errorf("%w: R%d", ErrRegionInTransit, region.ID)
	case RegionPrivate:
		if region.Owner != owner {
			return nil, nil, store.accessError(region, owner)
		}
		return region, slot, nil
	default:
		return nil, nil, fmt.Errorf("%w: %s", ErrStaleRef, ref)
	}
}

func (store *Store) writeSlotLocked(owner ownership.OwnerID, ref Ref, internal bool) (*Region, *Slot, error) {
	region, slot, err := store.slotLocked(ref)
	if err != nil {
		return nil, nil, err
	}
	if region.State == RegionPublished && !internal {
		return nil, nil, fmt.Errorf("%w: R%d", ErrImmutableRegion, region.ID)
	}
	if region.State == RegionInTransit && !internal {
		return nil, nil, fmt.Errorf("%w: R%d", ErrRegionInTransit, region.ID)
	}
	if region.Owner != owner {
		return nil, nil, store.accessError(region, owner)
	}
	return region, slot, nil
}

func (store *Store) mutableRegionLocked(owner ownership.OwnerID, id RegionID, internal bool) (*Region, error) {
	region := store.regions[id]
	if region == nil {
		return nil, fmt.Errorf("%w: R%d", ErrUnknownRegion, id)
	}
	if region.State == RegionDestroyed {
		return nil, fmt.Errorf("%w: R%d", ErrRegionDestroyed, id)
	}
	if region.State == RegionPublished && !internal {
		return nil, fmt.Errorf("%w: R%d", ErrImmutableRegion, id)
	}
	if region.State == RegionInTransit && !internal {
		return nil, fmt.Errorf("%w: R%d", ErrRegionInTransit, id)
	}
	if region.Owner != owner {
		return nil, store.accessError(region, owner)
	}
	return region, nil
}

func (store *Store) accessError(region *Region, owner ownership.OwnerID) error {
	return fmt.Errorf("%w: private region R%d owned by %s, accessed by %s", ErrAccessDenied, region.ID, region.Owner, owner)
}

func (store *Store) linkLocked(fromRegion RegionID, fromObject ownership.ObjectID, toRegion RegionID, toObject ownership.ObjectID) error {
	edge := objectEdge{from: fromObject, to: toObject}
	if store.objectEdges[edge] == math.MaxUint32 {
		return fmt.Errorf("memory: object edge count overflow")
	}
	if err := store.barrier.link(fromRegion, toRegion); err != nil {
		return err
	}
	if store.objectEdges[edge] == 0 {
		if err := store.ledger.AddReference(fromObject, toObject); err != nil {
			_ = store.barrier.unlink(fromRegion, toRegion)
			return err
		}
	}
	store.objectEdges[edge]++
	return nil
}

func (store *Store) unlinkLocked(fromRegion RegionID, fromObject ownership.ObjectID, toRegion RegionID, toObject ownership.ObjectID) error {
	edge := objectEdge{from: fromObject, to: toObject}
	count := store.objectEdges[edge]
	if count == 0 {
		return fmt.Errorf("memory: missing object edge %d -> %d", fromObject, toObject)
	}
	if err := store.barrier.unlink(fromRegion, toRegion); err != nil {
		return err
	}
	if count == 1 {
		if err := store.ledger.RemoveReference(fromObject, toObject); err != nil {
			_ = store.barrier.link(fromRegion, toRegion)
			return err
		}
		delete(store.objectEdges, edge)
		return nil
	}
	store.objectEdges[edge] = count - 1
	return nil
}

func (store *Store) unlinkSlotLocked(region *Region, slot *Slot) error {
	for _, value := range slotReferences(slot) {
		if !value.IsRef() {
			continue
		}
		targetRegion, targetSlot, err := store.slotLocked(value.Ref())
		if err != nil {
			return err
		}
		if err := store.unlinkLocked(region.ID, slot.object, targetRegion.ID, targetSlot.object); err != nil {
			return err
		}
	}
	return nil
}

func (store *Store) destroyRegionsLocked(ids map[RegionID]struct{}) error {
	if len(ids) == 0 {
		return nil
	}
	for edge, count := range store.objectEdges {
		if count == 0 {
			continue
		}
		fromRegion := store.regionForObjectLocked(edge.from)
		toRegion := store.regionForObjectLocked(edge.to)
		if toRegion == 0 {
			continue
		}
		if _, target := ids[toRegion]; !target {
			continue
		}
		if _, source := ids[fromRegion]; !source {
			return fmt.Errorf("%w: R%d -> R%d (%d refs)", ErrRegionReferenced, fromRegion, toRegion, count)
		}
	}
	ordered := sortedRegionSet(ids)
	for _, id := range ordered {
		region := store.regions[id]
		for index := range region.Slots {
			slot := &region.Slots[index]
			if slot.Occupied {
				if err := store.unlinkSlotLocked(region, slot); err != nil {
					return err
				}
			}
		}
	}
	for _, id := range ordered {
		region := store.regions[id]
		for index := range region.Slots {
			slot := &region.Slots[index]
			if !slot.Occupied {
				continue
			}
			if err := store.ledger.Release(slot.object, region.Owner); err != nil {
				return err
			}
			store.recordKindFreeLocked(slot)
			clearSlotPayload(slot)
			slot.object = 0
			slot.Occupied = false
			if slot.Generation != math.MaxUint32 {
				slot.Generation++
			}
			store.stats.LiveSlots--
		}
		region.free = nil
		region.State = RegionDestroyed
		store.stats.LiveRegions--
		store.stats.BulkRegionReleases++
	}
	return nil
}

func (store *Store) regionForObjectLocked(object ownership.ObjectID) RegionID {
	for id, region := range store.regions {
		if region.State == RegionDestroyed {
			continue
		}
		for index := range region.Slots {
			if region.Slots[index].Occupied && region.Slots[index].object == object {
				return id
			}
		}
	}
	return 0
}

func (store *Store) privateComponentLocked(owner ownership.OwnerID, refs []Ref, includeIncoming bool) (map[RegionID]struct{}, error) {
	ids := make(map[RegionID]struct{})
	for _, ref := range uniqueRefs(refs) {
		region, _, err := store.slotLocked(ref)
		if err != nil {
			return nil, err
		}
		if region.State == RegionPublished {
			continue
		}
		if region.State == RegionInTransit {
			return nil, fmt.Errorf("%w: R%d", ErrRegionInTransit, region.ID)
		}
		if region.Owner != owner {
			return nil, store.accessError(region, owner)
		}
		ids[region.ID] = struct{}{}
	}
	match := func(region *Region) bool {
		return region.State == RegionPrivate && region.Owner == owner
	}
	if includeIncoming {
		return store.connectedRegionsLocked(ids, match), nil
	}
	return store.outgoingRegionsLocked(ids, match), nil
}

func (store *Store) connectedRegionsLocked(seed map[RegionID]struct{}, match func(*Region) bool) map[RegionID]struct{} {
	result := cloneRegionSet(seed)
	changed := true
	for changed {
		changed = false
		for key := range store.barrier.edges {
			_, from := result[key.from]
			_, to := result[key.to]
			if from == to {
				continue
			}
			candidate := key.from
			if from {
				candidate = key.to
			}
			region := store.regions[candidate]
			if region != nil && match(region) {
				result[candidate] = struct{}{}
				changed = true
			}
		}
	}
	return result
}

func (store *Store) outgoingRegionsLocked(seed map[RegionID]struct{}, match func(*Region) bool) map[RegionID]struct{} {
	result := cloneRegionSet(seed)
	changed := true
	for changed {
		changed = false
		for key := range store.barrier.edges {
			if _, exists := result[key.from]; !exists {
				continue
			}
			if _, exists := result[key.to]; exists {
				continue
			}
			region := store.regions[key.to]
			if region != nil && match(region) {
				result[key.to] = struct{}{}
				changed = true
			}
		}
	}
	return result
}

func (store *Store) moveRegionsLocked(ids map[RegionID]struct{}, from, to ownership.OwnerID, state RegionState) error {
	if len(ids) == 0 {
		return nil
	}
	claim, err := store.ensureOwnerLocked(to)
	if err != nil {
		return err
	}
	ordered := sortedRegionSet(ids)
	moved := make([]*Region, 0, len(ordered))
	for _, id := range ordered {
		region := store.regions[id]
		if region == nil || region.State == RegionDestroyed || region.Owner != from {
			return fmt.Errorf("%w: R%d is not owned by %s", ErrOwnerMismatch, id, from)
		}
		objects := store.regionObjectsLocked(region)
		if err := store.ledger.TransferSet(objects, from, to); err != nil {
			for index := len(moved) - 1; index >= 0; index-- {
				rollback := moved[index]
				_ = store.ledger.TransferSet(store.regionObjectsLocked(rollback), to, from)
			}
			return err
		}
		moved = append(moved, region)
	}
	for _, region := range moved {
		region.Owner = to
		region.State = state
		region.claim = claim
	}
	return nil
}

func (store *Store) regionObjectsLocked(region *Region) []ownership.ObjectID {
	objects := make([]ownership.ObjectID, 0, len(region.Slots))
	for index := range region.Slots {
		if region.Slots[index].Occupied {
			objects = append(objects, region.Slots[index].object)
		}
	}
	return objects
}

func (store *Store) copyLocked(from, to ownership.OwnerID, roots []Ref) ([]Ref, error) {
	if len(roots) == 0 {
		return nil, nil
	}
	queue := append([]Ref(nil), roots...)
	seen := make(map[Ref]struct{})
	order := make([]Ref, 0)
	for len(queue) != 0 {
		ref := queue[0]
		queue = queue[1:]
		if _, exists := seen[ref]; exists {
			continue
		}
		region, slot, err := store.slotLocked(ref)
		if err != nil {
			return nil, err
		}
		if region.State == RegionInTransit {
			return nil, fmt.Errorf("%w: R%d", ErrRegionInTransit, region.ID)
		}
		if region.State == RegionPrivate && region.Owner != from {
			return nil, store.accessError(region, from)
		}
		seen[ref] = struct{}{}
		order = append(order, ref)
		for _, value := range slotReferences(slot) {
			if value.IsRef() {
				queue = append(queue, value.Ref())
			}
		}
	}
	claim, err := store.ensureOwnerLocked(to)
	if err != nil {
		return nil, err
	}
	if store.nextRegion == RegionID(math.MaxUint64) {
		return nil, fmt.Errorf("memory: exhausted region IDs")
	}
	store.nextRegion++
	destination := &Region{ID: store.nextRegion, Owner: to, State: stateForOwner(to), claim: claim}
	store.regions[destination.ID] = destination
	store.stats.LiveRegions++
	mapping := make(map[Ref]Ref, len(order))
	for _, source := range order {
		_, sourceSlot, _ := store.slotLocked(source)
		copyRef, allocErr := store.allocKindLocked(to, destination.ID, sourceSlot.Kind, true)
		if allocErr != nil {
			_ = store.destroyRegionsLocked(map[RegionID]struct{}{destination.ID: {}})
			return nil, allocErr
		}
		if sourceSlot.Kind == HeapString {
			_, copySlot, _ := store.slotLocked(copyRef)
			copySlot.String = cloneString(sourceSlot.String)
			store.stats.LiveBytes += uint64(len(copySlot.String.Text))
		}
		mapping[source] = copyRef
	}
	for _, source := range order {
		_, sourceSlot, _ := store.slotLocked(source)
		copyRef := mapping[source]
		switch sourceSlot.Kind {
		case HeapCell:
			for field, value := range sourceSlot.Cell.Fields {
				if value.IsRef() {
					value = RefValue(mapping[value.Ref()])
				}
				if err := store.setLocked(to, copyRef, field, value, true); err != nil {
					_ = store.destroyRegionsLocked(map[RegionID]struct{}{destination.ID: {}})
					return nil, err
				}
			}
		case HeapString:
			// Immutable payload was cloned during allocation.
		case HeapObject:
			prototype := remapValue(sourceSlot.Object.Prototype, mapping)
			if err := store.setPrototypeLocked(to, copyRef, prototype, true); err != nil {
				_ = store.destroyRegionsLocked(map[RegionID]struct{}{destination.ID: {}})
				return nil, err
			}
			for _, property := range sourceSlot.Object.Properties {
				name := mapping[property.Name]
				value := remapValue(property.Value, mapping)
				if err := store.setPropertyLocked(to, copyRef, name, value, true); err != nil {
					_ = store.destroyRegionsLocked(map[RegionID]struct{}{destination.ID: {}})
					return nil, err
				}
			}
		case HeapArray:
			if err := store.setArrayLengthLocked(to, copyRef, sourceSlot.Array.Length, true); err != nil {
				_ = store.destroyRegionsLocked(map[RegionID]struct{}{destination.ID: {}})
				return nil, err
			}
			for _, element := range sourceSlot.Array.Elements {
				if err := store.setArrayElementLocked(to, copyRef, element.Index, remapValue(element.Value, mapping), true); err != nil {
					_ = store.destroyRegionsLocked(map[RegionID]struct{}{destination.ID: {}})
					return nil, err
				}
			}
		case HeapContext:
			if err := store.setContextParentLocked(to, copyRef, remapValue(sourceSlot.Context.Parent, mapping), true); err != nil {
				_ = store.destroyRegionsLocked(map[RegionID]struct{}{destination.ID: {}})
				return nil, err
			}
			for _, binding := range sourceSlot.Context.Bindings {
				name := mapping[binding.Name]
				if err := store.declareBindingLocked(to, copyRef, name, binding.Mutable, true); err != nil {
					_ = store.destroyRegionsLocked(map[RegionID]struct{}{destination.ID: {}})
					return nil, err
				}
				if binding.Initialized {
					if err := store.initializeBindingLocked(to, copyRef, name, remapValue(binding.Value, mapping), true); err != nil {
						_ = store.destroyRegionsLocked(map[RegionID]struct{}{destination.ID: {}})
						return nil, err
					}
				}
			}
		case HeapFunction:
			function := cloneFunction(sourceSlot.Function)
			function.Name = remapValue(function.Name, mapping)
			function.Environment = remapValue(function.Environment, mapping)
			for index, constant := range function.Constants {
				function.Constants[index] = remapValue(constant, mapping)
			}
			if err := store.initializeFunctionLocked(to, copyRef, function, true); err != nil {
				_ = store.destroyRegionsLocked(map[RegionID]struct{}{destination.ID: {}})
				return nil, err
			}
		default:
			_ = store.destroyRegionsLocked(map[RegionID]struct{}{destination.ID: {}})
			return nil, fmt.Errorf("%w: cannot copy heap kind %d", ErrTypeMismatch, sourceSlot.Kind)
		}
	}
	result := make([]Ref, len(roots))
	for index, root := range roots {
		result[index] = mapping[root]
	}
	return result, nil
}

func remapValue(value Value, mapping map[Ref]Ref) Value {
	if value.IsRef() {
		return RefValue(mapping[value.Ref()])
	}
	return value
}

func uniqueRefs(refs []Ref) []Ref {
	seen := make(map[Ref]struct{}, len(refs))
	result := make([]Ref, 0, len(refs))
	for _, ref := range refs {
		if _, exists := seen[ref]; exists {
			continue
		}
		seen[ref] = struct{}{}
		result = append(result, ref)
	}
	return result
}

func cloneRegionSet(source map[RegionID]struct{}) map[RegionID]struct{} {
	result := make(map[RegionID]struct{}, len(source))
	for id := range source {
		result[id] = struct{}{}
	}
	return result
}

func sortedRegionSet(regions map[RegionID]struct{}) []RegionID {
	result := make([]RegionID, 0, len(regions))
	for id := range regions {
		result = append(result, id)
	}
	sort.Slice(result, func(left, right int) bool { return result[left] < result[right] })
	return result
}
