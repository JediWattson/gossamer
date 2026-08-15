# Phase 1: sequenced navigation

Phase 1 makes `browser.Page` the authority for document loading, resource
commit, and frame replacement. Network and decoding work stays outside the
Realm executor. Every state change caused by that work enters through the
Page's ordered task queue.

## State sequence

```text
Idle
  -> LoadingDocument
  -> LoadingResources
  -> Rendering
  -> Complete

Any active state
  -> Failed | Canceled
```

`Page.Navigate` cancels the previous navigation context and assigns a new
`NavigationID`. Fetching, parsing, DOM indexing, and resource discovery happen
off-Realm. The resulting document is not visible until its completion task
runs and verifies that the `NavigationID` is still current.

The document completion task installs the indexed document, final redirected
URL, empty stable resource store, and a new `DocumentGeneration` atomically.
Rendered resource requests then run off-Realm in DOM order. Each stylesheet or
image result is published back as another Page task. The last result queues one
render task; only that task may replace `Page.Frame` and mark the navigation
complete.

Resource transport or decode failures remain isolated to their resource. A
navigation-context cancellation terminates the navigation and does not replace
an existing screenshot or frame.

## Stable async identity

`NodeID` is scoped to one indexed document and may be reused by the next
document. Async browser work therefore uses:

```text
NodeHandle = (DocumentGeneration, NodeID)
```

Resource completion also carries `NavigationID`. A completion must match both
the current navigation and current document generation before it can update
the stable resource maps. Text-mutation tasks snapshot the same handle, so a
queued mutation cannot accidentally target a later document that reused its
numeric `NodeID`.

The renderer remains pointer based during migration. Immediately before
render, Page resolves its `NodeID`-keyed stylesheet and image stores into the
existing pointer-keyed `render.Resources` shape. That adapter is the only
transitional pointer boundary in the navigation path.

## Completion ownership sequence

Every document or resource completion receives a browser-owned shadow
envelope:

```text
browser region creates envelope
  -> browser publishes envelope to Page task queue
  -> browser releases its claim
  -> queue transfers its exclusive claim to receiving task
  -> task commits or rejects the completion
  -> task-region close releases the final claim
  -> envelope is destroyed
```

The final resource task allocates a task-local invalidation object and queues
the render task with it. This proves both external completion publication and
internal task-to-task handoff in one navigation trace.

## Current sequencing choices

- Resource requests are fetched in DOM order on one off-Realm worker. This
  keeps stylesheet/image completion order deterministic while the browser
  kernel is being proven.
- Duplicate image URLs share transport cache entries and decoded image data.
- The total navigation image-pixel budget is applied in DOM order.
- Only external stylesheets and `<img src>` consumers currently feed the
  renderer, preserving the previous CLI behavior.
- Rendering happens once after all initial rendered resources settle. Later
  incremental resource painting can retain the same task and generation
  barriers.

The `--screenshot` command now uses this Browser/Page sequence. There is no
second CLI-only resource-loading path.

## Completed follow-up

Input dispatch, generation-safe hit testing, Realm-retained timers, the
engine-neutral contracts, initial classic-script evaluation, and the fake
engine demo are now implemented. See
[`pre-v8-engine-boundary.md`](pre-v8-engine-boundary.md).

The stock V8 adapter now executes and profiles scripts behind that boundary.
The next implementation step is the stable-handle DOM wrapper table described
in [`stock-v8-integration.md`](stock-v8-integration.md).
