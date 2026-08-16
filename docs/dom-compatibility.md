# DOM compatibility gate

Gossamer keeps JavaScript objects in stock V8 and browser objects in Go. The
DOM compatibility gate verifies that this split remains observable as one DOM:
canonical V8 wrappers carry numeric `NodeHandle` values, while Go owns node
identity, mutation, form state, construction regions, and queue ARC.

## Selected completed milestones 8-36

| Milestone | Native surface | Regression boundary |
| --- | --- | --- |
| 8. Atomic mutation kernel | `append`, `prepend`, `before`, `after`, `replaceWith`, `remove`, `replaceChildren`, typed `DOMException` failures | One Go mutation transaction and one later render publication regardless of argument count |
| 9. HTML interfaces | Dedicated form-control, template, and iframe prototypes | One canonical wrapper per `NodeHandle`, including across base and specialized interface lookups |
| 10. Form state | Dirty `value`/`checked`/`selected` state, radio coordination, select/options, form owners, reset, live collections | Current state stays in Go and collection-only reachability retains the native owner graph |
| 11. Editing | UTF-16 selections, `select`, `setSelectionRange`, `beforeinput`, native edits, `input`, composition events | Cancelable edits roll back before mutation; event order crosses one input task |
| 12. Observation | `MutationObserver` and `MutationRecord`, subtree/filter/old-value options, `takeRecords`, `disconnect` | Records come from the Go mutation journal and observer-only detached targets survive collection |
| 13. Construction | Inert `template.content`, deep template cloning, `splitText`, `normalize`, same-document import/adopt, `Range`, `TreeWalker`, `NodeIterator`, `NodeFilter` | Template ownership is a semantic lifetime edge rather than a DOM parent edge; facade-only roots survive forced GC |
| 14. React gate | Pinned production React 19.2.7 render and reconciliation over the native DOM | Controlled text, textarea, checkbox, radio, and select state; delegated input; observer delivery; keyed identity; forty churn cycles; unmount; forced GC; zero ownership after Realm teardown |
| 15. Range and selection | Cross-container partial cloning/extraction/deletion, UTF-16 character-data boundaries, text-boundary insertion, canonical document/window `Selection`, mutation-aware filtered traversal | Fully selected nodes preserve identity during extraction, selection-only detached roots survive GC, reject prunes while skip promotes descendants, and traversal cursors adjust through insertion/removal |
| 24. Cross-Realm transfer | Child Page/Realm ownership, pointer-free import/adopt, structured clone, transferable ArrayBuffers, native `postMessage` | No Go pointer crosses a Realm; parent replacement closes children and transferred buffers detach atomically |
| 25. Form navigation | Constraint validation, `requestSubmit`, `submit`, `FormData`, cancelable events, GET/POST encoding, history/location, Realm replacement, final paint | Invalid or canceled submissions do not navigate; success invalidates the old generation and tears its Realm down in bulk |
| 26. Release gate | Focused WPT-aligned behavior, pinned React, forced GC, cross-Realm messaging, 25 submissions, 200 replacements | Bounded runtime/wrapper churn and zero native ownership after teardown |
| 27. CSSOM View and root scrolling | Fresh `DOMRect`, `getBoundingClientRect`, `getClientRects`, client/offset/scroll dimensions, viewport metrics, root scrolling, `scrollIntoView`, translated paint/hit testing, queued scroll events | Geometry reads reuse one immutable Go layout snapshot; React layout effects measure synchronously; scroll delivery is coalesced after the task; value-only rectangles survive forced GC; Realm teardown returns ownership to zero |
| 28. Element overflow scrolling | Typed `overflow-x`/`overflow-y`, Page-owned per-element offsets, nested `scrollTop`/`scrollLeft`, scrollport clipping, ancestor-aware `scrollIntoView`, translated paint/hit testing, element scroll events | Stable `NodeID` offsets do not mutate layout; nested geometry reads are synchronous; paint and input share one transform/clip projection; scroll events coalesce per target; navigation clears all offsets |
| 29. Animation timing and resize | `requestAnimationFrame`, cancellation, Page-relative `performance.now()`, batched timestamps, queued viewport resize events | Callback records transfer Realm to queue to task; canceled callbacks release or drain their region; one frame batch shares a timestamp; resize listeners and microtasks run before rAF and one render |
| 30. Layout observation | `ResizeObserver`, `IntersectionObserver`, entry geometry, thresholds, target registration lifecycle | Delivery reads the current immutable Go layout snapshot at the explicit checkpoint; observers retain canonical target wrappers across forced V8 GC; disconnect and Realm teardown release every native claim |
| 31. Positioned layout | Typed `position`, physical insets, integer `z-index`, relative/absolute/fixed block geometry, local stacking order | Absolute boxes leave normal flow; fixed boxes bypass root scrolling; display-list paint and hit testing share the same ordered positioned children; a React layout effect measures the same native geometry used by delegated click routing |
| 32. Flex formatting | `display:flex`, row/column directions and reversals, grow/shrink/basis, `order`, row/column gaps, justification, cross-axis alignment | Ordered element children share one single-line flex calculation for retained geometry, paint, CSSOM View, and hit testing; React style assignment and `useLayoutEffect` observe the native result; teardown returns ownership to zero |
| 33. Interactive window and native input | Backend-neutral window loop, AppKit presentation, resize, pointer, wheel, keyboard, focus, blur, text editing, stock-V8 launcher | The backend owns only copied RGBA pixels and value-only events; every DOM/script mutation crosses the Page queue; deterministic and stock-V8 tests assert event order, final state, frame publication, forced GC, and zero ownership after teardown |
| 34. Production page boot | Live `document.readyState`, blocking/defer/async scheduling, `DOMContentLoaded` and `load`, browser-fetched stock-V8 ES modules, per-Realm module identity | An HTTP-served Vite-shaped module graph boots pinned production React, commits and updates the Go DOM, evaluates a duplicated module once, unmounts, forces GC, and closes with zero native ownership |
| 35. Graphite browser shell | Single-tab Graphite chrome, editable address navigation, reload, content viewport, collapsed telemetry rail, engine/kernel inspector | Chrome remains in Go, page coordinates are translated before hit testing, native dimensions subtract to the exact DOM viewport, and deterministic plus stock-V8 tests retain ordered input and teardown behavior |
| 36. Session history traversal | `Page.Back`, `Forward`, `Go`, and `Reload`; Graphite controls and keyboard commands; live direction availability | Traversal commits the index only after a successful document rebuild, creates a fresh document generation and V8 Realm, invalidates departed wrappers, preserves the current entry after failure, and leaves zero ownership after teardown |

Run the gate against the locally built stock V8:

```sh
GOCACHE=/tmp/gossamer-go-cache ./tools/v8/test.sh -count=1
GOCACHE=/tmp/gossamer-go-cache go test ./...
GOCACHE=/tmp/gossamer-go-cache ./tools/v8/gate.sh
```

The React test is a compatibility gate, not an alternate DOM implementation.
The bundle calls the same public V8 prototypes and browser host callbacks as
page JavaScript. All mutations therefore pass through Go's stable-ID document
and ownership barriers before a separate render task publishes a frame.

## Current limits

- `MutationObserver` delivery occurs at Gossamer's explicit post-task
  checkpoint. Ordering relative to every browser-defined microtask source is
  not yet claimed.
- `ResizeObserver` and viewport-root `IntersectionObserver` delivery also occurs
  at that explicit checkpoint. Resize box options, custom intersection roots,
  `rootMargin`, visibility tracking, and browser-exact observer task timing are
  not yet claimed.
- Positioned layout currently covers block boxes, physical insets, viewport
  fixed positioning, nearest represented positioned containing blocks, and
  local integer stacking order. Inline containing blocks, logical insets,
  sticky positioning, transforms, and the complete CSS stacking-context paint
  algorithm remain future work.
- Flex layout currently covers a single line of element children. Wrapping,
  anonymous flex items for non-whitespace text, intrinsic/min-content sizing,
  auto margins, baseline alignment, `align-self`, and the complete flexible-box
  min/max constraint algorithm are not yet claimed; `inline-flex` computes but
  still follows the existing inline fallback.
- The AppKit backend is an Apple Silicon macOS milestone surface. Graphite
  currently supports one window and one functional tab with address/reload
  chrome plus mouse, wheel, basic keyboard text, focus, blur, and resize events.
  Multiple tabs, a back-forward cache, scrollbar widgets, clipboard,
  IME/composition, touch, drag-and-drop, accessibility, and non-macOS backends
  remain future work.
- Initial scripts run after the document and represented render resources are
  loaded, not from a streaming HTML parser. Static HTTP(S) module imports are
  supported; import maps, bare package specifiers, dynamic `import()`, module
  workers, and dynamically inserted scripts remain future work. The initial
  `load` event is delivered to the canonical document until `Window` becomes a
  complete `EventTarget`.
- Range contents now cross nested containers and UTF-16 character data.
  `surroundContents`, contextual fragments, point-comparison helpers, and full
  live boundary adjustment after unrelated DOM mutations remain future work.
- `TreeWalker` and `NodeIterator` refresh from the native mutation sequence,
  prune rejected subtrees, and promote skipped descendants. Exact Web Platform
  cursor adjustment for every complex reparenting case is not yet claimed.
- Selection currently owns at most one forward Range and supports collapse,
  select-all, deletion, string projection, and the standard range collection
  methods. Backward selections, `extend`, `setBaseAndExtent`, `modify`, and
  `containsNode` are not exposed yet.
- The Page API now performs cross-document import/adoption through explicit
  source and target Realm tasks using a pointer-free DOM encoding. Stock-V8
  Realms remain separate isolates, so a JavaScript wrapper itself cannot be
  passed directly between iframe globals; cross-Realm data uses the native
  structured-clone/`postMessage` seam and fresh target wrappers.
- This gate does not claim Shadow DOM, custom elements, multipart/file form
  submission, every validation constraint, script-visible History APIs or a
  back-forward cache, scrollbar UI,
  accessibility, or the complete Web Platform test surface. Overflow now
  clips and scrolls block boxes, but inline overflow fragmentation, scroll
  anchoring, snap points, overscroll behavior, and scrollbar layout remain.

These limits are intentional compatibility boundaries. New DOM work should
add a focused Go test, a stock-V8 behavior test, a forced-GC lifetime check
when a facade can outlive its owner, and a Realm teardown assertion.
