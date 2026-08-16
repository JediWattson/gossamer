# Production page boot

Milestone 34 turns navigation into a complete initial JavaScript boot sequence
without moving browser ownership into V8. Go fetches and resolves every source,
the Page queue owns lifecycle order, and stock V8 only compiles and evaluates
the pointer-free source graph supplied during an engine entry.

## Lifecycle sequence

```text
document fetch, parse, and index off-Realm
  -> commit document with readyState = loading
  -> load represented stylesheets and images
  -> start external async script loads
  -> execute blocking classic scripts
  -> readyState = interactive
  -> execute defer classics and modules in document order
  -> dispatch DOMContentLoaded at the canonical Document
  -> execute remaining async scripts as loads complete
  -> readyState = complete
  -> dispatch load at the canonical Document
  -> queue one final render
  -> publish Frame and complete navigation
```

Every evaluation and lifecycle event is a Page task. V8 microtasks drain at
the explicit engine checkpoint before the task closes. Script transport, MIME,
compile, or evaluation errors increment navigation telemetry; sibling scripts,
lifecycle completion, and final paint still proceed.

## Module boundary

The browser scans static top-level `import` and `export ... from` edges, resolves
relative or absolute HTTP(S) URLs against the final referrer URL, and fetches a
complete `ModuleGraph` outside the Realm. Each graph contains only strings:
root URL, source URL/text pairs, and referrer/specifier/resolved-URL edges.

Stock V8 compiles all supplied sources with module `ScriptOrigin` metadata,
instantiates them through a resolver backed only by those browser-provided
edges, and evaluates the root. A per-Realm V8 module cache preserves module
identity and one-time evaluation when multiple tags name the same source. The
cache is reset with the context during Realm teardown.

## Production proof

`TestStockV8BootsHTTPViteShapedProductionReactPage` serves a real HTML document,
the pinned production React 19.2.7 bundle, and a Vite-shaped `main.js`/`App.js`
module graph over a local HTTP server. It proves:

- blocking, deferred, and module code observe the expected ready states;
- `DOMContentLoaded` precedes `load`, and final paint waits for lifecycle
  completion;
- duplicate module tags share transport cache and execute the module once;
- production React commits into the Go-owned DOM and handles a delegated click;
- unmount, forced V8 GC, Page close, module-cache reset, and Realm close leave
  the native ownership ledger at zero.

## Deliberate limits

This is an initial-document loader, not full HTML scripting semantics. Scripts
run after the full document and represented render resources load rather than
pausing a streaming parser. Import maps, bare package specifiers, dynamic
`import()`, import attributes, module workers, dynamically inserted scripts,
top-level-await lifecycle blocking, and browser-exact preload/fetch priority
are not claimed. The initial `load` event targets the canonical document until
Gossamer's `Window` is a complete `EventTarget`.
