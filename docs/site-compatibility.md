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

The current efchat production distribution reaches HTML, CSS, DOM indexing,
layout, painting, and clean ownership teardown under both engines. Its first
shared blocker is module-graph scanning of JavaScript private-name syntax (`#`)
in the production entry chunk. Neither engine evaluates the entry module until
that shared scanner accepts the graph.
