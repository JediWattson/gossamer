# Stock V8 integration

Gossamer embeds an unmodified upstream V8 build behind the engine-neutral
browser socket. The initial target is Apple Silicon and the initial purpose is
measurement: execute real JavaScript, preserve Gossamer's explicit scheduling,
observe V8's own heap, and establish the first numeric DOM-wrapper and callback
boundary without modifying V8.

## Ownership boundary

```text
                 Gossamer
                     |
        +------------+------------+
        |                         |
    JS objects                Browser objects
        |                         |
       V8                       Go
     V8 GC                Regions + queue ARC
        |                         |
        +-------- wrappers -------+
```

The heaps remain deliberately separate:

- V8 allocates and traces JavaScript values, closures, promises, and wrapper
  objects. V8 handles are never exposed to Go.
- Go owns documents, DOM nodes, resources, tasks, queues, and logical regions.
- Each node wrapper stores only numeric `(DocumentGeneration, NodeID)`
  identity in two V8 internal fields. It is not a Go pointer and ordinary
  JavaScript aliases do not change ARC.
- A `ValueHandle` indexes a V8-owned persistent-function table. Publishing
  that handle to a Gossamer timer or microtask queue is the semantic lifetime
  boundary. DOM listeners instead run synchronously on the dispatch stack of
  the already-published browser input task.
- Each registered DOM listener contributes one semantic target claim. That
  claim is independent from weak wrapper ownership and keeps a detached target
  alive until the listener is removed or the document Realm is torn down.
- Realm close resets the V8 context and persistent tables, notifies the V8
  platform, disposes the isolate, and then lets the Go Realm release its claims.

## Pinned upstream build

`tools/v8/VERSION` pins both inputs needed to reproduce the build:

- V8 stable branch `15.2`, version `15.2.124.12`, commit
  `b379cb574a4ab63675b73decd2cef8c7dde100e8`.
- depot_tools commit `13febbee9ece9e03df923f69d540afc63c6db93e`.

The checkout and generated artifacts live under ignored `third_party/v8`
paths. `tools/v8/bootstrap.sh` uses V8's official `fetch` and `gclient` flow,
then verifies the exact source revision. `tools/v8/build.sh` invokes the
checkout's own pinned ARM64 GN and Ninja binaries.

The profile configuration is release V8 with full symbols, pointer
compression, the V8 sandbox, bundled ICU, embedded startup data, and V8's
hardened libc++. Gossamer's C++ bridge is compiled by the same GN toolchain as
a dylib that owns V8's complete C++, ICU, and Rust dependency closure. The Go
boundary links that artifact through `@rpath` and sees only the exported C ABI
declared in `internal/v8engine/bridge.h`; V8's C++ symbols remain hidden.

```sh
tools/v8/bootstrap.sh
tools/v8/build.sh
tools/v8/test.sh -v
tools/v8/profile.sh
```

The V8 adapter is intentionally guarded by the `v8` build tag. Ordinary
`go test ./...` and non-JavaScript builds do not require the 7+ GB source
checkout or the generated monolith.

## Implemented checkpoints

The adapter implements the engine-independent runtime and profiling calls:

- process-wide V8 platform and ICU initialization;
- one isolate and context per `JSRealm`;
- explicit `MicrotasksPolicy::kExplicit` checkpoints;
- classic source compilation and execution with URL, line, column, and stack
  diagnostics;
- controlled pumping of V8 foreground platform work at the checkpoint;
- GC prologue and epilogue counters and accumulated GC time;
- heap totals, global-handle totals, cumulative allocated bytes, native-context
  counts, and detached-context counts;
- sampling heap profiles with live and collected allocation totals;
- forced low-memory collection for repeatable profiling; and
- context reset, platform isolate-shutdown notification, and isolate disposal.

The wrapper layer now adds:

- `window`, `self`, and a canonical `document` wrapper for the native document
  root;
- inherited `EventTarget`, `Node`, `Element`, `HTMLElement`, `Text`, and
  `Document` prototypes with interface-correct `instanceof` behavior;
- constructible `Event`, `MouseEvent`, `PointerEvent`, `KeyboardEvent`,
  `InputEvent`, and `FocusEvent` objects;
- `document.getElementById()` over connected, generation-scoped Go nodes;
- `document.createElement()`, `document.createElementNS()`, and
  `document.createTextNode()` for detached, task-local construction nodes;
- inert `[SameObject]` `HTMLTemplateElement.content`, deep template cloning,
  `Text.splitText()`, `Node.normalize()`, and same-document import/adopt;
- native `Range`, `TreeWalker`, and `NodeIterator` facade objects that retain
  their Go boundary/root nodes across V8 collection;
- cross-container Range contents, UTF-16 text boundaries, a canonical
  document/window `Selection`, and mutation-aware filtered traversal;
- `ownerDocument`, `documentElement`, `head`, `body`, `defaultView`, `baseURI`,
  `namespaceURI`, `prefix`, and `localName` metadata;
- fresh value-only `DOMRect` objects, `getBoundingClientRect()`,
  `getClientRects()`, element client/offset/scroll dimensions, live viewport
  metrics, root scrolling methods, `scrollingElement`, and `scrollIntoView()`;
- one weakly cached V8 object per numeric node identity;
- `textContent` reads and replacement mutations;
- `appendChild()`, `insertBefore()`, and `removeChild()` with stable identity;
- live `[SameObject]` `NodeList` and `HTMLCollection` child views with indexed,
  named, iterable, `item()`, and `forEach()` behavior as appropriate;
- live `[SameObject]` `DOMTokenList` and `DOMStringMap` objects for `classList`
  and `dataset`, including token mutation and named-property enumeration;
- `getAttribute()`, `setAttribute()`, `removeAttribute()`, and
  `getAttributeNames()` with insertion-order snapshots;
- generic `addEventListener()`, `removeEventListener()`, and synchronous
  `dispatchEvent()` with boolean or `{capture, once, passive}` options;
- capture, target, and bubble phases with stable `target`, changing
  `currentTarget`, `eventPhase`, `composedPath()`, cancellation, and propagation
  controls;
- browser-dispatched click, pointer, keyboard, input, focus, and change events
  with their first payload fields and interface-correct `instanceof` behavior;
- `queueMicrotask(fn)`, `setTimeout(fn, delay)`, and `clearTimeout()`;
- `ResizeObserver` and viewport-root `IntersectionObserver` objects whose target
  registrations retain canonical wrappers while their geometry remains owned
  by the current Go layout snapshot;
- typed positioned-layout properties whose relative, absolute, fixed, and
  integer stacking geometry is measured synchronously through the same native
  layout snapshot used for paint and hit testing;
- typed single-line flex properties, including direction, order, gaps,
  grow/shrink/basis, justification, and alignment, consumed by Go layout from
  React-authored inline styles;
- execution-scoped C callback tables that carry a numeric registry ID, never a
  Go pointer, into an engine entry; and
- wrapper, callback, listener, and dispatch counts alongside V8 heap metrics.

Listener functions remain in V8-owned persistent handles attached to numeric
`NodeHandle` targets. External input crosses the Go task queue once, then V8
builds the target-to-document path and invokes the capture, target, and bubble
listeners synchronously in registration order. This preserves DOM propagation
state and lets `stopPropagation()`, `stopImmediatePropagation()`, once removal,
and `preventDefault()` affect the current dispatch. Timer and Gossamer
microtask callbacks continue to use one-shot numeric `ValueHandle` entries.

The wrapper cache is weak. One cached wrapper for a detached node contributes
one root to a wrapper-owned region, regardless of how many JavaScript aliases
reference it. Live facade objects cache on that canonical wrapper and retain it
through a V8 internal field; they do not introduce another Go ownership claim
or native identity. Listener claims share the reconciled ownership region but
are counted independently, so collecting a wrapper cannot reclaim a detached
target that still owns a listener. A connected wrapper or listener needs no
duplicate claim because the document already owns its graph; detachment
activates the necessary root before the document claim is released. V8 weak
collection queues numeric identities back to Go, and a detached subgraph whose
final wrapper, facade, and listener roots disappeared is reclaimed from the
`NodeStore` without reusing `NodeID`.

## Short-lived DOM construction regions

Every DOM node first observed during JavaScript execution receives its initial
claim from the active task region. Gossamer mirrors both child and parent tree
edges in the shadow ledger, so retaining any node in a detached tree preserves
the complete navigable component, including cycles.

```text
document.createElement()
  -> allocate NodeID + task-region object
  -> create one weak V8 wrapper
  -> add wrapper-region root
  -> task close drops the construction claim

appendChild() into connected document
  -> document write barrier retains reachable component once

V8 weak wrapper collection
  -> remove numeric wrapper root
  -> reconcile wrapper-region reachability
  -> reclaim nodes with no document, wrapper, or listener claim

addEventListener() on detached node
  -> retain one listener target claim
  -> removeEventListener() / once invocation releases that claim
  -> document Realm teardown releases every remaining listener together
```

The connected DOM and V8-facing wrapper/listener region are separate semantic
owners. Ordinary tree edges and aliases do not themselves increment ARC. Root
reconciliation is the explicit boundary that retains newly reachable nodes or
releases an unreachable component after connectivity, wrapper roots, or
listener claims change.

V8 platform initialization is process-wide. V8 documents `V8::Dispose()` as
permanent and disallows reinitialization, so closing a Gossamer `Engine` closes
all of its isolates but intentionally leaves the shared platform alive until
process exit. Isolate teardown remains complete and independently measurable.

`tools/v8/profile.sh` runs a browser-hosted allocation and DOM churn workload.
Its ordered JSON timeline joins stock-V8 heap, sampling, GC, wrapper, callback,
and listener counters with the Go Realm's physical RegionStore counters,
semantic ownership counters, queue depths, and document-node totals. Each live
iteration is captured while a representative typed native graph is owned by a
queued consumer; the final checkpoint follows DOM release, task settlement,
forced V8 collection, weak-wrapper reconciliation, and a Go heap invariant
check. Byte fields are raw bytes; `evaluationNanos` and `gcNanos` are integer
nanoseconds. An alternate browser source file can be supplied with `-script`.

```sh
tools/v8/profile.sh -iterations 5 -sampling-interval 4096
tools/v8/profile.sh -script ./workload.js
```

Before region-backed browser facade records, the five-iteration native
workload established the first allocator baseline:
each live checkpoint has seven native slots. Region buffer recycling performs
one eight-slot buffer allocation and four reuses with no growth; the final
checkpoint reports 35 semantic allocations, 35 frees, zero live slots, and one
pooled eight-slot buffer. These counters make later slab or payload-pool work a
measured change rather than an ownership-model change.

Each current-document `NodeHandle` now also has one immutable RegionStore
HostObject containing its facade class, document generation, and NodeID. V8's
canonical weak wrapper still carries only the numeric handle and remains under
V8 GC; aliases do not create additional HostObjects. Wrapper/listener claims
govern detached node lifetime, and native reclamation frees the corresponding
HostObject. The churn gate checks that live HostObject count rises once per
created native node, falls with detached wrapper collection, and reaches zero
at Page teardown.

V8 callback handles remain engine-owned identities, but their waiting lifetime
is now represented by native HostObjects. `setTimeout` retains a record in the
Realm until fire or clear; firing transfers it through the Page task queue.
`queueMicrotask` and host-queued callbacks copy a record directly from the
producer task into their queue. The consumer validates that record before
invoking V8, and task release reclaims it.

Child iframe Pages have separate stock-V8 Realms and Browser-unique document
generations. The browser kernel does not pass wrapper or DOM backing pointers
between those isolates. Cross-document node transfer uses a serialized native
ArrayBuffer envelope and creates fresh target wrappers; general native
`postMessage` payloads use RegionStore structured clone and transferable
ArrayBuffer detachment.

## Proven sequence

The tagged tests prove both the isolated adapter and the browser path:

```text
Page script task
  -> stock V8 compile + run
  -> return to Gossamer
  -> explicit V8 microtask checkpoint
  -> next Page script task observes Promise result
  -> navigation render task
  -> Frame publication
```

The direct adapter test additionally allocates a retained JavaScript graph,
captures sampling and cumulative-allocation telemetry, removes that graph,
forces collection, verifies GC callbacks, verifies exception diagnostics, and
closes the isolate. The engine profile records the closed Realm snapshot so
teardown can be correlated with Gossamer's region ledger.

The wrapper integration test proves the complete browser sequence:

```text
JavaScript document.getElementById
  -> weak V8 wrapper with numeric Go identity
  -> click hit test resolves the same NodeHandle
  -> Gossamer publishes one browser input task
  -> V8 builds the target-to-document propagation path
  -> capture, target, and bubble listeners run synchronously
  -> textContent mutates the Go Document
  -> separate render task publishes the updated Frame
```

It also forces V8 collection and verifies that an unreferenced transient
wrapper disappears while the listener-retained wrapper remains live.

The event integration test uses the delegation shape employed by React: one
listener on a root container receives a click whose native target is a nested
button. It verifies document/root capture, target capture and bubble, root and
document bubbling, wrapper identity in `target` and `currentTarget`, the
composed path, once/passive/removal behavior, and the pointer, keyboard, input,
focus, and change event families. No listener callback creates an extra Go
task or `ValueHandle`.

The live-facade integration test holds one cached object per node and verifies
that child collections, class tokens, datasets, named properties, indices, and
iterators re-read the current Go DOM after mutation. It then detaches a
parent/child component and retains only its `NodeList`, followed by a detached
element retained only through `classList`. Forced V8 collection preserves each
native component while the facade remains reachable and returns both the Go
node store and V8 wrapper cache to baseline after the final facade is released.

The mutation-churn test runs 64 framework-shaped subtree cycles through stock
V8. Every cycle creates elements and text, writes and removes attributes,
reorders children, detaches and reinserts a child, briefly connects the
subtree, and removes it. One final survivor reaches the renderer. The test
separately verifies:

- 322 new Go nodes begin in the script task's construction region;
- 323 V8 wrappers are created, including the canonical document wrapper, and
  the other 322 are reclaimed after JavaScript drops its aliases;
- 320 detached Go nodes are reclaimed with those wrappers, while the connected
  survivor element and its text keep their document claims;
- ordinary JavaScript aliases create no additional claims, while each wrapper
  and document region claims a reachable node at most once;
- script publication and render invalidation still cross exactly two queue
  boundaries, independent of mutation count; and
- Realm teardown clears the canonical wrapper and every other V8 handle, and
  ends with no callback handles or persistent shadow ownership objects.

This makes the reclamation boundary explicit. Stable numeric identity is
unchanged by promotion, detachment, wrapper collection, or physical removal;
reclaimed IDs become tombstones and are never reused within the document.

## Framework wrapper milestones 4-15

The framework-renderer sequence now runs through the same ownership socket:

1. `querySelector()`, static `querySelectorAll()` results, `matches()`, and
   `closest()` use the Go selector engine while preserving canonical wrappers;
2. `DocumentFragment`, deep and shallow cloning, `innerHTML`, serialization,
   and all four `insertAdjacentHTML()` positions mutate the stable-ID DOM;
3. dirty form `value` and `checked` state, markup defaults, focus,
   `activeElement`, input updates, checkbox activation, and canceled-default
   rollback live on the Go side of the boundary; and
4. a pinned production React 19.2.7 plus ReactDOM bundle renders, updates from
   delegated click and controlled-input events, unmounts, forces collection,
   and tears down its Realm with no Go ownership remaining;
5. atomic convenience mutation methods preserve one Go transaction and typed
   DOM failures;
6. specialized HTML interfaces preserve the canonical wrapper cache;
7. coordinated form state, editing/composition, and native MutationObserver
   delivery remain on the Go side of the boundary;
8. template construction, text normalization, Range, and filtered traversal
   objects retain their native boundary nodes; and
9. the expanded React gate drives controlled text, textarea, checkbox, radio,
   and select state, MutationObserver delivery, keyed reconciliation, forty
   churn cycles, unmount, forced GC, and Realm teardown; and
10. cross-container Range operations, canonical Selection state, and
    mutation-aware reject/skip traversal retain their boundary objects through
    forced collection and release everything at Realm teardown;
11. cross-document import/adopt and structured-clone transfer move pointer-free
    data through independently owned Page Realms;
12. `FormData`, validation, cancelable `requestSubmit`, GET/POST submission,
    history/location, document replacement, Realm teardown, and paint form one
    complete browser slice; and
13. the conformance gate combines focused Web Platform behavior, React,
    cross-Realm transfer, forced GC, 25 submissions, 200 replacements, zero
    ownership after teardown, and explicit performance budgets; and
14. a real React layout effect measures retained Go geometry, scrolls the root
    viewport, observes task-coalesced scroll delivery, survives forced GC with
    only a value `DOMRect`, and releases all ownership at Realm teardown.
15. nested overflow containers retain stable-ID scroll offsets in Go, expose
    synchronous CSSOM View geometry, clip paint and hit testing through one
    projection, and coalesce non-bubbling scroll events per element target.
16. `requestAnimationFrame` callbacks retain native Realm records, batch onto
    one Page task with a shared monotonic timestamp, drain microtasks, and
    publish one render; viewport resizing crosses the same ordered boundary.
17. resize and intersection observation sample the current native layout at an
    explicit checkpoint, preserve target identity while only an observer is
    reachable, and release all registrations on disconnect or Realm teardown.
18. a React layout effect measures relative, absolute, and fixed boxes, root
    scrolling leaves fixed geometry stable, and delegated overlap input selects
    the same highest local `z-index` box that the display list paints last.
19. a React-authored flex row distributes and orders native element boxes,
    exposes the used result synchronously in a layout effect, and routes a
    delegated click through the same reordered geometry used for paint.

The React fixture is generated by `tools/v8/build-react-fixture.sh`. It is a
real framework bundle, not a framework-shaped replacement, and the test pumps
React scheduler work in queue order before asserting DOM and paint state.

The exact supported surface and current Range/traversal/cross-document limits
are recorded in [`docs/dom-compatibility.md`](dom-compatibility.md).
The combined release command and budgets are recorded in
[`docs/conformance-gate.md`](conformance-gate.md).
