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

The ownership ledger is a shadow model. It does not replace Go's allocator or
garbage collector. Fake runtime objects carry IDs and metadata so the kernel
can prove publication, promotion, transfer, and release rules before those
rules govern JavaScript values. A region has at most one ownership claim on an
object. Local aliases and graph edges do not increment ARC. Publication and
longer-lived write barriers retain each required reachable object once per
destination region, including cyclic graphs.

```text
RC(object) = number of claiming regions

claim(region, object) is either present or absent
```

## Implemented kernel slice

- `internal/runtime/ownership` models task, queue, realm, and browser owners.
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

The next implementation step is V8 itself, isolated behind the proven adapter
boundary documented in
[`pre-v8-engine-boundary.md`](pre-v8-engine-boundary.md).

Physical allocation optimizations such as typed slabs or object pools should
be evaluated later against the semantic telemetry. They are implementation
details, not the Phase 0 ownership model.
