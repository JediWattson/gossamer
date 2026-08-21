# Pinned Solid counter fixture

This fixture is the production JavaScript emitted for `src/main.jsx` by:

- `solid-js` 1.9.14
- `vite-plugin-solid` 2.11.14
- Vite 8.2.0

Rebuild it from this directory with:

```sh
npm ci
npm run build:all
```

The checked-in IIFE keeps the direct engine-parity tests independent of a
module loader. Its SHA-256, including the repository's trailing newline, is:

```text
4d0e91f8d320fa0f26f16860957d895da1a94bd85adcefa3dd5bb5787735e624
```

The checked-in ES module build is a normal split production graph used by the
full navigation pipeline. The entry imports the pinned Solid runtime chunk, so
the shared Strand/stock-V8 gate exercises browser fetching, module linking,
live bindings, one-time evaluation, interaction, disposal, and teardown. Their
SHA-256 values are:

```text
20ca971a3b769bd7edb4a85a18ca4897467176b49e85df5db922af375b47cd1b  solid-parity-1.9.14.production.module.js
54d300c453e950707a3c2ed4af412b0b6e35dcda3f306be2e44dfdfb0c6d2b94  solid-runtime-1.9.14.production.module.js
```

`solid-LICENSE` is the upstream Solid MIT license shipped with the pinned npm
package. Do not hand-edit the generated bundle; change the source, rebuild it,
and update the hash instead.
