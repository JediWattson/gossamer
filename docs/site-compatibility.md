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
failures with URL and phase, a DOM excerpt, frame dimensions and display-list
command count, plus ownership statistics before and after teardown. The command
exits nonzero if navigation, resources, scripts, diagnostics, rasterization, or
zero-ownership teardown fail.

The harness deliberately does not hide unsupported features or retry with a
different engine. Its first job is to turn a real bundle into a deterministic
compatibility backlog. A failure in both Strand and V8 indicates a shared
browser-kernel boundary; a Strand-only failure identifies native engine or
binding work.

## Current efchat baseline

The shared module scanner now tolerates production-only syntax while extracting
imports, without weakening Strand's strict evaluator. With the stock-V8 oracle,
the current efchat production distribution evaluates its entry module, builds
the initial chat shell, paints a frame, reports zero script failures, and tears
ownership down to zero.

The same distribution currently reaches the evaluator under Strand and then
fails explicitly on private-class syntax (`#`) at `122:42627`. That is now a
Strand language-parity backlog rather than a shared browser-kernel or module
scanner failure. Native parity tests cover the browser utility surface added at
this rung: URL/URLSearchParams, UTF-8 codecs, Uint8Array, navigator,
matchMedia, interval timers, Image, and specialized HTML element identities.
