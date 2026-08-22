package ownership

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// DefaultEventLimit retains enough recent ownership transitions for the
// inspector and invariant diagnostics without allowing production workloads
// to turn telemetry into an unbounded heap.
const DefaultEventLimit = 16 * 1024

// Ledger is a concurrency-safe shadow model of Gossamer object ownership.
// One region contributes at most one semantic claim to an object; ordinary
// aliases and graph edges within that region do not affect its claim count.
type Ledger struct {
	mutex sync.Mutex

	nextObject ObjectID
	nextRegion RegionID
	nextEvent  uint64

	objects      map[ObjectID]*objectRecord
	regions      map[RegionID]*regionRecord
	ownerRegions map[OwnerID]RegionID
	events       []Event
	eventStart   int
	eventLimit   int
	destroyed    map[ObjectID]RegionID
	stats        Stats
}

type objectRecord struct {
	id     ObjectID
	region RegionID
	claims map[RegionID]struct{}
	edges  map[ObjectID]struct{}
	// incoming is a non-owning adjacency index used only for direct unlinking.
	// It never contributes an ARC claim.
	incoming map[ObjectID]struct{}
	alive    bool
}

type regionRecord struct {
	id      RegionID
	owner   OwnerID
	objects map[ObjectID]struct{}
	claims  map[ObjectID]struct{}
	closed  bool
}

func NewLedger() *Ledger {
	return NewLedgerWithEventLimit(DefaultEventLimit)
}

// NewLedgerWithEventLimit configures retained telemetry. A zero limit keeps
// counters but no Event payloads; a negative limit preserves unbounded history
// for deliberately small diagnostic workloads.
func NewLedgerWithEventLimit(eventLimit int) *Ledger {
	return &Ledger{
		objects:      make(map[ObjectID]*objectRecord),
		regions:      make(map[RegionID]*regionRecord),
		ownerRegions: make(map[OwnerID]RegionID),
		destroyed:    make(map[ObjectID]RegionID),
		eventLimit:   eventLimit,
	}
}

// CreateRegion registers the one logical region owned by owner.
func (ledger *Ledger) CreateRegion(owner OwnerID) (RegionID, error) {
	if ledger == nil {
		return 0, fmt.Errorf("ownership: nil ledger")
	}
	if owner.Value == 0 || owner.Kind > OwnerShared {
		return 0, fmt.Errorf("%w: %s", ErrInvalidOwner, owner)
	}

	ledger.mutex.Lock()
	defer ledger.mutex.Unlock()
	if _, exists := ledger.ownerRegions[owner]; exists {
		return 0, fmt.Errorf("%w: %s", ErrOwnerRegistered, owner)
	}
	ledger.nextRegion++
	region := &regionRecord{
		id:      ledger.nextRegion,
		owner:   owner,
		objects: make(map[ObjectID]struct{}),
		claims:  make(map[ObjectID]struct{}),
	}
	ledger.regions[region.id] = region
	ledger.ownerRegions[owner] = region.id
	ledger.recordLocked(Event{Kind: RegionCreated, Region: region.id, Owner: owner})
	return region.id, nil
}

// CreateObject allocates a fake runtime object in region. The allocation gives
// the region its single semantic claim regardless of local aliases.
func (ledger *Ledger) CreateObject(regionID RegionID) (ObjectID, error) {
	if ledger == nil {
		return 0, fmt.Errorf("ownership: nil ledger")
	}
	ledger.mutex.Lock()
	defer ledger.mutex.Unlock()
	region, err := ledger.activeRegionLocked(regionID)
	if err != nil {
		return 0, err
	}

	ledger.nextObject++
	object := &objectRecord{
		id:       ledger.nextObject,
		region:   region.id,
		claims:   map[RegionID]struct{}{region.id: {}},
		edges:    make(map[ObjectID]struct{}),
		incoming: make(map[ObjectID]struct{}),
		alive:    true,
	}
	ledger.objects[object.id] = object
	region.objects[object.id] = struct{}{}
	region.claims[object.id] = struct{}{}
	ledger.stats.ObjectsCreated++
	ledger.stats.LiveObjects++
	if region.owner.Kind == OwnerTask {
		ledger.stats.TaskLocalAllocations++
	}
	ledger.recordLocked(Event{
		Kind:       ObjectCreated,
		Object:     object.id,
		Region:     region.id,
		Owner:      region.owner,
		References: 1,
	})
	return object.id, nil
}

// AddReference records an ordinary object-graph edge. The edge itself does not
// change ARC. If a longer-lived claiming region points into a shorter-lived
// graph, the ownership barrier adds one claim from that region to each required
// reachable object before the edge becomes visible.
func (ledger *Ledger) AddReference(fromID, toID ObjectID) error {
	if ledger == nil {
		return fmt.Errorf("ownership: nil ledger")
	}
	ledger.mutex.Lock()
	defer ledger.mutex.Unlock()
	from, err := ledger.liveObjectLocked(fromID)
	if err != nil {
		return err
	}
	to, err := ledger.liveObjectLocked(toID)
	if err != nil {
		return err
	}
	if _, exists := from.edges[toID]; exists {
		return nil
	}

	claimRegions := sortedRegionIDs(from.claims)
	for _, regionID := range claimRegions {
		// A claimed object already has the region's claim propagated through
		// its reachable graph. Subsequent edges from that graph pass through
		// this same barrier, so an existing root claim is an O(1) no-op.
		if _, claimed := to.claims[regionID]; claimed {
			continue
		}
		if err := ledger.barrierRetainLocked(toID, regionID, fromID); err != nil {
			return err
		}
	}
	from.edges[toID] = struct{}{}
	to.incoming[fromID] = struct{}{}
	ledger.stats.LocalReferences++
	ledger.recordLocked(Event{Kind: ObjectLinked, Object: fromID, Target: toID, References: referenceCount(from)})
	return nil
}

// RemoveReference removes a local graph edge. Claims established by the
// barrier remain owned by their regions until an ownership boundary releases
// those regions; local pointer churn does not generate ARC churn.
func (ledger *Ledger) RemoveReference(fromID, toID ObjectID) error {
	if ledger == nil {
		return fmt.Errorf("ownership: nil ledger")
	}
	ledger.mutex.Lock()
	defer ledger.mutex.Unlock()
	from, err := ledger.liveObjectLocked(fromID)
	if err != nil {
		return err
	}
	if _, exists := from.edges[toID]; !exists {
		return nil
	}
	delete(from.edges, toID)
	if to := ledger.objects[toID]; to != nil {
		delete(to.incoming, fromID)
	}
	ledger.recordLocked(Event{Kind: ObjectUnlinked, Object: fromID, Target: toID, References: referenceCount(from)})
	return nil
}

// Reachable returns the deterministic transitive closure of roots, including
// the roots themselves.
func (ledger *Ledger) Reachable(roots []ObjectID) ([]ObjectID, error) {
	if ledger == nil {
		return nil, fmt.Errorf("ownership: nil ledger")
	}
	ledger.mutex.Lock()
	defer ledger.mutex.Unlock()
	return ledger.reachableLocked(roots)
}

// Publish adds the destination region's single claim to object and every
// object reachable from it without removing the publisher's claim.
func (ledger *Ledger) Publish(object ObjectID, from, to OwnerID) error {
	return ledger.PublishAll([]ObjectID{object}, from, to)
}

// PublishAll applies graph publication atomically to every supplied root.
func (ledger *Ledger) PublishAll(roots []ObjectID, from, to OwnerID) error {
	if ledger == nil {
		return fmt.Errorf("ownership: nil ledger")
	}
	if len(roots) == 0 {
		return nil
	}
	ledger.mutex.Lock()
	defer ledger.mutex.Unlock()
	fromRegion, err := ledger.activeOwnerRegionLocked(from)
	if err != nil {
		return err
	}
	toRegion, err := ledger.activeOwnerRegionLocked(to)
	if err != nil {
		return err
	}
	objects, err := ledger.reachableLocked(roots)
	if err != nil {
		return err
	}
	if err := ledger.requireClaimsLocked(uniqueObjectIDs(roots), fromRegion.id); err != nil {
		return err
	}
	ledger.stats.PublishOperations += len(uniqueObjectIDs(roots))
	ledger.publishSetLocked(objects, fromRegion, toRegion)
	return nil
}

// PublishSet publishes exactly the supplied objects. It is used by queue
// scheduling after reachability has already been partitioned.
func (ledger *Ledger) PublishSet(objects []ObjectID, from, to OwnerID) error {
	if ledger == nil {
		return fmt.Errorf("ownership: nil ledger")
	}
	if len(objects) == 0 {
		return nil
	}
	ledger.mutex.Lock()
	defer ledger.mutex.Unlock()
	fromRegion, err := ledger.activeOwnerRegionLocked(from)
	if err != nil {
		return err
	}
	toRegion, err := ledger.activeOwnerRegionLocked(to)
	if err != nil {
		return err
	}
	objects = uniqueObjectIDs(objects)
	if err := ledger.requireLiveLocked(objects); err != nil {
		return err
	}
	if err := ledger.requireClaimsLocked(objects, fromRegion.id); err != nil {
		return err
	}
	ledger.publishSetLocked(objects, fromRegion, toRegion)
	return nil
}

// Transfer moves the source region's claim on the reachable graph to the
// destination region. It does not model the move as retain plus release.
func (ledger *Ledger) Transfer(object ObjectID, from, to OwnerID) error {
	return ledger.TransferAll([]ObjectID{object}, from, to)
}

// TransferAll applies graph transfer atomically to every supplied root.
func (ledger *Ledger) TransferAll(roots []ObjectID, from, to OwnerID) error {
	if ledger == nil {
		return fmt.Errorf("ownership: nil ledger")
	}
	if len(roots) == 0 {
		return nil
	}
	ledger.mutex.Lock()
	defer ledger.mutex.Unlock()
	fromRegion, err := ledger.activeOwnerRegionLocked(from)
	if err != nil {
		return err
	}
	toRegion, err := ledger.activeOwnerRegionLocked(to)
	if err != nil {
		return err
	}
	objects, err := ledger.reachableLocked(roots)
	if err != nil {
		return err
	}
	if err := ledger.requireClaimsLocked(objects, fromRegion.id); err != nil {
		return err
	}
	ledger.transferSetLocked(objects, fromRegion, toRegion)
	return nil
}

// TransferSet transfers exactly the supplied claims after a scheduler has
// already partitioned shared and exclusively-owned reachable objects.
func (ledger *Ledger) TransferSet(objects []ObjectID, from, to OwnerID) error {
	if ledger == nil {
		return fmt.Errorf("ownership: nil ledger")
	}
	if len(objects) == 0 {
		return nil
	}
	ledger.mutex.Lock()
	defer ledger.mutex.Unlock()
	fromRegion, err := ledger.activeOwnerRegionLocked(from)
	if err != nil {
		return err
	}
	toRegion, err := ledger.activeOwnerRegionLocked(to)
	if err != nil {
		return err
	}
	objects = uniqueObjectIDs(objects)
	if err := ledger.requireLiveLocked(objects); err != nil {
		return err
	}
	if err := ledger.requireClaimsLocked(objects, fromRegion.id); err != nil {
		return err
	}
	ledger.transferSetLocked(objects, fromRegion, toRegion)
	return nil
}

// Handoff atomically publishes objects still shared by the source region and
// transfers objects for which the destination becomes the sole boundary
// owner. The supplied sets must already represent complete reachability.
func (ledger *Ledger) Handoff(shared, exclusive []ObjectID, from, to OwnerID) error {
	if ledger == nil {
		return fmt.Errorf("ownership: nil ledger")
	}
	shared = uniqueObjectIDs(shared)
	exclusive = uniqueObjectIDs(exclusive)
	if len(shared) == 0 && len(exclusive) == 0 {
		return nil
	}
	ledger.mutex.Lock()
	defer ledger.mutex.Unlock()
	fromRegion, err := ledger.activeOwnerRegionLocked(from)
	if err != nil {
		return err
	}
	toRegion, err := ledger.activeOwnerRegionLocked(to)
	if err != nil {
		return err
	}
	all := append(append(make([]ObjectID, 0, len(shared)+len(exclusive)), shared...), exclusive...)
	if len(uniqueObjectIDs(all)) != len(all) {
		return fmt.Errorf("ownership: handoff sets overlap")
	}
	if err := ledger.requireLiveLocked(all); err != nil {
		return err
	}
	if err := ledger.requireClaimsLocked(all, fromRegion.id); err != nil {
		return err
	}
	ledger.stats.PublishOperations += len(shared)
	ledger.publishSetLocked(shared, fromRegion, toRegion)
	ledger.transferSetLocked(exclusive, fromRegion, toRegion)
	return nil
}

// Release removes the owner's region claim and destroys the object when the
// final region claim disappears.
func (ledger *Ledger) Release(objectID ObjectID, owner OwnerID) error {
	if ledger == nil {
		return fmt.Errorf("ownership: nil ledger")
	}
	ledger.mutex.Lock()
	defer ledger.mutex.Unlock()
	object, err := ledger.liveObjectLocked(objectID)
	if err != nil {
		return err
	}
	region, err := ledger.ownerRegionLocked(owner)
	if err != nil {
		return err
	}
	if _, claimed := object.claims[region.id]; !claimed {
		return fmt.Errorf("%w: object %d by %s", ErrNotOwned, objectID, owner)
	}
	ledger.releaseClaimLocked(object, region)
	return nil
}

// ReconcileRegion makes owner's claims exactly cover the graph reachable from
// roots. It is the region-level counterpart to changing a semantic root set:
// ordinary aliases and graph-edge churn remain ARC-free, while a wrapper cache
// or document boundary can retain newly reachable objects and collect its
// unreachable subgraph in one operation. The returned IDs are the objects
// whose final claim disappeared.
func (ledger *Ledger) ReconcileRegion(owner OwnerID, roots []ObjectID) ([]ObjectID, error) {
	if ledger == nil {
		return nil, fmt.Errorf("ownership: nil ledger")
	}
	ledger.mutex.Lock()
	defer ledger.mutex.Unlock()
	region, err := ledger.activeOwnerRegionLocked(owner)
	if err != nil {
		return nil, err
	}
	reachable, err := ledger.reachableLocked(roots)
	if err != nil {
		return nil, err
	}
	kept := make(map[ObjectID]struct{}, len(reachable))
	for _, objectID := range reachable {
		kept[objectID] = struct{}{}
		object := ledger.objects[objectID]
		if !ledger.addClaimLocked(object, region) {
			continue
		}
		ledger.promoteStorageLocked(object, region)
		ledger.stats.RetainOperations++
		ledger.recordLocked(Event{
			Kind:       ObjectRootRetained,
			Object:     object.id,
			Region:     object.region,
			Owner:      region.owner,
			References: referenceCount(object),
		})
	}

	destroyed := make([]ObjectID, 0)
	for _, objectID := range sortedObjectIDs(region.claims) {
		if _, exists := kept[objectID]; exists {
			continue
		}
		object := ledger.objects[objectID]
		if object == nil || !object.alive {
			delete(region.claims, objectID)
			continue
		}
		ledger.releaseClaimLocked(object, region)
		if !object.alive {
			destroyed = append(destroyed, objectID)
		}
	}
	return destroyed, nil
}

// CloseRegion performs one semantic bulk release of every claim held by the
// region. Each object has at most one claim to remove.
func (ledger *Ledger) CloseRegion(regionID RegionID) error {
	if ledger == nil {
		return fmt.Errorf("ownership: nil ledger")
	}
	ledger.mutex.Lock()
	defer ledger.mutex.Unlock()
	region := ledger.regions[regionID]
	if region == nil {
		return fmt.Errorf("%w: %d", ErrUnknownRegion, regionID)
	}
	if region.closed {
		return nil
	}
	region.closed = true
	ledger.stats.BulkRegionReleases++
	ledger.recordLocked(Event{Kind: RegionReleased, Region: region.id, Owner: region.owner})

	claimedObjects := sortedObjectIDs(region.claims)
	for _, objectID := range claimedObjects {
		object := ledger.objects[objectID]
		if object == nil || !object.alive {
			delete(region.claims, objectID)
			continue
		}
		ledger.releaseClaimLocked(object, region)
		if object.alive && object.region == region.id {
			ledger.moveToLongestLivedClaimLocked(object)
		}
	}
	return nil
}

// OwnerRegion returns the active logical claim region registered for owner.
// Physical heaps can use this to bind their storage regions to the existing
// ownership ledger without conflating physical region identity with a claim.
func (ledger *Ledger) OwnerRegion(owner OwnerID) (RegionID, error) {
	if ledger == nil {
		return 0, fmt.Errorf("ownership: nil ledger")
	}
	ledger.mutex.Lock()
	defer ledger.mutex.Unlock()
	region, err := ledger.activeOwnerRegionLocked(owner)
	if err != nil {
		return 0, err
	}
	return region.id, nil
}

func (ledger *Ledger) Object(objectID ObjectID) (ObjectSnapshot, error) {
	if ledger == nil {
		return ObjectSnapshot{}, fmt.Errorf("ownership: nil ledger")
	}
	ledger.mutex.Lock()
	defer ledger.mutex.Unlock()
	object := ledger.objects[objectID]
	if object == nil {
		if region, destroyed := ledger.destroyed[objectID]; destroyed {
			return ObjectSnapshot{ID: objectID, Region: region, Alive: false}, nil
		}
		return ObjectSnapshot{}, fmt.Errorf("%w: %d", ErrUnknownObject, objectID)
	}
	owners := make(map[OwnerID]int, len(object.claims))
	claims := sortedRegionIDs(object.claims)
	for _, regionID := range claims {
		if region := ledger.regions[regionID]; region != nil {
			owners[region.owner] = 1
		}
	}
	edges := sortedObjectIDs(object.edges)
	return ObjectSnapshot{
		ID:         object.id,
		Region:     object.region,
		Owners:     owners,
		Claims:     claims,
		Edges:      edges,
		References: referenceCount(object),
		Alive:      object.alive,
	}, nil
}

func (ledger *Ledger) Region(regionID RegionID) (RegionSnapshot, error) {
	if ledger == nil {
		return RegionSnapshot{}, fmt.Errorf("ownership: nil ledger")
	}
	ledger.mutex.Lock()
	defer ledger.mutex.Unlock()
	region := ledger.regions[regionID]
	if region == nil {
		return RegionSnapshot{}, fmt.Errorf("%w: %d", ErrUnknownRegion, regionID)
	}
	return RegionSnapshot{
		ID:      region.id,
		Owner:   region.owner,
		Objects: sortedObjectIDs(region.objects),
		Claims:  sortedObjectIDs(region.claims),
		Closed:  region.closed,
	}, nil
}

func (ledger *Ledger) Events() []Event {
	if ledger == nil {
		return nil
	}
	ledger.mutex.Lock()
	defer ledger.mutex.Unlock()
	if ledger.eventStart == 0 || len(ledger.events) == 0 {
		return append([]Event(nil), ledger.events...)
	}
	events := make([]Event, 0, len(ledger.events))
	events = append(events, ledger.events[ledger.eventStart:]...)
	events = append(events, ledger.events[:ledger.eventStart]...)
	return events
}

func (ledger *Ledger) Stats() Stats {
	if ledger == nil {
		return Stats{}
	}
	ledger.mutex.Lock()
	defer ledger.mutex.Unlock()
	stats := ledger.stats
	for _, object := range ledger.objects {
		if !object.alive {
			continue
		}
		region := ledger.regions[object.region]
		if region != nil && region.owner.Kind > OwnerTask {
			stats.PersistentObjects++
		}
	}
	return stats
}

func (ledger *Ledger) barrierRetainLocked(root ObjectID, claimRegionID RegionID, parent ObjectID) error {
	claimRegion, err := ledger.activeRegionLocked(claimRegionID)
	if err != nil {
		return err
	}
	objects, err := ledger.reachableLocked([]ObjectID{root})
	if err != nil {
		return err
	}
	for _, objectID := range objects {
		object := ledger.objects[objectID]
		storage := ledger.regions[object.region]
		if storage == nil || claimRegion.owner.Kind < storage.owner.Kind {
			continue
		}
		if !ledger.addClaimLocked(object, claimRegion) {
			continue
		}
		ledger.promoteStorageLocked(object, claimRegion)
		ledger.stats.RetainOperations++
		ledger.stats.BarrierRetains++
		ledger.recordLocked(Event{
			Kind:       ObjectBarrierRetained,
			Object:     object.id,
			Region:     object.region,
			Target:     parent,
			Owner:      claimRegion.owner,
			References: referenceCount(object),
		})
	}
	return nil
}

func (ledger *Ledger) publishSetLocked(objects []ObjectID, fromRegion, toRegion *regionRecord) {
	for _, objectID := range objects {
		object := ledger.objects[objectID]
		added := ledger.addClaimLocked(object, toRegion)
		if added {
			ledger.promoteStorageLocked(object, toRegion)
			ledger.stats.RetainOperations++
		}
		ledger.recordLocked(Event{
			Kind:       ObjectPublished,
			Object:     object.id,
			Region:     object.region,
			From:       fromRegion.owner,
			To:         toRegion.owner,
			References: referenceCount(object),
		})
	}
}

func (ledger *Ledger) transferSetLocked(objects []ObjectID, fromRegion, toRegion *regionRecord) {
	if fromRegion.id == toRegion.id {
		return
	}
	for _, objectID := range objects {
		object := ledger.objects[objectID]
		ledger.removeClaimLocked(object, fromRegion)
		ledger.addClaimLocked(object, toRegion)
		ledger.promoteStorageLocked(object, toRegion)
		ledger.stats.TransferOperations++
		ledger.recordLocked(Event{
			Kind:       ObjectTransferred,
			Object:     object.id,
			Region:     object.region,
			From:       fromRegion.owner,
			To:         toRegion.owner,
			References: referenceCount(object),
		})
		if referenceCount(object) == 0 {
			ledger.destroyLocked(object)
		}
	}
}

func (ledger *Ledger) addClaimLocked(object *objectRecord, region *regionRecord) bool {
	if _, exists := object.claims[region.id]; exists {
		return false
	}
	object.claims[region.id] = struct{}{}
	region.claims[object.id] = struct{}{}
	return true
}

func (ledger *Ledger) removeClaimLocked(object *objectRecord, region *regionRecord) bool {
	if _, exists := object.claims[region.id]; !exists {
		return false
	}
	delete(object.claims, region.id)
	delete(region.claims, object.id)
	return true
}

func (ledger *Ledger) releaseClaimLocked(object *objectRecord, region *regionRecord) {
	if !ledger.removeClaimLocked(object, region) {
		return
	}
	ledger.stats.ReleaseOperations++
	ledger.recordLocked(Event{
		Kind:       ObjectReleased,
		Object:     object.id,
		Region:     object.region,
		Owner:      region.owner,
		References: referenceCount(object),
	})
	if referenceCount(object) == 0 {
		ledger.destroyLocked(object)
	}
}

func (ledger *Ledger) reachableLocked(roots []ObjectID) ([]ObjectID, error) {
	queue := uniqueObjectIDs(roots)
	visited := make(map[ObjectID]struct{}, len(queue))
	result := make([]ObjectID, 0, len(queue))
	for len(queue) != 0 {
		objectID := queue[0]
		queue = queue[1:]
		if _, seen := visited[objectID]; seen {
			continue
		}
		object, err := ledger.liveObjectLocked(objectID)
		if err != nil {
			return nil, err
		}
		visited[objectID] = struct{}{}
		result = append(result, objectID)
		for _, child := range sortedObjectIDs(object.edges) {
			if _, seen := visited[child]; !seen {
				queue = append(queue, child)
			}
		}
	}
	sort.Slice(result, func(left, right int) bool { return result[left] < result[right] })
	return result, nil
}

func (ledger *Ledger) requireLiveLocked(objects []ObjectID) error {
	for _, objectID := range objects {
		if _, err := ledger.liveObjectLocked(objectID); err != nil {
			return err
		}
	}
	return nil
}

func (ledger *Ledger) requireClaimsLocked(objects []ObjectID, regionID RegionID) error {
	for _, objectID := range objects {
		object := ledger.objects[objectID]
		if _, claimed := object.claims[regionID]; !claimed {
			region := ledger.regions[regionID]
			return fmt.Errorf("%w: object %d by %s", ErrNotOwned, objectID, region.owner)
		}
	}
	return nil
}

func (ledger *Ledger) activeRegionLocked(regionID RegionID) (*regionRecord, error) {
	region := ledger.regions[regionID]
	if region == nil {
		return nil, fmt.Errorf("%w: %d", ErrUnknownRegion, regionID)
	}
	if region.closed {
		return nil, fmt.Errorf("%w: %d", ErrRegionClosed, regionID)
	}
	return region, nil
}

func (ledger *Ledger) ownerRegionLocked(owner OwnerID) (*regionRecord, error) {
	regionID, exists := ledger.ownerRegions[owner]
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrOwnerNotRegistered, owner)
	}
	region := ledger.regions[regionID]
	if region == nil {
		return nil, fmt.Errorf("%w: %d", ErrUnknownRegion, regionID)
	}
	return region, nil
}

func (ledger *Ledger) activeOwnerRegionLocked(owner OwnerID) (*regionRecord, error) {
	region, err := ledger.ownerRegionLocked(owner)
	if err != nil {
		return nil, err
	}
	if region.closed {
		return nil, fmt.Errorf("%w: %d", ErrRegionClosed, region.id)
	}
	return region, nil
}

func (ledger *Ledger) liveObjectLocked(objectID ObjectID) (*objectRecord, error) {
	object := ledger.objects[objectID]
	if object == nil {
		if _, destroyed := ledger.destroyed[objectID]; destroyed {
			return nil, fmt.Errorf("%w: %d", ErrObjectDestroyed, objectID)
		}
		return nil, fmt.Errorf("%w: %d", ErrUnknownObject, objectID)
	}
	if !object.alive {
		return nil, fmt.Errorf("%w: %d", ErrObjectDestroyed, objectID)
	}
	return object, nil
}

func (ledger *Ledger) promoteStorageLocked(object *objectRecord, target *regionRecord) {
	current := ledger.regions[object.region]
	if target == nil || current == nil || target.owner.Kind <= current.owner.Kind {
		return
	}
	delete(current.objects, object.id)
	target.objects[object.id] = struct{}{}
	object.region = target.id
}

func (ledger *Ledger) moveToLongestLivedClaimLocked(object *objectRecord) {
	var target *regionRecord
	for regionID := range object.claims {
		candidate := ledger.regions[regionID]
		if candidate == nil || candidate.closed {
			continue
		}
		if target == nil || candidate.owner.Kind > target.owner.Kind ||
			(candidate.owner.Kind == target.owner.Kind && candidate.id < target.id) {
			target = candidate
		}
	}
	if target == nil {
		ledger.destroyLocked(object)
		return
	}
	if current := ledger.regions[object.region]; current != nil {
		delete(current.objects, object.id)
	}
	target.objects[object.id] = struct{}{}
	object.region = target.id
}

func (ledger *Ledger) destroyLocked(object *objectRecord) {
	if !object.alive {
		return
	}
	if region := ledger.regions[object.region]; region != nil {
		delete(region.objects, object.id)
	}
	for regionID := range object.claims {
		if region := ledger.regions[regionID]; region != nil {
			delete(region.claims, object.id)
		}
	}
	object.claims = nil
	for targetID := range object.edges {
		if target := ledger.objects[targetID]; target != nil {
			delete(target.incoming, object.id)
		}
	}
	for sourceID := range object.incoming {
		if source := ledger.objects[sourceID]; source != nil {
			delete(source.edges, object.id)
		}
	}
	object.edges = nil
	object.incoming = nil
	object.alive = false
	ledger.stats.ObjectsDestroyed++
	ledger.stats.LiveObjects--
	ledger.recordLocked(Event{
		Kind:       ObjectDestroyed,
		Object:     object.id,
		Region:     object.region,
		References: 0,
	})
	ledger.destroyed[object.id] = object.region
	delete(ledger.objects, object.id)
}

func (ledger *Ledger) recordLocked(event Event) {
	ledger.nextEvent++
	event.Sequence = ledger.nextEvent
	ledger.stats.EventsRecorded++
	if ledger.eventLimit == 0 {
		ledger.stats.EventsDropped++
		return
	}
	event.At = time.Now()
	if ledger.eventLimit < 0 || len(ledger.events) < ledger.eventLimit {
		ledger.events = append(ledger.events, event)
		ledger.stats.RetainedEvents = len(ledger.events)
		return
	}
	ledger.events[ledger.eventStart] = event
	ledger.eventStart = (ledger.eventStart + 1) % len(ledger.events)
	ledger.stats.EventsDropped++
}

func referenceCount(object *objectRecord) int {
	return len(object.claims)
}

func uniqueObjectIDs(objects []ObjectID) []ObjectID {
	unique := make(map[ObjectID]struct{}, len(objects))
	for _, object := range objects {
		unique[object] = struct{}{}
	}
	return sortedObjectIDs(unique)
}

func sortedObjectIDs[T ~struct{}](objects map[ObjectID]T) []ObjectID {
	result := make([]ObjectID, 0, len(objects))
	for object := range objects {
		result = append(result, object)
	}
	sort.Slice(result, func(left, right int) bool { return result[left] < result[right] })
	return result
}

func sortedRegionIDs(regions map[RegionID]struct{}) []RegionID {
	result := make([]RegionID, 0, len(regions))
	for region := range regions {
		result = append(result, region)
	}
	sort.Slice(result, func(left, right int) bool { return result[left] < result[right] })
	return result
}
