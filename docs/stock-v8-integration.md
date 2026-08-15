# Stock V8 integration

Gossamer embeds an unmodified upstream V8 build behind the engine-neutral
browser socket. The initial target is Apple Silicon and the initial purpose is
measurement: execute real JavaScript, preserve Gossamer's explicit scheduling,
observe V8's own heap, and prove isolate teardown before DOM wrappers are
introduced.

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
- The future wrapper payload is numeric `(DocumentGeneration, NodeID)`
  identity. It is not a Go pointer and ordinary JavaScript aliases do not
  change ARC.
- A `ValueHandle` will index a V8-owned persistent-function table. Publishing
  that handle to a Gossamer timer or queue is the semantic lifetime boundary.
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

## First checkpoint

The adapter currently implements the engine-independent calls required before
wrappers:

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

V8 platform initialization is process-wide. V8 documents `V8::Dispose()` as
permanent and disallows reinitialization, so closing a Gossamer `Engine` closes
all of its isolates but intentionally leaves the shared platform alive until
process exit. Isolate teardown remains complete and independently measurable.

`tools/v8/profile.sh` runs a standalone allocation workload and emits JSON for
three points: baseline, after JavaScript execution and its explicit microtask
checkpoint, and after releasing the workload followed by a forced collection.
Byte fields are raw bytes; `evaluationNanos` and `gcNanos` are integer
nanoseconds. An alternate source file can be supplied with `-script`.

```sh
tools/v8/profile.sh -iterations 5 -sampling-interval 4096
tools/v8/profile.sh -script ./workload.js
```

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

## Next wrapper milestone

DOM bindings remain intentionally absent from this checkpoint. `DispatchEvent`
and `Invoke` report that bindings are unavailable. The next milestone adds:

1. a V8 wrapper template containing numeric `NodeHandle` identity;
2. wrapper identity caching per Realm;
3. weak callbacks that remove cache entries without exposing Go pointers;
4. `ValueHandle -> v8::Global<Function>` callback storage;
5. execution-scoped host-call identifiers for text reads and writes; and
6. paired V8 heap and Gossamer region telemetry for wrapper publication and
   destruction.
