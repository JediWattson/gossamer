# Real-site compatibility harness

`gossamer-sitecheck` boots a production web distribution through the real
Browser/Page pipeline and emits one bounded JSON report. The same command can
select native Strand or the stock-V8 reference engine, so failures can be
assigned to the shared browser kernel or to one JavaScript engine without
changing the fixture.

Run an already-built distribution with Strand:

```sh
GOCACHE=/tmp/gossamer-go-cache go run ./cmd/gossamer-sitecheck \
  --engine=strand \
  --dist /absolute/path/to/web/dist \
  --viewport-width 1280 \
  --viewport-height 720 \
  --screenshot /tmp/site-strand.png
```

Run the same distribution with the pinned stock-V8 build:

```sh
GOCACHE=/tmp/gossamer-go-cache ./tools/v8/sitecheck.sh \
  --engine=v8 \
  --dist /absolute/path/to/web/dist \
  --screenshot /tmp/site-v8.png
```

An absolute HTTP or HTTPS URL can replace `--dist`:

```sh
go run ./cmd/gossamer-sitecheck --engine=strand https://example.com
```

The report includes navigation/resource/script counters, bounded script
failures with URL and phase, a DOM excerpt, frame dimensions, display-list
command count, raster SHA-256, painted/opaque pixel counts, a bounded unique
color count, painted bounds, plus ownership statistics before and after
teardown. Every run rasterizes the final frame even when no PNG is requested,
under a 16-megapixel viewport budget. The command exits
nonzero if navigation, resources, scripts, diagnostics, rasterization, or
zero-ownership teardown fail.

Once a frame is accepted, make it a repeatable visual gate by passing its
reported digest back to a later run:

```sh
go run ./cmd/gossamer-sitecheck \
  --expect-paint-sha256 0123456789abcdef... \
  https://example.com
```

The harness deliberately does not hide unsupported features or retry with a
different engine. Its first job is to turn a real bundle into a deterministic
compatibility backlog. A failure in both Strand and V8 indicates a shared
browser-kernel boundary; a Strand-only failure identifies native engine or
binding work.

## Current efchat baseline

The current efchat production distribution evaluates under Strand, hydrates an
anonymous session, opens the place WebSocket with the session cookie, and can
submit the real `#efchat-input` Enter path. The compatibility gate records the
same `message` envelope efchat would send, verifies the optimistic message in
the DOM, and requires zero live or persistent objects after teardown:

```sh
GOCACHE=/tmp/gossamer-go-cache go run ./cmd/gossamer-efchatcheck \
  --dist /absolute/path/to/efchat/web/dist \
  --message "hello from Strand"
```

The command serves the supplied distribution from an ephemeral local origin.
Its `/api/user` response and WebSocket are deterministic local fakes, so the
gate exercises the production frontend and Gossamer's cookie/socket boundary
without sending a message to efchat.net. A passing report contains
`session.role: "anon"`, `session.cookieOnSocket: true`,
`webSocket.event: "message"`, the requested content, clean navigation, and
zero teardown ownership.

For a manual live session, the Graphite shell can open efchat with Strand:

```sh
go run ./cmd/gossamer-window --engine=strand https://efchat.net/global
```

Unlike `gossamer-efchatcheck`, this command uses efchat's real HTTP and
WebSocket services, so a submitted message is public.

## Fetch and session rung

Document navigation and script-visible `fetch()` now share one Go-owned HTTP
client, redirect policy, and cookie jar for the document lifetime. The engine
boundary carries only copied request and response values; neither Strand nor V8
owns a socket, HTTP response, or Go pointer. Both engines expose the first
`Headers`, `Request`, and `Response` surface, including relative URLs, methods,
headers and bodies, resolved or rejected Promises, `text()`, `json()`,
`arrayBuffer()`, `clone()`, and `bodyUsed`.

The shared gate posts a body and headers through a relative URL, checks response
metadata and JSON cloning, then makes a second session request. Loader coverage
also proves that a real `Set-Cookie` response is carried into the next request
without mutating a caller-provided `http.Client`.

## Storage and identity rung

`localStorage` is now Browser-owned and partitioned by origin;
`sessionStorage` is Page-owned and partitioned by origin. Both survive document
and Realm replacement with a deterministic 5 MiB string quota, while separate
pages do not share session values. `document.cookie` reads and writes the
navigation loader's cookie jar, so cookies set by script, navigation, or fetch
all describe the same session. Strand and V8 share these host contracts and run
the same reload-persistence gate.

## WebSocket rung

WebSocket connections are now Page-owned browser resources. Go performs the
HTTP upgrade, carries the shared session cookies and document `Origin`, masks
client frames, validates server frames and negotiated subprotocols, assembles
fragmented messages, answers ping/close control frames, and closes every live
connection during document replacement or Page teardown. Network goroutines
never enter an engine: they publish open, message, error, and close events as
ordered Realm tasks.

Strand and stock V8 expose the same `WebSocket` constructor, state constants,
text and binary `send()`, explicit `close()`, handler properties, and event
listener surface. Their parity gates use an injected browser socket to prove
that open handlers can send, inbound messages run from the Page queue, close
metadata reaches JavaScript, and no engine retains a Go connection pointer.

## Rendering-fidelity rung

Sitecheck now establishes the requested viewport before navigation, so CSS
media queries, `matchMedia`, viewport units, synchronous geometry reads, final
layout, and rasterization all consume one environment from the first script.
The visual digest includes the viewport dimensions and canonical RGBA pixels;
an expected-digest mismatch is a first-class compatibility failure rather than
an informal screenshot comparison.

The committed 640x360 fidelity fixture covers responsive CSS, Grid, Flex,
positioning, borders, typography, and script-driven class/text mutations.
Native Strand and stock V8 must both produce the same committed pixel SHA-256.
This is the rendering regression floor for real-site work, not a claim of full
CSS fidelity: unsupported effects such as radii, shadows, transforms, and
author web fonts remain explicit follow-up compatibility items.
