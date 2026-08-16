# DOM compatibility gate

Gossamer keeps JavaScript objects in stock V8 and browser objects in Go. The
DOM compatibility gate verifies that this split remains observable as one DOM:
canonical V8 wrappers carry numeric `NodeHandle` values, while Go owns node
identity, mutation, form state, construction regions, and queue ARC.

## Completed milestones 8-15

| Milestone | Native surface | Regression boundary |
| --- | --- | --- |
| 8. Atomic mutation kernel | `append`, `prepend`, `before`, `after`, `replaceWith`, `remove`, `replaceChildren`, typed `DOMException` failures | One Go mutation transaction and one later render publication regardless of argument count |
| 9. HTML interfaces | Dedicated form-control, template, and iframe prototypes | One canonical wrapper per `NodeHandle`, including across base and specialized interface lookups |
| 10. Form state | Dirty `value`/`checked`/`selected` state, radio coordination, select/options, form owners, reset, live collections | Current state stays in Go and collection-only reachability retains the native owner graph |
| 11. Editing | UTF-16 selections, `select`, `setSelectionRange`, `beforeinput`, native edits, `input`, composition events | Cancelable edits roll back before mutation; event order crosses one input task |
| 12. Observation | `MutationObserver` and `MutationRecord`, subtree/filter/old-value options, `takeRecords`, `disconnect` | Records come from the Go mutation journal and observer-only detached targets survive collection |
| 13. Construction | Inert `template.content`, deep template cloning, `splitText`, `normalize`, same-document import/adopt, `Range`, `TreeWalker`, `NodeIterator`, `NodeFilter` | Template ownership is a semantic lifetime edge rather than a DOM parent edge; facade-only roots survive forced GC |
| 14. React gate | Pinned production React 19.2.7 render and reconciliation over the native DOM | Controlled text, textarea, checkbox, radio, and select state; delegated input; observer delivery; keyed identity; forty churn cycles; unmount; forced GC; zero ownership after Realm teardown |
| 15. Range and selection | Cross-container partial cloning/extraction/deletion, UTF-16 character-data boundaries, text-boundary insertion, canonical document/window `Selection`, mutation-aware filtered traversal | Fully selected nodes preserve identity during extraction, selection-only detached roots survive GC, reject prunes while skip promotes descendants, and traversal cursors adjust through insertion/removal |

Run the gate against the locally built stock V8:

```sh
GOCACHE=/tmp/gossamer-go-cache ./tools/v8/test.sh -count=1
GOCACHE=/tmp/gossamer-go-cache go test ./...
```

The React test is a compatibility gate, not an alternate DOM implementation.
The bundle calls the same public V8 prototypes and browser host callbacks as
page JavaScript. All mutations therefore pass through Go's stable-ID document
and ownership barriers before a separate render task publishes a frame.

## Current limits

- `MutationObserver` delivery occurs at Gossamer's explicit post-task
  checkpoint. Ordering relative to every browser-defined microtask source is
  not yet claimed.
- Range contents now cross nested containers and UTF-16 character data.
  `surroundContents`, contextual fragments, point-comparison helpers, and full
  live boundary adjustment after unrelated DOM mutations remain future work.
- `TreeWalker` and `NodeIterator` refresh from the native mutation sequence,
  prune rejected subtrees, and promote skipped descendants. Exact Web Platform
  cursor adjustment for every complex reparenting case is not yet claimed.
- Selection currently owns at most one forward Range and supports collapse,
  select-all, deletion, string projection, and the standard range collection
  methods. Backward selections, `extend`, `setBaseAndExtent`, `modify`, and
  `containsNode` are not exposed yet.
- The Page API now performs cross-document import/adoption through explicit
  source and target Realm tasks using a pointer-free DOM encoding. Stock-V8
  Realms remain separate isolates, so a JavaScript wrapper itself cannot be
  passed directly between iframe globals; cross-Realm data uses the native
  structured-clone/`postMessage` seam and fresh target wrappers.
- This gate does not claim Shadow DOM, custom elements, full form
  validation/submission, navigation globals, geometry, accessibility, or the
  complete Web Platform test surface.

These limits are intentional compatibility boundaries. New DOM work should
add a focused Go test, a stock-V8 behavior test, a forced-GC lifetime check
when a facade can outlive its owner, and a Realm teardown assertion.
