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

Current foundation: length-bearing properties accept bounded, typed
`calc()`, `min()`, `max()`, and `clamp()` expressions over px, em/rem,
percentages, and viewport units. Simple arithmetic is folded without an
allocation; mixed expressions remain immutable in computed-style snapshots,
serialize deterministically through CSSOM, and resolve only in layout once the
percentage base and viewport are known. Invalid dimensions, division by zero,
missing binary-operator whitespace, percentage border widths, and expressions
beyond the independent math-node budget fail closed. Absolute physical units
compute to CSS pixels; small/large/dynamic and logical viewport units map to
the current single horizontal viewport model, with `vmin`/`vmax` retained as
typed terms. Color values now cover the SVG/CSS named set, all hex forms, and
legacy/modern `rgb()`/`rgba()`, `hsl()`/`hsla()`, and `hwb()` syntax with
token-correct comma, whitespace, slash, angle, clamping, and alpha behavior.
The typed `currentcolor` reference resolves against the final element color
for backgrounds and borders, while `color: currentcolor` resolves from the
parent; CSS-wide inheritance preserves the reference rather than freezing an
ancestor's paint value.
The central property registry and layout now also carry `box-sizing`,
`min-height`, and `max-height`; block and replaced content apply constraints to
the selected content or border box, and layout-backed computed width/height
reads preserve the corresponding resolved box semantics.
Inherited `visibility` now suppresses an element's paint without discarding
geometry or preventing a visible descendant override. The typed `white-space`
longhand drives CSS-space collapsing, preserved segment breaks and spaces,
nowrap behavior, and soft-wrap opportunities for normal, pre, pre-wrap,
pre-line, and break-spaces modes; the built-in UA sheet applies `pre` to
`<pre>` elements.
Inherited `font-style` now remains typed through computed style, layout,
display-list commands, and rasterization. The bundled Go font family selects
real italic and bold-italic faces; `oblique` deliberately shares the italic
fallback until angle-aware synthesis or a dedicated face is added.
Inherited `font-family` keeps a normalized CSSOM fallback list and selects the
first bundled family it can satisfy. Sans/system/serif fall back to the Go sans
faces, while `monospace` and `Go Mono` select distinct regular, bold, italic,
and bold-italic faces for both measurement and rasterization.

- Extend CSS math beyond the current length-percentage subset as number,
  angle, time, resolution, transform, and color consumers arrive.
- Expand font-relative units and add angle, time, and resolution consumers.
- Add system colors, wide-gamut `color()`, Lab/LCH/OKLab/OKLCH, and
  relative/color-mix functions.
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

Current foundation: selector matching accepts an explicit non-retained match
context. Nested logical selectors and nth-child filters share it; hover and
active propagate through the inclusive ancestor chain, focus/focus-visible and
focus-within use the Page's stable focused node and modality bit, URL fragments
drive `:target`, and Page session history distinguishes `:link` from
`:visited`.
Pointer and focus transitions advance the Page style revision, invalidate the
cached snapshot, and coalesce a render at the existing task boundary even when
scripting is disabled. HTML `:checked` consumes live dirty input checkedness
and option selectedness rather than stale content attributes. `:enabled` and
`:disabled` implement select/optgroup/option propagation and disabled fieldset
first-legend exceptions, while `:required` and `:optional` respect input-type
applicability. Form mutations advance the document version, so style reads,
selector queries, and task-boundary rendering share one coherent state.
`:has()` parses its unforgiving relative-selector list from shared component
values, rejects nested relational selectors, and evaluates descendant, child,
adjacent, and general-sibling relations with iterative backtracking. One shared
operation budget bounds combinator and relational work in each top-level
evaluation and fails closed, including through negation; `@supports
selector()` uses the same parser.
Linguistic selectors now parse their Selectors 4 argument grammars from shared
component values. `:lang()` inherits HTML/SVG `lang` and foreign `xml:lang`,
canonicalizes recognized BCP 47 tags into extlang form, and applies RFC 4647
extended filtering while distinguishing explicitly untagged content.
`:dir()` follows HTML directionality rather than CSS's `direction` property,
including valid `dir` states, inherited direction, `bdi`, telephone defaults,
first-strong `dir=auto`, excluded isolated subtrees, and live input/textarea
values. Language ancestor walks and Unicode direction scans consume the same
fail-closed selector operation budget. Browser navigation supplies a single
protocol-level default language through match context without letting the
selector package reach outside a coherent DOM read.
Visited-link state resolves hyperlinks against the first HTML base URL, ignores
fragments, canonicalizes default ports, and passes only stable visited NodeIDs
into style snapshots. Gossamer deliberately partitions this state to
same-origin destinations in the current Page's own session history, so selector
APIs and computed style cannot probe arbitrary cross-origin browsing history.
History and href/base mutations invalidate style without publishing an early
frame.
HTML mutability selectors now distinguish the input types to which `readonly`
applies, actual disabledness, textarea state, and inherited `contenteditable`
editing hosts. `:placeholder-shown` follows applicable input/textarea controls
and their dirty live values; ancestor and textarea scans share the fail-closed
selector operation budget.
`:default` now distinguishes markup default checkedness/selectedness from live
state and finds the first submit button for an HTML form owner, including
external `form` associations and tree-order changes under the same budget.
`:indeterminate` observes the live `HTMLInputElement.indeterminate` state for
checkboxes, radio groups with no checked member, and progress elements without
a `value` attribute. Checkbox activation clears indeterminateness before event
dispatch and restores it when canceled; the live IDL setter, selector queries,
and computed-style invalidation share the versioned DOM state.
`:valid` and `:invalid` now share one bounded constraint evaluator with form
submission. It covers the engine's implemented required, checked/selected,
email/URL, numeric range, pattern, and text-length constraints; bars disabled,
readonly, hidden/button, and datalist-contained controls; and aggregates
invalid owned controls onto forms plus invalid descendants onto fieldsets.
Live control mutations invalidate selector queries and computed styles without
publishing an early frame.
`:user-valid` and `:user-invalid` reuse that evaluator for input, select, and
textarea controls after committed native interaction or a request-submission
attempt. Script value and checkedness setters do not fabricate user
interaction; reset clears it; native text editing remains pending until change
or blur; and `requestSubmit()` marks all owned controls before validation,
including the `novalidate` path. The state is versioned with the DOM so retained
selector queries and computed-style declarations observe it synchronously.
`:in-range` and `:out-of-range` share those candidate rules and parse the HTML
number, range, date, month, ISO week, time, and local date-time value spaces.
They honor valid one-sided or two-sided limits, reversed periodic time ranges,
empty-value semantics, range-input defaults/sanitization, and live value or
attribute mutations.
`::before` and `::after` now target generated subjects without leaking into
ordinary element matching. Sparse pseudo style slots inherit from the
originating element, participate in the same origin/layer/custom-property
cascade, and expose live computed values through the existing browser/V8
CSSOM boundary. Typed `content` supports bounded strings and `attr()` values;
layout projects generated inline text and block boxes in tree order while
retaining the originating element for geometry, paint ownership, and hit
testing.

- Finish the remaining pseudo-elements and stateful selector grammar.
- Add memoization and selector/property dependency indexes after profiling the
  now-bounded combinator backtracking paths.
- Extend generated content to counters, quotation marks, images, and retained
  empty inline boxes only with matching layout consumers.

Acceptance: static and dynamic selector state uses an explicit match context;
unknown state never silently matches.

### 7. Formatting contexts

Current foundation: block overflow containers now use typed `overflow-x` and
`overflow-y` computed values. Page-owned stable-ID offsets project immutable
layout through nested scrollport clips for geometry, paint, and hit testing.
Computed display retains separate outer and inner roles. `inline-block` and
`inline-flex` now create atomic inline formatting roots with bounded cached
intrinsic measurement, shrink-to-fit auto width, baseline participation,
box-model paint, stable-ID geometry, and flow-ordered hit testing. Block
children no longer escape those roots, and live computed width/height reads use
their retained geometry. Relative, absolute, and fixed positioning, basic
stacking order, and single-line row/column flex layout also have tested
foundations. Scrollbar layout, inline fragmentation, anchoring, snapping, and
advanced formatting contexts remain pending.

- Complete inline formatting with retained inline boxes, whitespace, bidi,
  shaping, line breaking, and vertical alignment.
- Correct percentage heights, margin collapsing, and justified line layout.
- Extend positioning, stacking contexts, overflow/scrolling, and Flexbox in
  measured slices; add floats as a dedicated compatibility slice.
- Keep computed values separate from used values that depend on geometry.

Acceptance: each formatting context has geometry fixtures and paint-order
tests before it participates in general page rendering.

### 8. CSSOM and invalidation

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
box-dependent resolved-value serialization remains pending. `::before` and
`::after` use sparse stable-ID style and layout slots, so retained computed
declarations observe generated content changes without publishing an early
frame; other pseudo-elements remain empty.

- Expose stylesheet, rule, declaration, and computed-style APIs through stable
  numeric browser handles.
- Classify effective DOM mutations as character-data, attribute, or child-list
  changes and record whether they touch the connected tree.
- Initially map every connected mutation to global style/layout/paint
  invalidation, coalesced at the Page task boundary.

Acceptance: `setAttribute`, `className`, `element.style`, stylesheet text, and
tree mutations are immediately visible to JavaScript and publish at most one
new frame after the current task and microtask checkpoint.

### 9. Tables, Grid, and advanced formatting

- Implement CSS table wrappers, anonymous table boxes, row/column sizing, and
  HTML table fixup before exposing table geometry broadly.
- Add Grid track sizing and placement, followed by intrinsic sizing and
  alignment.
- Add multicolumn and fragmentation only after their block/inline consumers
  are retained and testable.
- Keep each formatting context as an independent computed-to-used vertical
  slice with geometry and paint-order fixtures.

Acceptance: tables and Grid agree across anonymous-box construction, intrinsic
sizing, placement, overflow, hit testing, and display-list order before they
participate in general page rendering.

### 10. Incremental restyle and performance

- Cache parsed rules and declarations by stable sheet/element generation.
- Add selector candidate indexes and property/state dependency tracking.
- Classify connected mutations, then introduce subtree restyle against the
  full-document rebuild as an executable correctness oracle.
- Add incremental layout and paint damage only after style invalidation is
  proven equivalent.
- Introduce parallel work solely where profiles show independent, bounded
  tasks with a measurable gain.

Acceptance: incremental results are byte-for-byte equivalent to a full rebuild,
benchmarks demonstrate the improvement, and adversarial CSS/DOM inputs retain
hard memory and operation limits.

## Validation policy

Each milestone should include focused unit tests, an end-to-end DOM-to-display
list test, race coverage for touched packages, parser fuzzing where applicable,
and the complete Go test suite. Golden screenshots should be reserved for
paint behavior that cannot be expressed more precisely as geometry or display
commands.
