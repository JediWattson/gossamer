# CSS engine roadmap

Gossamer's CSS work should grow as one pipeline rather than as independent
parsers inside selectors, layout, and DOM bindings:

```text
bytes -> tokens/component values -> rules and declarations -> selector match
      -> cascade -> computed values -> formatting contexts -> display list
```

The renderer can continue rebuilding the whole style tree while the model is
young. Correct invalidation boundaries matter now; incremental restyling does
not.

## Boundaries

- `internal/css` owns CSS syntax, rules, selectors, media conditions,
  declaration lists, and reusable component-value operations.
- `internal/style` owns typed computed values, inheritance, and the immutable
  snapshots produced by the current cascade. Its central property registry
  drives the supported longhands through cascade and CSSOM; provenance and the
  remaining origins continue to move into it.
- `internal/render` owns used-value resolution that depends on containing-block
  geometry, immutable layout snapshots, display-list construction, and paint.
- `internal/browser` owns stylesheet sets, viewport/environment changes,
  invalidation, and scheduling.
- V8 bindings mutate the DOM through browser host APIs. They do not parse CSS,
  hold computed styles, or call the renderer directly.

The DOM `style` attribute remains the source of truth for inline declarations.
Any live `CSSStyleDeclaration` wrapper must read and edit that declaration list
rather than maintaining a V8 or C++ sidecar. Keep the syntax parser's raw,
ordered declarations (including duplicates) distinct from the CSSOM declaration
block view, which resolves duplicate priorities and serialization rules.

## Milestones

### 1. Token-aware values and variables

- Parse standalone declaration lists for `style` attributes.
- Cascade case-sensitive custom properties with normal importance, layer,
  specificity, and source-order rules.
- Resolve inherited custom properties, nested `var()` fallbacks, and cycles
  before validating ordinary property values and expanding shorthands.
- Bound nesting and expansion so hostile CSS cannot consume unbounded stack or
  memory.

Acceptance: variables affect the existing color, font, box-model, and display
properties; invalid winning substitutions compute as `unset` instead of
reviving a losing declaration.

### 2. CSS Syntax foundation

Current foundation: `internal/css` now exposes bounded CSS Syntax tokens and
recursively grouped component values with code-point preprocessing, decoded
escapes, and original byte spans. Raw and CSSOM declaration parsing share that
layer, including token-correct `!important` handling and recovery, while
stylesheet and inline declaration spans flow into computed-style provenance.
Selectors and media queries now consume the shared component-value tree,
including decoded escaped identifiers and token-boundary-correct recovery.
The central style registry and every currently supported shorthand now parse
their keywords, numbers, dimensions, colors, and lists from the same decoded
component values. Legacy whitespace- and suffix-based property scanners have
been removed.

- Migrate selectors, media queries, and property-value grammars onto the shared
  representation, adding grammar-specific escaped identifiers as each moves.
- Preserve declaration spans through future stylesheet identity and caching
  work so diagnostics never rely on transient DOM or source pointers.
- Expand recovery fixtures and fuzz corpora as each grammar migrates.

Acceptance: parser recovery and serialization are covered by conformance
fixtures and fuzzing, and existing selectors retain their behavior through the
shared tokenizer.

### 3. Explicit cascade and computed style

Current foundation: cascade and computed-value calculation live in
`internal/style`; browser pages cache versioned, stable-ID snapshots; and
render consumes the exact snapshot retained by each frame. The current 34
longhands share one registry for inheritance, validation, computation, copying,
serialization, and invalidation metadata, and `all` participates in the full
layer/importance cascade without resetting custom properties. User-agent,
user, author-presentational-hint, author-sheet, and inline cascade steps now
share one origin-aware candidate model for normal and important declarations,
including origin-correct `revert` and `revert-layer` behavior for ordinary and
custom properties. Immutable snapshots retain compact, stable-ID winner
provenance and can produce deterministic per-property explanation dumps.

- Move cascade and computed-value ownership out of `internal/render`.
- Extend CSS-wide handling and the central property registry with every new
  longhand while preserving `all` exclusions such as `direction`,
  `unicode-bidi`, and custom properties.
- Cache parsed embedded and inline declaration blocks without changing
  observable cascade order; retain source identities so existing spans remain
  meaningful after caching.

Acceptance: a deterministic computed-style dump explains the winning source
for every supported property independently of layout.

### 4. Values and units

- Add typed math for `calc()`, `min()`, `max()`, and `clamp()`.
- Expand absolute, font-relative, viewport, percentage, angle, time, and
  resolution units.
- Add complete color parsing, current color, system colors, and color
  functions.
- Add URLs, images, gradients, backgrounds, radii, shadows, transforms, and
  generated content as their consumers become available.

Acceptance: computed values are typed and immutable; layout no longer reparses
raw CSS strings.

### 5. Rules and stylesheet graph

Current foundation: browser pages own connected `<style>` and stylesheet-link
entries by stable owner ID, document order, content/URL fingerprint, and
monotonic sheet generation. Embedded sheets are parsed once per content
generation; dynamic link insertion and `href`/`rel`/`type`/`disabled` changes
start document-scoped fetches, and stale async results cannot overwrite a
newer owner generation. Render continues receiving an immutable compatibility
view while fetch, URL, and lifetime policy remain in `internal/browser`.
Bounded recursive `@import` loading now preserves import order, resolves URLs,
rejects cycles, and carries layer, media, and supports context for external and
embedded sheets. Tokenized `@supports` declaration, logical, nested, and
`selector()` conditions share the style engine's actual capability boundary.
Named, dotted, nested, and anonymous layer blocks now flatten into the correct
normal/important priority order, including distinct anonymous identities across
independently parsed and imported sheets. Ordered layer-declaration events keep
`@media`, `@supports`, and interleaved import conditions in the browser's
current environment instead of freezing one parse-time order.
Common CSS nesting now expands parent selector lists with the spec's maximum
parent specificity, supports implicit descendants, leading combinators, and a
leading `&` with compound suffixes, and preserves declaration/rule source order
through nested media, supports, and layer groups.

- Add `@font-face`, keyframes, and the remaining arbitrary/multiple nesting
  selector placements such as `:is(&)`.
- Retain imported-sheet identities and source spans instead of only the current
  flattened immutable renderer view.
- Track stylesheet identity and generations so dynamic `<style>` and `<link>`
  changes cannot reuse stale resources.

Acceptance: the browser owns an ordered stylesheet graph and can replace one
sheet without losing source provenance. The ownership and loading half is now
in place; imported-sheet identity and rule-object exposure remain.

### 6. Selectors and state

- Finish escaped identifiers, pseudo-elements, `:has()`, language/direction,
  form-state, link-state, focus, hover, and active matching.
- Add bounded matching and memoization for selectors with combinator
  backtracking.
- Introduce pseudo-element style slots and generated boxes only when layout can
  consume them.

Acceptance: static and dynamic selector state uses an explicit match context;
unknown state never silently matches.

### 7. Formatting contexts

Current foundation: block overflow containers now use typed `overflow-x` and
`overflow-y` computed values. Page-owned stable-ID offsets project immutable
layout through nested scrollport clips for geometry, paint, and hit testing.
Scrollbar layout, inline fragmentation, anchoring, snapping, and advanced
formatting contexts remain pending.

- Complete inline formatting with retained inline boxes, whitespace, bidi,
  shaping, line breaking, and vertical alignment.
- Add floats, positioning, stacking contexts, overflow and scrolling, tables,
  flexbox, grid, multicolumn, and fragmentation in measured slices.
- Keep computed values separate from used values that depend on geometry.

Acceptance: each formatting context has geometry fixtures and paint-order
tests before it participates in general page rendering.

### 8. CSSOM and incremental restyle

Current foundation: `element.style` remains a live mutable view of the DOM
attribute, while `getComputedStyle()` returns a fresh read-only declaration
whose property, indexed-name, and length reads synchronously consult the
browser's current versioned Go snapshot. Same-task DOM/style mutations are
therefore observable without publishing a frame or clearing render dirtiness.
Width and the currently definite height cases synchronously consult a separate
stable-ID layout snapshot and serialize principal content-box geometry in
pixels; the next render reuses that exact layout result. Percentage heights,
ordinary inline and `display:none` elements, and the current inline-block
fallback retain their layout-independent computed values. Broader
box-dependent resolved-value serialization and pseudo-element style slots are
pending.

- Expose stylesheet, rule, declaration, and computed-style APIs through stable
  numeric browser handles.
- Classify effective DOM mutations as character-data, attribute, or child-list
  changes and record whether they touch the connected tree.
- Initially map every connected mutation to global style/layout/paint
  invalidation, coalesced at the Page task boundary.
- Add selector/property dependency indexes and subtree restyle only after
  profiling proves the full rebuild is a bottleneck.

Acceptance: `setAttribute`, `className`, `element.style`, stylesheet text, and
tree mutations are immediately visible to JavaScript and publish at most one
new frame after the current task and microtask checkpoint.

## Validation policy

Each milestone should include focused unit tests, an end-to-end DOM-to-display
list test, race coverage for touched packages, parser fuzzing where applicable,
and the complete Go test suite. Golden screenshots should be reserved for
paint behavior that cannot be expressed more precisely as geometry or display
commands.
