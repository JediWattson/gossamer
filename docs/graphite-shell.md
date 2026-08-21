# Graphite browser shell

Graphite is Gossamer's first functional browser-owned GUI. It is a Go
compositor around the current immutable Page frame, not a DOM tree implemented
in AppKit and not a JavaScript-engine UI. Its launcher selects Strand or stock
V8 through the same engine-neutral browser socket.

```text
                         Graphite (Go)
       native event ---> chrome routing / coordinate translation
                              |
        +---------------------+--------------------+
        |                                          |
  engine socket                               browser kernel
  Strand native                               DOM / CSS / layout
  V8 reference                                regions / queue ARC
        |                                          |
        +----------- numeric wrappers -------------+
                              |
                      immutable Page frame
                              |
                      copied RGBA to AppKit
```

The knot mark carries the intended final architecture: teal is Strand, violet
is the Go browser kernel, and pearl crossings are the numeric wrapper and
ownership boundary. Stock V8 remains the default reference engine, while
`--engine=strand|v8` selects either implementation without changing Graphite,
Page scheduling, or browser-object ownership.

## Geometry and event ownership

`window.RunBrowser` adds an 84-pixel top chrome inset and a 48-pixel collapsed
right rail around the Page viewport. An initial 800 by 600 Page therefore opens
an 848 by 684 backend surface. A native resize to 368 by 324 publishes a 320 by
240 viewport resize to Page.

Graphite handles native input before the existing Page router:

1. tab, toolbar, address, history, reload, and rail events are consumed by the shell;
2. a content pointer has the chrome origin subtracted;
3. the translated value-only event enters the existing Page hit test and Realm
   queue;
4. DOM listeners, default actions, microtasks, observer work, and rendering
   retain their established order.

Root scrollbars are Graphite overlays, so they do not change CSS layout or DOM
viewport metrics. Thumb and track input is consumed before page hit testing;
drag positions become absolute root offsets through the same Page queue used
by wheel scrolling.

The native backend still sees only a copied `image.RGBA` and normalized values.
It receives no DOM node, JavaScript handle, Go region reference, or callback.
`window.Run` remains the chrome-free embedding primitive for tests and other
surfaces.

## Functional controls

- Clicking the address field or pressing Command-L focuses it and selects the
  current address.
- Enter normalizes a bare host to HTTPS and calls `Page.Navigate` with the
  shell's document loader.
- Escape restores the current Page URL.
- The reload control and Command-R rebuild the current history entry without
  appending a duplicate.
- Back/forward controls and Command-[ / Command-] traverse the Page's session
  history. Option-Left / Option-Right provide the same native shortcuts.
- The plus control and Command-T create a rendered blank Page with no URL or
  history entry; its first address navigation creates the first entry.
- Clicking a tab switches its Page, address/history state, and native input
  target. Control-Tab / Control-Shift-Tab cycle tabs, Command-Shift-[ / ] cycle
  in the same directions, and Command-1 through Command-8 select directly.
- A tab close control or Command-W closes that tab's Page, Realm, wrappers,
  queued work, and ownership regions. Closing the last tab ends the window.
- The collapsed right-rail disclosure opens an overlay with navigation state,
  live regions, native host objects, ownership claims, and task plus microtask
  queue depth.
- Back and forward controls dim automatically when the current history index
  has no entry in that direction.

The panel overlays content instead of changing the Page viewport. This keeps
opening diagnostics from causing layout or framework resize work.

## Regression contract

Backend-neutral tests assert composition layers, address navigation, exact
viewport subtraction, coordinate translation, DOM event dispatch, frame
dimensions, per-tab state, inactive-tab task pumping, and browser-owned Page
closure. Tagged stock-V8 tests repeat the Graphite input and tab sequences,
check JavaScript behavior, force V8 collection, close every Page, and require
balanced Realm creation/closure plus zero native ownership.

```sh
go test ./internal/window
tools/v8/window.sh --check -count=1
tools/v8/gate.sh
```

The ordinary macOS build can launch Strand without the V8 build tag:

```sh
go run ./cmd/gossamer-window --engine=strand https://example.com
```

## Deliberate next boundaries

Graphite does not yet claim page security-state derivation, downloads,
bookmarks, element scrollbar widgets, clipboard DOM events or Clipboard API,
complete marked-text candidate UI, drag-and-drop, touch, accessibility, GPU
compositing, or a non-macOS backend. Those features should extend the shell and
backend-neutral event contracts without moving browser object ownership into
the active JavaScript engine.
