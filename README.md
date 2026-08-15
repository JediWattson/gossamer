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

The rendering pipeline can turn that DOM into a deterministic 800x600 PNG:

```sh
go run ./cmd/gossamer --screenshot page.png https://example.com
```

Rendering is split into computed styles, retained layout boxes, a
backend-neutral display list, and a PNG painter. Screenshots load active
external stylesheets and bitmap `<img>` resources, preserve stylesheet DOM
order through the cascade, and place decoded images into inline or block flow.
A future window backend can replay the same display list instead of replacing
the page pipeline.

The resource pipeline resolves the document URL and its first `<base href>`,
discovers external stylesheets and image-bearing elements in DOM order, and
fetches them through a bounded navigation-scoped cache. PNG, JPEG, GIF, and
WebP assets are decoded with per-image and per-navigation pixel budgets before
they reach the renderer. Failed subresources degrade independently, so one bad
stylesheet or image does not prevent the rest of a screenshot.

The parser currently covers ordinary document structure, tags, attributes,
comments, initial named and numeric character-reference handling, processing
instructions, text elements, and basic implied end tags. Full script-data
states, legacy character-reference edge cases, table foster parenting,
formatting-element reconstruction, templates, SVG/MathML, encoding sniffing,
and script execution are intentionally deferred while the engine is brought up
in vertical slices. Because no scripting engine is present, body `<noscript>`
fallback content is rendered. The renderer currently implements a focused CSS
and layout subset: complex selectors, common colors and lengths, block flow,
inline text and image wrapping, intrinsic and author-specified image dimensions,
content-box width and height constraints, margins, padding, text alignment,
line height, bundled regular/bold fonts, links, backgrounds, and opacity. CSS
imports, full media-query evaluation, generated content, borders, floats,
advanced display modes, broken-image UI, SVG, and AVIF are not implemented yet.
