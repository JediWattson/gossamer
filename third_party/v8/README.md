# Stock V8 workspace

This directory is the ignored local workspace for Gossamer's unmodified V8
checkout and profiling build. The repository tracks only the exact upstream
pin and the scripts that reproduce it.

```sh
tools/v8/bootstrap.sh
tools/v8/build.sh
tools/v8/test.sh
tools/v8/profile.sh
```

The initial build targets Apple Silicon, produces a symbolized `v8_monolith`
and a C-ABI bridge dylib with its full GN dependency closure, keeps V8's
sandbox enabled, and preserves ICU support. See
`docs/stock-v8-integration.md` for the ownership boundary and profiling
sequence.
