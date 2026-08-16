# Native JavaScript frontend

Gossamer's native frontend is a deliberately bounded source-to-bytecode path
over the RegionStore interpreter. It is independent from the stock V8 adapter
and is not selected by the browser engine socket yet.

```text
JavaScript source
  -> source-spanned lexer
  -> source-spanned AST
  -> immutable portable Program
  -> task-region Program loader
  -> checked native bytecode
  -> RegionStore interpreter
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
  Functions, and Promise jobs are absent;
- source evaluation is not wired into `browser.Engine` or `browser.JSRealm`.

`runtime.Interpreter.Bootstrap` currently instantiates the native global
Context and intrinsic graph inside one task region. This deliberately leaves
cross-task Realm retention to the N11 engine adapter instead of bypassing the
ownership model with hidden Go pointers.

Unsupported constructs fail with source-ranged compiler diagnostics. There is
no per-expression fallback to V8.
