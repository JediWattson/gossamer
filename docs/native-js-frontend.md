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
- `Object.create`, descriptor definition/inspection, prototype inspection and
  mutation, `Object.keys`, plus Array `push`, `pop`, `join`, and `slice`;
- String construction and core inspection/transformation methods; Array
  callback/search methods; Map and Set constructors, mutation, lookup, size,
  and callback methods;
- explicit Array, String, Map, and Set iterator objects whose `next()` method
  returns ordinary `{value, done}` Objects in deterministic native order;
- Promise construction, `resolve`, `reject`, `then`, and `catch`, including
  chained fulfillment/rejection and Promise adoption, plus FIFO
  `queueMicrotask` callbacks drained at the native execution checkpoint;
- assignment, deletion, and prefix/postfix identifier or property updates;
- numeric, bitwise, strict comparison, logical, nullish, and conditional
  expressions, plus coercive arithmetic, relational comparison, unary `+`,
  and loose equality;
- `if`, `while`, `break`, and `continue`;
- Function declarations and expressions, lexical capture, calls, recursion,
  construction, and explicit `this` for constructors and method receivers;
- `return`, `throw`, `try`, `catch`, and `finally`, with general abrupt
  completions routing return, throw, break, and continue through nested
  finally blocks;
- native ReferenceError, TypeError, and RangeError values for classified
  language failures, routed through the same `catch`/`finally` machinery.

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
- full BigInt/Symbol and UTF-16 edge semantics, regex and template literals,
  `for...of`, well-known `Symbol.iterator`, modules, generators, async
  Functions, `await`, thenable assimilation, Promise combinators/finally, and
  browser-level unhandled-rejection reporting are absent;
- native `DispatchEvent` and opaque callback `Invoke` are not implemented yet;
  the adapter currently owns source evaluation and native microtask checkpoints
  while browser DOM APIs remain on the stock V8 path.

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
