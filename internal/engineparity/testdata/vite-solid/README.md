# Pinned Solid counter fixture

This fixture is the production JavaScript emitted for `src/main.jsx` by:

- `solid-js` 1.9.14
- `vite-plugin-solid` 2.11.14
- Vite 8.2.0

Rebuild it from this directory with:

```sh
npm ci
npm run build
```

The checked-in bundle is intentionally a single IIFE so both Strand and stock
V8 execute exactly the same compiler output without requiring either engine to
implement Vite's module loader first. Its SHA-256, including the repository's
trailing newline, is:

```text
e944bd261b2745bcd408b54fb3ee51ad0ca26a9932f7739619e3a7074d15d4ae
```

`solid-LICENSE` is the upstream Solid MIT license shipped with the pinned npm
package. Do not hand-edit the generated bundle; change the source, rebuild it,
and update the hash instead.
