# Native JavaScript frontend

Gossamer's native frontend is a deliberately bounded source-to-bytecode path
over the RegionStore interpreter. The `internal/nativeengine` adapter now
implements the same browser engine socket as stock V8, so tests and embedders
can select the native path without changing Page task scheduling.

```text
JavaScript source
  -> source-spanned lexer
  -> source-spanned AST
  -> immutable portable Program
  -> task-region Program loader
  -> checked native bytecode
  -> RegionStore interpreter
  -> browser.JSRealm checkpoint
```

Compilation never allocates a `memory.Ref`. A portable `Program` owns plain
Go literals, nested Function indexes, bytecode, and instruction source spans.
Only `program.Load` may instantiate Strings and Function templates through a
`runtime.TaskContext`. Separate loads therefore receive separate Ref identity,
and ordinary task release invalidates every unpromoted loaded Ref.

## Implemented source subset

- block-scoped `let` and `const` with temporal dead zones, plus
  Function-scoped `var`;
- numbers, Strings, booleans, null, objects, sparse arrays, and `this`;
- dynamic named properties, numeric array elements, and Array `length`;
- Object prototype-chain lookup plus RegionStore-owned data and accessor
  descriptors with writable, enumerable, and configurable attributes;
- task-local native intrinsics with canonical Object, Function, Array, and
  Error-family prototypes, constructor metadata, and global bindings;
- `Object.create`, `defineProperty`/`defineProperties`, descriptor inspection,
  prototype inspection and mutation, `Object.keys`, plus Array `push`, `pop`,
  `join`, and `slice`;
- String construction and core inspection/transformation methods; Array
  callback/search methods; Map and Set constructors, mutation, lookup, size,
  callback methods, and the canonical Set `keys`/`values` alias;
- explicit Array, String, Map, and Set iterator objects whose `next()` method
  returns ordinary `{value, done}` Objects in deterministic native order;
- `for...of` through the well-known `Symbol.iterator` protocol, including
  one-time `next` capture, custom and overridden iterables, and `IteratorClose`
  for abrupt consumer exits;
- array spread through that same iterator protocol, including custom iterable
  ordering and iterator-result failure behavior shared with stock V8;
- ordered object-literal spread and computed data-property keys, including
  enumerable String and Symbol keys and getter evaluation;
- object binding declarations with static keys, aliases, and defaults evaluated
  only for undefined property values;
- lazy generator Functions for the production `for...of` shape with direct or
  conditional yields, self-iterable iterator objects, and `return()` forwarding
  to the source iterator;
- Promise construction, `resolve`, `reject`, ordered iterable `all`, `then`, and
  `catch`, including chained fulfillment/rejection and Promise adoption, plus FIFO
  `queueMicrotask` callbacks drained at the native execution checkpoint;
- async Function declarations and expressions whose body is empty or a single
  return statement, including multiple nested `await` expressions lowered to
  Promise reactions backed by ordinary RegionStore closures;
- assignment, deletion, and prefix/postfix identifier or property updates;
- optional property/computed access, optional receiver-preserving method calls,
  direct optional calls, and contiguous chain short-circuiting;
- numeric, bitwise, strict comparison, logical, nullish, and conditional
  expressions, plus coercive arithmetic, relational comparison, unary `+`,
  and loose equality;
- `if`, `while`, `break`, and `continue`;
- Function declarations and expressions, lexical capture, calls, recursion,
  construction, and explicit `this` for constructors and method receivers;
- member access and further calls on freshly constructed values;
- default values on ordinary and parenthesized arrow Function parameters,
  evaluated in source order with access to earlier parameters;
- object and Array binding patterns in ordinary and arrow Function parameters,
  lowered in source order before the Function body;
- `return`, `throw`, `try`, `catch`, and `finally`, with general abrupt
  completions routing return, throw, break, and continue through nested
  finally blocks;
- native ReferenceError, TypeError, and RangeError values for classified
  language failures, routed through the same `catch`/`finally` machinery.
- static ES modules with default, named, namespace, and side-effect imports;
  local, indirect, namespace, and star exports; cyclic graph linking; immutable
  live import bindings; canonical namespace objects; strict top-level and
  Function `this`; canonical null-prototype `import.meta` objects with host
  initialized URLs and canonical `resolve()` results; literal dynamic imports
  with Promise results; and per-Realm
  evaluation and failure caching.

Every compiled Function invocation and ordinary block enters a fresh native
Context whose parent is its captured or enclosing Context. Catch bindings enter
their own Context. Function and `var` bindings are instantiated before body
execution; lexical bindings are declared before their block executes and stay
uninitialized until their declaration. Exception and completion routing
restore operand-stack and lexical-scope depth before entering a handler or loop
target, so a returned closure can retain a parameter, block, or catch binding
without keeping an interpreter frame alive.

## Deliberate boundaries

This is not yet an ECMAScript-compatible engine. In particular:

- Annex B block-Function compatibility and the full global-environment split
  are not implemented; Function declarations use the containing Function or
  script scope;
- Arrays retain canonical sparse indices and `length` alongside ordinary named
  properties and an Array prototype; the broader Array method surface remains
  intentionally incomplete;
- primitive coercion includes Boolean, Number, String, null, and undefined;
  Object coercion uses explicit or inherited `valueOf`/`toString` hooks;
- optional calls on an already-resolved method (`object.method?.()`) remain
  outside the current optional-chain subset;
- full BigInt and UTF-16 edge semantics,
  computed or script-level dynamic import, import attributes, general generator
  control flow, `yield*`, generator `next(value)`/`throw()` resumption, async
  generators, multi-statement async Function bodies, async arrows, top-level
  `await`, thenable assimilation,
  the remaining Promise combinators/finally, and browser-level
  unhandled-rejection reporting
  are absent;
- the native browser facade is intentionally smaller than the stock V8 facade:
  `Event` and `CustomEvent` are constructible and dispatch synchronously;
  canonical Node wrappers, live same-object child collections, MutationObserver,
  layout and style reads, and heap-backed Range/Selection facades are present,
  but many less common Web interfaces remain absent and tag-name lookup is a
  snapshot rather than a live collection.

## N11 browser Realm adapter

`runtime.Interpreter.Bootstrap` still instantiates the native global Context
and intrinsic graph in task-owned storage. At the browser checkpoint, the
native adapter copies every intrinsic root and its reachable global graph into
one Realm-owned region. The next browser task explicitly copies that graph
back into task-owned storage before evaluation. After native jobs drain, the
new graph replaces the previous Realm snapshot and the task copy is released
in bulk by the existing executor.

This copy-in/copy-out boundary keeps mutable memory under exactly one owner,
does not turn a borrowed `Ref` into an ownership claim, and lets unused loaded
Programs disappear with their task. Global lexical bindings, closures, and
intrinsic mutations survive because they remain reachable from the copied
roots. Page or navigation teardown destroys the retained region before the
runtime Realm closes.

The adapter calls `Interpreter.ExecuteWithoutCheckpoint` during
`JSRealm.Evaluate`, then drains Promise reactions and `queueMicrotask` jobs only
from `JSRealm.DrainMicrotasks`. Jobs may append more jobs and are drained FIFO
at that existing browser-visible checkpoint. Direct interpreter callers retain
the N10 convenience behavior: `Interpreter.Execute` still includes its own
checkpoint.

Separate classic-script compilations allow unresolved global names and defer
their lookup to the persistent native global Context. The ordinary standalone
`compiler.Compile` API remains closed-world and continues to reject unknown
bindings at compile time.

Unsupported constructs fail with source-ranged compiler diagnostics. There is
no per-expression fallback to V8.

## N12 native browser bindings

The native adapter now binds the first practical browser surface directly to
the Go-owned DOM. `window`, `self`, and `document` are ordinary RegionStore
Objects with private immutable HostObject identity records. Node handles encode
the document generation and stable `NodeID`; no Go pointer enters the native
heap. A RegionStore Map reachable from the global Context provides one
canonical wrapper per handle across source evaluations, task copies, and
microtask checkpoints.

The wrapper prototypes expose document root traversal and element creation;
node parent, sibling, child, text, insertion, removal, containment, and cloning
operations; element attributes, IDs, selectors, matching, and closest-ancestor
queries; live same-object `childNodes` and `children` collections; and snapshot
`querySelectorAll()` results. Range and the Realm-canonical Selection keep
their boundary wrappers in the traced RegionStore graph while Go owns clone,
extract, delete, insertion, and UTF-16 text mutation. All reads and mutations cross the
execution-scoped `browser.Host`, so the indexed Go Document remains the source
of truth and render invalidation follows the existing Page task boundary.

Timer functions use opaque numeric `browser.ValueHandle` identities. The Go
timer queue transports only that number and its own HostObject lifetime record;
the JavaScript Function stays in a hidden RegionStore callback Map. Firing a
timer copies the Realm graph into the callback task, resolves the remapped
Function by handle, consumes the entry once, and drains native jobs at the
normal `JSRealm.DrainMicrotasks` checkpoint. `clearTimeout()` removes both the
Page-owned timer record and the Function root.

`addEventListener()` and `removeEventListener()` keep target, type, capture,
once, and passive metadata in the adapter while listener Functions remain in
that same RegionStore callback graph. Browser input walks stable Go DOM parent
handles and synchronously invokes capture, target, and bubble listeners with
the canonical wrapper as `this`. The native Event value exposes normalized
input fields, cancellation state, current target, phase, and propagation
controls. Listener registration also uses the browser's independent event
target lifetime claim, so a detached target cannot disappear while it still
has a listener.

Script-created `Event` and `CustomEvent` values use that same dispatch path.
They retain type, initialization flags, cancellation state, target identity,
and `CustomEvent.detail` in the RegionStore graph, so an event stored on the
Realm global can cross a task checkpoint and still dispatch synchronously.
`dispatchEvent()` returns false exactly when a cancelable listener prevented
the default action; constructed events remain untrusted.

At deterministic checkpoints the adapter evacuates canonical wrapper identity,
MutationObserver callbacks, and non-window event-listener roots before native
collection. It restores only entries whose owning wrapper survived and asks
the Page to release dead Go DOM and event-target lifetime claims. Realm close
still releases the complete wrapper, callback, listener, timer, observer, and
intrinsic graph in bulk; tests require physical heap and semantic ownership
counts to return to zero.

## Production module navigation boundary

Strand implements `browser.JSModuleRealm` over browser-resolved static module
graphs. The browser pipeline fetches, MIME-checks, scans, URL-resolves, and
caches the complete pointer-free graph in Go. Strand compiles portable module
records, creates one RegionStore Context per canonical URL, links imports to
immutable indirect bindings, constructs namespace objects over live getters,
and evaluates the dependency graph once per Realm. Instantiation declares every
module local before dependency evaluation: `var` starts as undefined, hoisted
Functions close over the module environment immediately, and lexical bindings
retain their temporal dead zone until their declaration executes. Cycles link
before evaluation; cached link or evaluation failures are replayed without
recompiling or executing the module again. Graphs rooted at another URL reuse
already compiled dependency records by canonical source identity. Each
canonical module namespace is a sorted,
null-prototype, non-extensible object whose immutable prototype and live export
properties survive RegionStore copying and reject script mutation. Module code
runs with undefined top-level and ordinary-Function `this`, rejects writes to
undeclared bindings, and receives one mutable, null-prototype `import.meta`
object per canonical module. Its enumerable `url` property is initialized from
the browser-resolved module URL and repeated `import.meta` expressions in that
module return the same object.

The production gate is a normal split Vite Solid build: its application entry
imports a separately emitted Solid runtime chunk. The shared gate runs the same
graph under Strand and stock V8 and verifies per-resource fetch caching,
one-time module evaluation, ready-state and lifecycle ordering, Go-dispatched
interaction, reactive disposal, forced collection, and zero surviving
ownership after Page teardown. Literal dynamic imports are prefetched into the
pointer-free graph by Go and evaluated lazily by the engine with canonical
namespace identity. Computed dynamic imports that were not prefetched, import
attributes, top-level `await`, and network loading from inside the engine remain
out of scope.

A second independently built Solid taskboard gate adds a four-chunk graph with
an async resource fetcher, literal dynamic imports, a generator-backed filter,
Suspense fallback release, duplicate entry tags, and Go-dispatched filter
interaction. Strand precompiles every emitted chunk, evaluates each canonical
module at most once, and matches the same rendered and teardown result as stock
V8. Its lazy details interaction also traverses Vite's emitted preload helper,
including `Promise.all()`, static tag-name lookup, `String.endsWith()`, canonical
`import.meta.resolve()`, and the final dynamic module import.

The React gates use the unmodified React 19.2.7 production runtime. The first
executes initial render, state updates, delegated click dispatch, effect cleanup,
and unmount using Strand's native Promise and `queueMicrotask` implementation.
The second is a five-chunk Vite application with static imports, async data,
lazy Suspense details, duplicate entry tags, and teardown. Both gates run the
same artifacts and assertions under Strand and stock V8.
