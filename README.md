# Gossamer

**A browser engine taking shape in Go, from URL to pixels.**

Gossamer is an experimental browser engine built from scratch in Go. It can
fetch a page, tokenize and parse its HTML, build a DOM, load stylesheets and
images, compute styles, lay out the document, produce a display list, and paint
the result to a PNG.

<p align="center">
  <img src="./example.png" alt="Gossamer rendering example.com at 800 by 600 pixels" width="800">
</p>

<p align="center"><em>example.com rendered by Gossamer at 800 x 600</em></p>

Gossamer is early-stage engine work, not yet a general-purpose browser. The
goal is to grow a correct, understandable pipeline first, then reuse its
backend-neutral display list for an interactive windowed browser.

## Quick start

Gossamer currently targets Go 1.25.3. Remote pages require network access, and
the parent directory for a screenshot must already exist.

```sh
git clone https://github.com/JediWattson/gossamer.git
cd gossamer
```

Fetch a document and print its response body:

```sh
go run ./cmd/gossamer https://example.com
```

Parse the document and inspect the constructed DOM:

```sh
go run ./cmd/gossamer --dump-dom https://example.com
```

Render a fixed 800 x 600 screenshot:

```sh
go run ./cmd/gossamer --screenshot page.png https://example.com
```

The argument order is currently strict. The three modes cannot be combined.
URLs must be absolute `http` or `https` URLs.

You can also build a standalone binary:

```sh
go build -o gossamer ./cmd/gossamer
./gossamer --screenshot page.png https://example.com
```

## How it works

```mermaid
flowchart LR
    URL["URL"] --> Loader["HTTP loader"]
    Loader --> HTML["HTML tokenizer + parser"]
    HTML --> DOM["DOM"]
    DOM --> Resources["CSS + image resources"]
    Resources --> Style["Computed style tree"]
    Style --> Layout["Layout boxes"]
    Layout --> Display["Display list"]
    Display --> PNG["PNG painter"]
```

The display list is deliberately separate from rasterization. A future window
backend can replay the same paint commands without replacing the document,
style, or layout pipeline.

## Phase 0 execution kernel

Gossamer now has an engine-independent execution kernel under
`internal/runtime`. A realm admits one ordered logical executor, drains its
microtasks after each task, and can run concurrently with other realms. Its
first-class queues are also ownership boundaries: fake runtime objects are
published to a queue, transferred to the receiving task, and released in bulk
when that task's logical region ends.

The shadow ledger now backs a small native `RegionStore`. Its typed payloads
live in generation-checked region slots, cross-region writes maintain counted
region edges, and private refs can cross Realm queues only through explicit
Transfer, Publish, or Copy operations. Task and microtask queues use the same
Ref boundary. Explicit promotion copies only a reachable subgraph into
immutable shared storage, allowing the original temporary region to be
released. The first typed ladder now covers Strings, Objects, Arrays, lexical
Contexts, Function descriptors, Promises, BigInts, Symbols, ArrayBuffers,
TypedArrays, Maps, Sets, Dates, RegExps, Errors, weak collections, and opaque
HostObjects. These remain storage and ownership primitives independent from
JavaScript execution semantics and V8 internals. See
[`docs/phase-0-kernel.md`](docs/phase-0-kernel.md) for the invariants and
[`docs/native-heap-types.md`](docs/native-heap-types.md) for the typed payload
contract.

Browser async work now uses that same physical boundary. Timers retain one
Realm-owned HostObject while waiting, transfer it through the task queue when
they fire, and release it with the consumer task. Queued callbacks and
microtasks copy one native callback record directly into their destination
queue; clearing, navigation replacement, and Page teardown destroy records
that never fire.

A synchronous native interpreter can now execute hand-assembled Function
bytecode over those payloads. Its borrowed frames expose Ref roots, all heap
mutation stays behind TaskContext barriers, and returned private values still
require explicit promotion or queue semantics before task release. Stock V8
remains the browser's JavaScript engine. See
[`docs/native-interpreter.md`](docs/native-interpreter.md) for the instruction
and lifetime contract.

The DOM now also has additive stable `NodeID` identity over its existing
pointer-backed storage. A minimal `Browser`/`Page` boundary ties an indexed
Document to its Realm, URL, resources, invalidation state, and current Frame.
Stable-ID mutations can enqueue a later render task without changing the
working CSS/layout/render implementation.

Navigation now follows that boundary as well: document and subresource I/O
runs off-Realm, browser-owned completion envelopes enter through ordered Page
tasks, stale navigation generations are rejected, and the final resource task
queues rendering. The screenshot CLI uses this same path. See
[`docs/phase-1-navigation.md`](docs/phase-1-navigation.md) for the sequence.

The pre-V8 browser socket is also complete: Page tasks own input hit testing,
timers, classic-script evaluation, explicit engine microtask checkpoints,
opaque callback scheduling, and render invalidation. A replaceable fake engine
proves the full click-to-paint flow without a JavaScript parser. See
[`docs/pre-v8-engine-boundary.md`](docs/pre-v8-engine-boundary.md).

An optional stock V8 15.2 adapter now occupies that socket on Apple Silicon.
The pinned, symbolized build keeps JavaScript values under V8 GC while browser
objects remain under Go regions and queue ARC. Its wrapper layer exposes stable
numeric node identity, canonical `Document`/`Node`/`Element`/`HTMLElement`/`Text`
prototypes, namespace-aware creation, selector traversal, inline style, DOM
`Event` objects, capture/target/bubble dispatch, generic listeners, tree and
fragment mutation, markup parsing/serialization, attributes, live child
collections, `classList`, `dataset`, form state, focus, microtasks, and timers
without passing Go pointers into V8. It also exposes live, read-only
`getComputedStyle()` declarations for the longhands represented by the current
style engine; every read crosses the browser host boundary to the current Go
computed-style snapshot rather than caching values in V8.
Browser input currently covers click, pointer, keyboard, input, focus, and
change event families. Profiling covers heap totals, sampled allocations,
GC callbacks, weak-wrapper collection, wrapper-root region sweeps, callback
publication, explicit Promise checkpoints, and isolate teardown. Task-created
DOM nodes now begin in short-lived construction regions; after task close they
survive only through the connected document graph or live V8 wrappers. A
framework-shaped churn test proves that synchronous node aliases do not create
queue ARC claims. A pinned real React 19.2.7 gate additionally proves initial
render, delegated click and controlled-input updates, unmount, forced GC, and
Realm teardown against the stock engine. See
[`docs/stock-v8-integration.md`](docs/stock-v8-integration.md). The expanded
DOM milestones, including Range, Selection, and mutation-aware traversal, and
their precise compatibility limits are tracked in
[`docs/dom-compatibility.md`](docs/dom-compatibility.md).

## What works today

### Loading and resources

- HTTP and HTTPS document loading, redirects, cancellation, and a 30-second
  client timeout
- Resolution against the final document URL and the first `<base href>`
- Eligible external stylesheet and `<img src>` discovery in DOM order
- Navigation-scoped resource caching and independent subresource failures
- PNG, JPEG, GIF, and WebP decoding with per-image and per-page pixel budgets
- Atomic screenshot replacement, preserving an existing output file's mode

### HTML and DOM

- Tags, attributes, text, comments, doctypes, and character references
- Raw-text and escapable-raw-text elements such as `style`, `title`, and
  `textarea`
- Common implied elements and implied end tags
- Deterministic DOM dumps for parser development
- Stable `NodeID` lookup in both directions through an indexed `Document`
- Optional stock-V8 classic-script execution and first-slice DOM wrappers on
  Apple Silicon, including delegated DOM event dispatch, live `NodeList` and
  `HTMLCollection` views, `classList`, and `dataset`; default builds still
  render body `<noscript>` fallback

### CSS, layout, and paint

- Type, ID, class, attribute, and combinator selectors with selected structural
  and logical pseudo-classes
- Cascade ordering across `!important`, specificity, source order, inline
  styles, and named top-level `@layer` rules
- Versioned, immutable computed-style snapshots and a live, read-only
  `getComputedStyle()` subset for the currently supported longhands and custom
  properties
- Inherited custom properties and nested `var()` fallbacks with cycle
  invalidation
- Screen media queries for viewport width, height, orientation, media types,
  comma alternatives, `not`, and `only`
- Common colors and `px`, `%`, `em`, `rem`, `vw`, and `vh` lengths
- Block flow, inline text wrapping, inline and block images, and basic vertical
  margin collapsing
- Content-box width and height, min/max width, margins, padding, solid borders,
  backgrounds, opacity, and text alignment
- Regular and bold bundled fonts, font sizing, line height, underline, and the
  size/line-height/weight portions of the `font` shorthand
- Disc, circle, square, and decimal list markers
- A retained layout tree and backend-neutral display list

## Repository layout

| Path | Responsibility |
| --- | --- |
| `cmd/gossamer` | CLI orchestration and screenshot output |
| `internal/loader` | Document and subresource HTTP loading |
| `internal/html` | HTML tokenization and tree construction |
| `internal/dom` | DOM nodes, tree ownership, and deterministic dumps |
| `internal/css` | Stylesheet parsing, selectors, layers, and media queries |
| `internal/resource` | Resource discovery, caching, limits, and image decoding |
| `internal/style` | Cascade, inheritance, typed computed values, and immutable style snapshots |
| `internal/render` | Used-value resolution, layout, display lists, and PNG painting |
| `internal/runtime` | Realms, task and microtask queues, actor scheduling, and shadow ownership telemetry |
| `internal/browser` | Browser/Page ownership, loading, stable-ID mutation scheduling, and frame invalidation |

## Current limitations

The following are intentionally still outside the current milestone:

- General JavaScript/DOM APIs beyond the initial V8 wrapper slice, navigation
  history, forms, text input, selection, scrolling, and a windowed UI
- Full HTML error recovery, including table foster parenting, formatting
  element reconstruction, templates, SVG/MathML, and encoding sniffing
- Flexbox, Grid, table layout, floats, positioned layout, and retained inline
  boxes
- `calc()` and related functions, gradients, background images, border radii,
  shadows, transforms, and generated content
- `@import`, `@supports`, anonymous, nested, or dotted cascade layers, and the
  remaining media-query syntax and features
- SVG, AVIF, video, canvas, browser controls, and accessibility trees

Only `inline`, `block`, `list-item`, and `none` have dedicated display
behavior; `inline-block` currently degrades to inline. Screenshots clip to one
800 x 600 viewport. Unsupported CSS is skipped, and failed stylesheets or
images degrade independently so the rest of a page can still render.

The first `getComputedStyle()` surface is layout-independent. It reports the
typed computed values Gossamer currently owns, so percentages, viewport units,
and `auto` are not converted to box-dependent used pixels. Unsupported
pseudo-elements return an empty live declaration; pseudo-element layout and
generated boxes remain future work.

## Development

Run the complete test suite:

```sh
go test ./...
```

Run the race detector and static checks:

```sh
go test -race ./...
go vet ./...
```

Fuzz the CSS parser:

```sh
go test ./internal/css -run '^$' -fuzz=FuzzParseDoesNotPanic
```

## Roadmap

The next broad milestones are richer CSS values and formatting contexts, deeper
HTML tree-builder coverage, and an interactive window backend with navigation,
hit testing, input, and scrolling. The existing renderer is structured so each
of those capabilities can extend the pipeline instead of replacing it.

The staged CSS architecture and its V8/DOM invalidation boundary are tracked in
[`docs/css-engine-roadmap.md`](docs/css-engine-roadmap.md).
