# Native interpreter kernel

Gossamer's Go interpreter executes checked native Function descriptors against
a `runtime.TaskContext`. The native lexer, parser, compiler, and portable
Program loader provide a bounded source-to-bytecode path. The optional
`internal/nativeengine` adapter can now select this kernel at the browser Realm
boundary; stock V8 remains available for the broader web platform surface.

## Bytecode contract

Every instruction is deliberately explicit and fixed-width:

```text
1 byte opcode + uint32 A + uint32 B = 9 bytes
```

`Assemble` and `DecodeBytecode` are exact inverses. Decode rejects partial or
unknown instructions, and Function loading verifies constant indexes, branch
targets, binding flags, and exception handler kinds before execution. One
execution shares a bounded instruction counter and call-depth counter across
all nested bytecode calls.

Frames retain a Function Ref, captured Context, `this`, arguments, and an
operand stack. All frame Values are borrowed. `Frame.VisitRefs` exposes these
temporary roots for deterministic future collection checkpoints without
adding ownership claims or reference-count traffic.

## Implemented synchronous verbs

- Stack and literals: `Constant`, `Argument`, `Undefined`, `Null`, `True`,
  `False`, `Pop`, `Dup`, and `Return`.
- Objects and Arrays: allocation, descriptor-backed prototype traversal,
  accessor execution, named property get/set/delete, indexed get/set/delete,
  and length get/set.
- Contexts: load, declare, initialize, and store bindings plus `LoadThis`.
- Operators: JavaScript primitive coercion, arithmetic, remainder, unary
  increment/decrement/negate/plus, signed and unsigned bitwise operations,
  strict and loose equality, relational comparisons, String concatenation,
  logical not, and `typeof`.
- Control flow: unconditional, truthy, falsey, and nullish jumps.
- Iteration: explicit `GetIterator`, `IteratorNext`, and `IteratorClose` verbs
  over the well-known `Symbol.iterator` protocol, with compiler-generated
  finally unwinding for abrupt `for...of` consumer exits.
- Functions: bytecode calls, registered numeric native calls, captured closure
  creation, Function object metadata, and construction through the Function's
  actual `prototype` property with explicit `this`.
- Built-ins: core Object, Array, String, Error, Map, and Set constructors and
  prototype methods, including explicit deterministic iterator objects.
- Jobs: Promise reactions and `queueMicrotask` callbacks drained FIFO after
  top-level execution, with nested jobs appended to the same checkpoint. The
  browser adapter uses deferred execution so `JSRealm.DrainMicrotasks` owns the
  externally visible checkpoint.
- Exceptions: thrown Values, nested catch handlers, rethrow, and finally
  unwinding on normal and exceptional paths.

Arguments missing from a call evaluate to `undefined`. Array holes and missing
own properties also load as `undefined`. Mutations call the existing
TaskContext APIs, so every retained Ref passes through RegionStore's counted
write barrier.

Native Functions keep an opaque nonzero `NativeID` and optional RegionStore
Value captures. Captures retain resolver state through the counted graph; no
Go pointer is stored in a Function. A Go callback must be registered explicitly
with an Interpreter. A callback may return `runtime.Throw(value)` to
participate in bytecode exception unwinding; ordinary Go errors remain host
failures and are not catchable language Values.

## Lifetime boundary

Returning a Value from a Function changes no ownership. It is borrowed by its
caller in the same task. A top-level interpreter result allocated in the task's
private region dies during ordinary task release unless the caller explicitly
uses `Promote`, `Publish`, `Transfer`, or `Copy`.

The end-to-end ownership test executes a Function that returns a native Object,
rejects an unqualified microtask send, promotes the result, releases the source
task region, and reads the immutable promoted graph from the microtask.

## Deliberate boundaries

This is not an ECMAScript implementation. It does not provide implicit
promotion of bare return values, complete built-in coverage, modules,
generators, async Functions, browser method opcodes, or complete V8 replacement.
RegionStore has opaque HostObjects for browser facade identity, but invoking
browser behavior remains an embedder boundary. The Store's existing
copy-on-escape barrier still governs writes into longer-lived native owners.
