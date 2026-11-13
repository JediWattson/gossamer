# Interactive window backend

Milestone 33 connects the existing browser Page to a real macOS window without
changing the V8/Go ownership split. Milestone 35 composes the functional
Graphite browser shell around the same Page frame. The initial native backend
is AppKit on Apple Silicon; `MemoryBackend` drives the identical loop in
deterministic tests.

## Boundary

```text
engine socket              Go browser                    AppKit
-------------              ----------                    ------
V8 reference now           Graphite shell                NSWindow
Strand target              Page + Realm queue            copied RGBA buffer
DOM wrappers --numbers-->  NodeHandle
                            DOM/style/layout/display list
native event value <------ chrome/input router <--------- NSEvent
             \-----------> ordered Page task
```

The backend interface can open a surface, return one normalized event, present
an `image.RGBA`, and close. It never receives a `Document`, `Node`, V8 handle,
RegionStore reference, or callback. `Present` copies pixels before returning;
the native view owns its front buffer independently from the immutable Go
frame.

## Ordered session

`window.Run` and `window.RunBrowser` stay on one locked OS thread and repeat
this sequence:

1. drain every tab Page Realm task queue to its checkpoint;
2. rasterize and present when the immutable Frame pointer or Graphite shell
   revision changed;
3. poll one native event;
4. normalize it, let Graphite consume chrome events or translate content
   coordinates, and enqueue the corresponding Page operation;
5. let the next loop iteration execute that task, V8 listeners, microtasks,
   default action, observer/frame work, and rendering in browser order.

Resize calls `QueueViewportResize`. Wheel/trackpad input calls
`QueueViewportScrollBy`, which clamps the root offset, dispatches one root
`scroll` event, drains V8 microtasks, and schedules paint. Pointer records use
the existing retained layout hit test. A pointer-down remembers only a stable
numeric `NodeHandle`; pointer-up hit-tests again and emits a primary click only
when it lands on the same handle.
Keyboard, focus, and blur events target the active or most recently hit native
node. Physical keys and committed Unicode text are distinct records. Direct
text, paste, cut, and composition commits become cancelable `beforeinput`; the
existing Go default action updates form state and dispatches `input`.

`ClipboardBackend` is optional and transfers copied strings only. Command-C
reads the control's UTF-16 selection, while Command-X and Command-V enqueue
`deleteByCut` and `insertFromPaste`. AppKit implements `NSTextInputClient` and
queues composition start/update/end plus committed text after the physical key
record. Graphite consumes that same committed-text channel for its address
field. Candidate-window placement and the script-visible Clipboard API are not
claimed yet.

The Cocoa implementation converts its bottom-left coordinates to top-left CSS
pixels, normalizes scroll direction, maps basic special keys and alphanumeric
codes, applies a Dark Aqua native frame, bridges `NSPasteboard` and
`NSTextInputClient`, and copies RGBA pixels into a CoreGraphics image. AppKit
initialization is rejected off the process main thread.

Graphite reserves an 84-pixel top inset and 48-pixel collapsed rail outside
the DOM viewport. Address/reload input remains shell-owned. Document pointers
are translated through the content origin before the existing retained-layout
hit test. See [`graphite-shell.md`](graphite-shell.md) for its functional and
replacement-engine contract.

## Run and validate

Build the pinned stock V8 checkout once, then open an absolute HTTP(S) URL:

```sh
tools/v8/bootstrap.sh
tools/v8/build.sh
tools/v8/window.sh https://example.com
```

Compile the tagged launcher and run the deterministic backend test without
opening a desktop window:

```sh
GOCACHE=/tmp/gossamer-go-cache tools/v8/window.sh --check -count=1
```

The native launcher can write a value-only JSONL frame trace for either engine:

```sh
GOCACHE=/tmp/gossamer-go-cache go run ./cmd/gossamer-window \
  --engine=strand --frame-profile=/tmp/gossamer-frames.jsonl \
  https://example.com
```

For a repeatable non-interactive scroll sample, retain the same shell,
compositor, device-scale, and input path while replacing manual gestures with
a bounded sequence:

```sh
GOCACHE=/tmp/gossamer-go-cache go run ./cmd/gossamer-window \
  --engine=strand --frame-profile=/tmp/gossamer-scroll.jsonl \
  --profile-scroll-count=20 --profile-device-scale=2 \
  https://example.com
```

The profiler waits for navigation completion plus the configured warmup,
scrolls at the content center, and closes after the final presentation.

Each presentation records task, page-raster, Graphite-composite, native-present,
total-work, and inter-frame durations in nanoseconds. A 60 Hz frame budget is
16.67 milliseconds. `scroll_frame` distinguishes the compositor-style frame
published before ordered scroll listeners from later listener reconciliation;
`incremental_raster`, `damage_pixels`, `damage_rects`, and `raster_fallback`
show whether that frame reused its retained page pixels, how much of the
changing underlay prefix it repaired, how fragmented that repair was, or why it
safely fell back to a full raster. On the retained-underlay path,
`damage_pixels` does not include the separate underlay copy or stationary
overlay replay work.
On a fallback, `planned_damage_pixels`, `planned_damage_rects`, and
`planned_scroll_clip_area` retain the rejected conservative plan.
`planned_raw_damage_rects`, `planned_exact_damage_pixels`, and
`planned_exact_damage_rects` also report the unmerged inputs and their exact
geometric union, so a profile can distinguish real damage, fragmentation, and
area introduced by conservative merging. `planned_damage_source` and its
source pixel/rectangle counts identify the largest exact contributor class.
The planned scroll delta and moving/scroll command counts reveal when one
outlier motion was mistaken for the dominant scroll.
`retained_underlay` means that the update used the separate scrolling-base
path; it does not merely report whether a base is resident.
`underlay_cache_hit` says whether that update reused the base, while
`underlay_cache_miss` is empty on a hit and otherwise reports `empty`, `bounds`,
`frame`, `scale`, or `boundary`. These fields remain false or empty when the
path was not attempted. `underlay_copy_pixels` reports the base-to-visible
framebuffer copy separately. `overlay_suffix_commands` and `overlay_regions`
describe the stationary suffix and its disjoint replay components.
`underlay_build_ns`, `scroll_copy_ns`, `damage_replay_ns`, `underlay_copy_ns`,
and `overlay_replay_ns` divide retained raster time into the phases used for
profile-driven optimization.

Retained scrolling trades bounded memory for frame time: at most one extra
device-sized RGBA underlay is held by the session rasterizer, plus independent
8 MiB caps for rounded-shape coverage and effect-layer paint caches. The latter
shares its budget between pre-opacity RGBA layers and byte-exact 16-bit
premultiplied transform samples. The session's `Close` releases those buffers
and its lazy replay workers.
Profiling output is buffered so the trace itself does not add one file write to
every frame.

The stock-V8 conformance tests drive Graphite resize, translated pointer,
click, focus, keyboard editing, scroll, blur, tab creation, switching,
navigation, and closure through `MemoryBackend`. They check JavaScript event
order and payloads, input value, exact content viewport, shell-sized frame
publication, forced V8 collection, balanced Realm lifecycle, and zero native
ownership after Page teardown.

## Current scope

This is still an early browser shell. Graphite has up to eight functional tabs,
independent Page history and input state, address navigation, history
traversal, reload, a bounded region-aware back-forward cache, root overlay
scrollbars, nearest-ancestor element scrollbar overlays, native text clipboard
and composition foundations, and an engine/kernel telemetry rail. It does not
yet have author-defined scrollbar styling or layout-affecting scrollbar chrome,
clipboard DOM events or `navigator.clipboard`, complete marked-text candidate
UI, touch, drag-and-drop, accessibility tree, GPU compositor, or non-macOS
backend. Event polling remains serial. Exact disjoint damage and stationary
overlay components use bounded CPU replay workers; ambiguous paint groups stay
on a conservative single-region path.
