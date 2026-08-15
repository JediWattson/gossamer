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

## Deliberate boundaries

These native payloads are not yet ECMAScript implementations. String
interning, ropes, UTF-16 indexing, coercion, automatic promotion, tracing GC,
packed refs, and V8 heap integration are outside this layer.
