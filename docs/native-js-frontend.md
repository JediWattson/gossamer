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

- `let`, `const`, and first-pass `var` declarations;
- numbers, Strings, booleans, null, objects, sparse arrays, and `this`;
- own named properties and numeric array elements;
- assignment, deletion, prefix/postfix identifier updates;
- numeric, bitwise, strict comparison, logical, nullish, and conditional
  expressions;
- `if`, `while`, `break`, and `continue`;
- Function declarations and expressions, lexical capture, calls, recursion,
  construction, and explicit `this` for constructors;
- `return`, `throw`, `try`, `catch`, and `finally`, with general abrupt
  completions routing return, throw, break, and continue through nested
  finally blocks.

Every compiled Function invocation enters a fresh native Context whose parent
is its captured Context. Catch bindings enter their own Context. Exception and
return routing restore operand-stack and lexical-scope depth before entering a
handler, so a returned closure can retain a parameter or catch binding without
keeping an interpreter frame alive.

## Deliberate boundaries

This is not yet an ECMAScript-compatible engine. In particular:

- declarations are source ordered rather than fully hoisted;
- ordinary blocks do not yet create lexical Contexts;
- `var` currently follows the same declaration timing as `let`;
- property access is own-property access, and computed access is a numeric
  Array index;
- method calls do not yet preserve a receiver as `this`;
- unary plus and coercive equality are rejected;
- prototype lookup, descriptors, accessors, coercion, regex and template
  literals, modules, generators, async Functions, and Promise jobs are absent;
- source evaluation is not wired into `browser.Engine` or `browser.JSRealm`.

Unsupported constructs fail with source-ranged compiler diagnostics. There is
no per-expression fallback to V8.
