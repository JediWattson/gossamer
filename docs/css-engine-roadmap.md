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
  geometry, layout, display-list construction, and paint.
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

- Introduce CSS Syntax token and component-value types for identifiers,
  strings, numbers, dimensions, percentages, URLs, functions, blocks, and
  delimiters.
- Preserve source spans for diagnostics and CSSOM serialization.
- Migrate declarations, selectors, media queries, and value parsers away from
  independent byte scanners one grammar at a time.
- Add escaped identifiers and code-point preprocessing.

Acceptance: parser recovery and serialization are covered by conformance
fixtures and fuzzing, and existing selectors retain their behavior through the
shared tokenizer.

### 3. Explicit cascade and computed style

Current foundation: cascade and computed-value calculation live in
`internal/style`; browser pages cache versioned, stable-ID snapshots; and
render consumes the exact snapshot retained by each frame. The current 34
longhands share one registry for inheritance, validation, computation, copying,
serialization, and invalidation metadata, and `all` participates in the full
existing layer/importance cascade without resetting custom properties.
Provenance and the remaining origins are still pending.

- Move cascade and computed-value ownership out of `internal/render`.
- Model user-agent, user, presentational-hint, author, and inline origins,
  including normal and important order.
- Extend CSS-wide handling and the central property registry with every new
  longhand while preserving `all` exclusions such as `direction`,
  `unicode-bidi`, and custom properties.
- Cache parsed embedded and inline declaration blocks without changing
  observable cascade order.

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

- Load recursive `@import` rules in specified order with URL, layer, supports,
  and media context.
- Add `@supports`, nested and anonymous layers, `@font-face`, keyframes, and
  CSS nesting.
- Track stylesheet identity and generations so dynamic `<style>` and `<link>`
  changes cannot reuse stale resources.

Acceptance: the browser owns an ordered stylesheet graph and can replace one
sheet without losing source provenance.

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
The exposed values are still the layout-independent subset represented by
`internal/style`; box-dependent resolved-value serialization and
pseudo-element style slots are pending.

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
