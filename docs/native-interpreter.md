# Native interpreter kernel

Gossamer's first Go interpreter executes hand-assembled native Function
descriptors against a `runtime.TaskContext`. It proves that executable frames
can use RegionStore values without inventing a second lifetime model. Stock V8
remains the browser's JavaScript engine; this kernel does not parse or execute
JavaScript source.

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
- Objects and Arrays: allocation, own property get/set/delete, indexed
  get/set/delete, and length get/set.
- Contexts: load, declare, initialize, and store bindings plus `LoadThis`.
- Operators: numeric arithmetic, remainder, unary increment/decrement/negate,
  signed and unsigned bitwise operations, strict equality, numeric relational
  comparisons, logical not, String concatenation, and `typeof`.
- Control flow: unconditional, truthy, falsey, and nullish jumps.
- Functions: bytecode calls, registered numeric native calls, captured closure
  creation, and construction with explicit `this`.
- Exceptions: thrown Values, nested catch handlers, rethrow, and finally
  unwinding on normal and exceptional paths.

Arguments missing from a call evaluate to `undefined`. Array holes and missing
own properties also load as `undefined`. Mutations call the existing
TaskContext APIs, so every retained Ref passes through RegionStore's counted
write barrier.

Native Functions keep only an opaque nonzero `NativeID` in RegionStore. A Go
callback must be registered explicitly with an Interpreter. A callback may
return `runtime.Throw(value)` to participate in bytecode exception unwinding;
ordinary Go errors remain host failures and are not catchable language Values.

## Lifetime boundary

Returning a Value from a Function changes no ownership. It is borrowed by its
caller in the same task. A top-level interpreter result allocated in the task's
private region dies during ordinary task release unless the caller explicitly
uses `Promote`, `Publish`, `Transfer`, or `Copy`.

The end-to-end ownership test executes a Function that returns a native Object,
rejects an unqualified microtask send, promotes the result, releases the source
task region, and reads the immutable promoted graph from the microtask.

## Deliberate boundaries

This is not an ECMAScript implementation. It does not provide source parsing,
automatic coercion, prototype-chain lookup, descriptors, accessors, implicit
promotion of bare return values, weak edges, asynchronous opcodes, structured
clone, browser facade bindings, or V8 replacement. The Store's existing
copy-on-escape barrier still governs writes into longer-lived native owners.
