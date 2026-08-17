# Gossamer general roadmap

This roadmap tracks the full project direction and is not CSS-only.

## Near-term (core platform)
- Expand DOM/API compatibility with high-priority web behaviors already covered by milestones.
- Improve native shell stability and parity in the interactive path.
- Consolidate tab/history/session behavior, including navigation lifecycles, back-forward cache, and lifecycle events.
- Improve startup and boot path (`document.readyState`, script/module loading, and production-like module orchestration).
- Strengthen reliability of ownership, lifecycle, and teardown boundaries across queue/task transitions.

## Interaction and navigation
- Continue multi-tab browser shell work: tab creation, switching, closure, and independent tab state.
- Grow input/input-event coverage beyond the current mouse/keyboard/scroll slice.
- Expand navigation semantics and event ordering (`beforeunload`, `pagehide`, `unload`, `pageshow`, `popstate`, `hashchange`).
- Expand native shell behavior around clipboard/input composition, focus flow, and future touch/accessibility surfaces.

## Rendering and styling
- Continue CSS and rendering milestones while keeping a single pipeline.
- Extend layout/paint/hit-testing for richer formatting and contextual styling.
- Increase tree-building and overflow/scroll correctness coverage.

## Integration and quality
- Add platform coverage beyond the current macOS-first backend where feasible.
- Expand test coverage across Go + stock-V8 gates for every new behavior.
- Keep ownership/lifetime and teardown invariants in deterministic tests and stock-V8 conformance tracks.
- Move from broad statements to explicit compatibility boundaries and follow-up milestones.

## Ongoing references
- [CSS engine roadmap](css-engine-roadmap.md)
- [DOM compatibility gate](dom-compatibility.md)
- [Window backend milestone notes](window-backend.md)
- [Production page boot](production-page-boot.md)
- [Stock V8 integration timeline](stock-v8-integration.md)
