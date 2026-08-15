# Phase 0: Gossamer kernel

Phase 0 proves Gossamer's execution and semantic lifetime model before a
JavaScript engine is introduced. V8 must plug into the browser kernel; it must
not define the browser's scheduling, ownership, DOM, or rendering boundaries.

## Architectural thesis

```text
one realm = one ordered logical executor

task-local object
    -> queue publication
    -> bulk task-region release
    -> queue-to-task transfer
    -> final task-region release
    -> destruction
```

Multiple realms may execute concurrently, but tasks within one realm are
strictly ordered. Microtasks drain after each task before the next task begins.
Task queues are explicit objects because enqueue and dequeue are semantic
ownership boundaries.

The ownership ledger remains the semantic claim model. A native `RegionStore`
now uses it to own synthetic Cells in real generation-checked slots. Physical
region identity is deliberately distinct from a movable ledger claim: a Ref
keeps the same `RegionID + Slot + Generation` while its region moves from a
task to a queue and then to the receiving task. Local aliases do not add a
claim. Cross-region Cell fields update a counted region graph as well as the
ledger's object graph.

```text
RC(object) = number of claiming regions

claim(region, object) is either present or absent
```

## Implemented kernel slice

- `internal/runtime/ownership` models task, queue, wrapper, document, realm,
  browser, and immutable shared owners.
- `internal/runtime/memory` owns synthetic Cells in explicit regions and slots.
- Freed slots advance their generation and exhausted generations are retired,
  so a stale Ref cannot become valid again.
- Every mutable physical region has exactly one owner. Published regions are
  immutable, while regions held by a queue cannot be dereferenced.
- Every cross-region field maintains a counted `from -> to` edge. Replacing or
  clearing the field decrements the count and removes zero-count edges.
- Destroying a referenced Cell or region fails before storage is reclaimed.
- Unqualified queue sends accept only published refs. Private refs require an
  explicit Transfer, Publish, or Copy operation.
- Transfer moves the complete connected private region component through the
  queue; Copy clones the reachable Cell graph; Publish makes the outgoing
  region graph immutable and shared.
- Every owner has a logical region.
- Task-local fake objects receive an initial task reference.
- Enqueue publishes carried objects to the destination queue.
- Dequeue transfers queue references to the receiving task.
- Closing a task region releases all task references in one semantic operation.
- Publication promotes region metadata only toward longer-lived owners.
- Queue handoff transfers exclusive claims and retains only graphs still shared
  with queued work.
- A deterministic event stream and summary counters expose every transition.
- `internal/runtime.Realm` prevents concurrent executors for one realm.
- `internal/runtime.Scheduler` can run independent realm actors concurrently.
- `dom.NodeID`, `dom.NodeStore`, and `dom.Document` provide stable identity over
  the existing pointer-backed DOM without changing renderer behavior.
- `internal/browser.Page` owns a Realm, indexed Document, URL, resources,
  invalidation state, and current Frame.
- A stable-ID text mutation can schedule a separate render task and carry a
  shadow invalidation object through the queue ownership lifecycle.
- Phase 1 navigation now routes document and resource completion through Page
  tasks, rejects stale generations, and stores renderer inputs by `NodeID`.
  See [`phase-1-navigation.md`](phase-1-navigation.md) for the exact sequence.

## Adoption status

Gossamer already has HTML loading, a pointer-based DOM, style calculation,
layout, and painting. Phase 0 should wrap and migrate those working components
instead of discarding them:

The navigation, input, timer, stable hit-test, engine-interface, fake-engine,
and click-to-paint stages are implemented. Initial inline and external classic
scripts also enter through Page evaluation tasks with an explicit engine
microtask checkpoint.

Stock V8 now occupies the proven adapter boundary for evaluation, explicit
microtasks, profiling, isolate teardown, numeric DOM wrappers, and synchronous
node mutation churn. Task-created nodes begin in construction regions; the
document and V8 wrapper cache maintain independent semantic root sets, and a
weak-wrapper sweep can reclaim a detached Go subgraph without reusing its
stable IDs. See
[`stock-v8-integration.md`](stock-v8-integration.md).

Physical allocation optimizations such as typed slabs or object pools should
be evaluated later against the semantic telemetry. They are implementation
details, not the Phase 0 ownership model.

The RegionStore intentionally does not implement JavaScript objects, tracing
garbage collection, automatic escape promotion, packed refs, or V8 heap
integration. Those remain later milestones.
