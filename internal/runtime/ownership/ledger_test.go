package ownership

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
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
	if _, exists := ledger.objects[longer].incoming[shorter]; !exists {
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
