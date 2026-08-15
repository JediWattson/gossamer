# Native heap types

Gossamer's `RegionStore` uses one explicit `Ref` shape for every native heap
payload:

```text
RegionID + Slot + Generation
```

The slot carries a `HeapKind`; dereferencing through the wrong typed API fails
with `ErrTypeMismatch`. Ownership and queue operations remain properties of the
containing region, so adding a payload type cannot bypass Transfer, Publish,
Copy, Promote, stale-reference checks, or bulk region release.

## Implemented types

### String

Native strings are immutable region-owned payloads. `AllocString` clones input
text into a String slot and `DerefString` performs access and type checks.
Copying or promoting a graph clones string bytes into the destination region;
publishing or transferring a graph preserves its existing Ref identity.

Inline scalar `Value` instances cover `undefined`, `null`, booleans, and
numbers. A string used by another heap object is represented by `RefValue`, so
the ordinary object and region write barriers account for that edge.

### Object

Native Objects store a null-or-Object prototype and insertion-ordered own
properties. Property names are native String refs; equal String contents select
the same property even when they arrive through different Refs. Prototype,
name, and value references all pass through the counted write barrier.

`SetProperty`, `GetOwnProperty`, and `DeleteProperty` deliberately implement
own storage only. Prototype-chain lookup, descriptors, accessors, coercion, and
ECMAScript key ordering belong above RegionStore.

### Array

Native Arrays store a uint32 length and insertion-independent, index-sorted
sparse elements. A missing index is a hole and remains distinct from a present
`UndefinedValue`. Setting an element can extend length, deleting preserves
length, and shrinking length removes and unlinks every truncated element.

Array element references use the ordinary counted barrier. Copy and Promote
preserve holes, length, ordering, and shared aliases.

### Context

Native Contexts are lexical environments with a null-or-Context parent and
insertion-ordered String-named bindings. Declaration and initialization are
separate, so the temporal dead zone remains distinct from an initialized
`undefined`. Bindings record mutability, and assignment updates the nearest
binding in a cycle-checked parent chain.

Parent, name, and initialized value references are counted edges. Copy and
Promote preserve parent structure, binding order, initialization, mutability,
and captured aliases.

### Function

Native Functions are immutable executable descriptors. A bytecode Function
owns cloned instruction bytes, scalar-or-Ref constants, arity, an optional
native String name, and an optional captured Context. A native Function carries
the same name/environment metadata plus a nonzero opaque numeric callback ID;
it never stores a Go pointer.

Function names, environments, and Ref constants are counted edges. Copy and
Promote clone bytecode and preserve closure and constant aliasing. Execution is
deliberately deferred to the interpreter layer.

### Promise

Native Promises move exactly once from pending to fulfilled or rejected and
retain a scalar-or-Ref result. Reactions hold optional Function handlers and an
optional downstream Promise. Self-resolution and repeated settlement fail.

`DrainPromiseReactions` returns the settlement and removes retained reaction
edges without invoking callbacks. Realm scheduling remains outside
RegionStore. Copy and Promote preserve state, handled status, reactions, and
shared handler/result aliases.

### BigInt

Native BigInts are immutable canonical sign/magnitude payloads. Magnitudes are
owned big-endian bytes with no leading zero; zero has an empty magnitude and is
never negative. The Store supports checked parsing and formatting for bases 2
through 36, and base 0 parsing for explicit prefixes.

Copy and Promote clone magnitude bytes. Arithmetic and JavaScript coercion are
interpreter responsibilities rather than RegionStore mutation.

### Symbol

Native Symbols carry a nonzero semantic `SymbolID` plus an optional native
String description. Fresh Symbols are unequal even with equal descriptions.
Copy and Promote allocate a new physical Ref but preserve `SymbolID`, so moving
the payload does not change Symbol equality. Description refs remain ordinary
counted edges.

The global Symbol registry and property-key coercion remain language-runtime
services above RegionStore.

### ArrayBuffer

Native ArrayBuffers own mutable cloned byte slices. Reads return copies;
writes are bounds checked and reject published regions. Explicit detachment
releases the bytes, is idempotent, and causes later reads or writes to fail.

Copy and Promote clone attached bytes rather than sharing mutation. Detached
state is preserved, and live-byte telemetry drops immediately on detach.

### TypedArray

Native TypedArrays are immutable descriptors over an ArrayBuffer Ref. The
Store supports signed and unsigned 8/16/32-bit integers, clamped bytes, and
32/64-bit floats with alignment and range validation. Element access is
little-endian; integer writes wrap and clamped-byte writes use ties-to-even.

Multiple views share buffer mutation. Detachment is observed through every
view. Copy and Promote clone the reachable buffer and retarget the copied view,
so mutation never leaks back to the source graph. BigInt views and DataView are
deliberately separate later payloads.

### Map

Native Maps store insertion-ordered key/value entries and use SameValueZero
matching. `NaN` keys match, signed zeros match, native Strings compare by text,
BigInts compare by canonical value, Symbols compare by stable `SymbolID`, and
all other heap payloads compare by Ref identity.

Replacing a value preserves the original key and insertion position. Delete
and Clear unlink every removed key/value edge. Copy and Promote preserve order,
key semantics, and shared aliases.

### Set

Native Sets retain insertion-ordered unique values using the same SameValueZero
engine as Map. Adding an equivalent value is idempotent and preserves the
original Ref and position. Delete and Clear unlink removed members; Copy and
Promote preserve order and semantic equality.

### Date

Native Dates store mutable TimeClip milliseconds. Finite inputs within
`+-8.64e15` are truncated toward zero; non-finite or out-of-range inputs become
the invalid-date `NaN` value. Copy and Promote snapshot the numeric value, and
published Dates reject mutation.

Calendar calculations, parsing, formatting, and time zones remain language and
host-library concerns.

## Deliberate boundaries

These native payloads are not yet ECMAScript implementations. String
interning, ropes, UTF-16 indexing, coercion, property descriptors, automatic
promotion, tracing GC, packed refs, and V8 heap integration are outside this
layer.
