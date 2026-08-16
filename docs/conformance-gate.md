# Conformance, leak, and performance gate

Milestone 26 turns the browser/runtime boundary into one repeatable release
gate. It combines focused Web Platform behavior, the pinned production React
fixture, forced V8 collection, cross-Realm structured clone, form navigation,
and repeated document replacement.

Run it after building stock V8:

```sh
GOCACHE=/tmp/gossamer-go-cache ./tools/v8/gate.sh
```

The gate has three layers:

1. focused Go tests cover constraint validation, successful controls, GET and
   POST submission, structured-clone transfer, `postMessage`, queue ownership,
   and stale document generations;
2. the same ownership-critical Go slices run under the race detector; and
3. stock V8 runs focused WPT-aligned DOM/EventTarget/collection/form behavior,
   the real React 19.2.7 compatibility gate, forced GC, form-to-navigation
   teardown, and the replacement stress test.

The local Web Platform subset is deliberately narrow. It exercises supported
behavior using testharness-style assertions, but it does not claim that the
complete upstream WPT runner or unsupported platform APIs pass.

## Regression budgets

The stress test performs 25 successful JavaScript `requestSubmit()` cycles and
200 additional document replacements. Every replacement creates a fresh stock
V8 Realm and Go document generation, closes the previous Realm, invalidates its
wrappers, updates history/location, and paints the new document.

The committed budgets are:

| Boundary | Budget |
| --- | --- |
| Wall time | 225 replacements complete within 20 seconds |
| V8 Realms | `RealmsCreated == RealmsClosed` after Page teardown |
| Wrapper churn | At most 2,500 created or live-at-close diagnostic wrappers |
| Async roots | Zero live V8 callbacks and event listeners at close |
| Native ownership | Zero live and zero persistent ownership-ledger objects |
| Coverage proof | At least 25 evaluations and submit events plus one major GC |

`ClosedRealms.LiveWrappers` is a diagnostic snapshot taken immediately before
isolate deletion; it is bounded but is not the post-delete ownership signal.
The Go ownership ledger is the authoritative zero-after-teardown assertion.

The 20-second wall budget is intentionally broad enough for debug/symbolized
stock V8 while still catching accidental quadratic replacement or queue
behavior. The normal full suites remain separate release checks:

```sh
GOCACHE=/tmp/gossamer-go-cache go test ./...
GOCACHE=/tmp/gossamer-go-cache go test -race ./...
GOCACHE=/tmp/gossamer-go-cache ./tools/v8/test.sh -count=1
```
