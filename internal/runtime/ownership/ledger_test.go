package ownership

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"unsafe"
)

func TestTaskQueueTaskOwnershipLifecycle(t *testing.T) {
	t.Parallel()

	ledger := NewLedger()
	firstTask := OwnerID{Kind: OwnerTask, Value: 1}
	queue := OwnerID{Kind: OwnerQueue, Value: 1}
	secondTask := OwnerID{Kind: OwnerTask, Value: 2}
	firstRegion := mustCreateRegion(t, ledger, firstTask)
	queueRegion := mustCreateRegion(t, ledger, queue)
	secondRegion := mustCreateRegion(t, ledger, secondTask)
	object, err := ledger.CreateObject(firstRegion)
	if err != nil {
		t.Fatal(err)
	}

	if err := ledger.Publish(object, firstTask, queue); err != nil {
		t.Fatal(err)
	}
	assertObject(t, ledger, object, true, queueRegion, map[OwnerID]int{firstTask: 1, queue: 1})
	if err := ledger.CloseRegion(firstRegion); err != nil {
		t.Fatal(err)
	}
	assertObject(t, ledger, object, true, queueRegion, map[OwnerID]int{queue: 1})

	if err := ledger.Transfer(object, queue, secondTask); err != nil {
		t.Fatal(err)
	}
	assertObject(t, ledger, object, true, queueRegion, map[OwnerID]int{secondTask: 1})
	if err := ledger.CloseRegion(secondRegion); err != nil {
		t.Fatal(err)
	}
	assertObject(t, ledger, object, false, queueRegion, map[OwnerID]int{})

	stats := ledger.Stats()
	if stats.TaskLocalAllocations != 1 || stats.BulkRegionReleases != 2 || stats.ObjectsDestroyed != 1 || stats.LiveObjects != 0 {
		t.Fatalf("Stats() = %#v", stats)
	}
	if stats.PublishOperations != 1 || stats.TransferOperations != 1 || stats.RetainOperations != 1 || stats.ReleaseOperations != 2 {
		t.Fatalf("ownership operation stats = %#v", stats)
	}

	wantKinds := []EventKind{
		ObjectCreated,
		ObjectPublished,
		ObjectReleased,
		ObjectTransferred,
		ObjectReleased,
		ObjectDestroyed,
	}
	var gotKinds []EventKind
	for _, event := range ledger.Events() {
		if event.Object == object {
			gotKinds = append(gotKinds, event.Kind)
		}
	}
	if len(gotKinds) != len(wantKinds) {
		t.Fatalf("object event kinds = %#v, want %#v", gotKinds, wantKinds)
	}
	for index := range wantKinds {
		if gotKinds[index] != wantKinds[index] {
			t.Fatalf("object event kinds = %#v, want %#v", gotKinds, wantKinds)
		}
	}
}

func TestDestroyedObjectReleasesAdjacencyStorage(t *testing.T) {
	t.Parallel()

	ledger := NewLedger()
	owner := OwnerID{Kind: OwnerTask, Value: 99}
	region := mustCreateRegion(t, ledger, owner)
	root, err := ledger.CreateObject(region)
	if err != nil {
		t.Fatal(err)
	}
	target, err := ledger.CreateObject(region)
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.AddReference(root, target); err != nil {
		t.Fatal(err)
	}
	if err := ledger.CloseRegion(region); err != nil {
		t.Fatal(err)
	}
	for _, id := range []ObjectID{root, target} {
		if record := ledger.objects[id]; record != nil || ledger.destroyed[id] != region {
			t.Fatalf("destroyed object %d retained record %#v or lost tombstone %d", id, record, ledger.destroyed[id])
		}
	}
}

func TestTelemetryFormatsLifecycleAndSummary(t *testing.T) {
	t.Parallel()

	ledger := NewLedger()
	task := OwnerID{Kind: OwnerTask, Value: 1}
	region := mustCreateRegion(t, ledger, task)
	object, err := ledger.CreateObject(region)
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.CloseRegion(region); err != nil {
		t.Fatal(err)
	}

	var trace bytes.Buffer
	if err := ledger.WriteTrace(&trace); err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		"[ownership] create #" + objectString(object) + " owner=task:1",
		"[ownership] release #" + objectString(object) + " owner=task:1 refs=0",
		"[ownership] object #" + objectString(object) + " destroyed",
	} {
		if !strings.Contains(trace.String(), fragment) {
			t.Errorf("trace missing %q:\n%s", fragment, trace.String())
		}
	}

	var summary bytes.Buffer
	if err := ledger.WriteSummary(&summary); err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"Gossamer Memory", "Task-local allocations", "Bulk-region releases", "Live objects"} {
		if !strings.Contains(summary.String(), fragment) {
			t.Errorf("summary missing %q:\n%s", fragment, summary.String())
		}
	}
}

func TestUnpublishedTaskObjectsDieInOneRegionRelease(t *testing.T) {
	t.Parallel()

	ledger := NewLedger()
	task := OwnerID{Kind: OwnerTask, Value: 17}
	region := mustCreateRegion(t, ledger, task)
	objects := make([]ObjectID, 3)
	for index := range objects {
		object, err := ledger.CreateObject(region)
		if err != nil {
			t.Fatal(err)
		}
		objects[index] = object
	}
	if err := ledger.CloseRegion(region); err != nil {
		t.Fatal(err)
	}
	for _, object := range objects {
		assertObject(t, ledger, object, false, region, map[OwnerID]int{})
	}
	if stats := ledger.Stats(); stats.BulkRegionReleases != 1 || stats.ObjectsDestroyed != 3 {
		t.Fatalf("Stats() = %#v", stats)
	}
}

func TestRegionClaimsIgnoreLocalAliasesAndDuplicatePublication(t *testing.T) {
	t.Parallel()

	ledger := NewLedger()
	task := OwnerID{Kind: OwnerTask, Value: 1}
	queue := OwnerID{Kind: OwnerQueue, Value: 1}
	taskRegion := mustCreateRegion(t, ledger, task)
	mustCreateRegion(t, ledger, queue)
	object, err := ledger.CreateObject(taskRegion)
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.Publish(object, task, queue); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Publish(object, task, queue); err != nil {
		t.Fatal(err)
	}

	snapshot, err := ledger.Object(object)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.References != 2 || len(snapshot.Claims) != 2 {
		t.Fatalf("duplicate publication claims = %#v, want one task and one queue claim", snapshot)
	}
	stats := ledger.Stats()
	if stats.PublishOperations != 2 || stats.RetainOperations != 1 {
		t.Fatalf("duplicate publication stats = %#v", stats)
	}
}

func TestLocalObjectGraphEdgesDoNotChangeARC(t *testing.T) {
	t.Parallel()

	ledger := NewLedger()
	task := OwnerID{Kind: OwnerTask, Value: 1}
	region := mustCreateRegion(t, ledger, task)
	parent, err := ledger.CreateObject(region)
	if err != nil {
		t.Fatal(err)
	}
	child, err := ledger.CreateObject(region)
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.AddReference(parent, child); err != nil {
		t.Fatal(err)
	}
	if err := ledger.AddReference(parent, child); err != nil {
		t.Fatal(err)
	}

	assertObject(t, ledger, parent, true, region, map[OwnerID]int{task: 1})
	childSnapshot, err := ledger.Object(child)
	if err != nil {
		t.Fatal(err)
	}
	if childSnapshot.References != 1 || len(childSnapshot.Claims) != 1 {
		t.Fatalf("child after local aliases = %#v", childSnapshot)
	}
	stats := ledger.Stats()
	if stats.LocalReferences != 1 || stats.RetainOperations != 0 || stats.BarrierRetains != 0 {
		t.Fatalf("local reference stats = %#v", stats)
	}
}

func TestSameRegionMutualReferencesDoNotRetainObjects(t *testing.T) {
	t.Parallel()

	ledger := NewLedger()
	task := OwnerID{Kind: OwnerTask, Value: 1}
	region := mustCreateRegion(t, ledger, task)
	first, err := ledger.CreateObject(region)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ledger.CreateObject(region)
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.AddReference(first, second); err != nil {
		t.Fatal(err)
	}
	if err := ledger.AddReference(second, first); err != nil {
		t.Fatal(err)
	}

	for _, object := range []ObjectID{first, second} {
		assertObject(t, ledger, object, true, region, map[OwnerID]int{task: 1})
	}
	if stats := ledger.Stats(); stats.RetainOperations != 0 || stats.BarrierRetains != 0 {
		t.Fatalf("same-region cycle generated ARC operations: %#v", stats)
	}

	if err := ledger.CloseRegion(region); err != nil {
		t.Fatal(err)
	}
	for _, object := range []ObjectID{first, second} {
		assertObject(t, ledger, object, false, region, map[OwnerID]int{})
	}
	if stats := ledger.Stats(); stats.ObjectsDestroyed != 2 || stats.LiveObjects != 0 {
		t.Fatalf("same-region cycle survived region release: %#v", stats)
	}
}

func TestDestroyUnlinksOnlyAdjacentObjectEdges(t *testing.T) {
	t.Parallel()

	ledger := NewLedger()
	wrapper := OwnerID{Kind: OwnerWrapper, Value: 1}
	document := OwnerID{Kind: OwnerDocument, Value: 1}
	wrapperRegion := mustCreateRegion(t, ledger, wrapper)
	documentRegion := mustCreateRegion(t, ledger, document)
	shorter, err := ledger.CreateObject(wrapperRegion)
	if err != nil {
		t.Fatal(err)
	}
	longer, err := ledger.CreateObject(documentRegion)
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.AddReference(shorter, longer); err != nil {
		t.Fatal(err)
	}
	if ledger.objects[longer].incoming == 0 || ledger.edges.record(ledger.objects[longer].incoming).from != shorter {
		t.Fatal("incoming edge index does not contain source")
	}

	if err := ledger.CloseRegion(documentRegion); err != nil {
		t.Fatal(err)
	}
	shorterSnapshot, err := ledger.Object(shorter)
	if err != nil {
		t.Fatal(err)
	}
	if len(shorterSnapshot.Edges) != 0 {
		t.Fatalf("destroyed target remained reachable from source: %#v", shorterSnapshot)
	}
	if _, exists := ledger.objects[longer]; exists || ledger.destroyed[longer] != documentRegion {
		t.Fatalf("destroyed target retained its full object record: object=%#v tombstone=%d", ledger.objects[longer], ledger.destroyed[longer])
	}
}

func TestLongerLivedWriteBarrierRetainsReachableSubgraph(t *testing.T) {
	t.Parallel()

	ledger := NewLedger()
	realm := OwnerID{Kind: OwnerRealm, Value: 1}
	task := OwnerID{Kind: OwnerTask, Value: 1}
	realmRegion := mustCreateRegion(t, ledger, realm)
	taskRegion := mustCreateRegion(t, ledger, task)
	parent, err := ledger.CreateObject(realmRegion)
	if err != nil {
		t.Fatal(err)
	}
	child, err := ledger.CreateObject(taskRegion)
	if err != nil {
		t.Fatal(err)
	}
	grandchild, err := ledger.CreateObject(taskRegion)
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.AddReference(child, grandchild); err != nil {
		t.Fatal(err)
	}
	if err := ledger.AddReference(parent, child); err != nil {
		t.Fatal(err)
	}

	for _, object := range []ObjectID{child, grandchild} {
		assertObject(t, ledger, object, true, realmRegion, map[OwnerID]int{task: 1, realm: 1})
	}
	if stats := ledger.Stats(); stats.BarrierRetains != 2 || stats.RetainOperations != 2 {
		t.Fatalf("barrier stats = %#v", stats)
	}
	if err := ledger.CloseRegion(taskRegion); err != nil {
		t.Fatal(err)
	}
	for _, object := range []ObjectID{parent, child, grandchild} {
		assertObject(t, ledger, object, true, realmRegion, map[OwnerID]int{realm: 1})
	}
	if err := ledger.CloseRegion(realmRegion); err != nil {
		t.Fatal(err)
	}
	for _, object := range []ObjectID{parent, child, grandchild} {
		snapshot, err := ledger.Object(object)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.Alive {
			t.Fatalf("object %d survived realm release: %#v", object, snapshot)
		}
	}
}

func TestLedgerBoundsEventsAndCompactsDestroyedObjects(t *testing.T) {
	ledger := NewLedgerWithEventLimit(3)
	owner := OwnerID{Kind: OwnerTask, Value: 404}
	region := mustCreateRegion(t, ledger, owner)
	object, err := ledger.CreateObject(region)
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.Release(object, owner); err != nil {
		t.Fatal(err)
	}
	snapshot, err := ledger.Object(object)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Alive || snapshot.Region != region || snapshot.References != 0 {
		t.Fatalf("destroyed object tombstone = %#v", snapshot)
	}
	if _, err := ledger.liveObjectLocked(object); !errors.Is(err, ErrObjectDestroyed) {
		t.Fatalf("live destroyed object error = %v", err)
	}
	events := ledger.Events()
	if len(events) != 3 || events[0].Sequence+1 != events[1].Sequence || events[1].Sequence+1 != events[2].Sequence {
		t.Fatalf("bounded events = %#v", events)
	}
	stats := ledger.Stats()
	if stats.EventsRecorded != 4 || stats.EventsDropped != 1 || stats.RetainedEvents != 3 {
		t.Fatalf("event retention stats = %#v", stats)
	}
}

func TestLedgerCompactsCommonClaimsAndAllocatesEdgesLazily(t *testing.T) {
	ledger := NewLedgerWithEventLimit(0)
	task := OwnerID{Kind: OwnerTask, Value: 1}
	realm := OwnerID{Kind: OwnerRealm, Value: 1}
	taskRegion := mustCreateRegion(t, ledger, task)
	mustCreateRegion(t, ledger, realm)
	object, err := ledger.CreateObject(taskRegion)
	if err != nil {
		t.Fatal(err)
	}
	record := ledger.objects[object]
	if record.claim != taskRegion || record.claims != nil || record.outgoing != 0 || record.incoming != 0 || record.edgeLookup != nil {
		t.Fatalf("fresh object record is not compact: %#v", record)
	}
	if err := ledger.Publish(object, task, realm); err != nil {
		t.Fatal(err)
	}
	if record.claim != 0 || len(record.claims) != 2 {
		t.Fatalf("published claims = inline %d overflow %#v", record.claim, record.claims)
	}
	if err := ledger.Release(object, task); err != nil {
		t.Fatal(err)
	}
	if record.claim == 0 || record.claims != nil || referenceCount(record) != 1 {
		t.Fatalf("claims did not collapse inline: inline %d overflow %#v", record.claim, record.claims)
	}
}

func TestLedgerBoundsDestroyedTombstonesWithoutRevivingOldIDs(t *testing.T) {
	ledger := NewLedgerWithEventLimit(0)
	owner := OwnerID{Kind: OwnerTask, Value: 1}
	region := mustCreateRegion(t, ledger, owner)
	var first, last ObjectID
	for index := 0; index < DefaultTombstoneLimit+1; index++ {
		object, err := ledger.CreateObject(region)
		if err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			first = object
		}
		last = object
		if err := ledger.Release(object, owner); err != nil {
			t.Fatal(err)
		}
	}
	if len(ledger.destroyed) != DefaultTombstoneLimit || len(ledger.destroyedIDs) != DefaultTombstoneLimit {
		t.Fatalf("tombstone retention = map %d ring %d", len(ledger.destroyed), len(ledger.destroyedIDs))
	}
	if _, retained := ledger.destroyed[first]; retained {
		t.Fatalf("oldest tombstone %d was not evicted", first)
	}
	if snapshot, err := ledger.Object(first); err != nil || snapshot.Alive || snapshot.ID != first {
		t.Fatalf("expired tombstone resolved incorrectly: snapshot=%#v err=%v", snapshot, err)
	}
	if _, err := ledger.liveObjectLocked(first); !errors.Is(err, ErrObjectDestroyed) {
		t.Fatalf("expired tombstone became usable: %v", err)
	}
	if snapshot, err := ledger.Object(last); err != nil || snapshot.Alive || snapshot.Region != region {
		t.Fatalf("recent tombstone = %#v err=%v", snapshot, err)
	}
}

func TestPublicationRetainsCyclesOncePerDestinationRegion(t *testing.T) {
	t.Parallel()

	ledger := NewLedger()
	task := OwnerID{Kind: OwnerTask, Value: 1}
	queue := OwnerID{Kind: OwnerQueue, Value: 1}
	taskRegion := mustCreateRegion(t, ledger, task)
	queueRegion := mustCreateRegion(t, ledger, queue)
	first, err := ledger.CreateObject(taskRegion)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ledger.CreateObject(taskRegion)
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.AddReference(first, second); err != nil {
		t.Fatal(err)
	}
	if err := ledger.AddReference(second, first); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Publish(first, task, queue); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Publish(second, task, queue); err != nil {
		t.Fatal(err)
	}

	for _, object := range []ObjectID{first, second} {
		assertObject(t, ledger, object, true, queueRegion, map[OwnerID]int{task: 1, queue: 1})
	}
	if stats := ledger.Stats(); stats.RetainOperations != 2 {
		t.Fatalf("cycle RetainOperations = %d, want 2", stats.RetainOperations)
	}
}

func TestPublishPromotesOnlyTowardLongerLivedRegions(t *testing.T) {
	t.Parallel()

	ledger := NewLedger()
	task := OwnerID{Kind: OwnerTask, Value: 1}
	queue := OwnerID{Kind: OwnerQueue, Value: 1}
	realm := OwnerID{Kind: OwnerRealm, Value: 1}
	taskRegion := mustCreateRegion(t, ledger, task)
	queueRegion := mustCreateRegion(t, ledger, queue)
	realmRegion := mustCreateRegion(t, ledger, realm)
	object, err := ledger.CreateObject(taskRegion)
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.Publish(object, task, queue); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Publish(object, queue, realm); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Transfer(object, realm, task); err != nil {
		t.Fatal(err)
	}

	snapshot, err := ledger.Object(object)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Region != realmRegion {
		t.Fatalf("object region = %d, want realm region %d (queue region was %d)", snapshot.Region, realmRegion, queueRegion)
	}
	if stats := ledger.Stats(); stats.PersistentObjects != 1 {
		t.Fatalf("Stats().PersistentObjects = %d, want 1", stats.PersistentObjects)
	}
}

func TestCloseRegionRelocatesStorageAndRetiresOwnerMetadata(t *testing.T) {
	ledger := NewLedgerWithEventLimit(0)
	realm := OwnerID{Kind: OwnerRealm, Value: 41}
	task := OwnerID{Kind: OwnerTask, Value: 41}
	realmRegion := mustCreateRegion(t, ledger, realm)
	taskRegion := mustCreateRegion(t, ledger, task)
	object, err := ledger.CreateObject(realmRegion)
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.Transfer(object, realm, task); err != nil {
		t.Fatal(err)
	}
	if err := ledger.CloseRegion(realmRegion); err != nil {
		t.Fatal(err)
	}
	snapshot, err := ledger.Object(object)
	if err != nil || !snapshot.Alive || snapshot.Region != taskRegion {
		t.Fatalf("relocated object = %#v, %v", snapshot, err)
	}
	if _, exists := ledger.regions[realmRegion]; exists {
		t.Fatal("closed realm region was retained")
	}
	if _, exists := ledger.ownerRegions[realm]; exists {
		t.Fatal("closed realm owner was retained")
	}
	if _, err := ledger.CreateObject(realmRegion); !errors.Is(err, ErrRegionClosed) {
		t.Fatalf("CreateObject(closed region) = %v, want ErrRegionClosed", err)
	}
	if err := ledger.CloseRegion(realmRegion); err != nil {
		t.Fatalf("second CloseRegion() = %v", err)
	}
	if stats := ledger.Stats(); stats.PersistentObjects != 0 {
		t.Fatalf("PersistentObjects after relocation = %d", stats.PersistentObjects)
	}
	if err := ledger.CloseRegion(taskRegion); err != nil {
		t.Fatal(err)
	}
	if len(ledger.regions) != 0 || len(ledger.ownerRegions) != 0 {
		t.Fatalf("ledger retained regions=%d owners=%d", len(ledger.regions), len(ledger.ownerRegions))
	}
	snapshot, err = ledger.Object(object)
	if err != nil || snapshot.Alive || snapshot.Region != taskRegion {
		t.Fatalf("destroyed object snapshot = %#v, %v", snapshot, err)
	}
}

func TestEdgeArenaPhysicalStatsReleaseWholeSlabs(t *testing.T) {
	ledger := NewLedgerWithEventLimit(0)
	owner := OwnerID{Kind: OwnerRealm, Value: 88_001}
	region, err := ledger.CreateRegion(owner)
	if err != nil {
		t.Fatal(err)
	}
	hub, err := ledger.CreateObject(region)
	if err != nil {
		t.Fatal(err)
	}
	const edges = 257
	targets := make([]ObjectID, edges)
	for index := range targets {
		targets[index], err = ledger.CreateObject(region)
		if err != nil {
			t.Fatal(err)
		}
		if err := ledger.AddReference(hub, targets[index]); err != nil {
			t.Fatal(err)
		}
	}

	twoSlabs := ledger.PhysicalStats()
	if twoSlabs.LiveEdgeEntries != edges || twoSlabs.EdgeArenaSlabs != 2 ||
		twoSlabs.EdgeArenaOccupiedBytes != edges*twoSlabs.EdgeRecordSizeBytes ||
		twoSlabs.EdgeArenaReservedBytes <= twoSlabs.EdgeArenaOccupiedBytes {
		t.Fatalf("two-slab physical stats = %#v", twoSlabs)
	}
	for _, target := range targets[:256] {
		if err := ledger.RemoveReference(hub, target); err != nil {
			t.Fatal(err)
		}
	}
	oneSlab := ledger.PhysicalStats()
	if oneSlab.LiveEdgeEntries != 1 || oneSlab.EdgeArenaSlabs != 1 ||
		oneSlab.EdgeArenaReservedBytes >= twoSlabs.EdgeArenaReservedBytes {
		t.Fatalf("one-slab physical stats = %#v; before=%#v", oneSlab, twoSlabs)
	}
	if err := ledger.RemoveReference(hub, targets[256]); err != nil {
		t.Fatal(err)
	}
	if empty := ledger.PhysicalStats(); empty.LiveEdgeEntries != 0 || empty.EdgeArenaSlabs != 0 ||
		empty.EdgeArenaOccupiedBytes != 0 || empty.EdgeArenaReservedBytes != 0 {
		t.Fatalf("empty edge arena physical stats = %#v", empty)
	}
}

func TestMonotonicIDsFailBeforeOverflow(t *testing.T) {
	ledger := NewLedger()
	ledger.nextRegion = RegionID(math.MaxUint64)
	if _, err := ledger.CreateRegion(OwnerID{Kind: OwnerRealm, Value: 89_001}); err == nil || !strings.Contains(err.Error(), "exhausted region IDs") {
		t.Fatalf("CreateRegion() at ID exhaustion = %v", err)
	}
	if len(ledger.regions) != 0 || len(ledger.ownerRegions) != 0 {
		t.Fatal("exhausted CreateRegion mutated ledger metadata")
	}

	ledger = NewLedger()
	region := mustCreateRegion(t, ledger, OwnerID{Kind: OwnerRealm, Value: 89_002})
	ledger.nextObject = ObjectID(math.MaxUint64)
	if _, err := ledger.CreateObject(region); err == nil || !strings.Contains(err.Error(), "exhausted object IDs") {
		t.Fatalf("CreateObject() at ID exhaustion = %v", err)
	}
	if len(ledger.objects) != 0 || len(ledger.regions[region].objects) != 0 || len(ledger.regions[region].claims) != 0 {
		t.Fatal("exhausted CreateObject mutated ledger metadata")
	}
}

func TestOwnershipOperationsRejectInvalidTransitionsAtomically(t *testing.T) {
	t.Parallel()

	ledger := NewLedger()
	first := OwnerID{Kind: OwnerTask, Value: 1}
	second := OwnerID{Kind: OwnerTask, Value: 2}
	queue := OwnerID{Kind: OwnerQueue, Value: 1}
	firstRegion := mustCreateRegion(t, ledger, first)
	mustCreateRegion(t, ledger, second)
	mustCreateRegion(t, ledger, queue)
	firstObject, err := ledger.CreateObject(firstRegion)
	if err != nil {
		t.Fatal(err)
	}
	secondObject, err := ledger.CreateObject(firstRegion)
	if err != nil {
		t.Fatal(err)
	}

	if err := ledger.TransferAll([]ObjectID{firstObject, secondObject}, second, queue); !errors.Is(err, ErrNotOwned) {
		t.Fatalf("TransferAll() error = %v, want ErrNotOwned", err)
	}
	assertObject(t, ledger, firstObject, true, firstRegion, map[OwnerID]int{first: 1})
	assertObject(t, ledger, secondObject, true, firstRegion, map[OwnerID]int{first: 1})
}

func TestReconcileRegionCollectsUnreachableConstructionGraph(t *testing.T) {
	ledger := NewLedger()
	wrapper := OwnerID{Kind: OwnerWrapper, Value: 1}
	task := OwnerID{Kind: OwnerTask, Value: 1}
	wrapperRegion := mustCreateRegion(t, ledger, wrapper)
	taskRegion := mustCreateRegion(t, ledger, task)
	root, err := ledger.CreateObject(wrapperRegion)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := ledger.CreateObject(taskRegion)
	if err != nil {
		t.Fatal(err)
	}
	child, err := ledger.CreateObject(taskRegion)
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.AddReference(parent, child); err != nil {
		t.Fatal(err)
	}
	if err := ledger.AddReference(root, parent); err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.ReconcileRegion(wrapper, []ObjectID{root}); err != nil {
		t.Fatal(err)
	}
	if err := ledger.CloseRegion(taskRegion); err != nil {
		t.Fatal(err)
	}
	assertObject(t, ledger, parent, true, wrapperRegion, map[OwnerID]int{wrapper: 1})
	assertObject(t, ledger, child, true, wrapperRegion, map[OwnerID]int{wrapper: 1})

	if err := ledger.RemoveReference(root, parent); err != nil {
		t.Fatal(err)
	}
	destroyed, err := ledger.ReconcileRegion(wrapper, []ObjectID{root})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(destroyed, []ObjectID{parent, child}) {
		t.Fatalf("destroyed = %v, want [%d %d]", destroyed, parent, child)
	}
	assertObject(t, ledger, parent, false, wrapperRegion, map[OwnerID]int{})
	assertObject(t, ledger, child, false, wrapperRegion, map[OwnerID]int{})
}

func TestReconcileShorterRegionClaimsLongerLivedRoot(t *testing.T) {
	ledger := NewLedger()
	wrapper := OwnerID{Kind: OwnerWrapper, Value: 1}
	document := OwnerID{Kind: OwnerDocument, Value: 1}
	wrapperRegion := mustCreateRegion(t, ledger, wrapper)
	documentRegion := mustCreateRegion(t, ledger, document)
	root, err := ledger.CreateObject(wrapperRegion)
	if err != nil {
		t.Fatal(err)
	}
	node, err := ledger.CreateObject(documentRegion)
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.AddReference(root, node); err != nil {
		t.Fatal(err)
	}
	before, err := ledger.Object(node)
	if err != nil {
		t.Fatal(err)
	}
	if before.Owners[wrapper] != 0 {
		t.Fatalf("shorter root unexpectedly retained by write barrier: %#v", before)
	}
	if _, err := ledger.ReconcileRegion(wrapper, []ObjectID{root}); err != nil {
		t.Fatal(err)
	}
	if err := ledger.CloseRegion(documentRegion); err != nil {
		t.Fatal(err)
	}
	assertObject(t, ledger, node, true, wrapperRegion, map[OwnerID]int{wrapper: 1})
}

func TestEdgeArenaIndexesOnlyHighDegreeObjectsAndReusesRecords(t *testing.T) {
	ledger := NewLedgerWithEventLimit(0)
	owner := OwnerID{Kind: OwnerTask, Value: 92}
	region := mustCreateRegion(t, ledger, owner)
	root, err := ledger.CreateObject(region)
	if err != nil {
		t.Fatal(err)
	}
	targets := make([]ObjectID, edgeLookupThreshold+4)
	for index := range targets {
		targets[index], err = ledger.CreateObject(region)
		if err != nil {
			t.Fatal(err)
		}
		if err := ledger.AddReference(root, targets[index]); err != nil {
			t.Fatal(err)
		}
	}
	if len(ledger.objects[root].edgeLookup) != len(targets) {
		t.Fatalf("high-degree lookup has %d entries, want %d", len(ledger.objects[root].edgeLookup), len(targets))
	}
	for index := 1; index < len(targets); index += 2 {
		if err := ledger.RemoveReference(root, targets[index]); err != nil {
			t.Fatal(err)
		}
	}
	want := make([]ObjectID, 0, (len(targets)+1)/2)
	for index := 0; index < len(targets); index += 2 {
		want = append(want, targets[index])
	}
	if ledger.objects[root].edgeLookup != nil {
		t.Fatalf("low-degree object retained high-degree lookup with %d entries", len(ledger.objects[root].edgeLookup))
	}
	if snapshot, err := ledger.Object(root); err != nil || !reflect.DeepEqual(snapshot.Edges, want) {
		t.Fatalf("remaining high-degree edges = %#v, want %v, err=%v", snapshot.Edges, want, err)
	}

	keeper, err := ledger.CreateObject(region)
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.AddReference(keeper, root); err != nil {
		t.Fatal(err)
	}
	source, err := ledger.CreateObject(region)
	if err != nil {
		t.Fatal(err)
	}
	target, err := ledger.CreateObject(region)
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.AddReference(source, target); err != nil {
		t.Fatal(err)
	}
	if err := ledger.RemoveReference(source, target); err != nil {
		t.Fatal(err)
	}
	if allocations := testing.AllocsPerRun(100, func() {
		if err := ledger.AddReference(source, target); err != nil {
			t.Fatal(err)
		}
		if err := ledger.RemoveReference(source, target); err != nil {
			t.Fatal(err)
		}
	}); allocations != 0 {
		t.Fatalf("reused low-degree edge allocated %.2f objects per cycle", allocations)
	}
	if unsafe.Sizeof(edgeRecord{}) > 32 {
		t.Fatalf("edge record grew to %d bytes", unsafe.Sizeof(edgeRecord{}))
	}
	if err := ledger.CloseRegion(region); err != nil {
		t.Fatal(err)
	}
	if ledger.edges.live != 0 || len(ledger.edges.slabs) != 0 {
		t.Fatalf("region release retained edge arena: live=%d slabs=%d", ledger.edges.live, len(ledger.edges.slabs))
	}
}

func TestEdgeArenaRandomizedAgainstReferenceModel(t *testing.T) {
	ledger := NewLedgerWithEventLimit(0)
	owner := OwnerID{Kind: OwnerTask, Value: 93}
	region := mustCreateRegion(t, ledger, owner)
	objects := make([]ObjectID, 64)
	for index := range objects {
		var err error
		objects[index], err = ledger.CreateObject(region)
		if err != nil {
			t.Fatal(err)
		}
	}
	type pair struct{ from, to ObjectID }
	want := make(map[pair]bool)
	random := rand.New(rand.NewSource(93))
	for operation := 0; operation < 10_000; operation++ {
		edge := pair{
			from: objects[random.Intn(len(objects))],
			to:   objects[random.Intn(len(objects))],
		}
		if !want[edge] || random.Intn(3) != 0 {
			if err := ledger.AddReference(edge.from, edge.to); err != nil {
				t.Fatal(err)
			}
			want[edge] = true
		} else {
			if err := ledger.RemoveReference(edge.from, edge.to); err != nil {
				t.Fatal(err)
			}
			delete(want, edge)
		}
	}
	if int(ledger.edges.live) != len(want) {
		t.Fatalf("arena has %d live edges, want %d", ledger.edges.live, len(want))
	}
	for _, object := range objects {
		targets := make([]ObjectID, 0)
		for edge := range want {
			if edge.from == object {
				targets = append(targets, edge.to)
			}
		}
		sortObjectIDs(targets)
		snapshot, err := ledger.Object(object)
		if err != nil || !reflect.DeepEqual(snapshot.Edges, targets) {
			t.Fatalf("Object(%d).Edges = %v, want %v, err=%v", object, snapshot.Edges, targets, err)
		}
	}
	if err := ledger.CloseRegion(region); err != nil {
		t.Fatal(err)
	}
	if ledger.edges.live != 0 || len(ledger.edges.slabs) != 0 {
		t.Fatalf("randomized teardown retained edge arena: live=%d slabs=%d", ledger.edges.live, len(ledger.edges.slabs))
	}
}

func mustCreateRegion(t *testing.T, ledger *Ledger, owner OwnerID) RegionID {
	t.Helper()
	region, err := ledger.CreateRegion(owner)
	if err != nil {
		t.Fatal(err)
	}
	return region
}

func assertObject(t *testing.T, ledger *Ledger, object ObjectID, alive bool, region RegionID, owners map[OwnerID]int) {
	t.Helper()
	snapshot, err := ledger.Object(object)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Alive != alive || snapshot.Region != region || snapshot.References != ownerReferenceCount(owners) {
		t.Fatalf("Object(%d) = %#v, want alive=%t region=%d owners=%#v", object, snapshot, alive, region, owners)
	}
	if len(snapshot.Owners) != len(owners) {
		t.Fatalf("Object(%d).Owners = %#v, want %#v", object, snapshot.Owners, owners)
	}
	for owner, count := range owners {
		if snapshot.Owners[owner] != count {
			t.Fatalf("Object(%d).Owners = %#v, want %#v", object, snapshot.Owners, owners)
		}
	}
}

func ownerReferenceCount(owners map[OwnerID]int) int {
	count := 0
	for _, references := range owners {
		count += references
	}
	return count
}

func objectString(object ObjectID) string {
	return fmt.Sprintf("%d", object)
}
