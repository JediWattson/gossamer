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
var nextSymbolID atomic.Uint64

var (
	ErrStoreClosed           = errors.New("memory: store is closed")
	ErrUnknownRegion         = errors.New("memory: unknown region")
	ErrRegionDestroyed       = errors.New("memory: region is destroyed")
	ErrRegionInTransit       = errors.New("memory: region is in transit")
	ErrImmutableRegion       = errors.New("memory: published region is immutable")
	ErrAccessDenied          = errors.New("memory: private region access denied")
	ErrStaleRef              = errors.New("memory: stale reference")
	ErrTypeMismatch          = errors.New("memory: heap object type mismatch")
	ErrInvalidField          = errors.New("memory: invalid field")
	ErrInvalidIndex          = errors.New("memory: invalid array index")
	ErrBindingExists         = errors.New("memory: binding already exists")
	ErrBindingNotFound       = errors.New("memory: binding not found")
	ErrBindingUninitialized  = errors.New("memory: binding is uninitialized")
	ErrImmutableBinding      = errors.New("memory: binding is immutable")
	ErrBindingCycle          = errors.New("memory: indirect binding cycle")
	ErrContextCycle          = errors.New("memory: context parent cycle")
	ErrPrototypeCycle        = errors.New("memory: object prototype cycle")
	ErrReadOnlyProperty      = errors.New("memory: property is not writable")
	ErrNonConfigurable       = errors.New("memory: property is not configurable")
	ErrAccessorProperty      = errors.New("memory: accessor requires language execution")
	ErrInvalidFunction       = errors.New("memory: invalid function descriptor")
	ErrPromiseSettled        = errors.New("memory: promise is already settled")
	ErrPromisePending        = errors.New("memory: promise is still pending")
	ErrPromiseSelfResolution = errors.New("memory: promise cannot resolve to itself")
	ErrInvalidBigInt         = errors.New("memory: invalid BigInt")
	ErrInvalidSymbol         = errors.New("memory: invalid Symbol")
	ErrDetachedBuffer        = errors.New("memory: ArrayBuffer is detached")
	ErrBufferBounds          = errors.New("memory: buffer access is out of bounds")
	ErrInvalidTypedArray     = errors.New("memory: invalid TypedArray view")
	ErrInvalidRegExp         = errors.New("memory: invalid RegExp")
	ErrInvalidError          = errors.New("memory: invalid Error")
	ErrObjectReferenced      = errors.New("memory: heap object still has incoming references")
	ErrCellReferenced        = ErrObjectReferenced
	ErrRegionReferenced      = errors.New("memory: region still has incoming references")
	ErrExplicitSendRequired  = errors.New("memory: private refs require Transfer, Publish, or Copy")
	ErrInvalidTransfer       = errors.New("memory: transfer destination is not a queue")
	ErrDuplicateTransfer     = errors.New("memory: duplicate transferable")
	ErrOwnerMismatch         = errors.New("memory: ownership claim does not match")
)

// Stats describes physical heap activity, independently from ledger telemetry.
type Stats struct {
	Allocations              uint64 `json:"allocations"`
	Frees                    uint64 `json:"frees"`
	LiveSlots                uint64 `json:"liveSlots"`
	LiveCells                uint64 `json:"liveCells"`
	LiveStrings              uint64 `json:"liveStrings"`
	LiveObjects              uint64 `json:"liveObjects"`
	LiveArrays               uint64 `json:"liveArrays"`
	LiveContexts             uint64 `json:"liveContexts"`
	LiveFunctions            uint64 `json:"liveFunctions"`
	LivePromises             uint64 `json:"livePromises"`
	LiveBigInts              uint64 `json:"liveBigInts"`
	LiveSymbols              uint64 `json:"liveSymbols"`
	LiveArrayBuffers         uint64 `json:"liveArrayBuffers"`
	LiveTypedArrays          uint64 `json:"liveTypedArrays"`
	LiveMaps                 uint64 `json:"liveMaps"`
	LiveSets                 uint64 `json:"liveSets"`
	LiveDates                uint64 `json:"liveDates"`
	LiveRegExps              uint64 `json:"liveRegExps"`
	LiveErrors               uint64 `json:"liveErrors"`
	LiveWeakMaps             uint64 `json:"liveWeakMaps"`
	LiveWeakSets             uint64 `json:"liveWeakSets"`
	LiveIterators            uint64 `json:"liveIterators"`
	LiveHostObjects          uint64 `json:"liveHostObjects"`
	LiveBytes                uint64 `json:"liveBytes"`
	LiveRegions              uint64 `json:"liveRegions"`
	BulkRegionReleases       uint64 `json:"bulkRegionReleases"`
	AutomaticPromotions      uint64 `json:"automaticPromotions"`
	PromotionCacheHits       uint64 `json:"promotionCacheHits"`
	Collections              uint64 `json:"collections"`
	CollectedSlots           uint64 `json:"collectedSlots"`
	CollectedBytes           uint64 `json:"collectedBytes"`
	WeakEntriesCleared       uint64 `json:"weakEntriesCleared"`
	SlotBufferAllocations    uint64 `json:"slotBufferAllocations"`
	SlotBufferGrows          uint64 `json:"slotBufferGrows"`
	SlotBufferReuses         uint64 `json:"slotBufferReuses"`
	PooledSlotBuffers        uint64 `json:"pooledSlotBuffers"`
	PooledSlotCapacity       uint64 `json:"pooledSlotCapacity"`
	ReservedSlotCapacity     uint64 `json:"reservedSlotCapacity"`
	PeakReservedSlotCapacity uint64 `json:"peakReservedSlotCapacity"`
}

type objectEdge struct {
	from ownership.ObjectID
	to   ownership.ObjectID
}

type promotionKey struct {
	owner  ownership.OwnerID
	source Ref
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
	case HeapPromise:
		return "Promise"
	case HeapBigInt:
		return "BigInt"
	case HeapSymbol:
		return "Symbol"
	case HeapArrayBuffer:
		return "ArrayBuffer"
	case HeapTypedArray:
		return "TypedArray"
	case HeapMap:
		return "Map"
	case HeapSet:
		return "Set"
	case HeapDate:
		return "Date"
	case HeapRegExp:
		return "RegExp"
	case HeapError:
		return "Error"
	case HeapWeakMap:
		return "WeakMap"
	case HeapWeakSet:
		return "WeakSet"
	case HeapIterator:
		return "Iterator"
	case HeapHostObject:
		return "HostObject"
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

	nextRegion    RegionID
	regions       map[RegionID]*Region
	ownerClaims   map[ownership.OwnerID]ownership.RegionID
	closedOwners  map[ownership.OwnerID]bool
	barrier       *Barrier
	objectEdges   map[objectEdge]uint32
	objectRegions map[ownership.ObjectID]RegionID
	promotions    map[promotionKey]Ref
	slotBuffers   [][]Slot
	payloads      payloadAllocator
	stats         Stats

	sharedOwner ownership.OwnerID
	sharedClaim ownership.RegionID
}

func NewStore(ledger *ownership.Ledger) *Store {
	if ledger == nil {
		ledger = ownership.NewLedger()
	}
	return &Store{
		ledger:        ledger,
		regions:       make(map[RegionID]*Region),
		ownerClaims:   make(map[ownership.OwnerID]ownership.RegionID),
		closedOwners:  make(map[ownership.OwnerID]bool),
		barrier:       newBarrier(),
		objectEdges:   make(map[objectEdge]uint32),
		objectRegions: make(map[ownership.ObjectID]RegionID),
		promotions:    make(map[promotionKey]Ref),
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

// RegionMetadata returns state and capacity without cloning any Slot payload.
func (store *Store) RegionMetadata(id RegionID) (RegionMetadata, error) {
	if store == nil {
		return RegionMetadata{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	region := store.regions[id]
	if region == nil {
		return RegionMetadata{}, fmt.Errorf("%w: R%d", ErrUnknownRegion, id)
	}
	return RegionMetadata{
		ID:           region.ID,
		Owner:        region.Owner,
		State:        region.State,
		Slots:        len(region.Slots),
		SlotCapacity: cap(region.Slots),
	}, nil
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
	if kind < HeapCell || kind > HeapHostObject {
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
		store.initializeSlotPayloadLocked(slot, kind)
	} else {
		if uint64(len(region.Slots)) > math.MaxUint32 {
			_ = store.ledger.Release(object, owner)
			return Ref{}, fmt.Errorf("memory: region R%d exhausted slots", regionID)
		}
		index = uint32(len(region.Slots))
		store.ensureRegionSlotCapacityLocked(region, len(region.Slots)+1)
		region.Slots = append(region.Slots, Slot{Generation: 1, Occupied: true, object: object})
		store.initializeSlotPayloadLocked(&region.Slots[index], kind)
	}
	store.objectRegions[object] = region.ID
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
	*slot.String = cloneString(StringObject{Text: text})
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
	return cloneCell(*slot.Cell), nil
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
	value, err = store.replaceValueLocked(owner, region, slot, old, value, internal)
	if err != nil {
		return err
	}
	if field >= len(slot.Cell.Fields) {
		slot.Cell.Fields = append(slot.Cell.Fields, make([]Value, field-len(slot.Cell.Fields)+1)...)
	}
	slot.Cell.Fields[field] = value
	return nil
}

func (store *Store) replaceValueLocked(owner ownership.OwnerID, region *Region, slot *Slot, old, value Value, internal bool) (Value, error) {
	if old == value {
		return value, nil
	}
	prepared, err := store.prepareEscapingValueLocked(region, value, internal)
	if err != nil {
		return Value{}, err
	}
	value = prepared
	if old == value {
		return value, nil
	}
	var targetRegion *Region
	var targetSlot *Slot
	if value.IsRef() {
		targetRegion, targetSlot, err = store.readSlotLocked(owner, value.Ref())
		if err != nil && internal {
			targetRegion, targetSlot, err = store.slotLocked(value.Ref())
		}
		if err != nil {
			return Value{}, err
		}
		if targetRegion.State == RegionPrivate && targetRegion.Owner != region.Owner {
			return Value{}, fmt.Errorf("%w: R%d owned by %s cannot reference R%d owned by %s", ErrAccessDenied, region.ID, region.Owner, targetRegion.ID, targetRegion.Owner)
		}
		if err := store.linkLocked(region.ID, slot.object, targetRegion.ID, targetSlot.object); err != nil {
			return Value{}, err
		}
	}
	if old.IsRef() {
		oldRegion, oldSlot, oldErr := store.slotLocked(old.Ref())
		if oldErr != nil {
			if value.IsRef() {
				_ = store.unlinkLocked(region.ID, slot.object, targetRegion.ID, targetSlot.object)
			}
			return Value{}, oldErr
		}
		if err := store.unlinkLocked(region.ID, slot.object, oldRegion.ID, oldSlot.object); err != nil {
			if value.IsRef() {
				_ = store.unlinkLocked(region.ID, slot.object, targetRegion.ID, targetSlot.object)
			}
			return Value{}, err
		}
	}
	return value, nil
}

// prepareEscapingValueLocked implements the physical half of the lifetime
// ownership barrier. A mutable longer-lived region cannot point directly into
// a shorter-lived private region: the reachable graph is copied into storage
// owned by the destination lifetime and the stored Ref is rewritten. Repeated
// publication of the same source root to the same lifetime reuses the first
// promoted root.
func (store *Store) prepareEscapingValueLocked(destination *Region, value Value, internal bool) (Value, error) {
	if !value.IsRef() || internal {
		return value, nil
	}
	source, _, err := store.slotLocked(value.Ref())
	if err != nil {
		return Value{}, err
	}
	if source.State == RegionPublished || source.Owner == destination.Owner {
		return value, nil
	}
	if source.State == RegionInTransit {
		return Value{}, fmt.Errorf("%w: R%d", ErrRegionInTransit, source.ID)
	}
	if destination.Owner.Kind <= source.Owner.Kind {
		return Value{}, fmt.Errorf("%w: R%d owned by %s cannot retain R%d owned by %s", ErrAccessDenied, destination.ID, destination.Owner, source.ID, source.Owner)
	}
	key := promotionKey{owner: destination.Owner, source: value.Ref()}
	if cached := store.promotions[key]; cached != (Ref{}) {
		cachedRegion, _, cacheErr := store.slotLocked(cached)
		if cacheErr == nil && cachedRegion.Owner == destination.Owner {
			store.stats.PromotionCacheHits++
			return RefValue(cached), nil
		}
		delete(store.promotions, key)
	}
	promoted, err := store.copyLocked(source.Owner, destination.Owner, []Ref{value.Ref()})
	if err != nil {
		return Value{}, err
	}
	store.promotions[key] = promoted[0]
	store.stats.AutomaticPromotions++
	return RefValue(promoted[0]), nil
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
	if err := store.clearSlotPayloadLocked(slot); err != nil {
		return err
	}
	delete(store.objectRegions, slot.object)
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

// StructuredClone copies roots into one destination-owned graph and moves the
// bytes of every listed ArrayBuffer into the clone by detaching the source.
// Validation and copying complete before any source buffer is detached.
func (store *Store) StructuredClone(
	from, to ownership.OwnerID,
	roots []Ref,
	transfers []Ref,
) (clonedRoots []Ref, clonedTransfers []Ref, result error) {
	if store == nil {
		return nil, nil, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()

	seen := make(map[Ref]struct{}, len(transfers))
	for _, ref := range transfers {
		if _, duplicate := seen[ref]; duplicate {
			return nil, nil, fmt.Errorf("%w: %s", ErrDuplicateTransfer, ref)
		}
		seen[ref] = struct{}{}
		_, slot, err := store.writeSlotLocked(from, ref, false)
		if err != nil {
			return nil, nil, err
		}
		if slot.Kind != HeapArrayBuffer {
			return nil, nil, typeError(ref, slot.Kind, HeapArrayBuffer)
		}
		if slot.ArrayBuffer.Detached {
			return nil, nil, ErrDetachedBuffer
		}
	}

	allRoots := make([]Ref, 0, len(roots)+len(transfers))
	allRoots = append(allRoots, roots...)
	allRoots = append(allRoots, transfers...)
	cloned, err := store.copyLocked(from, to, allRoots)
	if err != nil {
		return nil, nil, err
	}
	for _, ref := range transfers {
		_, slot, _ := store.slotLocked(ref)
		store.stats.LiveBytes -= uint64(len(slot.ArrayBuffer.Bytes))
		slot.ArrayBuffer.Bytes = nil
		slot.ArrayBuffer.Detached = true
	}
	return cloned[:len(roots)], cloned[len(roots):], nil
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

// Kind returns the concrete payload kind after applying ordinary Ref access
// checks. It lets execution layers dispatch without probing every typed deref.
func (store *Store) Kind(owner ownership.OwnerID, ref Ref) (HeapKind, error) {
	if store == nil {
		return HeapInvalid, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	_, slot, err := store.readSlotLocked(owner, ref)
	if err != nil {
		return HeapInvalid, err
	}
	return slot.Kind, nil
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
	case HeapPromise:
		store.stats.LivePromises++
	case HeapBigInt:
		store.stats.LiveBigInts++
	case HeapSymbol:
		store.stats.LiveSymbols++
	case HeapArrayBuffer:
		store.stats.LiveArrayBuffers++
	case HeapTypedArray:
		store.stats.LiveTypedArrays++
	case HeapMap:
		store.stats.LiveMaps++
	case HeapSet:
		store.stats.LiveSets++
	case HeapDate:
		store.stats.LiveDates++
	case HeapRegExp:
		store.stats.LiveRegExps++
	case HeapError:
		store.stats.LiveErrors++
	case HeapWeakMap:
		store.stats.LiveWeakMaps++
	case HeapWeakSet:
		store.stats.LiveWeakSets++
	case HeapIterator:
		store.stats.LiveIterators++
	case HeapHostObject:
		store.stats.LiveHostObjects++
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
		store.stats.LiveBytes -= uint64(len(slot.Function.Code)) + uint64(len(slot.Function.Locations))*8
	case HeapPromise:
		store.stats.LivePromises--
	case HeapBigInt:
		store.stats.LiveBigInts--
		store.stats.LiveBytes -= uint64(len(slot.BigInt.Magnitude))
	case HeapSymbol:
		store.stats.LiveSymbols--
	case HeapArrayBuffer:
		store.stats.LiveArrayBuffers--
		store.stats.LiveBytes -= uint64(len(slot.ArrayBuffer.Bytes))
	case HeapTypedArray:
		store.stats.LiveTypedArrays--
	case HeapMap:
		store.stats.LiveMaps--
	case HeapSet:
		store.stats.LiveSets--
	case HeapDate:
		store.stats.LiveDates--
	case HeapRegExp:
		store.stats.LiveRegExps--
	case HeapError:
		store.stats.LiveErrors--
	case HeapWeakMap:
		store.stats.LiveWeakMaps--
	case HeapWeakSet:
		store.stats.LiveWeakSets--
	case HeapIterator:
		store.stats.LiveIterators--
	case HeapHostObject:
		store.stats.LiveHostObjects--
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
	store.releaseAllSlotBuffersLocked()
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
	_, err := store.unlinkSlotWithScratchLocked(region, slot, nil)
	return err
}

func (store *Store) unlinkSlotWithScratchLocked(region *Region, slot *Slot, references []Value) ([]Value, error) {
	references = appendSlotReferences(references[:0], slot)
	for _, value := range references {
		if !value.IsRef() {
			continue
		}
		targetRegion, targetSlot, err := store.slotLocked(value.Ref())
		if err != nil {
			return references, fmt.Errorf("memory: unlink %s object %d in R%d through %s: %w", slot.Kind, slot.object, region.ID, value.Ref(), err)
		}
		if err := store.unlinkLocked(region.ID, slot.object, targetRegion.ID, targetSlot.object); err != nil {
			return references, err
		}
	}
	return references, nil
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
	references := make([]Value, 0, 16)
	for _, id := range ordered {
		region := store.regions[id]
		for index := range region.Slots {
			slot := &region.Slots[index]
			if slot.Occupied {
				var err error
				references, err = store.unlinkSlotWithScratchLocked(region, slot, references)
				if err != nil {
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
			if err := store.clearSlotPayloadLocked(slot); err != nil {
				return err
			}
			delete(store.objectRegions, slot.object)
			slot.object = 0
			slot.Occupied = false
			if slot.Generation != math.MaxUint32 {
				slot.Generation++
			}
			store.stats.LiveSlots--
			store.stats.Frees++
		}
		region.free = nil
		store.releaseSlotBufferLocked(region.Slots)
		region.Slots = nil
		region.State = RegionDestroyed
		store.stats.LiveRegions--
		store.stats.BulkRegionReleases++
	}
	return nil
}

func (store *Store) regionForObjectLocked(object ownership.ObjectID) RegionID {
	return store.objectRegions[object]
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
	type copyCandidate struct {
		ref       Ref
		from      Ref
		fromKind  HeapKind
		reference int
		root      int
	}
	queue := make([]copyCandidate, len(roots))
	for index, root := range roots {
		queue[index] = copyCandidate{ref: root, reference: -1, root: index}
	}
	seen := make(map[Ref]struct{})
	order := make([]Ref, 0)
	references := make([]Value, 0, 16)
	drainStrongReferences := func() error {
		for len(queue) != 0 {
			candidate := queue[0]
			queue = queue[1:]
			ref := candidate.ref
			if _, exists := seen[ref]; exists {
				continue
			}
			region, slot, err := store.slotLocked(ref)
			if err != nil {
				if candidate.from != (Ref{}) {
					return fmt.Errorf("memory: copy reached %s from %s %s reference %d: %w", ref, candidate.from, candidate.fromKind, candidate.reference, err)
				}
				return fmt.Errorf("memory: copy root %d %s: %w", candidate.root, ref, err)
			}
			if region.State == RegionInTransit {
				return fmt.Errorf("%w: R%d", ErrRegionInTransit, region.ID)
			}
			if region.State == RegionPrivate && region.Owner != from {
				return store.accessError(region, from)
			}
			seen[ref] = struct{}{}
			order = append(order, ref)
			references = appendSlotReferences(references[:0], slot)
			for reference, value := range references {
				if value.IsRef() {
					queue = append(queue, copyCandidate{
						ref:       value.Ref(),
						from:      ref,
						fromKind:  slot.Kind,
						reference: reference,
						root:      -1,
					})
				}
			}
		}
		return nil
	}
	if err := drainStrongReferences(); err != nil {
		return nil, err
	}

	// WeakMap values are ephemerons: a strongly reachable table retains a
	// value only when its key is independently strongly reachable. Reach a
	// fixed point because one live ephemeron value may reveal another key.
	for {
		added := false
		for _, ref := range append([]Ref(nil), order...) {
			_, slot, _ := store.slotLocked(ref)
			if slot.Kind != HeapWeakMap {
				continue
			}
			for reference, entry := range slot.WeakMap.Entries {
				if _, keyLive := seen[entry.Key]; !keyLive || !entry.Value.IsRef() {
					continue
				}
				if _, valueLive := seen[entry.Value.Ref()]; valueLive {
					continue
				}
				queue = append(queue, copyCandidate{
					ref: entry.Value.Ref(), from: ref, fromKind: HeapWeakMap, reference: reference, root: -1,
				})
				added = true
			}
		}
		if !added {
			break
		}
		if err := drainStrongReferences(); err != nil {
			return nil, err
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
			*copySlot.String = cloneString(*sourceSlot.String)
			store.stats.LiveBytes += uint64(len(copySlot.String.Text))
		} else if sourceSlot.Kind == HeapBigInt {
			_, copySlot, _ := store.slotLocked(copyRef)
			*copySlot.BigInt = cloneBigInt(*sourceSlot.BigInt)
			store.stats.LiveBytes += uint64(len(copySlot.BigInt.Magnitude))
		} else if sourceSlot.Kind == HeapArrayBuffer {
			_, copySlot, _ := store.slotLocked(copyRef)
			*copySlot.ArrayBuffer = cloneArrayBuffer(*sourceSlot.ArrayBuffer)
			store.stats.LiveBytes += uint64(len(copySlot.ArrayBuffer.Bytes))
		} else if sourceSlot.Kind == HeapHostObject {
			_, copySlot, _ := store.slotLocked(copyRef)
			*copySlot.HostObject = *sourceSlot.HostObject
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
			// The shared object header is copied after the typed payload.
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
				if binding.Indirect {
					if err := store.declareIndirectBindingLocked(to, copyRef, name, mapping[binding.Target], mapping[binding.TargetName], true); err != nil {
						_ = store.destroyRegionsLocked(map[RegionID]struct{}{destination.ID: {}})
						return nil, err
					}
					continue
				}
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
			function := cloneFunction(*sourceSlot.Function)
			function.ObjectHeader = ObjectHeader{}
			function.Name = remapValue(function.Name, mapping)
			function.Environment = remapValue(function.Environment, mapping)
			function.LexicalThis = remapValue(function.LexicalThis, mapping)
			for index, constant := range function.Constants {
				function.Constants[index] = remapValue(constant, mapping)
			}
			for index, capture := range function.Captures {
				function.Captures[index] = remapValue(capture, mapping)
			}
			if err := store.initializeFunctionLocked(to, copyRef, function, true, false); err != nil {
				_ = store.destroyRegionsLocked(map[RegionID]struct{}{destination.ID: {}})
				return nil, err
			}
		case HeapPromise:
			for _, reaction := range sourceSlot.Promise.Reactions {
				copyReaction := PromiseReaction{
					OnFulfilled: remapValue(reaction.OnFulfilled, mapping),
					OnRejected:  remapValue(reaction.OnRejected, mapping),
					Downstream:  remapValue(reaction.Downstream, mapping),
				}
				if err := store.addPromiseReactionLocked(to, copyRef, copyReaction, true); err != nil {
					_ = store.destroyRegionsLocked(map[RegionID]struct{}{destination.ID: {}})
					return nil, err
				}
			}
			if sourceSlot.Promise.State != PromisePending {
				if err := store.settlePromiseLocked(to, copyRef, sourceSlot.Promise.State, remapValue(sourceSlot.Promise.Result, mapping), true); err != nil {
					_ = store.destroyRegionsLocked(map[RegionID]struct{}{destination.ID: {}})
					return nil, err
				}
			}
			if sourceSlot.Promise.Handled {
				if err := store.markPromiseHandledLocked(to, copyRef, true); err != nil {
					_ = store.destroyRegionsLocked(map[RegionID]struct{}{destination.ID: {}})
					return nil, err
				}
			}
		case HeapBigInt:
			// Immutable payload was cloned during allocation.
		case HeapSymbol:
			symbol := cloneSymbol(*sourceSlot.Symbol)
			symbol.Description = remapValue(symbol.Description, mapping)
			if err := store.initializeSymbolLocked(to, copyRef, symbol, true); err != nil {
				_ = store.destroyRegionsLocked(map[RegionID]struct{}{destination.ID: {}})
				return nil, err
			}
		case HeapArrayBuffer:
			// Mutable bytes were cloned during allocation.
		case HeapTypedArray:
			view := cloneTypedArray(*sourceSlot.TypedArray)
			view.Buffer = mapping[view.Buffer]
			if err := store.initializeTypedArrayLocked(to, copyRef, view, true); err != nil {
				_ = store.destroyRegionsLocked(map[RegionID]struct{}{destination.ID: {}})
				return nil, err
			}
		case HeapMap:
			for _, entry := range sourceSlot.Map.Entries {
				if err := store.mapSetLocked(to, copyRef, remapValue(entry.Key, mapping), remapValue(entry.Value, mapping), true); err != nil {
					_ = store.destroyRegionsLocked(map[RegionID]struct{}{destination.ID: {}})
					return nil, err
				}
			}
		case HeapSet:
			for _, value := range sourceSlot.Set.Values {
				if err := store.setAddLocked(to, copyRef, remapValue(value, mapping), true); err != nil {
					_ = store.destroyRegionsLocked(map[RegionID]struct{}{destination.ID: {}})
					return nil, err
				}
			}
		case HeapDate:
			if err := store.setDateTimeLocked(to, copyRef, sourceSlot.Date.Milliseconds, true); err != nil {
				_ = store.destroyRegionsLocked(map[RegionID]struct{}{destination.ID: {}})
				return nil, err
			}
		case HeapRegExp:
			expression := cloneRegExp(*sourceSlot.RegExp)
			expression.Pattern = mapping[expression.Pattern]
			if err := store.initializeRegExpLocked(to, copyRef, expression, true); err != nil {
				_ = store.destroyRegionsLocked(map[RegionID]struct{}{destination.ID: {}})
				return nil, err
			}
		case HeapError:
			value := cloneError(*sourceSlot.Error)
			value.ObjectHeader = ObjectHeader{}
			value.Message = remapValue(value.Message, mapping)
			value.Stack = remapValue(value.Stack, mapping)
			if value.HasCause {
				value.Cause = remapValue(value.Cause, mapping)
			}
			for index, member := range value.Errors {
				value.Errors[index] = remapValue(member, mapping)
			}
			if err := store.initializeErrorLocked(to, copyRef, value, true); err != nil {
				_ = store.destroyRegionsLocked(map[RegionID]struct{}{destination.ID: {}})
				return nil, err
			}
		case HeapWeakMap:
			_, copySlot, _ := store.slotLocked(copyRef)
			for _, entry := range sourceSlot.WeakMap.Entries {
				key, keyLive := mapping[entry.Key]
				if !keyLive {
					continue
				}
				value := entry.Value
				if value.IsRef() {
					mapped, valueLive := mapping[value.Ref()]
					if !valueLive {
						continue
					}
					value = RefValue(mapped)
				}
				copySlot.WeakMap.Entries = append(copySlot.WeakMap.Entries, WeakMapEntry{
					Key: key, Value: value,
				})
			}
		case HeapWeakSet:
			_, copySlot, _ := store.slotLocked(copyRef)
			for _, key := range sourceSlot.WeakSet.Keys {
				if mapped, live := mapping[key]; live {
					copySlot.WeakSet.Keys = append(copySlot.WeakSet.Keys, mapped)
				}
			}
		case HeapIterator:
			copyRegion, copySlot, _ := store.slotLocked(copyRef)
			target, err := store.replaceValueLocked(to, copyRegion, copySlot, Value{}, RefValue(mapping[sourceSlot.Iterator.Target]), true)
			if err != nil {
				_ = store.destroyRegionsLocked(map[RegionID]struct{}{destination.ID: {}})
				return nil, err
			}
			copySlot.Iterator.Target = target.Ref()
			copySlot.Iterator.Kind = sourceSlot.Iterator.Kind
			copySlot.Iterator.Next = sourceSlot.Iterator.Next
		case HeapHostObject:
			// Immutable scalar identity was cloned during allocation.
		default:
			_ = store.destroyRegionsLocked(map[RegionID]struct{}{destination.ID: {}})
			return nil, fmt.Errorf("%w: cannot copy heap kind %d", ErrTypeMismatch, sourceSlot.Kind)
		}
	}
	// Object headers are copied only after every typed payload is initialized.
	// Symbol property keys compare by semantic ID and accessors validate their
	// Function refs, so copying a header earlier could observe zero-value
	// destination payloads and collapse distinct keys or reject valid getters.
	for _, source := range order {
		_, sourceSlot, _ := store.slotLocked(source)
		sourceHeader, objectLike := objectHeaderForSlot(sourceSlot)
		if !objectLike {
			continue
		}
		if err := store.copyObjectHeaderLocked(to, mapping[source], *sourceHeader, mapping); err != nil {
			_ = store.destroyRegionsLocked(map[RegionID]struct{}{destination.ID: {}})
			return nil, err
		}
	}
	result := make([]Ref, len(roots))
	for index, root := range roots {
		result[index] = mapping[root]
	}
	return result, nil
}

func (store *Store) copyObjectHeaderLocked(to ownership.OwnerID, copyRef Ref, source ObjectHeader, mapping map[Ref]Ref) error {
	if err := store.setPrototypeLocked(to, copyRef, remapValue(source.Prototype, mapping), true); err != nil {
		return err
	}
	for _, property := range source.Properties {
		descriptor := property
		descriptor.Name = Ref{}
		descriptor.Value = remapValue(property.Value, mapping)
		descriptor.Getter = remapValue(property.Getter, mapping)
		descriptor.Setter = remapValue(property.Setter, mapping)
		if err := store.definePropertyLocked(to, copyRef, mapping[property.Name], descriptor, true); err != nil {
			return err
		}
	}
	return store.setObjectIntegrityLocked(to, copyRef, source.NonExtensible, source.ImmutablePrototype, true)
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
