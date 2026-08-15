# Gossamer

Gossamer is a browser built in Go.

The first development slice fetches an HTTP document and writes its response
body to standard output:

```sh
go run ./cmd/gossamer https://example.com
```

Gossamer can also tokenize the response, construct its initial DOM, and print a
deterministic tree for development:

```sh
go run ./cmd/gossamer --dump-dom https://example.com
```

The first rendering pipeline can turn that DOM into a deterministic 800x600
PNG:

```sh
go run ./cmd/gossamer --screenshot page.png https://example.com
```

Rendering is split into computed styles, retained layout boxes, a
backend-neutral display list, and a PNG painter. A future window backend can
replay the same display list instead of replacing the page pipeline.

The resource pipeline resolves the document URL and its first `<base href>`,
discovers external stylesheets and image-bearing elements in DOM order, and
fetches them through a bounded navigation-scoped cache. PNG, JPEG, GIF, and
WebP assets can be decoded with a separate pixel budget before they reach the
renderer.

The parser currently covers ordinary document structure, tags, attributes,
comments, initial named and numeric character-reference handling, processing
instructions, text elements, and basic implied end tags. Full script-data
states, legacy character-reference edge cases, table foster parenting,
formatting-element reconstruction, templates, SVG/MathML, encoding sniffing,
and script execution are intentionally deferred while the engine is brought up
in vertical slices. The renderer currently implements a focused CSS and layout
subset: complex selectors, common colors and lengths, block flow, inline text
wrapping, bundled regular/bold fonts, links, backgrounds, margins, and opacity.
