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
node. Printable key-down text becomes cancelable `beforeinput`; the existing
Go default action updates form state and dispatches `input`.

The Cocoa implementation converts its bottom-left coordinates to top-left CSS
pixels, normalizes scroll direction, maps basic special keys and alphanumeric
codes, applies a Dark Aqua native frame, and copies RGBA pixels into a
CoreGraphics image. AppKit initialization is rejected off the process main
thread.

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

The stock-V8 conformance tests drive Graphite resize, translated pointer,
click, focus, keyboard editing, scroll, blur, tab creation, switching,
navigation, and closure through `MemoryBackend`. They check JavaScript event
order and payloads, input value, exact content viewport, shell-sized frame
publication, forced V8 collection, balanced Realm lifecycle, and zero native
ownership after Page teardown.

## Current scope

This is still an early browser shell. Graphite has up to eight functional tabs,
independent Page history and input state, address navigation, history
traversal, reload, and an engine/kernel telemetry rail. It does not yet have a
back-forward cache, scrollbar chrome, clipboard, IME/composition bridge, touch,
drag-and-drop, accessibility tree, GPU compositor, or non-macOS backend. Event
polling and CPU rasterization are intentionally serial until correctness and
profiling establish where concurrency is valuable.
