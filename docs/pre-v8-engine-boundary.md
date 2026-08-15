# Pre-V8 engine boundary

Gossamer now has the complete engine-independent socket required before a
stock V8 adapter is introduced. Gossamer owns page order, external completion,
input, timers, engine entry, microtask checkpoints, DOM identity, invalidation,
and rendering. An engine supplies source evaluation and opaque callback
execution; it does not supply the browser event loop.

No V8 dependency, C++ bridge, isolate, or V8 heap integration exists yet.

## Engine contracts

`internal/browser/engine.go` defines four boundaries:

- `Engine` creates one `JSRealm` for each document and owns engine-wide
  shutdown.
- `JSRealm` evaluates `ScriptSource`, dispatches normalized input, invokes
  opaque `ValueHandle` callbacks, and performs an explicit microtask
  checkpoint.
- `Host` is valid only for the current Page task. It reads or changes text
  through `NodeHandle`, queues callbacks or Realm microtasks, and creates or
  clears timers.
- `ValueHandle` is engine-owned numeric identity. Go transports it without
  inspecting engine storage or retaining an engine pointer.

Navigation creates a replacement `JSRealm` when it publishes a new Document.
The old realm and its timers are closed before initial script evaluation
begins. All queued callbacks, microtasks, timer firings, and render
invalidations carry the `DocumentGeneration` that scheduled them. Work from an
old document becomes a no-op even if it was already runnable when navigation
committed.

## Script sequence

When scripting is enabled by `Browser.NewWithEngine`, navigation currently
uses this deterministic initial sequence:

```text
fetch + parse + index document off-Realm
  -> publish document completion task
  -> load rendered stylesheets/images off-Realm in DOM order
  -> publish each resource completion task
  -> load inline/external classic scripts off-Realm in DOM order
  -> publish one evaluation task per script
  -> JSRealm.Evaluate
  -> JSRealm.DrainMicrotasks
  -> queue final navigation render
  -> publish Frame and complete navigation
```

External script requests use the script HTTP destination and require a
supported JavaScript MIME type. Script transport, MIME, or evaluation failures
increment navigation telemetry but do not prevent sibling scripts or the Page
from rendering.

This first sequence intentionally executes supported classic scripts after
parse and initial rendered-resource loading. Parser-blocking execution,
`async`, `defer`, modules, dynamic script insertion, and full HTML scripting
semantics remain later browser features; the engine scheduling boundary does
not depend on them.

With no Engine, Page preserves the existing no-JavaScript behavior and does
not fetch script resources.

## Input sequence

```text
platform input
  -> browser-owned completion envelope
  -> Page task queue
  -> hit test current Frame
  -> convert renderer pointer to (DocumentGeneration, NodeID)
  -> JSRealm.DispatchEvent
  -> engine microtask checkpoint
  -> queued opaque callback
  -> JSRealm.Invoke in later Page task
  -> Host.SetText
  -> generation-bound render invalidation
  -> render task
  -> updated Frame
```

The fake-engine integration test proves this complete sequence and verifies
the external browser-to-queue publication, internal task-to-queue publications,
queue-to-task transfers, and final object destruction.

## Timer sequence

Timer registration creates a task-local shadow callback object and publishes
one claim to the Realm. The registering task then releases its region:

```text
registering task -> Realm retention -> timer fires
  -> Realm transfers claim to Page queue
  -> queue transfers claim to firing task
  -> JSRealm.Invoke
  -> task-region release destroys callback object
```

Cancellation releases the Realm claim without firing. Navigation and Page
close cancel all retained timers. A timer that reaches the queue immediately
before document replacement is rejected by its generation check.

## Microtask ownership

There are two explicit layers:

1. `JSRealm.DrainMicrotasks` is the engine checkpoint. A V8 adapter will use
   explicit microtask policy and call V8's checkpoint only here.
2. Gossamer's Realm microtask queue drains after the Page task returns. Host
   callbacks placed there cross the same ownership boundary as other queued
   work.

This keeps the ordering rule `Gossamer schedules V8`; V8 never runs an
independent browser loop.

## Fake engine

`internal/browser/fake` implements the same contracts with callback maps and
an optional source evaluator. It has no JavaScript parser. Its purpose is to
prove that the browser can:

- replace document realms on navigation;
- evaluate inline and external sources in the declared order;
- dispatch a stable-handle click;
- queue and invoke opaque callbacks;
- mutate the DOM only through Host;
- run a microtask checkpoint;
- retain and fire timers; and
- render the resulting document with a complete ownership trace.

## The next step is V8

The next implementation step requires setting up stock V8 behind this socket:

1. Choose and pin the V8 distribution/build for supported platforms.
2. Add a thin C++/C ABI implementing `Engine` and `JSRealm`.
3. Keep C ABI values numeric or owned byte buffers; never retain Go pointers
   in V8 or pass raw V8 pointers into Go.
4. Start with one isolate per Page `JSRealm` so independent Gossamer Realms can
   remain concurrent. The engine owns V8 platform initialization.
5. Configure explicit V8 microtask policy and run checkpoints only from
   `JSRealm.DrainMicrotasks`.
6. Keep JavaScript objects and callback handles in the V8 heap under V8 GC.
   Keep Go browser objects under Gossamer queue/region ownership.
7. Map wrapper identity with stable `NodeHandle` and opaque `ValueHandle`
   tables, then add weak-wrapper telemetry without changing ownership rules.

That adapter setup is deliberately not part of the current implementation.
