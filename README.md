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

The parser currently covers ordinary document structure, tags, attributes,
comments, initial named and numeric character-reference handling, processing
instructions, text elements, and basic implied end tags. Full script-data
states, legacy character-reference edge cases, table foster parenting,
formatting-element reconstruction, templates, SVG/MathML, encoding sniffing,
and script execution are intentionally deferred while the engine is brought up
in vertical slices.
